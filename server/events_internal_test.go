// SPDX-License-Identifier: MIT

package server

import (
	"net/netip"
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

var allDay = bacnet.Time{Hour: 23, Minute: 59, Second: 59, Hundredths: 99}

func macOf(ep bip.Endpoint) []byte {
	ip := ep.Addr.Addr().As4()
	return []byte{ip[0], ip[1], ip[2], ip[3], byte(ep.Addr.Port() >> 8), byte(ep.Addr.Port())}
}

// alarmHarness serves AI-9 (high limit 80) reporting through NC-7 to: device
// 200 (unconfirmed), the local broadcast (confirmed: skipped, and
// unconfirmed) and the client (confirmed).
func alarmHarness(t *testing.T) (*harness, *object.Point) {
	h := newHarness(t)
	dev200 := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 200}
	nc, err := object.NewNotificationClass(object.NotificationClassConfig{
		Instance: 7, Name: "NC-7", Priority: &[3]uint8{100, 50, 200},
		Recipients: []object.Destination{
			{ValidDays: 0x7F, To: allDay, Device: &dev200, ProcessID: 1, Transitions: 7},
			{ValidDays: 0x7F, To: allDay, ProcessID: 2, Confirmed: true, Transitions: 7},
			{ValidDays: 0x7F, To: allDay, ProcessID: 3, Transitions: 7},
			{ValidDays: 0x7F, To: allDay, MAC: macOf(clientEP), ProcessID: 4, Confirmed: true, Transitions: 7},
		},
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
	return h, ai
}

// collect reads n packets and sorts them by kind.
func (h *harness) collect(n int) (unicast, broadcast, confirmed []sent, whoIs int) {
	for range n {
		got := h.next()
		switch {
		case got.pdu.Type == apdu.TypeConfirmedRequest:
			confirmed = append(confirmed, got)
		case got.pdu.UnconfirmedRequest.ServiceChoice == apdu.ServiceWhoIs:
			whoIs++
		case got.pkt.Broadcast:
			broadcast = append(broadcast, got)
		default:
			unicast = append(unicast, got)
		}
	}
	return
}

func TestEventRecipients(t *testing.T) {
	h, ai := alarmHarness(t)
	if err := ai.SetPresentValue(bacnet.RealValue(95)); err != nil {
		t.Fatal(err)
	}
	// Device 200 is not bound: a Who-Is instead of its notification.
	unicast, broadcast, confirmed, whoIs := h.collect(3)
	if whoIs != 1 || len(broadcast) != 1 || len(confirmed) != 1 || len(unicast) != 0 {
		t.Fatalf("who-is %d, broadcast %d, confirmed %d, unicast %d", whoIs, len(broadcast), len(confirmed), len(unicast))
	}
	note, err := service.DecodeEventNotification(broadcast[0].pdu.UnconfirmedRequest.Payload, bacnet.DefaultDecodeLimits())
	if err != nil || note.ProcessIdentifier != 3 || note.ToState != object.EventStateHighLimit || note.Priority != 100 {
		t.Fatalf("broadcast notification %+v %v", note, err)
	}
	if !h.diagnosed("event") {
		t.Fatal("skipped recipients not diagnosed")
	}
	// The client refuses the confirmed one.
	req := confirmed[0].pdu.ConfirmedRequest
	if req.ServiceChoice != apdu.ServiceConfirmedEventNotification || confirmed[0].pkt.Destination != clientEP {
		t.Fatalf("confirmed %+v to %v", req, confirmed[0].pkt.Destination)
	}
	errBody := mustEncode(t)(bacnet.EncodeBACnetError(nil, bacnet.ErrorClassServices, bacnet.ErrorCodeOther))
	h.inject(apdu.AppendError(nil, apdu.ErrorPDU{InvokeID: req.InvokeID, ServiceChoice: req.ServiceChoice, Payload: errBody}))

	// Device 200 announces itself (I-Am from 10.0.0.3); its own I-Am and
	// a malformed one are ignored.
	ep3 := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.3:47808"))
	iam := func(from bip.Endpoint, id bacnet.ObjectIdentifier) {
		p := mustEncode(t)(service.EncodeIAm(service.IAm{Device: id, MaxAPDULength: 1476, VendorID: 1}))
		h.injectFrom(from, bvlc.FunctionOriginalBroadcastNPDU, npdu.NPDU{Version: npdu.Version1, APDU: apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{ServiceChoice: apdu.ServiceIAm, Payload: p})})
	}
	iam(ep3, bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 200})
	iam(clientEP, h.dev.Identifier())
	h.unconfirmed(apdu.ServiceIAm, []byte{0xFF})
	waitUntil(t, func() bool {
		h.s.events.bmu.Lock()
		defer h.s.events.bmu.Unlock()
		return len(h.s.events.bindings) == 1
	})

	if err := ai.SetPresentValue(bacnet.RealValue(50)); err != nil {
		t.Fatal(err)
	}
	unicast, broadcast, confirmed, whoIs = h.collect(3)
	if whoIs != 0 || len(unicast) != 1 || unicast[0].pkt.Destination != ep3 || len(broadcast) != 1 || len(confirmed) != 1 {
		t.Fatalf("who-is %d, unicast %d, broadcast %d, confirmed %d", whoIs, len(unicast), len(broadcast), len(confirmed))
	}
	req = confirmed[0].pdu.ConfirmedRequest
	h.inject(apdu.AppendSimpleACK(nil, apdu.SimpleACK{InvokeID: req.InvokeID, ServiceChoice: req.ServiceChoice}))
	h.quiet(50 * time.Millisecond)

	// While initiation is disabled no notification is sent (clause 16.1).
	h.s.dcc.set(service.EnableDisableDisableInitiation, time.Time{})
	if err := ai.SetPresentValue(bacnet.RealValue(10)); err != nil {
		t.Fatal(err)
	}
	h.quiet(50 * time.Millisecond)
}

