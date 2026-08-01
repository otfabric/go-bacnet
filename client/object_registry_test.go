// SPDX-License-Identifier: MIT

package client

import (
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/internal/clock"
	"github.com/otfabric/go-bacnet/internal/diag"
)

func TestObjectRegistryTTLAndCapacity(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	var events []diag.Event
	reg := newObjectRegistry(diag.Func(func(e diag.Event) { events = append(events, e) }), clk, RegistryOptions{
		MaxObservations: 2,
		ObservationTTL:  time.Minute,
	})
	peer := bip.Endpoint{}
	for i := 0; i < 3; i++ {
		reg.Upsert(ObjectObservation{
			DeviceInstance: uint32(i + 1),
			Object:         bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: uint32(i)},
			Name:           bacnet.CharacterString{Value: "n"},
			ImmediatePeer:  peer,
		})
		clk.Advance(time.Second)
	}
	if len(reg.Observations()) != 2 {
		t.Fatalf("cap=%d", len(reg.Observations()))
	}
	clk.Advance(2 * time.Minute)
	reg.Upsert(ObjectObservation{
		DeviceInstance: 99,
		Object:         bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 99},
		Name:           bacnet.CharacterString{Value: "fresh"},
		ImmediatePeer:  peer,
	})
	obs := reg.Observations()
	if len(obs) != 1 || obs[0].DeviceInstance != 99 {
		t.Fatalf("ttl expire failed: %+v events=%d", obs, len(events))
	}
}
