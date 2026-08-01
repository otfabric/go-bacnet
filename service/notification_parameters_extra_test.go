// SPDX-License-Identifier: MIT

package service

import (
	"encoding/binary"
	"errors"
	"math"
	"testing"

	"github.com/otfabric/go-bacnet"
)

func TestNotificationParametersChangeOfBitstringRoundTrip(t *testing.T) {
	flags := bacnet.BitString{UnusedBits: 4, Bytes: []byte{0x10}}
	ref := bacnet.BitString{UnusedBits: 4, Bytes: []byte{0xf0}}
	params := NotificationParameters{
		ChangeOfBitstring: &ChangeOfBitstringParams{
			ReferencedBitstring: ref,
			StatusFlags:         flags,
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
	if got.ChangeOfBitstring == nil || got.Choice != NotificationChangeOfBitstring {
		t.Fatalf("%+v", got)
	}
}

func TestNotificationParametersChangeOfValueRoundTrip(t *testing.T) {
	flags := bacnet.BitString{UnusedBits: 4, Bytes: []byte{0x00}}
	params := NotificationParameters{
		ChangeOfValue: &ChangeOfValueParams{
			NewValue: bacnet.ApplicationValue{
				Kind: bacnet.ValueConstructed,
				Elements: []bacnet.Element{
					{Value: bacnet.RealValue(12.5)},
				},
			},
			StatusFlags: flags,
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
	if got.ChangeOfValue == nil || got.Choice != NotificationChangeOfValue {
		t.Fatalf("%+v", got)
	}
}

func TestNotificationParametersRawAndEmpty(t *testing.T) {
	if _, err := EncodeNotificationParameters(NotificationParameters{}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("empty: %v", err)
	}
	params := NotificationParameters{
		Choice: NotificationBufferReady,
		RawElements: []bacnet.Element{
			{Context: true, TagNumber: 0, Value: bacnet.ApplicationValue{Kind: bacnet.ValueContext, OctetString: []byte{1}}},
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
	if got.Choice != NotificationBufferReady || len(got.RawElements) != 1 {
		t.Fatalf("%+v", got)
	}
}

func TestNotificationParametersPropertyStatesKinds(t *testing.T) {
	flags := bacnet.BitString{UnusedBits: 4, Bytes: []byte{0x00}}
	for _, ps := range []PropertyStates{
		{Choice: 0, Value: bacnet.BoolValue(true)},
		{Choice: 8, Value: bacnet.UnsignedValue(2)},
		{Choice: 8, Value: bacnet.EnumValue(1)},
	} {
		params := NotificationParameters{
			ChangeOfState: &ChangeOfStateParams{NewState: ps, StatusFlags: flags},
		}
		els, err := EncodeNotificationParameters(params)
		if err != nil {
			t.Fatalf("ps %+v: %v", ps, err)
		}
		got, err := DecodeNotificationParameters(els)
		if err != nil || got.ChangeOfState == nil {
			t.Fatalf("decode ps %+v: %v %+v", ps, err, got)
		}
	}
}

func TestDecodeNotificationParametersMalformed(t *testing.T) {
	if _, err := DecodeNotificationParameters(nil); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("%v", err)
	}
	if _, err := DecodeNotificationParameters([]bacnet.Element{{Value: bacnet.RealValue(1)}}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("%v", err)
	}
	// Typed decode fails → RawElements kept, no typed body.
	badCOS := []bacnet.Element{{
		Context: true, TagNumber: uint8(NotificationChangeOfState),
		Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed},
	}}
	got, err := DecodeNotificationParameters(badCOS)
	if err != nil || got.ChangeOfState != nil {
		t.Fatalf("bad COS: %v %+v", err, got)
	}
	badCOV := []bacnet.Element{{
		Context: true, TagNumber: uint8(NotificationChangeOfValue),
		Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{
			{Context: true, TagNumber: 9},
		}},
	}}
	got, err = DecodeNotificationParameters(badCOV)
	if err != nil || got.ChangeOfValue != nil {
		t.Fatalf("bad COV: %v %+v", err, got)
	}
	badCOB := []bacnet.Element{{
		Context: true, TagNumber: uint8(NotificationChangeOfBitstring),
		Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed},
	}}
	got, err = DecodeNotificationParameters(badCOB)
	if err != nil || got.ChangeOfBitstring != nil {
		t.Fatalf("bad COB: %v %+v", err, got)
	}
	// Unknown CHOICE tag hits default.
	unknown := []bacnet.Element{{
		Context: true, TagNumber: 99,
		Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed},
	}}
	got, err = DecodeNotificationParameters(unknown)
	if err != nil || got.Choice != 99 {
		t.Fatalf("unknown: %v %+v", err, got)
	}
}

func TestNotificationParametersDefaultPropertyStates(t *testing.T) {
	flags := bacnet.BitString{UnusedBits: 4, Bytes: []byte{0x00}}
	params := NotificationParameters{
		ChangeOfState: &ChangeOfStateParams{
			NewState: PropertyStates{
				Choice: 10,
				Value: bacnet.ApplicationValue{
					Kind: bacnet.ValueContext, OctetString: []byte{1, 2},
				},
			},
			StatusFlags: flags,
		},
	}
	els, err := EncodeNotificationParameters(params)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeNotificationParameters(els); err != nil {
		t.Fatal(err)
	}
}

func TestNotificationParametersContextRealForms(t *testing.T) {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], math.Float32bits(12.5))
	f, err := contextReal(bacnet.Element{
		Context: true, TagNumber: 0,
		Value: bacnet.ApplicationValue{Kind: bacnet.ValueContext, OctetString: buf[:]},
	}, 0)
	if err != nil || f != 12.5 {
		t.Fatalf("prim: %v %v", err, f)
	}
	if _, err := contextReal(bacnet.Element{TagNumber: 1}, 0); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("tag: %v", err)
	}
	if _, err := contextReal(bacnet.Element{
		TagNumber: 0,
		Context:   true,
		Value:     bacnet.ApplicationValue{Kind: bacnet.ValueConstructed},
	}, 0); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("constructed: %v", err)
	}
	if _, err := contextReal(bacnet.Element{
		TagNumber: 0,
		Context:   true,
		Value:     bacnet.ApplicationValue{Kind: bacnet.ValueContext, OctetString: []byte{1}},
	}, 0); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("len: %v", err)
	}
}

