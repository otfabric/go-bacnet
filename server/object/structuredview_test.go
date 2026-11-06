// SPDX-License-Identifier: MIT

package object

import (
	"context"
	"testing"

	"github.com/otfabric/go-bacnet"
)

func TestStructuredView(t *testing.T) {
	ctx := context.Background()
	remote := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 20}
	ai := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogInput, Instance: 1}
	bi := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeBinaryInput, Instance: 2}
	notDev := ai
	for i, cfg := range []StructuredViewConfig{
		{Instance: bacnet.MaxObjectInstance, Name: "x"},
		{},
		{Name: "x", NodeType: NodeTypeMax},
		{Name: "x", MaxSubordinates: 1, Subordinates: []DeviceObjectReference{{Object: ai}, {Object: bi}}},
		{Name: "x", Subordinates: []DeviceObjectReference{{Object: ai}}, Annotations: []string{}},
		{Name: "x", Subordinates: []DeviceObjectReference{{Object: ai, Device: &notDev}}},
	} {
		if _, err := NewStructuredView(cfg); err == nil {
			t.Fatalf("config %d accepted", i)
		}
	}
	sv, err := NewStructuredView(StructuredViewConfig{Instance: 1, Name: "floor-1", Description: "d", NodeType: 19, NodeSubtype: "floor",
		Subordinates: []DeviceObjectReference{{Object: ai}, {Object: bi, Device: &remote}}, Annotations: []string{"temperature", "door"}, MaxSubordinates: 3})
	if err != nil {
		t.Fatal(err)
	}
	subs := prop(bacnet.PropertySubordinateList)
	ann := prop(bacnet.PropertySubordinateAnnotations)
	w := func(ref bacnet.PropertyReference, v bacnet.ApplicationValue) error {
		return sv.WriteProperty(ctx, Write{Property: ref, Value: v})
	}

	if v, _ := sv.ReadProperty(ctx, prop(bacnet.PropertyNodeType)); v.Enumerated != 19 {
		t.Fatalf("Node_Type %+v", v)
	}
	if v, _ := sv.ReadProperty(ctx, prop(bacnet.PropertyNodeSubtype)); v.Character.Value != "floor" {
		t.Fatalf("Node_Subtype %+v", v)
	}
	if n, _ := sv.ReadProperty(ctx, propAt(bacnet.PropertySubordinateList, 0)); n.Unsigned != 2 {
		t.Fatalf("length %+v", n)
	}
	second, err := sv.ReadProperty(ctx, propAt(bacnet.PropertySubordinateList, 2))
	if err != nil {
		t.Fatal(err)
	}
	// Round trips in wire form: the whole list and one element.
	whole, _ := sv.ReadProperty(ctx, subs)
	if err := w(subs, onWire(t, whole)); err != nil {
		t.Fatal(err)
	}
	if again, _ := sv.ReadProperty(ctx, subs); encoded(t, again) != encoded(t, whole) {
		t.Fatal("Subordinate_List round trip")
	}
	if err := w(propAt(bacnet.PropertySubordinateList, 1), onWire(t, second)); err != nil {
		t.Fatal(err)
	}
	if got := sv.Subordinates(); got[0].Device == nil || *got[0].Device != remote || got[0].Object != bi {
		t.Fatalf("element write: %+v", got)
	}
	// Grow the list: the annotations follow.
	grown := bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{
		subordinateElement(DeviceObjectReference{Object: ai}), subordinateElement(DeviceObjectReference{Object: bi}), subordinateElement(DeviceObjectReference{Object: ai, Device: &remote})}}
	if err := w(subs, grown); err != nil {
		t.Fatal(err)
	}
	if a := sv.Annotations(); len(a) != 3 || a[0] != "temperature" || a[2] != "" {
		t.Fatalf("annotations after growth: %q", a)
	}
	// Annotations, whole and by element.
	if err := w(ann, onWire(t, bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{
		{Value: CharString("a")}, {Value: CharString("b")}, {Value: CharString("c")}}})); err != nil {
		t.Fatal(err)
	}
	if err := w(propAt(bacnet.PropertySubordinateAnnotations, 3), CharString("z")); err != nil {
		t.Fatal(err)
	}
	if a := sv.Annotations(); a[0] != "a" || a[2] != "z" {
		t.Fatalf("annotations: %q", a)
	}
	if v, _ := sv.ReadProperty(ctx, propAt(bacnet.PropertySubordinateAnnotations, 1)); v.Character.Value != "a" {
		t.Fatalf("annotation 1: %+v", v)
	}
	if err := w(prop(bacnet.PropertyNodeType), bacnet.EnumValue(2)); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		err  error
		want error
	}{
		{"node type kind", w(prop(bacnet.PropertyNodeType), bacnet.UnsignedValue(2)), ErrInvalidDataType},
		{"node type range", w(prop(bacnet.PropertyNodeType), bacnet.EnumValue(NodeTypeMax)), ErrValueOutOfRange},
		{"node type index", w(propAt(bacnet.PropertyNodeType, 1), bacnet.EnumValue(1)), ErrNotAnArray},
		{"subordinates primitive", w(subs, bacnet.ObjectIDValue(ai)), ErrInvalidDataType},
		{"subordinate index 0", w(propAt(bacnet.PropertySubordinateList, 0), second), ErrWriteAccessDenied},
		{"subordinate index 4", w(propAt(bacnet.PropertySubordinateList, 4), second), ErrInvalidArrayIndex},
		{"two at one index", w(propAt(bacnet.PropertySubordinateList, 1), grown), ErrInvalidDataType},
		{"too many", w(subs, bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: append(append([]bacnet.Element(nil), grown.Elements...), grown.Elements[0])}), ErrNoSpaceToWriteProperty},
		{"device not a device", w(subs, bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{
			{Context: true, TagNumber: 0, Value: bacnet.ObjectIDValue(ai)}, {Context: true, TagNumber: 1, Value: bacnet.ObjectIDValue(ai)}}}), ErrInvalidDataType},
		{"missing object", w(subs, bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{
			{Context: true, TagNumber: 0, Value: bacnet.ObjectIDValue(remote)}}}), ErrInvalidDataType},
		{"bad object", w(subs, bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{
			{Context: true, TagNumber: 1, Value: bacnet.UnsignedValue(1)}}}), ErrInvalidDataType},
		{"grouped extra field", w(subs, bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{{Value: bacnet.ApplicationValue{
			Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{{Context: true, TagNumber: 1, Value: bacnet.ObjectIDValue(ai)}, {Context: true, TagNumber: 2}}}}}}), ErrInvalidDataType},
		{"annotations count", w(ann, CharString("one")), ErrValueOutOfRange},
		{"annotations kind", w(ann, bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{
			{Value: CharString("a")}, {Value: bacnet.RealValue(1)}, {Value: CharString("c")}}}), ErrInvalidDataType},
		{"annotation kind", w(propAt(bacnet.PropertySubordinateAnnotations, 1), bacnet.RealValue(1)), ErrInvalidDataType},
		{"annotation index", w(propAt(bacnet.PropertySubordinateAnnotations, 9), CharString("x")), ErrInvalidArrayIndex},
	}
	for _, c := range cases {
		wantErr(t, c.name, c.err, c.want)
	}
	// Shrinking the list drops annotations.
	if err := w(subs, onWire(t, bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: grown.Elements[:1]})); err != nil {
		t.Fatal(err)
	}
	if a := sv.Annotations(); len(a) != 1 || a[0] != "a" {
		t.Fatalf("annotations after shrink: %q", a)
	}
	_, req := sv.Properties()
	if !req[bacnet.PropertyNodeType] || !req[bacnet.PropertySubordinateList] || req[bacnet.PropertySubordinateAnnotations] {
		t.Fatalf("required %v", req)
	}
	plain, _ := NewStructuredView(StructuredViewConfig{Name: "plain"})
	if plain.Annotations() != nil {
		t.Fatal("annotations without the property")
	}
	if _, err := plain.ReadProperty(ctx, ann); err == nil {
		t.Fatal("Subordinate_Annotations present")
	}
}

