// SPDX-License-Identifier: MIT

package object

import (
	"context"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
)

func hm(h, m uint8) bacnet.Time { return bacnet.Time{Hour: h, Minute: m} }

func tv(h, m uint8, v bacnet.ApplicationValue) bacnet.ScheduledTimeValue {
	return bacnet.ScheduledTimeValue{Time: hm(h, m), Value: v}
}

type scheduleRig struct {
	dev    *Device
	target *Point
	cal    *Calendar
	sch    *Schedule
}

// newScheduleRig builds a device with a commandable Analog Value, a holiday
// Calendar and a Schedule that writes to the value at priority 9.
//
//	Weekly:     Monday–Friday 08:00 22, 17:00 NULL; Tuesday also 20:00 25
//	Exceptions: holiday calendar, priority 10: 00:00 15
//	            2026-12-25, priority 5: 12:00 30, 13:00 NULL
//	            2026-12-25, priority 5: 12:00 31, 12:45 NULL (same priority, later: loses)
//	Default:    18
func newScheduleRig(t *testing.T) *scheduleRig {
	t.Helper()
	dev, err := NewDevice(DeviceConfig{Instance: 7, Name: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	target, err := NewAnalogValue(PointConfig{Instance: 1, Name: "sp", Value: 0, Commandable: true})
	if err != nil {
		t.Fatal(err)
	}
	cal, err := NewCalendar(CalendarConfig{Instance: 1, Name: "hol", DateList: holidays()})
	if err != nil {
		t.Fatal(err)
	}
	var week bacnet.WeeklySchedule
	for i := 0; i < 5; i++ {
		week[i].DaySchedule = []bacnet.ScheduledTimeValue{tv(17, 0, bacnet.NullValue()), tv(8, 0, bacnet.RealValue(22))}
	}
	week[1].DaySchedule = append(week[1].DaySchedule, tv(20, 0, bacnet.RealValue(25)))
	calID := cal.Identifier()
	xmas := bacnet.CalendarEntry{Choice: bacnet.CalendarEntryDate, Date: day(2026, 12, 25)}
	devID := dev.Identifier()
	sch, err := NewSchedule(ScheduleConfig{
		Instance: 1, Name: "sch", Description: "d", Default: bacnet.RealValue(18), Weekly: week, PriorityForWriting: 9,
		Exceptions: []bacnet.SpecialEvent{
			{PeriodObject: &calID, ListOfTimeValues: []bacnet.ScheduledTimeValue{tv(0, 0, bacnet.RealValue(15))}, EventPriority: 10},
			{CalendarEntry: xmas, ListOfTimeValues: []bacnet.ScheduledTimeValue{tv(12, 0, bacnet.RealValue(30)), tv(13, 0, bacnet.NullValue())}, EventPriority: 5},
			{CalendarEntry: xmas, ListOfTimeValues: []bacnet.ScheduledTimeValue{tv(12, 0, bacnet.RealValue(31)), tv(12, 45, bacnet.NullValue())}, EventPriority: 5},
		},
		References: []DeviceObjectPropertyReference{
			{Object: target.Identifier(), Property: bacnet.PropertyPresentValue, Device: &devID},
			{Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 99}, Property: bacnet.PropertyPresentValue},
			{Object: target.Identifier(), Property: bacnet.PropertyPresentValue, Device: &bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 8}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range []Object{target, cal, sch} {
		if err := dev.Add(o); err != nil {
			t.Fatal(err)
		}
	}
	return &scheduleRig{dev: dev, target: target, cal: cal, sch: sch}
}

// slot9 returns priority 9 of the target's Priority_Array.
func (r *scheduleRig) slot9(t *testing.T) bacnet.ApplicationValue {
	t.Helper()
	v, err := r.target.ReadProperty(context.Background(), propAt(bacnet.PropertyPriorityArray, 9))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestScheduleEvaluation(t *testing.T) {
	ctx := context.Background()
	r := newScheduleRig(t)
	cases := []struct {
		name string
		at   time.Time
		want float32
	}{
		{"monday before first entry", at(2026, 11, 2, 7, 59), 18},
		{"monday 08:00", at(2026, 11, 2, 8, 0), 22},
		{"monday afternoon", at(2026, 11, 2, 16, 59), 22},
		{"NULL relinquishes to the default", at(2026, 11, 2, 17, 0), 18},
		{"tuesday evening", at(2026, 11, 3, 23, 59), 25},
		{"midnight: wednesday starts from the default", at(2026, 11, 4, 0, 0), 18},
		{"wednesday 08:00", at(2026, 11, 4, 8, 0), 22},
		{"saturday has no entries", at(2026, 11, 7, 10, 0), 18},
		{"calendar holiday overrides the weekly schedule", at(2026, 11, 26, 9, 0), 15},
		{"priority 5 event beats priority 10", at(2026, 12, 25, 12, 30), 30},
		{"priority 5 relinquished: priority 10 holiday again", at(2026, 12, 25, 13, 0), 15},
		{"holiday range", at(2026, 7, 21, 9, 0), 15},
	}
	for _, c := range cases {
		r.sch.Poll(ctx, c.at, r.dev)
		pv := r.sch.PresentValue()
		if pv.Kind != bacnet.ValueReal || pv.Real != c.want {
			t.Fatalf("%s: Present_Value %+v, want %v", c.name, pv, c.want)
		}
		if s := r.slot9(t); s.Kind != bacnet.ValueReal || s.Real != c.want {
			t.Fatalf("%s: referenced property %+v, want %v", c.name, s, c.want)
		}
	}

	// Outside Effective_Period the default applies.
	period := bacnet.EncodeDateRange(bacnet.DateRange{Start: day(2026, 1, 1), End: day(2026, 6, 30)})
	if err := r.sch.WriteProperty(ctx, Write{Property: prop(bacnet.PropertyEffectivePeriod), Value: onWire(t, period)}); err != nil {
		t.Fatal(err)
	}
	r.sch.Poll(ctx, at(2026, 11, 2, 9, 0), r.dev)
	if pv := r.sch.PresentValue(); pv.Real != 18 {
		t.Fatalf("outside effective period: %+v", pv)
	}
	r.sch.Poll(ctx, at(2026, 6, 30, 9, 0), r.dev) // a Tuesday
	if pv := r.sch.PresentValue(); pv.Real != 22 {
		t.Fatalf("last day of effective period: %+v", pv)
	}
	got, _ := r.sch.ReadProperty(ctx, prop(bacnet.PropertyEffectivePeriod))
	if encoded(t, got) != encoded(t, period) {
		t.Fatal("Effective_Period read back")
	}
}

func TestScheduleOutOfService(t *testing.T) {
	ctx := context.Background()
	r := newScheduleRig(t)
	r.sch.Poll(ctx, at(2026, 11, 2, 9, 0), r.dev)
	pvRef := prop(bacnet.PropertyPresentValue)
	wantErr(t, "in service", r.sch.WriteProperty(ctx, Write{Property: pvRef, Value: bacnet.RealValue(40)}), ErrWriteAccessDenied)
	if err := r.sch.WriteProperty(ctx, Write{Property: prop(bacnet.PropertyOutOfService), Value: bacnet.BoolValue(true)}); err != nil {
		t.Fatal(err)
	}
	if sf, _ := r.sch.ReadProperty(ctx, prop(bacnet.PropertyStatusFlags)); !bit(sf, 3) {
		t.Fatal("out-of-service flag")
	}
	wantErr(t, "kind", r.sch.WriteProperty(ctx, Write{Property: pvRef, Value: bacnet.UnsignedValue(4)}), ErrInvalidDataType)
	wantErr(t, "index", r.sch.WriteProperty(ctx, Write{Property: propAt(bacnet.PropertyPresentValue, 1), Value: bacnet.RealValue(4)}), ErrNotAnArray)
	if err := r.sch.WriteProperty(ctx, Write{Property: pvRef, Value: bacnet.RealValue(40)}); err != nil {
		t.Fatal(err)
	}
	if err := r.sch.WriteProperty(ctx, Write{Property: pvRef, Value: bacnet.RealValue(40)}); err != nil {
		t.Fatal(err)
	}
	// The written value reaches the references on the next Poll and the
	// schedule does not evaluate while out of service.
	r.sch.Poll(ctx, at(2026, 11, 2, 18, 0), r.dev)
	if pv := r.sch.PresentValue(); pv.Real != 40 || r.slot9(t).Real != 40 {
		t.Fatalf("out of service: %+v %+v", pv, r.slot9(t))
	}
	if err := r.sch.WriteProperty(ctx, Write{Property: prop(bacnet.PropertyOutOfService), Value: bacnet.BoolValue(false)}); err != nil {
		t.Fatal(err)
	}
	if err := r.sch.WriteProperty(ctx, Write{Property: prop(bacnet.PropertyOutOfService), Value: bacnet.BoolValue(false)}); err != nil {
		t.Fatal(err)
	}
	wantErr(t, "oos kind", r.sch.WriteProperty(ctx, Write{Property: prop(bacnet.PropertyOutOfService), Value: bacnet.RealValue(1)}), ErrInvalidDataType)
	wantErr(t, "oos index", r.sch.WriteProperty(ctx, Write{Property: propAt(bacnet.PropertyOutOfService, 1), Value: bacnet.BoolValue(true)}), ErrNotAnArray)
	r.sch.Poll(ctx, at(2026, 11, 2, 18, 0), r.dev)
	if pv := r.sch.PresentValue(); pv.Real != 18 || r.slot9(t).Real != 18 {
		t.Fatalf("back in service: %+v", pv)
	}
	// Without a device nothing is written; the write stays pending.
	r.sch.Poll(ctx, at(2026, 11, 2, 9, 0), nil)
	if r.slot9(t).Real != 18 {
		t.Fatal("written without a device")
	}
	r.sch.Poll(ctx, at(2026, 11, 2, 9, 0), r.dev)
	if r.slot9(t).Real != 22 {
		t.Fatal("pending write lost")
	}
}

func TestScheduleRoundTrip(t *testing.T) {
	ctx := context.Background()
	r := newScheduleRig(t)
	for _, id := range []bacnet.PropertyIdentifier{bacnet.PropertyWeeklySchedule, bacnet.PropertyExceptionSchedule, bacnet.PropertyListOfObjectPropertyReferences} {
		v, err := r.sch.ReadProperty(ctx, prop(id))
		if err != nil {
			t.Fatal(err)
		}
		if err := r.sch.WriteProperty(ctx, Write{Property: prop(id), Value: onWire(t, v)}); err != nil {
			t.Fatalf("%v: %v", id, err)
		}
		again, _ := r.sch.ReadProperty(ctx, prop(id))
		if encoded(t, again) != encoded(t, v) {
			t.Fatalf("%v changed in a round trip", id)
		}
		// The grouped form (as read, not as decoded from the wire) is
		// accepted too.
		if err := r.sch.WriteProperty(ctx, Write{Property: prop(id), Value: v}); err != nil {
			t.Fatalf("%v grouped: %v", id, err)
		}
	}
	// Array elements.
	for _, id := range []bacnet.PropertyIdentifier{bacnet.PropertyWeeklySchedule, bacnet.PropertyExceptionSchedule} {
		el, err := r.sch.ReadProperty(ctx, propAt(id, 2))
		if err != nil {
			t.Fatal(err)
		}
		if err := r.sch.WriteProperty(ctx, Write{Property: propAt(id, 1), Value: onWire(t, el)}); err != nil {
			t.Fatalf("%v[1]: %v", id, err)
		}
		first, _ := r.sch.ReadProperty(ctx, propAt(id, 1))
		if encoded(t, first) != encoded(t, el) {
			t.Fatalf("%v[1] not written", id)
		}
	}
	if n, _ := r.sch.ReadProperty(ctx, propAt(bacnet.PropertyWeeklySchedule, 0)); n.Unsigned != 7 {
		t.Fatalf("weekly length %+v", n)
	}
	if got := r.sch.References(); len(got) != 3 || got[0].Device == nil || got[1].Device != nil {
		t.Fatalf("references: %+v", got)
	}
	// A single reference with an array index, written alone.
	idx := uint32(3)
	one := referenceElement(DeviceObjectPropertyReference{Object: r.target.Identifier(), Property: bacnet.PropertyPriorityArray, ArrayIndex: &idx})
	if err := r.sch.WriteProperty(ctx, Write{Property: prop(bacnet.PropertyListOfObjectPropertyReferences), Value: onWire(t, one.Value)}); err != nil {
		t.Fatal(err)
	}
	if got := r.sch.References(); len(got) != 1 || got[0].ArrayIndex == nil || *got[0].ArrayIndex != 3 {
		t.Fatalf("one reference: %+v", got)
	}
	// Priority_Array is not writable: the refused write leaves it as it was.
	r.sch.Poll(ctx, at(2026, 11, 2, 9, 0), r.dev)
	if v := r.slot9(t); v.Kind != bacnet.ValueNull {
		t.Fatalf("refused write: %+v", v)
	}
	if v, _ := r.sch.ReadProperty(ctx, prop(bacnet.PropertyPriorityForWriting)); v.Unsigned != 9 {
		t.Fatalf("Priority_For_Writing %+v", v)
	}
	if v, _ := r.sch.ReadProperty(ctx, prop(bacnet.PropertyScheduleDefault)); v.Real != 18 {
		t.Fatalf("Schedule_Default %+v", v)
	}
	_, req := r.sch.Properties()
	for _, id := range []bacnet.PropertyIdentifier{bacnet.PropertyPresentValue, bacnet.PropertyEffectivePeriod, bacnet.PropertyScheduleDefault,
		bacnet.PropertyListOfObjectPropertyReferences, bacnet.PropertyPriorityForWriting, bacnet.PropertyStatusFlags, bacnet.PropertyReliability, bacnet.PropertyOutOfService} {
		if !req[id] {
			t.Errorf("%v not required", id)
		}
	}
	if req[bacnet.PropertyWeeklySchedule] || req[bacnet.PropertyExceptionSchedule] {
		t.Error("optional schedules marked required")
	}
}

func TestScheduleWriteErrors(t *testing.T) {
	ctx := context.Background()
	r := newScheduleRig(t)
	w := func(ref bacnet.PropertyReference, v bacnet.ApplicationValue) error {
		return r.sch.WriteProperty(ctx, Write{Property: ref, Value: v})
	}
	weekly := prop(bacnet.PropertyWeeklySchedule)
	exc := prop(bacnet.PropertyExceptionSchedule)
	refs := prop(bacnet.PropertyListOfObjectPropertyReferences)
	boolDay := bacnet.EncodeDailySchedule(bacnet.DailySchedule{DaySchedule: []bacnet.ScheduledTimeValue{tv(1, 0, bacnet.BoolValue(true))}})
	var boolWeek bacnet.WeeklySchedule
	boolWeek[6] = bacnet.DailySchedule{DaySchedule: []bacnet.ScheduledTimeValue{tv(1, 0, bacnet.BoolValue(true))}}
	event := func(prio uint8, v bacnet.ApplicationValue) bacnet.ApplicationValue {
		return specialEventElement(bacnet.SpecialEvent{CalendarEntry: holidays()[0], ListOfTimeValues: []bacnet.ScheduledTimeValue{tv(1, 0, v)}, EventPriority: prio}).Value
	}
	good := event(3, bacnet.RealValue(1))
	cases := []struct {
		name string
		err  error
		want error
	}{
		{"weekly primitive", w(weekly, bacnet.RealValue(1)), ErrInvalidDataType},
		{"weekly six days", w(weekly, bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: make([]bacnet.Element, 6)}), ErrInvalidDataType},
		{"weekly wrong datatype", w(weekly, onWire(t, bacnet.EncodeWeeklySchedule(boolWeek))), ErrInvalidDataType},
		{"day wrong datatype", w(propAt(bacnet.PropertyWeeklySchedule, 1), onWire(t, boolDay)), ErrInvalidDataType},
		{"day malformed", w(propAt(bacnet.PropertyWeeklySchedule, 1), bacnet.ApplicationValue{Kind: bacnet.ValueConstructed}), ErrInvalidDataType},
		{"day index 0", w(propAt(bacnet.PropertyWeeklySchedule, 0), onWire(t, boolDay)), ErrWriteAccessDenied},
		{"day index 8", w(propAt(bacnet.PropertyWeeklySchedule, 8), onWire(t, boolDay)), ErrInvalidArrayIndex},
		{"exceptions primitive", w(exc, bacnet.RealValue(1)), ErrInvalidDataType},
		{"event priority 0", w(propAt(bacnet.PropertyExceptionSchedule, 1), onWire(t, event(0, bacnet.RealValue(1)))), ErrValueOutOfRange},
		{"event priority 17", w(exc, onWire(t, event(17, bacnet.RealValue(1)))), ErrValueOutOfRange},
		{"event wrong datatype", w(exc, onWire(t, event(4, bacnet.UnsignedValue(1)))), ErrInvalidDataType},
		{"event index 4", w(propAt(bacnet.PropertyExceptionSchedule, 4), onWire(t, good)), ErrInvalidArrayIndex},
		{"event index 0", w(propAt(bacnet.PropertyExceptionSchedule, 0), onWire(t, good)), ErrWriteAccessDenied},
		{"two events at one index", w(propAt(bacnet.PropertyExceptionSchedule, 1), bacnet.ApplicationValue{Kind: bacnet.ValueConstructed,
			Elements: append(append([]bacnet.Element(nil), good.Elements...), good.Elements...)}), ErrInvalidDataType},
		{"event truncated", w(exc, bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: good.Elements[:2]}), ErrInvalidDataType},
		{"event bad period", w(exc, bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{
			{Context: true, TagNumber: 4, Value: bacnet.UnsignedValue(1)}, good.Elements[1], good.Elements[2]}}), ErrInvalidDataType},
		{"event bad calendar entry", w(exc, bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{
			{Context: true, TagNumber: 0, Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{{Context: true, TagNumber: 6}}}},
			good.Elements[1], good.Elements[2]}}), ErrInvalidDataType},
		{"event bad reference", w(exc, bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{
			{Context: true, TagNumber: 1, Value: bacnet.ApplicationValue{Kind: bacnet.ValueContext, OctetString: []byte{1}}}, good.Elements[1], good.Elements[2]}}), ErrInvalidDataType},
		{"event bad time values", w(exc, bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{
			good.Elements[0], {Context: true, TagNumber: 2, Value: bacnet.UnsignedValue(1)}, good.Elements[2]}}), ErrInvalidDataType},
		{"event time without value", w(exc, bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{
			good.Elements[0], {Context: true, TagNumber: 2, Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{{Value: bacnet.TimeValue(hm(1, 0))}}}}, good.Elements[2]}}), ErrInvalidDataType},
		{"event bad priority tag", w(exc, bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{
			good.Elements[0], good.Elements[1], {Context: true, TagNumber: 4, Value: bacnet.UnsignedValue(1)}}}), ErrInvalidDataType},
		{"event bad priority", w(exc, bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{
			good.Elements[0], good.Elements[1], {Context: true, TagNumber: 3, Value: bacnet.ApplicationValue{Kind: bacnet.ValueContext, OctetString: make([]byte, 9)}}}}), ErrInvalidDataType},
		{"grouped event malformed", w(exc, bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{
			{Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed}}}}), ErrInvalidDataType},
		{"references index", w(propAt(bacnet.PropertyListOfObjectPropertyReferences, 1), bacnet.RealValue(1)), ErrNotAnArray},
		{"references primitive", w(refs, bacnet.RealValue(1)), ErrInvalidDataType},
		{"reference without property", w(refs, bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{
			{Context: true, TagNumber: 0, Value: bacnet.ObjectIDValue(r.target.Identifier())}}}), ErrInvalidDataType},
		{"reference bad object", w(refs, bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{
			{Context: true, TagNumber: 0, Value: bacnet.UnsignedValue(1)}, {Context: true, TagNumber: 1, Value: bacnet.UnsignedValue(85)}}}), ErrInvalidDataType},
		{"reference bad property", w(refs, bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{
			{Context: true, TagNumber: 0, Value: bacnet.ObjectIDValue(r.target.Identifier())}, {Context: true, TagNumber: 1, Value: bacnet.UnsignedValue(1 << 30)}}}), ErrInvalidDataType},
		{"reference bad index", w(refs, bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{
			{Context: true, TagNumber: 0, Value: bacnet.ObjectIDValue(r.target.Identifier())}, {Context: true, TagNumber: 1, Value: bacnet.UnsignedValue(85)},
			{Context: true, TagNumber: 2, Value: bacnet.UnsignedValue(1 << 40)}}}), ErrInvalidDataType},
		{"reference device not a device", w(refs, bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{
			{Context: true, TagNumber: 0, Value: bacnet.ObjectIDValue(r.target.Identifier())}, {Context: true, TagNumber: 1, Value: bacnet.UnsignedValue(85)},
			{Context: true, TagNumber: 3, Value: bacnet.ObjectIDValue(r.target.Identifier())}}}), ErrInvalidDataType},
		{"grouped reference with extra field", w(refs, bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{{Value: bacnet.ApplicationValue{
			Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{
				{Context: true, TagNumber: 0, Value: bacnet.ObjectIDValue(r.target.Identifier())}, {Context: true, TagNumber: 1, Value: bacnet.UnsignedValue(85)},
				{Context: true, TagNumber: 7, Value: bacnet.UnsignedValue(1)}}}}}}), ErrInvalidDataType},
		{"effective period primitive", w(prop(bacnet.PropertyEffectivePeriod), bacnet.DateValue(day(2026, 1, 1))), ErrInvalidDataType},
		{"effective period one date", w(prop(bacnet.PropertyEffectivePeriod), bacnet.ApplicationValue{Kind: bacnet.ValueConstructed,
			Elements: []bacnet.Element{{Value: bacnet.DateValue(day(2026, 1, 1))}}}), ErrInvalidDataType},
		{"effective period index", w(propAt(bacnet.PropertyEffectivePeriod, 1), bacnet.DateValue(day(2026, 1, 1))), ErrNotAnArray},
		{"priority for writing", w(prop(bacnet.PropertyPriorityForWriting), bacnet.UnsignedValue(3)), ErrWriteAccessDenied},
		{"schedule default", w(prop(bacnet.PropertyScheduleDefault), bacnet.RealValue(3)), ErrWriteAccessDenied},
	}
	for _, c := range cases {
		wantErr(t, c.name, c.err, c.want)
	}
	// Nothing above changed the schedule.
	r.sch.Poll(ctx, at(2026, 11, 2, 9, 0), r.dev)
	if pv := r.sch.PresentValue(); pv.Real != 22 {
		t.Fatalf("after refused writes: %+v", pv)
	}

	small, err := NewSchedule(ScheduleConfig{Name: "small", Default: bacnet.RealValue(0), MaxExceptions: 1, MaxReferences: 1})
	if err != nil {
		t.Fatal(err)
	}
	two := bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: append(append([]bacnet.Element(nil), good.Elements...), good.Elements...)}
	wantErr(t, "too many events", small.WriteProperty(ctx, Write{Property: exc, Value: two}), ErrNoSpaceToWriteProperty)
	refList, _ := r.sch.ReadProperty(ctx, refs)
	wantErr(t, "too many references", small.WriteProperty(ctx, Write{Property: refs, Value: refList}), ErrNoSpaceToWriteProperty)
}

