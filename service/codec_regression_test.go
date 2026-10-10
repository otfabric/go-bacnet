// SPDX-License-Identifier: MIT

package service_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func ptr[T any](v T) *T { return &v }

var regLimits = bacnet.DefaultDecodeLimits()

// BACnetPropertyValue.priority [3] is optional and must not fail the whole
// notification; an entry without a value is malformed.
func TestCOVNotificationPropertyValuePriority(t *testing.T) {
	// pid 1, device 8:1, AV 2:1, time 0, list { [0]85 [2]{real 1.0} [3] 8, [0]111 [2]{bits} }
	p := []byte{0x09, 0x01, 0x1C, 0x02, 0x00, 0x00, 0x01, 0x2C, 0x00, 0x80, 0x00, 0x01, 0x39, 0x00,
		0x4E,
		0x09, 0x55, 0x2E, 0x44, 0x3F, 0x80, 0x00, 0x00, 0x2F, 0x39, 0x08,
		0x09, 0x6F, 0x2E, 0x82, 0x04, 0x00, 0x2F,
		0x4F}
	n, err := service.DecodeCOVNotification(p, regLimits)
	if err != nil {
		t.Fatal(err)
	}
	if len(n.Values) != 2 || n.Values[0].Priority == nil || *n.Values[0].Priority != 8 ||
		n.Values[0].Value.Real != 1 || n.Values[1].Priority != nil ||
		n.Values[1].Property.Identifier != bacnet.PropertyStatusFlags {
		t.Fatalf("%+v", n.Values)
	}
	list := func(body ...byte) []byte {
		out := append([]byte{}, p[:15]...)
		out = append(out, body...)
		return append(out, 0x4F)
	}
	for name, body := range map[string][]byte{
		"entry without value":       {0x09, 0x55},
		"first of two has no value": {0x09, 0x55, 0x09, 0x6F, 0x2E, 0x82, 0x04, 0x00, 0x2F},
		"priority before value":     {0x09, 0x55, 0x39, 0x08, 0x2E, 0x44, 0x3F, 0x80, 0x00, 0x00, 0x2F},
		"priority 0":                {0x09, 0x55, 0x2E, 0x44, 0x3F, 0x80, 0x00, 0x00, 0x2F, 0x39, 0x00},
		"priority 17":               {0x09, 0x55, 0x2E, 0x44, 0x3F, 0x80, 0x00, 0x00, 0x2F, 0x39, 0x11},
		"duplicate priority":        {0x09, 0x55, 0x2E, 0x44, 0x3F, 0x80, 0x00, 0x00, 0x2F, 0x39, 0x08, 0x39, 0x08},
		"duplicate value":           {0x09, 0x55, 0x2E, 0x10, 0x2F, 0x2E, 0x10, 0x2F},
		"value without property":    {0x2E, 0x10, 0x2F},
		"index after value":         {0x09, 0x55, 0x2E, 0x10, 0x2F, 0x19, 0x01},
		"identifier beyond 32 bits": {0x0D, 0x05, 0x01, 0x00, 0x00, 0x00, 0x55, 0x2E, 0x10, 0x2F},
	} {
		if _, err := service.DecodeCOVNotification(list(body...), regLimits); !errors.Is(err, bacnet.ErrMalformed) {
			t.Errorf("%s: err=%v", name, err)
		}
	}
}

func TestCreateObjectInitialValuePriorityRoundTrip(t *testing.T) {
	ot := bacnet.ObjectTypeAnalogValue
	req := service.CreateObjectRequest{ObjectType: &ot, InitialValues: []service.PropertyValue{{
		Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
		Value:    bacnet.RealValue(1),
		Priority: ptr(uint8(8)),
	}}}
	raw, err := service.EncodeCreateObject(req)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x0E, 0x09, 0x02, 0x0F, 0x1E, 0x09, 0x55, 0x2E, 0x44, 0x3F, 0x80, 0x00, 0x00, 0x2F, 0x39, 0x08, 0x1F}
	if !bytes.Equal(raw, want) {
		t.Fatalf("got % X want % X", raw, want)
	}
	got, err := service.DecodeCreateObject(raw, regLimits)
	if err != nil || len(got.InitialValues) != 1 || got.InitialValues[0].Priority == nil || *got.InitialValues[0].Priority != 8 {
		t.Fatalf("%+v err=%v", got, err)
	}
	req.InitialValues[0].Priority = ptr(uint8(17))
	if _, err := service.EncodeCreateObject(req); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("priority 17: %v", err)
	}
}

