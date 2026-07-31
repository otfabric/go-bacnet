// SPDX-License-Identifier: MIT

package service_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestEncodeSubscribeCOVPropertyWithIncrementRoundTrip(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	inc := float32(1.5)
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 5}
	enc, err := service.EncodeSubscribeCOVProperty(service.SubscribeCOVPropertyRequest{
		SubscribeCOVRequest: service.SubscribeCOVRequest{
			ProcessIdentifier: 3,
			MonitoredObject:   obj,
			IssueConfirmed:    false,
			Lifetime:          90,
		},
		Property:     bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
		COVIncrement: &inc,
	})
	if err != nil {
		t.Fatal(err)
	}
	dec, err := service.DecodeSubscribeCOVProperty(enc, limits)
	if err != nil || dec.COVIncrement == nil || *dec.COVIncrement != inc {
		t.Fatalf("%+v %v", dec, err)
	}
}

func TestEncodeWritePropertyConstructedValue(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	val := bacnet.ApplicationValue{
		Kind: bacnet.ValueConstructed,
		Elements: []bacnet.Element{
			{Value: bacnet.RealValue(1.0)},
		},
	}
	wp := service.WritePropertyRequest{
		Object:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogOutput, Instance: 1},
		Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
		Value:    val,
	}
	enc, err := service.EncodeWriteProperty(wp)
	if err != nil {
		t.Fatal(err)
	}
	dec, err := service.DecodeWriteProperty(enc, limits)
	if err != nil {
		t.Fatalf("%v", err)
	}
	f, err := bacnet.AsReal(dec.Value)
	if err != nil || f != 1.0 {
		t.Fatalf("value %#v", dec.Value)
	}
}

func TestEncodeReadPropertyMultipleMultipleObjects(t *testing.T) {
	specs := []service.ReadAccessSpecification{
		{
			Object:     bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
			Properties: []bacnet.PropertyReference{{Identifier: bacnet.PropertyObjectName}},
		},
		{
			Object:     bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 2},
			Properties: []bacnet.PropertyReference{{Identifier: bacnet.PropertyVendorIdentifier}},
		},
	}
	enc, err := service.EncodeReadPropertyMultiple(specs)
	if err != nil || len(enc) == 0 {
		t.Fatalf("%v", err)
	}
}

func TestRPMACKConstructedPropertyValue(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
	val := bacnet.ApplicationValue{
		Kind: bacnet.ValueConstructed,
		Elements: []bacnet.Element{
			{Value: bacnet.UnsignedValue(7)},
		},
	}
	results := []service.ReadAccessResult{{
		Object: obj,
		Properties: []service.PropertyResult{{
			Property: bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName},
			Value:    val,
		}},
	}}
	enc, err := service.EncodeReadPropertyMultipleACK(results)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeReadPropertyMultipleACK(enc, limits)
	if err != nil {
		t.Fatalf("%v", err)
	}
	v, err := bacnet.AsUnsigned(got[0].Properties[0].Value)
	if err != nil || v != 7 {
		t.Fatalf("%+v", got[0].Properties[0].Value)
	}
}
