// SPDX-License-Identifier: MIT

package object

import (
	"math"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

// Event types (BACnetEventType) used by intrinsic reporting.
const (
	EventTypeChangeOfState       uint32 = 1
	EventTypeOutOfRange          uint32 = 5
	EventTypeChangeOfReliability uint32 = 19
)

// Notify types (BACnetNotifyType).
const (
	NotifyAlarm           uint32 = 0
	NotifyEvent           uint32 = 1
	NotifyAckNotification uint32 = 2
)

// BACnetLimitEnable bits.
const (
	LimitEnableLow  uint8 = 1 << 0
	LimitEnableHigh uint8 = 1 << 1
)

// Event enable bits (BACnetEventTransitionBits as a mask).
const (
	EnableToOffNormal uint8 = 1 << TransitionToOffNormal
	EnableToFault     uint8 = 1 << TransitionToFault
	EnableToNormal    uint8 = 1 << TransitionToNormal
)

// Alarm acknowledgment errors, with the codes bacnet-stack answers (ai.c
// Analog_Input_Alarm_Ack, h_alarm_ack.c).
var (
	ErrInvalidTimeStamp  = Err(bacnet.ErrorClassServices, bacnet.ErrorCodeInvalidTimeStamp)
	ErrInvalidEventState = Err(bacnet.ErrorClassServices, bacnet.ErrorCodeInvalidEventState)
	ErrNoAlarmConfigured = Err(bacnet.ErrorClassObject, bacnet.ErrorCodeNoAlarmConfigured)
)

// EventConfig enables intrinsic reporting on a point (clause 13.2):
// OUT_OF_RANGE for analog objects, CHANGE_OF_STATE for binary objects and
// for Multi-state Input and Value objects (clauses 12.18, 12.20), and
// FAULT for any object whose Reliability is not no-fault-detected.
type EventConfig struct {
	NotificationClass uint32
	EventEnable       uint8  // EnableTo… bits
	NotifyType        uint32 // NotifyAlarm or NotifyEvent
	TimeDelay         uint32 // seconds a condition holds before it is reported
	// Analog.
	HighLimit, LowLimit, Deadband float32
	LimitEnable                   uint8 // LimitEnable… bits
	// Binary: the Present_Value that is off-normal (0 inactive, 1 active).
	AlarmValue uint32
	// Multi-state Input and Value: Alarm_Values, the Present_Values that
	// are off-normal (CHANGE_OF_STATE, clause 13.3.2), and Fault_Values,
	// the Present_Values that are a fault: Reliability is then
	// MULTI_STATE_FAULT (FAULT_STATE, clause 13.4.5). Each value is a state
	// 1..Number_Of_States.
	AlarmValues []uint32
	FaultValues []uint32
}

// EventTransition is an event state change, or an acknowledgment, that the
// server reports to the recipients of the Notification Class.
type EventTransition struct {
	Object            bacnet.ObjectIdentifier
	NotificationClass uint32
	EventType         uint32
	NotifyType        uint32
	From, To          uint32
	Transition        int // TransitionTo…
	TimeStamp         time.Time
	Message           string
	Params            *service.NotificationParameters // nil for an ack notification
}

// EventInfo is an object's event status, for GetEventInformation,
// GetAlarmSummary and GetEnrollmentSummary.
type EventInfo struct {
	Object            bacnet.ObjectIdentifier
	EventType         uint32
	EventState        uint32
	Acked             uint8 // transition bits that are acknowledged
	TimeStamps        [3]time.Time
	NotifyType        uint32
	EventEnable       uint8
	NotificationClass uint32
}

// EventReporter is implemented by objects with intrinsic reporting.
type EventReporter interface {
	// EvaluateEvents runs event detection at now and returns the
	// transitions to report. ackRequired tells whether a transition of a
	// Notification Class needs an acknowledgment.
	EvaluateEvents(now time.Time, ackRequired func(class uint32, transition int) bool) []EventTransition
	EventInfo() (EventInfo, bool)
	// AcknowledgeAlarm acknowledges the transition into state that
	// happened at stamp and returns the acknowledgment notification.
	AcknowledgeAlarm(state uint32, stamp service.TimeStamp) (EventTransition, error)
}

// intrinsic is the event detection state of a point. Detection follows
// bacnet-stack (ai.c, bi.c Intrinsic_Reporting): Reliability other than
// no-fault-detected is FAULT and leaving it returns to NORMAL; otherwise
// the out-of-range or change-of-state condition must hold for Time_Delay.
type intrinsic struct {
	cfg       EventConfig
	enabled   bool // Event_Detection_Enable
	acked     uint8
	stamps    [3]time.Time
	pending   uint32 // state the current condition leads to
	since     time.Time
	lastFault uint32
}

func transitionOf(state uint32) int {
	switch state {
	case EventStateNormal:
		return TransitionToNormal
	case EventStateFault:
		return TransitionToFault
	default:
		return TransitionToOffNormal
	}
}

// defineEvents installs the event properties of a point.
func (p *Point) defineEvents(cfg EventConfig) {
	in := &intrinsic{cfg: cfg, enabled: true, acked: 7, pending: EventStateNormal}
	p.intrinsic = in
	unsigned := func(id bacnet.PropertyIdentifier, get func() uint32, set func(uint32) error) {
		p.Define(id, &Property{Writable: true,
			Get: func() bacnet.ApplicationValue { return bacnet.UnsignedValue(uint64(get())) },
			Set: func(w Write) ([]bacnet.PropertyIdentifier, error) {
				if w.Property.ArrayIndex != nil {
					return nil, ErrNotAnArray
				}
				if w.Value.Kind != bacnet.ValueUnsigned {
					return nil, ErrInvalidDataType
				}
				if w.Value.Unsigned > math.MaxUint32 {
					return nil, ErrValueOutOfRange
				}
				if err := set(uint32(w.Value.Unsigned)); err != nil {
					return nil, err
				}
				return []bacnet.PropertyIdentifier{id}, nil
			}})
	}
	bits := func(id bacnet.PropertyIdentifier, n int, field *uint8) {
		p.Define(id, &Property{Writable: true,
			Get: func() bacnet.ApplicationValue { return maskBits(n, *field) },
			Set: func(w Write) ([]bacnet.PropertyIdentifier, error) {
				if w.Property.ArrayIndex != nil {
					return nil, ErrNotAnArray
				}
				if w.Value.Kind != bacnet.ValueBitString {
					return nil, ErrInvalidDataType
				}
				*field = bitsOf(w.Value.BitString, n)
				return []bacnet.PropertyIdentifier{id}, nil
			}})
	}
	real := func(id bacnet.PropertyIdentifier, field *float32, nonNegative bool) {
		p.Define(id, &Property{Writable: true,
			Get: func() bacnet.ApplicationValue { return bacnet.RealValue(*field) },
			Set: func(w Write) ([]bacnet.PropertyIdentifier, error) {
				if w.Property.ArrayIndex != nil {
					return nil, ErrNotAnArray
				}
				if w.Value.Kind != bacnet.ValueReal {
					return nil, ErrInvalidDataType
				}
				f := w.Value.Real
				if math.IsNaN(float64(f)) || math.IsInf(float64(f), 0) || (nonNegative && f < 0) {
					return nil, ErrValueOutOfRange
				}
				*field = f
				return []bacnet.PropertyIdentifier{id}, nil
			}})
	}
	unsigned(bacnet.PropertyNotificationClass, func() uint32 { return in.cfg.NotificationClass }, func(v uint32) error { in.cfg.NotificationClass = v; return nil })
	unsigned(bacnet.PropertyTimeDelay, func() uint32 { return in.cfg.TimeDelay }, func(v uint32) error { in.cfg.TimeDelay = v; return nil })
	bits(bacnet.PropertyEventEnable, 3, &in.cfg.EventEnable)
	p.Define(bacnet.PropertyNotifyType, &Property{Writable: true,
		Get: func() bacnet.ApplicationValue { return bacnet.EnumValue(in.cfg.NotifyType) },
		Set: func(w Write) ([]bacnet.PropertyIdentifier, error) {
			if w.Property.ArrayIndex != nil {
				return nil, ErrNotAnArray
			}
			if w.Value.Kind != bacnet.ValueEnumerated {
				return nil, ErrInvalidDataType
			}
			if w.Value.Enumerated > NotifyEvent {
				return nil, ErrValueOutOfRange
			}
			in.cfg.NotifyType = w.Value.Enumerated
			return []bacnet.PropertyIdentifier{bacnet.PropertyNotifyType}, nil
		}})
	p.Define(bacnet.PropertyEventDetectionEnable, &Property{Writable: true,
		Get: func() bacnet.ApplicationValue { return bacnet.BoolValue(in.enabled) },
		Set: func(w Write) ([]bacnet.PropertyIdentifier, error) {
			if w.Property.ArrayIndex != nil {
				return nil, ErrNotAnArray
			}
			if w.Value.Kind != bacnet.ValueBoolean {
				return nil, ErrInvalidDataType
			}
			in.enabled = w.Value.Boolean
			return []bacnet.PropertyIdentifier{bacnet.PropertyEventDetectionEnable}, nil
		}})
	p.Define(bacnet.PropertyAckedTransitions, &Property{Get: func() bacnet.ApplicationValue { return maskBits(3, in.acked) }})
	p.Define(bacnet.PropertyEventTimeStamps, &Property{Array: true, Get: func() bacnet.ApplicationValue {
		els := make([]bacnet.Element, 3)
		for i, t := range in.stamps {
			els[i] = timeStampElement(t)
		}
		return bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: els}
	}})
	switch p.kind {
	case kindAnalog:
		real(bacnet.PropertyHighLimit, &in.cfg.HighLimit, false)
		real(bacnet.PropertyLowLimit, &in.cfg.LowLimit, false)
		real(bacnet.PropertyDeadband, &in.cfg.Deadband, true)
		bits(bacnet.PropertyLimitEnable, 2, &in.cfg.LimitEnable)
	case kindBinary:
		p.Define(bacnet.PropertyAlarmValue, &Property{Writable: true,
			Get: func() bacnet.ApplicationValue { return bacnet.EnumValue(in.cfg.AlarmValue) },
			Set: func(w Write) ([]bacnet.PropertyIdentifier, error) {
				if w.Property.ArrayIndex != nil {
					return nil, ErrNotAnArray
				}
				if w.Value.Kind != bacnet.ValueEnumerated {
					return nil, ErrInvalidDataType
				}
				if w.Value.Enumerated > 1 {
					return nil, ErrValueOutOfRange
				}
				in.cfg.AlarmValue = w.Value.Enumerated
				return []bacnet.PropertyIdentifier{bacnet.PropertyAlarmValue}, nil
			}})
	case kindMultiState:
		states(p, bacnet.PropertyAlarmValues, &in.cfg.AlarmValues)
		states(p, bacnet.PropertyFaultValues, &in.cfg.FaultValues)
	}
}

