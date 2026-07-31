// SPDX-License-Identifier: MIT

package service_test

import (
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestReadPropertyWithArrayIndexRoundTrip(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	idx := uint32(1)
	req := service.ReadPropertyRequest{
		Object:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		Property: bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName, ArrayIndex: &idx},
	}
	enc, err := service.EncodeReadProperty(req)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeReadProperty(enc, limits)
	if err != nil || got.Property.ArrayIndex == nil || *got.Property.ArrayIndex != 1 {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestRPMACKWithArrayIndexAndError(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	idx := uint32(0)
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
	results := []service.ReadAccessResult{{
		Object: obj,
		Properties: []service.PropertyResult{{
			Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue, ArrayIndex: &idx},
			Err:      &bacnet.ErrorResponse{Class: 2, Code: 32},
		}},
	}}
	enc, err := service.EncodeReadPropertyMultipleACK(results)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeReadPropertyMultipleACK(enc, limits)
	if err != nil || len(got) != 1 || got[0].Properties[0].Err == nil {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestDecodeWhoIsLimitOverflow(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	p, err := bacnet.AppendContextUnsigned(nil, 0, 1<<32)
	if err != nil {
		t.Fatal(err)
	}
	p, err = bacnet.AppendContextUnsigned(p, 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeWhoIs(p, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("overflow: %v", err)
	}
}

func TestDecodeSubscribeCOVDuplicateMonitoredObject(t *testing.T) {
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
	p, err = bacnet.AppendContextObjectID(p, 1, obj)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeSubscribeCOV(p, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("duplicate object: %v", err)
	}
}

func TestDecodeCOVNotificationDuplicateListOfValues(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	dev := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 2}
	p, err := bacnet.AppendContextUnsigned(nil, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	p, err = bacnet.AppendContextObjectID(p, 1, dev)
	if err != nil {
		t.Fatal(err)
	}
	p, err = bacnet.AppendContextObjectID(p, 2, obj)
	if err != nil {
		t.Fatal(err)
	}
	p, err = bacnet.AppendContextUnsigned(p, 3, 10)
	if err != nil {
		t.Fatal(err)
	}
	p = append(p, 0x4E, 0x4F, 0x4E, 0x4F)
	if _, err := service.DecodeCOVNotification(p, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("duplicate values: %v", err)
	}
}
