// SPDX-License-Identifier: MIT

package service_test

import (
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestDecodeSubscribeCOVPropertyPropertyReferenceErrors(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}

	dupPropID, err := bacnet.AppendContextUnsigned(nil, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	dupPropID, err = bacnet.AppendContextObjectID(dupPropID, 1, obj)
	if err != nil {
		t.Fatal(err)
	}
	dupPropID, err = bacnet.AppendContextBool(dupPropID, 2, true)
	if err != nil {
		t.Fatal(err)
	}
	dupPropID, err = bacnet.AppendContextUnsigned(dupPropID, 3, 60)
	if err != nil {
		t.Fatal(err)
	}
	dupPropID = append(dupPropID, 0x4E)
	dupPropID, err = bacnet.AppendContextUnsigned(dupPropID, 0, uint64(bacnet.PropertyPresentValue))
	if err != nil {
		t.Fatal(err)
	}
	dupPropID, err = bacnet.AppendContextUnsigned(dupPropID, 0, uint64(bacnet.PropertyObjectName))
	if err != nil {
		t.Fatal(err)
	}
	dupPropID = append(dupPropID, 0x4F)
	if _, err := service.DecodeSubscribeCOVProperty(dupPropID, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("duplicate property id: %v", err)
	}

	dupArray, err := bacnet.AppendContextUnsigned(nil, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	dupArray, err = bacnet.AppendContextObjectID(dupArray, 1, obj)
	if err != nil {
		t.Fatal(err)
	}
	dupArray, err = bacnet.AppendContextBool(dupArray, 2, true)
	if err != nil {
		t.Fatal(err)
	}
	dupArray, err = bacnet.AppendContextUnsigned(dupArray, 3, 60)
	if err != nil {
		t.Fatal(err)
	}
	dupArray = append(dupArray, 0x4E)
	dupArray, err = bacnet.AppendContextUnsigned(dupArray, 0, uint64(bacnet.PropertyPresentValue))
	if err != nil {
		t.Fatal(err)
	}
	dupArray, err = bacnet.AppendContextUnsigned(dupArray, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	dupArray, err = bacnet.AppendContextUnsigned(dupArray, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	dupArray = append(dupArray, 0x4F)
	if _, err := service.DecodeSubscribeCOVProperty(dupArray, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("duplicate array index: %v", err)
	}
}

func TestDecodeSubscribeCOVIncompleteAndDuplicateBool(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 2}

	dupBool, err := bacnet.AppendContextUnsigned(nil, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	dupBool, err = bacnet.AppendContextObjectID(dupBool, 1, obj)
	if err != nil {
		t.Fatal(err)
	}
	dupBool, err = bacnet.AppendContextBool(dupBool, 2, true)
	if err != nil {
		t.Fatal(err)
	}
	dupBool, err = bacnet.AppendContextBool(dupBool, 2, false)
	if err != nil {
		t.Fatal(err)
	}
	dupBool, err = bacnet.AppendContextUnsigned(dupBool, 3, 60)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeSubscribeCOV(dupBool, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("duplicate bool: %v", err)
	}
}

func TestDecodeReadPropertyACKUnexpectedTag(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	if _, err := service.DecodeReadPropertyACK([]byte{0x39, 0x01}, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("unexpected tag: %v", err)
	}
}

func TestDecodeIAmWrongValueTypes(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	dev := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
	p, err := bacnet.AppendApplicationValue(nil, bacnet.ObjectIDValue(dev))
	if err != nil {
		t.Fatal(err)
	}
	p, err = bacnet.AppendApplicationValue(p, bacnet.RealValue(1.0))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeIAm(p, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("max APDU type: %v", err)
	}
}

func TestDecodeCOVNotificationConstructedValue(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	note := service.COVNotification{
		ProcessIdentifier: 2,
		InitiatingDevice:  bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		MonitoredObject:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
		TimeRemaining:     5,
		Values: []service.PropertyValue{{
			Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
			Value: bacnet.ApplicationValue{
				Kind: bacnet.ValueConstructed,
				Elements: []bacnet.Element{
					{Value: bacnet.RealValue(2.0)},
					{Value: bacnet.RealValue(3.0)},
				},
			},
		}},
	}
	raw := encodeCOVNotification(t, note)
	got, err := service.DecodeCOVNotification(raw, limits)
	if err != nil || len(got.Values) != 1 || got.Values[0].Value.Kind != bacnet.ValueConstructed {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestEncodeSubscribeCOVPropertyCancellationWithArrayIndex(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	idx := uint32(1)
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue, ArrayIndex: &idx}
	enc, err := service.EncodeSubscribeCOVProperty(service.SubscribeCOVPropertyRequest{
		SubscribeCOVRequest: service.SubscribeCOVRequest{
			ProcessIdentifier: 1,
			MonitoredObject:   obj,
			Cancellation:      true,
		},
		Property: prop,
	})
	if err != nil {
		t.Fatal(err)
	}
	dec, err := service.DecodeSubscribeCOVProperty(enc, limits)
	if err != nil || !dec.Cancellation || dec.Property.ArrayIndex == nil {
		t.Fatalf("%+v %v", dec, err)
	}
}

func TestDecodeRPMDuplicatePropertyOutcome(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
	p, err := bacnet.AppendContextObjectID(nil, 0, obj)
	if err != nil {
		t.Fatal(err)
	}
	p = append(p, 0x1E)
	p, err = bacnet.AppendContextUnsigned(p, 2, uint64(bacnet.PropertyPresentValue))
	if err != nil {
		t.Fatal(err)
	}
	p = append(p, 0x4E, 0x44, 0x00, 0x00, 0x80, 0x3F, 0x4F)
	p = append(p, 0x4E, 0x44, 0x00, 0x00, 0x00, 0x00, 0x4F)
	p = append(p, 0x1F)
	if _, err := service.DecodeReadPropertyMultipleACK(p, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("duplicate outcome: %v", err)
	}
}

func TestDecodeRPMPropertyAccessErrorMalformed(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
	p, err := bacnet.AppendContextObjectID(nil, 0, obj)
	if err != nil {
		t.Fatal(err)
	}
	p = append(p, 0x1E)
	p, err = bacnet.AppendContextUnsigned(p, 2, uint64(bacnet.PropertyPresentValue))
	if err != nil {
		t.Fatal(err)
	}
	p = append(p, 0x5E, 0x5F)
	p = append(p, 0x1F)
	if _, err := service.DecodeReadPropertyMultipleACK(p, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("empty property error: %v", err)
	}
}

func TestEncodeWritePropertyWithArrayIndex(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	idx := uint32(1)
	wp := service.WritePropertyRequest{
		Object:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogOutput, Instance: 1},
		Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue, ArrayIndex: &idx},
		Value:    bacnet.RealValue(3.0),
	}
	enc, err := service.EncodeWriteProperty(wp)
	if err != nil {
		t.Fatal(err)
	}
	dec, err := service.DecodeWriteProperty(enc, limits)
	if err != nil || dec.Property.ArrayIndex == nil || *dec.Property.ArrayIndex != 1 {
		t.Fatalf("%+v %v", dec, err)
	}
}
