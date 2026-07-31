// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/service"
)

func TestReadPropertyMultipleComplexACK(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
	specs := []service.ReadAccessSpecification{{
		Object: obj,
		Properties: []bacnet.PropertyReference{
			{Identifier: bacnet.PropertyObjectName},
			{Identifier: bacnet.PropertyVendorIdentifier},
		},
	}}
	expected := []service.ReadAccessResult{{
		Object: obj,
		Properties: []service.PropertyResult{
			{
				Property: bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName},
				Value: bacnet.ApplicationValue{
					Kind:      bacnet.ValueCharacterString,
					Character: bacnet.CharacterString{Encoding: 0, Value: "dev-1"},
				},
			},
			{
				Property: bacnet.PropertyReference{Identifier: bacnet.PropertyVendorIdentifier},
				Value:    bacnet.UnsignedValue(999),
			},
		},
	}}

	go serveComplexACK(ctx, env.PeerTr, env.Local, func(serviceChoice uint8) ([]byte, error) {
		if serviceChoice != apdu.ServiceReadPropertyMultiple {
			t.Errorf("unexpected service %d", serviceChoice)
		}
		return service.EncodeReadPropertyMultipleACK(expected)
	})

	results, err := env.Client.ReadPropertyMultiple(ctx, env.Target, specs)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || len(results[0].Properties) != 2 {
		t.Fatalf("results %#v", results)
	}
	if results[0].Properties[0].Err != nil {
		t.Fatalf("object-name error: %v", results[0].Properties[0].Err)
	}
	if results[0].Properties[0].Value.Kind != bacnet.ValueCharacterString ||
		results[0].Properties[0].Value.Character.Value != "dev-1" {
		t.Fatalf("object name %#v", results[0].Properties[0].Value)
	}
	v, err := bacnet.AsUnsigned(results[0].Properties[1].Value)
	if err != nil || v != 999 {
		t.Fatalf("vendor id %d err=%v", v, err)
	}
}
