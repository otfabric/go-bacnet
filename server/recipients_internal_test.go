// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/netip"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/internal/virtual"
	"github.com/otfabric/go-bacnet/npdu"
	"github.com/otfabric/go-bacnet/server/object"
	"github.com/otfabric/go-bacnet/service"
)

// What the server sends on its own account is bounded per recipient: one
// Who-Is per unbound device and answer window, one Who-Is-Router-To-Network
// per network, a fixed number of confirmed requests waiting per
// destination, and address tables that keep what the server reports to.

func deviceID(instance uint32) bacnet.ObjectIdentifier {
	return bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: instance}
}

func endpointN(n int) bip.Endpoint {
	return bip.NewEndpoint(netip.AddrPortFrom(netip.AddrFrom4([4]byte{10, 1, byte(n >> 8), byte(n)}), 47808))
}

// reportingAI adds AI-9 (high limit 80) reporting through Notification
// Class class.
func reportingAI(t *testing.T, h *harness, class uint32) *object.Point {
	t.Helper()
	ai, err := object.NewAnalogInput(object.PointConfig{Instance: 9, Name: "AI-9", Value: 50, Events: &object.EventConfig{
		NotificationClass: class, EventEnable: 7, NotifyType: object.NotifyAlarm, HighLimit: 80, LowLimit: 20, Deadband: 2,
		LimitEnable: object.LimitEnableHigh | object.LimitEnableLow,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.dev.Add(ai); err != nil {
		t.Fatal(err)
	}
	return ai
}

// deviceRecipients adds Notification Class class with one unconfirmed
// device recipient per instance.
func deviceRecipients(t *testing.T, h *harness, class uint32, ackRequired uint8, instances ...uint32) {
	t.Helper()
	var dests []object.Destination
	for _, inst := range instances {
		id := deviceID(inst)
		dests = append(dests, object.Destination{ValidDays: 0x7F, To: allDay, Device: &id, ProcessID: 1, Transitions: 7})
	}
	nc, err := object.NewNotificationClass(object.NotificationClassConfig{Instance: class, Name: fmt.Sprintf("NC-%d", class), AckRequired: ackRequired, Recipients: dests})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.dev.Add(nc); err != nil {
		t.Fatal(err)
	}
}

func (h *harness) iAmFrom(from bip.Endpoint, instance uint32) {
	h.t.Helper()
	p := mustEncode(h.t)(service.EncodeIAm(service.IAm{Device: deviceID(instance), MaxAPDULength: 1476, VendorID: 1}))
	h.injectFrom(from, bvlc.FunctionOriginalBroadcastNPDU, npdu.NPDU{Version: npdu.Version1,
		APDU: apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{ServiceChoice: apdu.ServiceIAm, Payload: p})})
}

func (h *harness) bound(instance uint32) (bip.Endpoint, bool) {
	e := h.s.events
	e.bmu.Lock()
	defer e.bmu.Unlock()
	b, ok := e.bindings[instance]
	return b.to, ok
}

func (h *harness) bindings() int {
	e := h.s.events
	e.bmu.Lock()
	defer e.bmu.Unlock()
	return len(e.bindings)
}

func isWhoIs(s sent) bool {
	return s.pdu.Type == apdu.TypeUnconfirmedRequest && s.pdu.UnconfirmedRequest.ServiceChoice == apdu.ServiceWhoIs
}

func ackAlarm(t *testing.T, state uint32, stamp time.Time) []byte {
	return mustEncode(t)(service.EncodeAcknowledgeAlarm(service.AcknowledgeAlarmRequest{
		ProcessIdentifier: 1, EventObject: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogInput, Instance: 9},
		EventStateAcked: state, TimeStamp: object.TimeStampOf(stamp), AcknowledgmentSource: bacnet.CharacterString{Value: "op"},
		TimeOfAcknowledgment: service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 2},
	}))
}

