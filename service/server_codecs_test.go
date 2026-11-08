// SPDX-License-Identifier: MIT

package service_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/otfabric/go-bacnet/internal/capture"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func u32(v uint32) *uint32 { return &v }

func TestDecodeReadPropertyMultipleRoundTrip(t *testing.T) {
	specs := []service.ReadAccessSpecification{
		{
			Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1234},
			Properties: []bacnet.PropertyReference{
				{Identifier: bacnet.PropertyObjectName},
				{Identifier: bacnet.PropertyObjectList, ArrayIndex: u32(0)},
			},
		},
		{
			Object:     bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
			Properties: []bacnet.PropertyReference{{Identifier: bacnet.PropertyAll}},
		},
	}
	p, err := service.EncodeReadPropertyMultiple(specs)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeReadPropertyMultiple(p, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, specs) {
		t.Fatalf("got %+v, want %+v", got, specs)
	}
}

func TestDecodeReadPropertyMultipleMalformed(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	objID := []byte{0x0C, 0x00, 0x80, 0x00, 0x01} // [0] AV 1
	cases := map[string][]byte{
		"empty":                 {},
		"object without list":   objID,
		"list without object":   {0x1E, 0x09, 0x55, 0x1F},
		"empty list":            append(append([]byte{}, objID...), 0x1E, 0x1F),
		"index first":           append(append([]byte{}, objID...), 0x1E, 0x19, 0x01, 0x1F),
		"double index":          append(append([]byte{}, objID...), 0x1E, 0x09, 0x55, 0x19, 0x01, 0x19, 0x02, 0x1F),
		"unknown tag in list":   append(append([]byte{}, objID...), 0x1E, 0x29, 0x55, 0x1F),
		"application in list":   append(append([]byte{}, objID...), 0x1E, 0x21, 0x55, 0x1F),
		"duplicate list":        append(append([]byte{}, objID...), 0x1E, 0x09, 0x55, 0x1F, 0x1E, 0x09, 0x55, 0x1F),
		"second object no list": append(append(append([]byte{}, objID...), 0x1E, 0x09, 0x55, 0x1F), objID...),
		"unknown top tag":       {0x29, 0x01},
		"trailing":              append(append(append([]byte{}, objID...), 0x1E, 0x09, 0x55, 0x1F), 0x0C),
	}
	for name, p := range cases {
		if _, err := service.DecodeReadPropertyMultiple(p, limits); err == nil {
			t.Errorf("%s: decoded % x", name, p)
		}
	}
}

func TestEncodeCOVNotificationRoundTrip(t *testing.T) {
	pri := uint8(8)
	note := service.COVNotification{
		ProcessIdentifier: 7,
		InitiatingDevice:  bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1234},
		MonitoredObject:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
		TimeRemaining:     300,
		Values: []service.PropertyValue{
			{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}, Value: bacnet.RealValue(21.5), Priority: &pri},
			{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyStatusFlags}, Value: bacnet.ApplicationValue{Kind: bacnet.ValueBitString, BitString: bacnet.BitString{UnusedBits: 4, Bytes: []byte{0x00}}}},
		},
	}
	p, err := service.EncodeCOVNotification(note)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeCOVNotification(p, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if got.ProcessIdentifier != 7 || got.InitiatingDevice != note.InitiatingDevice || got.MonitoredObject != note.MonitoredObject ||
		got.TimeRemaining != 300 || len(got.Values) != 2 || got.Values[0].Value.Real != 21.5 ||
		got.Values[0].Priority == nil || *got.Values[0].Priority != 8 {
		t.Fatalf("round trip: %+v", got)
	}
}

func TestEncodeCOVNotificationErrors(t *testing.T) {
	if _, err := service.EncodeCOVNotification(service.COVNotification{}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Errorf("no values: %v", err)
	}
	bad := uint8(17)
	_, err := service.EncodeCOVNotification(service.COVNotification{Values: []service.PropertyValue{{
		Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}, Value: bacnet.RealValue(1), Priority: &bad,
	}}})
	if !errors.Is(err, bacnet.ErrMalformed) {
		t.Errorf("priority 17: %v", err)
	}
}

func FuzzDecodeReadPropertyMultiple(f *testing.F) {
	for _, s := range capture.LayerSeeds(capture.LayerService, capture.ServiceIs([]service.ReadAccessSpecification(nil))) { // recorded devices
		f.Add(s)
	}
	limits := bacnet.DefaultDecodeLimits()
	f.Add([]byte(nil))
	if p, err := service.EncodeReadPropertyMultiple([]service.ReadAccessSpecification{{
		Object:     bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
		Properties: []bacnet.PropertyReference{{Identifier: bacnet.PropertyPresentValue}, {Identifier: bacnet.PropertyPriorityArray, ArrayIndex: u32(8)}},
	}}); err == nil {
		f.Add(p)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		specs, err := service.DecodeReadPropertyMultiple(data, limits)
		if err != nil {
			return
		}
		// Whatever decodes must re-encode and decode to the same value.
		p, err := service.EncodeReadPropertyMultiple(specs)
		if err != nil {
			t.Fatalf("re-encode: %v", err)
		}
		again, err := service.DecodeReadPropertyMultiple(p, limits)
		if err != nil || !reflect.DeepEqual(again, specs) {
			t.Fatalf("round trip: %v %+v != %+v", err, again, specs)
		}
	})
}

