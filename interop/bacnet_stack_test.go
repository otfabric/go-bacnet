//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"context"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/client"
)

func TestBacnetStackWhoIsIAm(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startPeer(t, getEnv("BACNET_STACK_IMAGE", defaultStackImage), "bacnet-stack")
	if peer.assertedByReexec {
		return
	}
	c := newDiscoveryClient(t)

	// Directed Who-Is reaches the peer on Docker bridge networks where
	// 255.255.255.255 is not delivered between containers. bacserv answers
	// with a broadcast I-Am; newDiscoveryClient binds :47808 so that reply
	// is receivable.
	low, high := dev.DeviceInstance, dev.DeviceInstance
	if err := c.SendWhoIs(context.Background(), peer.endpoint, false, client.DiscoveryOptions{
		LowLimit:  &low,
		HighLimit: &high,
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
				if !obs.Capabilities.VendorID.Known {
					t.Fatalf("I-Am missing VendorID: %#v", obs.Capabilities)
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

func TestBacnetStackReadDeviceObjectName(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startPeer(t, getEnv("BACNET_STACK_IMAGE", defaultStackImage), "bacnet-stack")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	var lastErr error
	deadline := time.Now().Add(8 * time.Second)
	for {
		val, err := c.ReadProperty(ctx, peer.target,
			bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: dev.DeviceInstance},
			bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName},
		)
		if err == nil {
			name := characterString(t, val)
			if name != dev.DeviceName {
				t.Fatalf("object-name=%q, want %q", name, dev.DeviceName)
			}
			return
		}
		lastErr = err
		if time.Now().After(deadline) || ctx.Err() != nil {
			t.Fatalf("ReadProperty object-name: %v", lastErr)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestBacnetStackReadAnalogValue(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startPeer(t, getEnv("BACNET_STACK_IMAGE", defaultStackImage), "bacnet-stack")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 8*time.Second)
	defer cancel()

	av, want := analogValueObject(dev)
	val, err := c.ReadProperty(ctx, peer.target, av, bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue})
	if err != nil {
		t.Fatalf("ReadProperty AV present-value: %v", err)
	}
	f, err := bacnet.AsReal(val)
	if err != nil {
		t.Fatalf("AsReal: %v", err)
	}
	if float64(f) < float64(want)-0.01 || float64(f) > float64(want)+0.01 {
		t.Fatalf("present-value=%v, want %v", f, want)
	}
}
