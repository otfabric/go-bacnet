// SPDX-License-Identifier: MIT

package object

import (
	"context"
	"time"

	"github.com/otfabric/go-bacnet"
)

// DeviceObjectPropertyReference is a BACnetDeviceObjectPropertyReference
// (clause 21): a property of an object, optionally an array element, and
// optionally the device that holds it.
type DeviceObjectPropertyReference struct {
	Object     bacnet.ObjectIdentifier
	Property   bacnet.PropertyIdentifier
	ArrayIndex *uint32
	Device     *bacnet.ObjectIdentifier
}

// ScheduleConfig configures a Schedule object.
type ScheduleConfig struct {
	Instance    uint32
	Name        string
	Description string
	// EffectivePeriod limits the dates the schedule is in effect; nil means
	// always (both dates fully unspecified).
	EffectivePeriod *bacnet.DateRange
	// Weekly is Weekly_Schedule, Monday first.
	Weekly bacnet.WeeklySchedule
	// Exceptions is Exception_Schedule. A special event either has a
	// calendar entry or names a Calendar object (PeriodObject).
	Exceptions []bacnet.SpecialEvent
	// Default is Schedule_Default; its kind is the datatype of the schedule
	// (every time value is of that kind or NULL). A NULL default leaves the
	// datatype to the first non-NULL value of Weekly or Exceptions.
	Default bacnet.ApplicationValue
	// References is List_Of_Object_Property_References: the properties the
	// schedule writes Present_Value to.
	References []DeviceObjectPropertyReference
	// PriorityForWriting is 1..16 (default 16).
	PriorityForWriting uint8
	// MaxExceptions bounds Exception_Schedule (default 32) and MaxReferences
	// List_Of_Object_Property_References (default 32).
	MaxExceptions int
	MaxReferences int
}

// Schedule is a Schedule object (clause 12.24). Present_Value is evaluated
// on every Poll against the server's clock: the highest-priority special
// event of Exception_Schedule that is in effect and has a non-NULL value at
// the current time, otherwise the day's Weekly_Schedule entry, otherwise
// Schedule_Default; outside Effective_Period it is Schedule_Default. When
// Present_Value changes it is written, at Priority_For_Writing, to every
// referenced property of an object of the same device.
type Schedule struct {
	*Base
	effective  bacnet.DateRange
	weekly     bacnet.WeeklySchedule
	exceptions []bacnet.SpecialEvent
	def        bacnet.ApplicationValue
	kind       bacnet.ValueKind // datatype; ValueNull while undetermined
	refs       []DeviceObjectPropertyReference
	priority   uint8
	oos        bool
	pv         bacnet.ApplicationValue
	pending    bool // Present_Value is still to be written to the references
	maxExc     int
	maxRefs    int
}

var unspecifiedDate = bacnet.Date{Year: dateAny, Month: dateAny, Day: dateAny, Weekday: dateAny}

