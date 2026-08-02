// SPDX-License-Identifier: MIT

package service_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestListElementWithArrayIndex(t *testing.T) {
	idx := uint32(2)
	raw, err := service.EncodeListElementRequest(service.ListElementRequest{
		Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		Property: bacnet.PropertyReference{
			Identifier: bacnet.PropertyObjectName,
			ArrayIndex: &idx,
		},
		Elements: []bacnet.Element{{Value: bacnet.UnsignedValue(9)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeListElementRequest(raw, bacnet.DefaultDecodeLimits())
	if err != nil || got.Property.ArrayIndex == nil || *got.Property.ArrayIndex != 2 {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestListElementDecodeMissingFields(t *testing.T) {
	raw, err := bacnet.AppendContextObjectID(nil, 0, bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeListElementRequest(raw, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected missing fields")
	}
}
