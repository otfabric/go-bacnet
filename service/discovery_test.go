// SPDX-License-Identifier: MIT

package service_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestWhoIsIAmRoundTrip(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	payload, err := service.EncodeWhoIs(service.WhoIs{})
	if err != nil || len(payload) != 0 {
		t.Fatalf("unrestricted Who-Is: %v %x", err, payload)
	}
	low, high := uint32(1), uint32(100)
	payload, err = service.EncodeWhoIs(service.WhoIs{LowLimit: &low, HighLimit: &high})
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeWhoIs(payload, limits)
	if err != nil || got.LowLimit == nil || *got.LowLimit != 1 || *got.HighLimit != 100 {
		t.Fatalf("Who-Is decode %#v err=%v", got, err)
	}

	iam := service.IAm{
		Device:        bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1234},
		MaxAPDULength: 1476,
		Segmentation:  3,
		VendorID:      15,
	}
	p, err := service.EncodeIAm(iam)
	if err != nil {
		t.Fatal(err)
	}
	out, err := service.DecodeIAm(p, limits)
	if err != nil {
		t.Fatal(err)
	}
	if out.Device.Instance != 1234 || out.MaxAPDULength != 1476 || out.VendorID != 15 {
		t.Fatalf("I-Am %#v", out)
	}
}
