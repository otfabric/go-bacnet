// SPDX-License-Identifier: MIT

package client

import (
	"net/netip"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/internal/clock"
	"github.com/otfabric/go-bacnet/internal/diag"
)

func TestRegistryMaxObservationsEvictsLRU(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	var events []diag.Event
	reg := newRegistry(diag.Func(func(e diag.Event) { events = append(events, e) }), clk, RegistryOptions{
		MaxObservations:     2,
		MaxPathsPerInstance: 8,
		ObservationTTL:      -1,
	})

	upsert := func(inst uint32, host string) {
		peer := bip.NewEndpoint(netip.MustParseAddrPort(host + ":47808"))
		mac := peer.Addr.Addr().As4()
		addr := bacnet.LocalStation(bacnet.MustMAC([]byte{mac[0], mac[1], mac[2], mac[3], 0xBA, 0xC0}))
		reg.Upsert(DeviceObservation{Instance: inst, Address: addr, ImmediatePeer: peer})
		clk.Advance(time.Second)
	}

	upsert(1, "10.0.0.1")
	upsert(2, "10.0.0.2")
	upsert(3, "10.0.0.3")

	if reg.Len() != 2 {
		t.Fatalf("Len=%d want 2", reg.Len())
	}
	if len(reg.ByInstance(1)) != 0 {
		t.Fatal("instance 1 should have been evicted (LRU)")
	}
	if len(reg.ByInstance(2)) != 1 || len(reg.ByInstance(3)) != 1 {
		t.Fatalf("want instances 2 and 3 retained")
	}
	found := false
	for _, e := range events {
		if e.Kind == diag.KindRegistryEviction {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected registry eviction diagnostic, got %#v", events)
	}
}

func TestRegistryMaxPathsPerInstance(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	reg := newRegistry(diag.Discard{}, clk, RegistryOptions{
		MaxObservations:     64,
		MaxPathsPerInstance: 2,
		ObservationTTL:      -1,
	})

	for i, host := range []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"} {
		peer := bip.NewEndpoint(netip.MustParseAddrPort(host + ":47808"))
		mac := peer.Addr.Addr().As4()
		addr := bacnet.LocalStation(bacnet.MustMAC([]byte{mac[0], mac[1], mac[2], mac[3], 0xBA, 0xC0}))
		reg.Upsert(DeviceObservation{Instance: 9, Address: addr, ImmediatePeer: peer})
		clk.Advance(time.Second)
		_ = i
	}
	if len(reg.ByInstance(9)) != 2 {
		t.Fatalf("ByInstance len=%d want 2", len(reg.ByInstance(9)))
	}
}

func TestRegistryObservationTTL(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	reg := newRegistry(diag.Discard{}, clk, RegistryOptions{
		MaxObservations:     64,
		MaxPathsPerInstance: 8,
		ObservationTTL:      5 * time.Minute,
	})
	peer := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.2:47808"))
	addr := bacnet.LocalStation(bacnet.MustMAC([]byte{10, 0, 0, 2, 0xBA, 0xC0}))
	reg.Upsert(DeviceObservation{Instance: 1, Address: addr, ImmediatePeer: peer})
	clk.Advance(6 * time.Minute)
	// Trigger expiry via another upsert.
	peer2 := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.3:47808"))
	addr2 := bacnet.LocalStation(bacnet.MustMAC([]byte{10, 0, 0, 3, 0xBA, 0xC0}))
	reg.Upsert(DeviceObservation{Instance: 2, Address: addr2, ImmediatePeer: peer2})
	if len(reg.ByInstance(1)) != 0 {
		t.Fatal("instance 1 should have expired")
	}
	if len(reg.ByInstance(2)) != 1 {
		t.Fatal("instance 2 should remain")
	}
}

func TestObservationsSince(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	reg := newRegistry(diag.Discard{}, clk, RegistryOptions{ObservationTTL: -1})
	peer := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.2:47808"))
	addr := bacnet.LocalStation(bacnet.MustMAC([]byte{10, 0, 0, 2, 0xBA, 0xC0}))
	reg.Upsert(DeviceObservation{Instance: 1, Address: addr, ImmediatePeer: peer})
	since := clk.Now().Add(time.Second)
	clk.Advance(2 * time.Second)
	peer2 := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.3:47808"))
	addr2 := bacnet.LocalStation(bacnet.MustMAC([]byte{10, 0, 0, 3, 0xBA, 0xC0}))
	reg.Upsert(DeviceObservation{Instance: 2, Address: addr2, ImmediatePeer: peer2})
	got := reg.ObservationsSince(since)
	if len(got) != 1 || got[0].Instance != 2 {
		t.Fatalf("ObservationsSince %#v", got)
	}
}
