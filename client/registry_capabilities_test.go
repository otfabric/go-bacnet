// SPDX-License-Identifier: MIT

package client

import (
	"net/netip"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/internal/diag"
)

func TestDeviceCapabilitiesHelpers(t *testing.T) {
	var caps DeviceCapabilities
	caps.SetIAmFields(480, 3, 999)
	if !caps.MaxAPDULengthAccepted.Known || caps.MaxAPDULengthAccepted.Value != 480 {
		t.Fatalf("max APDU %#v", caps.MaxAPDULengthAccepted)
	}
	if caps.MaxAPDUOr(1476) != 480 {
		t.Fatal("MaxAPDUOr should return known value")
	}

	caps = DeviceCapabilities{}
	caps.EnsureFallbackMaxAPDU(206)
	if !caps.MaxAPDULengthAccepted.Known || caps.MaxAPDULengthAccepted.Source != CapabilityConservativeFallback {
		t.Fatalf("fallback %#v", caps.MaxAPDULengthAccepted)
	}
	if caps.MaxAPDUOr(480) != 206 {
		t.Fatal("MaxAPDUOr fallback")
	}
}

func TestRegistryUpsertResolveByInstance(t *testing.T) {
	reg := newRegistry(diag.Discard{}, nil, RegistryOptions{})
	peer := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.2:47808"))
	addr := bacnet.LocalStation(bacnet.MustMAC([]byte{10, 0, 0, 2, 0xBA, 0xC0}))

	caps := DeviceCapabilities{}
	caps.SetIAmFields(1024, 0, 42)
	obs := DeviceObservation{
		Instance:      55,
		Address:       addr,
		ImmediatePeer: peer,
		LastSeen:      time.Unix(0, 0).UTC(),
		Capabilities:  caps,
	}
	reg.Upsert(obs)

	all := reg.Observations()
	if len(all) != 1 || all[0].Instance != 55 {
		t.Fatalf("Observations %#v", all)
	}

	byInst := reg.ByInstance(55)
	if len(byInst) != 1 || byInst[0].Instance != 55 {
		t.Fatalf("ByInstance %#v", byInst)
	}

	got, ok := reg.ResolveCapabilities(Target{Address: addr, Endpoint: peer})
	if !ok {
		t.Fatal("ResolveCapabilities missed observation")
	}
	if got.MaxAPDUOr(480) != 1024 {
		t.Fatalf("resolved max APDU %d", got.MaxAPDUOr(480))
	}

	wrongPeer := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.99:47808"))
	if _, ok := reg.ResolveCapabilities(Target{Address: addr, Endpoint: wrongPeer}); ok {
		t.Fatal("ResolveCapabilities should require matching peer")
	}
}

func TestRegistryIAmViaClientDevices(t *testing.T) {
	env := newVirtualPair(t)
	// Reuse TestDiscoverIAmVirtual pattern through registry snapshot.
	caps := DeviceCapabilities{}
	caps.SetIAmFields(480, 0, 1)
	env.Client.reg.Upsert(DeviceObservation{
		Instance:      9,
		Address:       env.Target.Address,
		ImmediatePeer: env.Peer,
		LastSeen:      env.Clk.Now(),
		Capabilities:  caps,
	})
	devs := env.Client.Devices()
	if len(devs) != 1 || devs[0].Instance != 9 {
		t.Fatalf("Devices %#v", devs)
	}
}

