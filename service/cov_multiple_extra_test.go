// SPDX-License-Identifier: MIT

package service_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestEncodeGetAlarmSummaryEmpty(t *testing.T) {
	raw, err := service.EncodeGetAlarmSummary()
	if err != nil || raw != nil {
		t.Fatalf("got %v %v", raw, err)
	}
}

func TestEncodeSubscribeCOVPropertyMultipleRequiresSubscriptions(t *testing.T) {
	_, err := service.EncodeSubscribeCOVPropertyMultiple(service.SubscribeCOVPropertyMultipleRequest{
		SubscriberProcessIdentifier: 1,
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEncodeSubscribeCOVPropertyMultipleWithArrayIndex(t *testing.T) {
	idx := uint32(2)
	inc := float32(1)
	life := uint32(10)
	raw, err := service.EncodeSubscribeCOVPropertyMultiple(service.SubscribeCOVPropertyMultipleRequest{
		SubscriberProcessIdentifier: 1,
		IssueConfirmedNotifications: false,
		LifetimeRemaining:           &life,
		Subscriptions: []service.COVMultipleSubscription{{
			Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
			Properties: []service.COVPropertyReference{{
				Property: bacnet.PropertyReference{
					Identifier: bacnet.PropertyPresentValue,
					ArrayIndex: &idx,
				},
				COVIncrement: &inc,
				Timestamped:  false,
			}},
		}},
	})
	if err != nil || len(raw) < 10 {
		t.Fatalf("encode: %v len=%d", err, len(raw))
	}
}

func TestDecodeCOVNotificationMultipleFull(t *testing.T) {
	var raw []byte
	var err error
	raw, err = bacnet.AppendContextUnsigned(raw, 0, 7)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextObjectID(raw, 1, bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 9})
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextUnsigned(raw, 2, 5)
	if err != nil {
		t.Fatal(err)
	}
	tsEls := []bacnet.Element{
		{Value: bacnet.ApplicationValue{Kind: bacnet.ValueDate, Date: bacnet.Date{Year: 124, Month: 1, Day: 2}}},
		{Value: bacnet.ApplicationValue{Kind: bacnet.ValueTime, Time: bacnet.Time{Hour: 3, Minute: 4}}},
	}
	raw, err = bacnet.AppendContextTagged(raw, 3, tsEls)
	if err != nil {
		t.Fatal(err)
	}
	objBody, err := bacnet.AppendContextObjectID(nil, 0, bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1})
	if err != nil {
		t.Fatal(err)
	}
	valBody, err := bacnet.AppendContextUnsigned(nil, 0, uint64(bacnet.PropertyPresentValue))
	if err != nil {
		t.Fatal(err)
	}
	valBody, err = bacnet.AppendContextUnsigned(valBody, 1, 3)
	if err != nil {
		t.Fatal(err)
	}
	valBody, err = bacnet.AppendContextTagged(valBody, 2, []bacnet.Element{
		{Value: bacnet.RealValue(1)},
		{Value: bacnet.RealValue(2)},
	})
	if err != nil {
		t.Fatal(err)
	}
	valBody, err = bacnet.AppendContextTagged(valBody, 3, tsEls)
	if err != nil {
		t.Fatal(err)
	}
	els, n, err := bacnet.ParseSequence(valBody, bacnet.DefaultDecodeLimits(), -1)
	if err != nil || n != len(valBody) {
		t.Fatal(err)
	}
	objBody, err = bacnet.AppendContextTagged(objBody, 1, els)
	if err != nil {
		t.Fatal(err)
	}
	// orphan listOfValues without object → error path via second object list entry alone is hard;
	// add unexpected tag path and values-without-object via crafted object list.
	objEls, n, err := bacnet.ParseSequence(objBody, bacnet.DefaultDecodeLimits(), -1)
	if err != nil || n != len(objBody) {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextTagged(raw, 4, objEls)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeCOVNotificationMultiple(raw, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if got.Timestamp == nil || len(got.Objects) != 1 || got.Objects[0].Values[0].TimeStamp == nil {
		t.Fatalf("%+v", got)
	}
	if got.Objects[0].Values[0].Property.ArrayIndex == nil || *got.Objects[0].Values[0].Property.ArrayIndex != 3 {
		t.Fatalf("array index %+v", got.Objects[0].Values[0])
	}
	// unexpected tag
	bad, err := bacnet.AppendContextUnsigned(nil, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	bad, err = bacnet.AppendContextObjectID(bad, 1, bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1})
	if err != nil {
		t.Fatal(err)
	}
	bad, err = bacnet.AppendContextUnsigned(bad, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	bad, err = bacnet.AppendContextUnsigned(bad, 9, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeCOVNotificationMultiple(bad, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected unexpected tag")
	}
	// listOfValues without object
	orphan, err := bacnet.AppendContextTagged(nil, 1, []bacnet.Element{})
	if err != nil {
		t.Fatal(err)
	}
	orphanEls, n, err := bacnet.ParseSequence(orphan, bacnet.DefaultDecodeLimits(), -1)
	if err != nil || n != len(orphan) {
		t.Fatal(err)
	}
	bad2, err := bacnet.AppendContextUnsigned(nil, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	bad2, err = bacnet.AppendContextObjectID(bad2, 1, bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1})
	if err != nil {
		t.Fatal(err)
	}
	bad2, err = bacnet.AppendContextUnsigned(bad2, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	bad2, err = bacnet.AppendContextTagged(bad2, 4, orphanEls)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeCOVNotificationMultiple(bad2, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected orphan listOfValues")
	}
}

func TestEncodeGetEnrollmentSummaryFilters(t *testing.T) {
	es := uint32(2)
	et := uint32(5)
	nc := uint32(1)
	prio := [2]uint32{1, 200}
	raw, err := service.EncodeGetEnrollmentSummary(service.GetEnrollmentSummaryRequest{
		AcknowledgmentFilter:    service.EnrollmentFilterAll,
		EventStateFilter:        &es,
		EventTypeFilter:         &et,
		PriorityFilter:          &prio,
		NotificationClassFilter: &nc,
	})
	if err != nil || len(raw) < 8 {
		t.Fatalf("encode: %v len=%d", err, len(raw))
	}
}

func TestDecodeCOVNotificationMultipleErrors(t *testing.T) {
	if _, err := service.DecodeCOVNotificationMultiple([]byte{0x00}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected malformed")
	}
	raw, err := bacnet.AppendContextUnsigned(nil, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeCOVNotificationMultiple(raw, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected missing required")
	}
}