// NewSchedule returns a Schedule object.
func NewSchedule(cfg ScheduleConfig) (*Schedule, error) {
	if cfg.Instance >= bacnet.MaxObjectInstance {
		return nil, errorf("instance %d out of range", cfg.Instance)
	}
	if cfg.Name == "" {
		return nil, errorf("object name required")
	}
	if cfg.PriorityForWriting == 0 {
		cfg.PriorityForWriting = defaultPriority
	}
	if cfg.PriorityForWriting > 16 {
		return nil, errorf("priority for writing %d out of range 1..16", cfg.PriorityForWriting)
	}
	if cfg.MaxExceptions <= 0 {
		cfg.MaxExceptions = 32
	}
	if cfg.MaxReferences <= 0 {
		cfg.MaxReferences = 32
	}
	if len(cfg.Exceptions) > cfg.MaxExceptions {
		return nil, errorf("%d special events, at most %d", len(cfg.Exceptions), cfg.MaxExceptions)
	}
	if len(cfg.References) > cfg.MaxReferences {
		return nil, errorf("%d references, at most %d", len(cfg.References), cfg.MaxReferences)
	}
	id := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeSchedule, Instance: cfg.Instance}
	s := &Schedule{Base: NewBase(id, cfg.Name), effective: bacnet.DateRange{Start: unspecifiedDate, End: unspecifiedDate},
		priority: cfg.PriorityForWriting, maxExc: cfg.MaxExceptions, maxRefs: cfg.MaxReferences, pending: true}
	if cfg.EffectivePeriod != nil {
		s.effective = *cfg.EffectivePeriod
	}
	if !primitive(cfg.Default) {
		return nil, errorf("schedule default must be a primitive value")
	}
	s.def, s.kind = cfg.Default.Clone(), cfg.Default.Kind
	for i := range cfg.Weekly {
		if err := s.checkTimeValues(cfg.Weekly[i].DaySchedule); err != nil {
			return nil, errorf("weekly schedule day %d: %v", i+1, err)
		}
		s.weekly[i] = cloneDay(cfg.Weekly[i])
	}
	for i, e := range cfg.Exceptions {
		if err := s.checkEvent(e); err != nil {
			return nil, errorf("special event %d: %v", i+1, err)
		}
		s.exceptions = append(s.exceptions, cloneEvent(e))
	}
	s.refs = append(s.refs, cfg.References...)
	s.pv = s.def
	if cfg.Description != "" {
		s.Define(bacnet.PropertyDescription, &Property{Value: charString(cfg.Description), Writable: true, Kinds: []bacnet.ValueKind{bacnet.ValueCharacterString}})
	}
	s.Define(bacnet.PropertyPresentValue, &Property{Required: true, Writable: true, Get: func() bacnet.ApplicationValue { return s.pv }, Set: s.writePresentValue})
	s.Define(bacnet.PropertyEffectivePeriod, &Property{Required: true, Writable: true,
		Get: func() bacnet.ApplicationValue { return bacnet.EncodeDateRange(s.effective) }, Set: s.writeEffectivePeriod})
	s.Define(bacnet.PropertyWeeklySchedule, &Property{Array: true, Writable: true,
		Get: func() bacnet.ApplicationValue { return bacnet.EncodeWeeklySchedule(s.weekly) }, Set: s.writeWeekly})
	s.Define(bacnet.PropertyExceptionSchedule, &Property{Array: true, Writable: true, Get: s.exceptionSchedule, Set: s.writeExceptions})
	s.Define(bacnet.PropertyScheduleDefault, &Property{Required: true, Get: func() bacnet.ApplicationValue { return s.def }})
	s.Define(bacnet.PropertyListOfObjectPropertyReferences, &Property{Required: true, Writable: true, Get: s.references, Set: s.writeReferences})
	s.Define(bacnet.PropertyPriorityForWriting, &Property{Required: true, Get: func() bacnet.ApplicationValue { return bacnet.UnsignedValue(uint64(s.priority)) }})
	s.Define(bacnet.PropertyStatusFlags, &Property{Required: true, Get: func() bacnet.ApplicationValue { return statusFlags(false, false, false, s.oos) }})
	s.Define(bacnet.PropertyReliability, &Property{Required: true, Value: bacnet.EnumValue(ReliabilityNoFaultDetected)})
	s.Define(bacnet.PropertyOutOfService, &Property{Required: true, Writable: true,
		Get: func() bacnet.ApplicationValue { return bacnet.BoolValue(s.oos) }, Set: s.writeOutOfService})
	return s, nil
}

// primitive reports whether v is an application primitive (BACnetTimeValue
// and Schedule_Default carry primitive values only).
func primitive(v bacnet.ApplicationValue) bool {
	return v.Kind != bacnet.ValueConstructed && v.Kind != bacnet.ValueContext
}

