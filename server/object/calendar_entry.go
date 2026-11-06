// SPDX-License-Identifier: MIT

package object

import (
	"time"

	"github.com/otfabric/go-bacnet"
)

// Special values of a BACnetDate and a BACnetWeekNDay (clause 21).
const (
	dateAny       = 0xFF // any year, month, day or day of week
	monthOdd      = 13
	monthEven     = 14
	dayLast       = 32
	dayOdd        = 33
	dayEven       = 34
	weekLastSeven = 6 // weeks 6..9 count back from the end of the month
)

// fullYear is the calendar year of a BACnet date.
func fullYear(d bacnet.Date) int { return int(d.Year) + 1900 }

// daysIn returns the number of days of a month (1..12) of a year.
func daysIn(year, month int) int {
	return time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func monthMatches(pattern uint8, d bacnet.Date) bool {
	switch pattern {
	case dateAny:
		return true
	case monthOdd:
		return d.Month%2 == 1
	case monthEven:
		return d.Month%2 == 0
	default:
		return pattern == d.Month
	}
}

// dateMatches reports whether the specific date d matches the date pattern
// p with the special values of clause 21 BACnetDate: an unspecified (X'FF')
// field matches any value, month 13 and 14 the odd and even months, day 32
// the last day of the month and day 33 and 34 the odd and even days.
func dateMatches(p, d bacnet.Date) bool {
	if p.Year != dateAny && p.Year != d.Year {
		return false
	}
	if !monthMatches(p.Month, d) {
		return false
	}
	switch p.Day {
	case dateAny:
	case dayLast:
		if int(d.Day) != daysIn(fullYear(d), int(d.Month)) {
			return false
		}
	case dayOdd:
		if d.Day%2 != 1 {
			return false
		}
	case dayEven:
		if d.Day%2 != 0 {
			return false
		}
	default:
		if p.Day != d.Day {
			return false
		}
	}
	return p.Weekday == dateAny || p.Weekday == d.Weekday
}

// compareDate orders two dates by year, month and day; a field that is
// unspecified in either date is skipped and the day of week is ignored, as
// bacnet-stack's datetime_wildcard_compare_date (datetime.c) does. A fully
// unspecified date therefore compares equal to every date, which makes a
// date range with an unspecified start or end open on that side.
func compareDate(a, b bacnet.Date) int {
	if a.Year != dateAny && b.Year != dateAny && a.Year != b.Year {
		return int(a.Year) - int(b.Year)
	}
	if a.Month != dateAny && b.Month != dateAny && a.Month != b.Month {
		return int(a.Month) - int(b.Month)
	}
	if a.Day != dateAny && b.Day != dateAny && a.Day != b.Day {
		return int(a.Day) - int(b.Day)
	}
	return 0
}

// dateInRange reports whether d lies in r, both ends included.
func dateInRange(d bacnet.Date, r bacnet.DateRange) bool {
	return compareDate(r.Start, d) <= 0 && compareDate(d, r.End) <= 0
}

// weekNDayMatches reports whether d matches a BACnetWeekNDay (clause 21):
// month (13 odd, 14 even), week of month (1 = days 1-7 … 5 = days 29-31,
// 6 = the last 7 days of the month, 7..9 the 7 days before the last 7, 14
// and 21) and day of week (1 = Monday); X'FF' matches any.
func weekNDayMatches(w bacnet.WeekNDay, d bacnet.Date) bool {
	if !monthMatches(w.Month, d) {
		return false
	}
	if w.DayOfWeek != dateAny && w.DayOfWeek != d.Weekday {
		return false
	}
	switch {
	case w.WeekOfMonth == dateAny:
		return true
	case w.WeekOfMonth >= 1 && w.WeekOfMonth <= 5:
		return int(w.WeekOfMonth) == (int(d.Day)-1)/7+1
	case w.WeekOfMonth >= weekLastSeven && w.WeekOfMonth <= 9:
		toEnd := daysIn(fullYear(d), int(d.Month)) - int(d.Day)
		return int(w.WeekOfMonth)-weekLastSeven == toEnd/7
	default:
		return false
	}
}

// entryMatches reports whether the specific date d is in a calendar entry.
func entryMatches(e bacnet.CalendarEntry, d bacnet.Date) bool {
	switch e.Choice {
	case bacnet.CalendarEntryDate:
		return dateMatches(e.Date, d)
	case bacnet.CalendarEntryDateRange:
		return dateInRange(d, e.DateRange)
	case bacnet.CalendarEntryWeekNDay:
		return weekNDayMatches(e.WeekNDay, d)
	default:
		return false
	}
}

// calendarEntryElement encodes one BACnetCalendarEntry as its context
// element (date [0], dateRange [1] or weekNDay [2]).
func calendarEntryElement(e bacnet.CalendarEntry) (bacnet.Element, error) {
	v, err := bacnet.EncodeCalendarEntry(e)
	if err != nil {
		return bacnet.Element{}, err
	}
	return v.Elements[0], nil
}

// parseCalendarEntry decodes one BACnetCalendarEntry context element, in
// wire form or as built by calendarEntryElement.
func parseCalendarEntry(el bacnet.Element) (bacnet.CalendarEntry, error) {
	e, err := bacnet.DecodeCalendarEntry(bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{el}})
	if err != nil {
		return bacnet.CalendarEntry{}, ErrInvalidDataType
	}
	return e, nil
}

// listElements returns the elements of a written list value: a constructed
// value holds them, any other value is a list of one.
func listElements(v bacnet.ApplicationValue) []bacnet.Element {
	if v.Kind == bacnet.ValueConstructed {
		return v.Elements
	}
	return []bacnet.Element{{Value: v}}
}

// ctxUnsigned returns the unsigned value of a context primitive, typed or
// in wire form.
func ctxUnsigned(el bacnet.Element) (uint64, bool) {
	if !el.Context || el.Opening || el.Closing {
		return 0, false
	}
	if el.Value.Kind == bacnet.ValueUnsigned {
		return el.Value.Unsigned, true
	}
	u, err := bacnet.ContextUnsigned(el)
	return u, err == nil
}

// ctxObjectID returns the object identifier of a context primitive, typed
// or in wire form.
func ctxObjectID(el bacnet.Element) (bacnet.ObjectIdentifier, bool) {
	if !el.Context || el.Opening || el.Closing {
		return bacnet.ObjectIdentifier{}, false
	}
	if el.Value.Kind == bacnet.ValueObjectID {
		return el.Value.ObjectID, true
	}
	id, err := bacnet.ContextObjectID(el)
	return id, err == nil
}
