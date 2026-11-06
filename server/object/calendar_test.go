// SPDX-License-Identifier: MIT

package object

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
)

// onWire encodes v as a property value and decodes it as WriteProperty
// does, so writes are tested with the form the server hands over.
func onWire(t *testing.T, v bacnet.ApplicationValue) bacnet.ApplicationValue {
	t.Helper()
	b, err := bacnet.AppendApplicationValue(nil, v)
	if err != nil {
		t.Fatal(err)
	}
	els, _, err := bacnet.ParseSequence(b, bacnet.DefaultDecodeLimits(), -1)
	if err != nil {
		t.Fatal(err)
	}
	if len(els) == 1 && !els[0].Context {
		return els[0].Value
	}
	return bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: els}
}

func encoded(t *testing.T, v bacnet.ApplicationValue) string {
	t.Helper()
	b, err := bacnet.AppendApplicationValue(nil, v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func day(y int, m time.Month, d int) bacnet.Date {
	return dateOf(time.Date(y, m, d, 12, 0, 0, 0, time.UTC))
}

func at(y int, m time.Month, d, h, mi int) time.Time {
	return time.Date(y, m, d, h, mi, 0, 0, time.UTC)
}

func prop(id bacnet.PropertyIdentifier) bacnet.PropertyReference {
	return bacnet.PropertyReference{Identifier: id}
}

func propAt(id bacnet.PropertyIdentifier, idx uint32) bacnet.PropertyReference {
	return bacnet.PropertyReference{Identifier: id, ArrayIndex: &idx}
}

func wantErr(t *testing.T, what string, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("%s: got %v, want %v", what, err, want)
	}
}

func TestDateMatches(t *testing.T) {
	wild := unspecifiedDate
	with := func(f func(*bacnet.Date)) bacnet.Date { d := wild; f(&d); return d }
	cases := []struct {
		name string
		p    bacnet.Date
		d    bacnet.Date
		want bool
	}{
		{"exact", day(2026, 11, 6), day(2026, 11, 6), true},
		{"other day", day(2026, 11, 6), day(2026, 11, 7), false},
		{"other year", day(2026, 11, 6), day(2027, 11, 6), false},
		{"any", wild, day(2026, 3, 1), true},
		{"any year", with(func(d *bacnet.Date) { d.Month, d.Day = 12, 25 }), day(2031, 12, 25), true},
		{"odd month", with(func(d *bacnet.Date) { d.Month = monthOdd }), day(2026, 11, 6), true},
		{"odd month no", with(func(d *bacnet.Date) { d.Month = monthOdd }), day(2026, 10, 6), false},
		{"even month", with(func(d *bacnet.Date) { d.Month = monthEven }), day(2026, 10, 6), true},
		{"even month no", with(func(d *bacnet.Date) { d.Month = monthEven }), day(2026, 11, 6), false},
		{"last day leap", with(func(d *bacnet.Date) { d.Day = dayLast }), day(2024, 2, 29), true},
		{"last day not leap", with(func(d *bacnet.Date) { d.Day = dayLast }), day(2026, 2, 28), true},
		{"not last day", with(func(d *bacnet.Date) { d.Day = dayLast }), day(2024, 2, 28), false},
		{"odd day", with(func(d *bacnet.Date) { d.Day = dayOdd }), day(2026, 11, 7), true},
		{"odd day no", with(func(d *bacnet.Date) { d.Day = dayOdd }), day(2026, 11, 6), false},
		{"even day", with(func(d *bacnet.Date) { d.Day = dayEven }), day(2026, 11, 6), true},
		{"even day no", with(func(d *bacnet.Date) { d.Day = dayEven }), day(2026, 11, 7), false},
		{"friday", with(func(d *bacnet.Date) { d.Weekday = 5 }), day(2026, 11, 6), true},
		{"not monday", with(func(d *bacnet.Date) { d.Weekday = 1 }), day(2026, 11, 6), false},
	}
	for _, c := range cases {
		if got := dateMatches(c.p, c.d); got != c.want {
			t.Errorf("%s: %v", c.name, got)
		}
	}
}

func TestWeekNDay(t *testing.T) {
	cases := []struct {
		name string
		w    bacnet.WeekNDay
		d    bacnet.Date
		want bool
	}{
		{"any", bacnet.WeekNDay{Month: dateAny, WeekOfMonth: dateAny, DayOfWeek: dateAny}, day(2026, 11, 6), true},
		{"first friday", bacnet.WeekNDay{Month: dateAny, WeekOfMonth: 1, DayOfWeek: 5}, day(2026, 11, 6), true},
		{"second friday", bacnet.WeekNDay{Month: dateAny, WeekOfMonth: 2, DayOfWeek: 5}, day(2026, 11, 13), true},
		{"not second", bacnet.WeekNDay{Month: dateAny, WeekOfMonth: 2, DayOfWeek: 5}, day(2026, 11, 6), false},
		{"week 5", bacnet.WeekNDay{Month: dateAny, WeekOfMonth: 5, DayOfWeek: dateAny}, day(2026, 11, 29), true},
		{"last week", bacnet.WeekNDay{Month: 11, WeekOfMonth: 6, DayOfWeek: dateAny}, day(2026, 11, 24), true},
		{"last monday of may", bacnet.WeekNDay{Month: 5, WeekOfMonth: 6, DayOfWeek: 1}, day(2026, 5, 25), true},
		{"not in last week", bacnet.WeekNDay{Month: 11, WeekOfMonth: 6, DayOfWeek: dateAny}, day(2026, 11, 23), false},
		{"week before last", bacnet.WeekNDay{Month: dateAny, WeekOfMonth: 7, DayOfWeek: dateAny}, day(2026, 11, 23), true},
		{"week 9", bacnet.WeekNDay{Month: dateAny, WeekOfMonth: 9, DayOfWeek: dateAny}, day(2026, 11, 3), true},
		{"invalid week", bacnet.WeekNDay{Month: dateAny, WeekOfMonth: 10, DayOfWeek: dateAny}, day(2026, 11, 3), false},
		{"odd month", bacnet.WeekNDay{Month: monthOdd, WeekOfMonth: dateAny, DayOfWeek: dateAny}, day(2026, 11, 3), true},
		{"wrong month", bacnet.WeekNDay{Month: 12, WeekOfMonth: dateAny, DayOfWeek: dateAny}, day(2026, 11, 3), false},
		{"wrong weekday", bacnet.WeekNDay{Month: dateAny, WeekOfMonth: dateAny, DayOfWeek: 1}, day(2026, 11, 3), false},
	}
	for _, c := range cases {
		if got := weekNDayMatches(c.w, c.d); got != c.want {
			t.Errorf("%s: %v", c.name, got)
		}
	}
}

func TestDateRange(t *testing.T) {
	r := bacnet.DateRange{Start: day(2026, 12, 24), End: day(2027, 1, 2)}
	for d, want := range map[bacnet.Date]bool{
		day(2026, 12, 24): true, day(2027, 1, 2): true, day(2026, 12, 31): true,
		day(2026, 12, 23): false, day(2027, 1, 3): false,
	} {
		if dateInRange(d, r) != want {
			t.Errorf("%+v: want %v", d, want)
		}
	}
	open := bacnet.DateRange{Start: unspecifiedDate, End: day(2026, 1, 1)}
	if !dateInRange(day(1990, 5, 5), open) || dateInRange(day(2026, 1, 2), open) {
		t.Fatal("open start")
	}
	yearly := bacnet.DateRange{Start: bacnet.Date{Year: dateAny, Month: 6, Day: 1, Weekday: dateAny}, End: bacnet.Date{Year: dateAny, Month: 8, Day: 31, Weekday: dateAny}}
	if !dateInRange(day(2030, 7, 14), yearly) || dateInRange(day(2030, 9, 1), yearly) {
		t.Fatal("yearly range")
	}
	if entryMatches(bacnet.CalendarEntry{Choice: 9}, day(2026, 1, 1)) {
		t.Fatal("unknown choice matched")
	}
}

func holidays() []bacnet.CalendarEntry {
	return []bacnet.CalendarEntry{
		{Choice: bacnet.CalendarEntryDate, Date: bacnet.Date{Year: dateAny, Month: 12, Day: 25, Weekday: dateAny}},
		{Choice: bacnet.CalendarEntryDateRange, DateRange: bacnet.DateRange{Start: day(2026, 7, 20), End: day(2026, 7, 31)}},
		{Choice: bacnet.CalendarEntryWeekNDay, WeekNDay: bacnet.WeekNDay{Month: 11, WeekOfMonth: 4, DayOfWeek: 4}}, // fourth Thursday of November
	}
}

func TestCalendar(t *testing.T) {
	ctx := context.Background()
	if _, err := NewCalendar(CalendarConfig{Name: "c", Instance: bacnet.MaxObjectInstance}); err == nil {
		t.Fatal("instance accepted")
	}
	if _, err := NewCalendar(CalendarConfig{}); err == nil {
		t.Fatal("empty name accepted")
	}
	if _, err := NewCalendar(CalendarConfig{Name: "c", DateList: holidays(), MaxEntries: 2}); err == nil {
		t.Fatal("too many entries accepted")
	}
	if _, err := NewCalendar(CalendarConfig{Name: "c", DateList: []bacnet.CalendarEntry{{Choice: 7}}}); err == nil {
		t.Fatal("bad entry accepted")
	}
	c, err := NewCalendar(CalendarConfig{Instance: 1, Name: "holidays", Description: "d", DateList: holidays()})
	if err != nil {
		t.Fatal(err)
	}
	// The first evaluation used the host clock; count changes from a known
	// state.
	c.Poll(ctx, at(2026, 11, 25, 12, 0), nil)
	var changes []Change
	c.SetNotifier(func(ch Change) { changes = append(changes, ch) })

	pv := func() bool {
		v, err := c.ReadProperty(ctx, prop(bacnet.PropertyPresentValue))
		if err != nil || v.Kind != bacnet.ValueBoolean {
			t.Fatalf("Present_Value: %v %+v", err, v)
		}
		return v.Boolean
	}
	for _, tc := range []struct {
		at   time.Time
		want bool
	}{
		{at(2026, 11, 25, 23, 59), false},
		{at(2026, 11, 26, 0, 0), true}, // midnight: Thanksgiving starts
		{at(2026, 11, 27, 0, 0), false},
		{at(2026, 12, 25, 8, 0), true},
		{at(2026, 7, 31, 8, 0), true},
		{at(2026, 8, 1, 8, 0), false},
	} {
		c.Poll(ctx, tc.at, nil)
		if pv() != tc.want || c.PresentValue() != tc.want {
			t.Fatalf("%v: Present_Value %v", tc.at, !tc.want)
		}
	}
	if len(changes) != 4 {
		t.Fatalf("changes: %+v", changes)
	}

	// Date_List read back and written in wire form.
	dl, err := c.ReadProperty(ctx, prop(bacnet.PropertyDateList))
	if err != nil || len(dl.Elements) != 3 {
		t.Fatalf("Date_List: %v %+v", err, dl)
	}
	if err := c.WriteProperty(ctx, Write{Property: prop(bacnet.PropertyDateList), Value: onWire(t, dl)}); err != nil {
		t.Fatal(err)
	}
	if got := c.DateList(); len(got) != 3 || got[2] != holidays()[2] || got[1] != holidays()[1] || got[0] != holidays()[0] {
		t.Fatalf("round trip: %+v", got)
	}
	// One entry written alone, which makes today (1 August) a holiday.
	one := bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{
		{Context: true, TagNumber: 0, Value: bacnet.DateValue(bacnet.Date{Year: dateAny, Month: 8, Day: 1, Weekday: dateAny})}}}
	if err := c.WriteProperty(ctx, Write{Property: prop(bacnet.PropertyDateList), Value: onWire(t, one)}); err != nil {
		t.Fatal(err)
	}
	if !c.PresentValue() || len(c.DateList()) != 1 {
		t.Fatal("Date_List write did not re-evaluate")
	}
	// Empty list.
	if err := c.WriteProperty(ctx, Write{Property: prop(bacnet.PropertyDateList), Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed}}); err != nil || c.PresentValue() {
		t.Fatalf("empty: %v", err)
	}

	wantErr(t, "boolean", c.WriteProperty(ctx, Write{Property: prop(bacnet.PropertyDateList), Value: bacnet.BoolValue(true)}), ErrInvalidDataType)
	wantErr(t, "bad tag", c.WriteProperty(ctx, Write{Property: prop(bacnet.PropertyDateList), Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed,
		Elements: []bacnet.Element{{Context: true, TagNumber: 5, Value: bacnet.UnsignedValue(1)}}}}), ErrInvalidDataType)
	wantErr(t, "index", c.WriteProperty(ctx, Write{Property: propAt(bacnet.PropertyDateList, 1), Value: one}), ErrNotAnArray)
	wantErr(t, "present value", c.WriteProperty(ctx, Write{Property: prop(bacnet.PropertyPresentValue), Value: bacnet.BoolValue(true)}), ErrWriteAccessDenied)
	small, _ := NewCalendar(CalendarConfig{Name: "s", MaxEntries: 1})
	wantErr(t, "too long", small.WriteProperty(ctx, Write{Property: prop(bacnet.PropertyDateList), Value: dl}), ErrNoSpaceToWriteProperty)

	// The application side.
	if err := c.SetDateList(holidays()); err != nil || len(c.DateList()) != 3 {
		t.Fatal(err)
	}
	if !c.Contains(day(2030, 12, 25)) || c.Contains(day(2030, 12, 26)) {
		t.Fatal("Contains")
	}
	if err := small.SetDateList(holidays()); err == nil {
		t.Fatal("too long accepted")
	}
	if err := c.SetDateList([]bacnet.CalendarEntry{{Choice: 7}}); err == nil {
		t.Fatal("bad entry accepted")
	}
	all, req := c.Properties()
	if len(all) != 7 || !req[bacnet.PropertyDateList] || !req[bacnet.PropertyPresentValue] || req[bacnet.PropertyDescription] {
		t.Fatalf("properties: %v %v", all, req)
	}
}