// checkValue checks a scheduled value against the datatype of the
// schedule; the first non-NULL value fixes an undetermined datatype.
func (s *Schedule) checkValue(v bacnet.ApplicationValue) error {
	if !primitive(v) {
		return ErrInvalidDataType
	}
	if v.Kind == bacnet.ValueNull {
		return nil
	}
	if s.kind == bacnet.ValueNull {
		s.kind = v.Kind
		return nil
	}
	if v.Kind != s.kind {
		return ErrInvalidDataType
	}
	return nil
}

func (s *Schedule) checkTimeValues(tvs []bacnet.ScheduledTimeValue) error {
	for _, tv := range tvs {
		if err := s.checkValue(tv.Value); err != nil {
			return err
		}
	}
	return nil
}

func (s *Schedule) checkEvent(e bacnet.SpecialEvent) error {
	if e.EventPriority < 1 || e.EventPriority > 16 {
		return ErrValueOutOfRange
	}
	if e.PeriodObject == nil {
		if _, err := calendarEntryElement(e.CalendarEntry); err != nil {
			return ErrInvalidDataType
		}
	}
	return s.checkTimeValues(e.ListOfTimeValues)
}

func cloneDay(d bacnet.DailySchedule) bacnet.DailySchedule {
	out := bacnet.DailySchedule{DaySchedule: make([]bacnet.ScheduledTimeValue, len(d.DaySchedule))}
	for i, tv := range d.DaySchedule {
		out.DaySchedule[i] = bacnet.ScheduledTimeValue{Time: tv.Time, Value: tv.Value.Clone()}
	}
	return out
}

func cloneEvent(e bacnet.SpecialEvent) bacnet.SpecialEvent {
	out := e
	if e.PeriodObject != nil {
		id := *e.PeriodObject
		out.PeriodObject = &id
	}
	out.ListOfTimeValues = cloneDay(bacnet.DailySchedule{DaySchedule: e.ListOfTimeValues}).DaySchedule
	return out
}

func (s *Schedule) writePresentValue(w Write) ([]bacnet.PropertyIdentifier, error) {
	if w.Property.ArrayIndex != nil {
		return nil, ErrNotAnArray
	}
	// Present_Value is writable while Out_Of_Service is TRUE, and the
	// written value goes to the referenced properties as an evaluated one
	// would (clause 12.24, Present_Value and Out_Of_Service).
	if !s.oos {
		return nil, ErrWriteAccessDenied
	}
	if err := s.checkValue(w.Value); err != nil {
		return nil, err
	}
	if sameValue(s.pv, w.Value) {
		return nil, nil
	}
	s.pv = w.Value.Clone()
	s.pending = true
	return []bacnet.PropertyIdentifier{bacnet.PropertyPresentValue}, nil
}

func (s *Schedule) writeOutOfService(w Write) ([]bacnet.PropertyIdentifier, error) {
	if w.Property.ArrayIndex != nil {
		return nil, ErrNotAnArray
	}
	if w.Value.Kind != bacnet.ValueBoolean {
		return nil, ErrInvalidDataType
	}
	if s.oos == w.Value.Boolean {
		return nil, nil
	}
	s.oos = w.Value.Boolean
	return []bacnet.PropertyIdentifier{bacnet.PropertyOutOfService, bacnet.PropertyStatusFlags}, nil
}

func (s *Schedule) writeEffectivePeriod(w Write) ([]bacnet.PropertyIdentifier, error) {
	if w.Property.ArrayIndex != nil {
		return nil, ErrNotAnArray
	}
	if w.Value.Kind != bacnet.ValueConstructed {
		return nil, ErrInvalidDataType
	}
	dr, err := bacnet.DecodeDateRange(w.Value)
	if err != nil {
		return nil, ErrInvalidDataType
	}
	s.effective = dr
	return []bacnet.PropertyIdentifier{bacnet.PropertyEffectivePeriod}, nil
}

