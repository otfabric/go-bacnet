// SPDX-License-Identifier: MIT

package service_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestReadWritePropertyCodecs(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	req := service.ReadPropertyRequest{Object: obj, Property: prop}
	p, err := service.EncodeReadProperty(req)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeReadProperty(p, limits)
	if err != nil || got.Object != obj {
		t.Fatalf("%v %#v", err, got)
	}

	ack := service.ReadPropertyACK{Object: obj, Property: prop, Value: bacnet.RealValue(1.25)}
	ap, err := service.EncodeReadPropertyACK(ack)
	if err != nil {
		t.Fatal(err)
	}
	ag, err := service.DecodeReadPropertyACK(ap, limits)
	if err != nil {
		t.Fatal(err)
	}
	f, err := bacnet.AsReal(ag.Value)
	if err != nil || f != 1.25 {
		t.Fatalf("%v %v", f, err)
	}

	prio := uint8(8)
	wp := service.WritePropertyRequest{
		Object:   obj,
		Property: prop,
		Value:    bacnet.NullValue(),
		Priority: &prio,
	}
	wenc, err := service.EncodeWriteProperty(wp)
	if err != nil {
		t.Fatal(err)
	}
	wgot, err := service.DecodeWriteProperty(wenc, limits)
	if err != nil || wgot.Value.Kind != bacnet.ValueNull || wgot.Priority == nil || *wgot.Priority != 8 {
		t.Fatalf("%v %#v", err, wgot)
	}
}

func TestRPMEncodeRequest(t *testing.T) {
	specs := []service.ReadAccessSpecification{{
		Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		Properties: []bacnet.PropertyReference{
			{Identifier: bacnet.PropertyObjectName},
			{Identifier: bacnet.PropertyPresentValue},
		},
	}}
	enc, err := service.EncodeReadPropertyMultiple(specs)
	if err != nil || len(enc) == 0 {
		t.Fatalf("%v %x", err, enc)
	}
}
