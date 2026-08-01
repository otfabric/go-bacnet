//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/service"
)

func TestBACpypes3SegmentedWritePropertyMultipleSend(t *testing.T) {
	runSegmentedWritePropertyMultipleSend(t, getEnv("BACPYPES3_IMAGE", defaultBACpypes3Image), "bacpypes3")
}

func TestBACnet4JSegmentedWritePropertyMultipleSend(t *testing.T) {
	// BACnet4J rejects segmented confirmed requests (reject reason 9 / unsupported
	// confirmed service). PLAN marks BACnet4J segmented receive as a known gap;
	// keep BACpypes3 as the live send evidence peer.
	t.Skip("BACnet4J does not accept segmented confirmed-request receive")
}

// runSegmentedWritePropertyMultipleSend forces client-side segmented confirmed
// request transmission by claiming a small remote MaxAPDU after Who-Is proves
// the peer accepts segmented requests. Restores Description afterward.
func runSegmentedWritePropertyMultipleSend(t *testing.T, image, name string) {
	t.Helper()
	dev := loadDeviceFixture(t)
	peer := startPeer(t, image, name)
	if peer.assertedByReexec {
		return
	}
	c := newDiscoveryClient(t)

	low, high := dev.DeviceInstance, dev.DeviceInstance
	if err := c.SendWhoIs(context.Background(), peer.endpoint, false, client.DiscoveryOptions{
		LowLimit: &low, HighLimit: &high,
	}); err != nil {
		t.Fatalf("SendWhoIs: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var caps client.DeviceCapabilities
	found := false
	for time.Now().Before(deadline) {
		for _, obs := range c.Devices() {
			if obs.Instance == dev.DeviceInstance {
				caps = obs.Capabilities
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
		t.Fatalf("no I-Am for device %d", dev.DeviceInstance)
	}
	if !caps.Segmentation.Known {
		t.Fatal("I-Am missing Segmentation")
	}
	// both(0) or receive(2)
	if caps.Segmentation.Value != 0 && caps.Segmentation.Value != 2 {
		t.Skipf("peer Segmentation=%d does not accept segmented requests", caps.Segmentation.Value)
	}

	av, _ := analogValueObject(dev)
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyDescription}
	baselineVal, err := c.ReadProperty(context.Background(), peer.target, av, prop)
	if err != nil {
		t.Fatalf("ReadProperty Description: %v", err)
	}
	baseline := characterString(t, baselineVal)

	// Force segment sizing below remote capability while keeping peer Segmentation evidence.
	target := peer.target
	target.MaxAPDU = 50

	// Build a payload that exceeds MaxAPDU=50 when encoded as confirmed WPM.
	specs := []service.WriteAccessSpecification{{
		Object: av,
		Properties: []service.WritePropertyValue{{
			Property: prop,
			Value: bacnet.ApplicationValue{
				Kind:      bacnet.ValueCharacterString,
				Character: bacnet.CharacterString{Value: strings.Repeat("x", 80)},
			},
		}},
	}}
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()
	if err := c.WritePropertyMultiple(ctx, target, specs); err != nil {
		t.Fatalf("segmented WritePropertyMultiple: %v", err)
	}

	restore := []service.WriteAccessSpecification{{
		Object: av,
		Properties: []service.WritePropertyValue{{
			Property: prop,
			Value: bacnet.ApplicationValue{
				Kind:      bacnet.ValueCharacterString,
				Character: bacnet.CharacterString{Value: baseline},
			},
		}},
	}}
	if err := c.WritePropertyMultiple(ctx, peer.target, restore); err != nil {
		t.Fatalf("restore Description: %v", err)
	}
}
