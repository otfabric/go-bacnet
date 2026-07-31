//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/client"
)

func TestBACpypes3COVSubscribeNotifyCancel(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startPeer(t, getEnv("BACPYPES3_IMAGE", defaultBACpypes3Image), "bacpypes3")
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

func TestBacnetStackCOVSubscribeNotifyCancel(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startPeer(t, getEnv("BACNET_STACK_IMAGE", defaultStackImage), "bacnet-stack")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()

	obj, baseline := analogValueObject(dev)
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	sub, err := c.SubscribeCOV(ctx, peer.target, obj, client.COVOptions{
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
	written := baseline + 1.5
	if err := c.WriteProperty(ctx, peer.target, obj, prop, bacnet.RealValue(written), &prio); err != nil {
		t.Fatalf("WriteProperty to trigger COV on %v: %v", obj, err)
	}

	note := waitCOVNotification(t, sub, 8*time.Second)
	if note.MonitoredObject != obj {
		t.Fatalf("COV object=%v, want %v", note.MonitoredObject, obj)
	}
	if err := sub.Close(); err != nil {
		t.Fatalf("Subscription.Close: %v", err)
	}
	_ = c.WriteProperty(ctx, peer.target, obj, prop, bacnet.RealValue(baseline), &prio)
}

func TestBACpypes3COVRenew(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startPeer(t, getEnv("BACPYPES3_IMAGE", defaultBACpypes3Image), "bacpypes3")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()

	av, _ := analogValueObject(dev)
	sub, err := c.SubscribeCOV(ctx, peer.target, av, client.COVOptions{
		Lifetime:       4,
		IssueConfirmed: false,
		BufferSize:     16,
	})
	if err != nil {
		t.Fatalf("SubscribeCOV: %v", err)
	}
	defer func() { _ = sub.Close() }()

	// Lifetime/2 renew publishes Renewing then Active. Wait for Renewing.
	deadline := time.After(10 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for SubscriptionRenewing")
		case ev, ok := <-sub.Events():
			if !ok {
				t.Fatal("subscription closed before renew")
			}
			if ev.State == client.SubscriptionRenewing {
				return
			}
			if ev.State == client.SubscriptionExpired || ev.State == client.SubscriptionDegraded {
				t.Fatalf("unexpected state before renew: %v err=%v", ev.State, ev.Err)
			}
		}
	}
}