func TestSubscribeCOVPropertyMultipleStandardLayout(t *testing.T) {
	req := service.SubscribeCOVPropertyMultipleRequest{
		SubscriberProcessIdentifier: 1,
		IssueConfirmedNotifications: true,
		LifetimeRemaining:           ptr(uint32(60)),
		Subscriptions: []service.COVMultipleSubscription{{
			Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogInput, Instance: 1},
			Properties: []service.COVPropertyReference{{
				Property:     bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
				COVIncrement: ptr(float32(1)),
				Timestamped:  true,
			}},
		}},
	}
	raw, err := service.EncodeSubscribeCOVPropertyMultiple(req)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x09, 0x01, 0x19, 0x01, 0x29, 0x3C,
		0x4E, 0x0C, 0x00, 0x00, 0x00, 0x01,
		0x1E, 0x0E, 0x09, 0x55, 0x0F, 0x1C, 0x3F, 0x80, 0x00, 0x00, 0x29, 0x01, 0x1F,
		0x4F}
	if !bytes.Equal(raw, want) {
		t.Fatalf("got  % X\nwant % X", raw, want)
	}
	got, err := service.DecodeSubscribeCOVPropertyMultiple(raw, regLimits)
	if err != nil {
		t.Fatal(err)
	}
	ref := got.Subscriptions[0].Properties[0]
	if got.SubscriberProcessIdentifier != 1 || !got.IssueConfirmedNotifications ||
		got.LifetimeRemaining == nil || *got.LifetimeRemaining != 60 || got.MaxNotificationDelay != nil ||
		ref.Property.Identifier != bacnet.PropertyPresentValue || ref.COVIncrement == nil || *ref.COVIncrement != 1 || !ref.Timestamped {
		t.Fatalf("%+v", got)
	}

	// max-notification-delay [3], an array index, and two references.
	req.MaxNotificationDelay = ptr(uint32(5))
	req.Subscriptions[0].Properties = []service.COVPropertyReference{
		{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPriorityArray, ArrayIndex: ptr(uint32(8))}},
		{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyStatusFlags}, Timestamped: true},
	}
	raw, err = service.EncodeSubscribeCOVPropertyMultiple(req)
	if err != nil {
		t.Fatal(err)
	}
	want = []byte{0x09, 0x01, 0x19, 0x01, 0x29, 0x3C, 0x39, 0x05,
		0x4E, 0x0C, 0x00, 0x00, 0x00, 0x01,
		0x1E,
		0x0E, 0x09, 0x57, 0x19, 0x08, 0x0F, 0x29, 0x00,
		0x0E, 0x09, 0x6F, 0x0F, 0x29, 0x01,
		0x1F,
		0x4F}
	if !bytes.Equal(raw, want) {
		t.Fatalf("got  % X\nwant % X", raw, want)
	}
	got, err = service.DecodeSubscribeCOVPropertyMultiple(raw, regLimits)
	if err != nil || got.MaxNotificationDelay == nil || *got.MaxNotificationDelay != 5 || len(got.Subscriptions[0].Properties) != 2 ||
		got.Subscriptions[0].Properties[0].Property.ArrayIndex == nil || *got.Subscriptions[0].Properties[0].Property.ArrayIndex != 8 ||
		got.Subscriptions[0].Properties[0].Timestamped || !got.Subscriptions[0].Properties[1].Timestamped {
		t.Fatalf("%+v err=%v", got, err)
	}
	// timestamped [2] defaults to FALSE when a peer omits it.
	omitted := []byte{0x09, 0x01, 0x4E, 0x0C, 0x00, 0x00, 0x00, 0x01, 0x1E, 0x0E, 0x09, 0x55, 0x0F, 0x1F, 0x4F}
	got, err = service.DecodeSubscribeCOVPropertyMultiple(omitted, regLimits)
	if err != nil || got.Subscriptions[0].Properties[0].Timestamped || got.LifetimeRemaining != nil {
		t.Fatalf("omitted optionals: %+v err=%v", got, err)
	}
	for name, bad := range map[string][]byte{
		"no list":                {0x09, 0x01, 0x19, 0x01},
		"empty list":             {0x09, 0x01, 0x4E, 0x4F},
		"empty references":       {0x09, 0x01, 0x4E, 0x0C, 0x00, 0x00, 0x00, 0x01, 0x1E, 0x1F, 0x4F},
		"increment before prop":  {0x09, 0x01, 0x4E, 0x0C, 0x00, 0x00, 0x00, 0x01, 0x1E, 0x1C, 0x3F, 0x80, 0x00, 0x00, 0x1F, 0x4F},
		"short increment":        {0x09, 0x01, 0x4E, 0x0C, 0x00, 0x00, 0x00, 0x01, 0x1E, 0x0E, 0x09, 0x55, 0x0F, 0x19, 0x01, 0x1F, 0x4F},
		"both list layouts":      {0x09, 0x01, 0x19, 0x01, 0x3E, 0x0C, 0x00, 0x00, 0x00, 0x01, 0x1E, 0x09, 0x55, 0x39, 0x00, 0x1F, 0x3F, 0x4E, 0x0C, 0x00, 0x00, 0x00, 0x01, 0x1E, 0x0E, 0x09, 0x55, 0x0F, 0x1F, 0x4F},
		"unknown reference tag":  {0x09, 0x01, 0x4E, 0x0C, 0x00, 0x00, 0x00, 0x01, 0x1E, 0x0E, 0x09, 0x55, 0x0F, 0x39, 0x01, 0x1F, 0x4F},
		"duplicate timestamped":  {0x09, 0x01, 0x4E, 0x0C, 0x00, 0x00, 0x00, 0x01, 0x1E, 0x0E, 0x09, 0x55, 0x0F, 0x29, 0x01, 0x29, 0x01, 0x1F, 0x4F},
		"property id beyond u32": {0x09, 0x01, 0x4E, 0x0C, 0x00, 0x00, 0x00, 0x01, 0x1E, 0x0E, 0x0D, 0x05, 0x01, 0x00, 0x00, 0x00, 0x55, 0x0F, 0x1F, 0x4F},
	} {
		if _, err := service.DecodeSubscribeCOVPropertyMultiple(bad, regLimits); !errors.Is(err, bacnet.ErrMalformed) {
			t.Errorf("%s: err=%v", name, err)
		}
	}
	if _, err := service.EncodeSubscribeCOVPropertyMultiple(service.SubscribeCOVPropertyMultipleRequest{
		Subscriptions: []service.COVMultipleSubscription{{Object: bacnet.ObjectIdentifier{Instance: 1}}},
	}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("subscription without properties: %v", err)
	}
}

