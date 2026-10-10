//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/client"
)

// Discover with a real broadcast: the client is bound to port 47808 on the
// peer's network and sends its Who-Is to the subnet's broadcast address and
// to 255.255.255.255. The peers differ in which of the two they receive
// (bacnet-stack only the directed one, BACnet4J only the limited one), which
// is why both are sent; every other discovery test uses a unicast Who-Is and
// cannot see this.
//
// The device must come back with the address it really has, and
// DeviceObservation.Target must reach it.
func TestBroadcastDiscover(t *testing.T) {
	for _, p := range allPeers() {
		t.Run(p.name, func(t *testing.T) {
			if p.name == "bacpypes3" {
				// Up to adapter 0.9.0 the BACpypes3 device was started without
				// a subnet prefix and so without a broadcast address.
				requireAdapterVersion(t, p.image, "0.10.0")
			}
			dev := loadDeviceFixture(t)
			peer := startPeer(t, p.image, p.name)
			if peer.assertedByReexec {
				return
			}
			c := newDiscoveryClient(t)
			ctx, cancel := withTimeout(t, 15*time.Second)
			defer cancel()

			found, err := c.Discover(ctx, client.DiscoveryOptions{Window: 3 * time.Second})
			if err != nil {
				t.Fatalf("Discover: %v", err)
			}
			var obs *client.DeviceObservation
			for i := range found {
				if found[i].Instance == dev.DeviceInstance {
					obs = &found[i]
				}
			}
			if obs == nil {
				t.Fatalf("device %d did not answer the broadcast Who-Is; found %+v", dev.DeviceInstance, found)
			}
			if !obs.Address.Equal(peer.target.Address) {
				t.Fatalf("discovered at %v, the peer is %v", obs.Address, peer.target.Address)
			}
			if obs.Target().Endpoint != peer.target.Endpoint {
				t.Fatalf("Target() endpoint %v, the peer is %v", obs.Target().Endpoint, peer.target.Endpoint)
			}
			val, err := c.ReadProperty(ctx, obs.Target(), obs.DeviceID(), bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName})
			if err != nil {
				t.Fatalf("ReadProperty through the discovered target: %v", err)
			}
			if got := characterString(t, val); got != dev.DeviceName {
				t.Fatalf("object-name %q, want %q", got, dev.DeviceName)
			}
		})
	}
}
