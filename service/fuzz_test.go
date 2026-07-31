// SPDX-License-Identifier: MIT

package service_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func FuzzDecodeWhoIs(f *testing.F) {
	limits := bacnet.DefaultDecodeLimits()
	f.Add([]byte(nil))
	f.Add([]byte{})
	low, high := uint32(1), uint32(100)
	if p, err := service.EncodeWhoIs(service.WhoIs{LowLimit: &low, HighLimit: &high}); err == nil {
		f.Add(p)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = service.DecodeWhoIs(data, limits)
	})
}

func FuzzDecodeIAm(f *testing.F) {
	limits := bacnet.DefaultDecodeLimits()
	f.Add([]byte(nil))
	if p, err := service.EncodeIAm(service.IAm{
		Device:        bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1234},
		MaxAPDULength: 1476,
		Segmentation:  3,
		VendorID:      15,
	}); err == nil {
		f.Add(p)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = service.DecodeIAm(data, limits)
	})
}

func FuzzDecodeReadProperty(f *testing.F) {
	limits := bacnet.DefaultDecodeLimits()
	f.Add([]byte(nil))
	if p, err := service.EncodeReadProperty(service.ReadPropertyRequest{
		Object:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
		Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
	}); err == nil {
		f.Add(p)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = service.DecodeReadProperty(data, limits)
	})
}
