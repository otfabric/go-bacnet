//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestBACpypes3SegmentedReadPropertyMultiple(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startPeer(t, getEnv("BACPYPES3_IMAGE", defaultBACpypes3Image), "bacpypes3")
	if peer.assertedByReexec {
		return
	}
	// Advertise a small MaxAPDU so the peer segments the ComplexACK.
	c := newClientWithAdvertisedMaxAPDU(t, 50)
	ctx, cancel := withTimeout(t, 15*time.Second)
	defer cancel()

	av, wantPV := analogValueObject(dev)
	results, err := c.ReadPropertyMultiple(ctx, peer.target, []service.ReadAccessSpecification{
		{
			Object: deviceObject(dev),
			Properties: []bacnet.PropertyReference{
				{Identifier: bacnet.PropertyObjectName},
				{Identifier: bacnet.PropertyObjectIdentifier},
				{Identifier: bacnet.PropertyObjectType},
				{Identifier: bacnet.PropertyVendorIdentifier},
				{Identifier: bacnet.PropertyModelName},
				{Identifier: bacnet.PropertyDescription},
				{Identifier: bacnet.PropertySystemStatus},
				{Identifier: bacnet.PropertyProtocolVersion},
				{Identifier: bacnet.PropertyProtocolRevision},
				{Identifier: bacnet.PropertyMaxAPDULength},
				{Identifier: bacnet.PropertySegmentation},
			},
		},
		{
			Object: av,
			Properties: []bacnet.PropertyReference{
				{Identifier: bacnet.PropertyObjectName},
				{Identifier: bacnet.PropertyPresentValue},
				{Identifier: bacnet.PropertyDescription},
				{Identifier: bacnet.PropertyStatusFlags},
				{Identifier: bacnet.PropertyUnits},
			},
		},
	})
	if err != nil {
		t.Fatalf("segmented ReadPropertyMultiple: %v", err)
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
