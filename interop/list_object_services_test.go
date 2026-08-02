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

func deviceBaselineV5Path(t *testing.T) string {
	t.Helper()
	for _, p := range []string{
		filepath.Join("..", "bacnet-interop", "fixtures", "device", "device-baseline-v5.json"),
		filepath.Join("..", "..", "bacnet-interop", "fixtures", "device", "device-baseline-v5.json"),
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Fatal("device-baseline-v5.json not found")
	return ""
}

func TestBACnet4JCreateDeleteObject(t *testing.T) {
	// BACnet4J returns object/object-deletion-not-permitted (1/23) for AV-100
	// unless objects are marked deletable; CreateObject also needs explicit
	// creatable-type configuration. Tracked as BLOCKERS B9.
	t.Skip("blocker B9: BACnet4J CreateObject/DeleteObject not yet configured for fixture AV lifecycle")

	t.Setenv("BACNET_DEVICE_FIXTURE", deviceBaselineV5Path(t))
	peer := startPeer(t, bacnet4jImage(), "bacnet4j",
		"DEVICE_FIXTURE_FILE=/fixtures/device/device-baseline-v5.json",
		"FIXTURE=device-baseline-v5",
	)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	del := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 100}
	if err := c.DeleteObject(ctx, peer.target, service.DeleteObjectRequest{Object: del}); err != nil {
		t.Fatalf("DeleteObject AV-100: %v", err)
	}

	id := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 101}
	ack, err := c.CreateObject(ctx, peer.target, service.CreateObjectRequest{
		ObjectIdentifier: &id,
		InitialValues: []service.PropertyValue{{
			Property: bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName},
			Value: bacnet.ApplicationValue{
				Kind:      bacnet.ValueCharacterString,
				Character: bacnet.CharacterString{Value: "AV-CREATED-101"},
			},
		}},
	})
	if err != nil {
		t.Fatalf("CreateObject: %v", err)
	}
	if ack.Object.Instance != 101 {
		t.Fatalf("ack=%+v", ack)
	}
	name, err := c.ReadProperty(ctx, peer.target, ack.Object, bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName})
	if err != nil {
		t.Fatalf("readback: %v", err)
	}
	if name.Character.Value != "AV-CREATED-101" {
		t.Fatalf("object-name=%q", name.Character.Value)
	}
}
