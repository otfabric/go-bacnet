// SPDX-License-Identifier: MIT

package service_test

import (
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestDecodeReadPropertyContextUnsignedFailure(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
	p, err := bacnet.AppendContextObjectID(nil, 0, obj)
	if err != nil {
		t.Fatal(err)
	}
	// Invalid context unsigned tag payload for property identifier.
	p = append(p, 0x19)
	if _, err := service.DecodeReadProperty(p, limits); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestDecodeReadPropertyACKDuplicateProperty(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	p, err := bacnet.AppendContextObjectID(nil, 0, obj)
	if err != nil {
		t.Fatal(err)
	}
	p, err = bacnet.AppendContextUnsigned(p, 1, uint64(bacnet.PropertyPresentValue))
	if err != nil {
		t.Fatal(err)
	}
	p, err = bacnet.AppendContextUnsigned(p, 1, uint64(bacnet.PropertyObjectName))
	if err != nil {
		t.Fatal(err)
	}
	p = append(p, 0x3E, 0x3F)
	if _, err := service.DecodeReadPropertyACK(p, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("duplicate property: %v", err)
	}
}

func TestDecodeSubscribeCOVPropertyUnexpectedTag(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	if _, err := service.DecodeSubscribeCOVProperty([]byte{0x49, 0x01}, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("got %v", err)
	}
}

func TestDecodeCOVNotificationMissingFields(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	p, err := bacnet.AppendContextUnsigned(nil, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeCOVNotification(p, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("got %v", err)
	}
}

func TestDecodeRPMArrayIndexWithoutProperty(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
	p, err := bacnet.AppendContextObjectID(nil, 0, obj)
	if err != nil {
		t.Fatal(err)
	}
	p = append(p, 0x1E)
	p, err = bacnet.AppendContextUnsigned(p, 3, 1)
	if err != nil {
		t.Fatal(err)
	}
	p = append(p, 0x1F)
	if _, err := service.DecodeReadPropertyMultipleACK(p, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("array index without property: %v", err)
	}
}

func TestDecodeSubscribeCOVPropertyNestedConstructedInPropertyRef(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	p, err := bacnet.AppendContextUnsigned(nil, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	p, err = bacnet.AppendContextObjectID(p, 1, obj)
	if err != nil {
		t.Fatal(err)
	}
	p, err = bacnet.AppendContextBool(p, 2, true)
	if err != nil {
		t.Fatal(err)
	}
	p, err = bacnet.AppendContextUnsigned(p, 3, 60)
	if err != nil {
		t.Fatal(err)
	}
	p = append(p, 0x4E, 0x6E, 0x4F)
	if _, err := service.DecodeSubscribeCOVProperty(p, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("nested constructed property ref: %v", err)
	}
}

func TestReadPropertyACKWithArrayIndexRoundTrip(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	idx := uint32(2)
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	ack := service.ReadPropertyACK{
		Object: obj,
		Property: bacnet.PropertyReference{
			Identifier: bacnet.PropertyObjectName,
			ArrayIndex: &idx,
		},
		Value: bacnet.UnsignedValue(42),
	}
	enc, err := service.EncodeReadPropertyACK(ack)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeReadPropertyACK(enc, limits)
	if err != nil {
		t.Fatal(err)
	}
	if got.Property.ArrayIndex == nil || *got.Property.ArrayIndex != idx {
		t.Fatalf("array index: %+v", got.Property)
	}
	if got.Value.Unsigned != 42 {
		t.Fatalf("value: %+v", got.Value)
	}
}

func TestEncodeReadPropertyACKBadInputs(t *testing.T) {
	badObj := bacnet.ObjectIdentifier{Type: bacnet.ObjectType(0x400), Instance: 1}
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}

	if _, err := service.EncodeReadPropertyACK(service.ReadPropertyACK{
		Object:   badObj,
		Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
		Value:    bacnet.RealValue(1.0),
	}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("bad object: %v", err)
	}
	if _, err := service.EncodeReadPropertyACK(service.ReadPropertyACK{
		Object:   obj,
		Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
		Value:    bacnet.ApplicationValue{Kind: bacnet.ValueKind(200)},
	}); !errors.Is(err, bacnet.ErrUnsupported) {
		t.Fatalf("bad value kind: %v", err)
	}
}

func TestDecodeReadPropertyACKMalformedExtra(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()

	tests := []struct {
		name    string
		build   func(t *testing.T) []byte
		wantErr error
	}{
		{
			name:    "ParseSequence truncated",
			build:   func(t *testing.T) []byte { return []byte{0x19} },
			wantErr: bacnet.ErrMalformed,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := service.DecodeReadPropertyACK(tc.build(t), limits)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v want %v", err, tc.wantErr)
			}
		})
	}
}
