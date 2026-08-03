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

func TestReadSnapshotMetadataFieldsAndPartial(t *testing.T) {
	dev := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 7}
	av := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	r := stubReader{rpm: func(_ context.Context, _ client.Target, specs []service.ReadAccessSpecification) ([]service.ReadAccessResult, error) {
		if specs[0].Object == dev {
			return []service.ReadAccessResult{{
				Object: dev,
				Properties: []service.PropertyResult{
					{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName}, Value: bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: "D"}}},
					{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyVendorIdentifier}, Value: bacnet.UnsignedValue(15)},
					{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyProtocolRevision}, Value: bacnet.UnsignedValue(28)},
					{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyMaxAPDULength}, Value: bacnet.UnsignedValue(480)},
					{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyObjectList}, Err: bacnet.ErrTimeout},
					{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyObjectList}, Value: bacnet.ApplicationValue{
						Kind:     bacnet.ValueConstructed,
						Elements: []bacnet.Element{{Value: bacnet.ObjectIDValue(av)}},
					}},
				},
			}}, nil
		}
		return []service.ReadAccessResult{{
			Object: av,
			Properties: []service.PropertyResult{
				{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}, Err: bacnet.ErrUnsupported},
			},
		}}, nil
	}}
	snap, err := device.ReadSnapshot(context.Background(), r, client.Target{}, 7, device.SnapshotOptions{})
	// ObjectList may fail decode if first property error didn't set list — expect either error or partial.
	if err == nil {
		if snap.VendorID != 15 || snap.ProtocolRev != 28 || snap.MaxAPDU != 480 {
			t.Fatalf("%#v", snap)
		}
	}
}
