// SPDX-License-Identifier: MIT

package service_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func FuzzDecodeWhoHas(f *testing.F) {
	limits := bacnet.DefaultDecodeLimits()
	f.Add([]byte(nil))
	name := bacnet.CharacterString{Value: "AV-1"}
	if p, err := service.EncodeWhoHas(service.WhoHas{Name: &name}); err == nil {
		f.Add(p)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = service.DecodeWhoHas(data, limits)
	})
}

func FuzzDecodeIHave(f *testing.F) {
	limits := bacnet.DefaultDecodeLimits()
	f.Add([]byte(nil))
	if p, err := service.EncodeIHave(service.IHave{
		Device: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
		Name:   bacnet.CharacterString{Value: "AV-1"},
	}); err == nil {
		f.Add(p)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = service.DecodeIHave(data, limits)
	})
}