// An AcknowledgeAlarm for a transition that is acknowledged already is
// answered and changes nothing: nothing is sent to the recipients. One
// that acknowledges a transition is reported to them once.
func TestAcknowledgeAlarmReportedWhenItAcknowledges(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		h := newHarness(t)
		var recipients []uint32
		for i := range 32 {
			recipients = append(recipients, uint32(200+i))
		}
		deviceRecipients(t, h, 7, 7, recipients...)
		ai := reportingAI(t, h, 7)
		h.silent(0)

		// NORMAL, nothing to acknowledge: ten requests, ten answers.
		for i := range 10 {
			h.confirmed(uint8(i), apdu.ServiceAcknowledgeAlarm, ackAlarm(t, object.EventStateNormal, time.Time{}))
			got := h.next()
			if got.pdu.Type != apdu.TypeSimpleACK {
				t.Fatalf("request %d: the server sent %+v, want only the SimpleACK", i+1, got.pdu)
			}
		}
		h.silent(0)

		// A transition that needs an acknowledgment: one Who-Is per unbound
		// recipient for its notification.
		if err := ai.SetPresentValue(bacnet.RealValue(95)); err != nil {
			t.Fatal(err)
		}
		for i := range recipients {
			if got := h.next(); !isWhoIs(got) {
				t.Fatalf("packet %d after the transition: %+v", i+1, got.pdu)
			}
		}
		h.silent(0)
		// The recipients announce themselves, so the acknowledgment has
		// somewhere to go.
		for i, inst := range recipients {
			h.iAmFrom(endpointN(i+1), inst)
			idle()
		}
		info, _ := ai.EventInfo()
		stamp := info.TimeStamps[object.TransitionToOffNormal]
		h.confirmed(20, apdu.ServiceAcknowledgeAlarm, ackAlarm(t, object.EventStateHighLimit, stamp))
		h.expectType(apdu.TypeSimpleACK)
		for i := range recipients {
			got := h.next()
			if got.pdu.Type != apdu.TypeUnconfirmedRequest || got.pdu.UnconfirmedRequest.ServiceChoice != apdu.ServiceUnconfirmedEventNotification {
				t.Fatalf("acknowledgment notification %d: %+v", i+1, got.pdu)
			}
		}
		h.silent(0)
		// The same acknowledgment again: answered, not reported again.
		h.confirmed(21, apdu.ServiceAcknowledgeAlarm, ackAlarm(t, object.EventStateHighLimit, stamp))
		h.expectType(apdu.TypeSimpleACK)
		h.silent(0)
	})
}

// A device recipient that is not bound gets one Who-Is per answer window,
// however many notifications are skipped meanwhile.
func TestWhoIsForUnboundRecipientOncePerWindow(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		h := newHarness(t)
		deviceRecipients(t, h, 7, 0, 200)
		ai := reportingAI(t, h, 7)
		h.silent(0)
		set := func(v float32) {
			t.Helper()
			if err := ai.SetPresentValue(bacnet.RealValue(v)); err != nil {
				t.Fatal(err)
			}
		}
		set(95) // to-offnormal: the first notification for device 200
		if got := h.next(); !isWhoIs(got) || !got.pkt.Broadcast {
			t.Fatalf("first transition: %+v", got.pdu)
		}
		for _, v := range []float32{50, 95, 50} { // three more transitions at once
			set(v)
			idle()
			h.nothingSent()
		}
		if n := len(h.diagnostics("event")); n != 4 {
			t.Fatalf("%d skipped notifications diagnosed, want 4", n)
		}
		// After the window a notification asks again.
		time.Sleep(h.s.answerWindow())
		set(95)
		if got := h.next(); !isWhoIs(got) {
			t.Fatalf("after the window: %+v", got.pdu)
		}
		h.silent(0)
	})
}

// The address cache keeps the devices the server reports to. I-Ams of other
// devices take no room, so a recipient is bound whenever it announces
// itself, and its notification goes to it.
func TestAddressCacheKeepsEventRecipients(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		h := newHarness(t)
		deviceRecipients(t, h, 7, 0, 200)
		ai := reportingAI(t, h, 7)
		h.silent(0)
		for i := range maxBindings + 44 {
			h.iAmFrom(endpointN(i+1), uint32(1000+i))
			idle()
		}
		if n := h.bindings(); n != 0 {
			t.Fatalf("%d addresses kept for devices that are no event recipients", n)
		}
		recipient := endpointN(9000)
		h.iAmFrom(recipient, 200)
		idle()
		if ep, ok := h.bound(200); !ok || ep != recipient {
			t.Fatalf("recipient 200 bound to %v (%v) after its I-Am", ep, ok)
		}
		if err := ai.SetPresentValue(bacnet.RealValue(95)); err != nil {
			t.Fatal(err)
		}
		got := h.next()
		if got.pkt.Destination != recipient || got.pdu.Type != apdu.TypeUnconfirmedRequest ||
			got.pdu.UnconfirmedRequest.ServiceChoice != apdu.ServiceUnconfirmedEventNotification {
			t.Fatalf("notification %+v to %v, want an event notification to %v", got.pdu, got.pkt.Destination, recipient)
		}
		h.silent(0)
	})
}

