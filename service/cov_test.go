// SPDX-License-Identifier: MIT

package service_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestSubscribeCOVContextBoolEncoding(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 7}

	for _, confirmed := range []bool{true, false} {
		enc, err := service.EncodeSubscribeCOV(service.SubscribeCOVRequest{
			ProcessIdentifier: 1,
			MonitoredObject:   obj,
			IssueConfirmed:    confirmed,
			Lifetime:          60,
		})
		if err != nil {
			t.Fatal(err)
		}
		// Context boolean is tag 2 with one value octet 0x00 or 0x01 (not LVT-only).
		found := false
		for i := 0; i+1 < len(enc); i++ {
			if enc[i] == 0x29 { // context tag 2, length 1
				found = true
				want := byte(0)
				if confirmed {
					want = 1
				}
				if enc[i+1] != want {
					t.Fatalf("confirmed=%v context bool octet %#x want %#x (frame %x)", confirmed, enc[i+1], want, enc)
				}
				break
			}
		}
		if !found {
			t.Fatalf("confirmed=%v missing context bool tag in %x", confirmed, enc)
		}
		dec, err := service.DecodeSubscribeCOV(enc, limits)
		if err != nil {
			t.Fatal(err)
		}
		if dec.IssueConfirmed != confirmed || dec.Cancellation {
			t.Fatalf("%+v", dec)
		}
	}
}

func TestSubscribeCOVPropertyCancellationIncludesProperty(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 7}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	enc, err := service.EncodeSubscribeCOVProperty(service.SubscribeCOVPropertyRequest{
		SubscribeCOVRequest: service.SubscribeCOVRequest{
			ProcessIdentifier: 2,
			MonitoredObject:   obj,
			Cancellation:      true,
		},
		Property: prop,
	})
	if err != nil {
		t.Fatal(err)
	}
	dec, err := service.DecodeSubscribeCOVProperty(enc, limits)
	if err != nil {
		t.Fatal(err)
	}
	if !dec.Cancellation || !dec.Property.Equal(prop) {
		t.Fatalf("%+v", dec)
	}
	// Must not look like a plain SubscribeCOV cancellation (missing property).
	if _, err := service.DecodeSubscribeCOV(enc, limits); err == nil {
		t.Fatal("expected plain SubscribeCOV decode to reject property wrapper")
	}
}
