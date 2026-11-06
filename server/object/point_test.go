// SPDX-License-Identifier: MIT

package object

import (
	"context"
	"math"
	"testing"

	"github.com/otfabric/go-bacnet"
)

func TestPointConfigErrors(t *testing.T) {
	nan := float32(math.NaN())
	cases := []struct {
		name string
		make func(PointConfig) (*Point, error)
		cfg  PointConfig
	}{
		{"instance", NewAnalogInput, PointConfig{Instance: bacnet.MaxObjectInstance, Name: "x"}},
		{"no name", NewAnalogInput, PointConfig{Instance: 1}},
		{"negative COV increment", NewAnalogValue, PointConfig{Instance: 1, Name: "x", COVIncrement: -1}},
		{"NaN COV increment", NewAnalogValue, PointConfig{Instance: 1, Name: "x", COVIncrement: nan}},
		{"polarity", NewBinaryInput, PointConfig{Instance: 1, Name: "x", Polarity: 2}},
		{"no states", NewMultiStateValue, PointConfig{Instance: 1, Name: "x"}},
		{"state text", NewMultiStateValue, PointConfig{Instance: 1, Name: "x", NumberOfStates: 3, StateText: []string{"a", "b"}}},
		{"state", NewMultiStateValue, PointConfig{Instance: 1, Name: "x", NumberOfStates: 3, State: 4}},
		{"multi-state output events", NewMultiStateOutput, PointConfig{Instance: 1, Name: "x", NumberOfStates: 3, Events: &EventConfig{}}},
		{"alarm value", NewMultiStateInput, PointConfig{Instance: 1, Name: "x", NumberOfStates: 3, Events: &EventConfig{AlarmValues: []uint32{4}}}},
		{"fault value", NewMultiStateInput, PointConfig{Instance: 1, Name: "x", NumberOfStates: 3, Events: &EventConfig{FaultValues: []uint32{0}}}},
		{"notify type", NewAnalogInput, PointConfig{Instance: 1, Name: "x", Events: &EventConfig{NotifyType: NotifyEvent + 1}}},
		{"deadband", NewAnalogInput, PointConfig{Instance: 1, Name: "x", Events: &EventConfig{Deadband: -1}}},
	}
	for _, c := range cases {
		if p, err := c.make(c.cfg); err == nil || p != nil {
			t.Errorf("%s: accepted (%v)", c.name, err)
		}
	}
}

