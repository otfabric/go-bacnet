// SPDX-License-Identifier: MIT

package server

import (
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/service"
)

func (h *harness) notification(svc uint8) []byte {
	h.t.Helper()
	got := h.expectType(apdu.TypeUnconfirmedRequest)
	if got.pdu.UnconfirmedRequest.ServiceChoice != svc {
		h.t.Fatalf("service %d, want %d", got.pdu.UnconfirmedRequest.ServiceChoice, svc)
	}
	return got.pdu.UnconfirmedRequest.Payload
}

func readDeviceProp(t *testing.T, h *harness, inv uint8, prop bacnet.PropertyIdentifier) bacnet.ApplicationValue {
	t.Helper()
	h.confirmed(inv, apdu.ServiceReadProperty, mustEncode(t)(service.EncodeReadProperty(service.ReadPropertyRequest{
		Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 100}, Property: bacnet.PropertyReference{Identifier: prop},
	})))
	got := h.expectType(apdu.TypeComplexACK)
	ack, err := service.DecodeReadPropertyACK(got.pdu.ComplexACK.Payload, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	return ack.Value
}

func TestSubscribeCOVProperty(t *testing.T) {
	h := newHarness(t)
	inc := float32(2)
	sub := func(inv uint8, pid uint32, obj bacnet.ObjectIdentifier, prop bacnet.PropertyIdentifier, inc *float32, cancel bool) {
		h.confirmed(inv, apdu.ServiceSubscribeCOVProperty, mustEncode(t)(service.EncodeSubscribeCOVProperty(service.SubscribeCOVPropertyRequest{
			SubscribeCOVRequest: service.SubscribeCOVRequest{ProcessIdentifier: pid, MonitoredObject: obj, Lifetime: 60, Cancellation: cancel},
			Property:            bacnet.PropertyReference{Identifier: prop}, COVIncrement: inc,
		})))
	}
	sub(1, 7, av1, bacnet.PropertyPresentValue, &inc, false)
	h.expectType(apdu.TypeSimpleACK)
	note, err := service.DecodeCOVNotification(h.notification(apdu.ServiceUnconfirmedCOV), bacnet.DefaultDecodeLimits())
	if err != nil || note.ProcessIdentifier != 7 || len(note.Values) != 2 || note.TimeRemaining == 0 {
		t.Fatalf("initial notification %+v %v", note, err)
	}
	// Below the increment: no notification; above it: one.
	if err := h.av.SetPresentValue(bacnet.RealValue(2)); err != nil {
		t.Fatal(err)
	}
	h.quiet(50 * time.Millisecond)
	if err := h.av.SetPresentValue(bacnet.RealValue(4)); err != nil {
		t.Fatal(err)
	}
	h.notification(apdu.ServiceUnconfirmedCOV)

	// Active_COV_Subscriptions lists it with its increment.
	v := readDeviceProp(t, h, 2, bacnet.PropertyActiveCOVSubscriptions)
	if v.Kind != bacnet.ValueConstructed || len(v.Elements) != 5 {
		t.Fatalf("Active_COV_Subscriptions %+v", v)
	}

	// Errors: unknown object, unknown property, negative increment.
	sub(3, 7, bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 99}, bacnet.PropertyPresentValue, nil, false)
	h.expectError(bacnet.ErrorClassObject, bacnet.ErrorCodeUnknownObject)
	sub(4, 7, av1, bacnet.PropertyIdentifier(4000), nil, false)
	h.expectError(bacnet.ErrorClassProperty, bacnet.ErrorCodeUnknownProperty)
	neg := float32(-1)
	sub(5, 7, av1, bacnet.PropertyPresentValue, &neg, false)
	h.expectError(bacnet.ErrorClassProperty, bacnet.ErrorCodeValueOutOfRange)
	h.confirmed(6, apdu.ServiceSubscribeCOVProperty, []byte{0xFF})
	h.expectType(apdu.TypeReject)

	// Status_Flags itself: no second Status_Flags reference.
	sub(7, 8, av1, bacnet.PropertyStatusFlags, nil, false)
	h.expectType(apdu.TypeSimpleACK)
	note, err = service.DecodeCOVNotification(h.notification(apdu.ServiceUnconfirmedCOV), bacnet.DefaultDecodeLimits())
	if err != nil || len(note.Values) != 1 {
		t.Fatalf("status flags notification %+v %v", note, err)
	}
	if h.s.cov.count() != 2 {
		t.Fatalf("%d subscriptions", h.s.cov.count())
	}
	sub(8, 7, av1, bacnet.PropertyPresentValue, nil, true)
	h.expectType(apdu.TypeSimpleACK)
	if h.s.cov.count() != 1 {
		t.Fatalf("%d subscriptions after cancel", h.s.cov.count())
	}
	// Lifetime expiry ends it.
	h.s.cov.expire(time.Now().Add(2 * time.Minute))
	if h.s.cov.count() != 0 {
		t.Fatal("subscription did not expire")
	}
}