func TestCalendarListElements(t *testing.T) {
	ctx := context.Background()
	c, err := NewCalendar(CalendarConfig{Instance: 1, Name: "c", DateList: holidays()[:1], MaxEntries: 2})
	if err != nil {
		t.Fatal(err)
	}
	c.Evaluate(at(2026, 7, 22, 0, 0))
	el := func(e bacnet.CalendarEntry) bacnet.Element {
		x, err := calendarEntryElement(e)
		if err != nil {
			t.Fatal(err)
		}
		return onWire(t, bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{x}}).Elements[0]
	}
	dl := prop(bacnet.PropertyDateList)
	// Adding an entry already present is not an error and adds nothing.
	if err := c.AddListElements(ctx, dl, []bacnet.Element{el(holidays()[0]), el(holidays()[1])}); err != nil {
		t.Fatal(err)
	}
	if len(c.DateList()) != 2 || !c.PresentValue() {
		t.Fatalf("add: %+v", c.DateList())
	}
	var le *ListError
	err = c.AddListElements(ctx, dl, []bacnet.Element{el(holidays()[2])})
	if !errors.As(err, &le) || le.First != 1 || !errors.Is(err, ErrNoSpaceForListElement) {
		t.Fatalf("full: %v", err)
	}
	err = c.AddListElements(ctx, dl, []bacnet.Element{el(holidays()[2]), {Value: bacnet.BoolValue(true)}})
	if !errors.As(err, &le) || le.First != 2 || !errors.Is(err, ErrInvalidDataType) {
		t.Fatalf("bad element: %v", err)
	}
	err = c.RemoveListElements(ctx, dl, []bacnet.Element{el(holidays()[1]), el(holidays()[2])})
	if !errors.As(err, &le) || le.First != 2 || !errors.Is(err, ErrListElementNotFound) || len(c.DateList()) != 2 {
		t.Fatalf("remove missing: %v", err)
	}
	err = c.RemoveListElements(ctx, dl, []bacnet.Element{{Value: bacnet.BoolValue(true)}})
	if !errors.As(err, &le) || le.First != 1 {
		t.Fatalf("remove bad: %v", err)
	}
	if err := c.RemoveListElements(ctx, dl, []bacnet.Element{el(holidays()[1])}); err != nil || len(c.DateList()) != 1 || c.PresentValue() {
		t.Fatalf("remove: %v", err)
	}
	wantErr(t, "not a list", c.AddListElements(ctx, prop(bacnet.PropertyPresentValue), nil), ErrPropertyIsNotAList)
	wantErr(t, "unknown", c.RemoveListElements(ctx, prop(bacnet.PropertyUnits), nil), ErrUnknownProperty)
	wantErr(t, "index", c.AddListElements(ctx, propAt(bacnet.PropertyDateList, 1), nil), ErrNotAnArray)
	wantErr(t, "remove index", c.RemoveListElements(ctx, propAt(bacnet.PropertyDateList, 1), nil), ErrNotAnArray)
}

