// SPDX-License-Identifier: MIT

package service

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"github.com/otfabric/go-bacnet"
)

func TestVTDataACKCodec(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	count := uint32(3)
	for _, tc := range []struct {
		name string
		ack  VTDataACK
		wire []byte
	}{
		{"all accepted", VTDataACK{AllNewDataAccepted: true}, []byte{0x09, 0x01}},
		{"partly accepted", VTDataACK{AcceptedOctetCount: &count}, []byte{0x09, 0x00, 0x19, 0x03}},
	} {
		raw, err := EncodeVTDataACK(tc.ack)
		if err != nil || !bytes.Equal(raw, tc.wire) {
			t.Fatalf("%s: encoded %x (%v), want %x", tc.name, raw, err, tc.wire)
		}
		got, err := DecodeVTDataACK(tc.wire, limits)
		if err != nil || !reflect.DeepEqual(got, tc.ack) {
			t.Fatalf("%s: decoded %+v (%v), want %+v", tc.name, got, err, tc.ack)
		}
	}
	for name, wire := range map[string][]byte{
		"empty":                {},
		"application boolean":  {0x11},
		"wrong first tag":      {0x19, 0x01},
		"wrong second tag":     {0x09, 0x01, 0x29, 0x03},
		"third element":        {0x09, 0x00, 0x19, 0x03, 0x29, 0x00},
		"count is not a count": {0x09, 0x00, 0x1E, 0x1F},
	} {
		if _, err := DecodeVTDataACK(wire, limits); !errors.Is(err, bacnet.ErrMalformed) {
			t.Errorf("%s: got %v, want ErrMalformed", name, err)
		}
	}
}

// A cancellation carries neither issue-confirmed-notifications nor lifetime.
func TestSubscribeCOVPropertyMultipleCancellation(t *testing.T) {
	lifetime := uint32(60)
	req := SubscribeCOVPropertyMultipleRequest{
		SubscriberProcessIdentifier: 7,
		Cancellation:                true,
		IssueConfirmedNotifications: true, // ignored
		LifetimeRemaining:           &lifetime,
		Subscriptions: []COVMultipleSubscription{{
			Object:     bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
			Properties: []COVPropertyReference{{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}}},
		}},
	}
	raw, err := EncodeSubscribeCOVPropertyMultiple(req)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x09, 0x07, 0x4E, 0x0C, 0x00, 0x80, 0x00, 0x01, 0x1E, 0x0E, 0x09, 0x55, 0x0F, 0x29, 0x00, 0x1F, 0x4F}
	if !bytes.Equal(raw, want) {
		t.Fatalf("encoded %x, want %x", raw, want)
	}
	got, err := DecodeSubscribeCOVPropertyMultiple(raw, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if !got.Cancellation || got.IssueConfirmedNotifications || got.LifetimeRemaining != nil {
		t.Fatalf("decoded %+v, want a cancellation", got)
	}

	// With either field present it is a subscription request.
	req.Cancellation = false
	raw, _ = EncodeSubscribeCOVPropertyMultiple(req)
	if got, err = DecodeSubscribeCOVPropertyMultiple(raw, bacnet.DefaultDecodeLimits()); err != nil || got.Cancellation {
		t.Fatalf("decoded %+v (%v), want a subscription", got, err)
	}
}
