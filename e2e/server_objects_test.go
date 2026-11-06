// SPDX-License-Identifier: MIT

package e2e_test

import (
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/server"
	"github.com/otfabric/go-bacnet/server/object"
	"github.com/otfabric/go-bacnet/service"
)

// allObjectTypes returns one object of every built-in type that the default
// test server does not already hold, configured so that the optional
// properties (intrinsic reporting, priority arrays, lists) are present.
func allObjectTypes(t *testing.T) []object.Object {
	t.Helper()
	must := func(o object.Object, err error) object.Object {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return o
	}
	events := &object.EventConfig{
		NotificationClass: 1, EventEnable: object.EnableToOffNormal | object.EnableToFault | object.EnableToNormal,
		NotifyType: object.NotifyAlarm, HighLimit: 80, LowLimit: 0, Deadband: 1,
		LimitEnable: object.LimitEnableLow | object.LimitEnableHigh,
	}
	cal := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeCalendar, Instance: 1}
	device := dev
	morning := bacnet.Time{Hour: 7}
	evening := bacnet.Time{Hour: 19}
	var week bacnet.WeeklySchedule
	for d := range 5 {
		week[d].DaySchedule = []bacnet.ScheduledTimeValue{
			{Time: morning, Value: bacnet.RealValue(21)},
			{Time: evening, Value: bacnet.NullValue()},
		}
	}
	return []object.Object{
		must(object.NewAnalogOutput(object.PointConfig{Instance: 1, Name: "AO-1", Units: 98, Value: 50, Events: events})),
		must(object.NewBinaryInput(object.PointConfig{Instance: 1, Name: "BI-1", ActiveText: "on", InactiveText: "off"})),
		must(object.NewBinaryOutput(object.PointConfig{Instance: 1, Name: "BO-1", ActiveText: "on", InactiveText: "off"})),
		must(object.NewMultiStateInput(object.PointConfig{Instance: 1, Name: "MSI-1", NumberOfStates: 2, StateText: []string{"x", "y"}})),
		must(object.NewMultiStateOutput(object.PointConfig{Instance: 1, Name: "MSO-1", NumberOfStates: 3})),
		must(object.NewNotificationClass(object.NotificationClassConfig{
			Instance: 1, Name: "NC-1", Priority: &[3]uint8{100, 200, 50}, AckRequired: 1 << object.TransitionToOffNormal,
			Recipients: []object.Destination{{
				ValidDays: 0x7f, To: bacnet.Time{Hour: 23, Minute: 59, Second: 59, Hundredths: 99},
				Device: &bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 4000}, ProcessID: 7,
				Transitions: 1<<object.TransitionToOffNormal | 1<<object.TransitionToFault | 1<<object.TransitionToNormal,
			}},
		})),
		must(object.NewTrendLog(object.TrendLogConfig{
			Instance: 1, Name: "TL-1", BufferSize: 10, Source: &av1, SourceProperty: bacnet.PropertyPresentValue,
			LogInterval: time.Minute, Enable: true,
		})),
		must(object.NewFile(object.FileConfig{Instance: 1, Name: "FILE-1", FileType: "text", Data: []byte("hello")})),
		must(object.NewFile(object.FileConfig{Instance: 2, Name: "FILE-2", Record: true, Records: [][]byte{[]byte("r0"), []byte("r1")}})),
		must(object.NewCalendar(object.CalendarConfig{Instance: 1, Name: "CAL-1", DateList: []bacnet.CalendarEntry{
			{Choice: bacnet.CalendarEntryDate, Date: bacnet.Date{Year: 126, Month: 12, Day: 25, Weekday: 0xff}},
			{Choice: bacnet.CalendarEntryDateRange, DateRange: bacnet.DateRange{
				Start: bacnet.Date{Year: 126, Month: 7, Day: 1, Weekday: 0xff}, End: bacnet.Date{Year: 126, Month: 7, Day: 31, Weekday: 0xff},
			}},
			{Choice: bacnet.CalendarEntryWeekNDay, WeekNDay: bacnet.WeekNDay{Month: 0xff, WeekOfMonth: 0xff, DayOfWeek: 7}},
		}})),
		must(object.NewSchedule(object.ScheduleConfig{
			Instance: 1, Name: "SCH-1", Weekly: week, Default: bacnet.RealValue(16),
			Exceptions: []bacnet.SpecialEvent{{
				PeriodObject:     &cal,
				ListOfTimeValues: []bacnet.ScheduledTimeValue{{Time: morning, Value: bacnet.RealValue(18)}},
				EventPriority:    8,
			}},
			References: []object.DeviceObjectPropertyReference{{Object: av1, Property: bacnet.PropertyPresentValue}},
		})),
		must(object.NewStructuredView(object.StructuredViewConfig{
			Instance: 1, Name: "SV-1", NodeSubtype: "floor",
			Subordinates: []object.DeviceObjectReference{{Object: av1}, {Device: &device, Object: bv1}},
			Annotations:  []string{"setpoint", "fan"},
		})),
		must(object.NewCharacterStringValue(object.ValueConfig{Instance: 1, Name: "CSV-1", Text: "text", Commandable: true})),
		must(object.NewIntegerValue(object.ValueConfig{Instance: 1, Name: "IV-1", Integer: -5, Units: 95})),
		must(object.NewPositiveIntegerValue(object.ValueConfig{Instance: 1, Name: "PIV-1", Unsigned: 5, Units: 95})),
	}
}

// TestServerAllObjectTypes reads every property of every built-in object
// type with ReadPropertyMultiple ALL, one object per request and then the
// whole device in one segmented request. Every property the object lists
// must decode; this also feeds the dissector suite every encoder.
func TestServerAllObjectTypes(t *testing.T) {
	s := startServer(t, server.WithSegmentation(64, 16))
	for _, o := range allObjectTypes(t) {
		if err := s.dev.Add(o); err != nil {
			t.Fatal(err)
		}
	}
	c := newClient(t)
	ctx := ctxT(t)
	all := []bacnet.PropertyReference{{Identifier: bacnet.PropertyAll}}
	var specs []service.ReadAccessSpecification
	for _, id := range s.dev.ObjectIdentifiers() {
		specs = append(specs, service.ReadAccessSpecification{Object: id, Properties: all})
		res, err := c.ReadPropertyMultiple(ctx, serverTarget(s), specs[len(specs)-1:])
		if err != nil {
			t.Fatalf("%v: %v", id, err)
		}
		if len(res) != 1 || len(res[0].Properties) < 4 {
			t.Fatalf("%v: %+v", id, res)
		}
		for _, p := range res[0].Properties {
			// Log_Buffer is read with ReadRange only, as bacnet-stack does:
			// read-access-denied is the answer.
			if p.Property.Identifier == bacnet.PropertyLogBuffer && p.Err != nil {
				continue
			}
			if p.Err != nil {
				t.Errorf("%v %v: %v", id, p.Property.Identifier, p.Err)
			}
		}
	}
	res, err := c.ReadPropertyMultiple(ctx, serverTarget(s), specs)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != len(specs) {
		t.Fatalf("%d results for %d objects", len(res), len(specs))
	}
}
