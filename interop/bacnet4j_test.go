//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/service"
)

func bacnet4jImage() string {
	return getEnv("BACNET4J_IMAGE", defaultBACnet4JImage)
}

func TestBACnet4JWhoIsIAm(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startPeer(t, bacnet4jImage(), "bacnet4j")
	if peer.assertedByReexec {
		return
	}
	c := newDiscoveryClient(t)

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

func TestBACnet4JReadDeviceObjectName(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startPeer(t, bacnet4jImage(), "bacnet4j")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 8*time.Second)
	defer cancel()

	val, err := readPropertyRetry(t, ctx, c, peer.target, deviceObject(dev),
		bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName})
	if err != nil {
		t.Fatalf("ReadProperty object-name: %v", err)
	}
	if name := characterString(t, val); name != dev.DeviceName {
		t.Fatalf("object-name=%q, want %q", name, dev.DeviceName)
	}
}

func TestBACnet4JReadAnalogValue(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startPeer(t, bacnet4jImage(), "bacnet4j")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 8*time.Second)
	defer cancel()

	av, want := analogValueObject(dev)
	val, err := readPropertyRetry(t, ctx, c, peer.target, av,
		bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue})
	if err != nil {
		t.Fatalf("ReadProperty AV present-value: %v", err)
	}
	f, err := bacnet.AsReal(val)
	if err != nil {
		t.Fatalf("AsReal: %v", err)
	}
	if math.Abs(float64(f)-float64(want)) > 0.01 {
		t.Fatalf("present-value=%v, want %v", f, want)
	}
}

func TestBACnet4JReadPropertyUnknownPropertyError(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startPeer(t, bacnet4jImage(), "bacnet4j")
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

func TestBACnet4JRejectUnrecognizedService(t *testing.T) {
	peer := startPeer(t, bacnet4jImage(), "bacnet4j")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 8*time.Second)
	defer cancel()

	_, err := c.InvokeConfirmed(ctx, peer.target, unrecognizedConfirmedService, nil, client.ConfirmedInvokeOptions{
		SegmentedResponseAccepted: true,
	})
	if err == nil {
		t.Fatal("expected Reject PDU for unrecognized service")
	}
	assertRejectUnrecognized(t, err)
}

func TestBACnet4JReadPropertyMultiple(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startPeer(t, bacnet4jImage(), "bacnet4j")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 8*time.Second)
	defer cancel()

	results, err := c.ReadPropertyMultiple(ctx, peer.target, []service.ReadAccessSpecification{
		{
			Object: deviceObject(dev),
			Properties: []bacnet.PropertyReference{
				{Identifier: bacnet.PropertyObjectName},
				{Identifier: bacnet.PropertyObjectIdentifier},
			},
		},
	})
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

func TestBACnet4JReadPropertyMultiplePartialError(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startPeer(t, bacnet4jImage(), "bacnet4j")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 8*time.Second)
	defer cancel()

	results, err := c.ReadPropertyMultiple(ctx, peer.target, []service.ReadAccessSpecification{
		{
			Object: deviceObject(dev),
			Properties: []bacnet.PropertyReference{
				{Identifier: bacnet.PropertyObjectName},
				{Identifier: unknownPropertyID},
			},
		},
	})
	if err != nil {
		t.Fatalf("ReadPropertyMultiple transaction error: %v", err)
	}
	assertRPMPartialUnknownProperty(t, results, dev)
}

func TestBACnet4JWritePropertyReadbackReset(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startPeer(t, bacnet4jImage(), "bacnet4j")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 15*time.Second)
	defer cancel()

	av, baseline := analogValueObject(dev)
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	prio := uint8(8)
	written := baseline + 2.25
	if err := c.WriteProperty(ctx, peer.target, av, prop, bacnet.RealValue(written), &prio); err != nil {
		t.Fatalf("WriteProperty: %v", err)
	}
	val, err := readPropertyRetry(t, ctx, c, peer.target, av, prop)
	if err != nil {
		t.Fatalf("readback: %v", err)
	}
	f, err := bacnet.AsReal(val)
	if err != nil {
		t.Fatalf("AsReal: %v", err)
	}
	if math.Abs(float64(f)-float64(written)) > 0.01 {
		t.Fatalf("present-value=%v, want %v", f, written)
	}
	if err := c.WriteProperty(ctx, peer.target, av, prop, bacnet.RealValue(baseline), &prio); err != nil {
		t.Fatalf("restore WriteProperty: %v", err)
	}
}

