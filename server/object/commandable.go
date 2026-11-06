// SPDX-License-Identifier: MIT

package object

import "github.com/otfabric/go-bacnet"

// priorityArray is the command prioritisation of clause 19.2: sixteen
// slots, Present_Value is the value in the lowest-numbered non-NULL slot,
// or Relinquish_Default when every slot is NULL.
type priorityArray struct {
	slots      [16]*bacnet.ApplicationValue
	relinquish bacnet.ApplicationValue
}

// defaultPriority is used for a write without a priority (clause 19.2).
const defaultPriority = 16

func (pa *priorityArray) present() bacnet.ApplicationValue {
	for _, s := range pa.slots {
		if s != nil {
			return *s
		}
	}
	return pa.relinquish
}

func (pa *priorityArray) active() (uint8, bool) {
	for i, s := range pa.slots {
		if s != nil {
			return uint8(i + 1), true
		}
	}
	return 0, false
}

// write stores v at priority (0 = default); NULL relinquishes the slot.
func (pa *priorityArray) write(v bacnet.ApplicationValue, priority uint8) error {
	if priority == 0 {
		priority = defaultPriority
	}
	if priority < 1 || priority > 16 {
		return ErrValueOutOfRange
	}
	if v.Kind == bacnet.ValueNull {
		pa.slots[priority-1] = nil
		return nil
	}
	c := v.Clone()
	pa.slots[priority-1] = &c
	return nil
}

func (pa *priorityArray) value() bacnet.ApplicationValue {
	els := make([]bacnet.Element, 16)
	for i, s := range pa.slots {
		if s == nil {
			els[i] = bacnet.Element{Value: bacnet.NullValue()}
		} else {
			els[i] = bacnet.Element{Value: *s}
		}
	}
	return bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: els}
}

func (pa *priorityArray) currentPriority() bacnet.ApplicationValue {
	if p, ok := pa.active(); ok {
		return bacnet.UnsignedValue(uint64(p))
	}
	return bacnet.NullValue()
}

// defineCommandable adds Priority_Array, Relinquish_Default and
// Current_Command_Priority, and makes Present_Value commandable. check
// validates a written value (kind and range); notify lists what changes
// when the present value changes.
func (b *Base) defineCommandable(pa *priorityArray, check func(bacnet.ApplicationValue) error, onChange func() []bacnet.PropertyIdentifier) {
	b.Define(bacnet.PropertyPriorityArray, &Property{Required: true, Array: true, Get: pa.value})
	b.Define(bacnet.PropertyRelinquishDefault, &Property{Required: true, Writable: true,
		Get: func() bacnet.ApplicationValue { return pa.relinquish },
		Set: func(w Write) ([]bacnet.PropertyIdentifier, error) {
			if w.Property.ArrayIndex != nil {
				return nil, ErrNotAnArray
			}
			if err := check(w.Value); err != nil {
				return nil, err
			}
			before := pa.present()
			pa.relinquish = w.Value.Clone()
			changed := []bacnet.PropertyIdentifier{bacnet.PropertyRelinquishDefault}
			if !sameValue(before, pa.present()) {
				changed = append(changed, onChange()...)
			}
			return changed, nil
		}})
	b.Define(bacnet.PropertyCurrentCommandPriority, &Property{Required: true, Get: pa.currentPriority})
	b.Define(bacnet.PropertyPresentValue, &Property{Required: true, Writable: true, Get: pa.present,
		Set: func(w Write) ([]bacnet.PropertyIdentifier, error) {
			if w.Property.ArrayIndex != nil {
				return nil, ErrNotAnArray
			}
			if w.Value.Kind != bacnet.ValueNull {
				if err := check(w.Value); err != nil {
					return nil, err
				}
			}
			before := pa.present()
			if err := pa.write(w.Value, w.Priority); err != nil {
				return nil, err
			}
			changed := []bacnet.PropertyIdentifier{bacnet.PropertyPriorityArray, bacnet.PropertyCurrentCommandPriority}
			if !sameValue(before, pa.present()) {
				changed = append(changed, onChange()...)
			}
			return changed, nil
		}})
}

func sameValue(a, b bacnet.ApplicationValue) bool {
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case bacnet.ValueReal:
		return a.Real == b.Real
	case bacnet.ValueEnumerated:
		return a.Enumerated == b.Enumerated
	case bacnet.ValueUnsigned:
		return a.Unsigned == b.Unsigned
	case bacnet.ValueBoolean:
		return a.Boolean == b.Boolean
	case bacnet.ValueNull:
		return true
	default:
		ea, err1 := bacnet.AppendApplicationValue(nil, a)
		eb, err2 := bacnet.AppendApplicationValue(nil, b)
		return err1 == nil && err2 == nil && string(ea) == string(eb)
	}
}
