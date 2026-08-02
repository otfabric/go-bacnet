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

func deviceBaselineV8Path(t *testing.T) string {
	t.Helper()
	for _, p := range []string{
		filepath.Join("..", "bacnet-interop", "fixtures", "device", "device-baseline-v8.json"),
		filepath.Join("..", "..", "bacnet-interop", "fixtures", "device", "device-baseline-v8.json"),
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Fatal("device-baseline-v8.json not found")
	return ""
}

func runLifeSafetyOperation(t *testing.T, image, adapter string) {
	t.Helper()
	t.Setenv("BACNET_DEVICE_FIXTURE", deviceBaselineV8Path(t))
	peer := startPeer(t, image, adapter,
		"DEVICE_FIXTURE_FILE=/fixtures/device/device-baseline-v8.json",
		"FIXTURE=device-baseline-v8",
	)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeLifeSafetyPoint, Instance: 1}
	if err := c.LifeSafetyOperation(ctx, peer.target, service.LifeSafetyOperationRequest{
		RequestingProcessIdentifier: 1,
		RequestingSource:            "ops",
		Request:                     1, // silence
		Object:                      &obj,
	}); err != nil {
		t.Fatalf("LifeSafetyOperation: %v", err)
	}
}

func TestLifeSafetyOperation(t *testing.T) {
	for _, p := range []struct {
		name  string
		image string
	}{
		{"bacnet4j", bacnet4jImage()},
		{"bacnet-stack", getEnv("BACNET_STACK_IMAGE", defaultStackImage)},
	} {
		t.Run(p.name, func(t *testing.T) {
			runLifeSafetyOperation(t, p.image, p.name)
		})
	}
}

func TestBACpypes3LifeSafetyUnsupported(t *testing.T) {
	t.Skip("BACpypes3 LifeSafetyOperation unsupported-upstream; see bacnet-interop/EVIDENCE.md (B7f)")
}

func TestWorldietyLifeSafetyUnsupported(t *testing.T) {
	t.Skip("Worldiety LifeSafetyOperation unsupported-upstream; see bacnet-interop/EVIDENCE.md (B7f)")
}