// The non-standard layout emitted up to v0.3.0 (subscription list at context
// [3], flattened property reference) is rejected; the same request in the
// standard layout round-trips.
func TestSubscribeCOVPropertyMultipleRejectsPreStandardLayout(t *testing.T) {
	old := []byte{0x09, 0x01, 0x19, 0x00, 0x29, 0x3C, 0x3E, 0x0C, 0x00, 0x80, 0x00, 0x01, 0x1E, 0x09, 0x55, 0x39, 0x00, 0x1F, 0x3F}
	if _, err := service.DecodeSubscribeCOVPropertyMultiple(old, regLimits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("pre-standard layout: %v, want ErrMalformed", err)
	}
	std := []byte{0x09, 0x01, 0x19, 0x00, 0x29, 0x3C, 0x4E, 0x0C, 0x00, 0x80, 0x00, 0x01, 0x1E, 0x0E, 0x09, 0x55, 0x0F, 0x29, 0x00, 0x1F, 0x4F}
	req, err := service.DecodeSubscribeCOVPropertyMultiple(std, regLimits)
	if err != nil {
		t.Fatal(err)
	}
	if req.LifetimeRemaining == nil || *req.LifetimeRemaining != 60 || len(req.Subscriptions) != 1 {
		t.Fatalf("%+v", req)
	}
	raw, err := service.EncodeSubscribeCOVPropertyMultiple(req)
	if err != nil || !bytes.Equal(raw, std) {
		t.Fatalf("re-encode: % X err=%v", raw, err)
	}
}

