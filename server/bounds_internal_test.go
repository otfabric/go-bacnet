// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"errors"
	"net/netip"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/npdu"
	"github.com/otfabric/go-bacnet/server/object"
	"github.com/otfabric/go-bacnet/service"
)

// --- COV notifications of replaced subscriptions -------------------------

var operatorEP = bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.9:47808"))

func (h *harness) pendingInvokeIDs() int {
	h.s.req.mu.Lock()
	defer h.s.req.mu.Unlock()
	return len(h.s.req.pending)
}

func (h *harness) sentTo(to bip.Endpoint, match func(sent) bool) int {
	n := 0
	for _, p := range h.tr.Outbox() {
		if p.Destination.Equal(to) && match(decodeSent(h.t, p)) {
			n++
		}
	}
	return n
}

func isConfirmed(svc uint8) func(sent) bool {
	return func(s sent) bool {
		return s.pdu.Type == apdu.TypeConfirmedRequest && s.pdu.ConfirmedRequest.ServiceChoice == svc
	}
}

// Renewing a confirmed subscription from a silent subscriber ends the
// notification of the subscription it replaces, so invoke IDs stay free
// for other confirmed notifications.
func TestCOVRenewalReleasesPendingNotification(t *testing.T) {
	h := newHarness(t, WithCOVLimits(1, time.Minute), WithTransactionOptions(10*time.Second, 0, 200*time.Millisecond))
	nc, err := object.NewNotificationClass(object.NotificationClassConfig{
		Instance: 7, Name: "NC-7",
		Recipients: []object.Destination{{ValidDays: 0x7F, To: allDay, MAC: macOf(operatorEP), ProcessID: 4, Confirmed: true, Transitions: 7}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ai, err := object.NewAnalogInput(object.PointConfig{Instance: 9, Name: "AI-9", Value: 50, Events: &object.EventConfig{
		NotificationClass: 7, EventEnable: 7, NotifyType: object.NotifyAlarm, HighLimit: 80, LowLimit: 20, Deadband: 2,
		LimitEnable: object.LimitEnableHigh | object.LimitEnableLow,
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range []object.Object{nc, ai} {
		if err := h.dev.Add(o); err != nil {
			t.Fatal(err)
		}
	}
	payload := mustEncode(t)(service.EncodeSubscribeCOV(service.SubscribeCOVRequest{
		ProcessIdentifier: 1, MonitoredObject: av1, IssueConfirmed: true, Lifetime: 30,
	}))
	notes := isConfirmed(apdu.ServiceConfirmedCOVNotification)
	for i := 0; i < 256; i++ {
		h.confirmed(uint8(i), apdu.ServiceSubscribeCOV, payload)
		want := i + 1
		waitUntil(t, func() bool { return h.sentTo(clientEP, notes) >= want })
	}
	waitUntil(t, func() bool { return h.pendingInvokeIDs() <= 1 })
	if n := h.s.cov.count(); n != 1 {
		t.Fatalf("%d subscriptions", n)
	}
	if err := ai.SetPresentValue(bacnet.RealValue(95)); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, func() bool { return h.sentTo(operatorEP, isConfirmed(apdu.ServiceConfirmedEventNotification)) > 0 })
	if p := h.pendingInvokeIDs(); p > 2 {
		t.Fatalf("%d invoke IDs pending", p)
	}
}

// The references of all subscriptions are bounded, a
// SubscribeCOVPropertyMultiple is bounded so that its notification fits one
// APDU, and a notification that still does not fit is not sent.
func TestCOVReferenceBounds(t *testing.T) {
	h := newHarness(t, WithCOVReferenceLimit(5))
	sub := func(inv uint8, pid uint32) {
		h.confirmed(inv, apdu.ServiceSubscribeCOV, mustEncode(t)(service.EncodeSubscribeCOV(service.SubscribeCOVRequest{ProcessIdentifier: pid, MonitoredObject: av1, Lifetime: 60})))
	}
	sub(1, 1) // 2 references
	h.expectType(apdu.TypeSimpleACK)
	h.notification(apdu.ServiceUnconfirmedCOV)
	sub(2, 2) // 4
	h.expectType(apdu.TypeSimpleACK)
	h.notification(apdu.ServiceUnconfirmedCOV)
	sub(3, 3) // 6: beyond the limit
	h.expectError(bacnet.ErrorClassResources, bacnet.ErrorCodeNoSpaceToAddListElement)
	sub(4, 2) // a renewal keeps the count
	h.expectType(apdu.TypeSimpleACK)
	h.notification(apdu.ServiceUnconfirmedCOV)
	h.confirmed(5, apdu.ServiceSubscribeCOV, mustEncode(t)(service.EncodeSubscribeCOV(service.SubscribeCOVRequest{ProcessIdentifier: 1, MonitoredObject: av1, Cancellation: true})))
	h.expectType(apdu.TypeSimpleACK)
	sub(6, 3) // room again after the cancellation
	h.expectType(apdu.TypeSimpleACK)
	h.notification(apdu.ServiceUnconfirmedCOV)
	h.s.cov.mu.Lock()
	refs := h.s.cov.refs
	h.s.cov.mu.Unlock()
	if refs != 4 {
		t.Fatalf("%d references counted", refs)
	}
	h.s.cov.objectRemoved(av1)
	h.s.cov.mu.Lock()
	refs = h.s.cov.refs
	h.s.cov.mu.Unlock()
	if refs != 0 || h.s.cov.count() != 0 {
		t.Fatalf("%d references, %d subscriptions after removal", refs, h.s.cov.count())
	}

	// Per subscription.
	h2 := newHarness(t, WithMaxAPDU(480))
	per := h2.s.covRefsPerSubscription()
	refsOf := func(n int) []service.COVPropertyReference {
		out := make([]service.COVPropertyReference, n)
		for i := range out {
			out[i] = service.COVPropertyReference{Property: pvRef, Timestamped: true}
		}
		return out
	}
	life := uint32(60)
	scovpm := func(inv uint8, n int) {
		h2.confirmed(inv, apdu.ServiceSubscribeCOVPropertyMultiple, mustEncode(t)(service.EncodeSubscribeCOVPropertyMultiple(service.SubscribeCOVPropertyMultipleRequest{
			SubscriberProcessIdentifier: 1, LifetimeRemaining: &life, Subscriptions: []service.COVMultipleSubscription{{Object: av1, Properties: refsOf(n)}},
		})))
	}
	scovpm(1, per+1)
	h2.expectType(apdu.TypeError)
	scovpm(2, per)
	h2.expectType(apdu.TypeSimpleACK)
	note := h2.notification(apdu.ServiceUnconfirmedCOVNotificationMultiple)
	if len(note)+2 > 480 {
		t.Fatalf("notification of %d octets", len(note)+2)
	}

	// A notification larger than one APDU is not sent.
	h3 := newHarness(t, WithMaxAPDU(50))
	long, err := object.NewAnalogValue(object.PointConfig{Instance: 2, Name: strings.Repeat("n", 120)})
	if err != nil {
		t.Fatal(err)
	}
	if err := h3.dev.Add(long); err != nil {
		t.Fatal(err)
	}
	h3.confirmed(1, apdu.ServiceSubscribeCOVProperty, mustEncode(t)(service.EncodeSubscribeCOVProperty(service.SubscribeCOVPropertyRequest{
		SubscribeCOVRequest: service.SubscribeCOVRequest{ProcessIdentifier: 1, MonitoredObject: long.Identifier(), Lifetime: 60},
		Property:            bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName},
	})))
	h3.expectType(apdu.TypeSimpleACK)
	h3.quiet(100 * time.Millisecond)
	if !h3.diagnosed("cov") {
		t.Fatal("oversized notification not reported")
	}
}

// Active_COV_Subscriptions stops growing once it passes the largest reply.
func TestActiveCOVSubscriptionsBounded(t *testing.T) {
	h := newHarness(t, WithMaxAPDU(128), WithSegmentation(2, 1), WithCOVLimits(1000, 0))
	for pid := uint32(1); pid <= 50; pid++ {
		sub := &covSub{kind: subObject, key: strconv.Itoa(int(pid)), to: clientEP, pid: pid,
			targets: []*covTarget{{obj: av1, refs: []*covRef{{prop: pvRef}, {prop: bacnet.PropertyReference{Identifier: bacnet.PropertyStatusFlags}}}}}}
		if _, err := h.s.cov.upsert(sub); err != nil {
			t.Fatal(err)
		}
	}
	els := h.s.cov.activeSubscriptions()
	if len(els) == 0 || len(els) >= 50*4 {
		t.Fatalf("%d elements", len(els))
	}
	var size int
	for _, el := range els {
		b, _ := bacnet.AppendTag(nil, el)
		size += len(b)
	}
	if max := h.s.maxReply(); size <= max || size > max+200 {
		t.Fatalf("list of %d octets, reply limit %d", size, max)
	}
}

// --- ReadPropertyMultiple reply budget ------------------------------------

func rpmObjectLists(t *testing.T, refs int) []byte {
	devID := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 100}
	props := make([]bacnet.PropertyReference, refs)
	for i := range props {
		props[i] = bacnet.PropertyReference{Identifier: bacnet.PropertyObjectList}
	}
	return mustEncode(t)(service.EncodeReadPropertyMultiple([]service.ReadAccessSpecification{{Object: devID, Properties: props}}))
}

// A ReadPropertyMultiple whose reply cannot fit stops at the reply budget
// instead of encoding the whole answer first.
func TestReadPropertyMultipleStopsAtBudget(t *testing.T) {
	h := newHarness(t)
	for i := 2; i < 500; i++ {
		av, err := object.NewAnalogValue(object.PointConfig{Instance: uint32(i), Name: "AV-" + strconv.Itoa(i)})
		if err != nil {
			t.Fatal(err)
		}
		if err := h.dev.Add(av); err != nil {
			t.Fatal(err)
		}
	}
	payload := rpmObjectLists(t, 700)
	req := &apdu.ConfirmedRequest{SegmentedResponseAccepted: true, MaxSegments: 7, MaxAPDU: 5, InvokeID: 1, ServiceChoice: apdu.ServiceReadPropertyMultiple, Payload: payload}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	res := h.s.readPropertyMultiple(context.Background(), req, peer{})
	runtime.ReadMemStats(&after)
	var ab *abortError
	if !errors.As(res.err, &ab) || ab.reason != bacnet.AbortReasonBufferOverflow {
		t.Fatalf("result %+v", res)
	}
	if alloc := after.TotalAlloc - before.TotalAlloc; alloc > 32<<20 {
		t.Fatalf("allocated %d octets", alloc)
	}
	req.SegmentedResponseAccepted = false
	res = h.s.readPropertyMultiple(context.Background(), req, peer{})
	if !errors.As(res.err, &ab) || ab.reason != bacnet.AbortReasonSegmentationNotSupported {
		t.Fatalf("unsegmented result %+v", res)
	}
	// A reply that fits is unchanged.
	req.SegmentedResponseAccepted = true
	req.Payload = rpmObjectLists(t, 2)
	res = h.s.readPropertyMultiple(context.Background(), req, peer{})
	acks, err := service.DecodeReadPropertyMultipleACK(res.payload, bacnet.DefaultDecodeLimits())
	if err != nil || len(acks) != 1 || len(acks[0].Properties) != 2 || len(acks[0].Properties[0].Value.Elements) != 500 {
		t.Fatalf("reply: %v %+v", err, res)
	}
}

// --- AtomicWriteFile and the BeforeWrite hook -----------------------------

func TestAtomicWriteFileBeforeWrite(t *testing.T) {
	var mu sync.Mutex
	var seen []object.Write
	var refuse bool
	h := newHarness(t, WithHooks(Hooks{BeforeWrite: func(_ context.Context, id bacnet.ObjectIdentifier, w object.Write) error {
		mu.Lock()
		defer mu.Unlock()
		if id.Type == bacnet.ObjectTypeFile {
			seen = append(seen, w)
		}
		if refuse {
			return object.ErrWriteAccessDenied
		}
		return nil
	}}))
	f, err := object.NewFile(object.FileConfig{Instance: 7, Name: "cfg", Data: []byte("original")})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.dev.Add(f); err != nil {
		t.Fatal(err)
	}
	id := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 7}
	write := mustEncode(t)(service.EncodeAtomicWriteFile(service.AtomicWriteFileRequest{File: id, Access: service.FileAccessStream, StartPosition: -1, Data: []byte("+X")}))
	mu.Lock()
	refuse = true
	mu.Unlock()
	h.confirmed(1, apdu.ServiceAtomicWriteFile, write)
	h.expectError(bacnet.ErrorClassProperty, bacnet.ErrorCodeWriteAccessDenied)
	if string(f.Data()) != "original" {
		t.Fatalf("file changed: %q", f.Data())
	}
	mu.Lock()
	refuse = false
	mu.Unlock()
	h.confirmed(2, apdu.ServiceAtomicWriteFile, write)
	h.expectType(apdu.TypeComplexACK)
	if string(f.Data()) != "original+X" {
		t.Fatalf("file %q", f.Data())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 || seen[0].Property.Identifier != bacnet.PropertyFileSize {
		t.Fatalf("hook saw %+v", seen)
	}
	els := seen[0].Value.Elements
	if len(els) != 2 || els[0].Value.Signed != -1 || string(els[1].Value.OctetString) != "+X" {
		t.Fatalf("write value %+v", seen[0].Value)
	}
}

// A Schedule's writes to its references pass the BeforeWrite hook.
func TestScheduleWritesPassBeforeWrite(t *testing.T) {
	for _, refuse := range []bool{true, false} {
		h := newHarness(t, WithHooks(Hooks{BeforeWrite: func(_ context.Context, id bacnet.ObjectIdentifier, _ object.Write) error {
			if refuse && id == av1 {
				return object.ErrWriteAccessDenied
			}
			return nil
		}}))
		sch, err := object.NewSchedule(object.ScheduleConfig{Instance: 1, Name: "sch", Default: bacnet.RealValue(5),
			References: []object.DeviceObjectPropertyReference{{Object: av1, Property: bacnet.PropertyPresentValue}}})
		if err != nil {
			t.Fatal(err)
		}
		if err := h.dev.Add(sch); err != nil {
			t.Fatal(err)
		}
		sch.Poll(context.Background(), time.Now(), h.dev)
		v, err := h.av.ReadProperty(context.Background(), pvRef)
		if err != nil {
			t.Fatal(err)
		}
		if want := map[bool]float32{true: 1, false: 5}[refuse]; v.Real != want {
			t.Fatalf("refuse=%v: Present_Value %v, want %v", refuse, v.Real, want)
		}
	}
}

// --- GetEventInformation prefix search ------------------------------------

// A large event list against a small budget is answered quickly with the
// largest prefix that fits.
func TestGetEventInformationPrefix(t *testing.T) {
	h := newHarness(t, WithMaxAPDU(128), WithSegmentation(0, 0))
	for i := 0; i < 300; i++ {
		ai, err := object.NewAnalogInput(object.PointConfig{Instance: uint32(10 + i), Name: "AI-" + strconv.Itoa(i), Value: 95, Events: &object.EventConfig{
			EventEnable: 7, NotifyType: object.NotifyAlarm, HighLimit: 80, LowLimit: 20, LimitEnable: object.LimitEnableHigh,
		}})
		if err != nil {
			t.Fatal(err)
		}
		if err := h.dev.Add(ai); err != nil {
			t.Fatal(err)
		}
	}
	h.s.events.tick()
	waitUntil(t, func() bool { return len(h.s.eventInfos()) >= 300 })
	req := &apdu.ConfirmedRequest{MaxAPDU: 5, InvokeID: 1, ServiceChoice: apdu.ServiceGetEventInformation}
	start := time.Now()
	res := h.s.getEventInformation(context.Background(), req, peer{})
	if d := time.Since(start); d > time.Second {
		t.Fatalf("took %v", d)
	}
	if res.err != nil || len(res.payload) > h.s.responseBudget(req) {
		t.Fatalf("result %+v", res)
	}
	ack, err := service.DecodeGetEventInformationACK(res.payload, bacnet.DefaultDecodeLimits())
	if err != nil || !ack.MoreEvents || len(ack.Summaries) == 0 {
		t.Fatalf("ack %v %+v", err, ack)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if res := h.s.getEventInformation(ctx, req, peer{}); res.err == nil {
		t.Fatal("cancelled request answered")
	}
}

// --- Segmented requests and replies ----------------------------------------

func (h *harness) firstSegment(from bip.Endpoint, station byte, invokeID uint8, payload []byte) {
	h.t.Helper()
	a := apdu.AppendConfirmedRequest(nil, apdu.ConfirmedRequest{
		SegmentedMessage: true, MoreFollows: true, SegmentedResponseAccepted: true,
		MaxAPDU: 5, InvokeID: invokeID, ProposedWindowSize: 4,
		ServiceChoice: apdu.ServiceWriteProperty, Payload: payload,
	})
	src := bacnet.RemoteStation(9, bacnet.MustMAC([]byte{station}))
	h.injectFrom(from, bvlc.FunctionOriginalUnicastNPDU, npdu.NPDU{Version: npdu.Version1, ExpectingReply: true, Source: src, APDU: a})
}

func (h *harness) aborts(reason uint8) int {
	n := 0
	for _, p := range h.tr.Outbox() {
		s := decodeSent(h.t, p)
		if s.pdu.Type == apdu.TypeAbort && s.pdu.Abort.Reason == reason {
			n++
		}
	}
	return n
}

func (h *harness) reassembly() (open, octets, tx int) {
	r := h.s.resp
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.open, r.octets, len(r.tx)
}

// Open reassemblies are bounded per origin, in total and in buffered
// octets, and their shares are given back.
func TestReassemblyLimits(t *testing.T) {
	const per = 4
	h := newHarness(t, WithSegmentLimits(SegmentLimits{ReassembliesPerOrigin: per, Reassemblies: 6, ReassemblyOctets: 3000}))
	data := make([]byte, 400)
	for i := 0; i <= per; i++ {
		h.firstSegment(clientEP, byte(i), uint8(i), data)
	}
	waitUntil(t, func() bool { return h.aborts(bacnet.AbortReasonOutOfResources) == 1 })
	if open, octets, tx := h.reassembly(); open != per || tx > per || octets != per*400 {
		t.Fatalf("open %d octets %d tx %d", open, octets, tx)
	}
	// Other origins: two more fit the total of six.
	for i := 0; i < 3; i++ {
		from := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0." + strconv.Itoa(20+i) + ":47808"))
		h.firstSegment(from, 1, 1, data[:100])
	}
	waitUntil(t, func() bool { return h.aborts(bacnet.AbortReasonOutOfResources) == 2 })
	if open, _, _ := h.reassembly(); open != 6 {
		t.Fatalf("open %d", open)
	}
	// An abort from the requester gives the share back.
	h.injectFrom(clientEP, bvlc.FunctionOriginalUnicastNPDU, npdu.NPDU{Version: npdu.Version1, Source: bacnet.RemoteStation(9, bacnet.MustMAC([]byte{0})),
		APDU: apdu.AppendAbort(nil, apdu.AbortPDU{InvokeID: 0, Reason: bacnet.AbortReasonOther})})
	waitUntil(t, func() bool { open, _, _ := h.reassembly(); return open == 5 })
	// The octet bound: 3*400 + 2*100 buffered, 1700 more exceed 3000.
	h.firstSegment(bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.30:47808")), 1, 1, make([]byte, 1700))
	waitUntil(t, func() bool { return h.aborts(bacnet.AbortReasonOutOfResources) == 3 })
	if open, octets, _ := h.reassembly(); open != 5 || octets != 3*400+2*100 {
		t.Fatalf("open %d octets %d", open, octets)
	}
	// Expiry gives every share back.
	h.s.resp.expire(time.Now().Add(time.Hour))
	if open, octets, tx := h.reassembly(); open != 0 || octets != 0 || tx != 0 {
		t.Fatalf("after expiry: open %d octets %d tx %d", open, octets, tx)
	}
	h.s.resp.mu.Lock()
	n := len(h.s.resp.openFrom)
	h.s.resp.mu.Unlock()
	if n != 0 {
		t.Fatalf("%d origins still counted", n)
	}
}

// A completed segmented request gives its share back and is executed.
func TestReassemblyShareReleasedOnCompletion(t *testing.T) {
	h := newHarness(t)
	payload := mustEncode(t)(service.EncodeWriteProperty(service.WritePropertyRequest{Object: av1, Property: pvRef, Value: bacnet.RealValue(7)}))
	half := len(payload) / 2
	for i, part := range [][]byte{payload[:half], payload[half:]} {
		h.inject(apdu.AppendConfirmedRequest(nil, apdu.ConfirmedRequest{
			SegmentedMessage: true, MoreFollows: i == 0, SegmentedResponseAccepted: true, MaxAPDU: 5, InvokeID: 3,
			SequenceNumber: uint8(i), ProposedWindowSize: 4, ServiceChoice: apdu.ServiceWriteProperty, Payload: part,
		}))
	}
	h.expectType(apdu.TypeSegmentACK)
	h.expectType(apdu.TypeSegmentACK)
	h.expectType(apdu.TypeSimpleACK)
	waitUntil(t, func() bool { open, octets, tx := h.reassembly(); return open == 0 && octets == 0 && tx == 0 })
}

// A segmented reply that is never acknowledged does not hold the only
// worker; replies in progress are bounded per origin, and the transaction
// is released when the reply ends.
func TestSegmentedReplyDoesNotHoldWorker(t *testing.T) {
	h := newHarness(t, WithWorkers(1, 4), WithTransactionOptions(500*time.Millisecond, 1, 300*time.Millisecond),
		WithSegmentLimits(SegmentLimits{RepliesPerOrigin: 1}))
	big := apdu.ConfirmedRequest{SegmentedResponseAccepted: true, MaxAPDU: 0, InvokeID: 10, ServiceChoice: apdu.ServiceReadPropertyMultiple, Payload: readAll(t)}
	h.request(big)
	h.segment()
	// A second segmented reply to the same origin is refused.
	big.InvokeID = 11
	h.request(big)
	h.expectAbort(bacnet.AbortReasonOutOfResources)

	other := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.3:47808"))
	start := time.Now()
	h.injectFrom(other, bvlc.FunctionOriginalUnicastNPDU, npdu.NPDU{Version: npdu.Version1, ExpectingReply: true,
		APDU: apdu.AppendConfirmedRequest(nil, apdu.ConfirmedRequest{MaxAPDU: 5, InvokeID: 1, ServiceChoice: apdu.ServiceReadProperty, Payload: readPV(t)})})
	waitUntil(t, func() bool {
		return h.sentTo(other, func(s sent) bool { return s.pdu.Type == apdu.TypeComplexACK }) == 1
	})
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Fatalf("answered after %v", d)
	}
	// The unacknowledged reply ends with Abort TSM timeout and gives its
	// share and its transaction back.
	waitUntil(t, func() bool { return h.aborts(bacnet.AbortReasonTSMTimeout) == 1 })
	waitUntil(t, func() bool {
		r := h.s.resp
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.replies == 0 && len(r.repliesFrom) == 0 && len(r.tx) == 0
	})
}
