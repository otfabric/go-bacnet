// SPDX-License-Identifier: MIT

package service_test

import (
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func encodeCOVNotification(t *testing.T, note service.COVNotification) []byte {
	t.Helper()
	var dst []byte
	var err error
	dst, err = bacnet.AppendContextUnsigned(dst, 0, uint64(note.ProcessIdentifier))
	if err != nil {
		t.Fatal(err)
	}
	dst, err = bacnet.AppendContextObjectID(dst, 1, note.InitiatingDevice)
	if err != nil {
		t.Fatal(err)
	}
	dst, err = bacnet.AppendContextObjectID(dst, 2, note.MonitoredObject)
	if err != nil {
		t.Fatal(err)
	}
	dst, err = bacnet.AppendContextUnsigned(dst, 3, uint64(note.TimeRemaining))
	if err != nil {
		t.Fatal(err)
	}
	dst = append(dst, 0x4E) // opening listOfValues
	for _, pv := range note.Values {
		dst, err = bacnet.AppendContextUnsigned(dst, 0, uint64(pv.Property.Identifier))
		if err != nil {
			t.Fatal(err)
		}
		if pv.Property.ArrayIndex != nil {
			dst, err = bacnet.AppendContextUnsigned(dst, 1, uint64(*pv.Property.ArrayIndex))
			if err != nil {
				t.Fatal(err)
			}
		}
		dst = append(dst, 0x2E) // opening propertyValue
		dst, err = bacnet.AppendTag(dst, bacnet.Element{Value: pv.Value})
		if err != nil {
			t.Fatal(err)
		}
		dst = append(dst, 0x2F) // closing propertyValue
	}
	dst = append(dst, 0x4F)
	return dst
}

func TestDecodeCOVNotificationRoundTrip(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	idx := uint32(1)
	note := service.COVNotification{
		ProcessIdentifier: 9,
		InitiatingDevice:  bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 100},
		MonitoredObject:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
		TimeRemaining:     30,
		Values: []service.PropertyValue{{
			Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue, ArrayIndex: &idx},
			Value:    bacnet.RealValue(22.5),
		}},
	}
	raw := encodeCOVNotification(t, note)
	got, err := service.DecodeCOVNotification(raw, limits)
	if err != nil {
		t.Fatal(err)
	}
	if got.ProcessIdentifier != 9 || got.TimeRemaining != 30 || len(got.Values) != 1 {
		t.Fatalf("%+v", got)
	}
	if got.Values[0].Property.Identifier != bacnet.PropertyPresentValue || got.Values[0].Property.ArrayIndex == nil {
		t.Fatalf("property %+v", got.Values[0].Property)
	}
	f, err := bacnet.AsReal(got.Values[0].Value)
	if err != nil || f != 22.5 {
		t.Fatalf("value %v err=%v", got.Values[0].Value, err)
	}
}

func TestDecodeCOVNotificationErrors(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	if _, err := service.DecodeCOVNotification([]byte{0x09, 0x01}, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("missing fields: %v", err)
	}
	// trailing data after a valid-looking truncated sequence
	partial, err := bacnet.AppendContextUnsigned(nil, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	partial = append(partial, 0xFF) // garbage that ParseSequence may reject or trailing
	if _, err := service.DecodeCOVNotification(partial, limits); err == nil {
		t.Fatal("expected error on incomplete notification")
	}
}

func TestSubscribeCOVPropertyWithIncrement(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	inc := float32(0.5)
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 3}
	enc, err := service.EncodeSubscribeCOVProperty(service.SubscribeCOVPropertyRequest{
		SubscribeCOVRequest: service.SubscribeCOVRequest{
			ProcessIdentifier: 4,
			MonitoredObject:   obj,
			IssueConfirmed:    true,
			Lifetime:          120,
		},
		Property:     bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
		COVIncrement: &inc,
	})
	if err != nil {
		t.Fatal(err)
	}
	dec, err := service.DecodeSubscribeCOVProperty(enc, limits)
	if err != nil {
		t.Fatal(err)
	}
	if dec.Cancellation || dec.COVIncrement == nil || *dec.COVIncrement != inc || dec.Lifetime != 120 {
		t.Fatalf("%+v", dec)
	}
}

func TestSubscribeCOVCancellationAndMalformed(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeBinaryValue, Instance: 1}
	enc, err := service.EncodeSubscribeCOV(service.SubscribeCOVRequest{
		ProcessIdentifier: 1, MonitoredObject: obj, Cancellation: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	dec, err := service.DecodeSubscribeCOV(enc, limits)
	if err != nil || !dec.Cancellation {
		t.Fatalf("%+v %v", dec, err)
	}
	if _, err := service.DecodeSubscribeCOV([]byte{0x09, 0x01}, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("malformed: %v", err)
	}
}