// A full address cache makes room for a recipient that announces itself:
// the entry heard from longest ago leaves, and a diagnostic says so.
func TestAddressCacheFullMakesRoom(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		h := newHarness(t)
		// Recipients 1001, 1002, …: two more than the cache holds.
		const recipients, first = maxBindings + 2, 1001
		for class := range uint32((recipients + 31) / 32) {
			var instances []uint32
			for i := class * 32; i < min((class+1)*32, recipients); i++ {
				instances = append(instances, first+i)
			}
			deviceRecipients(t, h, 10+class, 0, instances...)
		}
		announce := func(inst uint32) {
			h.iAmFrom(endpointN(int(inst)), inst)
			idle()
			time.Sleep(time.Millisecond) // the bubble's clock: every I-Am has its own time
		}
		for i := range uint32(maxBindings) {
			announce(first + i)
		}
		if n := h.bindings(); n != maxBindings || len(h.diagnostics("registry_eviction")) != 0 {
			t.Fatalf("%d addresses, %d evictions", n, len(h.diagnostics("registry_eviction")))
		}
		announce(first)               // heard again: the second one is now the oldest
		announce(first + maxBindings) // no room: the second one leaves
		if _, ok := h.bound(first + maxBindings); !ok {
			t.Fatal("the recipient that announced itself is not bound")
		}
		if _, ok := h.bound(first + 1); ok {
			t.Fatal("the entry heard from longest ago is still there")
		}
		if _, ok := h.bound(first); !ok {
			t.Fatal("the entry that was heard again left")
		}
		ev := h.diagnostics("registry_eviction")
		if n := h.bindings(); n != maxBindings || len(ev) != 1 || ev[0].Fields["device"] != uint32(first+1) {
			t.Fatalf("%d addresses, evictions %+v", n, ev)
		}
	})
}

func (h *harness) fromNetwork(router bip.Endpoint, network uint16) {
	who := apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{ServiceChoice: apdu.ServiceIHave})
	h.injectFrom(router, bvlc.FunctionOriginalBroadcastNPDU, npdu.NPDU{Version: npdu.Version1,
		Source: bacnet.RemoteStation(network, bacnet.MustMAC([]byte{1})), APDU: who})
}

func (h *harness) iAmRouter(router bip.Endpoint, networks ...uint16) {
	var data []byte
	for _, n := range networks {
		data = append(data, byte(n>>8), byte(n))
	}
	h.injectFrom(router, bvlc.FunctionOriginalBroadcastNPDU, npdu.NPDU{Version: npdu.Version1, NetworkMessage: true,
		NetMsgType: npdu.NetMsgIAmRouterToNetwork, NetMsgData: data})
}

// A full router table makes room for a network that shows up: the route
// heard from longest ago leaves, and a diagnostic says so. A route that is
// heard again is updated.
func TestRouteTableFullMakesRoom(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		h := newHarness(t)
		router, other := endpointN(1), endpointN(2)
		for n := uint16(1); n <= maxRoutes; n++ {
			h.fromNetwork(router, n)
			idle()
			time.Sleep(time.Millisecond)
		}
		h.fromNetwork(other, 1) // network 1 is heard again, through another node
		idle()
		time.Sleep(time.Millisecond)
		if ep, _ := h.s.routes.lookup(1); ep != other {
			t.Fatalf("route 1 via %v, want the newer observation %v", ep, other)
		}
		h.fromNetwork(router, maxRoutes+1)
		idle()
		if ep, ok := h.s.routes.lookup(maxRoutes + 1); !ok || ep != router {
			t.Fatalf("network %d not learnt with the table full (%v, %v)", maxRoutes+1, ep, ok)
		}
		if _, ok := h.s.routes.lookup(2); ok {
			t.Fatal("the route heard from longest ago is still there")
		}
		if _, ok := h.s.routes.lookup(1); !ok {
			t.Fatal("the route that was heard again left")
		}
		ev := h.diagnostics("registry_eviction")
		if len(ev) != 1 || ev[0].Fields["network"] != uint16(2) {
			t.Fatalf("evictions %+v", ev)
		}
		h.silent(0)
	})
}

