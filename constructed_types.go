// SPDX-License-Identifier: MIT

package bacnet

import "fmt"

// DateRange is BACnetDateRange (start-date, end-date).
type DateRange struct {
	Start Date
	End   Date
}

// DateTime is BACnetDateTime.
type DateTime struct {
	Date Date
	Time Time
}

// TimeStampChoice discriminates BACnetTimeStamp.
type TimeStampChoice uint8

const (
	TimeStampTime TimeStampChoice = iota
	TimeStampSequence
	TimeStampDateTime
)

// TimeStamp is BACnetTimeStamp.
type TimeStamp struct {
	Choice   TimeStampChoice
	Time     Time
	Sequence uint16
	DateTime DateTime
}

// CalendarEntryChoice discriminates BACnetCalendarEntry.
type CalendarEntryChoice uint8

const (
	CalendarEntryDate CalendarEntryChoice = iota
	CalendarEntryDateRange
	CalendarEntryWeekNDay
)

// WeekNDay is BACnetWeekNDay (month, weekOfMonth, dayOfWeek).
type WeekNDay struct {
	Month       uint8
	WeekOfMonth uint8
	DayOfWeek   uint8
}

// CalendarEntry is BACnetCalendarEntry.
type CalendarEntry struct {
	Choice    CalendarEntryChoice
	Date      Date
	DateRange DateRange
	WeekNDay  WeekNDay
}

// DateValue returns a Date application value.
func DateValue(d Date) ApplicationValue {
	return ApplicationValue{Kind: ValueDate, Date: d}
}

// TimeValue returns a Time application value.
func TimeValue(t Time) ApplicationValue {
	return ApplicationValue{Kind: ValueTime, Time: t}
}

// AsDate converts a Date value.
func AsDate(v ApplicationValue) (Date, error) {
	if v.Kind != ValueDate {
		return Date{}, fmt.Errorf("%w: value kind %d is not date", ErrUnsupported, v.Kind)
	}
	return v.Date, nil
}

// AsTime converts a Time value.
func AsTime(v ApplicationValue) (Time, error) {
	if v.Kind != ValueTime {
		return Time{}, fmt.Errorf("%w: value kind %d is not time", ErrUnsupported, v.Kind)
	}
	return v.Time, nil
}

// EncodeDateRange encodes a DateRange as two application Date tags.
func EncodeDateRange(dr DateRange) ApplicationValue {
	return ApplicationValue{
		Kind: ValueConstructed,
		Elements: []Element{
			{Value: DateValue(dr.Start)},
			{Value: DateValue(dr.End)},
		},
	}
}

// DecodeDateRange decodes a constructed DateRange.
func DecodeDateRange(v ApplicationValue) (DateRange, error) {
	if len(v.Elements) != 2 {
		return DateRange{}, fmt.Errorf("%w: DateRange needs 2 elements", ErrMalformed)
	}
	start, err := AsDate(v.Elements[0].Value)
	if err != nil {
		return DateRange{}, err
	}
	end, err := AsDate(v.Elements[1].Value)
	if err != nil {
		return DateRange{}, err
	}
	return DateRange{Start: start, End: end}, nil
}

// EncodeDateTime encodes DateTime as date+time application tags.
func EncodeDateTime(dt DateTime) ApplicationValue {
	return ApplicationValue{
		Kind: ValueConstructed,
		Elements: []Element{
			{Value: DateValue(dt.Date)},
			{Value: TimeValue(dt.Time)},
		},
	}
}

// DecodeDateTime decodes a DateTime.
func DecodeDateTime(v ApplicationValue) (DateTime, error) {
	if len(v.Elements) != 2 {
		return DateTime{}, fmt.Errorf("%w: DateTime needs 2 elements", ErrMalformed)
	}
	d, err := AsDate(v.Elements[0].Value)
	if err != nil {
		return DateTime{}, err
	}
	tm, err := AsTime(v.Elements[1].Value)
	if err != nil {
		return DateTime{}, err
	}
	return DateTime{Date: d, Time: tm}, nil
}

