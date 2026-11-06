// SPDX-License-Identifier: MIT

package object

import (
	"context"
	"math"
	"testing"

	"github.com/otfabric/go-bacnet"
)

func TestValueObjects(t *testing.T) {
	ctx := context.Background()
	pvRef := prop(bacnet.PropertyPresentValue)
	cases := []struct {
		name     string
		make     func(ValueConfig) (*Value, error)
		cfg      ValueConfig
		typ      bacnet.ObjectType
		initial  bacnet.ApplicationValue
		next     bacnet.ApplicationValue
		wrong    bacnet.ApplicationValue
		tooLarge *bacnet.ApplicationValue
		units    bool
	}{
		{"characterstring", NewCharacterStringValue, ValueConfig{Instance: 1, Name: "csv", Text: "idle", Description: "d"},
			bacnet.ObjectTypeCharacterStringValue, CharString("idle"), CharString("running"), bacnet.RealValue(1), nil, false},
		{"integer", NewIntegerValue, ValueConfig{Instance: 2, Name: "iv", Integer: -5, Units: 95},
			bacnet.ObjectTypeIntegerValue, bacnet.SignedValue(-5), bacnet.SignedValue(math.MinInt32), bacnet.UnsignedValue(1), ptr(bacnet.SignedValue(math.MaxInt32 + 1)), true},
		{"positive integer", NewPositiveIntegerValue, ValueConfig{Instance: 3, Name: "piv", Unsigned: 7, Units: 95},
			bacnet.ObjectTypePositiveIntegerValue, bacnet.UnsignedValue(7), bacnet.UnsignedValue(math.MaxUint32), bacnet.SignedValue(1), ptr(bacnet.UnsignedValue(math.MaxUint32 + 1)), true},
	}
	for _, c := range cases {
		v, err := c.make(c.cfg)
		if err != nil {
			t.Fatal(err)
		}
		if v.Identifier().Type != c.typ {
			t.Fatalf("%s: type %v", c.name, v.Identifier().Type)
		}
		var changes int
		v.SetNotifier(func(Change) { changes++ })
		if got, _ := v.ReadProperty(ctx, pvRef); encoded(t, got) != encoded(t, c.initial) {
			t.Fatalf("%s: initial %+v", c.name, got)
		}
		if err := v.WriteProperty(ctx, Write{Property: pvRef, Value: c.next}); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if err := v.WriteProperty(ctx, Write{Property: pvRef, Value: c.next}); err != nil {
			t.Fatalf("%s: rewrite %v", c.name, err)
		}
		if changes != 1 || encoded(t, v.PresentValue()) != encoded(t, c.next) {
			t.Fatalf("%s: %d changes, %+v", c.name, changes, v.PresentValue())
		}
		wantErr(t, c.name+" wrong type", v.WriteProperty(ctx, Write{Property: pvRef, Value: c.wrong}), ErrInvalidDataType)
		wantErr(t, c.name+" index", v.WriteProperty(ctx, Write{Property: propAt(bacnet.PropertyPresentValue, 1), Value: c.next}), ErrNotAnArray)
		if c.tooLarge != nil {
			wantErr(t, c.name+" range", v.WriteProperty(ctx, Write{Property: pvRef, Value: *c.tooLarge}), ErrValueOutOfRange)
			if err := v.SetPresentValue(*c.tooLarge); err == nil {
				t.Fatalf("%s: application value out of range accepted", c.name)
			}
		}
		if err := v.SetPresentValue(c.initial); err != nil || encoded(t, v.PresentValue()) != encoded(t, c.initial) {
			t.Fatalf("%s: SetPresentValue %v", c.name, err)
		}
		_ = v.SetPresentValue(c.initial)
		if changes != 2 {
			t.Fatalf("%s: %d changes", c.name, changes)
		}
		// Out_Of_Service sets the flag; COV carries Present_Value and Status_Flags.
		if err := v.WriteProperty(ctx, Write{Property: prop(bacnet.PropertyOutOfService), Value: bacnet.BoolValue(true)}); err != nil {
			t.Fatal(err)
		}
		_ = v.WriteProperty(ctx, Write{Property: prop(bacnet.PropertyOutOfService), Value: bacnet.BoolValue(true)})
		wantErr(t, c.name+" oos type", v.WriteProperty(ctx, Write{Property: prop(bacnet.PropertyOutOfService), Value: c.next}), ErrInvalidDataType)
		wantErr(t, c.name+" oos index", v.WriteProperty(ctx, Write{Property: propAt(bacnet.PropertyOutOfService, 1), Value: bacnet.BoolValue(true)}), ErrNotAnArray)
		cov := v.COVValues(ctx)
		if len(cov) != 2 || cov[0].Property != bacnet.PropertyPresentValue || !bit(cov[1].Value, 3) {
			t.Fatalf("%s: COV %+v", c.name, cov)
		}
		if _, analog := v.COVIncrement(); analog {
			t.Fatalf("%s: COV increment", c.name)
		}
		_, req := v.Properties()
		if !req[bacnet.PropertyPresentValue] || !req[bacnet.PropertyStatusFlags] || req[bacnet.PropertyUnits] != c.units || req[bacnet.PropertyOutOfService] {
			t.Fatalf("%s: required %v", c.name, req)
		}
		for _, id := range []bacnet.PropertyIdentifier{bacnet.PropertyEventState, bacnet.PropertyReliability} {
			if x, err := v.ReadProperty(ctx, prop(id)); err != nil || x.Enumerated != 0 {
				t.Fatalf("%s: %v %v", c.name, id, err)
			}
		}
	}
}

