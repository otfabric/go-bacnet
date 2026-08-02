// SPDX-License-Identifier: MIT

package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/otfabric/go-bacnet"
)

func TestWritePropertyMultipleRoundTrip(t *testing.T) {
	prio := uint8(8)
	idx := uint32(2)
	specs := []WriteAccessSpecification{
		{
			Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogOutput, Instance: 1},
			Properties: []WritePropertyValue{
				{
					Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
					Value:    bacnet.RealValue(21.5),
					Priority: &prio,
				},
				{
					Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue, ArrayIndex: &idx},
					Value:    bacnet.NullValue(),
				},
			},
		},
		{
			Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeBinaryValue, Instance: 3},
			Properties: []WritePropertyValue{
				{
					Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
					Value:    bacnet.EnumValue(1),
				},
			},
		},
	}
	enc, err := EncodeWritePropertyMultiple(specs)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeWritePropertyMultiple(enc, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || len(got[0].Properties) != 2 || len(got[1].Properties) != 1 {
		t.Fatalf("unexpected decode: %+v", got)
	}
	if got[0].Properties[0].Priority == nil || *got[0].Properties[0].Priority != 8 {
		t.Fatalf("priority=%v", got[0].Properties[0].Priority)
	}
	if got[0].Properties[1].Property.ArrayIndex == nil || *got[0].Properties[1].Property.ArrayIndex != 2 {
		t.Fatal("missing array index")
	}
}

func TestWritePropertyMultipleRejectsEmpty(t *testing.T) {
	if _, err := EncodeWritePropertyMultiple(nil); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("got %v", err)
	}
	if _, err := EncodeWritePropertyMultiple([]WriteAccessSpecification{{
		Object: bacnet.ObjectIdentifier{Type: 1, Instance: 1},
	}}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("got %v", err)
	}
}

func TestWritePropertyMultipleErrorRoundTrip(t *testing.T) {
	want := WritePropertyMultipleError{
		Class:       2,
		Code:        32,
		FirstFailed: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogOutput, Instance: 7},
		FirstProperty: bacnet.PropertyReference{
			Identifier: bacnet.PropertyPresentValue,
		},
	}
	enc, err := EncodeWritePropertyMultipleError(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeWritePropertyMultipleError(enc, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if got.Class != want.Class || got.Code != want.Code || got.FirstFailed != want.FirstFailed {
		t.Fatalf("got %+v", got)
	}
	if got.Error() == "" {
		t.Fatal("empty error string")
	}
}

func TestDecodeWritePropertyMultipleMalformed(t *testing.T) {
	if _, err := DecodeWritePropertyMultiple([]byte{0xFF}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected error")
	}
	if _, err := DecodeWritePropertyMultipleError([]byte{0x91, 0x02}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected error")
	}
	badPrio := uint8(20)
	if _, err := EncodeWritePropertyMultiple([]WriteAccessSpecification{{
		Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
		Properties: []WritePropertyValue{{
			Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
			Value:    bacnet.RealValue(1),
			Priority: &badPrio,
		}},
	}}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("bad priority: %v", err)
	}
	// Object without listOfProperties.
	raw, err := bacnet.AppendContextObjectID(nil, 0, bacnet.ObjectIdentifier{Type: 1, Instance: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeWritePropertyMultiple(raw, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("missing properties: %v", err)
	}
	// Error missing firstFailedWriteAttempt.
	partial, err := bacnet.EncodeBACnetError(nil, 2, 32)
	if err != nil {
		t.Fatal(err)
	}
	partial = append([]byte{0x0E}, append(partial, 0x0F)...)
	if _, err := DecodeWritePropertyMultipleError(partial, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("incomplete WPM error: %v", err)
	}
	idx := uint32(3)
	enc, err := EncodeWritePropertyMultipleError(WritePropertyMultipleError{
		Class: 1, Code: 2,
		FirstFailed:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 9},
		FirstProperty: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue, ArrayIndex: &idx},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeWritePropertyMultipleError(enc, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if got.FirstProperty.ArrayIndex == nil || *got.FirstProperty.ArrayIndex != 3 {
		t.Fatalf("array index=%v", got.FirstProperty.ArrayIndex)
	}
}
func TestWPMStrictDuplicatesAndOverflow(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	base, err := EncodeWritePropertyMultiple([]WriteAccessSpecification{{
		Object: obj,
		Properties: []WritePropertyValue{{
			Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
			Value:    bacnet.RealValue(1),
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	// Append a second listOfProperties wrapper for the same object.
	extraList := []byte{0x1E, 0x21, 0x55, 0x2E, 0x44, 0x3f, 0x80, 0x00, 0x00, 0x2F, 0x1F}
	if _, err := DecodeWritePropertyMultiple(append(append([]byte(nil), base...), extraList...), limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("duplicate listOfProperties: %v", err)
	}

	idx := uint32(1)
	prio := uint8(8)
	withPrio, err := EncodeWritePropertyMultiple([]WriteAccessSpecification{{
		Object: obj,
		Properties: []WritePropertyValue{{
			Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue, ArrayIndex: &idx},
			Value:    bacnet.RealValue(1),
			Priority: &prio,
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if withPrio[len(withPrio)-1] != 0x1F {
		t.Fatal("expected trailing list close")
	}
	extraPrio, err := bacnet.AppendContextUnsigned(nil, 3, 9)
	if err != nil {
		t.Fatal(err)
	}
	prioDup := append(append([]byte(nil), withPrio[:len(withPrio)-1]...), append(extraPrio, 0x1F)...)
	if _, err := DecodeWritePropertyMultiple(prioDup, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("duplicate priority: %v", err)
	}

	objRaw, err := bacnet.AppendContextObjectID(nil, 0, obj)
	if err != nil {
		t.Fatal(err)
	}
	propRaw, err := bacnet.AppendContextUnsigned(nil, 0, uint64(bacnet.PropertyPresentValue))
	if err != nil {
		t.Fatal(err)
	}
	i1, err := bacnet.AppendContextUnsigned(nil, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	i2, err := bacnet.AppendContextUnsigned(nil, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	val := []byte{0x2E, 0x44, 0x3f, 0x80, 0x00, 0x00, 0x2F}
	dupIdx := append(append([]byte(nil), objRaw...), 0x1E)
	dupIdx = append(dupIdx, propRaw...)
	dupIdx = append(dupIdx, i1...)
	dupIdx = append(dupIdx, i2...)
	dupIdx = append(dupIdx, val...)
	dupIdx = append(dupIdx, 0x1F)
	_, err = DecodeWritePropertyMultiple(dupIdx, limits)
	if !errors.Is(err, bacnet.ErrMalformed) || !strings.Contains(err.Error(), "duplicate arrayIndex") {
		t.Fatalf("duplicate arrayIndex: %v", err)
	}

	ovIdx, err := bacnet.AppendContextUnsigned(nil, 1, 0x100000000)
	if err != nil {
		t.Fatal(err)
	}
	overflowIdx := append(append([]byte(nil), objRaw...), 0x1E)
	overflowIdx = append(overflowIdx, propRaw...)
	overflowIdx = append(overflowIdx, ovIdx...)
	overflowIdx = append(overflowIdx, val...)
	overflowIdx = append(overflowIdx, 0x1F)
	_, err = DecodeWritePropertyMultiple(overflowIdx, limits)
	if !errors.Is(err, bacnet.ErrMalformed) || !strings.Contains(err.Error(), "arrayIndex overflow") {
		t.Fatalf("arrayIndex overflow: %v", err)
	}
}