func TestEventWithoutNotificationClass(t *testing.T) {
	h := newHarness(t)
	ai, err := object.NewAnalogInput(object.PointConfig{Instance: 3, Name: "AI-3", Value: 50, Events: &object.EventConfig{
		NotificationClass: 99, EventEnable: 7, HighLimit: 80, LimitEnable: object.LimitEnableHigh,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.dev.Add(ai); err != nil {
		t.Fatal(err)
	}
	if err := ai.SetPresentValue(bacnet.RealValue(90)); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, func() bool { return h.diagnosed("event") })
	h.quiet(20 * time.Millisecond)
	if info, ok := ai.EventInfo(); !ok || info.EventState != object.EventStateHighLimit {
		t.Fatalf("event state not recorded: %+v", info)
	}
}

func TestGetEnrollmentSummaryFilters(t *testing.T) {
	h, ai := alarmHarness(t)
	h.s.dcc.set(service.EnableDisableDisableInitiation, time.Time{})
	if err := ai.SetPresentValue(bacnet.RealValue(95)); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, func() bool { info, _ := ai.EventInfo(); return info.EventState == object.EventStateHighLimit })
	u := func(v uint32) *uint32 { return &v }
	dev200 := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 200}
	dev201 := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 201}
	inv := uint8(0)
	count := func(r service.GetEnrollmentSummaryRequest) int {
		t.Helper()
		inv++
		h.confirmed(inv, apdu.ServiceGetEnrollmentSummary, mustEncode(t)(service.EncodeGetEnrollmentSummary(r)))
		got := h.expectType(apdu.TypeComplexACK)
		ack, err := service.DecodeGetEnrollmentSummaryACK(got.pdu.ComplexACK.Payload, bacnet.DefaultDecodeLimits())
		if err != nil {
			t.Fatal(err)
		}
		return len(ack.Entries)
	}
	// NC-7 requires no acknowledgment, so the transition counts as acked.
	for _, tc := range []struct {
		name string
		req  service.GetEnrollmentSummaryRequest
		want int
	}{
		{"all", service.GetEnrollmentSummaryRequest{}, 1},
		{"acked", service.GetEnrollmentSummaryRequest{AcknowledgmentFilter: service.AcknowledgmentFilterAcked}, 1},
		{"not acked", service.GetEnrollmentSummaryRequest{AcknowledgmentFilter: service.AcknowledgmentFilterNotAcked}, 0},
		{"offnormal", service.GetEnrollmentSummaryRequest{EventStateFilter: u(0)}, 1},
		{"fault", service.GetEnrollmentSummaryRequest{EventStateFilter: u(1)}, 0},
		{"normal", service.GetEnrollmentSummaryRequest{EventStateFilter: u(2)}, 0},
		{"state all", service.GetEnrollmentSummaryRequest{EventStateFilter: u(3)}, 1},
		{"active", service.GetEnrollmentSummaryRequest{EventStateFilter: u(4)}, 1},
		{"event type", service.GetEnrollmentSummaryRequest{EventTypeFilter: u(object.EventTypeChangeOfState)}, 0},
		{"priority in", service.GetEnrollmentSummaryRequest{PriorityFilter: &[2]uint32{90, 110}}, 1},
		{"priority out", service.GetEnrollmentSummaryRequest{PriorityFilter: &[2]uint32{0, 10}}, 0},
		{"class", service.GetEnrollmentSummaryRequest{NotificationClassFilter: u(8)}, 0},
		{"enrolled device", service.GetEnrollmentSummaryRequest{EnrollmentFilter: &service.RecipientProcess{Recipient: service.AuditDevice(dev200), ProcessIdentifier: 1}}, 1},
		{"enrolled device other process", service.GetEnrollmentSummaryRequest{EnrollmentFilter: &service.RecipientProcess{Recipient: service.AuditDevice(dev200), ProcessIdentifier: 2}}, 0},
		{"enrolled other device", service.GetEnrollmentSummaryRequest{EnrollmentFilter: &service.RecipientProcess{Recipient: service.AuditDevice(dev201), ProcessIdentifier: 1}}, 0},
		{"enrolled address", service.GetEnrollmentSummaryRequest{EnrollmentFilter: &service.RecipientProcess{Recipient: service.AuditRecipient{Address: &service.AuditAddress{MAC: macOf(clientEP)}}, ProcessIdentifier: 4}}, 1},
		{"enrolled broadcast", service.GetEnrollmentSummaryRequest{EnrollmentFilter: &service.RecipientProcess{Recipient: service.AuditRecipient{Address: &service.AuditAddress{}}, ProcessIdentifier: 3}}, 1},
		{"enrolled other network", service.GetEnrollmentSummaryRequest{EnrollmentFilter: &service.RecipientProcess{Recipient: service.AuditRecipient{Address: &service.AuditAddress{Network: 9, MAC: macOf(clientEP)}}, ProcessIdentifier: 4}}, 0},
	} {
		if got := count(tc.req); got != tc.want {
			t.Errorf("%s: %d entries, want %d", tc.name, got, tc.want)
		}
	}
	h.confirmed(99, apdu.ServiceGetEnrollmentSummary, []byte{0xFF})
	h.expectType(apdu.TypeReject)
}

