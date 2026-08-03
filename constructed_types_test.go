// SPDX-License-Identifier: MIT

package bacnet_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
)

func TestDateRangeDateTimeRoundTrip(t *testing.T) {
	dr := bacnet.DateRange{
		Start: bacnet.Date{Year: 126, Month: 1, Day: 2, Weekday: 5},
		End:   bacnet.Date{Year: 126, Month: 12, Day: 31, Weekday: 3},
	}
	got, err := bacnet.DecodeDateRange(bacnet.EncodeDateRange(dr))
	if err != nil || got != dr {
		t.Fatalf("DateRange: %#v err=%v", got, err)
	}
	dt := bacnet.DateTime{
		Date: dr.Start,
		Time: bacnet.Time{Hour: 12, Minute: 30, Second: 0, Hundredths: 0},
	}
	gotDT, err := bacnet.DecodeDateTime(bacnet.EncodeDateTime(dt))
	if err != nil || gotDT != dt {
		t.Fatalf("DateTime: %#v err=%v", gotDT, err)
	}
}

func TestDecodePropertyValueObjectList(t *testing.T) {
	id := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	v := bacnet.ApplicationValue{
		Kind: bacnet.ValueConstructed,
		Elements: []bacnet.Element{
			{Value: bacnet.ObjectIDValue(id)},
		},
	}
	typed, ok, err := bacnet.DecodePropertyValue(bacnet.ObjectTypeDevice, bacnet.PropertyObjectList, v)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	ids := typed.([]bacnet.ObjectIdentifier)
	if len(ids) != 1 || ids[0] != id {
		t.Fatalf("%#v", ids)
	}
}

func TestCalendarEntryAndHostNPortRoundTrip(t *testing.T) {
	entry := bacnet.CalendarEntry{
		Choice:   bacnet.CalendarEntryWeekNDay,
		WeekNDay: bacnet.WeekNDay{Month: 1, WeekOfMonth: 2, DayOfWeek: 3},
	}
	enc, err := bacnet.EncodeCalendarEntry(entry)
	if err != nil {
		t.Fatal(err)
	}
	got, err := bacnet.DecodeCalendarEntry(enc)
	if err != nil || got != entry {
		t.Fatalf("%#v err=%v", got, err)
	}
	h := bacnet.HostNPort{IP: [4]byte{192, 168, 1, 1}, Port: 47808}
	hg, err := bacnet.DecodeHostNPort(bacnet.EncodeHostNPort(h))
	if err != nil || hg.Port != 47808 || hg.IP != h.IP {
		t.Fatalf("%#v err=%v", hg, err)
	}
}

func TestWeeklyScheduleRoundTrip(t *testing.T) {
	var w bacnet.WeeklySchedule
	w[0] = bacnet.DailySchedule{DaySchedule: []bacnet.ScheduledTimeValue{{
		Time:  bacnet.Time{Hour: 8},
		Value: bacnet.RealValue(21.5),
	}}}
	got, err := bacnet.DecodeWeeklySchedule(bacnet.EncodeWeeklySchedule(w))
	if err != nil {
		t.Fatal(err)
	}
	if len(got[0].DaySchedule) != 1 || got[0].DaySchedule[0].Time.Hour != 8 {
		t.Fatalf("%#v", got[0])
	}
}

func TestObjectTypePropertyString(t *testing.T) {
	if bacnet.ObjectTypeDevice.String() != "device" {
		t.Fatal(bacnet.ObjectTypeDevice.String())
	}
	if bacnet.PropertyObjectList.String() != "object-list" {
		t.Fatal(bacnet.PropertyObjectList.String())
	}
	if bacnet.ObjectType(999).String() == "device" {
		t.Fatal("unknown should not map")
	}
}
