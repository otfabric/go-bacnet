// SPDX-License-Identifier: MIT

package service_test

import (
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestIAmSegmentationValuesRoundTrip(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	dev := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 9}
	for _, seg := range []uint8{0, 1, 2, 3} {
		iam := service.IAm{Device: dev, MaxAPDULength: 480, Segmentation: seg, VendorID: 1}
		raw, err := service.EncodeIAm(iam)
		if err != nil {
			t.Fatal(err)
		}
		got, err := service.DecodeIAm(raw, limits)
		if err != nil || got.Segmentation != seg {
			t.Fatalf("seg=%d got=%+v err=%v", seg, got, err)
		}
	}
}

func TestSubscribeCOVCancellationRoundTrip(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeBinaryValue, Instance: 2}
	enc, err := service.EncodeSubscribeCOV(service.SubscribeCOVRequest{
		ProcessIdentifier: 5,
		MonitoredObject:   obj,
		Cancellation:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	dec, err := service.DecodeSubscribeCOV(enc, limits)
	if err != nil || !dec.Cancellation || dec.ProcessIdentifier != 5 {
		t.Fatalf("%+v %v", dec, err)
	}
}

func TestDecodeReadPropertyACKMissingValue(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	p, err := bacnet.AppendContextObjectID(nil, 0, obj)
	if err != nil {
		t.Fatal(err)
	}
	p, err = bacnet.AppendContextUnsigned(p, 1, uint64(bacnet.PropertyPresentValue))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeReadPropertyACK(p, limits); err == nil {
		t.Fatal("expected missing value error")
	}
}

func TestEncodeSubscribeCOVPropertyWithIncrementEncodeOnly(t *testing.T) {
	inc := float32(0.25)
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 3}
	enc, err := service.EncodeSubscribeCOVProperty(service.SubscribeCOVPropertyRequest{
		SubscribeCOVRequest: service.SubscribeCOVRequest{
			ProcessIdentifier: 1,
			MonitoredObject:   obj,
			IssueConfirmed:    true,
			Lifetime:          120,
		},
		Property:     bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
		COVIncrement: &inc,
	})
	if err != nil || len(enc) == 0 {
		t.Fatalf("%v %x", err, enc)
	}
}

func TestEncodeReadPropertyMultipleWithArrayIndex(t *testing.T) {
	idx := uint32(1)
	specs := []service.ReadAccessSpecification{{
		Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		Properties: []bacnet.PropertyReference{{
			Identifier: bacnet.PropertyObjectName,
			ArrayIndex: &idx,
		}},
	}}
	enc, err := service.EncodeReadPropertyMultiple(specs)
	if err != nil || len(enc) == 0 {
		t.Fatalf("%v %x", err, enc)
	}
}

func TestDecodeReadPropertyMissingPropertyIdentifier(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	p, err := bacnet.AppendContextObjectID(nil, 0, bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1})
	if err != nil {
		t.Fatal(err)
	}
	p, err = bacnet.AppendContextUnsigned(p, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeReadProperty(p, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("unexpected tag order: %v", err)
	}
}

func TestDecodeWhoIsDuplicateHighLimit(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	p, err := bacnet.AppendContextUnsigned(nil, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	p, err = bacnet.AppendContextUnsigned(p, 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	p, err = bacnet.AppendContextUnsigned(p, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeWhoIs(p, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("duplicate high: %v", err)
	}
}

func TestDecodeWhoIsMalformedExtra(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()

	tests := []struct {
		name    string
		build   func(t *testing.T) []byte
		wantErr error
	}{
		{
			name:    "ParseSequence truncated",
			build:   func(t *testing.T) []byte { return []byte{0x19} },
			wantErr: bacnet.ErrMalformed,
		},
		{
			name:    "ContextUnsigned fail on low limit",
			build:   func(t *testing.T) []byte { return []byte{0x08, 0x19, 0x01, 0x0A} },
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "low limit overflow",
			build: func(t *testing.T) []byte {
				p, err := bacnet.AppendContextUnsigned(nil, 0, 1<<32)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextUnsigned(p, 1, 10)
				if err != nil {
					t.Fatal(err)
				}
				return p
			},
			wantErr: bacnet.ErrMalformed,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := service.DecodeWhoIs(tc.build(t), limits)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v want %v", err, tc.wantErr)
			}
		})
	}
}

func TestEncodeIAmBadDeviceObjectID(t *testing.T) {
	badDev := bacnet.ObjectIdentifier{Type: bacnet.ObjectType(0x400), Instance: 1}
	if _, err := service.EncodeIAm(service.IAm{
		Device: badDev, MaxAPDULength: 480, Segmentation: 0, VendorID: 1,
	}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("invalid object type: %v", err)
	}
}

func TestDecodeIAmMalformedExtra(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	dev := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}

	valid, err := service.EncodeIAm(service.IAm{
		Device: dev, MaxAPDULength: 480, Segmentation: 0, VendorID: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		raw     []byte
		wantErr error
	}{
		{name: "truncated", raw: valid[:len(valid)-1], wantErr: bacnet.ErrMalformed},
		{name: "MaxAPDU wrong kind", raw: buildIAmRaw(t,
			dev, bacnet.EnumValue(480), bacnet.EnumValue(0), bacnet.UnsignedValue(1),
		), wantErr: bacnet.ErrMalformed},
		{name: "MaxAPDU overflow", raw: buildIAmRaw(t,
			dev, bacnet.UnsignedValue(1<<16), bacnet.EnumValue(0), bacnet.UnsignedValue(1),
		), wantErr: bacnet.ErrMalformed},
		{name: "segmentation wrong kind", raw: buildIAmRaw(t,
			dev, bacnet.UnsignedValue(480), bacnet.UnsignedValue(0), bacnet.UnsignedValue(1),
		), wantErr: bacnet.ErrMalformed},
		{name: "segmentation out of range", raw: buildIAmRaw(t,
			dev, bacnet.UnsignedValue(480), bacnet.EnumValue(4), bacnet.UnsignedValue(1),
		), wantErr: bacnet.ErrMalformed},
		{name: "vendor overflow", raw: buildIAmRaw(t,
			dev, bacnet.UnsignedValue(480), bacnet.EnumValue(0), bacnet.UnsignedValue(1<<16),
		), wantErr: bacnet.ErrMalformed},
		{name: "trailing data", raw: append(valid, 0xFF), wantErr: bacnet.ErrTrailingData},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := service.DecodeIAm(tc.raw, limits)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v want %v", err, tc.wantErr)
			}
		})
	}
}

func TestDiscoveryAPDUErrorWraps(t *testing.T) {
	badDev := bacnet.ObjectIdentifier{Type: bacnet.ObjectType(0x400), Instance: 1}
	if _, err := service.EncodeIAmAPDU(service.IAm{
		Device: badDev, MaxAPDULength: 480, Segmentation: 0, VendorID: 1,
	}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("EncodeIAmAPDU: %v", err)
	}
	low := uint32(1)
	if _, err := service.EncodeWhoIsAPDU(service.WhoIs{LowLimit: &low}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("EncodeWhoIsAPDU: %v", err)
	}
}

func buildIAmRaw(t *testing.T, dev bacnet.ObjectIdentifier, maxAPDU, seg, vendor bacnet.ApplicationValue) []byte {
	t.Helper()
	var raw []byte
	var err error
	raw, err = bacnet.AppendApplicationValue(raw, bacnet.ObjectIDValue(dev))
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendApplicationValue(raw, maxAPDU)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendApplicationValue(raw, seg)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendApplicationValue(raw, vendor)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