// An object whose Notification Class does not exist has no recipients, so
// the enrollment filter leaves it out (clause 13.11.1.2).
func TestGetEnrollmentSummaryEnrollmentFilterWithoutClass(t *testing.T) {
	h := newHarness(t)
	ai, err := object.NewAnalogInput(object.PointConfig{Instance: 3, Name: "AI-3", Value: 90, Events: &object.EventConfig{
		NotificationClass: 99, EventEnable: 7, HighLimit: 80, LimitEnable: object.LimitEnableHigh,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.dev.Add(ai); err != nil {
		t.Fatal(err)
	}
	dev := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 200}
	for i, tc := range []struct {
		req  service.GetEnrollmentSummaryRequest
		want int
	}{
		{service.GetEnrollmentSummaryRequest{}, 1},
		{service.GetEnrollmentSummaryRequest{EnrollmentFilter: &service.RecipientProcess{Recipient: service.AuditDevice(dev)}}, 0},
	} {
		h.confirmed(uint8(i+1), apdu.ServiceGetEnrollmentSummary, mustEncode(t)(service.EncodeGetEnrollmentSummary(tc.req)))
		got := h.expectType(apdu.TypeComplexACK)
		ack, err := service.DecodeGetEnrollmentSummaryACK(got.pdu.ComplexACK.Payload, bacnet.DefaultDecodeLimits())
		if err != nil || len(ack.Entries) != tc.want {
			t.Fatalf("case %d: %+v %v", i, ack, err)
		}
	}
}

// remoteHarness serves AI-9 reporting through NC-8 to a station on network
// 5, once confirmed and once unconfirmed, and to the local broadcast,
// confirmed (never sent: clause 5.4).
func remoteHarness(t *testing.T) (*harness, *object.Point, []byte) {
	h := newHarness(t)
	station := []byte{10, 5, 0, 7, 0xBA, 0xC0}
	nc, err := object.NewNotificationClass(object.NotificationClassConfig{
		Instance: 8, Name: "NC-8",
		Recipients: []object.Destination{
			{ValidDays: 0x7F, To: allDay, Network: 5, MAC: station, ProcessID: 5, Confirmed: true, Transitions: 7},
			{ValidDays: 0x7F, To: allDay, Network: 5, MAC: station, ProcessID: 6, Transitions: 7},
			{ValidDays: 0x7F, To: allDay, ProcessID: 7, Confirmed: true, Transitions: 7},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ai, err := object.NewAnalogInput(object.PointConfig{Instance: 9, Name: "AI-9", Value: 50, Events: &object.EventConfig{
		NotificationClass: 8, EventEnable: 7, NotifyType: object.NotifyAlarm, HighLimit: 80, Deadband: 2,
		LimitEnable: object.LimitEnableHigh,
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range []object.Object{nc, ai} {
		if err := h.dev.Add(o); err != nil {
			t.Fatal(err)
		}
	}
	return h, ai, station
}

func TestEventRemoteStationRecipients(t *testing.T) {
	h, ai, station := remoteHarness(t)
	if p := nilPriorities(t, h); p != [3]uint8{255, 255, 255} {
		t.Fatalf("default priorities %v", p)
	}
	if err := ai.SetPresentValue(bacnet.RealValue(95)); err != nil {
		t.Fatal(err)
	}
	// No router to network 5 is known: Who-Is-Router-To-Network for each
	// remote recipient, the confirmed one is skipped, the unconfirmed one
	// goes out as a local broadcast with DNET 5.
	var whoIsRouter, broadcast int
	for range 3 {
		got := h.next()
		switch {
		case got.npdu.NetworkMessage:
			if got.npdu.NetMsgType != npdu.NetMsgWhoIsRouterToNetwork || !got.pkt.Broadcast ||
				len(got.npdu.NetMsgData) != 2 || got.npdu.NetMsgData[0] != 0 || got.npdu.NetMsgData[1] != 5 {
				t.Fatalf("network message %+v", got.npdu)
			}
			whoIsRouter++
		case got.pkt.Broadcast && got.pdu.Type == apdu.TypeUnconfirmedRequest:
			if got.npdu.Destination.Scope() != bacnet.AddressRemoteStation || got.npdu.Destination.Network() != 5 {
				t.Fatalf("broadcast to %v", got.npdu.Destination)
			}
			broadcast++
		default:
			t.Fatalf("unexpected %+v", got)
		}
	}
	if whoIsRouter != 2 || broadcast != 1 {
		t.Fatalf("who-is-router %d, broadcast %d", whoIsRouter, broadcast)
	}
	h.quiet(30 * time.Millisecond)
	if !h.diagnosed("event") {
		t.Fatal("skipped recipients not diagnosed")
	}

	// A router answers I-Am-Router-To-Network for networks 4 and 5; a
	// malformed network message is dropped and teaches nothing.
	router := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.9:47808"))
	h.injectFrom(router, bvlc.FunctionOriginalBroadcastNPDU, npdu.NPDU{Version: npdu.Version1, NetworkMessage: true,
		NetMsgType: npdu.NetMsgIAmRouterToNetwork, NetMsgData: []byte{0, 4, 0, 5}})
	h.injectRaw(mustEncode(t)(bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionOriginalBroadcastNPDU, Payload: []byte{0x01, 0x80, 0x01, 0x00}})))
	waitUntil(t, func() bool { _, ok := h.s.routes.lookup(5); return ok })
	if _, ok := h.s.routes.lookup(4); !ok {
		t.Fatal("network 4 not learnt")
	}

	if err := ai.SetPresentValue(bacnet.RealValue(50)); err != nil {
		t.Fatal(err)
	}
	var confirmed, unicast sent
	for range 2 {
		got := h.next()
		if got.pkt.Broadcast || got.pkt.Destination != router || got.npdu.Destination.Network() != 5 ||
			string(got.npdu.Destination.MAC().Bytes()) != string(station) {
			t.Fatalf("sent %+v to %v", got.npdu, got.pkt.Destination)
		}
		if got.pdu.Type == apdu.TypeConfirmedRequest {
			confirmed = got
		} else {
			unicast = got
		}
	}
	if confirmed.pdu.ConfirmedRequest.ServiceChoice != apdu.ServiceConfirmedEventNotification ||
		unicast.pdu.UnconfirmedRequest.ServiceChoice != apdu.ServiceUnconfirmedEventNotification {
		t.Fatalf("confirmed %+v, unconfirmed %+v", confirmed.pdu, unicast.pdu)
	}
	// The station answers through the router.
	req := confirmed.pdu.ConfirmedRequest
	h.injectFrom(router, bvlc.FunctionOriginalUnicastNPDU, npdu.NPDU{Version: npdu.Version1,
		Source: bacnet.RemoteStation(5, bacnet.MustMAC(station)),
		APDU:   apdu.AppendSimpleACK(nil, apdu.SimpleACK{InvokeID: req.InvokeID, ServiceChoice: req.ServiceChoice})})
	h.quiet(50 * time.Millisecond)
}

func nilPriorities(t *testing.T, h *harness) [3]uint8 {
	t.Helper()
	nc, ok := h.s.events.notificationClass(8)
	if !ok {
		t.Fatal("NC-8 missing")
	}
	return nc.Priorities()
}

// A source network in an NPDU teaches the route through the B/IP node that
// sent it: the sender, or the Forwarded-NPDU origin.
func TestRouteLearning(t *testing.T) {
	h := newHarness(t)
	router := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.9:47808"))
	who := apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{ServiceChoice: apdu.ServiceWhoIs})
	h.injectFrom(router, bvlc.FunctionOriginalBroadcastNPDU, npdu.NPDU{Version: npdu.Version1,
		Source: bacnet.RemoteStation(6, bacnet.MustMAC([]byte{1})), APDU: who})
	waitUntil(t, func() bool { ep, ok := h.s.routes.lookup(6); return ok && ep == router })

	origin := netip.MustParseAddrPort("192.168.7.9:47808")
	raw := mustEncode(t)(npdu.Append(nil, npdu.NPDU{Version: npdu.Version1, Source: bacnet.RemoteStation(7, bacnet.MustMAC([]byte{2})), APDU: who}))
	h.injectRaw(mustEncode(t)(bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionForwardedNPDU, OriginIP: origin.Addr().As4(), OriginPort: origin.Port(), Payload: raw})))
	waitUntil(t, func() bool { ep, ok := h.s.routes.lookup(7); return ok && ep == bip.NewEndpoint(origin) })
}

func TestRouteTableBounds(t *testing.T) {
	rt := newRouteTable()
	router := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.9:47808"))
	other := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.10:47808"))
	rt.learn(0, router)
	rt.learn(0xFFFF, router)
	rt.learn(3, bip.Endpoint{})
	if len(rt.routes) != 0 {
		t.Fatalf("invalid routes learnt: %v", rt.routes)
	}
	for n := range uint16(maxRoutes) {
		rt.learn(n+1, router)
	}
	rt.learn(maxRoutes+1, router)
	if _, ok := rt.lookup(maxRoutes + 1); ok || len(rt.routes) != maxRoutes {
		t.Fatalf("table not bounded: %d", len(rt.routes))
	}
	// A known network is updated when the table is full.
	rt.learn(1, other)
	if ep, _ := rt.lookup(1); ep != other {
		t.Fatalf("route 1 via %v", ep)
	}
	// A malformed network list teaches nothing.
	h := newHarness(t)
	h.s.learnRoutes(npdu.NPDU{NetworkMessage: true, NetMsgType: npdu.NetMsgIAmRouterToNetwork, NetMsgData: []byte{0}}, router)
	if len(h.s.routes.routes) != 0 {
		t.Fatal("malformed I-Am-Router-To-Network learnt")
	}
}

// A Multi-state Value reports CHANGE_OF_STATE on Alarm_Values with the new
// state as BACnetPropertyStates unsigned-value [11].
func TestMultiStateEventNotification(t *testing.T) {
	h := newHarness(t)
	nc, err := object.NewNotificationClass(object.NotificationClassConfig{
		Instance: 2, Name: "NC-2", Priority: &[3]uint8{0, 0, 0},
		Recipients: []object.Destination{{ValidDays: 0x7F, To: allDay, MAC: macOf(clientEP), ProcessID: 1, Transitions: 7}},
	})
	if err != nil {
		t.Fatal(err)
	}
	msv, err := object.NewMultiStateValue(object.PointConfig{Instance: 4, Name: "MSV-4", NumberOfStates: 3, Events: &object.EventConfig{
		NotificationClass: 2, EventEnable: 7, AlarmValues: []uint32{3}, FaultValues: []uint32{2},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range []object.Object{nc, msv} {
		if err := h.dev.Add(o); err != nil {
			t.Fatal(err)
		}
	}
	note := func() service.EventNotification {
		t.Helper()
		got := h.next()
		n, err := service.DecodeEventNotification(got.pdu.UnconfirmedRequest.Payload, bacnet.DefaultDecodeLimits())
		if err != nil || got.pkt.Destination != clientEP {
			t.Fatalf("%+v %v", got, err)
		}
		return n
	}
	if err := msv.SetPresentValue(bacnet.UnsignedValue(3)); err != nil {
		t.Fatal(err)
	}
	n := note()
	if n.EventType != object.EventTypeChangeOfState || n.ToState != object.EventStateOffnormal || n.Priority != 0 ||
		n.Parameters == nil || n.Parameters.ChangeOfState == nil || n.Parameters.ChangeOfState.NewState.Choice != 11 ||
		n.Parameters.ChangeOfState.NewState.Value.Enumerated != 3 { // the decoder keeps a context integer as Enumerated
		t.Fatalf("offnormal %+v", n)
	}
	if err := msv.SetPresentValue(bacnet.UnsignedValue(2)); err != nil {
		t.Fatal(err)
	}
	if n := note(); n.EventType != object.EventTypeChangeOfReliability || n.ToState != object.EventStateFault {
		t.Fatalf("fault %+v", n)
	}
}
