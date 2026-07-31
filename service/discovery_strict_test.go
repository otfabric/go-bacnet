// SPDX-License-Identifier: MIT

package service_test

import (
	"encoding/hex"
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestDecodeWhoIsRejectsPartialLimits(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	// Context unsigned tag 0 only (low without high).
	payload, err := bacnet.AppendContextUnsigned(nil, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.DecodeWhoIs(payload, limits)
	if !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("got %v", err)
	}
}

func TestDecodeWhoIsRejectsDuplicatesAndUnknown(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	payload, err := bacnet.AppendContextUnsigned(nil, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	payload, err = bacnet.AppendContextUnsigned(payload, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.DecodeWhoIs(payload, limits)
	if !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("duplicate: %v", err)
	}
	payload, err = bacnet.AppendContextUnsigned(nil, 3, 1)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.DecodeWhoIs(payload, limits)
	if !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("unknown tag: %v", err)
	}
}

func TestDecodeIAmRejectsNonDeviceAndBadSegmentation(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	bad := service.IAm{
		Device:        bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
		MaxAPDULength: 480,
		Segmentation:  0,
		VendorID:      1,
	}
	p, err := service.EncodeIAm(bad)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.DecodeIAm(p, limits)
	if !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("non-device: %v", err)
	}

	// Hand-built: Device + MaxAPDU + invalid segmentation enum 4 + vendor.
	p = nil
	p, err = bacnet.AppendApplicationValue(p, bacnet.ObjectIDValue(bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}))
	if err != nil {
		t.Fatal(err)
	}
	p, err = bacnet.AppendApplicationValue(p, bacnet.UnsignedValue(480))
	if err != nil {
		t.Fatal(err)
	}
	p, err = bacnet.AppendApplicationValue(p, bacnet.EnumValue(4))
	if err != nil {
		t.Fatal(err)
	}
	p, err = bacnet.AppendApplicationValue(p, bacnet.UnsignedValue(15))
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.DecodeIAm(p, limits)
	if !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("bad seg: %v (%s)", err, hex.EncodeToString(p))
	}
}

func TestDecodeReadPropertyRejectsMissingAndDuplicate(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	_, err := service.DecodeReadProperty(nil, limits)
	if !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("empty: %v", err)
	}
	payload, err := bacnet.AppendContextObjectID(nil, 0, bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.DecodeReadProperty(payload, limits)
	if !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("missing property: %v", err)
	}
	payload, err = bacnet.AppendContextObjectID(nil, 0, bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1})
	if err != nil {
		t.Fatal(err)
	}
	payload, err = bacnet.AppendContextObjectID(payload, 0, bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 2})
	if err != nil {
		t.Fatal(err)
	}
	payload, err = bacnet.AppendContextUnsigned(payload, 1, 75)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.DecodeReadProperty(payload, limits)
	if !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("duplicate object: %v", err)
	}
}
