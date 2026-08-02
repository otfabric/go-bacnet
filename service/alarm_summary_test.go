// SPDX-License-Identifier: MIT

package service_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestDecodeGetAlarmSummaryACKEmptyAndTrailing(t *testing.T) {
	got, err := service.DecodeGetAlarmSummaryACK(nil, bacnet.DefaultDecodeLimits())
	if err != nil || len(got.Entries) != 0 {
		t.Fatalf("%v %+v", err, got)
	}
	raw, err := service.EncodeGetAlarmSummaryACK(service.GetAlarmSummaryACK{Entries: []service.AlarmSummaryEntry{{
		Object:           bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
		AlarmState:       1,
		AckedTransitions: bacnet.BitString{UnusedBits: 5, Bytes: []byte{0x00}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeGetAlarmSummaryACK(append(append([]byte{}, raw...), 0x00), bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected trailing")
	}
	// wrong middle type
	bad, err := bacnet.AppendApplicationValue(nil, bacnet.ObjectIDValue(bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}))
	if err != nil {
		t.Fatal(err)
	}
	bad, err = bacnet.AppendApplicationValue(bad, bacnet.RealValue(1))
	if err != nil {
		t.Fatal(err)
	}
	bad, err = bacnet.AppendApplicationValue(bad, bacnet.ApplicationValue{Kind: bacnet.ValueBitString, BitString: bacnet.BitString{UnusedBits: 5, Bytes: []byte{0}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeGetAlarmSummaryACK(bad, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected type error")
	}
}

func TestGetAlarmSummaryACKRoundTrip(t *testing.T) {
	ack := service.GetAlarmSummaryACK{Entries: []service.AlarmSummaryEntry{{
		Object:           bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
		AlarmState:       3,
		AckedTransitions: bacnet.BitString{UnusedBits: 5, Bytes: []byte{0xa0}},
	}}}
	raw, err := service.EncodeGetAlarmSummaryACK(ack)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeGetAlarmSummaryACK(raw, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 1 || got.Entries[0].Object.Instance != 1 || got.Entries[0].AlarmState != 3 {
		t.Fatalf("%+v", got)
	}
}

func TestGetEnrollmentSummaryRoundTrip(t *testing.T) {
	ack := service.GetEnrollmentSummaryACK{Entries: []service.EnrollmentSummaryEntry{{
		Object:            bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeEventEnrollment, Instance: 1},
		EventType:         5,
		EventState:        2,
		Priority:          100,
		NotificationClass: 1,
	}}}
	raw, err := service.EncodeGetEnrollmentSummaryACK(ack)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeGetEnrollmentSummaryACK(raw, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 1 || got.Entries[0].Priority != 100 {
		t.Fatalf("%+v", got)
	}
	req := service.GetEnrollmentSummaryRequest{AcknowledgmentFilter: service.EnrollmentFilterAll}
	if _, err := service.EncodeGetEnrollmentSummary(req); err != nil {
		t.Fatal(err)
	}
}
