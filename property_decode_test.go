// SPDX-License-Identifier: MIT

package bacnet_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
)

func TestDecodePropertyValuePropertyListAndDateList(t *testing.T) {
	v := bacnet.ApplicationValue{
		Kind: bacnet.ValueConstructed,
		Elements: []bacnet.Element{
			{Value: bacnet.EnumValue(uint32(bacnet.PropertyPresentValue))},
		},
	}
	typed, ok, err := bacnet.DecodePropertyValue(bacnet.ObjectTypeAnalogValue, bacnet.PropertyPropertyList, v)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	ids := typed.([]bacnet.PropertyIdentifier)
	if len(ids) != 1 || ids[0] != bacnet.PropertyPresentValue {
		t.Fatalf("%#v", typed)
	}

	dr := bacnet.EncodeDateRange(bacnet.DateRange{
		Start: bacnet.Date{Year: 126, Month: 1, Day: 1, Weekday: 1},
		End:   bacnet.Date{Year: 126, Month: 1, Day: 2, Weekday: 2},
	})
	dateList := bacnet.ApplicationValue{
		Kind: bacnet.ValueConstructed,
		Elements: []bacnet.Element{
			{Value: bacnet.DateValue(bacnet.Date{Year: 126, Month: 2, Day: 3, Weekday: 4})},
			{Value: dr},
		},
	}
	typed, ok, err = bacnet.DecodePropertyValue(bacnet.ObjectTypeCalendar, bacnet.PropertyDateList, dateList)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	entries := typed.([]bacnet.CalendarEntry)
	if len(entries) != 2 {
		t.Fatalf("%#v", entries)
	}
	_, ok, err = bacnet.DecodePropertyValue(bacnet.ObjectTypeDevice, bacnet.PropertyPresentValue, bacnet.RealValue(1))
	if err != nil || ok {
		t.Fatalf("unknown should be ok=false err=%v ok=%v", err, ok)
	}
}

func TestCalendarEntryDateAndDateRange(t *testing.T) {
	d := bacnet.CalendarEntry{Choice: bacnet.CalendarEntryDate, Date: bacnet.Date{Year: 126, Month: 3, Day: 4, Weekday: 5}}
	enc, err := bacnet.EncodeCalendarEntry(d)
	if err != nil {
		t.Fatal(err)
	}
	got, err := bacnet.DecodeCalendarEntry(enc)
	if err != nil || got.Choice != bacnet.CalendarEntryDate || got.Date != d.Date {
		t.Fatalf("%#v err=%v", got, err)
	}
	dr := bacnet.CalendarEntry{
		Choice: bacnet.CalendarEntryDateRange,
		DateRange: bacnet.DateRange{
			Start: bacnet.Date{Year: 126, Month: 1, Day: 1, Weekday: 1},
			End:   bacnet.Date{Year: 126, Month: 1, Day: 2, Weekday: 2},
		},
	}
	enc, err = bacnet.EncodeCalendarEntry(dr)
	if err != nil {
		t.Fatal(err)
	}
	got, err = bacnet.DecodeCalendarEntry(enc)
	if err != nil || got.Choice != bacnet.CalendarEntryDateRange {
		t.Fatalf("%#v err=%v", got, err)
	}
}

func TestDecodePropertyValueErrorAndFallbackPaths(t *testing.T) {
	_, ok, err := bacnet.DecodePropertyValue(bacnet.ObjectTypeDevice, bacnet.PropertyObjectList, bacnet.ApplicationValue{
		Kind:     bacnet.ValueConstructed,
		Elements: []bacnet.Element{{Value: bacnet.BoolValue(true)}},
	})
	if err == nil || !ok {
		t.Fatalf("object-list bad element ok=%v err=%v", ok, err)
	}
	id := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 9}
	typed, ok, err := bacnet.DecodePropertyValue(bacnet.ObjectTypeDevice, bacnet.PropertyObjectList, bacnet.ObjectIDValue(id))
	if err != nil || !ok || typed.([]bacnet.ObjectIdentifier)[0] != id {
		t.Fatalf("%v %v %#v", ok, err, typed)
	}
	_, ok, err = bacnet.DecodePropertyValue(bacnet.ObjectTypeDevice, bacnet.PropertyPropertyList, bacnet.ApplicationValue{
		Kind:     bacnet.ValueConstructed,
		Elements: []bacnet.Element{{Value: bacnet.BoolValue(true)}},
	})
	if err == nil || !ok {
		t.Fatalf("property-list bad element ok=%v err=%v", ok, err)
	}
	typed, ok, err = bacnet.DecodePropertyValue(bacnet.ObjectTypeDevice, bacnet.PropertyPropertyList, bacnet.EnumValue(uint32(bacnet.PropertyPresentValue)))
	if err != nil || !ok || typed.([]bacnet.PropertyIdentifier)[0] != bacnet.PropertyPresentValue {
		t.Fatalf("%v %v %#v", ok, err, typed)
	}
	_, ok, err = bacnet.DecodePropertyValue(bacnet.ObjectTypeCalendar, bacnet.PropertyDateList, bacnet.ApplicationValue{
		Kind:     bacnet.ValueConstructed,
		Elements: []bacnet.Element{{Value: bacnet.BoolValue(true)}},
	})
	if err == nil || !ok {
		t.Fatalf("date-list opaque ok=%v err=%v", ok, err)
	}
	_, ok, err = bacnet.DecodePropertyValue(bacnet.ObjectTypeDevice, bacnet.PropertyObjectList, bacnet.RealValue(1))
	if err != nil || ok {
		t.Fatalf("empty object-list fallback ok=%v err=%v", ok, err)
	}
}
