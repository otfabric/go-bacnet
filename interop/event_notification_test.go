//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/client"
)

func TestBACpypes3EventNotificationReceive(t *testing.T) {
	runEventNotificationReceive(t, getEnv("BACPYPES3_IMAGE", defaultBACpypes3Image), "bacpypes3")
}

func TestBACnet4JEventNotificationReceive(t *testing.T) {
	runEventNotificationReceive(t, getEnv("BACNET4J_IMAGE", defaultBACnet4JImage), "bacnet4j")
}

func runEventNotificationReceive(t *testing.T, image, name string) {
	t.Helper()
	dev := loadDeviceFixture(t)
	peer := startPeer(t, image, name, "BACNET_EMIT_EVENT=1")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	got := make(chan client.EventNotificationDelivery, 1)
	c.SetEventNotificationHandler(func(d client.EventNotificationDelivery) {
		select {
		case got <- d:
		default:
		}
	})

	ctx, cancel := withTimeout(t, 10*time.Second)
	defer cancel()
	av, _ := analogValueObject(dev)
	// Trigger peer emit (first ReadProperty → UnconfirmedEventNotification to us).
	if _, err := c.ReadProperty(ctx, peer.target, av, bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}); err != nil {
		t.Fatalf("ReadProperty trigger: %v", err)
	}

	select {
	case d := <-got:
		if d.Confirmed {
			t.Fatal("expected unconfirmed EventNotification")
		}
		if d.Notification.InitiatingDevice.Instance != dev.DeviceInstance {
			t.Fatalf("initiating device=%d want %d", d.Notification.InitiatingDevice.Instance, dev.DeviceInstance)
		}
		if d.Notification.ProcessIdentifier != 1 {
			t.Fatalf("processIdentifier=%d want 1", d.Notification.ProcessIdentifier)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("timed out waiting for peer EventNotification")
	}
}
