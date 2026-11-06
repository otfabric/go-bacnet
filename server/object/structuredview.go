// SPDX-License-Identifier: MIT

package object

import "github.com/otfabric/go-bacnet"

// NodeTypeMax is one past the largest BACnetNodeType (clause 21; zone = 21,
// as bacnet-stack bacenum.h BACNET_NODE_TYPE_MAX).
const NodeTypeMax uint32 = 22

// DeviceObjectReference is a BACnetDeviceObjectReference (clause 21): an
// object, optionally in another device.
type DeviceObjectReference struct {
	Device *bacnet.ObjectIdentifier
	Object bacnet.ObjectIdentifier
}

// StructuredViewConfig configures a Structured View object.
type StructuredViewConfig struct {
	Instance    uint32
	Name        string
	Description string
	NodeType    uint32 // BACnetNodeType, below NodeTypeMax
	NodeSubtype string // Node_Subtype when not empty
	// Subordinates is Subordinate_List.
	Subordinates []DeviceObjectReference
	// Annotations, when not nil, is Subordinate_Annotations: one text per
	// subordinate.
	Annotations []string
	// MaxSubordinates bounds Subordinate_List (default 256).
	MaxSubordinates int
}

// StructuredView is a Structured View object (clause 12.29). As in
// bacnet-stack's structured_view.c, Node_Type, Node_Subtype,
// Subordinate_List and Subordinate_Annotations are writable. Writing the
// whole Subordinate_List may change its length; Subordinate_Annotations
// follows it (texts of removed entries are dropped, new entries get an
// empty text) and is written with as many texts as there are subordinates.
type StructuredView struct {
	*Base
	nodeType    uint32
	subs        []DeviceObjectReference
	annotations []string // nil when the property is absent
	max         int
}

// NewStructuredView returns a Structured View object.
func NewStructuredView(cfg StructuredViewConfig) (*StructuredView, error) {
	if cfg.Instance >= bacnet.MaxObjectInstance {
		return nil, errorf("instance %d out of range", cfg.Instance)
	}
	if cfg.Name == "" {
		return nil, errorf("object name required")
	}
	if cfg.NodeType >= NodeTypeMax {
		return nil, errorf("node type %d out of range", cfg.NodeType)
	}
	if cfg.MaxSubordinates <= 0 {
		cfg.MaxSubordinates = 256
	}
	if len(cfg.Subordinates) > cfg.MaxSubordinates {
		return nil, errorf("%d subordinates, at most %d", len(cfg.Subordinates), cfg.MaxSubordinates)
	}
	if cfg.Annotations != nil && len(cfg.Annotations) != len(cfg.Subordinates) {
		return nil, errorf("%d annotations for %d subordinates", len(cfg.Annotations), len(cfg.Subordinates))
	}
	for i, s := range cfg.Subordinates {
		if s.Device != nil && s.Device.Type != bacnet.ObjectTypeDevice {
			return nil, errorf("subordinate %d: device identifier of type %v", i+1, s.Device.Type)
		}
	}
	id := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeStructuredView, Instance: cfg.Instance}
	sv := &StructuredView{Base: NewBase(id, cfg.Name), nodeType: cfg.NodeType, max: cfg.MaxSubordinates,
		subs: append([]DeviceObjectReference(nil), cfg.Subordinates...)}
	if cfg.Description != "" {
		sv.Define(bacnet.PropertyDescription, &Property{Value: charString(cfg.Description), Writable: true, Kinds: []bacnet.ValueKind{bacnet.ValueCharacterString}})
	}
	sv.Define(bacnet.PropertyNodeType, &Property{Required: true, Writable: true,
		Get: func() bacnet.ApplicationValue { return bacnet.EnumValue(sv.nodeType) }, Set: sv.writeNodeType})
	if cfg.NodeSubtype != "" {
		sv.Define(bacnet.PropertyNodeSubtype, &Property{Value: charString(cfg.NodeSubtype), Writable: true, Kinds: []bacnet.ValueKind{bacnet.ValueCharacterString}})
	}
	sv.Define(bacnet.PropertySubordinateList, &Property{Required: true, Array: true, Writable: true, Get: sv.subordinateList, Set: sv.writeSubordinates})
	if cfg.Annotations != nil {
		sv.annotations = append([]string{}, cfg.Annotations...)
		sv.Define(bacnet.PropertySubordinateAnnotations, &Property{Array: true, Writable: true, Get: sv.annotationList, Set: sv.writeAnnotations})
	}
	return sv, nil
}

func (sv *StructuredView) writeNodeType(w Write) ([]bacnet.PropertyIdentifier, error) {
	if w.Property.ArrayIndex != nil {
		return nil, ErrNotAnArray
	}
	if w.Value.Kind != bacnet.ValueEnumerated {
		return nil, ErrInvalidDataType
	}
	if w.Value.Enumerated >= NodeTypeMax {
		return nil, ErrValueOutOfRange
	}
	sv.nodeType = w.Value.Enumerated
	return []bacnet.PropertyIdentifier{bacnet.PropertyNodeType}, nil
}