func TestObjectTypesSupported(t *testing.T) {
	d, err := NewDevice(DeviceConfig{Instance: 9, Name: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := NewCalendar(CalendarConfig{Instance: 1, Name: "cal"})
	s, _ := NewSchedule(ScheduleConfig{Instance: 1, Name: "sch", Default: bacnet.RealValue(1)})
	csv, _ := NewCharacterStringValue(ValueConfig{Instance: 1, Name: "csv"})
	iv, _ := NewIntegerValue(ValueConfig{Instance: 1, Name: "iv"})
	piv, _ := NewPositiveIntegerValue(ValueConfig{Instance: 1, Name: "piv"})
	sv, _ := NewStructuredView(StructuredViewConfig{Instance: 1, Name: "sv"})
	for _, o := range []Object{c, s, csv, iv, piv, sv} {
		if err := d.Add(o); err != nil {
			t.Fatal(err)
		}
	}
	v, err := d.ReadProperty(context.Background(), prop(bacnet.PropertyProtocolObjectTypesSupported))
	if err != nil {
		t.Fatal(err)
	}
	for _, ot := range []bacnet.ObjectType{bacnet.ObjectTypeDevice, bacnet.ObjectTypeCalendar, bacnet.ObjectTypeSchedule,
		bacnet.ObjectTypeStructuredView, bacnet.ObjectTypeCharacterStringValue, bacnet.ObjectTypeIntegerValue, bacnet.ObjectTypePositiveIntegerValue} {
		if !bit(v, int(ot)) {
			t.Errorf("%v not in Protocol_Object_Types_Supported", ot)
		}
	}
	if bit(v, int(bacnet.ObjectTypeAnalogInput)) {
		t.Error("analog-input reported")
	}
}
