// SPDX-License-Identifier: MIT

package service_test

import (
	"encoding/hex"
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/service"
)

func apduDecodeError(payload []byte) (uint16, uint16, error) {
	return apdu.DecodeErrorClassCode(payload, bacnet.DefaultDecodeLimits())
}

func TestRPMPartialPropertyErrorRoundTrip(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
	results := []service.ReadAccessResult{{
		Object: obj,
		Properties: []service.PropertyResult{
			{
				Property: bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName},
				Value: bacnet.ApplicationValue{
					Kind:      bacnet.ValueCharacterString,
					Character: bacnet.CharacterString{Encoding: 0, Value: "dev"},
				},
			},
			{
				Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
				Err:      &bacnet.ErrorResponse{Class: 2, Code: 32}, // property / unknown-property
			},
		},
	}}
	enc, err := service.EncodeReadPropertyMultipleACK(results)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeReadPropertyMultipleACK(enc, limits)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0].Properties) != 2 {
		t.Fatalf("%+v", got)
	}
	if got[0].Properties[0].Err != nil {
		t.Fatal("first property should succeed")
	}
	var er *bacnet.ErrorResponse
	if !errors.As(got[0].Properties[1].Err, &er) || er.Class != 2 || er.Code != 32 {
		t.Fatalf("%v", got[0].Properties[1].Err)
	}
}

func TestRPMPropertyErrorExternalVector(t *testing.T) {
	// Independently constructed RPM ACK (not produced by mirroring a prior decode):
	// device 1, object-name = "a", present-value propertyAccessError class=2 code=32.
	// BACnet-Error uses context [0] class and [1] code.
	const wire = "0c020000011e294d4e7200614f29555e090219205f1f"
	raw, err := hex.DecodeString(wire)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeReadPropertyMultipleACK(raw, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0].Properties) != 2 {
		t.Fatalf("%+v", got)
	}
	var er *bacnet.ErrorResponse
	if !errors.As(got[0].Properties[1].Err, &er) || er.Class != 2 || er.Code != 32 {
		t.Fatalf("%v", got[0].Properties[1].Err)
	}
}

func TestErrorPDUContextTags(t *testing.T) {
	payload, err := bacnet.EncodeBACnetError(nil, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	class, code, err := apduDecodeError(payload)
	if err != nil {
		t.Fatal(err)
	}
	if class != 1 || code != 2 {
		t.Fatalf("%d %d", class, code)
	}
}
