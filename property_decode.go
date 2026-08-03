// SPDX-License-Identifier: MIT

package bacnet

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
				if el.Value.Kind == ValueDate {
					entries = append(entries, CalendarEntry{Choice: CalendarEntryDate, Date: el.Value.Date})
					continue
				}
				if dr, err := DecodeDateRange(el.Value); err == nil {
					entries = append(entries, CalendarEntry{Choice: CalendarEntryDateRange, DateRange: dr})
					continue
				}
				// Preserve opaque element as date entry fallback failure.
				return nil, true, ErrMalformed
			}
			return entries, true, nil
		}
	default:
		return nil, false, nil
	}
	return nil, false, nil
}