// The router that answers the server's own Who-Is-Router-To-Network keeps
// the route against what is merely observed: another node that shows up for
// the network makes the server ask again, at most once per routeRecheck,
// and the answer decides.
func TestAnsweredRouteIsKept(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		h, ai, _ := remoteHarness(t)
		r1, r2 := endpointN(1), endpointN(2)
		isWhoIsRouter := func(s sent) bool {
			return s.npdu.NetworkMessage && s.npdu.NetMsgType == npdu.NetMsgWhoIsRouterToNetwork
		}
		if err := ai.SetPresentValue(bacnet.RealValue(95)); err != nil {
			t.Fatal(err)
		}
		// One Who-Is-Router-To-Network for network 5 and the unconfirmed
		// notification as a broadcast.
		var asked int
		for range 2 {
			if isWhoIsRouter(h.next()) {
				asked++
			}
		}
		if asked != 1 {
			t.Fatalf("%d Who-Is-Router-To-Network, want 1", asked)
		}
		h.iAmRouter(r1, 5)
		idle()
		if ep, _ := h.s.routes.lookup(5); ep != r1 {
			t.Fatalf("network 5 via %v, want the router that answered, %v", ep, r1)
		}
		// Once the answer window is over, another node forwards traffic of
		// network 5 and announces it unasked: the answered route stays, and
		// no question follows so soon after the last one.
		time.Sleep(h.s.answerWindow())
		h.fromNetwork(r2, 5)
		h.iAmRouter(r2, 5)
		h.silent(0)
		if ep, _ := h.s.routes.lookup(5); ep != r1 {
			t.Fatalf("network 5 via %v after an observation, want %v", ep, r1)
		}
		// Later the same observation makes the server ask once; the answer
		// is taken.
		time.Sleep(routeRecheck)
		h.fromNetwork(r2, 5)
		if got := h.next(); !isWhoIsRouter(got) || !got.pkt.Broadcast {
			t.Fatalf("after routeRecheck: %+v", got.npdu)
		}
		h.fromNetwork(r2, 5)
		h.silent(0) // asked a moment ago: not again
		h.iAmRouter(r2, 5)
		idle()
		if ep, _ := h.s.routes.lookup(5); ep != r2 {
			t.Fatalf("network 5 via %v, want the router that answered last, %v", ep, r2)
		}
	})
}

