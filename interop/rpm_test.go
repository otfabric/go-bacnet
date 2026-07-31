//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"errors"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestBacnetStackReadPropertyMultiple(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startPeer(t, getEnv("BACNET_STACK_IMAGE", defaultStackImage), "bacnet-stack")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 8*time.Second)
	defer cancel()

	results, err := c.ReadPropertyMultiple(ctx, peer.target, []service.ReadAccessSpecification{
		{
			Object: deviceObject(dev),
			Properties: []bacnet.PropertyReference{
				{Identifier: bacnet.PropertyObjectName},
				{Identifier: bacnet.PropertyObjectIdentifier},
			},
		},
	})
	if err != nil {
		t.Fatalf("ReadPropertyMultiple: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("objects=%d, want 1", len(results))
	}
	if results[0].Object != deviceObject(dev) {
		t.Fatalf("object=%v, want device:%d", results[0].Object, dev.DeviceInstance)
	}
	name := findPropertyValue(t, results[0], bacnet.PropertyObjectName)
	if characterString(t, name) != dev.DeviceName {
		t.Fatalf("object-name=%q, want %q", characterString(t, name), dev.DeviceName)
	}
}

func TestBACpypes3ReadPropertyMultiple(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startPeer(t, getEnv("BACPYPES3_IMAGE", defaultBACpypes3Image), "bacpypes3")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 8*time.Second)
	defer cancel()

	av, wantPV := analogValueObject(dev)
	results, err := c.ReadPropertyMultiple(ctx, peer.target, []service.ReadAccessSpecification{
		{
			Object: deviceObject(dev),
			Properties: []bacnet.PropertyReference{
				{Identifier: bacnet.PropertyObjectName},
			},
		},
		{
			Object: av,
			Properties: []bacnet.PropertyReference{
				{Identifier: bacnet.PropertyPresentValue},
				{Identifier: bacnet.PropertyObjectName},
			},
		},
	})
	if err != nil {
		t.Fatalf("ReadPropertyMultiple: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("objects=%d, want 2", len(results))
	}
	name := findPropertyValue(t, results[0], bacnet.PropertyObjectName)
	if characterString(t, name) != dev.DeviceName {
		t.Fatalf("device object-name=%q, want %q", characterString(t, name), dev.DeviceName)
	}
	pv := findPropertyValue(t, results[1], bacnet.PropertyPresentValue)
	f, err := bacnet.AsReal(pv)
	if err != nil {
		t.Fatalf("AsReal: %v", err)
	}
	if float64(f) < float64(wantPV)-0.01 || float64(f) > float64(wantPV)+0.01 {
		t.Fatalf("AV present-value=%v, want %v", f, wantPV)
	}
}

func TestBacnetStackReadPropertyMultiplePartialError(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startPeer(t, getEnv("BACNET_STACK_IMAGE", defaultStackImage), "bacnet-stack")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 8*time.Second)
	defer cancel()

	results, err := c.ReadPropertyMultiple(ctx, peer.target, []service.ReadAccessSpecification{
		{
			Object: deviceObject(dev),
			Properties: []bacnet.PropertyReference{
				{Identifier: bacnet.PropertyObjectName},
				{Identifier: unknownPropertyID},
			},
		},
	})
	if err != nil {
		t.Fatalf("ReadPropertyMultiple transaction error: %v", err)
	}
	assertRPMPartialUnknownProperty(t, results, dev)
}

func TestBACpypes3ReadPropertyMultiplePartialError(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startPeer(t, getEnv("BACPYPES3_IMAGE", defaultBACpypes3Image), "bacpypes3")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 8*time.Second)
	defer cancel()

	results, err := c.ReadPropertyMultiple(ctx, peer.target, []service.ReadAccessSpecification{
		{
			Object: deviceObject(dev),
			Properties: []bacnet.PropertyReference{
				{Identifier: bacnet.PropertyObjectName},
				{Identifier: unknownPropertyID},
			},
		},
	})
	if err != nil {
		t.Fatalf("ReadPropertyMultiple transaction error: %v", err)
	}
	assertRPMPartialUnknownProperty(t, results, dev)
}

func assertRPMPartialUnknownProperty(t *testing.T, results []service.ReadAccessResult, dev deviceFixture) {
	t.Helper()
	if len(results) != 1 {
		t.Fatalf("objects=%d, want 1", len(results))
	}
	var sawOK, sawErr bool
	for _, p := range results[0].Properties {
		switch p.Property.Identifier {
		case bacnet.PropertyObjectName:
			sawOK = true
			if p.Err != nil {
				t.Fatalf("object-name property error: %v", p.Err)
			}
			if characterString(t, p.Value) != dev.DeviceName {
				t.Fatalf("object-name=%q, want %q", characterString(t, p.Value), dev.DeviceName)
			}
		case unknownPropertyID:
			sawErr = true
			if p.Err == nil {
				t.Fatal("expected property-level error for unknown property")
			}
			var er *bacnet.ErrorResponse
			if !errors.As(p.Err, &er) {
				t.Fatalf("property Err type %T: %v", p.Err, p.Err)
			}
			if er.Class != errorClassProperty || er.Code != errorCodeUnknownProperty {
				t.Fatalf("property error %d/%d, want %d/%d", er.Class, er.Code, errorClassProperty, errorCodeUnknownProperty)
			}
		}
	}
	if !sawOK || !sawErr {
		t.Fatalf("partial RPM incomplete: ok=%v err=%v props=%#v", sawOK, sawErr, results[0].Properties)
	}
}

func findPropertyValue(t *testing.T, result service.ReadAccessResult, id bacnet.PropertyIdentifier) bacnet.ApplicationValue {
	t.Helper()
	for _, p := range result.Properties {
		if p.Property.Identifier == id {
			if p.Err != nil {
				t.Fatalf("property %d error: %v", id, p.Err)
			}
			return p.Value
		}
	}
	t.Fatalf("property %d missing from RPM result", id)
	return bacnet.ApplicationValue{}
}
