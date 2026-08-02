// SPDX-License-Identifier: MIT

package service_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func FuzzDecodeWritePropertyMultiple(f *testing.F) {
	limits := bacnet.DefaultDecodeLimits()
	f.Add([]byte(nil))
	if p, err := service.EncodeWritePropertyMultiple([]service.WriteAccessSpecification{{
		Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
		Properties: []service.WritePropertyValue{{
			Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
			Value:    bacnet.RealValue(1.25),
		}},
	}}); err == nil {
		f.Add(p)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = service.DecodeWritePropertyMultiple(data, limits)
	})
}

func FuzzDecodeWritePropertyMultipleError(f *testing.F) {
	limits := bacnet.DefaultDecodeLimits()
	f.Add([]byte(nil))
	if p, err := service.EncodeWritePropertyMultipleError(service.WritePropertyMultipleError{
		Class: 2, Code: 2,
		FirstFailed:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
		FirstProperty: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
	}); err == nil {
		f.Add(p)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = service.DecodeWritePropertyMultipleError(data, limits)
	})
}