// TestPointFamilies checks the type, the Present_Value datatype and range
// of each family (clauses 12.2-12.20) through SetPresentValue and
// WriteProperty.
func TestPointFamilies(t *testing.T) {
	ctx := context.Background()
	inf := float32(math.Inf(1))
	cases := []struct {
		name        string
		make        func(PointConfig) (*Point, error)
		cfg         PointConfig
		typ         bacnet.ObjectType
		initial     bacnet.ApplicationValue
		next        bacnet.ApplicationValue
		bad         []bacnet.ApplicationValue // out of range
		wrongKind   bacnet.ApplicationValue
		commandable bool
	}{
		{"AI", NewAnalogInput, PointConfig{Instance: 1, Name: "ai", Value: 2.5}, bacnet.ObjectTypeAnalogInput,
			bacnet.RealValue(2.5), bacnet.RealValue(3), []bacnet.ApplicationValue{bacnet.RealValue(float32(math.NaN())), bacnet.RealValue(inf)}, bacnet.EnumValue(1), false},
		{"AO", NewAnalogOutput, PointConfig{Instance: 2, Name: "ao"}, bacnet.ObjectTypeAnalogOutput,
			bacnet.RealValue(0), bacnet.RealValue(7), []bacnet.ApplicationValue{bacnet.RealValue(-inf)}, bacnet.UnsignedValue(1), true},
		{"BI", NewBinaryInput, PointConfig{Instance: 3, Name: "bi", Active: true}, bacnet.ObjectTypeBinaryInput,
			bacnet.EnumValue(1), bacnet.EnumValue(0), []bacnet.ApplicationValue{bacnet.EnumValue(2)}, bacnet.RealValue(1), false},
		{"BO", NewBinaryOutput, PointConfig{Instance: 4, Name: "bo"}, bacnet.ObjectTypeBinaryOutput,
			bacnet.EnumValue(0), bacnet.EnumValue(1), []bacnet.ApplicationValue{bacnet.EnumValue(5)}, bacnet.BoolValue(true), true},
		{"BV", NewBinaryValue, PointConfig{Instance: 5, Name: "bv"}, bacnet.ObjectTypeBinaryValue,
			bacnet.EnumValue(0), bacnet.EnumValue(1), []bacnet.ApplicationValue{bacnet.EnumValue(2)}, bacnet.UnsignedValue(1), false},
		{"MSI", NewMultiStateInput, PointConfig{Instance: 6, Name: "msi", NumberOfStates: 3}, bacnet.ObjectTypeMultiStateInput,
			bacnet.UnsignedValue(1), bacnet.UnsignedValue(3), []bacnet.ApplicationValue{bacnet.UnsignedValue(0), bacnet.UnsignedValue(4)}, bacnet.EnumValue(1), false},
		{"MSO", NewMultiStateOutput, PointConfig{Instance: 7, Name: "mso", NumberOfStates: 2, State: 2}, bacnet.ObjectTypeMultiStateOutput,
			bacnet.UnsignedValue(2), bacnet.UnsignedValue(1), []bacnet.ApplicationValue{bacnet.UnsignedValue(3)}, bacnet.SignedValue(1), true},
		{"MSV", NewMultiStateValue, PointConfig{Instance: 8, Name: "msv", NumberOfStates: 2}, bacnet.ObjectTypeMultiStateValue,
			bacnet.UnsignedValue(1), bacnet.UnsignedValue(2), []bacnet.ApplicationValue{bacnet.UnsignedValue(0)}, bacnet.RealValue(1), false},
	}
	for _, c := range cases {
		p, err := c.make(c.cfg)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if p.Identifier().Type != c.typ {
			t.Fatalf("%s: type %v", c.name, p.Identifier().Type)
		}
		if encoded(t, p.PresentValue()) != encoded(t, c.initial) {
			t.Fatalf("%s: initial %+v", c.name, p.PresentValue())
		}
		_, err = p.ReadProperty(ctx, prop(bacnet.PropertyPriorityArray))
		if commandable := err == nil; commandable != c.commandable {
			t.Fatalf("%s: commandable %v", c.name, commandable)
		}
		for _, v := range c.bad {
			wantErr(t, c.name+" SetPresentValue range", p.SetPresentValue(v), ErrValueOutOfRange)
		}
		wantErr(t, c.name+" SetPresentValue kind", p.SetPresentValue(c.wrongKind), ErrInvalidDataType)
		// Inputs accept a written Present_Value only while out of service.
		if p.input {
			wantErr(t, c.name+" write in service", p.WriteProperty(ctx, Write{Property: prop(bacnet.PropertyPresentValue), Value: c.next}), ErrWriteAccessDenied)
			if err := p.WriteProperty(ctx, Write{Property: prop(bacnet.PropertyOutOfService), Value: bacnet.BoolValue(true)}); err != nil {
				t.Fatal(err)
			}
		}
		wantErr(t, c.name+" write range", p.WriteProperty(ctx, Write{Property: prop(bacnet.PropertyPresentValue), Value: c.bad[0], Priority: 1}), ErrValueOutOfRange)
		wantErr(t, c.name+" write kind", p.WriteProperty(ctx, Write{Property: prop(bacnet.PropertyPresentValue), Value: c.wrongKind, Priority: 1}), ErrInvalidDataType)
		wantErr(t, c.name+" write index", p.WriteProperty(ctx, Write{Property: propAt(bacnet.PropertyPresentValue, 1), Value: c.next}), ErrNotAnArray)
		if err := p.WriteProperty(ctx, Write{Property: prop(bacnet.PropertyPresentValue), Value: c.next}); err != nil {
			t.Fatalf("%s: write %v", c.name, err)
		}
		if encoded(t, p.PresentValue()) != encoded(t, c.next) {
			t.Fatalf("%s: after write %+v", c.name, p.PresentValue())
		}
		if inc, ok := p.COVIncrement(); ok != (p.kind == kindAnalog) || inc != 0 {
			t.Fatalf("%s: COVIncrement %v %v", c.name, inc, ok)
		}
	}
}

