// SPDX-License-Identifier: MIT

package service_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestNotificationEncodeDecodeErrorPaths(t *testing.T) {
	if _, err := service.EncodeNotificationParameters(service.NotificationParameters{}); err == nil {
		t.Fatal("empty")
	}
	// RawElements path
	raw := service.NotificationParameters{Choice: service.NotificationExtended, RawElements: []bacnet.Element{{Value: bacnet.UnsignedValue(1)}}}
	if _, err := service.EncodeNotificationParameters(raw); err != nil {
		t.Fatal(err)
	}
	// Malformed CHOICE wrapper
	if _, err := service.DecodeNotificationParameters(nil); err == nil {
		t.Fatal("expected")
	}
	if _, err := service.DecodeNotificationParameters([]bacnet.Element{{Value: bacnet.UnsignedValue(1)}}); err == nil {
		t.Fatal("expected")
	}
	// Malformed typed bodies
	cases := []struct {
		name string
		els  []bacnet.Element
		tag  uint8
	}{
		{"life-safety", []bacnet.Element{{Value: bacnet.UnsignedValue(1)}}, uint8(service.NotificationChangeOfLifeSafety)},
		{"command-failure", []bacnet.Element{{Value: bacnet.UnsignedValue(1)}}, uint8(service.NotificationCommandFailure)},
		{"unsigned-range", []bacnet.Element{{Value: bacnet.UnsignedValue(1)}}, uint8(service.NotificationUnsignedRange)},
		{"change-of-state", []bacnet.Element{{Value: bacnet.UnsignedValue(1)}}, uint8(service.NotificationChangeOfState)},
		{"change-of-bitstring", []bacnet.Element{{Value: bacnet.UnsignedValue(1)}}, uint8(service.NotificationChangeOfBitstring)},
		{"floating-limit", []bacnet.Element{{Value: bacnet.UnsignedValue(1)}}, uint8(service.NotificationFloatingLimit)},
		{"out-of-range", []bacnet.Element{{Value: bacnet.UnsignedValue(1)}}, uint8(service.NotificationOutOfRange)},
	}
	for _, tc := range cases {
		_, err := service.DecodeNotificationParameters([]bacnet.Element{{
			Context: true, TagNumber: tc.tag,
			Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: tc.els},
		}})
		if err == nil {
			t.Fatalf("%s: expected error", tc.name)
		}
	}
}

func TestNotificationLifeSafetyExtendedDecodeErrors(t *testing.T) {
	badUnsigned := bacnet.Element{Context: true, TagNumber: 0, Value: bacnet.BoolValue(true)}
	flagsOK := bacnet.Element{Context: true, TagNumber: 2, Value: bacnet.ApplicationValue{
		Kind: bacnet.ValueBitString, BitString: bacnet.BitString{UnusedBits: 4, Bytes: []byte{0x40}},
	}}
	// Four fields but wrong types → ContextUnsigned / ContextBitString failures.
	els := []bacnet.Element{
		badUnsigned,
		{Context: true, TagNumber: 1, Value: bacnet.BoolValue(true)},
		{Context: true, TagNumber: 2, Value: bacnet.BoolValue(true)},
		{Context: true, TagNumber: 3, Value: bacnet.BoolValue(true)},
	}
	if _, err := service.DecodeNotificationParameters([]bacnet.Element{{
		Context: true, TagNumber: uint8(service.NotificationChangeOfLifeSafety),
		Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: els},
	}}); err == nil {
		t.Fatal("life-safety typed fields")
	}
	// Progressively valid prefix to hit each ContextUnsigned failure site.
	good0 := bacnet.Element{Context: true, TagNumber: 0, Value: bacnet.UnsignedValue(1)}
	good1 := bacnet.Element{Context: true, TagNumber: 1, Value: bacnet.UnsignedValue(2)}
	for _, body := range [][]bacnet.Element{
		{good0, {Context: true, TagNumber: 1, Value: bacnet.BoolValue(true)}, flagsOK, {Context: true, TagNumber: 3, Value: bacnet.UnsignedValue(3)}},
		{good0, good1, {Context: true, TagNumber: 2, Value: bacnet.BoolValue(true)}, {Context: true, TagNumber: 3, Value: bacnet.UnsignedValue(3)}},
		{good0, good1, flagsOK, {Context: true, TagNumber: 3, Value: bacnet.BoolValue(true)}},
	} {
		if _, err := service.DecodeNotificationParameters([]bacnet.Element{{
			Context: true, TagNumber: uint8(service.NotificationChangeOfLifeSafety),
			Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: body},
		}}); err == nil {
			t.Fatalf("expected life-safety field error for %#v", body)
		}
	}
	if _, err := service.DecodeNotificationParameters([]bacnet.Element{{
		Context: true, TagNumber: uint8(service.NotificationExtended),
		Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{
			{Context: true, TagNumber: 0, Value: bacnet.BoolValue(true)},
		}},
	}}); err == nil {
		t.Fatal("extended vendor")
	}
	if _, err := service.DecodeNotificationParameters([]bacnet.Element{{
		Context: true, TagNumber: uint8(service.NotificationExtended),
		Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{
			{Context: true, TagNumber: 0, Value: bacnet.UnsignedValue(1)},
			{Context: true, TagNumber: 1, Value: bacnet.BoolValue(true)},
		}},
	}}); err == nil {
		t.Fatal("extended event type")
	}
}

func TestNotificationTypedEncodes(t *testing.T) {
	flags := bacnet.BitString{UnusedBits: 4, Bytes: []byte{0x40}}
	for _, in := range []service.NotificationParameters{
		{ChangeOfBitstring: &service.ChangeOfBitstringParams{ReferencedBitstring: flags, StatusFlags: flags}},
		{ChangeOfValue: &service.ChangeOfValueParams{NewValue: bacnet.RealValue(1), StatusFlags: flags}},
		{OutOfRange: &service.OutOfRangeParams{ExceedingValue: 1, StatusFlags: flags, Deadband: 0.1, ExceededLimit: 2}},
		{CommandFailure: &service.CommandFailureParams{CommandValue: bacnet.BoolValue(true), StatusFlags: flags, FeedbackValue: bacnet.BoolValue(false)}},
		{FloatingLimit: &service.FloatingLimitParams{ReferenceValue: 1, StatusFlags: flags, SetPointValue: 2, ErrorLimit: 0.5}},
		{UnsignedRange: &service.UnsignedRangeParams{ExceedingValue: 9, StatusFlags: flags, ExceededLimit: 10}},
		{BufferReady: &service.BufferReadyParams{PreviousCount: 1, CurrentCount: 2}},
		{ChangeOfState: &service.ChangeOfStateParams{NewState: service.PropertyStates{Choice: 1, Value: bacnet.EnumValue(2)}, StatusFlags: flags}},
	} {
		els, err := service.EncodeNotificationParameters(in)
		if err != nil {
			t.Fatalf("%#v: %v", in, err)
		}
		if _, err := service.DecodeNotificationParameters(els); err != nil {
			t.Fatalf("decode %#v: %v", in, err)
		}
	}
}
