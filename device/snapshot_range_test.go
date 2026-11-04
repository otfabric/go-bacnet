// SPDX-License-Identifier: MIT

package device_test

import (
	"context"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/device"
	"github.com/otfabric/go-bacnet/service"
)

// emptyObjectList lets ReadSnapshot finish without inspecting objects.
var emptyObjectList = service.PropertyResult{
	Property: bacnet.PropertyReference{Identifier: bacnet.PropertyObjectList},
	Value:    bacnet.ApplicationValue{Kind: bacnet.ValueConstructed},
}

// TestReadSnapshotOutOfRangeUnsigned: a device that reports Vendor_Identifier,
// Protocol_Revision or Max_APDU_Length_Accepted wider than 32 bits must not
// have the value truncated into the snapshot. The value is dropped and the
// problem recorded in PartialErrors.
func TestReadSnapshotOutOfRangeUnsigned(t *testing.T) {
	dev := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
	const wide = 1<<32 + 5 // truncates to 5
	cases := []struct {
		name string
		prop bacnet.PropertyIdentifier
		get  func(device.Snapshot) uint32
	}{
		{"VendorID", bacnet.PropertyVendorIdentifier, func(s device.Snapshot) uint32 { return s.VendorID }},
		{"ProtocolRevision", bacnet.PropertyProtocolRevision, func(s device.Snapshot) uint32 { return s.ProtocolRev }},
		{"MaxAPDU", bacnet.PropertyMaxAPDULength, func(s device.Snapshot) uint32 { return s.MaxAPDU }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := stubReader{rpm: func(_ context.Context, _ client.Target, specs []service.ReadAccessSpecification) ([]service.ReadAccessResult, error) {
				return []service.ReadAccessResult{{
					Object: dev,
					Properties: []service.PropertyResult{
						{Property: bacnet.PropertyReference{Identifier: tc.prop}, Value: bacnet.UnsignedValue(wide)},
						emptyObjectList,
					},
				}}, nil
			}}
			snap, err := device.ReadSnapshot(context.Background(), r, client.Target{}, 1, device.SnapshotOptions{})
			if got := tc.get(snap); got != 0 {
				t.Fatalf("%s = %d: a %d on the wire was truncated", tc.name, got, uint64(wide))
			}
			if err == nil && len(snap.PartialErrors) == 0 {
				t.Fatal("an out-of-range value was dropped without a PartialErrors entry")
			}
		})
	}
}

// TestReadSnapshotVendorIDAboveUnsigned16: Vendor_Identifier is Unsigned16
// (Device object, ASHRAE 135 Clause 12.11); 65536 is not a vendor and must not appear as
// one.
func TestReadSnapshotVendorIDAboveUnsigned16(t *testing.T) {
	dev := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
	r := stubReader{rpm: func(context.Context, client.Target, []service.ReadAccessSpecification) ([]service.ReadAccessResult, error) {
		return []service.ReadAccessResult{{
			Object: dev,
			Properties: []service.PropertyResult{
				{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyVendorIdentifier}, Value: bacnet.UnsignedValue(65536)},
				emptyObjectList,
			},
		}}, nil
	}}
	snap, err := device.ReadSnapshot(context.Background(), r, client.Target{}, 1, device.SnapshotOptions{})
	if snap.VendorID > 0xFFFF {
		t.Fatalf("VendorID %d exceeds Unsigned16", snap.VendorID)
	}
	if err == nil && len(snap.PartialErrors) == 0 {
		t.Fatal("an out-of-range Vendor_Identifier was dropped without a PartialErrors entry")
	}
}