// states defines a BACnetLIST of Unsigned property of multi-state values
// (Alarm_Values, Fault_Values). A write replaces the list; every value must
// be a state of the object.
func states(p *Point, id bacnet.PropertyIdentifier, field *[]uint32) {
	p.Define(id, &Property{Writable: true,
		Get: func() bacnet.ApplicationValue {
			els := make([]bacnet.Element, len(*field))
			for i, v := range *field {
				els[i] = bacnet.Element{Value: bacnet.UnsignedValue(uint64(v))}
			}
			return bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: els}
		},
		Set: func(w Write) ([]bacnet.PropertyIdentifier, error) {
			if w.Property.ArrayIndex != nil {
				return nil, ErrNotAnArray
			}
			vals := []bacnet.ApplicationValue{w.Value}
			if w.Value.Kind == bacnet.ValueConstructed {
				vals = vals[:0]
				for _, el := range w.Value.Elements {
					if el.Context {
						return nil, ErrInvalidDataType
					}
					vals = append(vals, el.Value)
				}
			}
			next := make([]uint32, 0, len(vals))
			for _, v := range vals {
				if v.Kind != bacnet.ValueUnsigned {
					return nil, ErrInvalidDataType
				}
				if v.Unsigned < 1 || v.Unsigned > uint64(p.states) {
					return nil, ErrValueOutOfRange
				}
				next = append(next, uint32(v.Unsigned))
			}
			*field = next
			return []bacnet.PropertyIdentifier{id, bacnet.PropertyReliability, bacnet.PropertyStatusFlags}, nil
		}})
}

