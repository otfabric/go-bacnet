// SPDX-License-Identifier: MIT

package service_test

import (
	"fmt"
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
	var (
		wire []byte
		err  error
	)
	b.ReportAllocs()
	for b.Loop() {
		if wire, err = service.EncodeReadPropertyACK(ack); err != nil {
			b.Fatal(err)
		}
	}
	if len(wire) == 0 {
		b.Fatal("empty encoding")
	}
}

func BenchmarkDecodeReadPropertyACK(b *testing.B) {
	wire, err := service.EncodeReadPropertyACK(benchACK())
	if err != nil {
		b.Fatal(err)
	}
	limits := bacnet.DefaultDecodeLimits()
	var ack service.ReadPropertyACK
	b.ReportAllocs()
	b.SetBytes(int64(len(wire)))
	for b.Loop() {
		if ack, err = service.DecodeReadPropertyACK(wire, limits); err != nil {
			b.Fatal(err)
		}
	}
	if ack.Object.Instance != 1234 {
		b.Fatalf("decoded %+v", ack)
	}
}

// benchRPMElementsPerObject is what one object of benchRPMACK counts against
// DecodeLimits.MaxElements: the object identifier, the list of results, the
// property identifier, the property value wrapper and the value.
const benchRPMElementsPerObject = 5

// benchRPMACK is the payload of a ReadPropertyMultiple ACK with Present_Value
// (REAL) of the given number of analog inputs.
func benchRPMACK(b *testing.B, objects int) []byte {
	b.Helper()
	results := make([]service.ReadAccessResult, objects)
	for i := range results {
		results[i] = service.ReadAccessResult{
			Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogInput, Instance: uint32(i)},
			Properties: []service.PropertyResult{{
				Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
				Value:    bacnet.RealValue(float32(i) / 2),
			}},
		}
	}
	wire, err := service.EncodeReadPropertyMultipleACK(results)
	if err != nil {
		b.Fatal(err)
	}
	return wire
}

// BenchmarkDecodeReadPropertyMultipleACK decodes ACKs of growing size; the
// largest holds as many objects as the default element limit admits.
func BenchmarkDecodeReadPropertyMultipleACK(b *testing.B) {
	limits := bacnet.DefaultDecodeLimits()
	for _, objects := range []int{1, 16, 256, limits.MaxElements / benchRPMElementsPerObject} {
		b.Run(fmt.Sprintf("objects=%d", objects), func(b *testing.B) {
			wire := benchRPMACK(b, objects)
			var (
				results []service.ReadAccessResult
				err     error
			)
			b.ReportAllocs()
			b.SetBytes(int64(len(wire)))
			for b.Loop() {
				if results, err = service.DecodeReadPropertyMultipleACK(wire, limits); err != nil {
					b.Fatal(err)
				}
			}
			if len(results) != objects {
				b.Fatalf("decoded %d objects, want %d", len(results), objects)
			}
		})
	}
}
