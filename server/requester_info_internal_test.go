// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"net/netip"
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

func TestRequesterOf(t *testing.T) {
	inBubble(t, testRequesterOf)
}

func testRequesterOf(t *testing.T) {
	if _, ok := RequesterOf(context.Background()); ok {
		t.Fatal("a context of no request has a requester")
	}
	var mu sync.Mutex
	var got []Requester
	var oks []bool
	record := func(ctx context.Context) {
		r, ok := RequesterOf(ctx)
		mu.Lock()
		got, oks = append(got, r), append(oks, ok)
		mu.Unlock()
	}
	routerEP := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.9:47808"))
	groups := make(chan struct{}, 1)
	h := newHarness(t, WithHooks(Hooks{
		BeforeWrite: func(ctx context.Context, _ bacnet.ObjectIdentifier, _ object.Write) error { record(ctx); return nil },
		WriteGroup:  func(ctx context.Context, _ service.WriteGroup) { record(ctx); groups <- struct{}{} },
	}))
	enc := mustEncode(t)
	write := enc(service.EncodeWriteProperty(service.WritePropertyRequest{Object: av1, Property: pvRef, Value: bacnet.RealValue(2)}))

	// A local station: the datagram's sender, no NPDU source.
	h.confirmed(1, apdu.ServiceWriteProperty, write)
	h.expectType(apdu.TypeSimpleACK)
	// A station on network 5 behind the router at routerEP.
	routed := apdu.AppendConfirmedRequest(nil, apdu.ConfirmedRequest{MaxSegments: 0, MaxAPDU: 5, InvokeID: 2, ServiceChoice: apdu.ServiceWriteProperty, Payload: write})
	h.injectFrom(routerEP, bvlc.FunctionOriginalUnicastNPDU, npdu.NPDU{Version: npdu.Version1, ExpectingReply: true,
		Source: bacnet.RemoteStation(5, bacnet.MustMAC([]byte{7})), APDU: routed})
	h.expectType(apdu.TypeSimpleACK)
	// An unconfirmed request reaches its hook with its requester too.
	h.unconfirmed(apdu.ServiceWriteGroup, enc(service.EncodeWriteGroup(service.WriteGroup{GroupNumber: 1, WritePriority: 8,
		ChangeList: []bacnet.Element{{Context: true, TagNumber: 0, Value: bacnet.UnsignedValue(1)}}})))
	<-groups

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 3 || !oks[0] || !oks[1] || !oks[2] {
		t.Fatalf("requesters %+v %v", got, oks)
	}
	if !got[0].Endpoint.Equal(clientEP) || !got[0].Via.Equal(clientEP) || got[0].Source != (bacnet.Address{}) {
		t.Fatalf("local requester %+v", got[0])
	}
	if !got[1].Endpoint.Equal(routerEP) || !got[1].Via.Equal(routerEP) || !got[1].Source.Equal(bacnet.RemoteStation(5, bacnet.MustMAC([]byte{7}))) {
		t.Fatalf("routed requester %+v", got[1])
	}
	if !got[2].Endpoint.Equal(clientEP) {
		t.Fatalf("unconfirmed requester %+v", got[2])
	}
}

// A broadcast a BBMD forwarded names its origin as Endpoint and the BBMD as
// Via; a segmented request carries its requester to the hook once
// reassembled; a Schedule's write from the housekeeping goroutine has none.
func TestRequesterOfForwardedSegmentedAndHousekeeping(t *testing.T) {
	inBubble(t, testRequesterOfForwardedSegmentedAndHousekeeping)
}