// TimeValue is BACnetTimeValue (time + any value).
type ScheduledTimeValue struct {
	Time  Time
	Value ApplicationValue
}

// DailySchedule is BACnetDailySchedule (SEQUENCE OF TimeValue).
type DailySchedule struct {
	DaySchedule []ScheduledTimeValue
}

// WeeklySchedule is BACnetWeeklySchedule (ARRAY[7] OF DailySchedule).
type WeeklySchedule [7]DailySchedule

// SpecialEvent is one BACnetSpecialEvent (period CHOICE + list of TimeValue + priority).
type SpecialEvent struct {
	CalendarEntry    CalendarEntry // period as calendarEntry; CalendarRef uses ObjectID in PeriodObject
	PeriodObject     *ObjectIdentifier
	ListOfTimeValues []ScheduledTimeValue
	EventPriority    uint8
}

// ExceptionSchedule is BACnetExceptionSchedule (SEQUENCE OF SpecialEvent).
type ExceptionSchedule struct {
	SpecialEvents []SpecialEvent
}

// HostNPort is BACnetHostNPort (host CHOICE + optional port).
type HostNPort struct {
	IP   [4]byte
	Name string // non-empty means ip-address absent / name present
	Port uint16
}

// EncodeCalendarEntry encodes a CalendarEntry CHOICE as a constructed value.
func EncodeCalendarEntry(e CalendarEntry) (ApplicationValue, error) {
	switch e.Choice {
	case CalendarEntryDate:
		return ApplicationValue{Kind: ValueConstructed, Elements: []Element{{
			Context: true, TagNumber: 0, Value: DateValue(e.Date),
		}}}, nil
	case CalendarEntryDateRange:
		return ApplicationValue{Kind: ValueConstructed, Elements: []Element{{
			Context: true, TagNumber: 1, Value: EncodeDateRange(e.DateRange),
		}}}, nil
	case CalendarEntryWeekNDay:
		return ApplicationValue{Kind: ValueConstructed, Elements: []Element{{
			Context: true, TagNumber: 2,
			Value: ApplicationValue{Kind: ValueOctetString, OctetString: []byte{e.WeekNDay.Month, e.WeekNDay.WeekOfMonth, e.WeekNDay.DayOfWeek}},
		}}}, nil
	default:
		return ApplicationValue{}, fmt.Errorf("%w: CalendarEntry choice", ErrMalformed)
	}
}

// DecodeCalendarEntry decodes a CalendarEntry CHOICE.
func DecodeCalendarEntry(v ApplicationValue) (CalendarEntry, error) {
	if len(v.Elements) != 1 || !v.Elements[0].Context {
		return CalendarEntry{}, fmt.Errorf("%w: CalendarEntry CHOICE", ErrMalformed)
	}
	el := v.Elements[0]
	switch el.TagNumber {
	case 0:
		d, err := AsDate(el.Value)
		if err != nil {
			return CalendarEntry{}, err
		}
		return CalendarEntry{Choice: CalendarEntryDate, Date: d}, nil
	case 1:
		dr, err := DecodeDateRange(el.Value)
		if err != nil {
			return CalendarEntry{}, err
		}
		return CalendarEntry{Choice: CalendarEntryDateRange, DateRange: dr}, nil
	case 2:
		b := el.Value.OctetString
		if len(b) != 3 {
			return CalendarEntry{}, fmt.Errorf("%w: WeekNDay", ErrMalformed)
		}
		return CalendarEntry{Choice: CalendarEntryWeekNDay, WeekNDay: WeekNDay{Month: b[0], WeekOfMonth: b[1], DayOfWeek: b[2]}}, nil
	default:
		return CalendarEntry{}, fmt.Errorf("%w: CalendarEntry tag %d", ErrMalformed, el.TagNumber)
	}
}

