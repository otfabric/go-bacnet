// SPDX-License-Identifier: MIT

package object

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func ackAll(uint32, int) bool { return true }

func analogWithEvents(t *testing.T, delay uint32) *Point {
	t.Helper()
	p, err := NewAnalogInput(PointConfig{Instance: 1, Name: "AI", Value: 50, Events: &EventConfig{
		NotificationClass: 3, EventEnable: 7, NotifyType: NotifyAlarm, TimeDelay: delay,
		HighLimit: 80, LowLimit: 20, Deadband: 5, LimitEnable: LimitEnableHigh | LimitEnableLow,
	}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestOutOfRange(t *testing.T) {
	p := analogWithEvents(t, 0)
	now := t0
	step := func(v float32) []EventTransition {
		_ = p.SetPresentValue(bacnet.RealValue(v))
		now = now.Add(time.Second)
		return p.EvaluateEvents(now, ackAll)
	}
	if tr := step(60); len(tr) != 0 {
		t.Fatalf("normal: %+v", tr)
	}
	tr := step(90)
	if len(tr) != 1 || tr[0].To != EventStateHighLimit || tr[0].EventType != EventTypeOutOfRange || tr[0].Params.OutOfRange.ExceededLimit != 80 {
		t.Fatalf("high: %+v", tr)
	}
	// Within the deadband the state holds.
	if tr := step(77); len(tr) != 0 {
		t.Fatalf("deadband: %+v", tr)
	}
	tr = step(70)
	if len(tr) != 1 || tr[0].To != EventStateNormal || tr[0].From != EventStateHighLimit || tr[0].Params.OutOfRange.ExceededLimit != 80 {
		t.Fatalf("back to normal: %+v", tr)
	}
	tr = step(10)
	if len(tr) != 1 || tr[0].To != EventStateLowLimit {
		t.Fatalf("low: %+v", tr)
	}
	if tr := step(30); len(tr) != 1 || tr[0].To != EventStateNormal {
		t.Fatalf("low to normal: %+v", tr)
	}
	info, ok := p.EventInfo()
	if !ok || info.Acked != 1<<TransitionToFault || info.TimeStamps[TransitionToOffNormal].IsZero() || info.EventType != EventTypeOutOfRange {
		t.Fatalf("info: %+v", info)
	}
	// Disabling the low limit while in LOW_LIMIT returns to NORMAL.
	_ = step(10)
	ctx := context.Background()
	if err := p.WriteProperty(ctx, Write{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyLimitEnable}, Value: maskBits(2, LimitEnableHigh)}); err != nil {
		t.Fatal(err)
	}
	if tr := p.EvaluateEvents(now.Add(time.Second), ackAll); len(tr) != 1 || tr[0].To != EventStateNormal {
		t.Fatalf("limit disabled: %+v", tr)
	}
}

func TestTimeDelayAndFault(t *testing.T) {
	p := analogWithEvents(t, 10)
	_ = p.SetPresentValue(bacnet.RealValue(95))
	if tr := p.EvaluateEvents(t0, ackAll); len(tr) != 0 {
		t.Fatal("reported before the time delay")
	}
	if tr := p.EvaluateEvents(t0.Add(9*time.Second), ackAll); len(tr) != 0 {
		t.Fatal("reported at 9 s")
	}
	if tr := p.EvaluateEvents(t0.Add(10*time.Second), ackAll); len(tr) != 1 {
		t.Fatal("not reported at 10 s")
	}
	// A fault overrides the limit state and needs no delay.
	p.SetReliability(7)
	tr := p.EvaluateEvents(t0.Add(11*time.Second), nil)
	if len(tr) != 1 || tr[0].To != EventStateFault || tr[0].EventType != EventTypeChangeOfReliability || len(tr[0].Params.RawElements) != 3 {
		t.Fatalf("fault: %+v", tr)
	}
	// A different fault is reported again; the same one is not.
	if tr := p.EvaluateEvents(t0.Add(12*time.Second), nil); len(tr) != 0 {
		t.Fatal("same fault reported twice")
	}
	p.SetReliability(3)
	if tr := p.EvaluateEvents(t0.Add(13*time.Second), nil); len(tr) != 1 || tr[0].To != EventStateFault {
		t.Fatalf("new fault: %+v", tr)
	}
	p.SetReliability(ReliabilityNoFaultDetected)
	tr = p.EvaluateEvents(t0.Add(14*time.Second), nil)
	if len(tr) != 1 || tr[0].To != EventStateNormal || tr[0].EventType != EventTypeChangeOfReliability {
		t.Fatalf("fault cleared: %+v", tr)
	}
	// A condition that ends before the delay is not reported.
	if tr := p.EvaluateEvents(t0.Add(15*time.Second), nil); len(tr) != 0 {
		t.Fatal("unexpected")
	}
	_ = p.SetPresentValue(bacnet.RealValue(50))
	if tr := p.EvaluateEvents(t0.Add(40*time.Second), nil); len(tr) != 0 {
		t.Fatal("cleared condition reported")
	}
	// Event_Detection_Enable FALSE stops detection.
	ctx := context.Background()
	_ = p.WriteProperty(ctx, Write{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyEventDetectionEnable}, Value: bacnet.BoolValue(false)})
	p.SetReliability(7)
	if tr := p.EvaluateEvents(t0.Add(41*time.Second), nil); len(tr) != 0 {
		t.Fatal("detection disabled but reported")
	}
}

func TestChangeOfState(t *testing.T) {
	p, err := NewBinaryValue(PointConfig{Instance: 1, Name: "BV", Events: &EventConfig{NotificationClass: 1, EventEnable: 7, NotifyType: NotifyEvent, AlarmValue: 1}})
	if err != nil {
		t.Fatal(err)
	}
	_ = p.SetPresentValue(bacnet.EnumValue(1))
	tr := p.EvaluateEvents(t0, nil)
	if len(tr) != 1 || tr[0].To != EventStateOffnormal || tr[0].EventType != EventTypeChangeOfState || tr[0].Params.ChangeOfState.NewState.Value.Enumerated != 1 {
		t.Fatalf("offnormal: %+v", tr)
	}
	_ = p.SetPresentValue(bacnet.EnumValue(0))
	if tr := p.EvaluateEvents(t0.Add(time.Second), nil); len(tr) != 1 || tr[0].To != EventStateNormal {
		t.Fatalf("normal: %+v", tr)
	}
	if info, _ := p.EventInfo(); info.Acked != 7 {
		t.Fatal("transitions without Ack_Required must stay acknowledged")
	}
	if _, err := NewMultiStateOutput(PointConfig{Instance: 1, Name: "MSO", NumberOfStates: 2, Events: &EventConfig{}}); err == nil {
		t.Fatal("multi-state output events accepted")
	}
	for _, cfg := range []EventConfig{{NotifyType: 2}, {Deadband: -1}} {
		cfg := cfg
		if _, err := NewAnalogValue(PointConfig{Instance: 1, Name: "AV", Events: &cfg}); err == nil {
			t.Errorf("accepted %+v", cfg)
		}
	}
	plain, _ := NewAnalogValue(PointConfig{Instance: 2, Name: "AV2"})
	if tr := plain.EvaluateEvents(t0, nil); tr != nil {
		t.Fatal("no events configured")
	}
	if _, ok := plain.EventInfo(); ok {
		t.Fatal("info without events")
	}
	if _, err := plain.AcknowledgeAlarm(EventStateNormal, service.TimeStamp{}); !errors.Is(err, ErrNoAlarmConfigured) {
		t.Fatal(err)
	}
}

func TestAcknowledgeAlarm(t *testing.T) {
	p := analogWithEvents(t, 0)
	_ = p.SetPresentValue(bacnet.RealValue(99))
	at := time.Date(2026, 5, 4, 3, 2, 1, 0, time.Local)
	if tr := p.EvaluateEvents(at, ackAll); len(tr) != 1 {
		t.Fatal("no transition")
	}
	if _, err := p.AcknowledgeAlarm(EventStateHighLimit, service.TimeStamp{Choice: service.TimeStampSequence}); !errors.Is(err, ErrInvalidTimeStamp) {
		t.Fatalf("sequence stamp: %v", err)
	}
	if _, err := p.AcknowledgeAlarm(EventStateHighLimit, TimeStampOf(at.Add(-time.Second))); !errors.Is(err, ErrInvalidTimeStamp) {
		t.Fatalf("old stamp: %v", err)
	}
	if _, err := p.AcknowledgeAlarm(9, TimeStampOf(at)); !errors.Is(err, ErrInvalidEventState) {
		t.Fatalf("bad state: %v", err)
	}
	ev, err := p.AcknowledgeAlarm(EventStateHighLimit, TimeStampOf(at))
	if err != nil || ev.NotifyType != NotifyAckNotification || ev.Params != nil || !ev.TimeStamp.Equal(at) {
		t.Fatalf("ack: %v %+v", err, ev)
	}
	// Acknowledged again: accepted for the current state only.
	if _, err := p.AcknowledgeAlarm(EventStateHighLimit, TimeStampOf(at)); err != nil {
		t.Fatalf("re-ack current: %v", err)
	}
	if _, err := p.AcknowledgeAlarm(EventStateFault, TimeStampOf(at)); !errors.Is(err, ErrInvalidEventState) {
		t.Fatalf("ack of other state: %v", err)
	}
	ctx := context.Background()
	v, err := p.ReadProperty(ctx, bacnet.PropertyReference{Identifier: bacnet.PropertyEventTimeStamps})
	if err != nil || len(v.Elements) != 3 || v.Elements[TransitionToFault].Value.Elements[0].Value.Date.Year != 0xFF {
		t.Fatalf("Event_Time_Stamps: %v %+v", err, v)
	}
	if v, _ := p.ReadProperty(ctx, bacnet.PropertyReference{Identifier: bacnet.PropertyAckedTransitions}); !bit(v, TransitionToOffNormal) {
		t.Fatal("Acked_Transitions")
	}
}

func TestEventPropertyWrites(t *testing.T) {
	ctx := context.Background()
	p := analogWithEvents(t, 0)
	b, _ := NewBinaryInput(PointConfig{Instance: 1, Name: "BI", Events: &EventConfig{}})
	idx := uint32(1)
	type c struct {
		o    *Point
		prop bacnet.PropertyIdentifier
		idx  *uint32
		v    bacnet.ApplicationValue
		want error
	}
	for i, k := range []c{
		{p, bacnet.PropertyNotificationClass, nil, bacnet.UnsignedValue(4), nil},
		{p, bacnet.PropertyNotificationClass, nil, bacnet.UnsignedValue(1 << 33), ErrValueOutOfRange},
		{p, bacnet.PropertyNotificationClass, nil, bacnet.RealValue(1), ErrInvalidDataType},
		{p, bacnet.PropertyNotificationClass, &idx, bacnet.UnsignedValue(1), ErrNotAnArray},
		{p, bacnet.PropertyTimeDelay, nil, bacnet.UnsignedValue(5), nil},
		{p, bacnet.PropertyEventEnable, nil, maskBits(3, 1), nil},
		{p, bacnet.PropertyEventEnable, nil, bacnet.UnsignedValue(1), ErrInvalidDataType},
		{p, bacnet.PropertyEventEnable, &idx, maskBits(3, 1), ErrNotAnArray},
		{p, bacnet.PropertyNotifyType, nil, bacnet.EnumValue(NotifyEvent), nil},
		{p, bacnet.PropertyNotifyType, nil, bacnet.EnumValue(2), ErrValueOutOfRange},
		{p, bacnet.PropertyNotifyType, nil, bacnet.UnsignedValue(1), ErrInvalidDataType},
		{p, bacnet.PropertyNotifyType, &idx, bacnet.EnumValue(0), ErrNotAnArray},
		{p, bacnet.PropertyEventDetectionEnable, nil, bacnet.UnsignedValue(1), ErrInvalidDataType},
		{p, bacnet.PropertyEventDetectionEnable, &idx, bacnet.BoolValue(true), ErrNotAnArray},
		{p, bacnet.PropertyHighLimit, nil, bacnet.RealValue(100), nil},
		{p, bacnet.PropertyHighLimit, nil, bacnet.UnsignedValue(1), ErrInvalidDataType},
		{p, bacnet.PropertyHighLimit, &idx, bacnet.RealValue(1), ErrNotAnArray},
		{p, bacnet.PropertyDeadband, nil, bacnet.RealValue(-1), ErrValueOutOfRange},
		{b, bacnet.PropertyAlarmValue, nil, bacnet.EnumValue(0), nil},
		{b, bacnet.PropertyAlarmValue, nil, bacnet.EnumValue(2), ErrValueOutOfRange},
		{b, bacnet.PropertyAlarmValue, nil, bacnet.UnsignedValue(1), ErrInvalidDataType},
		{b, bacnet.PropertyAlarmValue, &idx, bacnet.EnumValue(1), ErrNotAnArray},
	} {
		err := k.o.WriteProperty(ctx, Write{Property: bacnet.PropertyReference{Identifier: k.prop, ArrayIndex: k.idx}, Value: k.v})
		if (k.want == nil && err != nil) || (k.want != nil && !errors.Is(err, k.want)) {
			t.Errorf("case %d (%d): %v", i, k.prop, err)
		}
	}
	if v, _ := p.ReadProperty(ctx, bacnet.PropertyReference{Identifier: bacnet.PropertyNotificationClass}); v.Unsigned != 4 {
		t.Fatal("notification class not written")
	}
}
