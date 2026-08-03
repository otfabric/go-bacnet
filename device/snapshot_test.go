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

type stubReader struct {
	rpm func(context.Context, client.Target, []service.ReadAccessSpecification) ([]service.ReadAccessResult, error)
}

func (s stubReader) ReadProperty(ctx context.Context, target client.Target, object bacnet.ObjectIdentifier, property bacnet.PropertyReference) (bacnet.ApplicationValue, error) {
	return bacnet.ApplicationValue{}, bacnet.ErrUnsupported
}

func (s stubReader) ReadPropertyMultiple(ctx context.Context, target client.Target, specs []service.ReadAccessSpecification) ([]service.ReadAccessResult, error) {
	return s.rpm(ctx, target, specs)
}

func TestReadSnapshotMinimal(t *testing.T) {
	dev := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
	av := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	r := stubReader{rpm: func(_ context.Context, _ client.Target, specs []service.ReadAccessSpecification) ([]service.ReadAccessResult, error) {
		if specs[0].Object == dev {
			return []service.ReadAccessResult{{
				Object: dev,
				Properties: []service.PropertyResult{
					{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName}, Value: bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: "Dev"}}},
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
				{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName}, Value: bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: "AV-1"}}},
			},
		}}, nil
	}}
	snap, err := device.ReadSnapshot(context.Background(), r, client.Target{}, 1, device.SnapshotOptions{MaxObjects: 10, MaxProperties: 2})
	if err != nil {
		t.Fatal(err)
	}
	if snap.ObjectName != "Dev" || len(snap.Objects) != 1 {
		t.Fatalf("%#v", snap)
	}
}

func TestReadSnapshotRPMError(t *testing.T) {
	r := stubReader{rpm: func(context.Context, client.Target, []service.ReadAccessSpecification) ([]service.ReadAccessResult, error) {
		return nil, bacnet.ErrTimeout
	}}
	if _, err := device.ReadSnapshot(context.Background(), r, client.Target{}, 1, device.SnapshotOptions{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestReadSnapshotPartialPaths(t *testing.T) {
	dev := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
	av := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	calls := 0
	r := stubReader{rpm: func(_ context.Context, _ client.Target, specs []service.ReadAccessSpecification) ([]service.ReadAccessResult, error) {
		calls++
		if specs[0].Object == dev {
			return []service.ReadAccessResult{{
				Object: dev,
				Properties: []service.PropertyResult{
					{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyVendorName}, Value: bacnet.UnsignedValue(1)},
					{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyObjectList}, Value: bacnet.BoolValue(true)},
				},
			}}, nil
		}
		return nil, bacnet.ErrTimeout
	}}
	if _, err := device.ReadSnapshot(context.Background(), r, client.Target{}, 1, device.SnapshotOptions{MaxObjects: 10}); err == nil {
		t.Fatal("expected object-list decode error")
	}

	r2 := stubReader{rpm: func(_ context.Context, _ client.Target, specs []service.ReadAccessSpecification) ([]service.ReadAccessResult, error) {
		if specs[0].Object == dev {
			return []service.ReadAccessResult{{
				Object: dev,
				Properties: []service.PropertyResult{
					{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyObjectList}, Value: bacnet.ApplicationValue{
						Kind:     bacnet.ValueConstructed,
						Elements: []bacnet.Element{{Value: bacnet.ObjectIDValue(av)}},
					}},
				},
			}}, nil
		}
		return nil, bacnet.ErrTimeout
	}}
	snap, err := device.ReadSnapshot(context.Background(), r2, client.Target{}, 1, device.SnapshotOptions{MaxObjects: 10, MaxProperties: 4})
	if err != nil || len(snap.PartialErrors) == 0 || len(snap.Objects) != 1 {
		t.Fatalf("%v %#v", err, snap)
	}
}