// TestReadEveryProperty reads every property the new objects list.
func TestReadEveryProperty(t *testing.T) {
	ctx := context.Background()
	r := newScheduleRig(t)
	csv, _ := NewCharacterStringValue(ValueConfig{Name: "csv", Commandable: true})
	sv, _ := NewStructuredView(StructuredViewConfig{Name: "sv", NodeSubtype: "x", Annotations: []string{}})
	for _, o := range []Object{r.cal, r.sch, csv, sv} {
		all, _ := o.Properties()
		for _, id := range all {
			v, err := o.ReadProperty(ctx, prop(id))
			if err != nil {
				t.Fatalf("%v %v: %v", o.Identifier(), id, err)
			}
			if _, err := bacnet.AppendApplicationValue(nil, v); err != nil {
				t.Fatalf("%v %v not encodable: %v", o.Identifier(), id, err)
			}
		}
	}
	if _, ok := ctxUnsigned(bacnet.Element{Value: bacnet.UnsignedValue(1)}); ok {
		t.Fatal("application unsigned taken as context")
	}
	if _, ok := ctxObjectID(bacnet.Element{Value: bacnet.ObjectIDValue(sv.Identifier())}); ok {
		t.Fatal("application object identifier taken as context")
	}
	wantErr(t, "constructed time value", r.sch.checkValue(bacnet.ApplicationValue{Kind: bacnet.ValueConstructed}), ErrInvalidDataType)
}