func ptr(v bacnet.ApplicationValue) *bacnet.ApplicationValue { return &v }

func TestValueCommandable(t *testing.T) {
	ctx := context.Background()
	v, err := NewIntegerValue(ValueConfig{Instance: 1, Name: "iv", Integer: 3, Commandable: true})
	if err != nil {
		t.Fatal(err)
	}
	pvRef := prop(bacnet.PropertyPresentValue)
	if err := v.WriteProperty(ctx, Write{Property: pvRef, Value: bacnet.SignedValue(10), Priority: 8}); err != nil {
		t.Fatal(err)
	}
	if p, _ := v.ReadProperty(ctx, prop(bacnet.PropertyCurrentCommandPriority)); p.Unsigned != 8 || v.PresentValue().Signed != 10 {
		t.Fatalf("commanded: %+v %+v", p, v.PresentValue())
	}
	wantErr(t, "priority 17", v.WriteProperty(ctx, Write{Property: pvRef, Value: bacnet.SignedValue(1), Priority: 17}), ErrValueOutOfRange)
	wantErr(t, "range", v.WriteProperty(ctx, Write{Property: pvRef, Value: bacnet.SignedValue(1 << 40), Priority: 1}), ErrValueOutOfRange)
	wantErr(t, "relinquish default type", v.WriteProperty(ctx, Write{Property: prop(bacnet.PropertyRelinquishDefault), Value: bacnet.RealValue(1)}), ErrInvalidDataType)
	// The application sets Relinquish_Default.
	if err := v.SetPresentValue(bacnet.SignedValue(4)); err != nil || v.PresentValue().Signed != 10 {
		t.Fatalf("set while commanded: %v", err)
	}
	if err := v.WriteProperty(ctx, Write{Property: pvRef, Value: bacnet.NullValue(), Priority: 8}); err != nil || v.PresentValue().Signed != 4 {
		t.Fatalf("relinquished: %v %+v", err, v.PresentValue())
	}
	if rd, _ := v.ReadProperty(ctx, prop(bacnet.PropertyRelinquishDefault)); rd.Signed != 4 {
		t.Fatalf("Relinquish_Default %+v", rd)
	}
	if cov := v.COVValues(ctx); cov[0].Value.Signed != 4 {
		t.Fatalf("COV %+v", cov)
	}
	for _, cfg := range []ValueConfig{{Instance: bacnet.MaxObjectInstance, Name: "x"}, {}} {
		if _, err := NewCharacterStringValue(cfg); err == nil {
			t.Fatalf("config %+v accepted", cfg)
		}
	}
}
