// SPDX-License-Identifier: MIT

package service_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestEncodeSubscribeCOVPropertyMultiple(t *testing.T) {
	life := uint32(60)
	inc := float32(0.5)
	raw, err := service.EncodeSubscribeCOVPropertyMultiple(service.SubscribeCOVPropertyMultipleRequest{
		SubscriberProcessIdentifier: 7,
		IssueConfirmedNotifications: true,
		LifetimeRemaining:           &life,
		Subscriptions: []service.COVMultipleSubscription{{
			Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
			Properties: []service.COVPropertyReference{{
				Property:     bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
				COVIncrement: &inc,
				Timestamped:  true,
			}},
		}},
	})
	if err != nil || len(raw) == 0 {
		t.Fatalf("encode: %v len=%d", err, len(raw))
	}
}

func TestEncodeCOVNotificationMultipleMinimal(t *testing.T) {
	note := service.COVNotificationMultiple{
		SubscriberProcessIdentifier: 1,
		InitiatingDevice:            bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		TimeRemaining:               1,
		Objects: []service.COVNotificationMultipleObject{{
			Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
			Values: []service.COVNotificationMultipleValue{{
				Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
				Value:    bacnet.RealValue(1),
			}},
		}},
	}
	raw, err := service.EncodeCOVNotificationMultiple(note)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeCOVNotificationMultiple(raw, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if got.Timestamp != nil || len(got.Objects) != 1 {
		t.Fatalf("%+v", got)
	}
}

func TestEncodeDecodeCOVNotificationMultipleRoundTrip(t *testing.T) {
	idx := uint32(1)
	note := service.COVNotificationMultiple{
		SubscriberProcessIdentifier: 3,
		InitiatingDevice:            bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 10},
		TimeRemaining:               40,
		Timestamp: &service.DateTime{
			Date: bacnet.Date{Year: 124, Month: 6, Day: 1},
			Time: bacnet.Time{Hour: 12},
		},
		Objects: []service.COVNotificationMultipleObject{{
			Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
			Values: []service.COVNotificationMultipleValue{{
				Property: bacnet.PropertyReference{
					Identifier: bacnet.PropertyPresentValue,
					ArrayIndex: &idx,
				},
				Value: bacnet.RealValue(9.5),
				TimeStamp: &service.DateTime{
					Date: bacnet.Date{Year: 124, Month: 6, Day: 1},
					Time: bacnet.Time{Hour: 12, Minute: 1},
				},
			}},
		}},
	}
	raw, err := service.EncodeCOVNotificationMultiple(note)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeCOVNotificationMultiple(raw, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if got.SubscriberProcessIdentifier != 3 || len(got.Objects) != 1 {
		t.Fatalf("%+v", got)
	}
	if got.Objects[0].Values[0].Property.ArrayIndex == nil || *got.Objects[0].Values[0].Property.ArrayIndex != 1 {
		t.Fatalf("index %+v", got.Objects[0].Values[0])
	}
}

func TestDecodeCOVNotificationMultipleMinimal(t *testing.T) {
	// Build via tags: pid, device, timeRemaining, listOfValues for one object.
	var raw []byte
	var err error
	raw, err = bacnet.AppendContextUnsigned(raw, 0, 7)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextObjectID(raw, 1, bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1234})
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextUnsigned(raw, 2, 30)
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
	valBody, err = bacnet.AppendContextTagged(valBody, 2, []bacnet.Element{{Value: bacnet.RealValue(21.5)}})
	if err != nil {
		t.Fatal(err)
	}
	objBody, err = bacnet.AppendContextTagged(objBody, 1, mustParseCOVEls(t, valBody))
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextTagged(raw, 4, mustParseCOVEls(t, objBody))
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeCOVNotificationMultiple(raw, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if got.SubscriberProcessIdentifier != 7 || len(got.Objects) != 1 {
		t.Fatalf("%+v", got)
	}
}

func mustParseCOVEls(t *testing.T, raw []byte) []bacnet.Element {
	t.Helper()
	els, n, err := bacnet.ParseSequence(raw, bacnet.DefaultDecodeLimits(), -1)
	if err != nil || n != len(raw) {
		t.Fatalf("parse: %v n=%d", err, n)
	}
	return els
}
