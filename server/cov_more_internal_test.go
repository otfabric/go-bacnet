// SPDX-License-Identifier: MIT

package server

import (
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/server/object"
	"github.com/otfabric/go-bacnet/service"
)

func TestSubscribeCOVObjectErrors(t *testing.T) {
	h := newHarness(t)
	if err := h.dev.Add(newListObject(0)); err != nil {
		t.Fatal(err)
	}
	sub := func(inv uint8, obj bacnet.ObjectIdentifier) {
		h.confirmed(inv, apdu.ServiceSubscribeCOV, mustEncode(t)(service.EncodeSubscribeCOV(service.SubscribeCOVRequest{ProcessIdentifier: 1, MonitoredObject: obj, Lifetime: 60})))
	}
	sub(1, bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 99})
	h.expectError(bacnet.ErrorClassObject, bacnet.ErrorCodeUnknownObject)
	// An object that does not report COV.
	sub(2, listObjectID)
	h.expectError(bacnet.ErrorClassObject, bacnet.ErrorCodeOptionalFunctionalityNotSupported)
	if h.s.cov.count() != 0 {
		t.Fatalf("%d subscriptions", h.s.cov.count())
	}
}

// TestSubscribeCOVPropertyArrayIndex subscribes to one slot of
// Priority_Array; Active_COV_Subscriptions names the slot in the
// monitoredPropertyReference (BACnetObjectPropertyReference, clause 21).
func TestSubscribeCOVPropertyArrayIndex(t *testing.T) {
	h := newHarness(t)
	enc := mustEncode(t)
	slot := uint32(8)
	pa := bacnet.PropertyReference{Identifier: bacnet.PropertyPriorityArray, ArrayIndex: &slot}
	sub := func(inv uint8, ref bacnet.PropertyReference) {
		h.confirmed(inv, apdu.ServiceSubscribeCOVProperty, enc(service.EncodeSubscribeCOVProperty(service.SubscribeCOVPropertyRequest{
			SubscribeCOVRequest: service.SubscribeCOVRequest{ProcessIdentifier: 4, MonitoredObject: av1, Lifetime: 60},
			Property:            ref,
		})))
	}
	sub(1, pa)
	h.expectType(apdu.TypeSimpleACK)
	note, err := service.DecodeCOVNotification(h.notification(apdu.ServiceUnconfirmedCOV), bacnet.DefaultDecodeLimits())
	if err != nil || len(note.Values) == 0 || note.Values[0].Property.Identifier != bacnet.PropertyPriorityArray ||
		note.Values[0].Property.ArrayIndex == nil || *note.Values[0].Property.ArrayIndex != 8 || note.Values[0].Value.Kind != bacnet.ValueNull {
		t.Fatalf("initial notification %+v %v", note, err)
	}
	// The same property without an index is another subscription.
	sub(2, bacnet.PropertyReference{Identifier: bacnet.PropertyPriorityArray})
	h.expectType(apdu.TypeSimpleACK)
	h.notification(apdu.ServiceUnconfirmedCOV)
	if h.s.cov.count() != 2 {
		t.Fatalf("%d subscriptions", h.s.cov.count())
	}
	v := readDeviceProp(t, h, 3, bacnet.PropertyActiveCOVSubscriptions)
	var indexed int
	for _, el := range v.Elements {
		if el.TagNumber != 1 || el.Value.Kind != bacnet.ValueConstructed {
			continue
		}
		mpr := el.Value.Elements
		if len(mpr) != 3 || mpr[2].TagNumber != 2 {
			continue
		}
		if idx, err := bacnet.ContextUnsigned(mpr[2]); err == nil && idx == 8 {
			indexed++
		}
	}
	if indexed != 1 {
		t.Fatalf("%d monitored references with index 8", indexed)
	}
	// Commanding slot 8 changes it.
	if err := h.av.WriteProperty(t.Context(), object.Write{Property: pvRef, Value: bacnet.RealValue(5), Priority: 8}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		h.notification(apdu.ServiceUnconfirmedCOV)
	}
}

// TestCOVMultipleObjectRemoved deletes one of two monitored objects: the
// subscription goes on for the other.
func TestCOVMultipleObjectRemoved(t *testing.T) {
	h := newHarness(t)
	av2, err := object.NewAnalogValue(object.PointConfig{Instance: 2, Name: "AV-2"})
	if err != nil {
		t.Fatal(err)
	}
	av2.SetDeletable(true)
	if err := h.dev.Add(av2); err != nil {
		t.Fatal(err)
	}
	enc := mustEncode(t)
	life := uint32(120)
	pv := []service.COVPropertyReference{{Property: pvRef}}
	h.confirmed(1, apdu.ServiceSubscribeCOVPropertyMultiple, enc(service.EncodeSubscribeCOVPropertyMultiple(service.SubscribeCOVPropertyMultipleRequest{
		SubscriberProcessIdentifier: 3, LifetimeRemaining: &life,
		Subscriptions: []service.COVMultipleSubscription{{Object: av1, Properties: pv}, {Object: av2.Identifier(), Properties: pv}},
	})))
	h.expectType(apdu.TypeSimpleACK)
	note, err := service.DecodeCOVNotificationMultiple(h.notification(apdu.ServiceUnconfirmedCOVNotificationMultiple), bacnet.DefaultDecodeLimits())
	if err != nil || len(note.Objects) != 2 {
		t.Fatalf("initial notification %+v %v", note, err)
	}
	h.confirmed(2, apdu.ServiceDeleteObject, enc(service.EncodeDeleteObject(service.DeleteObjectRequest{Object: av2.Identifier()})))
	h.expectType(apdu.TypeSimpleACK)
	if h.s.cov.count() != 1 {
		t.Fatalf("%d subscriptions after deleting one of two objects", h.s.cov.count())
	}
	if err := h.av.SetPresentValue(bacnet.RealValue(9)); err != nil {
		t.Fatal(err)
	}
	note, err = service.DecodeCOVNotificationMultiple(h.notification(apdu.ServiceUnconfirmedCOVNotificationMultiple), bacnet.DefaultDecodeLimits())
	if err != nil || len(note.Objects) != 1 || note.Objects[0].Object != av1 {
		t.Fatalf("notification after delete %+v %v", note, err)
	}
}
