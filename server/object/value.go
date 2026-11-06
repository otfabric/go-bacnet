// SPDX-License-Identifier: MIT

package object

import (
	"context"
	"math"

	"github.com/otfabric/go-bacnet"
)

// Value is a CharacterString Value, Integer Value or Positive Integer Value
// object (clauses 12.37, 12.43, 12.44). Present_Value is a CharacterString,
// an INTEGER (Signed) or an Unsigned; the integers are held in 32 bits, as
// bacnet-stack's iv.c and piv.c hold them, and a value outside that range is
// value-out-of-range.
type Value struct {
	*Base
	kind         bacnet.ValueKind
	pv           bacnet.ApplicationValue // when not commandable
	pa           *priorityArray          // when commandable
	outOfService bool
}

// ValueConfig configures a value object. Fields that do not apply to the
// type are ignored.
type ValueConfig struct {
	Instance    uint32
	Name        string
	Description string
	// Commandable gives the object a priority array (Priority_Array,
	// Relinquish_Default, Current_Command_Priority).
	Commandable bool
	// Units is Engineering_Units of the integer values.
	Units uint32
	// Initial Present_Value (Relinquish_Default when commandable).
	Text     string // CharacterString Value
	Integer  int32  // Integer Value
	Unsigned uint32 // Positive Integer Value
}

// NewCharacterStringValue returns a CharacterString Value object
// (clause 12.37).
func NewCharacterStringValue(cfg ValueConfig) (*Value, error) {
	return newValue(bacnet.ObjectTypeCharacterStringValue, bacnet.ValueCharacterString, charString(cfg.Text), cfg)
}

// NewIntegerValue returns an Integer Value object (clause 12.43).
func NewIntegerValue(cfg ValueConfig) (*Value, error) {
	return newValue(bacnet.ObjectTypeIntegerValue, bacnet.ValueSigned, bacnet.SignedValue(int64(cfg.Integer)), cfg)
}

// NewPositiveIntegerValue returns a Positive Integer Value object
// (clause 12.44).
func NewPositiveIntegerValue(cfg ValueConfig) (*Value, error) {
	return newValue(bacnet.ObjectTypePositiveIntegerValue, bacnet.ValueUnsigned, bacnet.UnsignedValue(uint64(cfg.Unsigned)), cfg)
}

func newValue(t bacnet.ObjectType, kind bacnet.ValueKind, initial bacnet.ApplicationValue, cfg ValueConfig) (*Value, error) {
	if cfg.Instance >= bacnet.MaxObjectInstance {
		return nil, errorf("instance %d out of range", cfg.Instance)
	}
	if cfg.Name == "" {
		return nil, errorf("object name required")
	}
	v := &Value{Base: NewBase(bacnet.ObjectIdentifier{Type: t, Instance: cfg.Instance}, cfg.Name), kind: kind}
	pvChanged := func() []bacnet.PropertyIdentifier { return []bacnet.PropertyIdentifier{bacnet.PropertyPresentValue} }
	if cfg.Commandable {
		v.pa = &priorityArray{relinquish: initial}
		v.defineCommandable(v.pa, v.checkValue, pvChanged)
	} else {
		v.pv = initial
		v.Define(bacnet.PropertyPresentValue, &Property{Required: true, Writable: true,
			Get: func() bacnet.ApplicationValue { return v.pv }, Set: v.writePresentValue})
	}
	if cfg.Description != "" {
		v.Define(bacnet.PropertyDescription, &Property{Value: charString(cfg.Description), Writable: true, Kinds: []bacnet.ValueKind{bacnet.ValueCharacterString}})
	}
	v.Define(bacnet.PropertyStatusFlags, &Property{Required: true, Get: v.statusFlags})
	v.Define(bacnet.PropertyEventState, &Property{Value: bacnet.EnumValue(EventStateNormal)})
	v.Define(bacnet.PropertyReliability, &Property{Value: bacnet.EnumValue(ReliabilityNoFaultDetected)})
	v.Define(bacnet.PropertyOutOfService, &Property{Writable: true,
		Get: func() bacnet.ApplicationValue { return bacnet.BoolValue(v.outOfService) },
		Set: func(w Write) ([]bacnet.PropertyIdentifier, error) {
			if w.Property.ArrayIndex != nil {
				return nil, ErrNotAnArray
			}
			if w.Value.Kind != bacnet.ValueBoolean {
				return nil, ErrInvalidDataType
			}
			if v.outOfService == w.Value.Boolean {
				return nil, nil
			}
			v.outOfService = w.Value.Boolean
			return []bacnet.PropertyIdentifier{bacnet.PropertyOutOfService, bacnet.PropertyStatusFlags}, nil
		}})
	if kind != bacnet.ValueCharacterString {
		v.Define(bacnet.PropertyUnits, &Property{Required: true, Value: bacnet.EnumValue(cfg.Units)})
	}
	return v, nil
}

// checkValue validates a Present_Value or Relinquish_Default.
func (v *Value) checkValue(x bacnet.ApplicationValue) error {
	if x.Kind != v.kind {
		return ErrInvalidDataType
	}
	switch x.Kind {
	case bacnet.ValueSigned:
		if x.Signed < math.MinInt32 || x.Signed > math.MaxInt32 {
			return ErrValueOutOfRange
		}
	case bacnet.ValueUnsigned:
		if x.Unsigned > math.MaxUint32 {
			return ErrValueOutOfRange
		}
	default:
	}
	return nil
}

func (v *Value) writePresentValue(w Write) ([]bacnet.PropertyIdentifier, error) {
	if w.Property.ArrayIndex != nil {
		return nil, ErrNotAnArray
	}
	if err := v.checkValue(w.Value); err != nil {
		return nil, err
	}
	if sameValue(v.pv, w.Value) {
		return nil, nil
	}
	v.pv = w.Value.Clone()
	return []bacnet.PropertyIdentifier{bacnet.PropertyPresentValue}, nil
}

func (v *Value) statusFlags() bacnet.ApplicationValue {
	return statusFlags(false, false, false, v.outOfService)
}

func (v *Value) present() bacnet.ApplicationValue {
	if v.pa != nil {
		return v.pa.present()
	}
	return v.pv
}

// PresentValue returns the current Present_Value.
func (v *Value) PresentValue() bacnet.ApplicationValue {
	var x bacnet.ApplicationValue
	v.View(func() { x = v.present().Clone() })
	return x
}

// SetPresentValue sets Present_Value from the application; for a
// commandable object it sets Relinquish_Default.
func (v *Value) SetPresentValue(x bacnet.ApplicationValue) error {
	if err := v.checkValue(x); err != nil {
		return err
	}
	v.Update(func() []bacnet.PropertyIdentifier {
		before := v.present()
		if v.pa != nil {
			v.pa.relinquish = x.Clone()
		} else {
			v.pv = x.Clone()
		}
		if sameValue(before, v.present()) {
			return nil
		}
		return []bacnet.PropertyIdentifier{bacnet.PropertyPresentValue}
	})
	return nil
}

// COVValues implements COVReporter: Present_Value and Status_Flags
// (clause 13.1).
func (v *Value) COVValues(context.Context) []PropertyValue {
	var out []PropertyValue
	v.View(func() {
		out = []PropertyValue{
			{Property: bacnet.PropertyPresentValue, Value: v.present().Clone()},
			{Property: bacnet.PropertyStatusFlags, Value: v.statusFlags()},
		}
	})
	return out
}

// COVIncrement implements COVReporter: every change of Present_Value
// notifies (COV_Increment is not offered).
func (v *Value) COVIncrement() (float32, bool) { return 0, false }
