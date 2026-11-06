// SPDX-License-Identifier: MIT

package object

import (
	"context"
	"math"

	"github.com/otfabric/go-bacnet"
)

// Event states (BACnetEventState).
const (
	EventStateNormal    uint32 = 0
	EventStateFault     uint32 = 1
	EventStateOffnormal uint32 = 2
	EventStateHighLimit uint32 = 3
	EventStateLowLimit  uint32 = 4
)

// Reliability values (BACnetReliability).
const (
	ReliabilityNoFaultDetected uint32 = 0
	ReliabilityMultiStateFault uint32 = 9
)

// pointKind is the value family of a point object.
type pointKind int

const (
	kindAnalog pointKind = iota
	kindBinary
	kindMultiState
)

// Point is an analog, binary or multi-state input, output or value object
// (clauses 12.2–12.4, 12.6–12.8, 12.18–12.20). The type of Present_Value
// follows the family: REAL, BACnetBinaryPV (ENUMERATED 0/1) or Unsigned
// 1..Number_Of_States.
type Point struct {
	*Base
	kind         pointKind
	input        bool
	pv           bacnet.ApplicationValue // when not commandable
	pa           *priorityArray          // when commandable
	outOfService bool
	eventState   uint32
	reliability  uint32
	covIncrement float32
	states       uint32
	intrinsic    *intrinsic
}

// PointConfig configures a point object. Fields that do not apply to the
// family are ignored.
type PointConfig struct {
	Instance    uint32
	Name        string
	Description string
	// Commandable gives a value object a priority array. Output objects are
	// always commandable, input objects never.
	Commandable bool

	// Analog.
	Units        uint32  // BACnetEngineeringUnits
	Value        float32 // initial Present_Value / Relinquish_Default
	COVIncrement float32 // default 0: every change notifies

	// Binary.
	Active       bool
	Polarity     uint32 // 0 normal, 1 reverse (inputs and outputs)
	ActiveText   string
	InactiveText string

	// Multi-state.
	NumberOfStates uint32 // required, >= 1
	State          uint32 // initial value, 1..NumberOfStates (default 1)
	StateText      []string

	// Events enables intrinsic reporting (analog and binary objects, and
	// Multi-state Input and Value objects).
	Events *EventConfig
}

// NewAnalogInput returns an Analog Input object (clause 12.2).
func NewAnalogInput(cfg PointConfig) (*Point, error) {
	return newPoint(bacnet.ObjectTypeAnalogInput, kindAnalog, true, false, cfg)
}

// NewAnalogOutput returns an Analog Output object (clause 12.3).
func NewAnalogOutput(cfg PointConfig) (*Point, error) {
	return newPoint(bacnet.ObjectTypeAnalogOutput, kindAnalog, false, true, cfg)
}

// NewAnalogValue returns an Analog Value object (clause 12.4).
func NewAnalogValue(cfg PointConfig) (*Point, error) {
	return newPoint(bacnet.ObjectTypeAnalogValue, kindAnalog, false, cfg.Commandable, cfg)
}

// NewBinaryInput returns a Binary Input object (clause 12.6).
func NewBinaryInput(cfg PointConfig) (*Point, error) {
	return newPoint(bacnet.ObjectTypeBinaryInput, kindBinary, true, false, cfg)
}

// NewBinaryOutput returns a Binary Output object (clause 12.7).
func NewBinaryOutput(cfg PointConfig) (*Point, error) {
	return newPoint(bacnet.ObjectTypeBinaryOutput, kindBinary, false, true, cfg)
}

// NewBinaryValue returns a Binary Value object (clause 12.8).
func NewBinaryValue(cfg PointConfig) (*Point, error) {
	return newPoint(bacnet.ObjectTypeBinaryValue, kindBinary, false, cfg.Commandable, cfg)
}

// NewMultiStateInput returns a Multi-state Input object (clause 12.18).
func NewMultiStateInput(cfg PointConfig) (*Point, error) {
	return newPoint(bacnet.ObjectTypeMultiStateInput, kindMultiState, true, false, cfg)
}

