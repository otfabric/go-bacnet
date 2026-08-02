// SPDX-License-Identifier: MIT

package service_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

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
	if len(got.Entries) != 1 || got.Entries[0].Priority != 100 || got.Entries[0].NotificationClass != 1 {
		t.Fatalf("%+v", got)
	}
	req := service.GetEnrollmentSummaryRequest{AcknowledgmentFilter: service.EnrollmentFilterAll}
	if _, err := service.EncodeGetEnrollmentSummary(req); err != nil {
		t.Fatal(err)
	}
}

func TestDecodeGetEnrollmentSummaryACKEmptyAndErrors(t *testing.T) {
	got, err := service.DecodeGetEnrollmentSummaryACK(nil, bacnet.DefaultDecodeLimits())
	if err != nil || len(got.Entries) != 0 {
		t.Fatalf("%v %+v", err, got)
	}
	raw, err := service.EncodeGetEnrollmentSummaryACK(service.GetEnrollmentSummaryACK{Entries: []service.EnrollmentSummaryEntry{{
		Object:    bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeEventEnrollment, Instance: 1},
		EventType: 1, EventState: 2, Priority: 3, NotificationClass: 4,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeGetEnrollmentSummaryACK(append(append([]byte{}, raw...), 0x21, 0x01), bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("trailing")
	}
	// wrong field types mid-sequence
	bad, err := bacnet.AppendApplicationValue(nil, bacnet.ObjectIDValue(bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeEventEnrollment, Instance: 1}))
	if err != nil {
		t.Fatal(err)
	}
	bad, err = bacnet.AppendApplicationValue(bad, bacnet.RealValue(1))
	if err != nil {
		t.Fatal(err)
	}
	bad, err = bacnet.AppendApplicationValue(bad, bacnet.EnumValue(1))
	if err != nil {
		t.Fatal(err)
	}
	bad, err = bacnet.AppendApplicationValue(bad, bacnet.UnsignedValue(1))
	if err != nil {
		t.Fatal(err)
	}
	bad, err = bacnet.AppendApplicationValue(bad, bacnet.UnsignedValue(1))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeGetEnrollmentSummaryACK(bad, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("type error")
	}
}

func TestEncodeCOVNotificationMultipleEmptyObjects(t *testing.T) {
	raw, err := service.EncodeCOVNotificationMultiple(service.COVNotificationMultiple{
		SubscriberProcessIdentifier: 1,
		InitiatingDevice:            bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		TimeRemaining:               0,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeCOVNotificationMultiple(raw, bacnet.DefaultDecodeLimits())
	if err != nil || len(got.Objects) != 0 {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestNotificationOutOfRangeRoundTripExtra(t *testing.T) {
	params := service.NotificationParameters{
		OutOfRange: &service.OutOfRangeParams{
			ExceedingValue: 90,
			StatusFlags:    bacnet.BitString{UnusedBits: 4, Bytes: []byte{0x80}},
			Deadband:       1,
			ExceededLimit:  80,
		},
	}
	els, err := service.EncodeNotificationParameters(params)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeNotificationParameters(els)
	if err != nil || got.OutOfRange == nil || got.OutOfRange.ExceedingValue != 90 {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestGetEnrollmentSummaryRequestFilters(t *testing.T) {
	es := uint32(1)
	et := uint32(5)
	pri := [2]uint32{1, 200}
	nc := uint32(1)
	raw, err := service.EncodeGetEnrollmentSummary(service.GetEnrollmentSummaryRequest{
		AcknowledgmentFilter:    service.EnrollmentFilterAll,
		EventStateFilter:        &es,
		EventTypeFilter:         &et,
		PriorityFilter:          &pri,
		NotificationClassFilter: &nc,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 10 {
		t.Fatalf("short payload %x", raw)
	}
	prioOnly := [2]uint32{1, 2}
	raw2, err := service.EncodeGetEnrollmentSummary(service.GetEnrollmentSummaryRequest{PriorityFilter: &prioOnly})
	if err != nil || len(raw2) < 4 {
		t.Fatal(err)
	}
}

func TestGetEnrollmentSummaryACKEncodeDecodeErrors(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	badObj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: bacnet.MaxObjectInstance + 1}
	if _, err := service.EncodeGetEnrollmentSummaryACK(service.GetEnrollmentSummaryACK{Entries: []service.EnrollmentSummaryEntry{{
		Object: badObj, EventType: 1, EventState: 1, Priority: 1, NotificationClass: 1,
	}}}); err == nil {
		t.Fatal("expected encode error")
	}
	one, err := bacnet.AppendApplicationValue(nil, bacnet.UnsignedValue(1))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeGetEnrollmentSummaryACK(one, limits); err == nil {
		t.Fatal("expected length error")
	}
	if _, err := service.DecodeGetEnrollmentSummaryACK([]byte{0xff}, limits); err == nil {
		t.Fatal("expected parse error")
	}
	badEnrollObj, err := bacnet.AppendApplicationValue(nil, bacnet.UnsignedValue(1))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []bacnet.ApplicationValue{
		bacnet.EnumValue(1), bacnet.EnumValue(1), bacnet.UnsignedValue(1), bacnet.UnsignedValue(1),
	} {
		badEnrollObj, err = bacnet.AppendApplicationValue(badEnrollObj, v)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.DecodeGetEnrollmentSummaryACK(badEnrollObj, limits); err == nil {
		t.Fatal("expected bad object")
	}
	badEnrollPrio, err := bacnet.AppendApplicationValue(nil, bacnet.ObjectIDValue(bacnet.ObjectIdentifier{
		Type: bacnet.ObjectTypeEventEnrollment, Instance: 1,
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []bacnet.ApplicationValue{
		bacnet.EnumValue(1), bacnet.EnumValue(1), bacnet.RealValue(1), bacnet.UnsignedValue(1),
	} {
		badEnrollPrio, err = bacnet.AppendApplicationValue(badEnrollPrio, v)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.DecodeGetEnrollmentSummaryACK(badEnrollPrio, limits); err == nil {
		t.Fatal("expected bad priority")
	}
	badEnrollNC, err := bacnet.AppendApplicationValue(nil, bacnet.ObjectIDValue(bacnet.ObjectIdentifier{
		Type: bacnet.ObjectTypeEventEnrollment, Instance: 1,
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []bacnet.ApplicationValue{
		bacnet.EnumValue(1), bacnet.EnumValue(1), bacnet.UnsignedValue(1), bacnet.RealValue(1),
	} {
		badEnrollNC, err = bacnet.AppendApplicationValue(badEnrollNC, v)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.DecodeGetEnrollmentSummaryACK(badEnrollNC, limits); err == nil {
		t.Fatal("expected bad notification class")
	}
}
