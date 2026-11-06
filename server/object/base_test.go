// SPDX-License-Identifier: MIT

package object

import (
	"context"
	"testing"

	"github.com/otfabric/go-bacnet"
)

// testBase is an application object built on Base: a writable
// CharacterString array, a writable REAL, a read-only value and an array
// whose items are context-tagged.
func testBase(t *testing.T) *Base {
	t.Helper()
	b := NewBase(bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 9}, "app")
	b.Define(bacnet.PropertyStateText, &Property{Array: true, Writable: true, Kinds: []bacnet.ValueKind{bacnet.ValueCharacterString},
		Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{{Value: charString("a")}, {Value: charString("b")}}}})
	b.Define(bacnet.PropertyPresentValue, &Property{Required: true, Writable: true, Kinds: []bacnet.ValueKind{bacnet.ValueReal}, Value: bacnet.RealValue(1)})
	b.Define(bacnet.PropertyUnits, &Property{Required: true, Value: bacnet.EnumValue(62)})
	ctxEl := bacnet.Element{Context: true, TagNumber: 0, Value: bacnet.UnsignedValue(5)}
	b.Define(bacnet.PropertyEventTimeStamps, &Property{Array: true, Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{ctxEl}}})
	// An array property whose value is not a list has length 0.
	b.Define(bacnet.PropertyAlarmValues, &Property{Array: true, Value: bacnet.UnsignedValue(1)})
	return b
}

func TestBaseRead(t *testing.T) {
	ctx := context.Background()
	b := testBase(t)
	wantErr(t, "unknown", readErr(b, bacnet.PropertyLocation), ErrUnknownProperty)
	_, err := b.ReadProperty(ctx, propAt(bacnet.PropertyPresentValue, 1))
	wantErr(t, "index of a scalar", err, ErrNotAnArray)
	if n, err := b.ReadProperty(ctx, propAt(bacnet.PropertyStateText, 0)); err != nil || n.Kind != bacnet.ValueUnsigned || n.Unsigned != 2 {
		t.Fatalf("length %+v %v", n, err)
	}
	if v, err := b.ReadProperty(ctx, propAt(bacnet.PropertyStateText, 2)); err != nil || v.Character.Value != "b" {
		t.Fatalf("item 2 %+v %v", v, err)
	}
	_, err = b.ReadProperty(ctx, propAt(bacnet.PropertyStateText, 3))
	wantErr(t, "index past the end", err, ErrInvalidArrayIndex)
	// A context-tagged item comes back as a one-element constructed value.
	v, err := b.ReadProperty(ctx, propAt(bacnet.PropertyEventTimeStamps, 1))
	if err != nil || v.Kind != bacnet.ValueConstructed || len(v.Elements) != 1 || !v.Elements[0].Context || v.Elements[0].Value.Unsigned != 5 {
		t.Fatalf("context item %+v %v", v, err)
	}
	if n, err := b.ReadProperty(ctx, propAt(bacnet.PropertyAlarmValues, 0)); err != nil || n.Unsigned != 0 {
		t.Fatalf("length of a scalar array value %+v %v", n, err)
	}
	_, err = b.ReadProperty(ctx, propAt(bacnet.PropertyAlarmValues, 1))
	wantErr(t, "item of an empty array", err, ErrInvalidArrayIndex)
	// Property_List leaves out the four properties every object has.
	pl, err := b.ReadProperty(ctx, prop(bacnet.PropertyPropertyList))
	if err != nil || len(pl.Elements) != 5 || pl.Elements[0].Value.Enumerated != uint32(bacnet.PropertyStateText) {
		t.Fatalf("Property_List %+v %v", pl, err)
	}
	all, req := b.Properties()
	if len(all) != 9 || !req[bacnet.PropertyObjectName] || !req[bacnet.PropertyUnits] || req[bacnet.PropertyStateText] {
		t.Fatalf("Properties %v %v", all, req)
	}
	if b.Name() != "app" {
		t.Fatalf("Name %q", b.Name())
	}
}