// NewMultiStateOutput returns a Multi-state Output object (clause 12.19).
func NewMultiStateOutput(cfg PointConfig) (*Point, error) {
	return newPoint(bacnet.ObjectTypeMultiStateOutput, kindMultiState, false, true, cfg)
}

// NewMultiStateValue returns a Multi-state Value object (clause 12.20).
func NewMultiStateValue(cfg PointConfig) (*Point, error) {
	return newPoint(bacnet.ObjectTypeMultiStateValue, kindMultiState, false, cfg.Commandable, cfg)
}

func newPoint(t bacnet.ObjectType, kind pointKind, input, commandable bool, cfg PointConfig) (*Point, error) {
	if cfg.Instance > bacnet.MaxObjectInstance-1 {
		return nil, errorf("instance %d out of range", cfg.Instance)
	}
	if cfg.Name == "" {
		return nil, errorf("object name required")
	}
	id := bacnet.ObjectIdentifier{Type: t, Instance: cfg.Instance}
	p := &Point{Base: NewBase(id, cfg.Name), kind: kind, input: input, reliability: ReliabilityNoFaultDetected}
	var initial bacnet.ApplicationValue
	switch kind {
	case kindAnalog:
		if cfg.COVIncrement < 0 || math.IsNaN(float64(cfg.COVIncrement)) {
			return nil, errorf("COV increment must be >= 0")
		}
		initial = bacnet.RealValue(cfg.Value)
		p.covIncrement = cfg.COVIncrement
	case kindBinary:
		if cfg.Polarity > 1 {
			return nil, errorf("polarity must be 0 or 1")
		}
		initial = bacnet.EnumValue(boolEnum(cfg.Active))
	case kindMultiState:
		if cfg.NumberOfStates == 0 {
			return nil, errorf("number of states required")
		}
		if cfg.StateText != nil && uint32(len(cfg.StateText)) != cfg.NumberOfStates {
			return nil, errorf("state text has %d entries for %d states", len(cfg.StateText), cfg.NumberOfStates)
		}
		st := cfg.State
		if st == 0 {
			st = 1
		}
		if st > cfg.NumberOfStates {
			return nil, errorf("state %d out of range 1..%d", st, cfg.NumberOfStates)
		}
		p.states = cfg.NumberOfStates
		initial = bacnet.UnsignedValue(uint64(st))
	}

	pvChanged := func() []bacnet.PropertyIdentifier { return []bacnet.PropertyIdentifier{bacnet.PropertyPresentValue} }
	if commandable {
		p.pa = &priorityArray{relinquish: initial}
		p.defineCommandable(p.pa, p.checkValue, pvChanged)
	} else {
		p.pv = initial
		p.Define(bacnet.PropertyPresentValue, &Property{Required: true, Writable: true,
			Get: func() bacnet.ApplicationValue { return p.pv },
			Set: p.writePresentValue})
	}
	if cfg.Description != "" {
		p.Define(bacnet.PropertyDescription, &Property{Value: charString(cfg.Description), Writable: true, Kinds: []bacnet.ValueKind{bacnet.ValueCharacterString}})
	}
	p.Define(bacnet.PropertyStatusFlags, &Property{Required: true, Get: p.statusFlags})
	p.Define(bacnet.PropertyEventState, &Property{Required: true, Get: func() bacnet.ApplicationValue { return bacnet.EnumValue(p.eventState) }})
	p.Define(bacnet.PropertyReliability, &Property{Get: func() bacnet.ApplicationValue { return bacnet.EnumValue(p.currentReliability()) }})
	p.Define(bacnet.PropertyOutOfService, &Property{Required: true, Writable: true,
		Get: func() bacnet.ApplicationValue { return bacnet.BoolValue(p.outOfService) },
		Set: func(w Write) ([]bacnet.PropertyIdentifier, error) {
			if w.Property.ArrayIndex != nil {
				return nil, ErrNotAnArray
			}
			if w.Value.Kind != bacnet.ValueBoolean {
				return nil, ErrInvalidDataType
			}
			if p.outOfService == w.Value.Boolean {
				return nil, nil
			}
			p.outOfService = w.Value.Boolean
			return []bacnet.PropertyIdentifier{bacnet.PropertyOutOfService, bacnet.PropertyStatusFlags}, nil
		}})
	switch kind {
	case kindAnalog:
		p.Define(bacnet.PropertyUnits, &Property{Required: true, Value: bacnet.EnumValue(cfg.Units)})
		p.Define(bacnet.PropertyCOVIncrement, &Property{Writable: true,
			Get: func() bacnet.ApplicationValue { return bacnet.RealValue(p.covIncrement) },
			Set: func(w Write) ([]bacnet.PropertyIdentifier, error) {
				if w.Property.ArrayIndex != nil {
					return nil, ErrNotAnArray
				}
				if w.Value.Kind != bacnet.ValueReal {
					return nil, ErrInvalidDataType
				}
				if w.Value.Real < 0 || math.IsNaN(float64(w.Value.Real)) {
					return nil, ErrValueOutOfRange
				}
				p.covIncrement = w.Value.Real
				return []bacnet.PropertyIdentifier{bacnet.PropertyCOVIncrement}, nil
			}})
	case kindBinary:
		if t != bacnet.ObjectTypeBinaryValue {
			p.Define(bacnet.PropertyPolarity, &Property{Required: true, Value: bacnet.EnumValue(cfg.Polarity)})
		}
		if cfg.ActiveText != "" || cfg.InactiveText != "" {
			p.Define(bacnet.PropertyInactiveText, &Property{Value: charString(cfg.InactiveText), Writable: true, Kinds: []bacnet.ValueKind{bacnet.ValueCharacterString}})
			p.Define(bacnet.PropertyActiveText, &Property{Value: charString(cfg.ActiveText), Writable: true, Kinds: []bacnet.ValueKind{bacnet.ValueCharacterString}})
		}
	case kindMultiState:
		p.Define(bacnet.PropertyNumberOfStates, &Property{Required: true, Value: bacnet.UnsignedValue(uint64(cfg.NumberOfStates))})
		if cfg.StateText != nil {
			els := make([]bacnet.Element, len(cfg.StateText))
			for i, s := range cfg.StateText {
				els[i] = bacnet.Element{Value: charString(s)}
			}
			p.Define(bacnet.PropertyStateText, &Property{Array: true, Writable: true, Kinds: []bacnet.ValueKind{bacnet.ValueCharacterString},
				Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: els}})
		}
	}
	if cfg.Events != nil {
		if t == bacnet.ObjectTypeMultiStateOutput {
			// A Multi-state Output reports COMMAND_FAILURE on
			// Feedback_Value (clause 12.19), which is not implemented.
			return nil, errorf("intrinsic reporting is not supported on multi-state output objects")
		}
		if kind == kindMultiState {
			for _, list := range [][]uint32{cfg.Events.AlarmValues, cfg.Events.FaultValues} {
				for _, v := range list {
					if v < 1 || v > cfg.NumberOfStates {
						return nil, errorf("alarm or fault value %d out of range 1..%d", v, cfg.NumberOfStates)
					}
				}
			}
		}
		if cfg.Events.NotifyType > NotifyEvent {
			return nil, errorf("notify type %d", cfg.Events.NotifyType)
		}
		if cfg.Events.Deadband < 0 {
			return nil, errorf("deadband must be >= 0")
		}
		p.defineEvents(*cfg.Events)
	}
	return p, nil
}

