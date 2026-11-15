// SPDX-License-Identifier: MIT

package object

import (
	"context"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

// What an object keeps is its own: the caller may change what it passed in
// afterwards, and what an object hands out may be changed by whoever gets
// it, without the object's state moving (API.md, "Byte ownership").

func TestTrendLogKeepsItsOwnStatusFlags(t *testing.T) {
	tl, err := NewTrendLog(TrendLogConfig{Instance: 1, Name: "TL", BufferSize: 4, Enable: true})
	if err != nil {
		t.Fatal(err)
	}
	flags := &bacnet.BitString{UnusedBits: 4, Bytes: []byte{0x80}}
	if err := tl.Log(time.Date(2026, 1, 1, 0, 0, 0, 0, time.Local), bacnet.RealValue(1), flags); err != nil {
		t.Fatal(err)
	}
	flags.UnusedBits, flags.Bytes[0] = 0, 0x10 // the caller reuses its value
	res, err := tl.ReadRange(context.Background(), bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer}, RangeQuery{By: service.ReadRangeAll})
	if err != nil || len(res.Records) != 1 || res.Records[0].StatusFlags == nil {
		t.Fatalf("records %+v, %v", res.Records, err)
	}
	if got := *res.Records[0].StatusFlags; got.UnusedBits != 4 || len(got.Bytes) != 1 || got.Bytes[0] != 0x80 {
		t.Fatalf("logged status flags %+v, want the value at the time of Log", got)
	}
}

func TestNotificationClassKeepsItsOwnRecipients(t *testing.T) {
	dev := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 200}
	mac := []byte{10, 0, 0, 2, 0xBA, 0xC0}
	allDay := bacnet.Time{Hour: 23, Minute: 59, Second: 59, Hundredths: 99}
	recipients := []Destination{
		{ValidDays: 0x7F, To: allDay, Device: &dev, ProcessID: 1, Transitions: 7},
		{ValidDays: 0x7F, To: allDay, MAC: mac, ProcessID: 2, Transitions: 7},
	}
	nc, err := NewNotificationClass(NotificationClassConfig{Instance: 1, Name: "NC", Recipients: recipients})
	if err != nil {
		t.Fatal(err)
	}
	check := func(when string) {
		t.Helper()
		_, _, to := nc.Routing(time.Date(2026, 1, 5, 12, 0, 0, 0, time.Local), TransitionToOffNormal)
		if len(to) != 2 || to[0].Device == nil || to[0].Device.Instance != 200 || string(to[1].MAC) != string([]byte{10, 0, 0, 2, 0xBA, 0xC0}) {
			t.Fatalf("%s: recipients %+v", when, to)
		}
	}
	check("at first")
	// The caller changes what it configured the class with.
	dev.Instance, mac[0] = 999, 99
	recipients[0].ProcessID = 77
	check("after the caller changed its configuration")
	// A caller of Routing changes what it got.
	_, _, to := nc.Routing(time.Date(2026, 1, 5, 12, 0, 0, 0, time.Local), TransitionToOffNormal)
	to[0].Device.Instance, to[1].MAC[0] = 555, 55
	check("after a caller changed the result of Routing")
}

func TestScheduleKeepsItsOwnReferences(t *testing.T) {
	idx := uint32(3)
	dev := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
	refs := []DeviceObjectPropertyReference{{
		Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}, Property: bacnet.PropertyPriorityArray, ArrayIndex: &idx, Device: &dev,
	}}
	s, err := NewSchedule(ScheduleConfig{Instance: 1, Name: "SCH", Default: bacnet.RealValue(1), References: refs})
	if err != nil {
		t.Fatal(err)
	}
	check := func(when string) {
		t.Helper()
		got := s.References()
		if len(got) != 1 || got[0].ArrayIndex == nil || *got[0].ArrayIndex != 3 || got[0].Device == nil || got[0].Device.Instance != 1 {
			t.Fatalf("%s: references %+v", when, got)
		}
	}
	check("at first")
	idx, dev.Instance = 9, 9 // the caller changes what it configured the schedule with
	check("after the caller changed its configuration")
	got := s.References()
	*got[0].ArrayIndex, got[0].Device.Instance = 7, 7 // a caller changes what it got
	check("after a caller changed the result of References")
}

// The reference a Schedule writes through reaches the BeforeWrite guard
// and the object with an array index of its own.
func TestScheduleWritesWithItsOwnArrayIndex(t *testing.T) {
	d := newTestDevice(t, DeviceConfig{Instance: 1})
	idx := uint32(2)
	target := &indexKeeper{Base: NewBase(bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 5}, "target")}
	if err := d.Add(target); err != nil {
		t.Fatal(err)
	}
	s, err := NewSchedule(ScheduleConfig{Instance: 1, Name: "SCH", Default: bacnet.RealValue(1), References: []DeviceObjectPropertyReference{{
		Object: target.Identifier(), Property: bacnet.PropertyPresentValue, ArrayIndex: &idx,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Add(s); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteProperty(context.Background(), Write{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyOutOfService}, Value: bacnet.BoolValue(true)}); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteProperty(context.Background(), Write{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}, Value: bacnet.RealValue(2)}); err != nil {
		t.Fatal(err)
	}
	s.Poll(context.Background(), time.Date(2026, 1, 5, 12, 0, 0, 0, time.Local), d)
	if target.writes != 1 {
		t.Fatalf("%d writes reached the target, want 1", target.writes)
	}
	// The target changed the index it was given; the schedule's is as it was.
	if got := s.References(); got[0].ArrayIndex == nil || *got[0].ArrayIndex != 2 {
		t.Fatalf("references after the write: %+v", got)
	}
}

// indexKeeper is an object that changes the array index of a write it gets.
type indexKeeper struct {
	*Base
	writes int
}

func (o *indexKeeper) WriteProperty(_ context.Context, w Write) error {
	o.writes++
	if w.Property.ArrayIndex != nil {
		*w.Property.ArrayIndex = 99
	}
	return nil
}
