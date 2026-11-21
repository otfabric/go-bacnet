// SPDX-License-Identifier: MIT

package e2e_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/server"
	"github.com/otfabric/go-bacnet/server/object"
	"github.com/otfabric/go-bacnet/service"
)

// A client reads a Schedule's Exception_Schedule as typed special events,
// writes a new one with EncodeExceptionSchedule and reads it back; the
// server refuses an event priority outside 1..16 with value-out-of-range.
func TestServerExceptionScheduleCodec(t *testing.T) {
	s := startServer(t)
	cal := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeCalendar, Instance: 1}
	sch := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeSchedule, Instance: 1}
	calendar, err := object.NewCalendar(object.CalendarConfig{Instance: 1, Name: "CAL-1"})
	if err != nil {
		t.Fatal(err)
	}
	initial := bacnet.ExceptionSchedule{SpecialEvents: []bacnet.SpecialEvent{{
		PeriodObject:     &cal,
		ListOfTimeValues: []bacnet.ScheduledTimeValue{{Time: bacnet.Time{Hour: 7}, Value: bacnet.RealValue(18)}},
		EventPriority:    8,
	}}}
	schedule, err := object.NewSchedule(object.ScheduleConfig{
		Instance: 1, Name: "SCH-1", Default: bacnet.RealValue(16), Exceptions: initial.SpecialEvents,
		References: []object.DeviceObjectPropertyReference{{Object: av1, Property: bacnet.PropertyPresentValue}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range []object.Object{calendar, schedule} {
		if err := s.dev.Add(o); err != nil {
			t.Fatal(err)
		}
	}
	c := newClient(t)
	ctx := ctxT(t)
	target := serverTarget(s)
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyExceptionSchedule}
	read := func() bacnet.ExceptionSchedule {
		t.Helper()
		v, err := c.ReadProperty(ctx, target, sch, prop)
		if err != nil {
			t.Fatal(err)
		}
		got, err := bacnet.DecodeExceptionSchedule(v)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := read(); !reflect.DeepEqual(got, initial) {
		t.Fatalf("read:\n got %+v\nwant %+v", got, initial)
	}

	next := bacnet.ExceptionSchedule{SpecialEvents: []bacnet.SpecialEvent{
		{CalendarEntry: bacnet.CalendarEntry{Choice: bacnet.CalendarEntryDate, Date: bacnet.Date{Year: 126, Month: 12, Day: 25, Weekday: 0xFF}},
			ListOfTimeValues: []bacnet.ScheduledTimeValue{{Time: bacnet.Time{Hour: 0}, Value: bacnet.RealValue(15)}}, EventPriority: 1},
		{CalendarEntry: bacnet.CalendarEntry{Choice: bacnet.CalendarEntryWeekNDay, WeekNDay: bacnet.WeekNDay{Month: 0xFF, WeekOfMonth: 0xFF, DayOfWeek: 6}},
			ListOfTimeValues: []bacnet.ScheduledTimeValue{{Time: bacnet.Time{Hour: 8}, Value: bacnet.RealValue(19)}, {Time: bacnet.Time{Hour: 12}, Value: bacnet.NullValue()}},
			EventPriority:    16},
		{PeriodObject: &cal, ListOfTimeValues: []bacnet.ScheduledTimeValue{}, EventPriority: 4},
	}}
	v, err := bacnet.EncodeExceptionSchedule(next)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.WriteProperty(ctx, target, sch, prop, v, nil); err != nil {
		t.Fatal(err)
	}
	if got := read(); !reflect.DeepEqual(got, next) {
		t.Fatalf("after the write:\n got %+v\nwant %+v", got, next)
	}

	// The encoder refuses these priorities, so the event is built by hand.
	// 256 does not fit the decoder's uint8 and must still answer
	// value-out-of-range, not invalid-data-type.
	for _, prio := range []uint64{0, 17, 256} {
		bad, err := bacnet.EncodeExceptionSchedule(bacnet.ExceptionSchedule{SpecialEvents: next.SpecialEvents[:1]})
		if err != nil {
			t.Fatal(err)
		}
		bad.Elements[0].Value.Elements[2] = bacnet.Element{Context: true, TagNumber: 3, Value: bacnet.UnsignedValue(prio)}
		err = c.WriteProperty(ctx, target, sch, prop, bad, nil)
		var er *bacnet.ErrorResponse
		if !errors.As(err, &er) || er.Code != bacnet.ErrorCodeValueOutOfRange {
			t.Fatalf("priority %d: %v, want value-out-of-range", prio, err)
		}
	}
	if got := read(); !reflect.DeepEqual(got, next) {
		t.Fatalf("a refused write changed Exception_Schedule: %+v", got)
	}
}

// InvokeUnconfirmed delivers an unconfirmed request to a device addressed by
// a Target; the server runs its TimeSync hook.
func TestServerInvokeUnconfirmed(t *testing.T) {
	synced := make(chan service.TimeSynchronization, 1)
	s := startServer(t, server.WithHooks(server.Hooks{
		TimeSync: func(ts service.TimeSynchronization, utc bool) {
			if !utc {
				synced <- ts
			}
		},
	}))
	c := newClient(t)
	ts := service.TimeSynchronization{Date: bacnet.Date{Year: 126, Month: 11, Day: 19, Weekday: 4}, Time: bacnet.Time{Hour: 9, Minute: 30}}
	payload, err := service.EncodeTimeSynchronization(ts)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.InvokeUnconfirmed(ctxT(t), serverTarget(s), apdu.ServiceTimeSynchronization, payload); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-synced:
		if got.Date != ts.Date || got.Time != ts.Time {
			t.Fatalf("TimeSync hook got %+v, want %+v", got, ts)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the server's TimeSync hook was not called")
	}
}