func testRequesterOfForwardedSegmentedAndHousekeeping(t *testing.T) {
	type seen struct {
		r  Requester
		ok bool
	}
	writes := make(chan seen, 4)
	groups := make(chan seen, 1)
	h := newHarness(t, WithHooks(Hooks{
		WriteGroup: func(ctx context.Context, _ service.WriteGroup) {
			r, ok := RequesterOf(ctx)
			groups <- seen{r, ok}
		},
		BeforeWrite: func(ctx context.Context, _ bacnet.ObjectIdentifier, _ object.Write) error {
			r, ok := RequesterOf(ctx)
			writes <- seen{r, ok}
			return nil
		},
	}))
	enc := mustEncode(t)
	write := enc(service.EncodeWriteProperty(service.WritePropertyRequest{Object: av1, Property: pvRef, Value: bacnet.RealValue(2)}))

	// A broadcast a BBMD at clientEP forwarded (Forwarded-NPDU carries
	// broadcasts only, so an unconfirmed request) from origin.
	origin := netip.MustParseAddrPort("192.168.7.9:47808")
	wg := apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{ServiceChoice: apdu.ServiceWriteGroup,
		Payload: enc(service.EncodeWriteGroup(service.WriteGroup{GroupNumber: 1, WritePriority: 8,
			ChangeList: []bacnet.Element{{Context: true, TagNumber: 0, Value: bacnet.UnsignedValue(1)}}}))})
	raw := enc(npdu.Append(nil, npdu.NPDU{Version: npdu.Version1, Destination: bacnet.GlobalBroadcast(), APDU: wg}))
	h.injectRaw(enc(bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionForwardedNPDU, OriginIP: origin.Addr().As4(), OriginPort: origin.Port(), Payload: raw})))
	if got := <-groups; !got.ok || !got.r.Endpoint.Equal(bip.NewEndpoint(origin)) || !got.r.Via.Equal(clientEP) || got.r.Source != (bacnet.Address{}) {
		t.Fatalf("forwarded requester %+v", got)
	}

	// A segmented WriteProperty in two segments.
	seg := func(seq uint8, part []byte, more bool) {
		h.request(apdu.ConfirmedRequest{SegmentedMessage: true, MoreFollows: more, MaxSegments: 7, MaxAPDU: 5,
			InvokeID: 2, SequenceNumber: seq, ProposedWindowSize: 1, ServiceChoice: apdu.ServiceWriteProperty, Payload: part})
	}
	half := len(write) / 2
	seg(0, write[:half], true)
	h.expectType(apdu.TypeSegmentACK)
	seg(1, write[half:], false)
	h.expectType(apdu.TypeSegmentACK)
	h.expectType(apdu.TypeSimpleACK)
	if got := <-writes; !got.ok || !got.r.Endpoint.Equal(clientEP) || !got.r.Via.Equal(clientEP) {
		t.Fatalf("segmented requester %+v", got)
	}

	// The housekeeping goroutine's Schedule write to av1.
	sch, err := object.NewSchedule(object.ScheduleConfig{Instance: 1, Name: "sch", Default: bacnet.RealValue(5),
		References: []object.DeviceObjectPropertyReference{{Object: av1, Property: bacnet.PropertyPresentValue}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.dev.Add(sch); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond)
	idle()
	select {
	case got := <-writes:
		if got.ok {
			t.Fatalf("a housekeeping write has a requester: %+v", got.r)
		}
	default:
		t.Fatal("the Schedule did not write its reference")
	}
}

// An unconfirmed request that finds every worker busy and no queue is
// dropped with a queue_drop diagnostic; its hook does not run.
func TestUnconfirmedDroppedWhenWorkersBusy(t *testing.T) {
	inBubble(t, testUnconfirmedDroppedWhenWorkersBusy)
}

func testUnconfirmedDroppedWhenWorkersBusy(t *testing.T) {
	release := make(chan struct{})
	var mu sync.Mutex
	var groups []uint32
	h := newHarness(t, WithWorkers(1, 0), WithHooks(Hooks{WriteGroup: func(_ context.Context, w service.WriteGroup) {
		mu.Lock()
		groups = append(groups, w.GroupNumber)
		mu.Unlock()
		<-release
	}}))
	enc := mustEncode(t)
	group := func(n uint32) []byte {
		return enc(service.EncodeWriteGroup(service.WriteGroup{GroupNumber: n, WritePriority: 8,
			ChangeList: []bacnet.Element{{Context: true, TagNumber: 0, Value: bacnet.UnsignedValue(1)}}}))
	}
	idle() // the worker waits for work
	h.unconfirmed(apdu.ServiceWriteGroup, group(1))
	idle() // the worker is in the hook
	h.unconfirmed(apdu.ServiceWriteGroup, group(2))
	idle()
	drops := h.diagnostics("queue_drop")
	if len(drops) != 1 || !strings.Contains(drops[0].Message, "unconfirmed") {
		t.Fatalf("queue_drop diagnostics %+v", drops)
	}
	close(release)
	idle()
	mu.Lock()
	defer mu.Unlock()
	if len(groups) != 1 || groups[0] != 1 {
		t.Fatalf("WriteGroup hook ran for groups %v, want only 1", groups)
	}
}
