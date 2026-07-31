// SPDX-License-Identifier: MIT

package service_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/service"
)

func TestEncodeWhoIsAndIAmAPDU(t *testing.T) {
	low, high := uint32(1), uint32(10)
	raw, err := service.EncodeWhoIsAPDU(service.WhoIs{LowLimit: &low, HighLimit: &high})
	if err != nil {
		t.Fatal(err)
	}
	pdu, err := apdu.Parse(raw, bacnet.DefaultDecodeLimits())
	if err != nil || pdu.UnconfirmedRequest == nil || pdu.UnconfirmedRequest.ServiceChoice != apdu.ServiceWhoIs {
		t.Fatalf("%v %#v", err, pdu)
	}

	raw, err = service.EncodeIAmAPDU(service.IAm{
		Device:        bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 5},
		MaxAPDULength: 480, VendorID: 999,
	})
	if err != nil {
		t.Fatal(err)
	}
	pdu, err = apdu.Parse(raw, bacnet.DefaultDecodeLimits())
	if err != nil || pdu.UnconfirmedRequest == nil || pdu.UnconfirmedRequest.ServiceChoice != apdu.ServiceIAm {
		t.Fatalf("%v %#v", err, pdu)
	}
}

func TestEncodeReadPropertyPayloadAndWriteRoundTrip(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	req := service.ReadPropertyRequest{
		Object:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
		Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
	}
	payload, err := service.EncodeReadPropertyPayload(req)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeReadProperty(payload, limits)
	if err != nil || !got.Property.Equal(req.Property) {
		t.Fatalf("%+v %v", got, err)
	}

	prio := uint8(8)
	wp := service.WritePropertyRequest{
		Object:   req.Object,
		Property: req.Property,
		Value:    bacnet.RealValue(1.25),
		Priority: &prio,
	}
	enc, err := service.EncodeWriteProperty(wp)
	if err != nil {
		t.Fatal(err)
	}
	dec, err := service.DecodeWriteProperty(enc, limits)
	if err != nil || dec.Priority == nil || *dec.Priority != 8 {
		t.Fatalf("%+v %v", dec, err)
	}
	f, err := bacnet.AsReal(dec.Value)
	if err != nil || f != 1.25 {
		t.Fatalf("value %v %v", f, err)
	}
}

func TestReadPropertyMultipleACKRoundTrip(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	results := []service.ReadAccessResult{{
		Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		Properties: []service.PropertyResult{{
			Property: bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName},
			Value:    bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: "Dev"}},
		}},
	}}
	enc, err := service.EncodeReadPropertyMultipleACK(results)
	if err != nil {
		t.Fatal(err)
	}
	dec, err := service.DecodeReadPropertyMultipleACK(enc, limits)
	if err != nil || len(dec) != 1 || len(dec[0].Properties) != 1 {
		t.Fatalf("%+v %v", dec, err)
	}
	if dec[0].Properties[0].Value.Character.Value != "Dev" {
		t.Fatalf("%+v", dec[0].Properties[0])
	}
}