func TestPointNonCommandableWrites(t *testing.T) {
	ctx := context.Background()
	p, err := NewAnalogValue(PointConfig{Instance: 1, Name: "av", Value: 1, Description: "supply", Units: 62})
	if err != nil {
		t.Fatal(err)
	}
	var changes []Change
	p.SetNotifier(func(c Change) { changes = append(changes, c) })
	pv := prop(bacnet.PropertyPresentValue)
	if err := p.WriteProperty(ctx, Write{Property: pv, Value: bacnet.RealValue(1)}); err != nil || len(changes) != 0 {
		t.Fatalf("same value: %v, %d changes", err, len(changes))
	}
	if err := p.WriteProperty(ctx, Write{Property: pv, Value: bacnet.RealValue(4)}); err != nil || len(changes) != 1 || changes[0].Properties[0] != bacnet.PropertyPresentValue {
		t.Fatalf("write: %v, %+v", err, changes)
	}
	// The application's SetPresentValue notifies only a change.
	if err := p.SetPresentValue(bacnet.RealValue(4)); err != nil || len(changes) != 1 {
		t.Fatalf("same application value: %v, %d", err, len(changes))
	}
	if err := p.SetPresentValue(bacnet.RealValue(5)); err != nil || len(changes) != 2 || p.PresentValue().Real != 5 {
		t.Fatalf("application value: %v, %d, %+v", err, len(changes), p.PresentValue())
	}
	// Description is a writable CharacterString.
	if d, err := p.ReadProperty(ctx, prop(bacnet.PropertyDescription)); err != nil || d.Character.Value != "supply" {
		t.Fatalf("Description %+v %v", d, err)
	}
	if err := p.WriteProperty(ctx, Write{Property: prop(bacnet.PropertyDescription), Value: CharString("return")}); err != nil {
		t.Fatal(err)
	}
	wantErr(t, "description kind", p.WriteProperty(ctx, Write{Property: prop(bacnet.PropertyDescription), Value: bacnet.RealValue(1)}), ErrInvalidDataType)
	if u, err := p.ReadProperty(ctx, prop(bacnet.PropertyUnits)); err != nil || u.Enumerated != 62 {
		t.Fatalf("Units %+v %v", u, err)
	}
	wantErr(t, "units read-only", p.WriteProperty(ctx, Write{Property: prop(bacnet.PropertyUnits), Value: bacnet.EnumValue(1)}), ErrWriteAccessDenied)
}

func TestPointOutOfService(t *testing.T) {
	ctx := context.Background()
	p, err := NewBinaryInput(PointConfig{Instance: 1, Name: "bi"})
	if err != nil {
		t.Fatal(err)
	}
	var changes []Change
	p.SetNotifier(func(c Change) { changes = append(changes, c) })
	oos := prop(bacnet.PropertyOutOfService)
	wantErr(t, "index", p.WriteProperty(ctx, Write{Property: propAt(bacnet.PropertyOutOfService, 1), Value: bacnet.BoolValue(true)}), ErrNotAnArray)
	wantErr(t, "kind", p.WriteProperty(ctx, Write{Property: oos, Value: bacnet.EnumValue(1)}), ErrInvalidDataType)
	if err := p.WriteProperty(ctx, Write{Property: oos, Value: bacnet.BoolValue(false)}); err != nil || len(changes) != 0 {
		t.Fatalf("unchanged: %v, %d", err, len(changes))
	}
	if err := p.WriteProperty(ctx, Write{Property: oos, Value: bacnet.BoolValue(true)}); err != nil || len(changes) != 1 {
		t.Fatalf("set: %v, %d", err, len(changes))
	}
	got := changes[0].Properties
	if len(got) != 2 || got[0] != bacnet.PropertyOutOfService || got[1] != bacnet.PropertyStatusFlags {
		t.Fatalf("changed %v", got)
	}
	// While out of service a reading of the input is ignored.
	if err := p.SetPresentValue(bacnet.EnumValue(1)); err != nil || p.PresentValue().Enumerated != 0 || len(changes) != 1 {
		t.Fatalf("reading while out of service: %v, %+v, %d", err, p.PresentValue(), len(changes))
	}
	// A written Present_Value is accepted then.
	if err := p.WriteProperty(ctx, Write{Property: prop(bacnet.PropertyPresentValue), Value: bacnet.EnumValue(1)}); err != nil || p.PresentValue().Enumerated != 1 {
		t.Fatalf("write while out of service: %v, %+v", err, p.PresentValue())
	}
	cov := p.COVValues(ctx)
	if len(cov) != 2 || cov[0].Property != bacnet.PropertyPresentValue || cov[0].Value.Enumerated != 1 ||
		cov[1].Property != bacnet.PropertyStatusFlags || !bit(cov[1].Value, 3) || bit(cov[1].Value, 1) {
		t.Fatalf("COV %+v", cov)
	}
	if pol, err := p.ReadProperty(ctx, prop(bacnet.PropertyPolarity)); err != nil || pol.Enumerated != 0 {
		t.Fatalf("Polarity %+v %v", pol, err)
	}
}

