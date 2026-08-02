//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func deviceBaselineV3Path(t *testing.T) string {
	t.Helper()
	for _, p := range []string{
		filepath.Join("..", "bacnet-interop", "fixtures", "device", "device-baseline-v3.json"),
		filepath.Join("..", "..", "bacnet-interop", "fixtures", "device", "device-baseline-v3.json"),
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Fatal("device-baseline-v3.json not found")
	return ""
}

// TestBACnet4JGetAlarmSummary triggers AV-1 Out_Of_Range then queries GetAlarmSummary.
// BACnet4J v3 enables intrinsic reporting with highLimit=80; COV-multiple remains
// NotImplemented upstream (B3 remainder).
func TestBACnet4JGetAlarmSummary(t *testing.T) {
	t.Setenv("BACNET_DEVICE_FIXTURE", deviceBaselineV3Path(t))
	peer := startPeer(t, bacnet4jImage(), "bacnet4j",
		"DEVICE_FIXTURE_FILE=/fixtures/device/device-baseline-v3.json",
		"FIXTURE=device-baseline-v3",
	)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	av := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	prio := uint8(8)
	if err := c.WriteProperty(ctx, peer.target, av, prop, bacnet.RealValue(90), &prio); err != nil {
		t.Fatalf("WriteProperty AV-1=90: %v", err)
	}

	deadline := time.Now().Add(8 * time.Second)
	var ack service.GetAlarmSummaryACK
	var err error
	for {
		ack, err = c.GetAlarmSummary(ctx, peer.target)
		if err != nil {
			t.Fatalf("GetAlarmSummary: %v", err)
		}
		found := false
		for _, e := range ack.Entries {
			if e.Object.Type == bacnet.ObjectTypeAnalogValue && e.Object.Instance == 1 {
				found = true
				break
			}
		}
		if found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("AV-1 not in alarm summary after trigger; entries=%+v", ack.Entries)
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Restore normal so peer state does not leak across tests (fresh container anyway).
	_ = c.WriteProperty(ctx, peer.target, av, prop, bacnet.RealValue(20), &prio)
}
