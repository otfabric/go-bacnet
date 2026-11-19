// SPDX-License-Identifier: MIT

package bacnet

import "fmt"

// EncodeExceptionSchedule encodes a BACnetExceptionSchedule (SEQUENCE OF
// BACnetSpecialEvent, clause 12.24.8) as a constructed value holding one
// constructed element per special event: its period (calendarEntry [0] or
// calendarReference [1]), listOfTimeValues [2] and eventPriority [3]. An
// event priority outside 1..16 (clause 12.24.8) is refused with ErrMalformed.
func EncodeExceptionSchedule(s ExceptionSchedule) (ApplicationValue, error) {
	els := make([]Element, len(s.SpecialEvents))
	for i, e := range s.SpecialEvents {
		el, err := encodeSpecialEvent(e)
		if err != nil {
			return ApplicationValue{}, fmt.Errorf("special event %d: %w", i, err)
		}
		els[i] = el
	}
	return ApplicationValue{Kind: ValueConstructed, Elements: els}, nil
}

func encodeSpecialEvent(e SpecialEvent) (Element, error) {
	if e.EventPriority < 1 || e.EventPriority > 16 {
		return Element{}, fmt.Errorf("%w: SpecialEvent eventPriority %d, not 1..16", ErrMalformed, e.EventPriority)
	}
	var period Element
	if e.PeriodObject != nil {
		period = Element{Context: true, TagNumber: 1, Value: ObjectIDValue(*e.PeriodObject)}
	} else {
		ce, err := EncodeCalendarEntry(e.CalendarEntry)
		if err != nil {
			return Element{}, err
		}
		period = Element{Context: true, TagNumber: 0, Value: ApplicationValue{Kind: ValueConstructed, Elements: ce.Elements}}
	}
	tvs := EncodeDailySchedule(DailySchedule{DaySchedule: e.ListOfTimeValues}).Elements[0]
	tvs.TagNumber = 2
	return Element{Value: ApplicationValue{Kind: ValueConstructed, Elements: []Element{
		period, tvs, {Context: true, TagNumber: 3, Value: UnsignedValue(uint64(e.EventPriority))},
	}}}, nil
}

// DecodeExceptionSchedule decodes a BACnetExceptionSchedule from the flat
// elements ReadProperty returns (period, [2], [3], repeated) or from a value
// built by EncodeExceptionSchedule (one constructed element per event). The
// event priority is returned as sent, so a peer's out-of-range priority can
// be seen; clause 12.24.8 bounds it to 1..16. One over 255 does not fit
// SpecialEvent and is ErrMalformed.
func DecodeExceptionSchedule(v ApplicationValue) (ExceptionSchedule, error) {
	if v.Kind != ValueConstructed {
		return ExceptionSchedule{}, fmt.Errorf("%w: ExceptionSchedule is not constructed", ErrMalformed)
	}
	els := v.Elements
	out := ExceptionSchedule{SpecialEvents: make([]SpecialEvent, 0, len(els))}
	for i := 0; i < len(els); {
		if !els[i].Context && els[i].Value.Kind == ValueConstructed {
			e, err := decodeSpecialEvent(els[i].Value.Elements)
			if err != nil {
				return ExceptionSchedule{}, err
			}
			out.SpecialEvents = append(out.SpecialEvents, e)
			i++
			continue
		}
		if i+3 > len(els) {
			return ExceptionSchedule{}, fmt.Errorf("%w: SpecialEvent needs 3 elements", ErrMalformed)
		}
		e, err := decodeSpecialEvent(els[i : i+3])
		if err != nil {
			return ExceptionSchedule{}, err
		}
		out.SpecialEvents = append(out.SpecialEvents, e)
		i += 3
	}
	return out, nil
}

func decodeSpecialEvent(els []Element) (SpecialEvent, error) {
	var e SpecialEvent
	if len(els) != 3 {
		return e, fmt.Errorf("%w: SpecialEvent needs 3 elements", ErrMalformed)
	}
	period, tvs, prio := els[0], els[1], els[2]
	switch {
	case period.Context && period.TagNumber == 0 && IsContextConstructed(period) && len(period.Value.Elements) == 1:
		ce, err := decodeCalendarEntryElement(period.Value.Elements[0])
		if err != nil {
			return e, err
		}
		e.CalendarEntry = ce
	case period.Context && period.TagNumber == 1 && !IsContextConstructed(period):
		id, err := contextOrTypedObjectID(period)
		if err != nil {
			return e, err
		}
		e.PeriodObject = &id
	default:
		return e, fmt.Errorf("%w: SpecialEvent period", ErrMalformed)
	}
	if tvs.TagNumber != 2 || !IsContextConstructed(tvs) {
		return e, fmt.Errorf("%w: SpecialEvent listOfTimeValues", ErrMalformed)
	}
	tvs.TagNumber = 0
	day, err := DecodeDailySchedule(ApplicationValue{Kind: ValueConstructed, Elements: []Element{tvs}})
	if err != nil {
		return e, err
	}
	e.ListOfTimeValues = day.DaySchedule
	if !prio.Context || prio.TagNumber != 3 || prio.Opening || prio.Closing {
		return e, fmt.Errorf("%w: SpecialEvent eventPriority", ErrMalformed)
	}
	p := prio.Value.Unsigned
	if prio.Value.Kind != ValueUnsigned {
		if p, err = ContextUnsigned(prio); err != nil {
			return e, fmt.Errorf("%w: SpecialEvent eventPriority", ErrMalformed)
		}
	}
	if p > 255 {
		return e, fmt.Errorf("%w: SpecialEvent eventPriority %d", ErrMalformed, p)
	}
	e.EventPriority = uint8(p)
	return e, nil
}

// contextOrTypedObjectID returns the object identifier of a context
// primitive, typed or in wire form.
func contextOrTypedObjectID(el Element) (ObjectIdentifier, error) {
	if el.Value.Kind == ValueObjectID {
		return el.Value.ObjectID, nil
	}
	return ContextObjectID(el)
}