func TestPointReliability(t *testing.T) {
	ctx := context.Background()
	p, err := NewAnalogInput(PointConfig{Instance: 1, Name: "ai"})
	if err != nil {
		t.Fatal(err)
	}
	var n int
	p.SetNotifier(func(Change) { n++ })
	p.SetReliability(ReliabilityNoFaultDetected)
	if n != 0 {
		t.Fatalf("unchanged reliability notified")
	}
	const overRange = 2 // BACnetReliability over-range
	p.SetReliability(overRange)
	p.SetReliability(overRange)
	if n != 1 {
		t.Fatalf("%d notifications", n)
	}
	if r, err := p.ReadProperty(ctx, prop(bacnet.PropertyReliability)); err != nil || r.Enumerated != overRange {
		t.Fatalf("Reliability %+v %v", r, err)
	}
	if sf, err := p.ReadProperty(ctx, prop(bacnet.PropertyStatusFlags)); err != nil || !bit(sf, 1) || bit(sf, 0) || bit(sf, 3) {
		t.Fatalf("Status_Flags %+v %v", sf, err)
	}
}

func TestPointCOVIncrement(t *testing.T) {
	ctx := context.Background()
	p, err := NewAnalogInput(PointConfig{Instance: 1, Name: "ai", COVIncrement: 0.5})
	if err != nil {
		t.Fatal(err)
	}
	if inc, ok := p.COVIncrement(); !ok || inc != 0.5 {
		t.Fatalf("COVIncrement %v %v", inc, ok)
	}
	ref := prop(bacnet.PropertyCOVIncrement)
	wantErr(t, "index", p.WriteProperty(ctx, Write{Property: propAt(bacnet.PropertyCOVIncrement, 1), Value: bacnet.RealValue(1)}), ErrNotAnArray)
	wantErr(t, "kind", p.WriteProperty(ctx, Write{Property: ref, Value: bacnet.UnsignedValue(1)}), ErrInvalidDataType)
	wantErr(t, "negative", p.WriteProperty(ctx, Write{Property: ref, Value: bacnet.RealValue(-1)}), ErrValueOutOfRange)
	wantErr(t, "NaN", p.WriteProperty(ctx, Write{Property: ref, Value: bacnet.RealValue(float32(math.NaN()))}), ErrValueOutOfRange)
	var changes []Change
	p.SetNotifier(func(c Change) { changes = append(changes, c) })
	if err := p.WriteProperty(ctx, Write{Property: ref, Value: bacnet.RealValue(2)}); err != nil {
		t.Fatal(err)
	}
	if inc, _ := p.COVIncrement(); inc != 2 || len(changes) != 1 || changes[0].Properties[0] != bacnet.PropertyCOVIncrement {
		t.Fatalf("after write %v, %+v", inc, changes)
	}
	if v, err := p.ReadProperty(ctx, ref); err != nil || v.Real != 2 {
		t.Fatalf("read %+v %v", v, err)
	}
}

