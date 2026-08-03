// SPDX-License-Identifier: MIT

package bacnet_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
)

func TestConstructedTypeErrorPaths(t *testing.T) {
	if _, err := bacnet.EncodeCalendarEntry(bacnet.CalendarEntry{Choice: 99}); err == nil {
		t.Fatal("bad choice")
	}
	if _, err := bacnet.DecodeCalendarEntry(bacnet.ApplicationValue{}); err == nil {
		t.Fatal("empty")
	}
	if _, err := bacnet.DecodeCalendarEntry(bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{{
		Context: true, TagNumber: 9, Value: bacnet.UnsignedValue(1),
	}}}); err == nil {
		t.Fatal("bad tag")
	}
	if _, err := bacnet.DecodeDailySchedule(bacnet.ApplicationValue{}); err == nil {
		t.Fatal("daily")
	}
	if _, err := bacnet.DecodeWeeklySchedule(bacnet.ApplicationValue{Kind: bacnet.ValueConstructed}); err == nil {
		t.Fatal("weekly")
	}
	if _, err := bacnet.DecodeHostNPort(bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{{
		Context: true, TagNumber: 0, Value: bacnet.ApplicationValue{Kind: bacnet.ValueOctetString, OctetString: []byte{1}},
	}}}); err == nil {
		t.Fatal("bad ip")
	}
	d := bacnet.CalendarEntry{Choice: bacnet.CalendarEntryDate, Date: bacnet.Date{Year: 126, Month: 1, Day: 1, Weekday: 1}}
	enc, err := bacnet.EncodeCalendarEntry(d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bacnet.DecodeCalendarEntry(enc); err != nil {
		t.Fatal(err)
	}
	dr := bacnet.CalendarEntry{Choice: bacnet.CalendarEntryDateRange, DateRange: bacnet.DateRange{
		Start: bacnet.Date{Year: 126, Month: 1, Day: 1, Weekday: 1},
		End:   bacnet.Date{Year: 126, Month: 1, Day: 2, Weekday: 2},
	}}
	enc, err = bacnet.EncodeCalendarEntry(dr)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bacnet.DecodeCalendarEntry(enc); err != nil {
		t.Fatal(err)
	}
}

func TestConstructedDecodeErrorPaths(t *testing.T) {
	bad := bacnet.ApplicationValue{
		Kind: bacnet.ValueConstructed,
		Elements: []bacnet.Element{
			{Value: bacnet.BoolValue(true)},
			{Value: bacnet.DateValue(bacnet.Date{Year: 126, Month: 1, Day: 1, Weekday: 1})},
		},
	}
	if _, err := bacnet.DecodeDateRange(bad); err == nil {
		t.Fatal("DateRange start")
	}
	bad.Elements[0].Value = bacnet.DateValue(bacnet.Date{Year: 126, Month: 1, Day: 1, Weekday: 1})
	bad.Elements[1].Value = bacnet.BoolValue(true)
	if _, err := bacnet.DecodeDateRange(bad); err == nil {
		t.Fatal("DateRange end")
	}
	if _, err := bacnet.DecodeDateTime(bacnet.ApplicationValue{
		Kind: bacnet.ValueConstructed,
		Elements: []bacnet.Element{
			{Value: bacnet.BoolValue(true)},
			{Value: bacnet.TimeValue(bacnet.Time{})},
		},
	}); err == nil {
		t.Fatal("DateTime date")
	}
	if _, err := bacnet.DecodeDateTime(bacnet.ApplicationValue{
		Kind: bacnet.ValueConstructed,
		Elements: []bacnet.Element{
			{Value: bacnet.DateValue(bacnet.Date{Year: 126, Month: 1, Day: 1, Weekday: 1})},
			{Value: bacnet.BoolValue(true)},
		},
	}); err == nil {
		t.Fatal("DateTime time")
	}
	if _, err := bacnet.DecodeCalendarEntry(bacnet.ApplicationValue{
		Kind: bacnet.ValueConstructed,
		Elements: []bacnet.Element{{
			Context: true, TagNumber: 0, Value: bacnet.BoolValue(true),
		}},
	}); err == nil {
		t.Fatal("CalendarEntry date")
	}
	if _, err := bacnet.DecodeCalendarEntry(bacnet.ApplicationValue{
		Kind: bacnet.ValueConstructed,
		Elements: []bacnet.Element{{
			Context: true, TagNumber: 1, Value: bacnet.BoolValue(true),
		}},
	}); err == nil {
		t.Fatal("CalendarEntry date-range")
	}
	if _, err := bacnet.DecodeCalendarEntry(bacnet.ApplicationValue{
		Kind: bacnet.ValueConstructed,
		Elements: []bacnet.Element{{
			Context: true, TagNumber: 2, Value: bacnet.ApplicationValue{Kind: bacnet.ValueOctetString, OctetString: []byte{1}},
		}},
	}); err == nil {
		t.Fatal("CalendarEntry weeknday")
	}
	if _, err := bacnet.DecodeDailySchedule(bacnet.ApplicationValue{
		Kind: bacnet.ValueConstructed,
		Elements: []bacnet.Element{{
			Context: true, TagNumber: 0,
			Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{{
				Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{{Value: bacnet.BoolValue(true)}}},
			}}},
		}},
	}); err == nil {
		t.Fatal("DailySchedule TimeValue short")
	}
	if _, err := bacnet.DecodeDailySchedule(bacnet.ApplicationValue{
		Kind: bacnet.ValueConstructed,
		Elements: []bacnet.Element{{
			Context: true, TagNumber: 0,
			Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{{
				Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{
					{Value: bacnet.BoolValue(true)},
					{Value: bacnet.RealValue(1)},
				}},
			}}},
		}},
	}); err == nil {
		t.Fatal("DailySchedule AsTime")
	}
	var w bacnet.WeeklySchedule
	w[0] = bacnet.DailySchedule{}
	enc := bacnet.EncodeWeeklySchedule(w)
	enc.Elements[0].Value = bacnet.BoolValue(true)
	if _, err := bacnet.DecodeWeeklySchedule(enc); err == nil {
		t.Fatal("WeeklySchedule day decode")
	}
}