// arrayIndex checks the index of an array write of n elements. As
// bacnet-stack's schedule.c, the arrays are not resizable through index 0.
func arrayIndex(idx uint32, n int) error {
	if idx == 0 {
		return ErrWriteAccessDenied
	}
	if idx > uint32(n) {
		return ErrInvalidArrayIndex
	}
	return nil
}

func (s *Schedule) writeWeekly(w Write) ([]bacnet.PropertyIdentifier, error) {
	if w.Value.Kind != bacnet.ValueConstructed {
		return nil, ErrInvalidDataType
	}
	kind := s.kind
	next := s.weekly
	if w.Property.ArrayIndex != nil {
		idx := *w.Property.ArrayIndex
		if err := arrayIndex(idx, len(next)); err != nil {
			return nil, err
		}
		day, err := bacnet.DecodeDailySchedule(w.Value)
		if err != nil {
			return nil, ErrInvalidDataType
		}
		next[idx-1] = day
	} else {
		week, err := bacnet.DecodeWeeklySchedule(w.Value)
		if err != nil {
			return nil, ErrInvalidDataType
		}
		next = week
	}
	for i := range next {
		if err := s.checkTimeValues(next[i].DaySchedule); err != nil {
			s.kind = kind
			return nil, err
		}
		next[i] = cloneDay(next[i])
	}
	s.weekly = next
	return []bacnet.PropertyIdentifier{bacnet.PropertyWeeklySchedule}, nil
}

// specialEventElement encodes one BACnetSpecialEvent as an array element.
func specialEventElement(e bacnet.SpecialEvent) bacnet.Element {
	var period bacnet.Element
	if e.PeriodObject != nil {
		period = bacnet.Element{Context: true, TagNumber: 1, Value: bacnet.ObjectIDValue(*e.PeriodObject)}
	} else {
		ce, _ := calendarEntryElement(e.CalendarEntry) // validated when stored
		period = bacnet.Element{Context: true, TagNumber: 0, Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{ce}}}
	}
	day := bacnet.EncodeDailySchedule(bacnet.DailySchedule{DaySchedule: e.ListOfTimeValues})
	tvs := day.Elements[0]
	tvs.TagNumber = 2
	return bacnet.Element{Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{
		period, tvs, {Context: true, TagNumber: 3, Value: bacnet.UnsignedValue(uint64(e.EventPriority))},
	}}}
}

func (s *Schedule) exceptionSchedule() bacnet.ApplicationValue {
	els := make([]bacnet.Element, len(s.exceptions))
	for i, e := range s.exceptions {
		els[i] = specialEventElement(e)
	}
	return bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: els}
}

// parseSpecialEvents decodes BACnetSpecialEvent values from wire elements
// (period, list-of-time-values [2], event-priority [3], repeated) or from
// elements that group one event each.
func parseSpecialEvents(els []bacnet.Element) ([]bacnet.SpecialEvent, error) {
	var out []bacnet.SpecialEvent
	for i := 0; i < len(els); {
		if !els[i].Context && els[i].Value.Kind == bacnet.ValueConstructed {
			e, err := parseSpecialEvent(els[i].Value.Elements)
			if err != nil {
				return nil, err
			}
			out = append(out, e)
			i++
			continue
		}
		if i+3 > len(els) {
			return nil, ErrInvalidDataType
		}
		e, err := parseSpecialEvent(els[i : i+3])
		if err != nil {
			return nil, err
		}
		out = append(out, e)
		i += 3
	}
	return out, nil
}

