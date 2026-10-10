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

func deviceBaselineV7Path(t *testing.T) string {
	t.Helper()
	for _, p := range []string{
		filepath.Join("..", "bacnet-interop", "fixtures", "device", "device-baseline-v7.json"),
		filepath.Join("..", "..", "bacnet-interop", "fixtures", "device", "device-baseline-v7.json"),
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Fatal("device-baseline-v7.json not found")
	return ""
}

func TestBacnetStackWhoAmIYouAre(t *testing.T) {
	t.Setenv("BACNET_DEVICE_FIXTURE", deviceBaselineV7Path(t))
	peer := startPeer(t, getEnv("BACNET_STACK_IMAGE", defaultStackImage), "bacnet-stack",
		"DEVICE_FIXTURE_FILE=/fixtures/device/device-baseline-v7.json",
		"FIXTURE=device-baseline-v7",
	)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	peer.clearOperations()
	if err := c.WhoAmI(ctx, peer.target.Endpoint, false, service.WhoAmI{
		VendorID: 999, ModelName: "InteropModel", SerialNumber: "SN-001",
	}); err != nil {
		t.Fatalf("WhoAmI: %v", err)
	}
	awaitOperation(t, peer, "who-am-i", 5*time.Second)

	peer.clearOperations()
	if err := c.YouAre(ctx, peer.target.Endpoint, false, service.YouAre{
		VendorID: 999, ModelName: "InteropModel", SerialNumber: "SN-001",
		Device: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1234},
	}); err != nil {
		t.Fatalf("YouAre: %v", err)
	}
	awaitOperation(t, peer, "you-are", 5*time.Second)
}

func TestBACnet4JWhoAmIYouAreUnsupported(t *testing.T) {
	t.Skip("BACnet4J 6.1.1 has no WhoAmI/YouAre service classes; see bacnet-interop/PEER_SUPPORT.md")
}

func TestBACpypes3WhoAmIYouAreUnsupported(t *testing.T) {
	t.Skip("BACpypes3 Who-Am-I/You-Are unsupported-upstream; see bacnet-interop/PEER_SUPPORT.md")
}

func TestWorldietyWhoAmIYouAreUnsupported(t *testing.T) {
	t.Skip("Worldiety Who-Am-I/You-Are unsupported-upstream; see bacnet-interop/PEER_SUPPORT.md")
}

// Audit notifications, AuditLogQuery, VT and AuthRequest used to be skip
// stubs here. Their evidence is in audit_services_test.go, vt_services_test.go
// (TestVTOpenUnrecognizedService, TestAuthRequestUnrecognizedService) and
// cov_multiple_services_test.go.
