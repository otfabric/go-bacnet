//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"testing"
	"time"

	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/service"
)

// TestBacnetStackDeviceCommunicationControlEnable exercises the safe DCC
// enable path. Disable is intentionally not live-tested (would mute the peer).
func TestBacnetStackDeviceCommunicationControlEnable(t *testing.T) {
	_ = loadDeviceFixture(t)
	peer := startPeer(t, getEnv("BACNET_STACK_IMAGE", defaultStackImage), "bacnet-stack")
	if peer.assertedByReexec {
		return
	}
	c := newClientOpts(t, client.WithDeviceManagementEnabled(client.DeviceManagementConfirm))
	ctx, cancel := withTimeout(t, 10*time.Second)
	defer cancel()

	if err := c.DeviceCommunicationControl(ctx, peer.target, service.DeviceCommunicationControlRequest{
		EnableDisable: service.EnableDisableEnable,
	}); err != nil {
		t.Fatalf("DeviceCommunicationControl enable: %v", err)
	}
}

func TestBacnetStackReinitializeDeviceWarmstart(t *testing.T) {
	runReinitializeDeviceWarmstart(t, getEnv("BACNET_STACK_IMAGE", defaultStackImage), "bacnet-stack")
}

func TestBACpypes3ReinitializeDeviceWarmstart(t *testing.T) {
	runReinitializeDeviceWarmstart(t, getEnv("BACPYPES3_IMAGE", defaultBACpypes3Image), "bacpypes3")
}

func TestBACnet4JReinitializeDeviceWarmstart(t *testing.T) {
	runReinitializeDeviceWarmstart(t, getEnv("BACNET4J_IMAGE", defaultBACnet4JImage), "bacnet4j")
}

func runReinitializeDeviceWarmstart(t *testing.T, image, name string) {
	t.Helper()
	_ = loadDeviceFixture(t)
	peer := startPeer(t, image, name)
	if peer.assertedByReexec {
		return
	}
	c := newClientOpts(t, client.WithDeviceManagementEnabled(client.DeviceManagementConfirm))
	ctx, cancel := withTimeout(t, 10*time.Second)
	defer cancel()

	if err := c.ReinitializeDevice(ctx, peer.target, service.ReinitializeDeviceRequest{
		State: service.ReinitializedWarmstart,
	}); err != nil {
		t.Fatalf("ReinitializeDevice warmstart: %v", err)
	}
}