// TestPointRelinquishDefault checks Relinquish_Default of a commandable
// object (clause 19.2): Present_Value follows it only while every slot of
// the priority array is NULL.
func TestPointRelinquishDefault(t *testing.T) {
	ctx := context.Background()
	p, err := NewAnalogOutput(PointConfig{Instance: 1, Name: "ao", Value: 1})
	if err != nil {
		t.Fatal(err)
	}
	var changes []Change
	p.SetNotifier(func(c Change) { changes = append(changes, c) })
	rd := prop(bacnet.PropertyRelinquishDefault)
	wantErr(t, "index", p.WriteProperty(ctx, Write{Property: propAt(bacnet.PropertyRelinquishDefault, 1), Value: bacnet.RealValue(2)}), ErrNotAnArray)
	wantErr(t, "range", p.WriteProperty(ctx, Write{Property: rd, Value: bacnet.RealValue(float32(math.Inf(-1)))}), ErrValueOutOfRange)
	if err := p.WriteProperty(ctx, Write{Property: rd, Value: bacnet.RealValue(2)}); err != nil {
		t.Fatal(err)
	}
	if p.PresentValue().Real != 2 || len(changes) != 1 || !hasProp(changes[0].Properties, bacnet.PropertyRelinquishDefault) || !hasProp(changes[0].Properties, bacnet.PropertyPresentValue) {
		t.Fatalf("relinquished: %+v, %+v", p.PresentValue(), changes)
	}
	// Commanded, a new Relinquish_Default does not change Present_Value.
	if err := p.WriteProperty(ctx, Write{Property: prop(bacnet.PropertyPresentValue), Value: bacnet.RealValue(9), Priority: 8}); err != nil {
		t.Fatal(err)
	}
	changes = nil
	if err := p.WriteProperty(ctx, Write{Property: rd, Value: bacnet.RealValue(3)}); err != nil {
		t.Fatal(err)
	}
	if p.PresentValue().Real != 9 || len(changes) != 1 || hasProp(changes[0].Properties, bacnet.PropertyPresentValue) {
		t.Fatalf("commanded: %+v, %+v", p.PresentValue(), changes)
	}
	if v, err := p.ReadProperty(ctx, rd); err != nil || v.Real != 3 {
		t.Fatalf("Relinquish_Default %+v %v", v, err)
	}
	// SetPresentValue sets Relinquish_Default of a commandable object.
	if err := p.SetPresentValue(bacnet.RealValue(4)); err != nil || p.PresentValue().Real != 9 {
		t.Fatalf("SetPresentValue while commanded: %v %+v", err, p.PresentValue())
	}
	if v, _ := p.ReadProperty(ctx, rd); v.Real != 4 {
		t.Fatalf("Relinquish_Default %+v", v)
	}
}

func hasProp(ids []bacnet.PropertyIdentifier, id bacnet.PropertyIdentifier) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func TestPointOptionalProperties(t *testing.T) {
	ctx := context.Background()
	bo, err := NewBinaryOutput(PointConfig{Instance: 1, Name: "bo", Polarity: 1, ActiveText: "on", InactiveText: "off"})
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[bacnet.PropertyIdentifier]string{bacnet.PropertyActiveText: "on", bacnet.PropertyInactiveText: "off"} {
		if v, err := bo.ReadProperty(ctx, prop(id)); err != nil || v.Character.Value != want {
			t.Fatalf("%v: %+v %v", id, v, err)
		}
	}
	if pol, _ := bo.ReadProperty(ctx, prop(bacnet.PropertyPolarity)); pol.Enumerated != 1 {
		t.Fatalf("Polarity %+v", pol)
	}
	// A Binary Value has no Polarity (clause 12.8).
	bv, err := NewBinaryValue(PointConfig{Instance: 1, Name: "bv"})
	if err != nil {
		t.Fatal(err)
	}
	wantErr(t, "BV polarity", readErr(bv, bacnet.PropertyPolarity), ErrUnknownProperty)
	wantErr(t, "BV active text", readErr(bv, bacnet.PropertyActiveText), ErrUnknownProperty)
	ms, err := NewMultiStateValue(PointConfig{Instance: 1, Name: "msv", NumberOfStates: 2, StateText: []string{"off", "on"}})
	if err != nil {
		t.Fatal(err)
	}
	if v, err := ms.ReadProperty(ctx, propAt(bacnet.PropertyStateText, 2)); err != nil || v.Character.Value != "on" {
		t.Fatalf("State_Text[2] %+v %v", v, err)
	}
	if v, err := ms.ReadProperty(ctx, prop(bacnet.PropertyNumberOfStates)); err != nil || v.Unsigned != 2 {
		t.Fatalf("Number_Of_States %+v %v", v, err)
	}
}

func readErr(o Object, id bacnet.PropertyIdentifier) error {
	_, err := o.ReadProperty(context.Background(), prop(id))
	return err
}
