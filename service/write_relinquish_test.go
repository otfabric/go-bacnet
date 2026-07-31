// SPDX-License-Identifier: MIT

package service_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestWritePropertyNullRelinquish(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	prio := uint8(8)
	idx := uint32(1)
	enc, err := service.EncodeWriteProperty(service.WritePropertyRequest{
		Object:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
		Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue, ArrayIndex: &idx},
		Value:    bacnet.NullValue(),
		Priority: &prio,
	})
	if err != nil {
		t.Fatal(err)
	}
	dec, err := service.DecodeWriteProperty(enc, limits)
	if err != nil {
		t.Fatal(err)
	}
	if dec.Value.Kind != bacnet.ValueNull || dec.Priority == nil || *dec.Priority != 8 || dec.Property.ArrayIndex == nil {
		t.Fatalf("%+v", dec)
	}
}