func TestCOVNotificationMultipleTimeOfChange(t *testing.T) {
	// [0]1 [1]device 8:1 [2]0 [4]{ [0]AI:1 [1]{ [0]85 [2]{real 1} [3] 08:00:00.00 } }
	wire := []byte{0x09, 0x01, 0x1C, 0x02, 0x00, 0x00, 0x01, 0x29, 0x00,
		0x4E, 0x0C, 0x00, 0x00, 0x00, 0x01, 0x1E, 0x09, 0x55, 0x2E, 0x44, 0x3F, 0x80, 0x00, 0x00, 0x2F,
		0x3C, 0x08, 0x00, 0x00, 0x00, 0x1F, 0x4F}
	n, err := service.DecodeCOVNotificationMultiple(wire, regLimits)
	if err != nil {
		t.Fatal(err)
	}
	v := n.Objects[0].Values[0]
	if v.TimeOfChange == nil || v.TimeOfChange.Hour != 8 || v.TimeStamp != nil || v.Value.Real != 1 {
		t.Fatalf("%+v", v)
	}
	raw, err := service.EncodeCOVNotificationMultiple(n)
	if err != nil || !bytes.Equal(raw, wire) {
		t.Fatalf("re-encode: % X err=%v", raw, err)
	}
	// The legacy TimeStamp field is emitted as a Time, never as date+time.
	n.Objects[0].Values[0].TimeOfChange = nil
	n.Objects[0].Values[0].TimeStamp = &service.DateTime{Date: bacnet.Date{Year: 126}, Time: bacnet.Time{Hour: 8}}
	raw, err = service.EncodeCOVNotificationMultiple(n)
	if err != nil || !bytes.Equal(raw, wire) {
		t.Fatalf("legacy field: % X err=%v", raw, err)
	}
	// The constructed date+time form emitted up to v0.3.0 is still read.
	legacy := []byte{0x09, 0x01, 0x1C, 0x02, 0x00, 0x00, 0x01, 0x29, 0x00,
		0x4E, 0x0C, 0x00, 0x00, 0x00, 0x01, 0x1E, 0x09, 0x55, 0x2E, 0x44, 0x3F, 0x80, 0x00, 0x00, 0x2F,
		0x3E, 0xA4, 0x7E, 0x01, 0x01, 0x04, 0xB4, 0x09, 0x00, 0x00, 0x00, 0x3F, 0x1F, 0x4F}
	n, err = service.DecodeCOVNotificationMultiple(legacy, regLimits)
	if err != nil {
		t.Fatal(err)
	}
	v = n.Objects[0].Values[0]
	if v.TimeOfChange == nil || v.TimeOfChange.Hour != 9 || v.TimeStamp == nil || v.TimeStamp.Date.Year != 126 {
		t.Fatalf("legacy: %+v", v)
	}
	for name, bad := range map[string][]byte{
		"short time":      {0x09, 0x01, 0x1C, 0x02, 0x00, 0x00, 0x01, 0x29, 0x00, 0x4E, 0x0C, 0x00, 0x00, 0x00, 0x01, 0x1E, 0x09, 0x55, 0x2E, 0x10, 0x2F, 0x39, 0x08, 0x1F, 0x4F},
		"no value":        {0x09, 0x01, 0x1C, 0x02, 0x00, 0x00, 0x01, 0x29, 0x00, 0x4E, 0x0C, 0x00, 0x00, 0x00, 0x01, 0x1E, 0x09, 0x55, 0x1F, 0x4F},
		"duplicate value": {0x09, 0x01, 0x1C, 0x02, 0x00, 0x00, 0x01, 0x29, 0x00, 0x4E, 0x0C, 0x00, 0x00, 0x00, 0x01, 0x1E, 0x09, 0x55, 0x2E, 0x10, 0x2F, 0x2E, 0x10, 0x2F, 0x1F, 0x4F},
		"pid beyond u32":  {0x0D, 0x05, 0x01, 0x00, 0x00, 0x00, 0x01, 0x1C, 0x02, 0x00, 0x00, 0x01, 0x29, 0x00, 0x4E, 0x4F},
	} {
		if _, err := service.DecodeCOVNotificationMultiple(bad, regLimits); !errors.Is(err, bacnet.ErrMalformed) {
			t.Errorf("%s: err=%v", name, err)
		}
	}
}

// Decoded values must own their memory.
func TestDecodedValuesDoNotAliasInput(t *testing.T) {
	covm := []byte{0x09, 0x01, 0x1C, 0x02, 0x00, 0x00, 0x01, 0x29, 0x00,
		0x4E, 0x0C, 0x00, 0x00, 0x00, 0x01, 0x1E, 0x09, 0x55, 0x2E, 0x62, 0xAA, 0xBB, 0x2F, 0x1F, 0x4F}
	n, err := service.DecodeCOVNotificationMultiple(covm, regLimits)
	if err != nil {
		t.Fatal(err)
	}
	alarm := []byte{0xC4, 0x00, 0x00, 0x00, 0x01, 0x91, 0x03, 0x82, 0x05, 0xE0}
	a, err := service.DecodeGetAlarmSummaryACK(alarm, regLimits)
	if err != nil {
		t.Fatal(err)
	}
	// command-failure [3] { [0]{octets AA BB} [1] flags [2]{octets CC} }
	params, _, err := bacnet.ParseSequence([]byte{0x3E, 0x0E, 0x62, 0xAA, 0xBB, 0x0F, 0x1A, 0x04, 0x00, 0x2E, 0x61, 0xCC, 0x2F, 0x3F}, regLimits, -1)
	if err != nil {
		t.Fatal(err)
	}
	cf, err := service.DecodeNotificationParameters(params)
	if err != nil {
		t.Fatal(err)
	}
	for i := range covm {
		covm[i] = 0
	}
	for i := range alarm {
		alarm[i] = 0
	}
	params[0].Value.Elements[0].Value.Elements[0].Value.OctetString[0] = 0
	params[0].Value.Elements[2].Value.Elements[0].Value.OctetString[0] = 0
	if got := n.Objects[0].Values[0].Value.OctetString; !bytes.Equal(got, []byte{0xAA, 0xBB}) {
		t.Fatalf("COVNotificationMultiple value aliases input: % X", got)
	}
	if got := a.Entries[0].AckedTransitions.Bytes; !bytes.Equal(got, []byte{0xE0}) {
		t.Fatalf("AckedTransitions aliases input: % X", got)
	}
	if got := cf.CommandFailure.CommandValue.Elements[0].Value.OctetString; !bytes.Equal(got, []byte{0xAA, 0xBB}) {
		t.Fatalf("command value aliases input: % X", got)
	}
	if got := cf.CommandFailure.FeedbackValue.Elements[0].Value.OctetString; !bytes.Equal(got, []byte{0xCC}) {
		t.Fatalf("feedback value aliases input: % X", got)
	}
}