func hasState(list []uint32, v uint64) bool {
	for _, s := range list {
		if uint64(s) == v {
			return true
		}
	}
	return false
}

// timeStampElement encodes a BACnetTimeStamp dateTime [2]; the zero time
// is the unspecified date and time.
func timeStampElement(t time.Time) bacnet.Element {
	d := bacnet.Date{Year: 0xFF, Month: 0xFF, Day: 0xFF, Weekday: 0xFF}
	tm := bacnet.Time{Hour: 0xFF, Minute: 0xFF, Second: 0xFF, Hundredths: 0xFF}
	if !t.IsZero() {
		d, tm = dateOf(t), timeOf(t)
	}
	return bacnet.Element{Context: true, TagNumber: 2, Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{
		{Value: bacnet.DateValue(d)}, {Value: bacnet.TimeValue(tm)},
	}}}
}

// TimeStampOf returns a time as a BACnetTimeStamp dateTime.
func TimeStampOf(t time.Time) service.TimeStamp {
	return service.TimeStamp{Choice: service.TimeStampDateTime, DateTime: service.DateTime{Date: dateOf(t), Time: timeOf(t)}}
}

func (p *Point) eventType(state uint32) uint32 {
	if state == EventStateFault {
		return EventTypeChangeOfReliability
	}
	if p.kind != kindAnalog {
		return EventTypeChangeOfState
	}
	return EventTypeOutOfRange
}