// One destination holds at most maxPendingPerDestination of the server's
// invoke IDs: its further confirmed COV notifications wait their turn, and
// a notification for another destination goes out meanwhile.
func TestConfirmedNotificationsPerDestination(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		const subscriptions = 300 // more than there are invoke IDs
		h := newHarness(t, WithCOVLimits(2*subscriptions, 0))
		other := endpointN(77)
		subscribe := func(from bip.Endpoint, invokeID uint8, process uint32) {
			t.Helper()
			p := mustEncode(t)(service.EncodeSubscribeCOV(service.SubscribeCOVRequest{ProcessIdentifier: process, MonitoredObject: av1, IssueConfirmed: true, Lifetime: 600}))
			h.injectFrom(from, bvlc.FunctionOriginalUnicastNPDU, npdu.NPDU{Version: npdu.Version1, ExpectingReply: true,
				APDU: apdu.AppendConfirmedRequest(nil, apdu.ConfirmedRequest{MaxAPDU: 5, InvokeID: invokeID, ServiceChoice: apdu.ServiceSubscribeCOV, Payload: p})})
		}
		// The first subscriber never answers its notifications.
		notified := 0
		for i := range subscriptions {
			subscribe(clientEP, uint8(i), uint32(i+1))
			for {
				got := h.next()
				if got.pdu.Type == apdu.TypeConfirmedRequest {
					notified++
					continue
				}
				if got.pdu.Type != apdu.TypeSimpleACK {
					t.Fatalf("subscription %d: %+v", i+1, got.pdu)
				}
				break
			}
		}
		idle()
		for {
			out := h.tr.Outbox()
			h.mu.Lock()
			more := h.seen < len(out)
			h.mu.Unlock()
			if !more {
				break
			}
			if got := h.next(); got.pdu.Type == apdu.TypeConfirmedRequest {
				notified++
			}
		}
		if notified != maxPendingPerDestination {
			t.Fatalf("%d notifications wait for an answer from one destination, want %d", notified, maxPendingPerDestination)
		}
		// Another subscriber: its notification is sent at once.
		subscribe(other, 1, 1)
		var gotACK, gotNote bool
		for range 2 {
			got := h.next()
			if got.pkt.Destination != other {
				t.Fatalf("packet to %v while the other subscriber waits: %+v", got.pkt.Destination, got.pdu)
			}
			switch got.pdu.Type {
			case apdu.TypeSimpleACK:
				gotACK = true
			case apdu.TypeConfirmedRequest:
				gotNote = got.pdu.ConfirmedRequest.ServiceChoice == apdu.ServiceConfirmedCOVNotification
			default:
			}
		}
		if !gotACK || !gotNote {
			t.Fatalf("second subscriber: SimpleACK %v, notification %v", gotACK, gotNote)
		}
		for _, d := range h.diagnostics("cov") {
			t.Errorf("diagnostic: %s %v", d.Message, d.Fields)
		}
	})
}

// Event notifications to a destination that does not answer do not queue:
// beyond maxPendingPerDestination they are dropped and diagnosed.
func TestEventNotificationsBeyondDestinationLimitAreDropped(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		h := newHarness(t)
		for i := range maxPendingPerDestination + 3 {
			h.s.req.async(func() {
				err := h.s.req.invoke(h.s.ctx, clientEP, bacnet.Address{}, apdu.ServiceConfirmedEventNotification, []byte{0x09, byte(i)}, false)
				if errors.Is(err, errDestinationBusy) {
					h.s.report("event", "confirmed event notification failed", map[string]any{"error": err.Error()})
				}
			})
			idle()
		}
		sent := 0
		for {
			out := h.tr.Outbox()
			h.mu.Lock()
			more := h.seen < len(out)
			h.mu.Unlock()
			if !more {
				break
			}
			h.next()
			sent++
		}
		if sent != maxPendingPerDestination || len(h.diagnostics("event")) != 3 {
			t.Fatalf("%d requests sent, %d dropped; want %d and 3", sent, len(h.diagnostics("event")), maxPendingPerDestination)
		}
	})
}