func TestBACnet4JCOVSubscribeNotifyCancel(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startPeer(t, bacnet4jImage(), "bacnet4j")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()

	av, baseline := analogValueObject(dev)
	sub, err := c.SubscribeCOV(ctx, peer.target, av, client.COVOptions{
		Lifetime:       60,
		IssueConfirmed: false,
		BufferSize:     8,
	})
	if err != nil {
		t.Fatalf("SubscribeCOV: %v", err)
	}
	defer func() { _ = sub.Close() }()

	select {
	case ev := <-sub.Events():
		if ev.State != client.SubscriptionActive {
			t.Fatalf("first event state=%v, want Active", ev.State)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for SubscriptionActive")
	}

	prio := uint8(8)
	written := baseline + 3.5
	if err := c.WriteProperty(ctx, peer.target, av, bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}, bacnet.RealValue(written), &prio); err != nil {
		t.Fatalf("WriteProperty to trigger COV: %v", err)
	}

	note := waitCOVNotification(t, sub, 8*time.Second)
	if note.MonitoredObject != av {
		t.Fatalf("COV object=%v, want %v", note.MonitoredObject, av)
	}

	if err := sub.Close(); err != nil {
		t.Fatalf("Subscription.Close (cancel): %v", err)
	}
	_ = c.WriteProperty(ctx, peer.target, av, bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}, bacnet.RealValue(baseline), &prio)
}

func TestBACnet4JSegmentedReadPropertyMultiple(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startPeer(t, bacnet4jImage(), "bacnet4j", "BACNET_MAX_APDU=50")
	if peer.assertedByReexec {
		return
	}
	c := newClientWithAdvertisedMaxAPDU(t, 50)
	ctx, cancel := withTimeout(t, 15*time.Second)
	defer cancel()

	results, err := c.ReadPropertyMultiple(ctx, peer.target, []service.ReadAccessSpecification{
		{
			Object: deviceObject(dev),
			Properties: []bacnet.PropertyReference{
				{Identifier: bacnet.PropertyObjectName},
				{Identifier: bacnet.PropertyObjectIdentifier},
				{Identifier: bacnet.PropertyObjectType},
				{Identifier: bacnet.PropertyVendorIdentifier},
				{Identifier: bacnet.PropertyModelName},
				{Identifier: bacnet.PropertyDescription},
				{Identifier: bacnet.PropertySystemStatus},
				{Identifier: bacnet.PropertyProtocolVersion},
				{Identifier: bacnet.PropertyProtocolRevision},
				{Identifier: bacnet.PropertyMaxAPDULength},
				{Identifier: bacnet.PropertySegmentation},
			},
		},
	})
	if err != nil {
		t.Fatalf("segmented ReadPropertyMultiple: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("objects=%d, want 1", len(results))
	}
	name := findPropertyValue(t, results[0], bacnet.PropertyObjectName)
	if characterString(t, name) != dev.DeviceName {
		t.Fatalf("object-name=%q, want %q", characterString(t, name), dev.DeviceName)
	}
}

func TestBACnet4JRoutedReadProperty(t *testing.T) {
	dev := loadDeviceFixture(t)
	topo := startRoutedTopology(t, bacnet4jImage(), "bacnet4j",
		fmt.Sprintf("BACNET_NETWORK=%d", topologyRemoteNet))
	if topo.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()

	// Explicit next-hop DNET/DADR via bip-router (same shape as bacnet-stack).
	// Who-Is-Router→ResolveTarget coverage lives in the BACpypes3 routed test;
	// BACnet4J under docker can drop the unicast I-Am-Router, so do not gate RP
	// on the router cache here.
	target := client.Target{
		Address:  topo.remoteAddress(),
		Endpoint: topo.router,
		MaxAPDU:  1476,
	}
	val, err := readPropertyRetry(t, ctx, c, target, deviceObject(dev),
		bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName})
	if err != nil {
		t.Fatalf("routed ReadProperty: %v", err)
	}
	if name := characterString(t, val); name != dev.DeviceName {
		t.Fatalf("object-name=%q, want %q", name, dev.DeviceName)
	}
}

func TestBACnet4JForeignDeviceWhoIsReadProperty(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startPeer(t, bacnet4jImage(), "bacnet4j", "BACNET_BBMD=1")
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

	ctx, cancel := withTimeout(t, 10*time.Second)
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

	val, err := readPropertyRetry(t, ctx, c, peer.target, deviceObject(dev),
		bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName})
	if err != nil {
		t.Fatalf("FD ReadProperty: %v", err)
	}
	if name := characterString(t, val); name != dev.DeviceName {
		t.Fatalf("object-name=%q, want %q", name, dev.DeviceName)
	}
}
