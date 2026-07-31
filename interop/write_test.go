//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"math"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
)

func TestBACpypes3WritePropertyReadbackReset(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startPeer(t, getEnv("BACPYPES3_IMAGE", defaultBACpypes3Image), "bacpypes3")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 15*time.Second)
	defer cancel()

	av, baseline := analogValueObject(dev)
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	prio := uint8(8)
	written := float32(42.25)

	if err := c.WriteProperty(ctx, peer.target, av, prop, bacnet.RealValue(written), &prio); err != nil {
		t.Fatalf("WriteProperty: %v", err)
	}
	val, err := c.ReadProperty(ctx, peer.target, av, prop)
	if err != nil {
		t.Fatalf("ReadProperty after write: %v", err)
	}
	got, err := bacnet.AsReal(val)
	if err != nil {
		t.Fatalf("AsReal: %v", err)
	}
	if math.Abs(float64(got)-float64(written)) > 0.01 {
		t.Fatalf("after write present-value=%v, want %v", got, written)
	}

	// Relinquish priority 8 with Null, then restore baseline at priority 8.
	if err := c.WriteProperty(ctx, peer.target, av, prop, bacnet.NullValue(), &prio); err != nil {
		t.Fatalf("WriteProperty Null relinquish: %v", err)
	}
	if err := c.WriteProperty(ctx, peer.target, av, prop, bacnet.RealValue(baseline), &prio); err != nil {
		t.Fatalf("WriteProperty restore baseline: %v", err)
	}
	val, err = c.ReadProperty(ctx, peer.target, av, prop)
	if err != nil {
		t.Fatalf("ReadProperty after reset: %v", err)
	}
	got, err = bacnet.AsReal(val)
	if err != nil {
		t.Fatalf("AsReal reset: %v", err)
	}
	if math.Abs(float64(got)-float64(baseline)) > 0.01 {
		t.Fatalf("after reset present-value=%v, want baseline %v", got, baseline)
	}
}

func TestBacnetStackWritePropertyReadbackReset(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startPeer(t, getEnv("BACNET_STACK_IMAGE", defaultStackImage), "bacnet-stack")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 15*time.Second)
	defer cancel()

	av, baseline := analogValueObject(dev)
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}

	baselineVal, err := c.ReadProperty(ctx, peer.target, av, prop)
	if err != nil {
		t.Fatalf("ReadProperty baseline AV: %v", err)
	}
	gotBaseline, err := bacnet.AsReal(baselineVal)
	if err != nil {
		t.Fatalf("baseline AsReal: %v", err)
	}
	if math.Abs(float64(gotBaseline)-float64(baseline)) > 0.01 {
		t.Fatalf("fixture baseline=%v, device present-value=%v", baseline, gotBaseline)
	}

	prio := uint8(8)
	written := baseline + 1.25
	if err := c.WriteProperty(ctx, peer.target, av, prop, bacnet.RealValue(written), &prio); err != nil {
		t.Fatalf("WriteProperty: %v", err)
	}
	val, err := c.ReadProperty(ctx, peer.target, av, prop)
	if err != nil {
		t.Fatalf("ReadProperty after write: %v", err)
	}
	got, err := bacnet.AsReal(val)
	if err != nil {
		t.Fatalf("AsReal: %v", err)
	}
	if math.Abs(float64(got)-float64(written)) > 0.01 {
		t.Fatalf("after write present-value=%v, want %v", got, written)
	}

	if err := c.WriteProperty(ctx, peer.target, av, prop, bacnet.RealValue(baseline), &prio); err != nil {
		t.Fatalf("WriteProperty restore: %v", err)
	}
}