func TestScheduleConfig(t *testing.T) {
	cal := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeCalendar, Instance: 1}
	bad := []ScheduleConfig{
		{Instance: bacnet.MaxObjectInstance, Name: "s"},
		{},
		{Name: "s", PriorityForWriting: 17},
		{Name: "s", Default: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed}},
		{Name: "s", MaxExceptions: 1, Exceptions: make([]bacnet.SpecialEvent, 2)},
		{Name: "s", MaxReferences: 1, References: make([]DeviceObjectPropertyReference, 2)},
		{Name: "s", Default: bacnet.RealValue(1), Weekly: bacnet.WeeklySchedule{{DaySchedule: []bacnet.ScheduledTimeValue{tv(1, 0, bacnet.BoolValue(true))}}}},
		{Name: "s", Exceptions: []bacnet.SpecialEvent{{PeriodObject: &cal}}},
		{Name: "s", Exceptions: []bacnet.SpecialEvent{{CalendarEntry: bacnet.CalendarEntry{Choice: 9}, EventPriority: 1}}},
	}
	for i, cfg := range bad {
		if _, err := NewSchedule(cfg); err == nil {
			t.Errorf("config %d accepted", i)
		}
	}
	// A NULL default leaves the datatype to the first value; others must
	// then match it.
	s, err := NewSchedule(ScheduleConfig{Name: "s", EffectivePeriod: &bacnet.DateRange{Start: day(2026, 1, 1), End: day(2026, 12, 31)},
		Weekly: bacnet.WeeklySchedule{{DaySchedule: []bacnet.ScheduledTimeValue{tv(1, 0, bacnet.UnsignedValue(2))}}}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	wantErr(t, "datatype from the first value", s.WriteProperty(ctx, Write{Property: prop(bacnet.PropertyExceptionSchedule),
		Value: specialEventElement(bacnet.SpecialEvent{CalendarEntry: holidays()[0], ListOfTimeValues: []bacnet.ScheduledTimeValue{tv(1, 0, bacnet.RealValue(1))}, EventPriority: 1}).Value}), ErrInvalidDataType)
	s.Poll(ctx, at(2026, 11, 2, 0, 30), nil) // Monday before 01:00: NULL default
	if pv := s.PresentValue(); pv.Kind != bacnet.ValueNull {
		t.Fatalf("NULL default: %+v", pv)
	}
	s.Poll(ctx, at(2026, 11, 2, 1, 0), nil)
	if pv := s.PresentValue(); pv.Kind != bacnet.ValueUnsigned || pv.Unsigned != 2 {
		t.Fatalf("Monday 01:00: %+v", pv)
	}
}

func TestScheduleCalendarReference(t *testing.T) {
	ctx := context.Background()
	dev, _ := NewDevice(DeviceConfig{Instance: 7, Name: "dev"})
	bv, _ := NewBinaryValue(PointConfig{Instance: 1, Name: "flag"})
	_ = dev.Add(bv)
	missing := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeCalendar, Instance: 5}
	flag := bv.Identifier()
	s, err := NewSchedule(ScheduleConfig{Name: "s", Default: bacnet.RealValue(0), Exceptions: []bacnet.SpecialEvent{
		{PeriodObject: &missing, ListOfTimeValues: []bacnet.ScheduledTimeValue{tv(0, 0, bacnet.RealValue(1))}, EventPriority: 1},
		{PeriodObject: &flag, ListOfTimeValues: []bacnet.ScheduledTimeValue{tv(0, 0, bacnet.RealValue(2))}, EventPriority: 2},
	}})
	if err != nil {
		t.Fatal(err)
	}
	s.Poll(ctx, at(2026, 11, 2, 9, 0), dev)
	if s.PresentValue().Real != 0 {
		t.Fatal("a missing calendar or a non-boolean Present_Value is not in effect")
	}
	s.Poll(ctx, at(2026, 11, 2, 9, 0), nil)
	if s.PresentValue().Real != 0 {
		t.Fatal("calendar reference without a device")
	}
}

func TestCompareTime(t *testing.T) {
	if compareTime(hm(8, 0), hm(9, 0)) >= 0 || compareTime(hm(9, 30), hm(9, 0)) <= 0 {
		t.Fatal("order")
	}
	if compareTime(bacnet.Time{Hour: 8, Minute: dateAny, Second: dateAny, Hundredths: dateAny}, hm(8, 59)) != 0 {
		t.Fatal("wildcard")
	}
	if _, ok := valueAt(nil, hm(1, 0)); ok {
		t.Fatal("empty day")
	}
}