// EncodeDailySchedule encodes SEQUENCE OF TimeValue under context tag 0.
func EncodeDailySchedule(d DailySchedule) ApplicationValue {
	els := make([]Element, 0, len(d.DaySchedule))
	for _, tv := range d.DaySchedule {
		els = append(els, Element{Value: ApplicationValue{
			Kind: ValueConstructed,
			Elements: []Element{
				{Value: TimeValue(tv.Time)},
				{Value: tv.Value},
			},
		}})
	}
	return ApplicationValue{Kind: ValueConstructed, Elements: []Element{{
		Context: true, TagNumber: 0, Value: ApplicationValue{Kind: ValueConstructed, Elements: els},
	}}}
}

// TimeValueTag returns an application Time tag (named to avoid clash with TimeValue type helpers).
// DecodeDailySchedule decodes a DailySchedule.
func DecodeDailySchedule(v ApplicationValue) (DailySchedule, error) {
	if len(v.Elements) != 1 || !v.Elements[0].Context || v.Elements[0].TagNumber != 0 {
		return DailySchedule{}, fmt.Errorf("%w: DailySchedule", ErrMalformed)
	}
	inner := v.Elements[0].Value.Elements
	out := DailySchedule{DaySchedule: make([]ScheduledTimeValue, 0, len(inner))}
	for _, el := range inner {
		if len(el.Value.Elements) < 2 {
			return DailySchedule{}, fmt.Errorf("%w: TimeValue", ErrMalformed)
		}
		tm, err := AsTime(el.Value.Elements[0].Value)
		if err != nil {
			return DailySchedule{}, err
		}
		out.DaySchedule = append(out.DaySchedule, ScheduledTimeValue{Time: tm, Value: el.Value.Elements[1].Value})
	}
	return out, nil
}

// EncodeWeeklySchedule encodes ARRAY[7] OF DailySchedule as seven constructed days.
func EncodeWeeklySchedule(w WeeklySchedule) ApplicationValue {
	els := make([]Element, 7)
	for i := 0; i < 7; i++ {
		els[i] = Element{Value: EncodeDailySchedule(w[i])}
	}
	return ApplicationValue{Kind: ValueConstructed, Elements: els}
}

// DecodeWeeklySchedule decodes seven DailySchedule elements.
func DecodeWeeklySchedule(v ApplicationValue) (WeeklySchedule, error) {
	var w WeeklySchedule
	if len(v.Elements) != 7 {
		return w, fmt.Errorf("%w: WeeklySchedule needs 7 days", ErrMalformed)
	}
	for i := 0; i < 7; i++ {
		d, err := DecodeDailySchedule(v.Elements[i].Value)
		if err != nil {
			return w, err
		}
		w[i] = d
	}
	return w, nil
}

// EncodeHostNPort encodes host as ip-address [0] octet string + port [1].
func EncodeHostNPort(h HostNPort) ApplicationValue {
	var host Element
	if h.Name != "" {
		host = Element{Context: true, TagNumber: 1, Value: ApplicationValue{Kind: ValueCharacterString, Character: CharacterString{Value: h.Name}}}
	} else {
		host = Element{Context: true, TagNumber: 0, Value: ApplicationValue{Kind: ValueOctetString, OctetString: h.IP[:]}}
	}
	return ApplicationValue{Kind: ValueConstructed, Elements: []Element{
		host,
		{Context: true, TagNumber: 2, Value: ApplicationValue{Kind: ValueUnsigned, Unsigned: uint64(h.Port)}},
	}}
}

// DecodeHostNPort decodes HostNPort.
func DecodeHostNPort(v ApplicationValue) (HostNPort, error) {
	var h HostNPort
	for _, el := range v.Elements {
		switch {
		case el.Context && el.TagNumber == 0:
			b := el.Value.OctetString
			if len(b) != 4 {
				return HostNPort{}, fmt.Errorf("%w: HostNPort ip", ErrMalformed)
			}
			copy(h.IP[:], b)
		case el.Context && el.TagNumber == 1:
			h.Name = el.Value.Character.Value
		case el.Context && el.TagNumber == 2:
			h.Port = uint16(el.Value.Unsigned)
		}
	}
	return h, nil
}
