// SPDX-License-Identifier: MIT

package bacnet

import (
	"errors"
	"reflect"
	"testing"
)

func exceptionScheduleSample() ExceptionSchedule {
	cal := ObjectIdentifier{Type: ObjectTypeCalendar, Instance: 3}
	return ExceptionSchedule{SpecialEvents: []SpecialEvent{
		{CalendarEntry: CalendarEntry{Choice: CalendarEntryDate, Date: Date{Year: 126, Month: 12, Day: 25, Weekday: 5}},
			ListOfTimeValues: []ScheduledTimeValue{{Time: Time{Hour: 0}, Value: RealValue(16)}}, EventPriority: 1},
		{CalendarEntry: CalendarEntry{Choice: CalendarEntryDateRange, DateRange: DateRange{
			Start: Date{Year: 126, Month: 8, Day: 1, Weekday: 0xFF}, End: Date{Year: 126, Month: 8, Day: 14, Weekday: 0xFF}}},
			ListOfTimeValues: []ScheduledTimeValue{{Time: Time{Hour: 7}, Value: RealValue(18)}, {Time: Time{Hour: 17}, Value: NullValue()}}, EventPriority: 8},
		{CalendarEntry: CalendarEntry{Choice: CalendarEntryWeekNDay, WeekNDay: WeekNDay{Month: 0xFF, WeekOfMonth: 0xFF, DayOfWeek: 7}},
			ListOfTimeValues: []ScheduledTimeValue{}, EventPriority: 16},
		{PeriodObject: &cal, ListOfTimeValues: []ScheduledTimeValue{{Time: Time{Hour: 12, Minute: 30}, Value: RealValue(20)}}, EventPriority: 4},
	}}
}

func TestExceptionScheduleRoundTrip(t *testing.T) {
	want := exceptionScheduleSample()
	v, err := EncodeExceptionSchedule(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeExceptionSchedule(v)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("in memory:\n got %+v\nwant %+v", got, want)
	}
	// The wire form: what ReadProperty returns, the events' elements one after another.
	wire, err := AppendApplicationValue(nil, v)
	if err != nil {
		t.Fatal(err)
	}
	els, _, err := ParseSequence(wire, DefaultDecodeLimits(), -1)
	if err != nil {
		t.Fatal(err)
	}
	if len(els) != 3*len(want.SpecialEvents) {
		t.Fatalf("wire elements %d, want %d", len(els), 3*len(want.SpecialEvents))
	}
	got, err = DecodeExceptionSchedule(ApplicationValue{Kind: ValueConstructed, Elements: els})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("wire:\n got %+v\nwant %+v", got, want)
	}
	empty, err := DecodeExceptionSchedule(ApplicationValue{Kind: ValueConstructed})
	if err != nil || len(empty.SpecialEvents) != 0 {
		t.Fatalf("empty schedule: %+v, %v", empty, err)
	}
}

func TestExceptionScheduleMalformed(t *testing.T) {
	good, err := EncodeExceptionSchedule(exceptionScheduleSample())
	if err != nil {
		t.Fatal(err)
	}
	ev := good.Elements[0].Value.Elements
	withPrio := func(p Element) ApplicationValue {
		return ApplicationValue{Kind: ValueConstructed, Elements: []Element{ev[0], ev[1], p}}
	}
	for name, v := range map[string]ApplicationValue{
		"primitive":          RealValue(1),
		"two elements":       {Kind: ValueConstructed, Elements: ev[:2]},
		"period tag 2":       {Kind: ValueConstructed, Elements: []Element{{Context: true, TagNumber: 2, Value: ev[0].Value}, ev[1], ev[2]}},
		"time values tag 4":  {Kind: ValueConstructed, Elements: []Element{ev[0], {Context: true, TagNumber: 4, Value: ev[1].Value}, ev[2]}},
		"priority tag 5":     withPrio(Element{Context: true, TagNumber: 5, Value: UnsignedValue(1)}),
		"priority 256":       withPrio(Element{Context: true, TagNumber: 3, Value: UnsignedValue(256)}),
		"priority not a tag": withPrio(Element{Value: UnsignedValue(1)}),
	} {
		if _, err := DecodeExceptionSchedule(v); !errors.Is(err, ErrMalformed) {
			t.Errorf("%s: err = %v, want ErrMalformed", name, err)
		}
	}
	for _, prio := range []uint8{0, 17, 255} {
		bad := exceptionScheduleSample()
		bad.SpecialEvents[1].EventPriority = prio
		if _, err := EncodeExceptionSchedule(bad); !errors.Is(err, ErrMalformed) {
			t.Errorf("event priority %d encoded: %v", prio, err)
		}
	}
	// A peer's priority outside 1..16 that fits a uint8 decodes as sent.
	lax := withPrio(Element{Context: true, TagNumber: 3, Value: UnsignedValue(200)})
	if got, err := DecodeExceptionSchedule(lax); err != nil || got.SpecialEvents[0].EventPriority != 200 {
		t.Errorf("priority 200 from a peer: %+v, %v", got, err)
	}
	bad := exceptionScheduleSample()
	bad.SpecialEvents[0].CalendarEntry.Choice = 9
	if _, err := EncodeExceptionSchedule(bad); !errors.Is(err, ErrMalformed) {
		t.Fatalf("an unknown calendar entry choice encoded: %v", err)
	}
}

// appendElements encodes a parsed element list back to tag bytes.
func appendElements(t *testing.T, els []Element) []byte {
	t.Helper()
	var out []byte
	for _, el := range els {
		var err error
		if out, err = AppendTag(out, el); err != nil {
			t.Fatalf("AppendTag: %v", err)
		}
	}
	return out
}

func FuzzDecodeExceptionSchedule(f *testing.F) {
	v, err := EncodeExceptionSchedule(exceptionScheduleSample())
	if err != nil {
		f.Fatal(err)
	}
	seed, err := AppendTag(nil, Element{Value: v})
	if err == nil {
		f.Add(seed)
	}
	var body []byte
	for _, el := range v.Elements {
		if body, err = AppendTag(body, el); err != nil {
			f.Fatal(err)
		}
	}
	f.Add(body)
	f.Add([]byte{})
	f.Add([]byte{0x0E, 0x0F})
	f.Fuzz(func(t *testing.T, data []byte) {
		els, _, err := ParseSequence(data, DefaultDecodeLimits(), -1)
		if err != nil {
			return
		}
		exc, err := DecodeExceptionSchedule(ApplicationValue{Kind: ValueConstructed, Elements: els})
		if err != nil {
			if !errors.Is(err, ErrMalformed) {
				t.Fatalf("error %v is not ErrMalformed", err)
			}
			return
		}
		// What the decoder accepts with a valid priority, the encoder emits,
		// and the re-encoded form decodes to the same value.
		for _, ev := range exc.SpecialEvents {
			if ev.EventPriority < 1 || ev.EventPriority > 16 {
				return
			}
		}
		again, err := EncodeExceptionSchedule(exc)
		if err != nil {
			t.Fatalf("re-encode: %v", err)
		}
		round, err := DecodeExceptionSchedule(again)
		if err != nil {
			t.Fatalf("decode of re-encoded % X: %v", appendElements(t, again.Elements), err)
		}
		if !reflect.DeepEqual(round, exc) {
			t.Fatalf("round trip:\n got %+v\nwant %+v", round, exc)
		}
	})
}