// condition is the state the out-of-range or change-of-state algorithm
// leads to from the current event state, without the time delay.
func (p *Point) condition(in *intrinsic) uint32 {
	cfg := in.cfg
	state := p.eventState
	pv := p.present()
	switch p.kind {
	case kindAnalog:
		v := pv.Real
		switch state {
		case EventStateNormal:
			if v > cfg.HighLimit && cfg.LimitEnable&LimitEnableHigh != 0 && cfg.EventEnable&EnableToOffNormal != 0 {
				return EventStateHighLimit
			}
			if v < cfg.LowLimit && cfg.LimitEnable&LimitEnableLow != 0 && cfg.EventEnable&EnableToOffNormal != 0 {
				return EventStateLowLimit
			}
		case EventStateHighLimit:
			if cfg.LimitEnable&LimitEnableHigh == 0 || (v < cfg.HighLimit-cfg.Deadband && cfg.EventEnable&EnableToNormal != 0) {
				return EventStateNormal
			}
		case EventStateLowLimit:
			if cfg.LimitEnable&LimitEnableLow == 0 || (v > cfg.LowLimit+cfg.Deadband && cfg.EventEnable&EnableToNormal != 0) {
				return EventStateNormal
			}
		}
	case kindBinary:
		switch state {
		case EventStateNormal:
			if pv.Enumerated == cfg.AlarmValue && cfg.EventEnable&EnableToOffNormal != 0 {
				return EventStateOffnormal
			}
		case EventStateOffnormal:
			if pv.Enumerated != cfg.AlarmValue && cfg.EventEnable&EnableToNormal != 0 {
				return EventStateNormal
			}
		}
	case kindMultiState:
		// CHANGE_OF_STATE (clause 13.3.2) on Alarm_Values, enabled
		// transitions only, as for binary points. The optional re-alarm on
		// a different alarm value while OFFNORMAL is not done.
		alarm := hasState(cfg.AlarmValues, pv.Unsigned)
		switch state {
		case EventStateNormal:
			if alarm && cfg.EventEnable&EnableToOffNormal != 0 {
				return EventStateOffnormal
			}
		case EventStateOffnormal:
			if !alarm && cfg.EventEnable&EnableToNormal != 0 {
				return EventStateNormal
			}
		}
	}
	return state
}