func TestGetEnrollmentSummaryEnrollmentFilterRoundTrip(t *testing.T) {
	dev := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 77}
	for _, f := range []service.RecipientProcess{
		{Recipient: service.AuditDevice(dev), ProcessIdentifier: 9},
		{Recipient: service.AuditRecipient{Address: &service.AuditAddress{Network: 5, MAC: []byte{10, 0, 0, 1, 0xBA, 0xC0}}}, ProcessIdentifier: 0xFFFFFFFF},
		{Recipient: service.AuditRecipient{Address: &service.AuditAddress{MAC: []byte{}}}},
	} {
		req := service.GetEnrollmentSummaryRequest{AcknowledgmentFilter: service.AcknowledgmentFilterNotAcked, EnrollmentFilter: &f, EventStateFilter: u32(2)}
		raw, err := service.EncodeGetEnrollmentSummary(req)
		if err != nil {
			t.Fatal(err)
		}
		got, err := service.DecodeGetEnrollmentSummary(raw, bacnet.DefaultDecodeLimits())
		if err != nil {
			t.Fatalf("%x: %v", raw, err)
		}
		if !reflect.DeepEqual(got, req) {
			t.Fatalf("round trip %x:\n got %+v\nwant %+v", raw, got, req)
		}
	}
	// The wire form of clause 13.11.1: [1] { [0] { device [0] }, [1] pid }.
	f := service.RecipientProcess{Recipient: service.AuditDevice(dev), ProcessIdentifier: 9}
	raw, err := service.EncodeGetEnrollmentSummary(service.GetEnrollmentSummaryRequest{EnrollmentFilter: &f})
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x09, 0x00, 0x1E, 0x0E, 0x0C, 0x02, 0x00, 0x00, 0x4D, 0x0F, 0x19, 0x09, 0x1F}
	if !reflect.DeepEqual(raw, want) {
		t.Fatalf("wire %x, want %x", raw, want)
	}
	// A recipient with neither or both choices is not encoded.
	for _, r := range []service.AuditRecipient{{}, {Device: &dev, Address: &service.AuditAddress{}}} {
		if _, err := service.EncodeGetEnrollmentSummary(service.GetEnrollmentSummaryRequest{EnrollmentFilter: &service.RecipientProcess{Recipient: r}}); !errors.Is(err, bacnet.ErrMalformed) {
			t.Fatalf("recipient %+v: %v", r, err)
		}
	}
}

func TestDecodeGetEnrollmentSummaryEnrollmentFilterMalformed(t *testing.T) {
	for name, raw := range map[string][]byte{
		"empty":             {0x09, 0x00, 0x1E, 0x1F},
		"no pid":            {0x09, 0x00, 0x1E, 0x0E, 0x0C, 0x02, 0x00, 0x00, 0x4D, 0x0F, 0x1F},
		"pid tag 2":         {0x09, 0x00, 0x1E, 0x0E, 0x0C, 0x02, 0x00, 0x00, 0x4D, 0x0F, 0x29, 0x09, 0x1F},
		"pid constructed":   {0x09, 0x00, 0x1E, 0x0E, 0x0C, 0x02, 0x00, 0x00, 0x4D, 0x0F, 0x1E, 0x21, 0x09, 0x1F, 0x1F},
		"pid over 32 bits":  {0x09, 0x00, 0x1E, 0x0E, 0x0C, 0x02, 0x00, 0x00, 0x4D, 0x0F, 0x1D, 0x05, 0x01, 0x00, 0x00, 0x00, 0x00, 0x1F},
		"recipient not [0]": {0x09, 0x00, 0x1E, 0x1C, 0x02, 0x00, 0x00, 0x4D, 0x19, 0x09, 0x1F},
		"recipient choice":  {0x09, 0x00, 0x1E, 0x0E, 0x2C, 0x02, 0x00, 0x00, 0x4D, 0x0F, 0x19, 0x09, 0x1F},
		"address shape":     {0x09, 0x00, 0x1E, 0x0E, 0x1E, 0x21, 0x05, 0x1F, 0x0F, 0x19, 0x09, 0x1F},
		"device not objid":  {0x09, 0x00, 0x1E, 0x0E, 0x09, 0x01, 0x0F, 0x19, 0x09, 0x1F},
	} {
		if _, err := service.DecodeGetEnrollmentSummary(raw, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func FuzzDecodeGetEnrollmentSummary(f *testing.F) {
	f.Add([]byte{0x09, 0x00, 0x1E, 0x0E, 0x0C, 0x02, 0x00, 0x00, 0x4D, 0x0F, 0x19, 0x09, 0x1F})
	f.Add([]byte{0x09, 0x02, 0x1E, 0x0E, 0x1E, 0x21, 0x05, 0x65, 0x06, 0x0A, 0x00, 0x00, 0x01, 0xBA, 0xC0, 0x1F, 0x0F, 0x19, 0x01, 0x1F, 0x29, 0x02})
	f.Fuzz(func(t *testing.T, data []byte) {
		req, err := service.DecodeGetEnrollmentSummary(data, bacnet.DefaultDecodeLimits())
		if err != nil {
			return
		}
		raw, err := service.EncodeGetEnrollmentSummary(req)
		if err != nil {
			return
		}
		again, err := service.DecodeGetEnrollmentSummary(raw, bacnet.DefaultDecodeLimits())
		if err != nil || !reflect.DeepEqual(again, req) {
			t.Fatalf("re-decode %x: %+v %v, want %+v", raw, again, err, req)
		}
	})
}
