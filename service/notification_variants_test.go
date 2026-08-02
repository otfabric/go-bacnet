// SPDX-License-Identifier: MIT

package service

import (
	"testing"

	"github.com/otfabric/go-bacnet"
)

func TestNotificationParametersExtraVariantsRoundTrip(t *testing.T) {
	flags := bacnet.BitString{UnusedBits: 4, Bytes: []byte{0x10}}
	cases := []NotificationParameters{
		{CommandFailure: &CommandFailureParams{
			CommandValue:  bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{{Value: bacnet.UnsignedValue(1)}}},
			StatusFlags:   flags,
			FeedbackValue: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{{Value: bacnet.UnsignedValue(2)}}},
		}},
		{FloatingLimit: &FloatingLimitParams{
			ReferenceValue: 10, StatusFlags: flags, SetPointValue: 20, ErrorLimit: 1,
		}},
		{UnsignedRange: &UnsignedRangeParams{
			ExceedingValue: 99, StatusFlags: flags, ExceededLimit: 80,
		}},
		{BufferReady: &BufferReadyParams{
			BufferProperty: []bacnet.Element{{Value: bacnet.UnsignedValue(1)}},
			PreviousCount:  1, CurrentCount: 2,
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
		case params.CommandFailure != nil && got.CommandFailure == nil:
			t.Fatalf("%d command-failure missing", i)
		case params.FloatingLimit != nil && (got.FloatingLimit == nil || got.FloatingLimit.ReferenceValue != 10):
			t.Fatalf("%d floating-limit %+v", i, got)
		case params.UnsignedRange != nil && (got.UnsignedRange == nil || got.UnsignedRange.ExceedingValue != 99):
			t.Fatalf("%d unsigned-range %+v", i, got)
		case params.BufferReady != nil && (got.BufferReady == nil || got.BufferReady.CurrentCount != 2):
			t.Fatalf("%d buffer-ready %+v", i, got)
		}
	}
}

func TestNotificationParametersExtraVariantErrors(t *testing.T) {
	if _, err := DecodeNotificationParameters([]bacnet.Element{{
		Context: true, TagNumber: uint8(NotificationCommandFailure),
		Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed},
	}}); err == nil {
		t.Fatal("command-failure")
	}
	if _, err := DecodeNotificationParameters([]bacnet.Element{{
		Context: true, TagNumber: uint8(NotificationFloatingLimit),
		Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed},
	}}); err == nil {
		t.Fatal("floating-limit")
	}
	if _, err := DecodeNotificationParameters([]bacnet.Element{{
		Context: true, TagNumber: uint8(NotificationUnsignedRange),
		Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed},
	}}); err == nil {
		t.Fatal("unsigned-range")
	}
}
