//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet/client"
)

func runReadBDT(t *testing.T, image, adapter string, bbmdEnv ...string) {
	t.Helper()
	peer := startPeer(t, image, adapter, bbmdEnv...)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	entries, err := c.ReadBroadcastDistributionTable(ctx, peer.endpoint)
	if err != nil {
		t.Fatalf("Read-BDT: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("expected at least one BDT entry from %s", adapter)
	}
}

func TestBacnetStackReadBDT(t *testing.T) {
	runReadBDT(t, getEnv("BACNET_STACK_IMAGE", defaultStackImage), "bacnet-stack")
}

func TestBACpypes3ReadBDT(t *testing.T) {
	runReadBDT(t, getEnv("BACPYPES3_IMAGE", defaultBACpypes3Image), "bacpypes3", "BACNET_BBMD=1")
}

func TestBACnet4JReadBDT(t *testing.T) {
	runReadBDT(t, getEnv("BACNET4J_IMAGE", defaultBACnet4JImage), "bacnet4j", "BACNET_BBMD=1")
}

func TestWorldietyReadBDTUnsupported(t *testing.T) {
	t.Skip("Worldiety peer-as-BBMD unavailable; see bacnet-interop/PEER_SUPPORT.md")
}

func runReadFDTAfterFD(t *testing.T, image, adapter string, bbmdEnv ...string) {
	t.Helper()
	peer := startPeer(t, image, adapter, bbmdEnv...)
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
			t.Fatal("FD registration failed")
		}
		time.Sleep(50 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	entries, err := c.ReadForeignDeviceTable(ctx, peer.endpoint)
	if err != nil {
		t.Fatalf("Read-FDT: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("expected FDT entry after FD registration on %s", adapter)
	}
}

func TestBacnetStackReadFDTAfterFD(t *testing.T) {
	runReadFDTAfterFD(t, getEnv("BACNET_STACK_IMAGE", defaultStackImage), "bacnet-stack")
}

func TestBACpypes3ReadFDTAfterFD(t *testing.T) {
	runReadFDTAfterFD(t, getEnv("BACPYPES3_IMAGE", defaultBACpypes3Image), "bacpypes3", "BACNET_BBMD=1")
}

func TestBACnet4JReadFDTAfterFD(t *testing.T) {
	runReadFDTAfterFD(t, getEnv("BACNET4J_IMAGE", defaultBACnet4JImage), "bacnet4j", "BACNET_BBMD=1")
}

func TestBacnetStackWriteBDTNAK(t *testing.T) {
	peer := startPeer(t, getEnv("BACNET_STACK_IMAGE", defaultStackImage), "bacnet-stack")
	if peer.assertedByReexec {
		return
	}
	c := newClientOpts(t, client.WithNetworkManagementEnabled(client.NetworkManagementConfirm))
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	err := c.WriteBroadcastDistributionTable(ctx, peer.endpoint, nil)
	var op *client.BVLCOperationError
	if !errors.As(err, &op) {
		t.Fatalf("expected BVLCOperationError NAK, got %v", err)
	}
}

func runWriteBDTIdentity(t *testing.T, image, adapter string, bbmdEnv ...string) {
	t.Helper()
	peer := startPeer(t, image, adapter, bbmdEnv...)
	if peer.assertedByReexec {
		return
	}
	c := newClientOpts(t, client.WithNetworkManagementEnabled(client.NetworkManagementConfirm))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	entries, err := c.ReadBroadcastDistributionTable(ctx, peer.endpoint)
	if err != nil {
		t.Fatalf("Read-BDT: %v", err)
	}
	if err := c.WriteBroadcastDistributionTable(ctx, peer.endpoint, entries); err != nil {
		t.Fatalf("Write-BDT identity: %v", err)
	}
	again, err := c.ReadBroadcastDistributionTable(ctx, peer.endpoint)
	if err != nil {
		t.Fatalf("Read-BDT after write: %v", err)
	}
	if len(again) != len(entries) {
		t.Fatalf("BDT length %d -> %d", len(entries), len(again))
	}
}

func TestBACpypes3WriteBDTNAK(t *testing.T) {
	// BACpypes3 peer-as-BBMD accepts Read-BDT/FDT/FDR but NAKs Write-BDT at the
	// current pin (result 0x0010). Documented in PEER_SUPPORT / COMPATIBILITY.
	peer := startPeer(t, getEnv("BACPYPES3_IMAGE", defaultBACpypes3Image), "bacpypes3", "BACNET_BBMD=1")
	if peer.assertedByReexec {
		return
	}
	c := newClientOpts(t, client.WithNetworkManagementEnabled(client.NetworkManagementConfirm))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	entries, err := c.ReadBroadcastDistributionTable(ctx, peer.endpoint)
	if err != nil {
		t.Fatalf("Read-BDT: %v", err)
	}
	err = c.WriteBroadcastDistributionTable(ctx, peer.endpoint, entries)
	var op *client.BVLCOperationError
	if !errors.As(err, &op) || op.Code != 0x0010 {
		t.Fatalf("expected Write-BDT NAK 0x0010, got %v", err)
	}
}

func TestBACnet4JWriteBDTIdentity(t *testing.T) {
	runWriteBDTIdentity(t, getEnv("BACNET4J_IMAGE", defaultBACnet4JImage), "bacnet4j", "BACNET_BBMD=1")
}

func runDeleteFDTEntry(t *testing.T, image, adapter string, bbmdEnv ...string) {
	t.Helper()
	peer := startPeer(t, image, adapter, bbmdEnv...)
	if peer.assertedByReexec {
		return
	}
	fdClient := newClientOpts(t,
		client.WithForeignDevice(client.ForeignDeviceConfig{
			BBMD: peer.endpoint,
			TTL:  60 * time.Second,
		}),
	)
	deadline := time.Now().Add(8 * time.Second)
	for !fdClient.ForeignDeviceRegistered() {
		if time.Now().After(deadline) {
			t.Fatal("FD registration failed")
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Use the address the BBMD recorded (not LocalEndpoint, which may be 0.0.0.0).
	mgmt := newClientOpts(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	before, err := mgmt.ReadForeignDeviceTable(ctx, peer.endpoint)
	if err != nil || len(before) == 0 {
		t.Fatalf("Read-FDT before delete: %v entries=%d", err, len(before))
	}
	entry := before[0].Address
	_ = fdClient.Close() // stop renew so delete is not immediately re-added

	if err := mgmt.DeleteForeignDeviceTableEntry(ctx, peer.endpoint, entry); err != nil {
		t.Fatalf("Delete-FDT %v: %v", entry, err)
	}
	entries, err := mgmt.ReadForeignDeviceTable(ctx, peer.endpoint)
	if err != nil {
		t.Fatalf("Read-FDT: %v", err)
	}
	for _, e := range entries {
		if e.Address.Equal(entry) {
			t.Fatalf("FDT still contains deleted entry %v", entry)
		}
	}
}

func TestBacnetStackDeleteFDTEntry(t *testing.T) {
	runDeleteFDTEntry(t, getEnv("BACNET_STACK_IMAGE", defaultStackImage), "bacnet-stack")
}

func TestBACpypes3DeleteFDTEntry(t *testing.T) {
	runDeleteFDTEntry(t, getEnv("BACPYPES3_IMAGE", defaultBACpypes3Image), "bacpypes3", "BACNET_BBMD=1")
}

func TestBACnet4JDeleteFDTEntry(t *testing.T) {
	runDeleteFDTEntry(t, getEnv("BACNET4J_IMAGE", defaultBACnet4JImage), "bacnet4j", "BACNET_BBMD=1")
}

func TestWorldietyBDTUnsupported(t *testing.T) {
	t.Skip("Worldiety peer-as-BBMD unavailable; see bacnet-interop/PEER_SUPPORT.md")
}