func TestGetEnrollmentSummaryPriorityFilterAndOptionalClass(t *testing.T) {
	req := service.GetEnrollmentSummaryRequest{PriorityFilter: &[2]uint32{1, 200}}
	raw, err := service.EncodeGetEnrollmentSummary(req)
	if err != nil {
		t.Fatal(err)
	}
	if want := []byte{0x09, 0x00, 0x4E, 0x09, 0x01, 0x19, 0xC8, 0x4F}; !bytes.Equal(raw, want) {
		t.Fatalf("got % X want % X", raw, want)
	}
	got, err := service.DecodeGetEnrollmentSummary(raw, regLimits)
	if err != nil || got.PriorityFilter == nil || *got.PriorityFilter != [2]uint32{1, 200} {
		t.Fatalf("%+v err=%v", got, err)
	}
	if _, err := service.EncodeGetEnrollmentSummary(service.GetEnrollmentSummaryRequest{PriorityFilter: &[2]uint32{0, 256}}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("priority 256: %v", err)
	}
	// The application-tagged form emitted up to v0.3.0 is not the standard's.
	if _, err := service.DecodeGetEnrollmentSummary([]byte{0x09, 0x00, 0x4E, 0x21, 0x01, 0x21, 0xC8, 0x4F}, regLimits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("application-tagged priority filter: %v", err)
	}

	// ACK: first entry without the optional notificationClass, second with it.
	wire := []byte{
		0xC4, 0x00, 0x00, 0x00, 0x01, 0x91, 0x05, 0x91, 0x03, 0x21, 0x64,
		0xC4, 0x00, 0x00, 0x00, 0x02, 0x91, 0x01, 0x91, 0x00, 0x21, 0x0A, 0x21, 0x07,
	}
	ack, err := service.DecodeGetEnrollmentSummaryACK(wire, regLimits)
	if err != nil {
		t.Fatal(err)
	}
	if len(ack.Entries) != 2 || !ack.Entries[0].NotificationClassAbsent || ack.Entries[0].Priority != 100 ||
		ack.Entries[1].NotificationClassAbsent || ack.Entries[1].NotificationClass != 7 || ack.Entries[1].Object.Instance != 2 {
		t.Fatalf("%+v", ack.Entries)
	}
	raw, err = service.EncodeGetEnrollmentSummaryACK(ack)
	if err != nil || !bytes.Equal(raw, wire) {
		t.Fatalf("re-encode: % X err=%v", raw, err)
	}
	for name, bad := range map[string][]byte{
		"truncated entry":   {0xC4, 0x00, 0x00, 0x00, 0x01, 0x91, 0x05, 0x91, 0x03},
		"priority over 255": {0xC4, 0x00, 0x00, 0x00, 0x01, 0x91, 0x05, 0x91, 0x03, 0x22, 0x01, 0x00},
		"context tag":       {0x0C, 0x00, 0x00, 0x00, 0x01, 0x91, 0x05, 0x91, 0x03, 0x21, 0x64},
		"two classes":       {0xC4, 0x00, 0x00, 0x00, 0x01, 0x91, 0x05, 0x91, 0x03, 0x21, 0x64, 0x21, 0x01, 0x21, 0x02},
	} {
		if _, err := service.DecodeGetEnrollmentSummaryACK(bad, regLimits); !errors.Is(err, bacnet.ErrMalformed) {
			t.Errorf("%s: err=%v", name, err)
		}
	}
}