func boolEnum(b bool) uint32 {
	if b {
		return 1
	}
	return 0
}

// checkValue validates a Present_Value (or Relinquish_Default) of the
// point's family.
func (p *Point) checkValue(v bacnet.ApplicationValue) error {
	switch p.kind {
	case kindAnalog:
		if v.Kind != bacnet.ValueReal {
			return ErrInvalidDataType
		}
		if math.IsNaN(float64(v.Real)) || math.IsInf(float64(v.Real), 0) {
			return ErrValueOutOfRange
		}
	case kindBinary:
		if v.Kind != bacnet.ValueEnumerated {
			return ErrInvalidDataType
		}
		if v.Enumerated > 1 {
			return ErrValueOutOfRange
		}
	case kindMultiState:
		if v.Kind != bacnet.ValueUnsigned {
			return ErrInvalidDataType
		}
		if v.Unsigned < 1 || v.Unsigned > uint64(p.states) {
			return ErrValueOutOfRange
		}
	}
	return nil
}

// writePresentValue handles writes to a non-commandable Present_Value. An
// input's Present_Value is writable only while Out_Of_Service is TRUE
// (clause 12.2 and the corresponding input clauses).
func (p *Point) writePresentValue(w Write) ([]bacnet.PropertyIdentifier, error) {
	if w.Property.ArrayIndex != nil {
		return nil, ErrNotAnArray
	}
	if p.input && !p.outOfService {
		return nil, ErrWriteAccessDenied
	}
	if err := p.checkValue(w.Value); err != nil {
		return nil, err
	}
	if sameValue(p.pv, w.Value) {
		return nil, nil
	}
	p.pv = w.Value.Clone()
	return []bacnet.PropertyIdentifier{bacnet.PropertyPresentValue}, nil
}

