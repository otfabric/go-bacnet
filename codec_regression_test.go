// SPDX-License-Identifier: MIT

package bacnet_test

import (
	"bytes"
	"errors"
	"math"
	"testing"

	"github.com/otfabric/go-bacnet"
)

func mustAppend(t *testing.T, v bacnet.ApplicationValue) []byte {
	t.Helper()
	b, err := bacnet.AppendApplicationValue(nil, v)
	if err != nil {
		t.Fatalf("AppendApplicationValue: %v", err)
	}
	return b
}

func mustParse(t *testing.T, wire []byte) bacnet.ApplicationValue {
	t.Helper()
	els, n, err := bacnet.ParseSequence(wire, bacnet.DefaultDecodeLimits(), -1)
	if err != nil || n != len(wire) {
		t.Fatalf("ParseSequence(% X): n=%d err=%v", wire, n, err)
	}
	return bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: els}
}

// A context element with a typed primitive value must carry that value's
// content octets instead of an empty tag.
func TestAppendTagContextTypedValues(t *testing.T) {
	oid := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
	cases := []struct {
		name string
		el   bacnet.Element
		want []byte
	}{
		{"real", bacnet.Element{Context: true, TagNumber: 0, Value: bacnet.RealValue(1.5)}, []byte{0x0C, 0x3F, 0xC0, 0x00, 0x00}},
		{"unsigned", bacnet.Element{Context: true, TagNumber: 2, Value: bacnet.UnsignedValue(47808)}, []byte{0x2A, 0xBA, 0xC0}},
		{"signed", bacnet.Element{Context: true, TagNumber: 1, Value: bacnet.SignedValue(-1)}, []byte{0x19, 0xFF}},
		{"boolean", bacnet.Element{Context: true, TagNumber: 3, Value: bacnet.BoolValue(true)}, []byte{0x39, 0x01}},
		{"enumerated", bacnet.Element{Context: true, TagNumber: 4, Value: bacnet.EnumValue(3)}, []byte{0x49, 0x03}},
		{"null", bacnet.Element{Context: true, TagNumber: 5, Value: bacnet.NullValue()}, []byte{0x58}},
		{"date", bacnet.Element{Context: true, TagNumber: 0, Value: bacnet.DateValue(bacnet.Date{Year: 126, Month: 10, Day: 10, Weekday: 6})}, []byte{0x0C, 0x7E, 0x0A, 0x0A, 0x06}},
		{"time", bacnet.Element{Context: true, TagNumber: 3, Value: bacnet.TimeValue(bacnet.Time{Hour: 8})}, []byte{0x3C, 0x08, 0x00, 0x00, 0x00}},
		{"object-id", bacnet.Element{Context: true, TagNumber: 1, Value: bacnet.ObjectIDValue(oid)}, []byte{0x1C, 0x02, 0x00, 0x00, 0x01}},
		{"double", bacnet.Element{Context: true, TagNumber: 0, Value: bacnet.DoubleValue(1)}, []byte{0x0D, 0x08, 0x3F, 0xF0, 0, 0, 0, 0, 0, 0}},
		{"character-string", bacnet.Element{Context: true, TagNumber: 2, Value: bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: "ab"}}}, []byte{0x2B, 0x00, 'a', 'b'}},
		{"bit-string", bacnet.Element{Context: true, TagNumber: 1, Value: bacnet.ApplicationValue{Kind: bacnet.ValueBitString, BitString: bacnet.BitString{UnusedBits: 4, Bytes: []byte{0x80}}}}, []byte{0x1A, 0x04, 0x80}},
		{"octet-string", bacnet.Element{Context: true, TagNumber: 1, Value: bacnet.ApplicationValue{Kind: bacnet.ValueOctetString, OctetString: []byte{1, 2}}}, []byte{0x1A, 0x01, 0x02}},
		{"raw-context", bacnet.Element{Context: true, TagNumber: 1, Value: bacnet.ApplicationValue{Kind: bacnet.ValueContext, OctetString: []byte{9}}}, []byte{0x19, 0x09}},
	}
	for _, tc := range cases {
		got, err := bacnet.AppendTag(nil, tc.el)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if !bytes.Equal(got, tc.want) {
			t.Fatalf("%s: got % X want % X", tc.name, got, tc.want)
		}
	}
	// A context "primitive" that carries nested elements cannot be encoded.
	_, err := bacnet.AppendTag(nil, bacnet.Element{Context: true, Value: bacnet.ApplicationValue{
		Kind: bacnet.ValueContext, Elements: []bacnet.Element{{Value: bacnet.NullValue()}},
	}})
	if !errors.Is(err, bacnet.ErrUnsupported) {
		t.Fatalf("nested context primitive: err=%v", err)
	}
}