// REAL fields of out-of-range and floating-limit are context-tagged
// primitives; change-of-value new-value is a CHOICE and never loses its value.
func TestNotificationParametersWireEncoding(t *testing.T) {
	enc := func(p service.NotificationParameters) []byte {
		t.Helper()
		els, err := service.EncodeNotificationParameters(p)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := bacnet.AppendContextTagged(nil, 12, els)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	flags := bacnet.BitString{UnusedBits: 4, Bytes: []byte{0x80}}
	oor := service.OutOfRangeParams{ExceedingValue: 81, StatusFlags: flags, Deadband: 1, ExceededLimit: 80}
	got := enc(service.NotificationParameters{OutOfRange: &oor})
	want := []byte{0xCE, 0x5E, 0x0C, 0x42, 0xA2, 0x00, 0x00, 0x1A, 0x04, 0x80, 0x2C, 0x3F, 0x80, 0x00, 0x00, 0x3C, 0x42, 0xA0, 0x00, 0x00, 0x5F, 0xCF}
	if !bytes.Equal(got, want) {
		t.Fatalf("out-of-range: got % X want % X", got, want)
	}
	fl := service.FloatingLimitParams{ReferenceValue: 81, StatusFlags: flags, SetPointValue: 1, ErrorLimit: 80}
	got = enc(service.NotificationParameters{FloatingLimit: &fl})
	want[1], want[len(want)-2] = 0x4E, 0x4F
	if !bytes.Equal(got, want) {
		t.Fatalf("floating-limit: got % X want % X", got, want)
	}
	// Both decode from the standard bytes, and still from the constructed
	// form emitted up to v0.3.0.
	for _, wire := range [][]byte{
		{0x5E, 0x0C, 0x42, 0xA2, 0x00, 0x00, 0x1A, 0x04, 0x80, 0x2C, 0x3F, 0x80, 0x00, 0x00, 0x3C, 0x42, 0xA0, 0x00, 0x00, 0x5F},
		{0x5E, 0x0E, 0x44, 0x42, 0xA2, 0x00, 0x00, 0x0F, 0x1A, 0x04, 0x80, 0x2E, 0x44, 0x3F, 0x80, 0x00, 0x00, 0x2F, 0x3E, 0x44, 0x42, 0xA0, 0x00, 0x00, 0x3F, 0x5F},
	} {
		els, _, err := bacnet.ParseSequence(wire, regLimits, -1)
		if err != nil {
			t.Fatal(err)
		}
		p, err := service.DecodeNotificationParameters(els)
		if err != nil || p.OutOfRange == nil || p.OutOfRange.ExceedingValue != 81 || p.OutOfRange.Deadband != 1 || p.OutOfRange.ExceededLimit != 80 {
			t.Fatalf("decode % X: %+v err=%v", wire, p, err)
		}
	}

	zero := bacnet.BitString{UnusedBits: 4, Bytes: []byte{0x00}}
	got = enc(service.NotificationParameters{ChangeOfValue: &service.ChangeOfValueParams{NewValue: bacnet.RealValue(21.5), StatusFlags: zero}})
	want = []byte{0xCE, 0x2E, 0x0E, 0x1C, 0x41, 0xAC, 0x00, 0x00, 0x0F, 0x1A, 0x04, 0x00, 0x2F, 0xCF}
	if !bytes.Equal(got, want) {
		t.Fatalf("change-of-value real: got % X want % X", got, want)
	}
	bits := bacnet.ApplicationValue{Kind: bacnet.ValueBitString, BitString: bacnet.BitString{UnusedBits: 6, Bytes: []byte{0x40}}}
	got = enc(service.NotificationParameters{ChangeOfValue: &service.ChangeOfValueParams{NewValue: bits, StatusFlags: zero}})
	want = []byte{0xCE, 0x2E, 0x0E, 0x0A, 0x06, 0x40, 0x0F, 0x1A, 0x04, 0x00, 0x2F, 0xCF}
	if !bytes.Equal(got, want) {
		t.Fatalf("change-of-value bits: got % X want % X", got, want)
	}
	// A decoded notification re-encodes to the same bytes.
	els, _, err := bacnet.ParseSequence([]byte{0x2E, 0x0E, 0x1C, 0x41, 0xAC, 0x00, 0x00, 0x0F, 0x1A, 0x04, 0x00, 0x2F}, regLimits, -1)
	if err != nil {
		t.Fatal(err)
	}
	p, err := service.DecodeNotificationParameters(els)
	if err != nil {
		t.Fatal(err)
	}
	p.RawElements = nil
	if got = enc(p); !bytes.Equal(got, []byte{0xCE, 0x2E, 0x0E, 0x1C, 0x41, 0xAC, 0x00, 0x00, 0x0F, 0x1A, 0x04, 0x00, 0x2F, 0xCF}) {
		t.Fatalf("change-of-value round trip: % X", got)
	}
	// A value that is not one of the CHOICE alternatives is an error.
	_, err = service.EncodeNotificationParameters(service.NotificationParameters{ChangeOfValue: &service.ChangeOfValueParams{NewValue: bacnet.UnsignedValue(1), StatusFlags: zero}})
	if !errors.Is(err, bacnet.ErrUnsupported) {
		t.Fatalf("change-of-value unsigned: %v", err)
	}
}

func TestYouAreOptionalFields(t *testing.T) {
	dev := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
	base := []byte{0x21, 0x05, 0x72, 0x00, 0x6D, 0x72, 0x00, 0x73}
	cases := []struct {
		name string
		y    service.YouAre
		wire []byte
	}{
		{"neither", service.YouAre{VendorID: 5, ModelName: "m", SerialNumber: "s"}, base},
		{"device", service.YouAre{VendorID: 5, ModelName: "m", SerialNumber: "s", Device: dev}, append(append([]byte{}, base...), 0xC4, 0x02, 0x00, 0x00, 0x01)},
		{"mac", service.YouAre{VendorID: 5, ModelName: "m", SerialNumber: "s", MAC: []byte{0x0A}}, append(append([]byte{}, base...), 0x61, 0x0A)},
		{"device and mac", service.YouAre{VendorID: 5, ModelName: "m", SerialNumber: "s", Device: dev, MAC: []byte{0x0A}}, append(append([]byte{}, base...), 0xC4, 0x02, 0x00, 0x00, 0x01, 0x61, 0x0A)},
	}
	for _, tc := range cases {
		raw, err := service.EncodeYouAre(tc.y)
		if err != nil || !bytes.Equal(raw, tc.wire) {
			t.Fatalf("%s encode: % X err=%v", tc.name, raw, err)
		}
		got, err := service.DecodeYouAre(tc.wire, regLimits)
		if err != nil || got.VendorID != 5 || got.Device != tc.y.Device || !bytes.Equal(got.MAC, tc.y.MAC) || got.HasDevice() != tc.y.HasDevice() {
			t.Fatalf("%s decode: %+v err=%v", tc.name, got, err)
		}
	}
	for name, extra := range map[string][]byte{
		"mac then device":  {0x61, 0x0A, 0xC4, 0x02, 0x00, 0x00, 0x01},
		"two devices":      {0xC4, 0x02, 0x00, 0x00, 0x01, 0xC4, 0x02, 0x00, 0x00, 0x02},
		"unsigned":         {0x21, 0x01},
		"data after a mac": {0x61, 0x0A, 0x21, 0x01},
	} {
		_, err := service.DecodeYouAre(append(append([]byte{}, base...), extra...), regLimits)
		if !errors.Is(err, bacnet.ErrMalformed) && !errors.Is(err, bacnet.ErrTrailingData) {
			t.Errorf("%s: err=%v", name, err)
		}
	}
}

func TestWritePropertyPriorityRange(t *testing.T) {
	req := service.WritePropertyRequest{
		Object:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogOutput, Instance: 1},
		Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
		Value:    bacnet.RealValue(1),
	}
	for _, p := range []uint8{0, 17, 255} {
		req.Priority = ptr(p)
		if _, err := service.EncodeWriteProperty(req); !errors.Is(err, bacnet.ErrMalformed) {
			t.Fatalf("priority %d: err=%v", p, err)
		}
	}
	for _, p := range []uint8{1, 16} {
		req.Priority = ptr(p)
		raw, err := service.EncodeWriteProperty(req)
		if err != nil || raw[len(raw)-2] != 0x49 || raw[len(raw)-1] != p {
			t.Fatalf("priority %d: % X err=%v", p, raw, err)
		}
	}
	dup := []byte{0x0C, 0x00, 0x40, 0x00, 0x01, 0x19, 0x55, 0x3E, 0x44, 0x3F, 0x80, 0x00, 0x00, 0x3F, 0x49, 0x08, 0x49, 0x08}
	if _, err := service.DecodeWriteProperty(dup, regLimits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("duplicate priority: %v", err)
	}
}

