//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"math"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestBACpypes3WritePropertyMultipleReadbackReset(t *testing.T) {
	runWritePropertyMultipleReadbackReset(t, getEnv("BACPYPES3_IMAGE", defaultBACpypes3Image), "bacpypes3")
}

func TestBacnetStackWritePropertyMultipleReadbackReset(t *testing.T) {
	runWritePropertyMultipleReadbackReset(t, getEnv("BACNET_STACK_IMAGE", defaultStackImage), "bacnet-stack")
}

func TestBACnet4JWritePropertyMultipleReadbackReset(t *testing.T) {
	runWritePropertyMultipleReadbackReset(t, getEnv("BACNET4J_IMAGE", defaultBACnet4JImage), "bacnet4j")
}

func runWritePropertyMultipleReadbackReset(t *testing.T, image, name string) {
	t.Helper()
	dev := loadDeviceFixture(t)
	peer := startPeer(t, image, name)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 15*time.Second)
	defer cancel()

	av, baseline := analogValueObject(dev)
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	prio := uint8(8)
	written := baseline + 2.5

	specs := []service.WriteAccessSpecification{{
		Object: av,
		Properties: []service.WritePropertyValue{{
			Property: prop,
			Value:    bacnet.RealValue(written),
			Priority: &prio,
		}},
	}}
	if err := c.WritePropertyMultiple(ctx, peer.target, specs); err != nil {
		t.Fatalf("WritePropertyMultiple: %v", err)
	}
	val, err := c.ReadProperty(ctx, peer.target, av, prop)
	if err != nil {
		t.Fatalf("ReadProperty after WPM: %v", err)
	}
	got, err := bacnet.AsReal(val)
	if err != nil {
		t.Fatalf("AsReal: %v", err)
	}
	if math.Abs(float64(got)-float64(written)) > 0.01 {
		t.Fatalf("after WPM present-value=%v, want %v", got, written)
	}

	restore := []service.WriteAccessSpecification{{
		Object: av,
		Properties: []service.WritePropertyValue{{
			Property: prop,
			Value:    bacnet.RealValue(baseline),
			Priority: &prio,
		}},
	}}
	if err := c.WritePropertyMultiple(ctx, peer.target, restore); err != nil {
		t.Fatalf("WritePropertyMultiple restore: %v", err)
	}
}