func TestContextRealAndUnsigned32Helpers(t *testing.T) {
	raw, err := bacnet.AppendContextReal(nil, 2, 1)
	if err != nil || !bytes.Equal(raw, []byte{0x2C, 0x3F, 0x80, 0x00, 0x00}) {
		t.Fatalf("AppendContextReal: % X err=%v", raw, err)
	}
	el, _, err := bacnet.ParseTag(raw, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if f, err := bacnet.ContextReal(el); err != nil || f != 1 {
		t.Fatalf("ContextReal: %v err=%v", f, err)
	}
	if _, err := bacnet.ContextReal(bacnet.Element{Context: true, Value: bacnet.ApplicationValue{Kind: bacnet.ValueContext, OctetString: []byte{1}}}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("ContextReal short: %v", err)
	}
	if _, err := bacnet.ContextDate(bacnet.Element{Context: true, Value: bacnet.ApplicationValue{Kind: bacnet.ValueContext, OctetString: []byte{1}}}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("ContextDate short: %v", err)
	}
	// 0x01_0000_0055 must not be truncated to 85.
	big, _, err := bacnet.ParseTag([]byte{0x1D, 0x05, 0x01, 0x00, 0x00, 0x00, 0x55}, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bacnet.ContextUnsigned32(big); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("ContextUnsigned32 overflow: %v", err)
	}
	ok, _, _ := bacnet.ParseTag([]byte{0x1C, 0xFF, 0xFF, 0xFF, 0xFF}, bacnet.DefaultDecodeLimits())
	if u, err := bacnet.ContextUnsigned32(ok); err != nil || u != math.MaxUint32 {
		t.Fatalf("ContextUnsigned32 max: %d err=%v", u, err)
	}
}

func TestParseTagStrictness(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	bad := map[string][]byte{
		"boolean LVT 5":                 {0x15},
		"boolean LVT 2":                 {0x12},
		"boolean LVT 6":                 {0x16},
		"extended tag number below 15":  {0xF9, 0x05, 0x00},
		"extended tag number 255":       {0xF9, 0xFF, 0x00},
		"extended application tag 1":    {0xF1, 0x01, 0xAA},
		"extended opening tag 255":      {0xFE, 0xFF},
		"bit string 9 unused bits":      {0x82, 0x09, 0xFF},
		"empty bit string, unused bits": {0x81, 0x03},
		"32-bit length beyond input":    {0x65, 0xFF, 0x7F, 0xFF, 0xFF, 0xFF, 0x00},
	}
	for name, in := range bad {
		if _, _, err := bacnet.ParseTag(in, limits); !errors.Is(err, bacnet.ErrMalformed) {
			t.Errorf("%s (% X): err=%v, want ErrMalformed", name, in, err)
		}
	}
	for _, in := range [][]byte{{0x10}, {0x11}} {
		el, n, err := bacnet.ParseTag(in, limits)
		if err != nil || n != 1 || el.Value.Kind != bacnet.ValueBoolean || el.Value.Boolean != (in[0] == 0x11) {
			t.Fatalf("boolean % X: %+v n=%d err=%v", in, el, n, err)
		}
	}
	// Extended context tag numbers 15..254 remain valid.
	el, n, err := bacnet.ParseTag([]byte{0xF9, 0x0F, 0x07}, limits)
	if err != nil || n != 3 || !el.Context || el.TagNumber != 15 {
		t.Fatalf("extended tag 15: %+v n=%d err=%v", el, n, err)
	}
	// Valid bit strings: empty, and 0..7 unused bits with data.
	for _, in := range [][]byte{{0x81, 0x00}, {0x82, 0x07, 0x80}} {
		if _, _, err := bacnet.ParseTag(in, limits); err != nil {
			t.Fatalf("bit string % X: %v", in, err)
		}
	}
	// Context bit strings are held to the same rule.
	ctx, _, err := bacnet.ParseTag([]byte{0x1A, 0x09, 0xFF}, limits)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bacnet.ContextBitString(ctx); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("ContextBitString unused=9: %v", err)
	}
}

func TestEncodeRejectsInvalidTagsAndBitStrings(t *testing.T) {
	bs := bacnet.BitString{UnusedBits: 8, Bytes: []byte{0xFF}}
	if _, err := bacnet.AppendApplicationValue(nil, bacnet.ApplicationValue{Kind: bacnet.ValueBitString, BitString: bs}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("application bit string: %v", err)
	}
	if _, err := bacnet.AppendContextBitString(nil, 1, bacnet.BitString{UnusedBits: 1}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("context bit string: %v", err)
	}
	if _, err := bacnet.AppendContextUnsigned(nil, 255, 1); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("context tag 255: %v", err)
	}
	if _, err := bacnet.AppendContextTagged(nil, 255, nil); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("constructed tag 255: %v", err)
	}
	if _, err := bacnet.AppendTag(nil, bacnet.Element{Context: true, TagNumber: 255, Opening: true}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("opening tag 255: %v", err)
	}
}

