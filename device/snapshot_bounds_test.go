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

// ReadSnapshot stores only the requested properties of the requested object,
// at most MaxProperties per object, and ignores the rest of the reply.
func TestReadSnapshotStoresOnlyRequestedProperties(t *testing.T) {
	dev := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
	other := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeBinaryValue, Instance: 9}
	ref := func(id bacnet.PropertyIdentifier) bacnet.PropertyReference {
		return bacnet.PropertyReference{Identifier: id}
	}
	r := stubReader{rpm: func(_ context.Context, _ client.Target, specs []service.ReadAccessSpecification) ([]service.ReadAccessResult, error) {
		if specs[0].Object == dev {
			var els []bacnet.Element
			for i := range uint32(4) {
				els = append(els, bacnet.Element{Value: bacnet.ObjectIDValue(bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: i})})
			}
			devRes := service.ReadAccessResult{Object: dev, Properties: []service.PropertyResult{
				{Property: ref(bacnet.PropertyObjectList), Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: els}},
			}}
			// Unrequested metadata errors must not grow PartialErrors.
			for i := range 500 {
				devRes.Properties = append(devRes.Properties, service.PropertyResult{
					Property: ref(bacnet.PropertyIdentifier(1000 + i)), Err: bacnet.ErrUnsupported,
				})
			}
			return []service.ReadAccessResult{devRes, {Object: other, Properties: []service.PropertyResult{
				{Property: ref(bacnet.PropertyObjectName), Err: bacnet.ErrUnsupported},
			}}}, nil
		}
		res := service.ReadAccessResult{Object: specs[0].Object}
		res.Properties = append(res.Properties,
			service.PropertyResult{Property: ref(bacnet.PropertyObjectName), Value: bacnet.NullValue()},
			service.PropertyResult{Property: ref(bacnet.PropertyPresentValue), Value: bacnet.NullValue()},
			service.PropertyResult{Property: ref(bacnet.PropertyStatusFlags), Err: bacnet.ErrUnsupported},
		)
		for i := range 1000 {
			res.Properties = append(res.Properties, service.PropertyResult{Property: ref(bacnet.PropertyIdentifier(1000 + i)), Value: bacnet.NullValue()})
		}
		// A result for an object that was not requested.
		return []service.ReadAccessResult{res, {Object: other, Properties: []service.PropertyResult{
			{Property: ref(bacnet.PropertyObjectType), Value: bacnet.NullValue()},
		}}}, nil
	}}
	snap, err := device.ReadSnapshot(context.Background(), r, client.Target{}, 1, device.SnapshotOptions{MaxProperties: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.PartialErrors) != 0 {
		t.Fatalf("%d partial errors from unrequested properties", len(snap.PartialErrors))
	}
	if len(snap.Objects) != 4 {
		t.Fatalf("objects=%d", len(snap.Objects))
	}
	for _, o := range snap.Objects {
		if n := len(o.Properties) + len(o.Errors); n > 2 {
			t.Fatalf("%v: %d properties stored, MaxProperties=2", o.Object, n)
		}
		for id := range o.Properties {
			if id != bacnet.PropertyObjectName && id != bacnet.PropertyObjectType {
				t.Fatalf("%v: unrequested property %v stored", o.Object, id)
			}
		}
		if _, ok := o.Properties[bacnet.PropertyObjectType]; ok {
			t.Fatalf("%v: property of another object stored", o.Object)
		}
		if _, ok := o.Properties[bacnet.PropertyObjectName]; !ok {
			t.Fatalf("%v: requested Object_Name missing", o.Object)
		}
	}
}
