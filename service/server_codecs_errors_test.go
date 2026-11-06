// SPDX-License-Identifier: MIT

package service_test

import (
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestDecodeReadPropertyMultipleFieldErrors(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	objID := []byte{0x0C, 0x00, 0x80, 0x00, 0x01} // [0] AV 1
	cat := func(parts ...[]byte) []byte {
		var out []byte
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	cases := map[string][]byte{
		// A second objectIdentifier before the first one's list.
		"object after object": cat(objID, objID, []byte{0x1E, 0x09, 0x55, 0x1F}),
		// objectIdentifier [0] of three octets.
		"short object identifier": {0x0B, 0x00, 0x80, 0x00, 0x1E, 0x09, 0x55, 0x1F},
		"property over 32 bits":   cat(objID, []byte{0x1E, 0x0D, 0x05, 1, 0, 0, 0, 0, 0x1F}),
		"index over 32 bits":      cat(objID, []byte{0x1E, 0x09, 0x55, 0x1D, 0x05, 1, 0, 0, 0, 0, 0x1F}),
	}
	for name, p := range cases {
		if _, err := service.DecodeReadPropertyMultiple(p, limits); !errors.Is(err, bacnet.ErrMalformed) {
			t.Errorf("%s: % x: %v", name, p, err)
		}
	}
}

func TestGetEnrollmentSummaryFieldErrors(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	ee := bacnet.ObjectIDValue(bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeEventEnrollment, Instance: 1})
	enc := func(vs ...bacnet.ApplicationValue) []byte {
		var out []byte
		for _, v := range vs {
			var err error
			if out, err = bacnet.AppendApplicationValue(out, v); err != nil {
				t.Fatal(err)
			}
		}
		return out
	}
	cases := map[string][]byte{
		"event state not enumerated": enc(ee, bacnet.EnumValue(1), bacnet.RealValue(1), bacnet.UnsignedValue(1)),
		"notification class over 32 bits": enc(ee, bacnet.EnumValue(1), bacnet.EnumValue(0), bacnet.UnsignedValue(1),
			bacnet.UnsignedValue(1<<33)),
	}
	for name, p := range cases {
		if _, err := service.DecodeGetEnrollmentSummaryACK(p, limits); !errors.Is(err, bacnet.ErrMalformed) {
			t.Errorf("%s: %v", name, err)
		}
	}
	got, err := service.DecodeGetEnrollmentSummaryACK(enc(ee, bacnet.EnumValue(1), bacnet.EnumValue(0), bacnet.UnsignedValue(1), bacnet.UnsignedValue(9)), limits)
	if err != nil || len(got.Entries) != 1 || got.Entries[0].NotificationClass != 9 || got.Entries[0].NotificationClassAbsent {
		t.Fatalf("valid entry %+v %v", got, err)
	}
	bad := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: bacnet.MaxObjectInstance + 1}
	if _, err := service.EncodeGetEnrollmentSummary(service.GetEnrollmentSummaryRequest{
		EnrollmentFilter: &service.RecipientProcess{Recipient: service.AuditRecipient{Device: &bad}},
	}); err == nil {
		t.Fatal("enrollment filter with an invalid device encoded")
	}
}