func parseSpecialEvent(els []bacnet.Element) (bacnet.SpecialEvent, error) {
	var e bacnet.SpecialEvent
	if len(els) != 3 {
		return e, ErrInvalidDataType
	}
	period, tvs, prio := els[0], els[1], els[2]
	switch {
	case period.Context && period.TagNumber == 0 && bacnet.IsContextConstructed(period) && len(period.Value.Elements) == 1:
		ce, err := parseCalendarEntry(period.Value.Elements[0])
		if err != nil {
			return e, err
		}
		e.CalendarEntry = ce
	case period.Context && period.TagNumber == 1 && !bacnet.IsContextConstructed(period):
		id, ok := ctxObjectID(period)
		if !ok {
			return e, ErrInvalidDataType
		}
		e.PeriodObject = &id
	default:
		return e, ErrInvalidDataType
	}
	if tvs.TagNumber != 2 || !bacnet.IsContextConstructed(tvs) {
		return e, ErrInvalidDataType
	}
	tvs.TagNumber = 0
	day, err := bacnet.DecodeDailySchedule(bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{tvs}})
	if err != nil {
		return e, ErrInvalidDataType
	}
	e.ListOfTimeValues = day.DaySchedule
	if prio.TagNumber != 3 {
		return e, ErrInvalidDataType
	}
	p, ok := ctxUnsigned(prio)
	if !ok {
		return e, ErrInvalidDataType
	}
	if p < 1 || p > 16 {
		return e, ErrValueOutOfRange
	}
	e.EventPriority = uint8(p)
	return e, nil
}

func (s *Schedule) writeExceptions(w Write) ([]bacnet.PropertyIdentifier, error) {
	if w.Value.Kind != bacnet.ValueConstructed {
		return nil, ErrInvalidDataType
	}
	events, err := parseSpecialEvents(w.Value.Elements)
	if err != nil {
		return nil, err
	}
	next := append([]bacnet.SpecialEvent(nil), s.exceptions...)
	if w.Property.ArrayIndex != nil {
		idx := *w.Property.ArrayIndex
		if err := arrayIndex(idx, len(next)); err != nil {
			return nil, err
		}
		if len(events) != 1 {
			return nil, ErrInvalidDataType
		}
		next[idx-1] = events[0]
	} else {
		if len(events) > s.maxExc {
			return nil, ErrNoSpaceToWriteProperty
		}
		next = events
	}
	kind := s.kind
	for i, e := range next {
		if err := s.checkEvent(e); err != nil {
			s.kind = kind
			return nil, err
		}
		next[i] = cloneEvent(e)
	}
	s.exceptions = next
	return []bacnet.PropertyIdentifier{bacnet.PropertyExceptionSchedule}, nil
}

// referenceElement encodes a BACnetDeviceObjectPropertyReference as one
// element grouping its context fields.
func referenceElement(r DeviceObjectPropertyReference) bacnet.Element {
	els := []bacnet.Element{
		{Context: true, TagNumber: 0, Value: bacnet.ObjectIDValue(r.Object)},
		{Context: true, TagNumber: 1, Value: bacnet.EnumValue(uint32(r.Property))},
	}
	if r.ArrayIndex != nil {
		els = append(els, bacnet.Element{Context: true, TagNumber: 2, Value: bacnet.UnsignedValue(uint64(*r.ArrayIndex))})
	}
	if r.Device != nil {
		els = append(els, bacnet.Element{Context: true, TagNumber: 3, Value: bacnet.ObjectIDValue(*r.Device)})
	}
	return bacnet.Element{Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: els}}
}

func (s *Schedule) references() bacnet.ApplicationValue {
	els := make([]bacnet.Element, len(s.refs))
	for i, r := range s.refs {
		els[i] = referenceElement(r)
	}
	return bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: els}
}

// parseReferences decodes BACnetDeviceObjectPropertyReference values from
// wire elements or from elements that group one reference each.
func parseReferences(els []bacnet.Element) ([]DeviceObjectPropertyReference, error) {
	var out []DeviceObjectPropertyReference
	for i := 0; i < len(els); {
		if !els[i].Context && els[i].Value.Kind == bacnet.ValueConstructed {
			r, n, err := parseReference(els[i].Value.Elements)
			if err != nil || n != len(els[i].Value.Elements) {
				return nil, ErrInvalidDataType
			}
			out = append(out, r)
			i++
			continue
		}
		r, n, err := parseReference(els[i:])
		if err != nil {
			return nil, err
		}
		out = append(out, r)
		i += n
	}
	return out, nil
}