// An answer completes the request it belongs to: an ACK or Error for
// another service than the one requested does not, and the request then
// ends as an unanswered one does.
func TestRequesterAnswerForAnotherService(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		h := newHarness(t)
		invoke := func() <-chan error {
			done := make(chan error, 1)
			go func() {
				done <- h.s.req.invoke(context.Background(), clientEP, bacnet.Address{}, apdu.ServiceConfirmedEventNotification, []byte{0x09, 0x01}, false)
			}()
			return done
		}
		errBody := mustEncode(t)(bacnet.EncodeBACnetError(nil, bacnet.ErrorClassServices, bacnet.ErrorCodeOther))
		for name, other := range map[string]func(id uint8) []byte{
			"SimpleACK": func(id uint8) []byte {
				return apdu.AppendSimpleACK(nil, apdu.SimpleACK{InvokeID: id, ServiceChoice: apdu.ServiceConfirmedCOVNotification})
			},
			"ComplexACK": func(id uint8) []byte {
				return apdu.AppendComplexACK(nil, apdu.ComplexACK{InvokeID: id, ServiceChoice: apdu.ServiceReadProperty})
			},
			"Error": func(id uint8) []byte {
				return apdu.AppendError(nil, apdu.ErrorPDU{InvokeID: id, ServiceChoice: apdu.ServiceWriteProperty, Payload: errBody})
			},
		} {
			done := invoke()
			req := h.expectType(apdu.TypeConfirmedRequest).pdu.ConfirmedRequest
			h.inject(other(req.InvokeID))
			idle()
			select {
			case err := <-done:
				t.Fatalf("%s for another service completed the request: %v", name, err)
			default:
			}
			if len(h.diagnostics("wrong_service")) == 0 {
				t.Fatalf("%s for another service not diagnosed", name)
			}
			// It is sent again (one retry in this harness) and then times out.
			h.expectType(apdu.TypeConfirmedRequest)
			if err := within(t, done, 10*time.Second, "the request ends"); !errors.Is(err, bacnet.ErrTimeout) {
				t.Fatalf("%s: request ended with %v, want a timeout", name, err)
			}
		}
		// The answer for the requested service completes it; a Reject and an
		// Abort name no service.
		done := invoke()
		req := h.expectType(apdu.TypeConfirmedRequest).pdu.ConfirmedRequest
		h.inject(apdu.AppendSimpleACK(nil, apdu.SimpleACK{InvokeID: req.InvokeID, ServiceChoice: req.ServiceChoice}))
		if err := within(t, done, 10*time.Second, "the request ends"); err != nil {
			t.Fatalf("answered request: %v", err)
		}
		done = invoke()
		req = h.expectType(apdu.TypeConfirmedRequest).pdu.ConfirmedRequest
		h.inject(apdu.AppendReject(nil, apdu.RejectPDU{InvokeID: req.InvokeID, Reason: bacnet.RejectReasonOther}))
		var rj *bacnet.RejectError
		if err := within(t, done, 10*time.Second, "the request ends"); !errors.As(err, &rj) {
			t.Fatalf("rejected request: %v", err)
		}
	})
}

// A negative SegmentACK that names the last acknowledged segment asks for
// the window again: it is sent at once, not when the segment timer fires,
// and at one position at most as often as there are retries.
func TestSegmentedReplyResentOnNegativeACK(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		h := newHarness(t) // one retry
		h.request(apdu.ConfirmedRequest{SegmentedResponseAccepted: true, MaxSegments: 0, MaxAPDU: 0, InvokeID: 1, ServiceChoice: apdu.ServiceReadPropertyMultiple, Payload: readAll(t)})
		start := time.Now()
		if first := h.segment(); first.SequenceNumber != 0 {
			t.Fatalf("first segment %d", first.SequenceNumber)
		}
		window := func(from uint8, what string) {
			t.Helper()
			for want := from; want < from+3; want++ {
				if s := h.segment(); s.SequenceNumber != want {
					t.Fatalf("%s: segment %d, want %d", what, s.SequenceNumber, want)
				}
			}
		}
		// The requester has nothing in order yet and says so: sequence
		// number 255, the one before segment 0, with its window of three.
		h.segmentACK(1, 255, 3, true)
		window(0, "after the negative ACK for the start")
		h.segmentACK(1, 2, 3, false)
		window(3, "next window")
		// It missed segment 3: the last one it has in order is 2.
		h.segmentACK(1, 2, 3, true)
		window(3, "window sent again")
		if waited := time.Since(start); waited != 0 {
			t.Fatalf("the windows were sent again after %v, want at once", waited)
		}
		// A second negative ACK at the same position is one more than the
		// retries: the segment timer decides from here.
		h.segmentACK(1, 2, 3, true)
		idle()
		h.nothingSent()
		window(3, "when the segment timer fires")
		if waited := time.Since(start); waited != 200*time.Millisecond {
			t.Fatalf("the window was sent again after %v, want the segment timeout", waited)
		}
		// The requester aborts; the reply ends.
		h.inject(apdu.AppendAbort(nil, apdu.AbortPDU{InvokeID: 1, Reason: bacnet.AbortReasonOther}))
		h.silent(0)
	})
}

