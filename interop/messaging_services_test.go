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

func deviceBaselineV6Path(t *testing.T) string {
	t.Helper()
	for _, p := range []string{
		filepath.Join("..", "bacnet-interop", "fixtures", "device", "device-baseline-v6.json"),
		filepath.Join("..", "..", "bacnet-interop", "fixtures", "device", "device-baseline-v6.json"),
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Fatal("device-baseline-v6.json not found")
	return ""
}

func TestBACnet4JTimeSynchronization(t *testing.T) {
	t.Setenv("BACNET_DEVICE_FIXTURE", deviceBaselineV6Path(t))
	peer := startPeer(t, bacnet4jImage(), "bacnet4j",
		"DEVICE_FIXTURE_FILE=/fixtures/device/device-baseline-v6.json",
		"FIXTURE=device-baseline-v6",
	)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := c.TimeSynchronization(ctx, peer.target.Endpoint, false, service.TimeSynchronization{
		Date: bacnet.Date{Year: 126, Month: 8, Day: 2, Weekday: 7},
		Time: bacnet.Time{Hour: 12, Minute: 0, Second: 0, Hundredths: 0},
	}); err != nil {
		t.Fatalf("TimeSynchronization: %v", err)
	}
	if err := c.UnconfirmedTextMessage(ctx, peer.target.Endpoint, false, service.TextMessage{
		TextMessageSourceDevice: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		Message:                 "interop-hello",
	}); err != nil {
		t.Fatalf("UnconfirmedTextMessage: %v", err)
	}
}