func parseReference(els []bacnet.Element) (DeviceObjectPropertyReference, int, error) {
	var r DeviceObjectPropertyReference
	if len(els) < 2 || els[0].TagNumber != 0 || els[1].TagNumber != 1 {
		return r, 0, ErrInvalidDataType
	}
	obj, ok := ctxObjectID(els[0])
	if !ok {
		return r, 0, ErrInvalidDataType
	}
	r.Object = obj
	var prop uint64
	if els[1].Context && els[1].Value.Kind == bacnet.ValueEnumerated {
		prop, ok = uint64(els[1].Value.Enumerated), true
	} else {
		prop, ok = ctxUnsigned(els[1])
	}
	if !ok || prop > 0x3FFFFF {
		return r, 0, ErrInvalidDataType
	}
	r.Property = bacnet.PropertyIdentifier(prop)
	n := 2
	if n < len(els) && els[n].Context && els[n].TagNumber == 2 {
		idx, ok := ctxUnsigned(els[n])
		if !ok || idx > 0xFFFFFFFF {
			return r, 0, ErrInvalidDataType
		}
		i := uint32(idx)
		r.ArrayIndex = &i
		n++
	}
	if n < len(els) && els[n].Context && els[n].TagNumber == 3 {
		dev, ok := ctxObjectID(els[n])
		if !ok || dev.Type != bacnet.ObjectTypeDevice {
			return r, 0, ErrInvalidDataType
		}
		r.Device = &dev
		n++
	}
	return r, n, nil
}

func (s *Schedule) writeReferences(w Write) ([]bacnet.PropertyIdentifier, error) {
	if w.Property.ArrayIndex != nil {
		return nil, ErrNotAnArray
	}
	refs, err := parseReferences(listElements(w.Value))
	if err != nil {
		return nil, err
	}
	if len(refs) > s.maxRefs {
		return nil, ErrNoSpaceToWriteProperty
	}
	s.refs = refs
	return []bacnet.PropertyIdentifier{bacnet.PropertyListOfObjectPropertyReferences}, nil
}

// PresentValue returns Present_Value.
func (s *Schedule) PresentValue() bacnet.ApplicationValue {
	var v bacnet.ApplicationValue
	s.View(func() { v = s.pv.Clone() })
	return v
}

// References returns List_Of_Object_Property_References.
func (s *Schedule) References() []DeviceObjectPropertyReference {
	var out []DeviceObjectPropertyReference
	s.View(func() { out = append(out, s.refs...) })
	return out
}

// compareTime orders two times; a field unspecified (X'FF') in either is
// skipped, as bacnet-stack's datetime_wildcard_compare_time (datetime.c).
func compareTime(a, b bacnet.Time) int {
	pairs := [4][2]uint8{{a.Hour, b.Hour}, {a.Minute, b.Minute}, {a.Second, b.Second}, {a.Hundredths, b.Hundredths}}
	for _, p := range pairs {
		if p[0] != dateAny && p[1] != dateAny && p[0] != p[1] {
			return int(p[0]) - int(p[1])
		}
	}
	return 0
}

// valueAt returns the value in effect at t: the time value with the latest
// time not after t. ok is false before the first time value of the day, as
// bacnet-stack's Schedule_Recalculate_PV (schedule.c) evaluates a day.
func valueAt(tvs []bacnet.ScheduledTimeValue, t bacnet.Time) (bacnet.ApplicationValue, bool) {
	best := -1
	for i, tv := range tvs {
		if compareTime(t, tv.Time) < 0 {
			continue
		}
		if best < 0 || compareTime(tv.Time, tvs[best].Time) >= 0 {
			best = i
		}
	}
	if best < 0 {
		return bacnet.ApplicationValue{}, false
	}
	return tvs[best].Value, true
}