func subordinateElement(s DeviceObjectReference) bacnet.Element {
	var els []bacnet.Element
	if s.Device != nil {
		els = append(els, bacnet.Element{Context: true, TagNumber: 0, Value: bacnet.ObjectIDValue(*s.Device)})
	}
	els = append(els, bacnet.Element{Context: true, TagNumber: 1, Value: bacnet.ObjectIDValue(s.Object)})
	return bacnet.Element{Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: els}}
}

func (sv *StructuredView) subordinateList() bacnet.ApplicationValue {
	els := make([]bacnet.Element, len(sv.subs))
	for i, s := range sv.subs {
		els[i] = subordinateElement(s)
	}
	return bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: els}
}

// parseSubordinates decodes BACnetDeviceObjectReference values from wire
// elements (device-identifier [0] OPTIONAL, object-identifier [1]) or from
// elements that group one reference each.
func parseSubordinates(els []bacnet.Element) ([]DeviceObjectReference, error) {
	var out []DeviceObjectReference
	for i := 0; i < len(els); {
		if !els[i].Context && els[i].Value.Kind == bacnet.ValueConstructed {
			s, n, err := parseSubordinate(els[i].Value.Elements)
			if err != nil || n != len(els[i].Value.Elements) {
				return nil, ErrInvalidDataType
			}
			out = append(out, s)
			i++
			continue
		}
		s, n, err := parseSubordinate(els[i:])
		if err != nil {
			return nil, err
		}
		out = append(out, s)
		i += n
	}
	return out, nil
}

func parseSubordinate(els []bacnet.Element) (DeviceObjectReference, int, error) {
	var s DeviceObjectReference
	n := 0
	if n < len(els) && els[n].TagNumber == 0 {
		dev, ok := ctxObjectID(els[n])
		if !ok || dev.Type != bacnet.ObjectTypeDevice {
			return s, 0, ErrInvalidDataType
		}
		s.Device = &dev
		n++
	}
	if n >= len(els) || els[n].TagNumber != 1 {
		return s, 0, ErrInvalidDataType
	}
	obj, ok := ctxObjectID(els[n])
	if !ok {
		return s, 0, ErrInvalidDataType
	}
	s.Object = obj
	return s, n + 1, nil
}

func (sv *StructuredView) writeSubordinates(w Write) ([]bacnet.PropertyIdentifier, error) {
	if w.Value.Kind != bacnet.ValueConstructed {
		return nil, ErrInvalidDataType
	}
	subs, err := parseSubordinates(w.Value.Elements)
	if err != nil {
		return nil, err
	}
	changed := []bacnet.PropertyIdentifier{bacnet.PropertySubordinateList}
	if w.Property.ArrayIndex != nil {
		idx := *w.Property.ArrayIndex
		if err := arrayIndex(idx, len(sv.subs)); err != nil {
			return nil, err
		}
		if len(subs) != 1 {
			return nil, ErrInvalidDataType
		}
		next := append([]DeviceObjectReference(nil), sv.subs...)
		next[idx-1] = subs[0]
		sv.subs = next
		return changed, nil
	}
	if len(subs) > sv.max {
		return nil, ErrNoSpaceToWriteProperty
	}
	sv.subs = subs
	if sv.annotations != nil && len(sv.annotations) != len(subs) {
		next := make([]string, len(subs))
		copy(next, sv.annotations)
		sv.annotations = next
		changed = append(changed, bacnet.PropertySubordinateAnnotations)
	}
	return changed, nil
}

func (sv *StructuredView) annotationList() bacnet.ApplicationValue {
	els := make([]bacnet.Element, len(sv.annotations))
	for i, a := range sv.annotations {
		els[i] = bacnet.Element{Value: charString(a)}
	}
	return bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: els}
}

func (sv *StructuredView) writeAnnotations(w Write) ([]bacnet.PropertyIdentifier, error) {
	changed := []bacnet.PropertyIdentifier{bacnet.PropertySubordinateAnnotations}
	if w.Property.ArrayIndex != nil {
		idx := *w.Property.ArrayIndex
		if err := arrayIndex(idx, len(sv.annotations)); err != nil {
			return nil, err
		}
		if w.Value.Kind != bacnet.ValueCharacterString {
			return nil, ErrInvalidDataType
		}
		next := append([]string(nil), sv.annotations...)
		next[idx-1] = w.Value.Character.Value
		sv.annotations = next
		return changed, nil
	}
	els := listElements(w.Value)
	next := make([]string, len(els))
	for i, el := range els {
		if el.Context || el.Value.Kind != bacnet.ValueCharacterString {
			return nil, ErrInvalidDataType
		}
		next[i] = el.Value.Character.Value
	}
	if len(next) != len(sv.subs) {
		return nil, ErrValueOutOfRange
	}
	sv.annotations = next
	return changed, nil
}

// Subordinates returns Subordinate_List.
func (sv *StructuredView) Subordinates() []DeviceObjectReference {
	var out []DeviceObjectReference
	sv.View(func() { out = append(out, sv.subs...) })
	return out
}

// Annotations returns Subordinate_Annotations (nil when absent).
func (sv *StructuredView) Annotations() []string {
	var out []string
	sv.View(func() {
		if sv.annotations != nil {
			out = append([]string{}, sv.annotations...)
		}
	})
	return out
}