func TestBaseWrite(t *testing.T) {
	ctx := context.Background()
	b := testBase(t)
	var changes []Change
	b.SetNotifier(func(c Change) { changes = append(changes, c) })
	st := bacnet.PropertyStateText
	cases := []struct {
		name string
		w    Write
		want error
	}{
		{"unknown", Write{Property: prop(bacnet.PropertyLocation), Value: charString("x")}, ErrUnknownProperty},
		{"read-only", Write{Property: prop(bacnet.PropertyUnits), Value: bacnet.EnumValue(1)}, ErrWriteAccessDenied},
		{"index of a scalar", Write{Property: propAt(bacnet.PropertyPresentValue, 1), Value: bacnet.RealValue(2)}, ErrNotAnArray},
		{"index 0", Write{Property: propAt(st, 0), Value: bacnet.UnsignedValue(3)}, ErrInvalidArrayIndex},
		{"index past the end", Write{Property: propAt(st, 3), Value: charString("c")}, ErrInvalidArrayIndex},
		{"item kind", Write{Property: propAt(st, 1), Value: bacnet.RealValue(1)}, ErrInvalidDataType},
		{"array not a list", Write{Property: prop(st), Value: charString("c")}, ErrInvalidDataType},
		{"array context item", Write{Property: prop(st), Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{{Context: true, Value: charString("c")}}}}, ErrInvalidDataType},
		{"array item kind", Write{Property: prop(st), Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{{Value: bacnet.RealValue(1)}}}}, ErrInvalidDataType},
		{"scalar kind", Write{Property: prop(bacnet.PropertyPresentValue), Value: bacnet.EnumValue(1)}, ErrInvalidDataType},
	}
	for _, c := range cases {
		wantErr(t, c.name, b.WriteProperty(ctx, c.w), c.want)
	}
	if len(changes) != 0 {
		t.Fatalf("failed writes notified: %+v", changes)
	}
	if err := b.WriteProperty(ctx, Write{Property: propAt(st, 2), Value: charString("B")}); err != nil {
		t.Fatal(err)
	}
	if v, _ := b.ReadProperty(ctx, propAt(st, 2)); v.Character.Value != "B" {
		t.Fatalf("item 2 %+v", v)
	}
	if v, _ := b.ReadProperty(ctx, propAt(st, 1)); v.Character.Value != "a" {
		t.Fatalf("item 1 %+v", v)
	}
	if err := b.WriteProperty(ctx, Write{Property: prop(st), Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{{Value: charString("x")}}}}); err != nil {
		t.Fatal(err)
	}
	if n, _ := b.ReadProperty(ctx, propAt(st, 0)); n.Unsigned != 1 {
		t.Fatalf("length after whole-array write %+v", n)
	}
	if err := b.WriteProperty(ctx, Write{Property: prop(bacnet.PropertyPresentValue), Value: bacnet.RealValue(7)}); err != nil {
		t.Fatal(err)
	}
	if v, _ := b.ReadProperty(ctx, prop(bacnet.PropertyPresentValue)); v.Real != 7 {
		t.Fatalf("Present_Value %+v", v)
	}
	if len(changes) != 3 || changes[0].Object != b.Identifier() || changes[0].Properties[0] != st || changes[2].Properties[0] != bacnet.PropertyPresentValue {
		t.Fatalf("changes %+v", changes)
	}
	// Update reports only what fn says changed.
	b.Update(func() []bacnet.PropertyIdentifier { return nil })
	if len(changes) != 3 {
		t.Fatalf("empty update notified")
	}
	b.SetNotifier(nil)
	b.Update(func() []bacnet.PropertyIdentifier { return []bacnet.PropertyIdentifier{st} })
	if err := b.WriteProperty(ctx, Write{Property: prop(bacnet.PropertyPresentValue), Value: bacnet.RealValue(8)}); err != nil || len(changes) != 3 {
		t.Fatalf("notified without a notifier: %v", err)
	}
	if b.Deletable() {
		t.Fatal("deletable by default")
	}
	b.SetDeletable(true)
	if !b.Deletable() {
		t.Fatal("SetDeletable")
	}
}

func TestBitStrings(t *testing.T) {
	sf := statusFlags(false, false, true, false)
	if bit(sf, 0) || bit(sf, 1) || !bit(sf, 2) || bit(sf, 3) || sf.BitString.UnusedBits != 4 {
		t.Fatalf("overridden %+v", sf)
	}
	empty := bitString(0, func(int) bool { return true })
	if empty.Kind != bacnet.ValueBitString || empty.BitString.Bytes != nil || empty.BitString.UnusedBits != 0 {
		t.Fatalf("empty %+v", empty)
	}
	nine := bitString(9, func(i int) bool { return i == 8 })
	if len(nine.BitString.Bytes) != 2 || nine.BitString.UnusedBits != 7 || !bit(nine, 8) || bit(nine, 7) {
		t.Fatalf("nine %+v", nine)
	}
	if bit(nine, 16) || bit(bacnet.UnsignedValue(1), 0) {
		t.Fatal("bit out of range or of a non-bit-string")
	}
}