func TestNotificationParametersChangeOfStateForms(t *testing.T) {
	flagsRaw, err := bacnet.AppendContextBitString(nil, 1, bacnet.BitString{UnusedBits: 4, Bytes: []byte{0x00}})
	if err != nil {
		t.Fatal(err)
	}
	flagsEl, _, err := bacnet.ParseTag(flagsRaw, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	els := []bacnet.Element{
		{Context: true, TagNumber: 0, Value: bacnet.ApplicationValue{
			Kind: bacnet.ValueConstructed,
			Elements: []bacnet.Element{{
				Context: true, TagNumber: 8,
				Value: bacnet.ApplicationValue{
					Kind: bacnet.ValueConstructed,
					Elements: []bacnet.Element{
						{Value: bacnet.EnumValue(1)},
					},
				},
			}},
		}},
		flagsEl,
	}
	got, err := decodeChangeOfState(els)
	if err != nil || got.NewState.Value.Kind != bacnet.ValueConstructed {
		t.Fatalf("%v %+v", err, got)
	}
	if _, err := decodeChangeOfState([]bacnet.Element{
		{Context: true, TagNumber: 0, Value: bacnet.ApplicationValue{
			Kind:     bacnet.ValueConstructed,
			Elements: []bacnet.Element{{Value: bacnet.EnumValue(1)}},
		}},
		flagsEl,
	}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("non-context ps: %v", err)
	}
	if _, err := decodeChangeOfState([]bacnet.Element{
		els[0],
		{Context: true, TagNumber: 2, Value: flagsEl.Value},
	}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("flags tag: %v", err)
	}
}

func TestNotificationParametersChangeOfValuePrimitive(t *testing.T) {
	flagsRaw, err := bacnet.AppendContextBitString(nil, 1, bacnet.BitString{UnusedBits: 4, Bytes: []byte{0x00}})
	if err != nil {
		t.Fatal(err)
	}
	flagsEl, _, err := bacnet.ParseTag(flagsRaw, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	prim, err := bacnet.AppendContextUnsigned(nil, 0, 7)
	if err != nil {
		t.Fatal(err)
	}
	newVal, _, err := bacnet.ParseTag(prim, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeChangeOfValue([]bacnet.Element{newVal, flagsEl})
	if err != nil || got.NewValue.Kind != bacnet.ValueContext {
		t.Fatalf("%v %+v", err, got)
	}
	if _, err := decodeChangeOfValue([]bacnet.Element{newVal}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("short: %v", err)
	}
	if _, err := decodeChangeOfBitstring([]bacnet.Element{flagsEl, flagsEl}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("cob tag0: %v", err)
	}
}

func TestNotificationParametersOutOfRangePrimitiveReals(t *testing.T) {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], math.Float32bits(1.25))
	mk := func(tag uint8) bacnet.Element {
		return bacnet.Element{
			Context: true, TagNumber: tag,
			Value: bacnet.ApplicationValue{Kind: bacnet.ValueContext, OctetString: append([]byte(nil), buf[:]...)},
		}
	}
	flagsRaw, err := bacnet.AppendContextBitString(nil, 1, bacnet.BitString{UnusedBits: 4, Bytes: []byte{0x00}})
	if err != nil {
		t.Fatal(err)
	}
	flagsEl, _, err := bacnet.ParseTag(flagsRaw, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeOutOfRange([]bacnet.Element{mk(0), flagsEl, mk(2), mk(3)})
	if err != nil || got.ExceedingValue != 1.25 || got.ExceededLimit != 1.25 {
		t.Fatalf("%v %+v", err, got)
	}
	if _, err := decodeOutOfRange([]bacnet.Element{mk(0)}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("short: %v", err)
	}
	badFlags := []bacnet.Element{mk(0), {Context: true, TagNumber: 9}, mk(2), mk(3)}
	if _, err := decodeOutOfRange(badFlags); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("flags: %v", err)
	}
}