// EvaluateEvents implements EventReporter.
func (p *Point) EvaluateEvents(now time.Time, ackRequired func(uint32, int) bool) []EventTransition {
	if p.intrinsic == nil {
		return nil
	}
	// Time stamps carry hundredths of a second in local time, as on the
	// wire, so that an acknowledgment can name them exactly.
	now = now.Local().Truncate(10 * time.Millisecond)
	var out []EventTransition
	p.Update(func() []bacnet.PropertyIdentifier {
		in := p.intrinsic
		if !in.enabled {
			return nil
		}
		from := p.eventState
		to := from
		// Leaving a limit state names the limit that was exceeded.
		limit := func(state uint32) float32 {
			switch state {
			case EventStateHighLimit:
				return in.cfg.HighLimit
			case EventStateLowLimit:
				return in.cfg.LowLimit
			}
			return 0
		}
		rel := p.currentReliability()
		switch {
		case rel != ReliabilityNoFaultDetected:
			to = EventStateFault
			in.pending = EventStateFault
		case from == EventStateFault:
			to = EventStateNormal
			in.pending = EventStateNormal
		default:
			next := p.condition(in)
			if next == from {
				in.pending = from
				break
			}
			if next != in.pending {
				in.pending, in.since = next, now
			}
			if now.Sub(in.since) >= time.Duration(in.cfg.TimeDelay)*time.Second {
				to = next
			}
		}
		if to == from && (to != EventStateFault || rel == in.lastFault) {
			return nil
		}
		exceeded := limit(to)
		if to == EventStateNormal {
			exceeded = limit(from)
		}
		p.eventState = to
		tr := transitionOf(to)
		in.stamps[tr] = now
		if to == EventStateFault {
			in.lastFault = rel
		}
		ev := EventTransition{
			Object: p.id, NotificationClass: in.cfg.NotificationClass, EventType: p.eventType(to),
			NotifyType: in.cfg.NotifyType, From: from, To: to, Transition: tr, TimeStamp: now,
		}
		if to == EventStateNormal && from == EventStateFault {
			ev.EventType = EventTypeChangeOfReliability
		}
		ev.Params = p.eventParams(ev.EventType, exceeded)
		if ackRequired != nil && ackRequired(in.cfg.NotificationClass, tr) {
			in.acked &^= 1 << tr
		}
		out = append(out, ev)
		return []bacnet.PropertyIdentifier{bacnet.PropertyEventState, bacnet.PropertyStatusFlags, bacnet.PropertyEventTimeStamps, bacnet.PropertyAckedTransitions}
	})
	return out
}

