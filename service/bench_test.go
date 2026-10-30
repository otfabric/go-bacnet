// SPDX-License-Identifier: MIT

package service_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func benchACK() service.ReadPropertyACK {
	return service.ReadPropertyACK{
		Object:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1234},
		Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
		Value:    bacnet.RealValue(21.5),
	}
}

func BenchmarkEncodeReadPropertyACK(b *testing.B) {
	ack := benchACK()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := service.EncodeReadPropertyACK(ack); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodeReadPropertyACK(b *testing.B) {
	wire, err := service.EncodeReadPropertyACK(benchACK())
	if err != nil {
		b.Fatal(err)
	}
	limits := bacnet.DefaultDecodeLimits()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := service.DecodeReadPropertyACK(wire, limits); err != nil {
			b.Fatal(err)
		}
	}
}