func TestCharacterStringText(t *testing.T) {
	cases := []struct {
		name string
		cs   bacnet.CharacterString
		want string
	}{
		{"utf-8", bacnet.CharacterString{Encoding: bacnet.CharacterEncodingUTF8, Value: "Zone Température"}, "Zone Température"},
		{"ucs-2", bacnet.CharacterString{Encoding: bacnet.CharacterEncodingUCS2, Value: "\x00A\x00\xE9\x20\xAC"}, "Aé€"},
		{"ucs-4", bacnet.CharacterString{Encoding: bacnet.CharacterEncodingUCS4, Value: "\x00\x00\x00A\x00\x01\xF6\x00"}, "A\U0001F600"},
		{"iso-8859-1", bacnet.CharacterString{Encoding: bacnet.CharacterEncodingISO88591, Value: "caf\xE9"}, "café"},
		{"empty", bacnet.CharacterString{Encoding: bacnet.CharacterEncodingUCS2}, ""},
	}
	for _, tc := range cases {
		got, err := tc.cs.Text()
		if err != nil || got != tc.want {
			t.Fatalf("%s: %q err=%v", tc.name, got, err)
		}
	}
	for _, enc := range []uint8{bacnet.CharacterEncodingDBCS, bacnet.CharacterEncodingJIS, 6, 255} {
		if _, err := (bacnet.CharacterString{Encoding: enc, Value: "x"}).Text(); !errors.Is(err, bacnet.ErrUnsupported) {
			t.Fatalf("encoding %d: err=%v", enc, err)
		}
	}
	malformed := []bacnet.CharacterString{
		{Encoding: bacnet.CharacterEncodingUTF8, Value: "\xFF\xFE"},
		{Encoding: bacnet.CharacterEncodingUCS2, Value: "\x00"},
		{Encoding: bacnet.CharacterEncodingUCS4, Value: "\x00\x00\x00"},
		{Encoding: bacnet.CharacterEncodingUCS4, Value: "\x00\x11\x00\x00"},
		{Encoding: bacnet.CharacterEncodingUCS4, Value: "\x00\x00\xD8\x00"},
	}
	for _, cs := range malformed {
		if _, err := cs.Text(); !errors.Is(err, bacnet.ErrMalformed) {
			t.Fatalf("%+q: err=%v", cs, err)
		}
	}
	// The decoder keeps the raw octets; Text is the conversion.
	el, _, err := bacnet.ParseTag([]byte{0x75, 0x03, 0x04, 0x00, 0x41}, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if el.Value.Character.Value != "\x00A" {
		t.Fatalf("raw value %+q", el.Value.Character.Value)
	}
	if s, err := el.Value.Character.Text(); err != nil || s != "A" {
		t.Fatalf("Text: %q err=%v", s, err)
	}
}

func TestCalendarEntryWire(t *testing.T) {
	date := bacnet.CalendarEntry{Choice: bacnet.CalendarEntryDate, Date: bacnet.Date{Year: 126, Month: 10, Day: 10, Weekday: 6}}
	rng := bacnet.CalendarEntry{Choice: bacnet.CalendarEntryDateRange, DateRange: bacnet.DateRange{
		Start: bacnet.Date{Year: 126, Month: 1, Day: 1, Weekday: 4}, End: bacnet.Date{Year: 126, Month: 1, Day: 2, Weekday: 5},
	}}
	wnd := bacnet.CalendarEntry{Choice: bacnet.CalendarEntryWeekNDay, WeekNDay: bacnet.WeekNDay{Month: 0xFF, WeekOfMonth: 1, DayOfWeek: 5}}
	cases := []struct {
		entry bacnet.CalendarEntry
		wire  []byte
	}{
		{date, []byte{0x0C, 0x7E, 0x0A, 0x0A, 0x06}},
		{rng, []byte{0x1E, 0xA4, 0x7E, 0x01, 0x01, 0x04, 0xA4, 0x7E, 0x01, 0x02, 0x05, 0x1F}},
		{wnd, []byte{0x2B, 0xFF, 0x01, 0x05}},
	}
	var list []byte
	for _, tc := range cases {
		v, err := bacnet.EncodeCalendarEntry(tc.entry)
		if err != nil {
			t.Fatal(err)
		}
		if got := mustAppend(t, v); !bytes.Equal(got, tc.wire) {
			t.Fatalf("encode choice %d: got % X want % X", tc.entry.Choice, got, tc.wire)
		}
		got, err := bacnet.DecodeCalendarEntry(mustParse(t, tc.wire))
		if err != nil || got != tc.entry {
			t.Fatalf("decode choice %d: %+v err=%v", tc.entry.Choice, got, err)
		}
		list = append(list, tc.wire...)
	}
	// Date_List as ReadProperty delivers it: SEQUENCE OF BACnetCalendarEntry.
	out, ok, err := bacnet.DecodePropertyValue(bacnet.ObjectTypeCalendar, bacnet.PropertyDateList, mustParse(t, list))
	if err != nil || !ok {
		t.Fatalf("Date_List: ok=%v err=%v", ok, err)
	}
	entries := out.([]bacnet.CalendarEntry)
	if len(entries) != 3 || entries[0] != date || entries[1] != rng || entries[2] != wnd {
		t.Fatalf("Date_List entries: %+v", entries)
	}
	// A wrong-shaped date is malformed, not unsupported.
	if _, err := bacnet.DecodeCalendarEntry(mustParse(t, []byte{0x09, 0x01})); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("short date: %v", err)
	}
	if _, _, err := bacnet.DecodePropertyValue(bacnet.ObjectTypeCalendar, bacnet.PropertyDateList, mustParse(t, []byte{0x39, 0x01})); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("Date_List unknown tag: %v", err)
	}
}

