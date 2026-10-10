//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

// GetEnrollmentSummary against BACpypes3 is adapter-shim evidence: the stack
// decodes the request (GetEnrollmentSummaryRequest) and encodes the reply
// (GetEnrollmentSummaryACK); the adapter supplies the handler and a fixed list
// of enrollments, one per Analog Value (out-of-range, priority 100,
// notification class 1) and per Binary Value (change-of-state, priority 200,
// notification class 2) of the fixture, all in event state normal and
// acknowledged, to which it applies the request's filters. So every filter
// go-bacnet encodes is seen to arrive in a second implementation.
//
// BACpypes3 itself has no handler: without the adapter's it answers Reject
// unrecognized-service. bacnet-stack 1.6.x has no
// GetEnrollmentSummary codec at all, so BACnet4J stays the only stack that
// executes the service itself (TestBACnet4JGetEnrollmentSummary).
func TestBACpypes3GetEnrollmentSummary(t *testing.T) {
	image := bacpypes3Peer().image
	requireAdapterVersion(t, image, "0.10.0")
	dev := loadDeviceFixture(t)
	peer := startPeer(t, image, "bacpypes3")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()

	const (
		eventTypeChangeOfState = 1
		eventTypeOutOfRange    = 5
		eventStateNormal       = 0
		filterOffnormal        = 0 // eventStateFilter values differ from BACnetEventState
		filterNormal           = 2
	)
	av, _ := analogValueObject(dev)
	avEntry := service.EnrollmentSummaryEntry{Object: av, EventType: eventTypeOutOfRange,
		EventState: eventStateNormal, Priority: 100, NotificationClass: 1}
	bvEntry := service.EnrollmentSummaryEntry{
		Object:    bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeBinaryValue, Instance: 1},
		EventType: eventTypeChangeOfState, EventState: eventStateNormal, Priority: 200, NotificationClass: 2}
	u := func(v uint32) *uint32 { return &v }

	first, err := c.GetEnrollmentSummary(ctx, peer.target, service.GetEnrollmentSummaryRequest{})
	if err != nil {
		t.Fatalf("GetEnrollmentSummary: %v", err)
	}
	for _, tc := range []struct {
		name string
		req  service.GetEnrollmentSummaryRequest
		want []service.EnrollmentSummaryEntry
	}{
		{"all", service.GetEnrollmentSummaryRequest{}, []service.EnrollmentSummaryEntry{avEntry, bvEntry}},
		{"acked", service.GetEnrollmentSummaryRequest{AcknowledgmentFilter: service.AcknowledgmentFilterAcked},
			[]service.EnrollmentSummaryEntry{avEntry, bvEntry}},
		{"not acked", service.GetEnrollmentSummaryRequest{AcknowledgmentFilter: service.AcknowledgmentFilterNotAcked}, nil},
		{"event state normal", service.GetEnrollmentSummaryRequest{EventStateFilter: u(filterNormal)},
			[]service.EnrollmentSummaryEntry{avEntry, bvEntry}},
		{"event state offnormal", service.GetEnrollmentSummaryRequest{EventStateFilter: u(filterOffnormal)}, nil},
		{"event type change-of-state", service.GetEnrollmentSummaryRequest{EventTypeFilter: u(eventTypeChangeOfState)},
			[]service.EnrollmentSummaryEntry{bvEntry}},
		{"priority 50 to 150", service.GetEnrollmentSummaryRequest{PriorityFilter: &[2]uint32{50, 150}},
			[]service.EnrollmentSummaryEntry{avEntry}},
		{"notification class 2", service.GetEnrollmentSummaryRequest{NotificationClassFilter: u(2)},
			[]service.EnrollmentSummaryEntry{bvEntry}},
		{"every filter", service.GetEnrollmentSummaryRequest{
			AcknowledgmentFilter: service.AcknowledgmentFilterAcked, EventStateFilter: u(filterNormal),
			EventTypeFilter: u(eventTypeOutOfRange), PriorityFilter: &[2]uint32{100, 100}, NotificationClassFilter: u(1)},
			[]service.EnrollmentSummaryEntry{avEntry}},
	} {
		ack := first
		if tc.name != "all" {
			if ack, err = c.GetEnrollmentSummary(ctx, peer.target, tc.req); err != nil {
				t.Fatalf("%s: GetEnrollmentSummary: %v", tc.name, err)
			}
		}
		if len(ack.Entries) != len(tc.want) {
			t.Fatalf("%s: %d entries %+v, want %+v", tc.name, len(ack.Entries), ack.Entries, tc.want)
		}
		for i, e := range ack.Entries {
			if e != tc.want[i] {
				t.Fatalf("%s: entry %d = %+v, want %+v", tc.name, i, e, tc.want[i])
			}
		}
	}
	awaitOperation(t, peer, "get-enrollment-summary", 5*time.Second)
}