// SubscribeCOVPropertyMultiple checks a COV increment as SubscribeCOVProperty
// does: a negative one, or one that is not a number, is value-out-of-range.
func TestSubscribeCOVPropertyMultipleIncrement(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		h := newHarness(t)
		nan := math.Float32frombits(0x7FC00000)
		life := uint32(60)
		for i, tc := range []struct {
			increment float32
			ok        bool
		}{{0, true}, {0.5, true}, {-0.5, false}, {nan, false}} {
			inc := tc.increment
			p := mustEncode(t)(service.EncodeSubscribeCOVPropertyMultiple(service.SubscribeCOVPropertyMultipleRequest{
				SubscriberProcessIdentifier: uint32(i + 1), IssueConfirmedNotifications: false, LifetimeRemaining: &life,
				Subscriptions: []service.COVMultipleSubscription{{Object: av1, Properties: []service.COVPropertyReference{{Property: pvRef, COVIncrement: &inc}}}},
			}))
			h.confirmed(uint8(i), apdu.ServiceSubscribeCOVPropertyMultiple, p)
			got := h.next()
			if tc.ok {
				if got.pdu.Type != apdu.TypeSimpleACK {
					t.Fatalf("increment %v: %+v", tc.increment, got.pdu)
				}
				h.expectType(apdu.TypeUnconfirmedRequest) // the initial notification
				continue
			}
			if got.pdu.Type != apdu.TypeError {
				t.Fatalf("increment %v accepted: %+v", tc.increment, got.pdu)
			}
			// errorType [0] { class, code } comes first in the Error.
			els, _, err := bacnet.ParseSequence(got.pdu.Error.Payload, bacnet.DefaultDecodeLimits(), -1)
			if err != nil || len(els) == 0 || len(els[0].Value.Elements) != 2 ||
				els[0].Value.Elements[0].Value.Enumerated != uint32(bacnet.ErrorClassProperty) ||
				els[0].Value.Elements[1].Value.Enumerated != uint32(bacnet.ErrorCodeValueOutOfRange) {
				t.Fatalf("increment %v: error %x (%v), want property/value-out-of-range", tc.increment, got.pdu.Error.Payload, err)
			}
		}
		if n := h.s.cov.count(); n != 2 {
			t.Fatalf("%d subscriptions, want the two with a valid increment", n)
		}
	})
}

// WithRecipientLimits: a zero field keeps its default, a negative one is a
// configuration error.
func TestRecipientLimitsDefaults(t *testing.T) {
	want := RecipientLimits{PendingPerDestination: 8, Addresses: 256, Routes: 64, RouteRecheck: 5 * time.Minute}
	if got := defaultConfig().recipLimits; got != want {
		t.Fatalf("defaults %+v, want %+v", got, want)
	}
	var c config
	WithRecipientLimits(RecipientLimits{})(&c)
	if c.recipLimits != want || len(c.errs) != 0 {
		t.Fatalf("zero limits: %+v %v, want the defaults", c.recipLimits, c.errs)
	}
	c = config{}
	WithRecipientLimits(RecipientLimits{Routes: 3})(&c)
	if w := (RecipientLimits{PendingPerDestination: 8, Addresses: 256, Routes: 3, RouteRecheck: 5 * time.Minute}); c.recipLimits != w {
		t.Fatalf("one field: %+v, want %+v", c.recipLimits, w)
	}
	dev, err := object.NewDevice(object.DeviceConfig{Instance: 1, Name: "d", VendorID: 1})
	if err != nil {
		t.Fatal(err)
	}
	for name, l := range map[string]RecipientLimits{
		"pending":   {PendingPerDestination: -1},
		"addresses": {Addresses: -1},
		"routes":    {Routes: -1},
		"recheck":   {RouteRecheck: -time.Second},
	} {
		s, err := New(dev, WithTransport(virtual.New(serverEP, nil, 8)), WithRecipientLimits(l))
		if err == nil {
			_ = s.Close()
		}
		if !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("%s: %v, want ErrInvalidConfig", name, err)
		}
	}
}

// RecipientLimits.PendingPerDestination sets how many of the server's
// confirmed requests wait for an answer from one destination.
func TestRecipientLimitsPendingPerDestination(t *testing.T) {
	for _, limit := range []int{1, 2, maxPendingPerDestination + 4} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			inBubble(t, func(t *testing.T) {
				h := newHarness(t, WithRecipientLimits(RecipientLimits{PendingPerDestination: limit}))
				for i := range limit + 3 {
					h.s.req.async(func() {
						err := h.s.req.invoke(h.s.ctx, clientEP, bacnet.Address{}, apdu.ServiceConfirmedEventNotification, []byte{0x09, byte(i)}, false)
						if errors.Is(err, errDestinationBusy) {
							h.s.report("event", "confirmed event notification failed", map[string]any{"error": err.Error()})
						}
					})
					idle()
				}
				sent := 0
				for {
					out := h.tr.Outbox()
					h.mu.Lock()
					more := h.seen < len(out)
					h.mu.Unlock()
					if !more {
						break
					}
					h.next()
					sent++
				}
				if sent != limit || len(h.diagnostics("event")) != 3 {
					t.Fatalf("%d requests sent, %d dropped; want %d and 3", sent, len(h.diagnostics("event")), limit)
				}
			})
		})
	}
}