func TestRegistryCapabilityMergePrecedence(t *testing.T) {
	reg := newRegistry(diag.Discard{}, nil, RegistryOptions{})
	peer := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.2:47808"))
	addr := bacnet.LocalStation(bacnet.MustMAC([]byte{10, 0, 0, 2, 0xBA, 0xC0}))
	base := DeviceObservation{
		Instance:      77,
		Address:       addr,
		ImmediatePeer: peer,
		LastSeen:      time.Unix(0, 0).UTC(),
	}

	iam := DeviceCapabilities{}
	iam.SetIAmFields(480, 1, 10)
	base.Capabilities = iam
	reg.Upsert(base)

	deviceObj := DeviceCapabilities{}
	deviceObj.MaxAPDULengthAccepted = Capability[uint16]{Value: 1024, Known: true, Source: CapabilityFromDeviceObject}
	deviceObj.ProtocolVersion = Capability[uint8]{Value: 1, Known: true, Source: CapabilityFromDeviceObject}
	base.Capabilities = deviceObj
	reg.Upsert(base)

	got, ok := reg.ResolveCapabilities(Target{Address: addr, Endpoint: peer})
	if !ok || got.MaxAPDUOr(480) != 1024 {
		t.Fatalf("device object max APDU %#v", got.MaxAPDULengthAccepted)
	}
	if !got.ProtocolVersion.Known || got.ProtocolVersion.Value != 1 {
		t.Fatalf("protocol version %#v", got.ProtocolVersion)
	}
	if got.VendorID.Value != 10 || got.VendorID.Source != CapabilityFromIAm {
		t.Fatalf("I-Am vendor should remain %#v", got.VendorID)
	}

	// Lower-rank I-Am refresh must not clobber device-object max APDU.
	iamRefresh := DeviceCapabilities{}
	iamRefresh.SetIAmFields(206, 2, 99)
	base.Capabilities = iamRefresh
	reg.Upsert(base)
	got, _ = reg.ResolveCapabilities(Target{Address: addr, Endpoint: peer})
	if got.MaxAPDUOr(480) != 1024 {
		t.Fatalf("I-Am must not downgrade max APDU: %#v", got.MaxAPDULengthAccepted)
	}

	override := DeviceCapabilities{}
	override.MaxAPDULengthAccepted = Capability[uint16]{Value: 1476, Known: true, Source: CapabilityUserOverride}
	base.Capabilities = override
	reg.Upsert(base)
	got, _ = reg.ResolveCapabilities(Target{Address: addr, Endpoint: peer})
	if got.MaxAPDUOr(480) != 1476 || got.MaxAPDULengthAccepted.Source != CapabilityUserOverride {
		t.Fatalf("user override %#v", got.MaxAPDULengthAccepted)
	}
}

func TestRegistryDuplicateInstanceDiagnostic(t *testing.T) {
	var events []diag.Event
	reg := newRegistry(diag.Func(func(e diag.Event) { events = append(events, e) }), nil, RegistryOptions{})
	peerA := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.2:47808"))
	peerB := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.3:47808"))
	addrA := bacnet.LocalStation(bacnet.MustMAC([]byte{10, 0, 0, 2, 0xBA, 0xC0}))
	addrB := bacnet.LocalStation(bacnet.MustMAC([]byte{10, 0, 0, 3, 0xBA, 0xC0}))

	caps := DeviceCapabilities{}
	caps.SetIAmFields(480, 0, 1)
	reg.Upsert(DeviceObservation{Instance: 5, Address: addrA, ImmediatePeer: peerA, Capabilities: caps})
	reg.Upsert(DeviceObservation{Instance: 5, Address: addrB, ImmediatePeer: peerB, Capabilities: caps})

	if len(events) != 1 || events[0].Kind != diag.KindDuplicateInstance {
		t.Fatalf("events %#v", events)
	}
	if len(reg.ByInstance(5)) != 2 {
		t.Fatalf("ByInstance %#v", reg.ByInstance(5))
	}
}

func TestRegistryResolveWithOrigin(t *testing.T) {
	reg := newRegistry(diag.Discard{}, nil, RegistryOptions{})
	origin := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.1:47808"))
	peer := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.2:47808"))
	remote := bacnet.RemoteStation(2, bacnet.MustMAC([]byte{10, 0, 0, 2, 0xBA, 0xC0}))
	caps := DeviceCapabilities{}
	caps.SetIAmFields(480, 0, 1)
	reg.Upsert(DeviceObservation{
		Instance: 3, Address: remote, Origin: origin, ImmediatePeer: peer,
		Capabilities: caps, LastSeen: time.Unix(0, 0).UTC(),
	})

	if _, ok := reg.ResolveCapabilities(Target{Address: remote, Endpoint: peer, Origin: origin}); !ok {
		t.Fatal("expected match with origin")
	}
	wrongOrigin := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.99:47808"))
	if _, ok := reg.ResolveCapabilities(Target{Address: remote, Endpoint: peer, Origin: wrongOrigin}); ok {
		t.Fatal("origin mismatch should not resolve")
	}
}
