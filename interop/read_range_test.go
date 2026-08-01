//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestBacnetStackReadRangeByPosition(t *testing.T) {
	runReadRangeByPosition(t, getEnv("BACNET_STACK_IMAGE", defaultStackImage), "bacnet-stack")
}

func TestBACnet4JReadRangeByPosition(t *testing.T) {
	runReadRangeByPosition(t, getEnv("BACNET4J_IMAGE", defaultBACnet4JImage), "bacnet4j")
}

func runReadRangeByPosition(t *testing.T, image, name string) {
	t.Helper()
	dev := loadDeviceFixture(t)
	tl, ok := trendLogObject(dev)
	if !ok {
		failOrSkip(t, "device fixture has no trend-log (need device-baseline-v2)")
	}
	peer := startPeer(t, image, name)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 15*time.Second)
	defer cancel()

	ack, err := c.ReadRange(ctx, peer.target, service.ReadRangeRequest{
		Object:         tl,
		Property:       bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer},
		By:             service.ReadRangeByPosition,
		ReferenceIndex: 1,
		Count:          2,
	})
	if err != nil {
		t.Fatalf("ReadRange: %v", err)
	}
	if ack.Object != tl {
		t.Fatalf("ACK object=%v want %v", ack.Object, tl)
	}
	if ack.ItemCount < 1 {
		t.Fatalf("ItemCount=%d, want >= 1", ack.ItemCount)
	}
	if len(ack.ItemData) == 0 {
		t.Fatal("ItemData empty")
	}
}