// RecipientLimits.Addresses sets the size of the device address cache: one
// recipient more than it holds makes the entry heard from longest ago
// leave, and the diagnostic names the limit.
func TestRecipientLimitsAddresses(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		const limit, first = 4, 1001
		h := newHarness(t, WithRecipientLimits(RecipientLimits{Addresses: limit}))
		deviceRecipients(t, h, 10, 0, first, first+1, first+2, first+3, first+4)
		for i := range uint32(limit + 1) {
			h.iAmFrom(endpointN(int(first+i)), first+i)
			idle()
			time.Sleep(time.Millisecond) // the bubble's clock: every I-Am has its own time
		}
		ev := h.diagnostics("registry_eviction")
		if n := h.bindings(); n != limit || len(ev) != 1 || ev[0].Fields["device"] != uint32(first) || ev[0].Fields["limit"] != limit {
			t.Fatalf("%d addresses, evictions %+v; want %d and one for device %d", n, ev, limit, first)
		}
		if _, ok := h.bound(first + limit); !ok {
			t.Fatal("the recipient that announced itself last is not bound")
		}
	})
}

// RecipientLimits.Routes sets the size of the router table.
func TestRecipientLimitsRoutes(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		const limit = 3
		h := newHarness(t, WithRecipientLimits(RecipientLimits{Routes: limit}))
		router := endpointN(1)
		for n := uint16(1); n <= limit+1; n++ {
			h.fromNetwork(router, n)
			idle()
			time.Sleep(time.Millisecond)
		}
		if _, ok := h.s.routes.lookup(1); ok {
			t.Fatal("the route heard from longest ago is still there")
		}
		for n := uint16(2); n <= limit+1; n++ {
			if _, ok := h.s.routes.lookup(n); !ok {
				t.Fatalf("network %d not learnt", n)
			}
		}
		ev := h.diagnostics("registry_eviction")
		if len(ev) != 1 || ev[0].Fields["network"] != uint16(1) || ev[0].Fields["limit"] != limit {
			t.Fatalf("evictions %+v, want one for network 1 with limit %d", ev, limit)
		}
		h.silent(0)
	})
}

// RecipientLimits.RouteRecheck sets how soon another node that shows up
// for a network with an answered route makes the server ask again.
func TestRecipientLimitsRouteRecheck(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		const recheck = time.Minute
		h, ai, _ := remoteHarness(t, WithRecipientLimits(RecipientLimits{RouteRecheck: recheck}))
		r1, r2 := endpointN(1), endpointN(2)
		isWhoIsRouter := func(s sent) bool {
			return s.npdu.NetworkMessage && s.npdu.NetMsgType == npdu.NetMsgWhoIsRouterToNetwork
		}
		if err := ai.SetPresentValue(bacnet.RealValue(95)); err != nil {
			t.Fatal(err)
		}
		for range 2 { // the Who-Is-Router-To-Network and the broadcast notification
			h.next()
		}
		h.iAmRouter(r1, 5)
		idle()
		if ep, _ := h.s.routes.lookup(5); ep != r1 {
			t.Fatalf("network 5 via %v, want %v", ep, r1)
		}
		// Within the recheck time another node shows up: no question.
		time.Sleep(recheck - time.Second)
		h.fromNetwork(r2, 5)
		h.silent(0)
		// After it, one question.
		time.Sleep(time.Second)
		h.fromNetwork(r2, 5)
		if got := h.next(); !isWhoIsRouter(got) || !got.pkt.Broadcast {
			t.Fatalf("after the recheck time: %+v", got.npdu)
		}
		h.silent(0)
	})
}