func (p *Point) statusFlags() bacnet.ApplicationValue {
	return statusFlags(p.eventState != EventStateNormal, p.currentReliability() != ReliabilityNoFaultDetected, false, p.outOfService)
}

// currentReliability is Reliability: the value the application set, or,
// while that is no-fault-detected, MULTI_STATE_FAULT when Present_Value is
// one of the Fault_Values (FAULT_STATE, clause 13.4.5).
func (p *Point) currentReliability() uint32 {
	if p.reliability == ReliabilityNoFaultDetected && p.kind == kindMultiState && p.intrinsic != nil &&
		hasState(p.intrinsic.cfg.FaultValues, p.present().Unsigned) {
		return ReliabilityMultiStateFault
	}
	return p.reliability
}

// PresentValue returns the current Present_Value.
func (p *Point) PresentValue() bacnet.ApplicationValue {
	var v bacnet.ApplicationValue
	p.View(func() { v = p.present() })
	return v
}

func (p *Point) present() bacnet.ApplicationValue {
	if p.pa != nil {
		return p.pa.present()
	}
	return p.pv
}

// SetPresentValue sets Present_Value from the application (a new reading of
// an input, for example). For a commandable object it sets
// Relinquish_Default; while an input is out of service the reading is
// ignored, as the standard decouples Present_Value from the input then.
func (p *Point) SetPresentValue(v bacnet.ApplicationValue) error {
	if err := p.checkValue(v); err != nil {
		return err
	}
	var err error
	p.Update(func() []bacnet.PropertyIdentifier {
		before := p.present()
		switch {
		case p.pa != nil:
			p.pa.relinquish = v.Clone()
		case p.input && p.outOfService:
			return nil
		default:
			p.pv = v.Clone()
		}
		if sameValue(before, p.present()) {
			return nil
		}
		return []bacnet.PropertyIdentifier{bacnet.PropertyPresentValue}
	})
	return err
}

// SetReliability sets Reliability; anything but no-fault-detected sets the
// fault flag of Status_Flags.
func (p *Point) SetReliability(r uint32) {
	p.Update(func() []bacnet.PropertyIdentifier {
		if p.reliability == r {
			return nil
		}
		p.reliability = r
		return []bacnet.PropertyIdentifier{bacnet.PropertyReliability, bacnet.PropertyStatusFlags}
	})
}

// COVValues implements COVReporter: Present_Value and Status_Flags
// (clause 13.1).
func (p *Point) COVValues(context.Context) []PropertyValue {
	var out []PropertyValue
	p.View(func() {
		out = []PropertyValue{
			{Property: bacnet.PropertyPresentValue, Value: p.present()},
			{Property: bacnet.PropertyStatusFlags, Value: p.statusFlags()},
		}
	})
	return out
}

// COVIncrement implements COVReporter.
func (p *Point) COVIncrement() (float32, bool) {
	if p.kind != kindAnalog {
		return 0, false
	}
	var inc float32
	p.View(func() { inc = p.covIncrement })
	return inc, true
}