// periodActive reports whether a special event applies to the date d.
func periodActive(ctx context.Context, e bacnet.SpecialEvent, d bacnet.Date, dev *Device) bool {
	if e.PeriodObject == nil {
		return entryMatches(e.CalendarEntry, d)
	}
	if dev == nil {
		return false
	}
	o, ok := dev.Object(*e.PeriodObject)
	if !ok {
		return false
	}
	if c, ok := o.(*Calendar); ok {
		return c.Contains(d)
	}
	v, err := o.ReadProperty(ctx, bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue})
	return err == nil && v.Kind == bacnet.ValueBoolean && v.Boolean
}

// evaluate computes Present_Value at now (clause 12.24): Schedule_Default
// outside Effective_Period; otherwise the non-NULL value of the special
// event in effect with the numerically lowest Event_Priority (the first
// in Exception_Schedule on a tie), then the day's Weekly_Schedule value,
// and Schedule_Default when neither gives one. A NULL time value
// relinquishes: the next lower layer applies.
func evaluate(ctx context.Context, now time.Time, dev *Device, eff bacnet.DateRange, weekly *bacnet.WeeklySchedule,
	exceptions []bacnet.SpecialEvent, def bacnet.ApplicationValue) bacnet.ApplicationValue {
	d, t := dateOf(now), timeOf(now)
	if !dateInRange(d, eff) {
		return def
	}
	var best *bacnet.ApplicationValue
	bestPrio := uint8(17)
	for _, e := range exceptions {
		if e.EventPriority >= bestPrio || !periodActive(ctx, e, d, dev) {
			continue
		}
		if v, ok := valueAt(e.ListOfTimeValues, t); ok && v.Kind != bacnet.ValueNull {
			best, bestPrio = &v, e.EventPriority
		}
	}
	if best != nil {
		return *best
	}
	if v, ok := valueAt(weekly[d.Weekday-1].DaySchedule, t); ok && v.Kind != bacnet.ValueNull {
		return v
	}
	return def
}

// Poll implements Poller: Present_Value is evaluated against the server's
// clock unless Out_Of_Service is TRUE, and a changed (or written) value is
// written to the references.
func (s *Schedule) Poll(ctx context.Context, now time.Time, dev *Device) {
	var (
		oos        bool
		eff        bacnet.DateRange
		weekly     bacnet.WeeklySchedule
		exceptions []bacnet.SpecialEvent
		def        bacnet.ApplicationValue
	)
	s.View(func() {
		oos, eff, weekly, def = s.oos, s.effective, s.weekly, s.def
		exceptions = append(exceptions, s.exceptions...)
	})
	if !oos {
		v := evaluate(ctx, now, dev, eff, &weekly, exceptions, def)
		s.Update(func() []bacnet.PropertyIdentifier {
			if s.oos || sameValue(s.pv, v) {
				return nil
			}
			s.pv = v.Clone()
			s.pending = true
			return []bacnet.PropertyIdentifier{bacnet.PropertyPresentValue}
		})
	}
	if dev == nil {
		return
	}
	s.mu.Lock()
	pending, pv, prio := s.pending, s.pv.Clone(), s.priority
	refs := append([]DeviceObjectPropertyReference(nil), s.refs...)
	s.pending = false
	s.mu.Unlock()
	if !pending {
		return
	}
	self := dev.Identifier()
	for _, r := range refs {
		if r.Device != nil && *r.Device != self {
			continue // another device: the server does not initiate WriteProperty
		}
		o, ok := dev.Object(r.Object)
		if !ok || o.Identifier() == s.Identifier() {
			continue
		}
		// A refused write leaves the property as it was; the schedule
		// writes again when Present_Value next changes.
		_ = o.WriteProperty(ctx, Write{Property: bacnet.PropertyReference{Identifier: r.Property, ArrayIndex: r.ArrayIndex}, Value: pv, Priority: prio})
	}
}
