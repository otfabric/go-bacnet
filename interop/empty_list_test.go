//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

// Device_Address_Binding of a device that has bound no other device is an
// empty list: an empty propertyValue on the wire. It must read as a value
// without elements, alone and next to other properties in one
// ReadPropertyMultiple. (Up to v0.3.0 the client refused it as malformed.)
func TestReadEmptyListProperty(t *testing.T) {
	const deviceAddressBinding bacnet.PropertyIdentifier = 30
	forPeers(t, []interopPeer{stackPeer(), bacpypes3Peer(), bacnet4jPeer()}, func(t *testing.T, dev deviceFixture, peer *peerHandle) {
		c := newClient(t)
		ctx, cancel := withTimeout(t, 10*time.Second)
		defer cancel()
		ref := bacnet.PropertyReference{Identifier: deviceAddressBinding}

		val, err := c.ReadProperty(ctx, peer.target, deviceObject(dev), ref)
		if err != nil {
			t.Fatalf("ReadProperty device-address-binding: %v", err)
		}
		if val.Kind != bacnet.ValueConstructed || len(val.Elements) != 0 {
			t.Fatalf("device-address-binding = %+v, want an empty list", val)
		}

		results, err := c.ReadPropertyMultiple(ctx, peer.target, []service.ReadAccessSpecification{{
			Object:     deviceObject(dev),
			Properties: []bacnet.PropertyReference{{Identifier: bacnet.PropertyObjectName}, ref},
		}})
		if err != nil {
			t.Fatalf("ReadPropertyMultiple with an empty list: %v", err)
		}
		if got := characterString(t, findPropertyValue(t, results[0], bacnet.PropertyObjectName)); got != dev.DeviceName {
			t.Fatalf("object-name %q next to the empty list, want %q", got, dev.DeviceName)
		}
		if list := findPropertyValue(t, results[0], deviceAddressBinding); list.Kind != bacnet.ValueConstructed || len(list.Elements) != 0 {
			t.Fatalf("device-address-binding in RPM = %+v, want an empty list", list)
		}
	})
}
