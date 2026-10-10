// SPDX-License-Identifier: MIT

package bacnet

import "fmt"

// DecodePropertyValue optionally maps a known object/property combination to a
// typed value. Unknown combinations return ok=false with no error so callers
// keep the raw ApplicationValue.
func DecodePropertyValue(objectType ObjectType, property PropertyIdentifier, value ApplicationValue) (any, bool, error) {
	switch property {
	case PropertyObjectList:
		ids := make([]ObjectIdentifier, 0, len(value.Elements))
		for _, el := range value.Elements {
			id, err := AsObjectID(el.Value)
			if err != nil {
				return nil, true, err
			}
			ids = append(ids, id)
		}
		if value.Kind == ValueObjectID {
			return []ObjectIdentifier{value.ObjectID}, true, nil
		}
		if len(ids) > 0 || value.Kind == ValueConstructed {
			return ids, true, nil
		}
	case PropertyPropertyList:
		ids := make([]PropertyIdentifier, 0, len(value.Elements))
		for _, el := range value.Elements {
			n, err := AsEnumerated(el.Value)
			if err != nil {
				return nil, true, err
			}
			ids = append(ids, PropertyIdentifier(n))
		}
		if value.Kind == ValueEnumerated {
			return []PropertyIdentifier{PropertyIdentifier(value.Enumerated)}, true, nil
		}
		if len(ids) > 0 || value.Kind == ValueConstructed {
			return ids, true, nil
		}
	case PropertyDateList:
		if objectType == ObjectTypeCalendar && (value.Kind == ValueConstructed || len(value.Elements) > 0) {
			entries := make([]CalendarEntry, 0, len(value.Elements))
			for _, el := range value.Elements {
				// Legacy in-memory shapes: a bare Date or a bare DateRange.
				if !el.Context && el.Value.Kind == ValueDate {
					entries = append(entries, CalendarEntry{Choice: CalendarEntryDate, Date: el.Value.Date})
					continue
				}
				if !el.Context {
					dr, err := DecodeDateRange(el.Value)
					if err != nil {
						return nil, true, fmt.Errorf("%w: Date_List entry", ErrMalformed)
					}
					entries = append(entries, CalendarEntry{Choice: CalendarEntryDateRange, DateRange: dr})
					continue
				}
				// Wire form: SEQUENCE OF BACnetCalendarEntry, one context
				// element per entry (date [0], dateRange [1], weekNDay [2]).
				entry, err := decodeCalendarEntryElement(el)
				if err != nil {
					return nil, true, err
				}
				entries = append(entries, entry)
			}
			return entries, true, nil
		}
	default:
		return nil, false, nil
	}
	return nil, false, nil
}