// Identifiers and indexes wider than 32 bits are rejected, never truncated.
func TestDecodersRejectValuesBeyond32Bits(t *testing.T) {
	// 0x01_0000_0055 would truncate to 85 (present-value).
	wide := []byte{0x05, 0x01, 0x00, 0x00, 0x00, 0x55}
	tagged := func(tag byte) []byte { return append([]byte{tag<<4 | 0x0D}, wide...) }
	obj := []byte{0x0C, 0x00, 0x00, 0x00, 0x01}
	val := []byte{0x44, 0x3F, 0x80, 0x00, 0x00}
	cat := func(parts ...[]byte) []byte {
		var out []byte
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	rpAck := func(prop, idx []byte) []byte {
		return cat(obj, prop, idx, []byte{0x3E}, val, []byte{0x3F})
	}
	if _, err := service.DecodeReadPropertyACK(rpAck(tagged(1), nil), regLimits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("ReadPropertyACK property: %v", err)
	}
	if _, err := service.DecodeReadPropertyACK(rpAck([]byte{0x19, 0x55}, tagged(2)), regLimits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("ReadPropertyACK index: %v", err)
	}
	if _, err := service.DecodeReadPropertyACK(rpAck([]byte{0x19, 0x55}, []byte{0x29, 0x01, 0x29, 0x02}), regLimits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("ReadPropertyACK duplicate index: %v", err)
	}
	if ack, err := service.DecodeReadPropertyACK(rpAck([]byte{0x19, 0x55}, []byte{0x29, 0x01}), regLimits); err != nil || *ack.Property.ArrayIndex != 1 {
		t.Fatalf("ReadPropertyACK valid: %+v err=%v", ack, err)
	}
	if _, err := service.DecodeWriteProperty(rpAck(tagged(1), nil), regLimits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("WriteProperty property: %v", err)
	}
	if _, err := service.DecodeWriteProperty(rpAck([]byte{0x19, 0x55}, tagged(2)), regLimits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("WriteProperty index: %v", err)
	}
	rpm := func(prop, idx []byte) []byte {
		return cat(obj, []byte{0x1E}, prop, idx, []byte{0x4E}, val, []byte{0x4F, 0x1F})
	}
	if _, err := service.DecodeReadPropertyMultipleACK(rpm(tagged(2), nil), regLimits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("RPM ACK property: %v", err)
	}
	if _, err := service.DecodeReadPropertyMultipleACK(rpm([]byte{0x29, 0x55}, tagged(3)), regLimits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("RPM ACK index: %v", err)
	}
	if res, err := service.DecodeReadPropertyMultipleACK(rpm([]byte{0x29, 0x55}, []byte{0x39, 0x02}), regLimits); err != nil || *res[0].Properties[0].Property.ArrayIndex != 2 {
		t.Fatalf("RPM ACK valid: %+v err=%v", res, err)
	}
}

// An empty list or array property is an empty propertyValue wrapper on the
// wire (3E 3F). These are the ACKs bacnet-stack, BACnet4J and BACpypes3 send
// for Device_Address_Binding (30) and Active_COV_Subscriptions (152) of a
// device that has neither; up to v0.3.0 they were refused as malformed, and a
// ReadPropertyMultiple reply containing one was lost whole.
func TestEmptyListPropertyValues(t *testing.T) {
	dev := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1234}
	for name, raw := range map[string][]byte{
		"device-address-binding":   {0x0C, 0x02, 0x00, 0x04, 0xD2, 0x19, 0x1E, 0x3E, 0x3F},
		"active-cov-subscriptions": {0x0C, 0x02, 0x00, 0x04, 0xD2, 0x19, 0x98, 0x3E, 0x3F},
	} {
		ack, err := service.DecodeReadPropertyACK(raw, regLimits)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if ack.Object != dev || ack.Value.Kind != bacnet.ValueConstructed || len(ack.Value.Elements) != 0 {
			t.Fatalf("%s: %+v", name, ack)
		}
		// It encodes back to the same bytes.
		again, err := service.EncodeReadPropertyACK(ack)
		if err != nil || !bytes.Equal(again, raw) {
			t.Fatalf("%s re-encode: % X err=%v", name, again, err)
		}
	}

	// One object with object-name "AV" and an empty list: the name must
	// survive next to the empty value.
	rpm := []byte{
		0x0C, 0x02, 0x00, 0x04, 0xD2, 0x1E,
		0x29, 0x4D, 0x4E, 0x75, 0x03, 0x00, 0x41, 0x56, 0x4F, // object-name = "AV"
		0x29, 0x1E, 0x4E, 0x4F, // device-address-binding = {}
		0x1F,
	}
	results, err := service.DecodeReadPropertyMultipleACK(rpm, regLimits)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || len(results[0].Properties) != 2 {
		t.Fatalf("%+v", results)
	}
	name, list := results[0].Properties[0], results[0].Properties[1]
	if name.Err != nil || name.Value.Character.Value != "AV" {
		t.Fatalf("object-name: %+v", name)
	}
	if list.Err != nil || list.Value.Kind != bacnet.ValueConstructed || len(list.Value.Elements) != 0 {
		t.Fatalf("empty list: %+v", list)
	}

	// A COV notification may carry an empty list value too.
	cov := []byte{0x09, 0x01, 0x1C, 0x02, 0x00, 0x04, 0xD2, 0x2C, 0x00, 0x80, 0x00, 0x01, 0x39, 0x0A,
		0x4E, 0x09, 0x1E, 0x2E, 0x2F, 0x4F}
	note, err := service.DecodeCOVNotification(cov, regLimits)
	if err != nil {
		t.Fatal(err)
	}
	if len(note.Values) != 1 || note.Values[0].Value.Kind != bacnet.ValueConstructed || len(note.Values[0].Value.Elements) != 0 {
		t.Fatalf("%+v", note.Values)
	}
}
