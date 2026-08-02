// SPDX-License-Identifier: MIT

package service_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

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
