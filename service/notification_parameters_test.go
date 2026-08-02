// SPDX-License-Identifier: MIT

package service

import (
	"testing"

	"github.com/otfabric/go-bacnet"
)

func TestNotificationParametersChangeOfStateRoundTrip(t *testing.T) {
	flags := bacnet.BitString{UnusedBits: 4, Bytes: []byte{0x00}}
	// PropertyStates state [8] BACnetEventState = offnormal (1) is common.
	params := NotificationParameters{
		ChangeOfState: &ChangeOfStateParams{
			NewState: PropertyStates{
				Choice: 8,
				Value:  bacnet.EnumValue(1),
			},
			StatusFlags: flags,
		},
	}
	note := EventNotification{
		ProcessIdentifier: 1,
		InitiatingDevice:  bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		EventObject:       bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
		TimeStamp:         TimeStamp{Choice: TimeStampSequence, Sequence: 1},
		NotificationClass: 1,
		Priority:          100,
		EventType:         1, // change-of-state
		NotifyType:        0,
		ToState:           1,
		Parameters:        &params,
	}
	raw, err := EncodeEventNotification(note)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeEventNotification(raw, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if got.Parameters == nil || got.Parameters.ChangeOfState == nil {
		t.Fatalf("typed params missing: %+v", got.Parameters)
	}
	cos := got.Parameters.ChangeOfState
	if cos.NewState.Choice != 8 || cos.NewState.Value.Enumerated != 1 {
		t.Fatalf("new-state %+v", cos.NewState)
	}
	if got.NotificationParams == nil {
		t.Fatal("opaque NotificationParams should remain populated")
	}
}

func TestNotificationParametersOutOfRangeRoundTrip(t *testing.T) {
	flags := bacnet.BitString{UnusedBits: 4, Bytes: []byte{0x40}}
	params := NotificationParameters{
		OutOfRange: &OutOfRangeParams{
			ExceedingValue: 95.5,
			StatusFlags:    flags,
			Deadband:       1.0,
			ExceededLimit:  90.0,
		},
	}
	els, err := EncodeNotificationParameters(params)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeNotificationParameters(els)
	if err != nil {
		t.Fatal(err)
	}
	if got.OutOfRange == nil || got.OutOfRange.ExceedingValue != 95.5 || got.OutOfRange.ExceededLimit != 90 {
		t.Fatalf("%+v", got.OutOfRange)
	}
}

func TestNotificationParametersChangeVariantsRoundTrip(t *testing.T) {
	flags := bacnet.BitString{UnusedBits: 4, Bytes: []byte{0x10}}
	bits := bacnet.BitString{UnusedBits: 0, Bytes: []byte{0xff}}
	cases := []NotificationParameters{
		{ChangeOfBitstring: &ChangeOfBitstringParams{
			ReferencedBitstring: bits, StatusFlags: flags,
		}},
		{ChangeOfValue: &ChangeOfValueParams{
			NewValue: bacnet.ApplicationValue{
				Kind: bacnet.ValueConstructed,
				Elements: []bacnet.Element{{
					Value: bacnet.RealValue(3.5),
				}},
			},
			StatusFlags: flags,
		}},
		{ChangeOfState: &ChangeOfStateParams{
			NewState: PropertyStates{Choice: 0, Value: bacnet.BoolValue(true)}, StatusFlags: flags,
		}},
		{ChangeOfState: &ChangeOfStateParams{
			NewState: PropertyStates{Choice: 1, Value: bacnet.EnumValue(2)}, StatusFlags: flags,
		}},
		{ChangeOfState: &ChangeOfStateParams{
			NewState: PropertyStates{Choice: 2, Value: bacnet.UnsignedValue(7)}, StatusFlags: flags,
		}},
		{ChangeOfState: &ChangeOfStateParams{
			NewState: PropertyStates{Choice: 9, Value: bacnet.RealValue(1.5)}, StatusFlags: flags,
		}},
	}
	for i, params := range cases {
		els, err := EncodeNotificationParameters(params)
		if err != nil {
			t.Fatalf("%d encode: %v", i, err)
		}
		got, err := DecodeNotificationParameters(els)
		if err != nil {
			t.Fatalf("%d decode: %v", i, err)
		}
		switch {
		case params.ChangeOfBitstring != nil && got.ChangeOfBitstring == nil:
			t.Fatalf("%d bitstring", i)
		case params.ChangeOfValue != nil && got.ChangeOfValue == nil:
			t.Fatalf("%d value", i)
		case params.ChangeOfState != nil && got.ChangeOfState == nil:
			t.Fatalf("%d state", i)
		}
	}
}

func TestNotificationParametersEmptyAndRawElements(t *testing.T) {
	if _, err := EncodeNotificationParameters(NotificationParameters{}); err == nil {
		t.Fatal("expected empty NotificationParameters")
	}
	rawNP, err := EncodeNotificationParameters(NotificationParameters{
		Choice: NotificationExtended,
		RawElements: []bacnet.Element{{
			Value: bacnet.UnsignedValue(1),
		}},
	})
	if err != nil || len(rawNP) == 0 {
		t.Fatalf("raw NotificationParameters: %v %#v", err, rawNP)
	}
}
