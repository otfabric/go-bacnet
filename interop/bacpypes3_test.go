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
)

func TestBACpypes3WhoIsIAm(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startPeer(t, getEnv("BACPYPES3_IMAGE", defaultBACpypes3Image), "bacpypes3")
	if peer.assertedByReexec {
		return
	}
	c := newDiscoveryClient(t)

	// Directed Who-Is on the isolated docker network; client binds :47808 so
	// either unicast or broadcast I-Am is receivable.
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

func TestBACpypes3ReadDeviceObjectName(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startPeer(t, getEnv("BACPYPES3_IMAGE", defaultBACpypes3Image), "bacpypes3")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	val, err := readPropertyRetry(t, ctx, c, peer.target,
		bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: dev.DeviceInstance},
		bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName},
	)
	if err != nil {
		t.Fatalf("ReadProperty object-name: %v", err)
	}
	if name := characterString(t, val); name != dev.DeviceName {
		t.Fatalf("object-name=%q, want %q", name, dev.DeviceName)
	}
}

func TestBACpypes3ReadAnalogValue(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startPeer(t, getEnv("BACPYPES3_IMAGE", defaultBACpypes3Image), "bacpypes3")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)

	var want float64 = 21.5
	var avInst uint32 = 1
	for _, o := range dev.Objects {
		if o.Type == "analog-value" {
			avInst = o.Instance
			if f, ok := o.PresentValue.(float64); ok {
				want = f
			}
			break
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	val, err := readPropertyRetry(t, ctx, c, peer.target,
		bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: avInst},
		bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
	)
	if err != nil {
		t.Fatalf("ReadProperty AV present-value: %v", err)
	}
	f, err := bacnet.AsReal(val)
	if err != nil {
		t.Fatalf("AsReal: %v", err)
	}
	if math.Abs(float64(f)-want) > 0.01 {
		t.Fatalf("present-value=%v, want %v", f, want)
	}
}

func readPropertyRetry(t *testing.T, ctx context.Context, c *client.Client, target client.Target, obj bacnet.ObjectIdentifier, prop bacnet.PropertyReference) (bacnet.ApplicationValue, error) {
	t.Helper()
	var lastErr error
	for {
		val, err := c.ReadProperty(ctx, target, obj, prop)
		if err == nil {
			return val, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return bacnet.ApplicationValue{}, lastErr
		}
		select {
		case <-ctx.Done():
			return bacnet.ApplicationValue{}, lastErr
		case <-time.After(100 * time.Millisecond):
		}
	}
}
