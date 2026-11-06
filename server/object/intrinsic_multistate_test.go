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

func multiStateWithEvents(t *testing.T, input bool, delay uint32) *Point {
	t.Helper()
	cfg := PointConfig{Instance: 1, Name: "MS", NumberOfStates: 5, State: 1, Events: &EventConfig{
		NotificationClass: 2, EventEnable: 7, NotifyType: NotifyAlarm, TimeDelay: delay,
		AlarmValues: []uint32{3, 4}, FaultValues: []uint32{5},
	}}
	newMS := NewMultiStateValue
	if input {
		newMS = NewMultiStateInput
	}
	p, err := newMS(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func readProp(t *testing.T, p *Point, id bacnet.PropertyIdentifier) bacnet.ApplicationValue {
	t.Helper()
	v, err := p.ReadProperty(context.Background(), bacnet.PropertyReference{Identifier: id})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// CHANGE_OF_STATE on Alarm_Values with Time_Delay (clause 13.3.2).
func TestMultiStateChangeOfState(t *testing.T) {
	for _, input := range []bool{false, true} {
		p := multiStateWithEvents(t, input, 10)
		set := func(v uint64) {
			t.Helper()
			if err := p.SetPresentValue(bacnet.UnsignedValue(v)); err != nil {
				t.Fatal(err)
			}
		}
		if tr := p.EvaluateEvents(t0, ackAll); len(tr) != 0 {
			t.Fatalf("normal value reported: %+v", tr)
		}
		set(3)
		if tr := p.EvaluateEvents(t0, ackAll); len(tr) != 0 {
			t.Fatal("reported before the time delay")
		}
		// Another alarm value keeps the condition and its start.
		set(4)
		if tr := p.EvaluateEvents(t0.Add(9*time.Second), ackAll); len(tr) != 0 {
			t.Fatal("reported before the time delay")
		}
		tr := p.EvaluateEvents(t0.Add(10*time.Second), ackAll)
		if len(tr) != 1 || tr[0].To != EventStateOffnormal || tr[0].From != EventStateNormal || tr[0].EventType != EventTypeChangeOfState ||
			tr[0].Transition != TransitionToOffNormal || tr[0].NotificationClass != 2 {
			t.Fatalf("offnormal: %+v", tr)
		}
		cos := tr[0].Params.ChangeOfState
		if cos == nil || cos.NewState.Choice != 11 || cos.NewState.Value.Unsigned != 4 || cos.StatusFlags.Bytes[0]&0x80 == 0 {
			t.Fatalf("change-of-state parameters: %+v", tr[0].Params)
		}
		// The parameters encode as BACnetPropertyStates unsigned-value [11].
		if _, err := service.EncodeNotificationParameters(*tr[0].Params); err != nil {
			t.Fatal(err)
		}
		if info, _ := p.EventInfo(); info.EventState != EventStateOffnormal || info.EventType != EventTypeChangeOfState || info.Acked&EnableToOffNormal != 0 {
			t.Fatalf("info %+v", info)
		}
		if _, err := p.AcknowledgeAlarm(EventStateOffnormal, TimeStampOf(tr[0].TimeStamp)); err != nil {
			t.Fatal(err)
		}
		// Back to a normal value: NORMAL after the time delay.
		set(2)
		if tr := p.EvaluateEvents(t0.Add(11*time.Second), ackAll); len(tr) != 0 {
			t.Fatal("normal before the time delay")
		}
		if tr := p.EvaluateEvents(t0.Add(21*time.Second), ackAll); len(tr) != 1 || tr[0].To != EventStateNormal || tr[0].Params.ChangeOfState.NewState.Value.Unsigned != 2 {
			t.Fatalf("normal: %+v", tr)
		}
		if v := readProp(t, p, bacnet.PropertyEventState); v.Enumerated != EventStateNormal {
			t.Fatalf("Event_State %v", v)
		}
	}
}

// Event_Enable gates the transitions, as for binary points.
func TestMultiStateEventEnable(t *testing.T) {
	p, err := NewMultiStateValue(PointConfig{Instance: 1, Name: "MSV", NumberOfStates: 3, Events: &EventConfig{EventEnable: EnableToOffNormal, AlarmValues: []uint32{2}}})
	if err != nil {
		t.Fatal(err)
	}
	_ = p.SetPresentValue(bacnet.UnsignedValue(2))
	if tr := p.EvaluateEvents(t0, nil); len(tr) != 1 || tr[0].To != EventStateOffnormal {
		t.Fatalf("offnormal: %+v", tr)
	}
	_ = p.SetPresentValue(bacnet.UnsignedValue(1))
	if tr := p.EvaluateEvents(t0.Add(time.Second), nil); len(tr) != 0 {
		t.Fatalf("to-normal disabled but reported: %+v", tr)
	}
	q, _ := NewMultiStateValue(PointConfig{Instance: 2, Name: "MSV2", NumberOfStates: 3, Events: &EventConfig{EventEnable: EnableToNormal, AlarmValues: []uint32{2}}})
	_ = q.SetPresentValue(bacnet.UnsignedValue(2))
	if tr := q.EvaluateEvents(t0, nil); len(tr) != 0 {
		t.Fatalf("to-offnormal disabled but reported: %+v", tr)
	}
}

// Fault_Values: Reliability MULTI_STATE_FAULT (clause 13.4.5) and FAULT
// through change-of-reliability, without time delay.
func TestMultiStateFault(t *testing.T) {
	p := multiStateWithEvents(t, false, 10)
	_ = p.SetPresentValue(bacnet.UnsignedValue(5))
	if v := readProp(t, p, bacnet.PropertyReliability); v.Enumerated != ReliabilityMultiStateFault {
		t.Fatalf("Reliability %v", v)
	}
	if v := readProp(t, p, bacnet.PropertyStatusFlags); v.BitString.Bytes[0]&0x40 == 0 {
		t.Fatalf("Status_Flags %v", v)
	}
	tr := p.EvaluateEvents(t0, ackAll)
	if len(tr) != 1 || tr[0].To != EventStateFault || tr[0].EventType != EventTypeChangeOfReliability || tr[0].Transition != TransitionToFault {
		t.Fatalf("fault: %+v", tr)
	}
	rel, err := bacnet.ContextUnsigned(tr[0].Params.RawElements[0])
	if err != nil || rel != uint64(ReliabilityMultiStateFault) {
		t.Fatalf("reliability parameter %d %v", rel, err)
	}
	if tr := p.EvaluateEvents(t0.Add(time.Second), ackAll); len(tr) != 0 {
		t.Fatalf("fault reported twice: %+v", tr)
	}
	// An application reliability wins over Fault_Values and is a new fault.
	p.SetReliability(7)
	if tr := p.EvaluateEvents(t0.Add(2*time.Second), ackAll); len(tr) != 1 || tr[0].To != EventStateFault {
		t.Fatalf("new fault: %+v", tr)
	}
	if v := readProp(t, p, bacnet.PropertyReliability); v.Enumerated != 7 {
		t.Fatalf("Reliability %v", v)
	}
	p.SetReliability(ReliabilityNoFaultDetected)
	// Leaving the fault value returns to NORMAL at once.
	_ = p.SetPresentValue(bacnet.UnsignedValue(1))
	tr = p.EvaluateEvents(t0.Add(3*time.Second), ackAll)
	if len(tr) != 1 || tr[0].From != EventStateFault || tr[0].To != EventStateNormal || tr[0].EventType != EventTypeChangeOfReliability {
		t.Fatalf("normal: %+v", tr)
	}
	if v := readProp(t, p, bacnet.PropertyReliability); v.Enumerated != ReliabilityNoFaultDetected {
		t.Fatalf("Reliability %v", v)
	}
	// Fault_Values apply while event detection is disabled, as reliability
	// evaluation; the event state does not follow then.
	_ = p.WriteProperty(context.Background(), Write{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyEventDetectionEnable}, Value: bacnet.BoolValue(false)})
	_ = p.SetPresentValue(bacnet.UnsignedValue(5))
	if v := readProp(t, p, bacnet.PropertyReliability); v.Enumerated != ReliabilityMultiStateFault {
		t.Fatalf("Reliability %v", v)
	}
	if tr := p.EvaluateEvents(t0.Add(4*time.Second), ackAll); len(tr) != 0 {
		t.Fatalf("detection disabled but reported: %+v", tr)
	}
}

func TestMultiStateEventProperties(t *testing.T) {
	ctx := context.Background()
	p := multiStateWithEvents(t, false, 0)
	list := func(vals ...uint64) bacnet.ApplicationValue {
		els := make([]bacnet.Element, len(vals))
		for i, v := range vals {
			els[i] = bacnet.Element{Value: bacnet.UnsignedValue(v)}
		}
		return bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: els}
	}
	if v := readProp(t, p, bacnet.PropertyAlarmValues); len(v.Elements) != 2 || v.Elements[0].Value.Unsigned != 3 || v.Elements[1].Value.Unsigned != 4 {
		t.Fatalf("Alarm_Values %+v", v)
	}
	if v := readProp(t, p, bacnet.PropertyFaultValues); len(v.Elements) != 1 || v.Elements[0].Value.Unsigned != 5 {
		t.Fatalf("Fault_Values %+v", v)
	}
	idx := uint32(1)
	for i, k := range []struct {
		prop bacnet.PropertyIdentifier
		idx  *uint32
		v    bacnet.ApplicationValue
		want error
	}{
		{bacnet.PropertyAlarmValues, nil, list(2), nil},
		{bacnet.PropertyAlarmValues, nil, bacnet.UnsignedValue(2), nil},
		{bacnet.PropertyAlarmValues, nil, list(), nil},
		{bacnet.PropertyAlarmValues, nil, list(1, 6), ErrValueOutOfRange},
		{bacnet.PropertyAlarmValues, nil, list(0), ErrValueOutOfRange},
		{bacnet.PropertyAlarmValues, nil, bacnet.EnumValue(1), ErrInvalidDataType},
		{bacnet.PropertyAlarmValues, nil, bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{{Context: true, Value: bacnet.UnsignedValue(1)}}}, ErrInvalidDataType},
		{bacnet.PropertyAlarmValues, &idx, bacnet.UnsignedValue(1), ErrNotAnArray},
		{bacnet.PropertyFaultValues, nil, list(4, 5), nil},
		{bacnet.PropertyFaultValues, nil, list(9), ErrValueOutOfRange},
	} {
		err := p.WriteProperty(ctx, Write{Property: bacnet.PropertyReference{Identifier: k.prop, ArrayIndex: k.idx}, Value: k.v})
		if (k.want == nil && err != nil) || (k.want != nil && !errors.Is(err, k.want)) {
			t.Errorf("case %d (%d): %v", i, k.prop, err)
		}
	}
	// The failed writes left the last good ones.
	if v := readProp(t, p, bacnet.PropertyAlarmValues); len(v.Elements) != 0 {
		t.Fatalf("Alarm_Values %+v", v)
	}
	if v := readProp(t, p, bacnet.PropertyFaultValues); len(v.Elements) != 2 {
		t.Fatalf("Fault_Values %+v", v)
	}
	// A write of Fault_Values changes Reliability at once.
	_ = p.SetPresentValue(bacnet.UnsignedValue(4))
	if v := readProp(t, p, bacnet.PropertyReliability); v.Enumerated != ReliabilityMultiStateFault {
		t.Fatalf("Reliability %v", v)
	}
	// Configured values outside 1..Number_Of_States are refused.
	for _, ev := range []EventConfig{{AlarmValues: []uint32{0}}, {FaultValues: []uint32{6}}} {
		ev := ev
		if _, err := NewMultiStateInput(PointConfig{Instance: 1, Name: "MSI", NumberOfStates: 5, Events: &ev}); err == nil {
			t.Errorf("accepted %+v", ev)
		}
	}
	// Without Events a multi-state point has no event properties.
	plain, _ := NewMultiStateValue(PointConfig{Instance: 3, Name: "MSV3", NumberOfStates: 2})
	if _, err := plain.ReadProperty(ctx, bacnet.PropertyReference{Identifier: bacnet.PropertyAlarmValues}); err == nil {
		t.Fatal("Alarm_Values without events")
	}
	if v := readProp(t, plain, bacnet.PropertyReliability); v.Enumerated != ReliabilityNoFaultDetected {
		t.Fatalf("Reliability %v", v)
	}
}