// eventParams builds the notification parameters, as bacnet-stack does.
func (p *Point) eventParams(eventType uint32, exceeded float32) *service.NotificationParameters {
	flags := p.statusFlags().BitString
	pv := p.present()
	switch eventType {
	case EventTypeOutOfRange:
		return &service.NotificationParameters{Choice: service.NotificationOutOfRange, OutOfRange: &service.OutOfRangeParams{
			ExceedingValue: pv.Real, StatusFlags: flags, Deadband: p.intrinsic.cfg.Deadband, ExceededLimit: exceeded,
		}}
	case EventTypeChangeOfState:
		// BACnetPropertyStates binary-value [1], or unsigned-value [11]
		// for a multi-state Present_Value (clause 21).
		choice := uint8(1)
		if p.kind == kindMultiState {
			choice = 11
		}
		return &service.NotificationParameters{Choice: service.NotificationChangeOfState, ChangeOfState: &service.ChangeOfStateParams{
			NewState: service.PropertyStates{Choice: choice, Value: pv.Clone()}, StatusFlags: flags,
		}}
	default:
		// change-of-reliability [19]: reliability [0], status-flags [1],
		// property-values [2] with Present_Value (event.c, ai.c).
		rel, _ := bacnet.AppendContextUnsigned(nil, 0, uint64(p.currentReliability()))
		relEl, _, _ := bacnet.ParseTag(rel, bacnet.DefaultDecodeLimits())
		sf, _ := bacnet.AppendContextBitString(nil, 1, flags)
		sfEl, _, _ := bacnet.ParseTag(sf, bacnet.DefaultDecodeLimits())
		pid, _ := bacnet.AppendContextUnsigned(nil, 0, uint64(bacnet.PropertyPresentValue))
		pidEl, _, _ := bacnet.ParseTag(pid, bacnet.DefaultDecodeLimits())
		values := bacnet.Element{Context: true, TagNumber: 2, Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{
			pidEl,
			{Context: true, TagNumber: 2, Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{{Value: pv.Clone()}}}},
		}}}
		return &service.NotificationParameters{Choice: service.NotificationParametersChoice(EventTypeChangeOfReliability), RawElements: []bacnet.Element{relEl, sfEl, values}}
	}
}

// EventInfo implements EventReporter.
func (p *Point) EventInfo() (EventInfo, bool) {
	if p.intrinsic == nil {
		return EventInfo{}, false
	}
	var info EventInfo
	p.View(func() {
		in := p.intrinsic
		info = EventInfo{
			Object: p.id, EventType: p.eventType(EventStateNormal), EventState: p.eventState, Acked: in.acked,
			TimeStamps: in.stamps, NotifyType: in.cfg.NotifyType, EventEnable: in.cfg.EventEnable,
			NotificationClass: in.cfg.NotificationClass,
		}
	})
	return info, true
}

// AcknowledgeAlarm implements EventReporter with bacnet-stack's checks
// (ai.c Analog_Input_Alarm_Ack): an unacknowledged transition needs a
// dateTime time stamp not older than the transition; an acknowledged one
// is accepted again only for the current event state.
func (p *Point) AcknowledgeAlarm(state uint32, stamp service.TimeStamp) (EventTransition, error) {
	if p.intrinsic == nil {
		return EventTransition{}, ErrNoAlarmConfigured
	}
	if state > EventStateLowLimit {
		return EventTransition{}, ErrInvalidEventState
	}
	var ev EventTransition
	var err error
	p.Update(func() []bacnet.PropertyIdentifier {
		in := p.intrinsic
		tr := transitionOf(state)
		if in.acked&(1<<tr) == 0 {
			if stamp.Choice != service.TimeStampDateTime {
				err = ErrInvalidTimeStamp
				return nil
			}
			if in.stamps[tr].After(dateTimeToTime(stamp.DateTime)) {
				err = ErrInvalidTimeStamp
				return nil
			}
			in.acked |= 1 << tr
		} else if state != p.eventState {
			err = ErrInvalidEventState
			return nil
		}
		ev = EventTransition{
			Object: p.id, NotificationClass: in.cfg.NotificationClass, EventType: p.eventType(state),
			NotifyType: NotifyAckNotification, From: p.eventState, To: state, Transition: tr, TimeStamp: in.stamps[tr],
		}
		return []bacnet.PropertyIdentifier{bacnet.PropertyAckedTransitions}
	})
	return ev, err
}