func TestSubscribeCOVPropertyMultiple(t *testing.T) {
	h := newHarness(t)
	life := uint32(120)
	req := func(inv uint8, specs []service.COVMultipleSubscription, cancel bool) {
		h.confirmed(inv, apdu.ServiceSubscribeCOVPropertyMultiple, mustEncode(t)(service.EncodeSubscribeCOVPropertyMultiple(service.SubscribeCOVPropertyMultipleRequest{
			SubscriberProcessIdentifier: 3, Cancellation: cancel, LifetimeRemaining: &life, Subscriptions: specs,
		})))
	}
	pv := []service.COVPropertyReference{{Property: pvRef, Timestamped: true}, {Property: bacnet.PropertyReference{Identifier: bacnet.PropertyStatusFlags}}}
	req(1, []service.COVMultipleSubscription{{Object: av1, Properties: pv}}, false)
	h.expectType(apdu.TypeSimpleACK)
	note, err := service.DecodeCOVNotificationMultiple(h.notification(apdu.ServiceUnconfirmedCOVNotificationMultiple), bacnet.DefaultDecodeLimits())
	if err != nil || len(note.Objects) != 1 || len(note.Objects[0].Values) != 2 {
		t.Fatalf("initial notification %+v %v", note, err)
	}
	if err := h.av.SetPresentValue(bacnet.RealValue(9)); err != nil {
		t.Fatal(err)
	}
	h.notification(apdu.ServiceUnconfirmedCOVNotificationMultiple)
	// The device lists each monitored property.
	if v := readDeviceProp(t, h, 2, bacnet.PropertyActiveCOVSubscriptions); len(v.Elements) != 8 {
		t.Fatalf("Active_COV_Subscriptions %d elements", len(v.Elements))
	}

	// The error names the first failed subscription.
	missing := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 50}
	req(3, []service.COVMultipleSubscription{{Object: av1, Properties: pv}, {Object: missing, Properties: pv}}, false)
	got := h.expectType(apdu.TypeError)
	els, _, err := bacnet.ParseSequence(got.pdu.Error.Payload, bacnet.DefaultDecodeLimits(), -1)
	if err != nil || len(els) != 2 || els[1].TagNumber != 1 {
		t.Fatalf("error body %x: %v", got.pdu.Error.Payload, err)
	}
	if id, err := bacnet.ContextObjectID(els[1].Value.Elements[0]); err != nil || id != missing {
		t.Fatalf("first failed %v %v", id, err)
	}
	idx := uint32(1)
	req(4, []service.COVMultipleSubscription{{Object: av1, Properties: []service.COVPropertyReference{{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyIdentifier(4000), ArrayIndex: &idx}}}}}, false)
	h.expectType(apdu.TypeError)
	h.confirmed(5, apdu.ServiceSubscribeCOVPropertyMultiple, []byte{0xFF})
	h.expectType(apdu.TypeReject)

	// Deleting the object ends a subscription that has nothing left.
	h.s.cov.objectRemoved(av1)
	if h.s.cov.count() != 0 {
		t.Fatal("subscription kept after its object was removed")
	}
	req(6, []service.COVMultipleSubscription{{Object: av1, Properties: pv}}, false)
	h.expectType(apdu.TypeSimpleACK)
	h.notification(apdu.ServiceUnconfirmedCOVNotificationMultiple)
	req(7, []service.COVMultipleSubscription{{Object: av1, Properties: pv}}, true)
	h.expectType(apdu.TypeSimpleACK)
	if h.s.cov.count() != 0 {
		t.Fatal("cancel did not end the subscription")
	}
}

func TestCOVLimits(t *testing.T) {
	h := newHarness(t, WithCOVLimits(1, time.Minute))
	sub := func(inv uint8, pid uint32, life uint32) {
		h.confirmed(inv, apdu.ServiceSubscribeCOV, mustEncode(t)(service.EncodeSubscribeCOV(service.SubscribeCOVRequest{ProcessIdentifier: pid, MonitoredObject: av1, Lifetime: life})))
	}
	sub(1, 1, 0) // indefinite: refused when lifetimes are limited
	h.expectType(apdu.TypeError)
	sub(2, 1, 600) // longer than the limit
	h.expectType(apdu.TypeError)
	sub(3, 1, 30)
	h.expectType(apdu.TypeSimpleACK)
	h.notification(apdu.ServiceUnconfirmedCOV)
	sub(4, 2, 30) // the table is full
	h.expectType(apdu.TypeError)
	sub(5, 1, 30) // a renewal of the same one is accepted
	h.expectType(apdu.TypeSimpleACK)
}
