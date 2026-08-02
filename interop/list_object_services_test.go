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

// Peers with upstream-native CreateObject/DeleteObject for analog-value at current pins.
// BACpypes3 0.0.106 and Worldiety have no Create/Delete server handlers.
func lifecyclePeers() []struct {
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

func runCreateDeleteObject(t *testing.T, image, adapter string) {
	t.Helper()
	t.Setenv("BACNET_DEVICE_FIXTURE", deviceBaselineV5Path(t))
	peer := startPeer(t, image, adapter,
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
	req := service.CreateObjectRequest{ObjectIdentifier: &id}
	// BACnet4J accepts Object_Name in List of Initial Values. bacnet-stack AV
	// rejects Object_Name writes (write-access-denied), so keep create bare there.
	if adapter == "bacnet4j" {
		req.InitialValues = []service.PropertyValue{{
			Property: bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName},
			Value: bacnet.ApplicationValue{
				Kind:      bacnet.ValueCharacterString,
				Character: bacnet.CharacterString{Value: "AV-CREATED-101"},
			},
		}}
	}
	ack, err := c.CreateObject(ctx, peer.target, req)
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
	if adapter == "bacnet4j" && name.Character.Value != "AV-CREATED-101" {
		t.Fatalf("object-name=%q", name.Character.Value)
	}
	if name.Kind != bacnet.ValueCharacterString || name.Character.Value == "" {
		t.Fatalf("expected object-name character string, got %+v", name)
	}
}

func TestCreateDeleteObject(t *testing.T) {
	for _, p := range lifecyclePeers() {
		t.Run(p.name, func(t *testing.T) {
			runCreateDeleteObject(t, p.image, p.name)
		})
	}
}
