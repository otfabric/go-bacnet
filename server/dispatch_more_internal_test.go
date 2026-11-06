// SPDX-License-Identifier: MIT

package server

import (
	"fmt"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/server/object"
	"github.com/otfabric/go-bacnet/service"
)

func TestRejectAndAbortResults(t *testing.T) {
	// A decode limit hit is buffer-overflow (clause 18.8).
	r := rejectFor(fmt.Errorf("decoding: %w", bacnet.ErrLimitExceeded))
	rj, ok := r.err.(*rejectError)
	if !ok || rj.reason != bacnet.RejectReasonBufferOverflow || rj.Error() != "buffer-overflow" {
		t.Fatalf("limit exceeded: %+v", r.err)
	}
	reply, _, complex := fail(&abortError{reason: bacnet.AbortReasonOutOfResources}).encode(9, apdu.ServiceReadProperty)
	pdu, err := apdu.Parse(reply, bacnet.DefaultDecodeLimits())
	if err != nil || complex || pdu.Type != apdu.TypeAbort || !pdu.Abort.Server || pdu.Abort.InvokeID != 9 || pdu.Abort.Reason != bacnet.AbortReasonOutOfResources {
		t.Fatalf("abort %+v %v", pdu, err)
	}
	if e := (&abortError{reason: bacnet.AbortReasonOutOfResources}); e.Error() != "out-of-resources" {
		t.Fatalf("abort error %q", e.Error())
	}
	if e := (&createObjectError{}); e.Error() == "" {
		t.Fatal("empty CreateObject-Error text")
	}
}

func TestDecodeMaxSegments(t *testing.T) {
	// Clause 20.1.2.4: B'000' unspecified, B'111' more than 64.
	want := []int{0, 2, 4, 8, 16, 32, 64, 0}
	for code, n := range want {
		if got := decodeMaxSegments(uint8(code)); got != n {
			t.Errorf("code %d: %d, want %d", code, got, n)
		}
	}
}

func TestAcknowledgeAlarmWithoutEvents(t *testing.T) {
	h := newHarness(t)
	if err := h.dev.Add(newListObject(0)); err != nil {
		t.Fatal(err)
	}
	ackReq := func(inv uint8, obj bacnet.ObjectIdentifier) {
		h.confirmed(inv, apdu.ServiceAcknowledgeAlarm, mustEncode(t)(service.EncodeAcknowledgeAlarm(service.AcknowledgeAlarmRequest{
			ProcessIdentifier: 1, EventObject: obj, EventStateAcked: object.EventStateHighLimit,
			TimeStamp: service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 1}, AcknowledgmentSource: bacnet.CharacterString{Value: "op"},
			TimeOfAcknowledgment: service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 2},
		})))
	}
	// An object that is not an event reporter, and one without intrinsic
	// reporting configured.
	ackReq(1, listObjectID)
	h.expectError(bacnet.ErrorClassObject, bacnet.ErrorCodeNoAlarmConfigured)
	ackReq(2, av1)
	h.expectError(bacnet.ErrorClassObject, bacnet.ErrorCodeNoAlarmConfigured)
}

// TestGetEnrollmentSummaryFault checks that an object in FAULT is listed
// with the to-fault priority of its Notification Class.
func TestGetEnrollmentSummaryFault(t *testing.T) {
	h, ai := alarmHarness(t)
	h.s.dcc.set(service.EnableDisableDisableInitiation, time.Time{})
	const openLoop = 4 // BACnetReliability open-loop
	ai.SetReliability(openLoop)
	waitUntil(t, func() bool { info, _ := ai.EventInfo(); return info.EventState == object.EventStateFault })
	fault := uint32(1)
	h.confirmed(1, apdu.ServiceGetEnrollmentSummary, mustEncode(t)(service.EncodeGetEnrollmentSummary(service.GetEnrollmentSummaryRequest{EventStateFilter: &fault})))
	ack, err := service.DecodeGetEnrollmentSummaryACK(h.complexACK(), bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	// NC-7 priorities are to-offnormal 100, to-fault 50, to-normal 200.
	if len(ack.Entries) != 1 || ack.Entries[0].Object != ai.Identifier() || ack.Entries[0].EventState != object.EventStateFault || ack.Entries[0].Priority != 50 {
		t.Fatalf("entries %+v", ack.Entries)
	}
}
