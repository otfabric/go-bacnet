//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"context"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/client"
)

func TestBACpypes3RoutedWhoIsRouterReadProperty(t *testing.T) {
	dev := loadDeviceFixture(t)
	// Do not set BACNET_NETWORK here: BACpypes3 reverse-routes more reliably
	// via bip-router return-path assist when it replies as a local station.
	topo := startRoutedTopology(t, getEnv("BACPYPES3_IMAGE", defaultBACpypes3Image), "bacpypes3")
	if topo.assertedByReexec {
		return
	}
	// Bind :47808 so Original-Broadcast I-Am-Router (and unicast replies to
	// the same port) are receivable on docker bridges; ephemeral :0 clients
	// miss broadcasts addressed to UDP/47808.
	c := newDiscoveryClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	remote := topo.remoteNet
	// Directed Who-Is-Router to the known next hop; also local-broadcast so a
	// missed unicast can still be recovered via the router's broadcast reply.
	// Learning the cache is best-effort under Docker Desktop (I-Am-Router UDP
	// is occasionally dropped); ResolveTarget is preferred when present, else
	// explicit next-hop — hard assertion is the routed ReadProperty below.
	// Unit coverage for cache fill lives in client/routing_test.go.
	var target client.Target
	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := c.WhoIsRouterToNetworkAt(ctx, topo.router, false, &remote); err != nil {
			t.Fatalf("WhoIsRouterToNetworkAt: %v", err)
		}
		_ = c.WhoIsRouterToNetwork(ctx, &remote)
		time.Sleep(50 * time.Millisecond)
		resolved, err := c.ResolveTarget(topo.remoteAddress(), bip.Endpoint{})
		if err == nil && resolved.Endpoint.IsValid() && resolved.Endpoint.Equal(topo.router) {
			target = resolved
			target.MaxAPDU = 1476
			t.Log("resolved next-hop from I-Am-Router cache")
			break
		}
		if time.Now().After(deadline) {
			t.Logf("router cache miss for net %d after Who-Is-Router (err=%v); using explicit next-hop", topo.remoteNet, err)
			target = client.Target{
				Address:  topo.remoteAddress(),
				Endpoint: topo.router,
				MaxAPDU:  1476,
			}
			break
		}
		time.Sleep(150 * time.Millisecond)
	}

	// Remote-broadcast Who-Is via router next hop (best-effort discovery aid).
	low, high := dev.DeviceInstance, dev.DeviceInstance
	_ = c.SendWhoIs(ctx, topo.router, false, client.DiscoveryOptions{
		LowLimit:  &low,
		HighLimit: &high,
		Address:   bacnet.RemoteBroadcast(topo.remoteNet),
	})
	iamDeadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(iamDeadline) {
		for _, obs := range c.Devices() {
			if obs.Instance == dev.DeviceInstance && obs.Address.Network() == topo.remoteNet {
				t.Logf("routed I-Am observed for device %d on net %d", obs.Instance, topo.remoteNet)
				goto readProperty
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Logf("routed I-Am not observed (continuing with known remote MAC); devices=%v", c.Devices())

readProperty:
	val, err := readPropertyRetry(t, ctx, c, target,
		bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: dev.DeviceInstance},
		bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName},
	)
	if err != nil {
		t.Fatalf("routed ReadProperty: %v", err)
	}
	if name := characterString(t, val); name != dev.DeviceName {
		t.Fatalf("object-name=%q, want %q", name, dev.DeviceName)
	}
}

func TestBACnetStackRoutedReadProperty(t *testing.T) {
	dev := loadDeviceFixture(t)
	// bacnet-stack has no BACNET_NETWORK; bip-router return-path assist carries
	// the reply after final unicast delivery omits SNET.
	topo := startRoutedTopology(t, getEnv("BACNET_STACK_IMAGE", defaultStackImage), "bacnet-stack")
	if topo.assertedByReexec {
		return
	}
	// Bind :47808 like the BACpypes3 routed test so router broadcasts and
	// unicast replies to the standard port are receivable on docker bridges.
	c := newDiscoveryClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Warm the next hop / return-path (best-effort; hard assert is RP below).
	remote := topo.remoteNet
	_ = c.WhoIsRouterToNetworkAt(ctx, topo.router, false, &remote)
	time.Sleep(100 * time.Millisecond)

	target := client.Target{
		Address:  topo.remoteAddress(),
		Endpoint: topo.router,
		MaxAPDU:  1476,
	}
	val, err := readPropertyRetry(t, ctx, c, target,
		bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: dev.DeviceInstance},
		bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName},
	)
	if err != nil {
		t.Fatalf("routed ReadProperty: %v", err)
	}
	if name := characterString(t, val); name != dev.DeviceName {
		t.Fatalf("object-name=%q, want %q", name, dev.DeviceName)
	}
}

func TestBACpypes3ForeignDeviceWhoIsReadProperty(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startPeer(t, getEnv("BACPYPES3_IMAGE", defaultBACpypes3Image), "bacpypes3", "BACNET_BBMD=1")
	if peer.assertedByReexec {
		return
	}

	c := newClientOpts(t,
		client.WithForeignDevice(client.ForeignDeviceConfig{
			BBMD: peer.endpoint,
			TTL:  60 * time.Second,
		}),
	)

	deadline := time.Now().Add(8 * time.Second)
	for !c.ForeignDeviceRegistered() {
		if time.Now().After(deadline) {
			t.Fatal("foreign-device registration did not succeed")
		}
		time.Sleep(50 * time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	low, high := dev.DeviceInstance, dev.DeviceInstance
	if err := c.SendWhoIs(ctx, peer.endpoint, true, client.DiscoveryOptions{
		LowLimit:  &low,
		HighLimit: &high,
	}); err != nil {
		t.Fatalf("FD SendWhoIs (DBTN): %v", err)
	}

	iamDeadline := time.Now().Add(5 * time.Second)
	found := false
	for time.Now().Before(iamDeadline) {
		for _, obs := range c.Devices() {
			if obs.Instance == dev.DeviceInstance {
				found = true
				break
			}
		}
		if found {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !found {
		t.Fatalf("no I-Am after FD Who-Is; devices=%v", c.Devices())
	}

	val, err := readPropertyRetry(t, ctx, c, peer.target,
		bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: dev.DeviceInstance},
		bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName},
	)
	if err != nil {
		t.Fatalf("FD ReadProperty: %v", err)
	}
	if name := characterString(t, val); name != dev.DeviceName {
		t.Fatalf("object-name=%q, want %q", name, dev.DeviceName)
	}
}