func TestHostNPortWire(t *testing.T) {
	cases := []struct {
		h    bacnet.HostNPort
		wire []byte
	}{
		{bacnet.HostNPort{IP: [4]byte{192, 168, 1, 1}, Port: 47808}, []byte{0x0E, 0x1C, 0xC0, 0xA8, 0x01, 0x01, 0x0F, 0x1A, 0xBA, 0xC0}},
		{bacnet.HostNPort{Name: "bbmd", Port: 47808}, []byte{0x0E, 0x2D, 0x05, 0x00, 'b', 'b', 'm', 'd', 0x0F, 0x1A, 0xBA, 0xC0}},
		{bacnet.HostNPort{None: true, Port: 0}, []byte{0x0E, 0x08, 0x0F, 0x19, 0x00}},
	}
	for _, tc := range cases {
		if got := mustAppend(t, bacnet.EncodeHostNPort(tc.h)); !bytes.Equal(got, tc.wire) {
			t.Fatalf("encode %+v: got % X want % X", tc.h, got, tc.wire)
		}
		got, err := bacnet.DecodeHostNPort(mustParse(t, tc.wire))
		if err != nil || got != tc.h {
			t.Fatalf("decode % X: %+v err=%v", tc.wire, got, err)
		}
		// In-memory round trip without going through the wire.
		got, err = bacnet.DecodeHostNPort(bacnet.EncodeHostNPort(tc.h))
		if err != nil || got != tc.h {
			t.Fatalf("in-memory %+v: %+v err=%v", tc.h, got, err)
		}
	}
	// 16-octet (IPv6) addresses are well-formed but not representable.
	v6 := append([]byte{0x0E, 0x1D, 0x10}, make([]byte, 16)...)
	v6 = append(v6, 0x0F, 0x1A, 0xBA, 0xC0)
	if _, err := bacnet.DecodeHostNPort(mustParse(t, v6)); !errors.Is(err, bacnet.ErrUnsupported) {
		t.Fatalf("ipv6: %v", err)
	}
	for name, wire := range map[string][]byte{
		"missing port":   {0x0E, 0x1C, 1, 2, 3, 4, 0x0F},
		"bare host":      {0x0C, 1, 2, 3, 4, 0x1A, 0xBA, 0xC0},
		"unknown choice": {0x0E, 0x39, 0x01, 0x0F, 0x1A, 0xBA, 0xC0},
		"port overflow":  {0x0E, 0x1C, 1, 2, 3, 4, 0x0F, 0x1B, 0x01, 0x00, 0x00},
		"none with data": {0x0E, 0x09, 0x01, 0x0F, 0x19, 0x00},
	} {
		if _, err := bacnet.DecodeHostNPort(mustParse(t, wire)); !errors.Is(err, bacnet.ErrMalformed) {
			t.Fatalf("%s: err=%v", name, err)
		}
	}
}

