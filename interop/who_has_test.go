//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"context"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/client"
)

func TestBACpypes3WhoHasIHave(t *testing.T) {
	runWhoHasIHave(t, getEnv("BACPYPES3_IMAGE", defaultBACpypes3Image), "bacpypes3")
}

func TestBacnetStackWhoHasIHave(t *testing.T) {
	runWhoHasIHave(t, getEnv("BACNET_STACK_IMAGE", defaultStackImage), "bacnet-stack")
}

func TestBACnet4JWhoHasIHave(t *testing.T) {
	runWhoHasIHave(t, getEnv("BACNET4J_IMAGE", defaultBACnet4JImage), "bacnet4j")
}

func runWhoHasIHave(t *testing.T, image, name string) {
	t.Helper()
	dev := loadDeviceFixture(t)
	peer := startPeer(t, image, name)
	if peer.assertedByReexec {
		return
	}
	// Bind :47808 so broadcast I-Have replies are receivable (same as Who-Is).
	c := newDiscoveryClient(t)

	av, _ := analogValueObject(dev)
	objName := bacnet.CharacterString{Value: "AV-1"}
	if err := c.SendWhoHas(context.Background(), peer.endpoint, false, client.WhoHasOptions{
		Name: &objName,
	}); err != nil {
		t.Fatalf("SendWhoHas: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		for _, obs := range c.Objects() {
			if obs.DeviceInstance == dev.DeviceInstance && obs.Object == av && obs.Name.Value == "AV-1" {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no I-Have for AV-1 from device %d; objects=%v", dev.DeviceInstance, c.Objects())
		}
		time.Sleep(50 * time.Millisecond)
	}
}
