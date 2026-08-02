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

// Peers with native GetAlarmSummary + intrinsic Out_Of_Range on AV-1 (v3).
func alarmSummaryPeers() []struct {
	name  string
	image string
} {
	return []struct {
		name  string
		image string
	}{
		{"bacnet4j", bacnet4jImage()},
		{"bacnet-stack", getEnv("BACNET_STACK_IMAGE", defaultStackImage)},
	}
}

func runGetAlarmSummaryOutOfRange(t *testing.T, image, adapter string) {
	t.Helper()
	t.Setenv("BACNET_DEVICE_FIXTURE", deviceBaselineV3Path(t))
	peer := startPeer(t, image, adapter,
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
				// Peers report Out_Of_Range as offnormal(2) or high-limit(3).
				if e.AlarmState != 2 && e.AlarmState != 3 {
					t.Fatalf("alarmState=%d want offnormal(2) or high-limit(3); entry=%+v", e.AlarmState, e)
				}
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

	if err := c.WriteProperty(ctx, peer.target, av, prop, bacnet.RealValue(20), &prio); err != nil {
		t.Fatalf("WriteProperty AV-1=20 restore: %v", err)
	}
	deadline = time.Now().Add(8 * time.Second)
	for {
		ack, err = c.GetAlarmSummary(ctx, peer.target)
		if err != nil {
			t.Fatalf("GetAlarmSummary after restore: %v", err)
		}
		still := false
		for _, e := range ack.Entries {
			if e.Object.Type == bacnet.ObjectTypeAnalogValue && e.Object.Instance == 1 {
				still = true
				break
			}
		}
		if !still {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("AV-1 still in alarm summary after restore; entries=%+v", ack.Entries)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func TestGetAlarmSummaryOutOfRange(t *testing.T) {
	for _, p := range alarmSummaryPeers() {
		t.Run(p.name, func(t *testing.T) {
			runGetAlarmSummaryOutOfRange(t, p.image, p.name)
		})
	}
}

// GetEnrollmentSummary: BACnet4J is the only pinned peer with a native
// executable path (EventEnrollmentObject + GetEnrollmentSummaryRequest.handle).
// bacnet-stack / BACpypes3 / Worldiety: unsupported-upstream — see
// bacnet-interop/PEER_SUPPORT.md.

func TestBACnet4JGetEnrollmentSummary(t *testing.T) {
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

	nc := uint32(1)
	ack, err := c.GetEnrollmentSummary(ctx, peer.target, service.GetEnrollmentSummaryRequest{
		AcknowledgmentFilter:    service.EnrollmentFilterAll,
		NotificationClassFilter: &nc,
	})
	if err != nil {
		t.Fatalf("GetEnrollmentSummary: %v", err)
	}

	// EE-1 monitors AV-1 Out_Of_Range on NC-1 (device-baseline-v3).
	const eventTypeOutOfRange = 5
	foundEE := false
	for _, e := range ack.Entries {
		if e.Object.Type == bacnet.ObjectTypeEventEnrollment && e.Object.Instance == 1 {
			if e.EventType != eventTypeOutOfRange {
				t.Fatalf("EE-1 eventType=%d want out-of-range(%d); entry=%+v", e.EventType, eventTypeOutOfRange, e)
			}
			if e.NotificationClass != 1 {
				t.Fatalf("EE-1 notificationClass=%d want 1; entry=%+v", e.NotificationClass, e)
			}
			foundEE = true
			break
		}
	}
	if !foundEE {
		t.Fatalf("EE-1 missing from enrollment summary; entries=%+v", ack.Entries)
	}
}
