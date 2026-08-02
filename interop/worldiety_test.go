//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/service"
)

func worldietyImage() string {
	return getEnv("WORLDIETY_IMAGE", defaultWorldietyImage)
}

func TestWorldietyWhoIsIAm(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startPeer(t, worldietyImage(), "worldiety")
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
	for {
		for _, obs := range c.Devices() {
			if obs.Instance == dev.DeviceInstance {
				if !obs.Capabilities.MaxAPDULengthAccepted.Known {
					t.Fatalf("I-Am missing MaxAPDULengthAccepted: %#v", obs.Capabilities)
				}
				if !obs.Capabilities.Segmentation.Known {
					t.Fatalf("I-Am missing Segmentation: %#v", obs.Capabilities)
				}
				// segmented-both = 0
				if obs.Capabilities.Segmentation.Value != 0 {
					t.Fatalf("Segmentation=%d, want segmented-both(0)", obs.Capabilities.Segmentation.Value)
				}
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no I-Am for device %d; devices=%v", dev.DeviceInstance, c.Devices())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestWorldietyReadDeviceObjectName(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startPeer(t, worldietyImage(), "worldiety")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 8*time.Second)
	defer cancel()

	val, err := c.ReadProperty(ctx, peer.target, deviceObject(dev),
		bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName})
	if err != nil {
		t.Fatalf("ReadProperty: %v", err)
	}
	if characterString(t, val) != dev.DeviceName {
		t.Fatalf("object-name=%q, want %q", characterString(t, val), dev.DeviceName)
	}
}

func TestWorldietyReadAnalogValuePresentValue(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startPeer(t, worldietyImage(), "worldiety")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 8*time.Second)
	defer cancel()

	av, want := analogValueObject(dev)
	val, err := c.ReadProperty(ctx, peer.target, av, bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue})
	if err != nil {
		t.Fatalf("ReadProperty: %v", err)
	}
	got, err := bacnet.AsReal(val)
	if err != nil {
		t.Fatalf("AsReal: %v", err)
	}
	if math.Abs(float64(got)-float64(want)) > 0.01 {
		t.Fatalf("present-value=%v, want %v", got, want)
	}
}

func TestWorldietyReadPropertyUnknownPropertyError(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startPeer(t, worldietyImage(), "worldiety")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 8*time.Second)
	defer cancel()

	_, err := c.ReadProperty(ctx, peer.target, deviceObject(dev), bacnet.PropertyReference{Identifier: unknownPropertyID})
	if err == nil {
		t.Fatal("expected Error PDU for unknown property")
	}
	assertErrorUnknownProperty(t, err)
}

func TestWorldietyReadPropertyMultiple(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startPeer(t, worldietyImage(), "worldiety")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 8*time.Second)
	defer cancel()

	results, err := c.ReadPropertyMultiple(ctx, peer.target, []service.ReadAccessSpecification{{
		Object: deviceObject(dev),
		Properties: []bacnet.PropertyReference{
			{Identifier: bacnet.PropertyObjectName},
			{Identifier: bacnet.PropertyObjectIdentifier},
		},
	}})
	if err != nil {
		t.Fatalf("ReadPropertyMultiple: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("objects=%d, want 1", len(results))
	}
	name := findPropertyValue(t, results[0], bacnet.PropertyObjectName)
	if characterString(t, name) != dev.DeviceName {
		t.Fatalf("object-name=%q, want %q", characterString(t, name), dev.DeviceName)
	}
}

func TestWorldietyWritePropertyReadbackReset(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startPeer(t, worldietyImage(), "worldiety")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 15*time.Second)
	defer cancel()

	av, baseline := analogValueObject(dev)
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	prio := uint8(8)
	written := float32(42.25)

	if err := c.WriteProperty(ctx, peer.target, av, prop, bacnet.RealValue(written), &prio); err != nil {
		t.Fatalf("WriteProperty: %v", err)
	}
	val, err := c.ReadProperty(ctx, peer.target, av, prop)
	if err != nil {
		t.Fatalf("ReadProperty after write: %v", err)
	}
	got, err := bacnet.AsReal(val)
	if err != nil {
		t.Fatalf("AsReal: %v", err)
	}
	if math.Abs(float64(got)-float64(written)) > 0.01 {
		t.Fatalf("present-value=%v, want %v", got, written)
	}
	if err := c.WriteProperty(ctx, peer.target, av, prop, bacnet.RealValue(baseline), &prio); err != nil {
		t.Fatalf("WriteProperty restore: %v", err)
	}
}

func TestWorldietyWritePropertyMultipleReadbackReset(t *testing.T) {
	runWritePropertyMultipleReadbackReset(t, worldietyImage(), "worldiety")
}

func TestWorldietyWhoHasIHave(t *testing.T) {
	runWhoHasIHave(t, worldietyImage(), "worldiety")
}

func TestWorldietyReadRangeByPosition(t *testing.T) {
	runReadRangeByPosition(t, worldietyImage(), "worldiety")
}

func TestWorldietySegmentedWritePropertyMultipleSend(t *testing.T) {
	// Worldiety (3cb2aa80) omits service choice on confirmed-request continuation
	// segments; go-bacnet sends choice on every segment (BACpypes3/BACnet4J agree).
	// Mis-parse of continuation payloads yields Error rather than SimpleACK.
	t.Skip("blocker B6: Worldiety omits service-choice on confirmed-request segments >0; see bacnet-interop/BLOCKERS.md")
}

func TestWorldietySegmentedReadPropertyMultiple(t *testing.T) {
	// Worldiety encodes ComplexACK continuation segments without service choice;
	// go-bacnet treats a missing/mismatched choice as ErrProtocolViolation.
	t.Skip("blocker B6: Worldiety omits service-choice on ComplexACK segments >0; see bacnet-interop/BLOCKERS.md")
}