func TestScheduleWireRoundTrip(t *testing.T) {
	day := bacnet.DailySchedule{DaySchedule: []bacnet.ScheduledTimeValue{
		{Time: bacnet.Time{Hour: 8}, Value: bacnet.RealValue(21)},
		{Time: bacnet.Time{Hour: 17}, Value: bacnet.NullValue()},
	}}
	wire := mustAppend(t, bacnet.EncodeDailySchedule(day))
	want := []byte{0x0E, 0xB4, 0x08, 0x00, 0x00, 0x00, 0x44, 0x41, 0xA8, 0x00, 0x00, 0xB4, 0x11, 0x00, 0x00, 0x00, 0x00, 0x0F}
	if !bytes.Equal(wire, want) {
		t.Fatalf("DailySchedule wire: % X", wire)
	}
	got, err := bacnet.DecodeDailySchedule(mustParse(t, wire))
	if err != nil || len(got.DaySchedule) != 2 || got.DaySchedule[0].Time.Hour != 8 ||
		got.DaySchedule[0].Value.Real != 21 || got.DaySchedule[1].Value.Kind != bacnet.ValueNull {
		t.Fatalf("DailySchedule decode: %+v err=%v", got, err)
	}

	var week bacnet.WeeklySchedule
	week[0] = day
	week[6] = bacnet.DailySchedule{DaySchedule: []bacnet.ScheduledTimeValue{{Time: bacnet.Time{Hour: 1}, Value: bacnet.EnumValue(1)}}}
	weekWire := mustAppend(t, bacnet.EncodeWeeklySchedule(week))
	gotWeek, err := bacnet.DecodeWeeklySchedule(mustParse(t, weekWire))
	if err != nil {
		t.Fatalf("WeeklySchedule decode: %v", err)
	}
	if len(gotWeek[0].DaySchedule) != 2 || len(gotWeek[1].DaySchedule) != 0 || len(gotWeek[6].DaySchedule) != 1 ||
		gotWeek[6].DaySchedule[0].Value.Enumerated != 1 {
		t.Fatalf("WeeklySchedule: %+v", gotWeek)
	}
	// A time without a value and a value without a time are malformed.
	for name, bad := range map[string][]byte{
		"dangling time": {0x0E, 0xB4, 0x08, 0x00, 0x00, 0x00, 0x0F},
		"value first":   {0x0E, 0x44, 0x41, 0xA8, 0x00, 0x00, 0xB4, 0x08, 0x00, 0x00, 0x00, 0x0F},
		"context value": {0x0E, 0xB4, 0x08, 0x00, 0x00, 0x00, 0x09, 0x01, 0x0F},
	} {
		if _, err := bacnet.DecodeDailySchedule(mustParse(t, bad)); !errors.Is(err, bacnet.ErrMalformed) {
			t.Fatalf("%s: err=%v", name, err)
		}
	}
}

func TestLifeSafetyEnumerationValues(t *testing.T) {
	// ASHRAE 135 Clause 21 numbering.
	if bacnet.LifeSafetyModeUnmanned != 4 || bacnet.LifeSafetyModeArmed != 5 || bacnet.LifeSafetyModeDisarmed != 6 ||
		bacnet.LifeSafetyModePrearmed != 7 || bacnet.LifeSafetyModeSlow != 8 || bacnet.LifeSafetyModeFast != 9 ||
		bacnet.LifeSafetyModeDisconnected != 10 || bacnet.LifeSafetyModeEnabled != 11 || bacnet.LifeSafetyModeDisabled != 12 ||
		bacnet.LifeSafetyModeAutomaticReleaseDisabled != 13 || bacnet.LifeSafetyModeDefault != 14 {
		t.Fatal("BACnetLifeSafetyMode values")
	}
	if bacnet.LifeSafetyStateHoldup != 13 || bacnet.LifeSafetyStateDuress != 14 || bacnet.LifeSafetyStateTamperAlarm != 15 ||
		bacnet.LifeSafetyStateAbnormal != 16 || bacnet.LifeSafetyStateEmergencyPower != 17 || bacnet.LifeSafetyStateDelayed != 18 ||
		bacnet.LifeSafetyStateBlocked != 19 || bacnet.LifeSafetyStateLocalAlarm != 20 || bacnet.LifeSafetyStateGeneralAlarm != 21 ||
		bacnet.LifeSafetyStateSupervisory != 22 || bacnet.LifeSafetyStateTestSupervisory != 23 {
		t.Fatal("BACnetLifeSafetyState values")
	}
	if bacnet.LockStatusLocked != 0 || bacnet.LockStatusUnlocked != 1 || bacnet.LockStatusLockFault != 2 ||
		bacnet.LockStatusUnused != 3 || bacnet.LockStatusUnknown != 4 {
		t.Fatal("BACnetLockStatus values")
	}
}
