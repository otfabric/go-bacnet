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

// ScheduledTimeValue is BACnetTimeValue (time + primitive value).
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

// HostNPort is BACnetHostNPort:
//
//	host [0] BACnetHostAddress CHOICE {
//	  none       [0] NULL,
//	  ip-address [1] OCTET STRING,
//	  name       [2] CharacterString }
//	port [1] Unsigned16
//
// A non-empty Name selects the name alternative, None selects none, and
// otherwise the IPv4 address in IP is encoded as ip-address.
type HostNPort struct {
	IP   [4]byte
	Name string // non-empty means name is present and IP is ignored
	None bool   // host is the "none" alternative; IP and Name are ignored
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
			Value: ApplicationValue{Kind: ValueContext, OctetString: []byte{e.WeekNDay.Month, e.WeekNDay.WeekOfMonth, e.WeekNDay.DayOfWeek}},
		}}}, nil
	default:
		return ApplicationValue{}, fmt.Errorf("%w: CalendarEntry choice", ErrMalformed)
	}
}

// DecodeCalendarEntry decodes a CalendarEntry CHOICE from a constructed value
// holding exactly one context element: date [0], dateRange [1] or
// weekNDay [2].
func DecodeCalendarEntry(v ApplicationValue) (CalendarEntry, error) {
	if len(v.Elements) != 1 {
		return CalendarEntry{}, fmt.Errorf("%w: CalendarEntry CHOICE", ErrMalformed)
	}
	return decodeCalendarEntryElement(v.Elements[0])
}

// decodeCalendarEntryElement decodes one context-tagged CalendarEntry
// alternative. Wire data carries the date and weekNDay alternatives as raw
// context octets; values built with EncodeCalendarEntry carry typed values.
func decodeCalendarEntryElement(el Element) (CalendarEntry, error) {
	if !el.Context || el.Opening || el.Closing {
		return CalendarEntry{}, fmt.Errorf("%w: CalendarEntry CHOICE", ErrMalformed)
	}
	switch el.TagNumber {
	case 0:
		if el.Value.Kind == ValueDate {
			return CalendarEntry{Choice: CalendarEntryDate, Date: el.Value.Date}, nil
		}
		d, err := ContextDate(el)
		if err != nil {
			return CalendarEntry{}, fmt.Errorf("%w: CalendarEntry date", ErrMalformed)
		}
		return CalendarEntry{Choice: CalendarEntryDate, Date: d}, nil
	case 1:
		if el.Value.Kind != ValueConstructed {
			return CalendarEntry{}, fmt.Errorf("%w: CalendarEntry dateRange", ErrMalformed)
		}
		dr, err := DecodeDateRange(el.Value)
		if err != nil {
			return CalendarEntry{}, fmt.Errorf("%w: CalendarEntry dateRange", ErrMalformed)
		}
		return CalendarEntry{Choice: CalendarEntryDateRange, DateRange: dr}, nil
	case 2:
		b := el.Value.OctetString
		if el.Value.Kind == ValueConstructed || len(b) != 3 {
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

// DecodeDailySchedule decodes a DailySchedule: one context [0] constructed
// element holding a flat sequence of (Time, value) application-tag pairs, as
// ParseSequence returns it from the wire.
func DecodeDailySchedule(v ApplicationValue) (DailySchedule, error) {
	if len(v.Elements) != 1 || !IsContextConstructed(v.Elements[0]) || v.Elements[0].TagNumber != 0 {
		return DailySchedule{}, fmt.Errorf("%w: DailySchedule", ErrMalformed)
	}
	inner := v.Elements[0].Value.Elements
	out := DailySchedule{DaySchedule: make([]ScheduledTimeValue, 0, len(inner)/2)}
	for i := 0; i < len(inner); {
		el := inner[i]
		// In-memory form built by EncodeDailySchedule: each BACnetTimeValue is
		// grouped in its own constructed element.
		if !el.Context && el.Value.Kind == ValueConstructed {
			if len(el.Value.Elements) != 2 {
				return DailySchedule{}, fmt.Errorf("%w: TimeValue", ErrMalformed)
			}
			tm, err := AsTime(el.Value.Elements[0].Value)
			if err != nil {
				return DailySchedule{}, fmt.Errorf("%w: TimeValue time", ErrMalformed)
			}
			out.DaySchedule = append(out.DaySchedule, ScheduledTimeValue{Time: tm, Value: el.Value.Elements[1].Value.Clone()})
			i++
			continue
		}
		// Wire form: application Time followed by one application primitive.
		if el.Context || el.Value.Kind != ValueTime {
			return DailySchedule{}, fmt.Errorf("%w: TimeValue time", ErrMalformed)
		}
		if i+1 >= len(inner) || inner[i+1].Context {
			return DailySchedule{}, fmt.Errorf("%w: TimeValue value", ErrMalformed)
		}
		out.DaySchedule = append(out.DaySchedule, ScheduledTimeValue{Time: el.Value.Time, Value: inner[i+1].Value.Clone()})
		i += 2
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

// DecodeWeeklySchedule decodes seven DailySchedule elements. Each element is
// either the day's context [0] constructed element (wire form, as returned by
// ParseSequence or ReadProperty) or a value built by EncodeWeeklySchedule.
func DecodeWeeklySchedule(v ApplicationValue) (WeeklySchedule, error) {
	var w WeeklySchedule
	if len(v.Elements) != 7 {
		return w, fmt.Errorf("%w: WeeklySchedule needs 7 days", ErrMalformed)
	}
	for i := 0; i < 7; i++ {
		day := v.Elements[i].Value
		if IsContextConstructed(v.Elements[i]) {
			day = ApplicationValue{Kind: ValueConstructed, Elements: []Element{v.Elements[i]}}
		}
		d, err := DecodeDailySchedule(day)
		if err != nil {
			return w, err
		}
		w[i] = d
	}
	return w, nil
}

// EncodeHostNPort encodes a BACnetHostNPort: host [0] { none [0] |
// ip-address [1] | name [2] } followed by port [1].
func EncodeHostNPort(h HostNPort) ApplicationValue {
	var host Element
	switch {
	case h.None:
		host = Element{Context: true, TagNumber: 0, Value: ApplicationValue{Kind: ValueContext}}
	case h.Name != "":
		host = Element{Context: true, TagNumber: 2, Value: ApplicationValue{Kind: ValueCharacterString, Character: CharacterString{Value: h.Name}}}
	default:
		host = Element{Context: true, TagNumber: 1, Value: ApplicationValue{Kind: ValueOctetString, OctetString: append([]byte(nil), h.IP[:]...)}}
	}
	return ApplicationValue{Kind: ValueConstructed, Elements: []Element{
		{Context: true, TagNumber: 0, Value: ApplicationValue{Kind: ValueConstructed, Elements: []Element{host}}},
		{Context: true, TagNumber: 1, Value: UnsignedValue(uint64(h.Port))},
	}}
}

// DecodeHostNPort decodes a BACnetHostNPort from wire elements or from a
// value built by EncodeHostNPort. Only 4-octet (IPv4) ip-address values are
// supported; other lengths return an error wrapping ErrUnsupported.
func DecodeHostNPort(v ApplicationValue) (HostNPort, error) {
	if len(v.Elements) != 2 {
		return HostNPort{}, fmt.Errorf("%w: HostNPort needs host and port", ErrMalformed)
	}
	hostEl, portEl := v.Elements[0], v.Elements[1]
	if hostEl.TagNumber != 0 || !IsContextConstructed(hostEl) || len(hostEl.Value.Elements) != 1 {
		return HostNPort{}, fmt.Errorf("%w: HostNPort host", ErrMalformed)
	}
	choice := hostEl.Value.Elements[0]
	if !choice.Context || choice.Opening || choice.Closing || choice.Value.Kind == ValueConstructed {
		return HostNPort{}, fmt.Errorf("%w: HostNPort host choice", ErrMalformed)
	}
	var h HostNPort
	switch choice.TagNumber {
	case 0:
		if len(choice.Value.OctetString) != 0 {
			return HostNPort{}, fmt.Errorf("%w: HostNPort none", ErrMalformed)
		}
		h.None = true
	case 1:
		b := choice.Value.OctetString
		if len(b) != 4 {
			return HostNPort{}, fmt.Errorf("%w: HostNPort ip-address of %d octets", ErrUnsupported, len(b))
		}
		copy(h.IP[:], b)
	case 2:
		if choice.Value.Kind == ValueCharacterString {
			h.Name = choice.Value.Character.Value
			break
		}
		cs, err := ContextCharacterString(choice)
		if err != nil {
			return HostNPort{}, fmt.Errorf("%w: HostNPort name", ErrMalformed)
		}
		h.Name = cs.Value
	default:
		return HostNPort{}, fmt.Errorf("%w: HostNPort host tag %d", ErrMalformed, choice.TagNumber)
	}
	if !portEl.Context || portEl.TagNumber != 1 || portEl.Opening || portEl.Closing || portEl.Value.Kind == ValueConstructed {
		return HostNPort{}, fmt.Errorf("%w: HostNPort port", ErrMalformed)
	}
	port := portEl.Value.Unsigned
	if portEl.Value.Kind != ValueUnsigned {
		u, err := ContextUnsigned(portEl)
		if err != nil {
			return HostNPort{}, fmt.Errorf("%w: HostNPort port", ErrMalformed)
		}
		port = u
	}
	if port > 0xFFFF {
		return HostNPort{}, fmt.Errorf("%w: HostNPort port %d", ErrMalformed, port)
	}
	h.Port = uint16(port)
	return h, nil
}
