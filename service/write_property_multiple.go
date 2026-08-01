// SPDX-License-Identifier: MIT

package service

import (
	"fmt"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
)

// WritePropertyValue is one property write inside a WriteAccessSpecification.
//
// Wire tags differ from single WriteProperty: value is context [2] and optional
// priority is context [3] (ASHRAE 135 BACnetPropertyValue).
type WritePropertyValue struct {
	Property bacnet.PropertyReference
	Value    bacnet.ApplicationValue
	Priority *uint8 // 1..16; nil means unspecified
}

// WriteAccessSpecification selects an object and the properties to write.
type WriteAccessSpecification struct {
	Object     bacnet.ObjectIdentifier
	Properties []WritePropertyValue
}

// EncodeWritePropertyMultiple encodes a WritePropertyMultiple request payload.
func EncodeWritePropertyMultiple(specs []WriteAccessSpecification) ([]byte, error) {
	if len(specs) == 0 {
		return nil, fmt.Errorf("%w: WritePropertyMultiple requires at least one specification", bacnet.ErrMalformed)
	}
	var dst []byte
	for _, spec := range specs {
		if len(spec.Properties) == 0 {
			return nil, fmt.Errorf("%w: WriteAccessSpecification requires properties", bacnet.ErrMalformed)
		}
		var err error
		dst, err = bacnet.AppendContextObjectID(dst, 0, spec.Object)
		if err != nil {
			return nil, err
		}
		dst = append(dst, 0x1E) // opening context 1
		for _, p := range spec.Properties {
			dst, err = bacnet.AppendContextUnsigned(dst, 0, uint64(p.Property.Identifier))
			if err != nil {
				return nil, err
			}
			if p.Property.ArrayIndex != nil {
				dst, err = bacnet.AppendContextUnsigned(dst, 1, uint64(*p.Property.ArrayIndex))
				if err != nil {
					return nil, err
				}
			}
			var body []byte
			body, err = bacnet.AppendApplicationValue(body, p.Value)
			if err != nil {
				return nil, err
			}
			dst = append(dst, 0x2E) // opening context 2 (propertyValue)
			dst = append(dst, body...)
			dst = append(dst, 0x2F) // closing context 2
			if p.Priority != nil {
				if *p.Priority < 1 || *p.Priority > 16 {
					return nil, fmt.Errorf("%w: WritePropertyMultiple priority out of range", bacnet.ErrMalformed)
				}
				dst, err = bacnet.AppendContextUnsigned(dst, 3, uint64(*p.Priority))
				if err != nil {
					return nil, err
				}
			}
		}
		dst = append(dst, 0x1F) // closing context 1
	}
	return dst, nil
}

// DecodeWritePropertyMultiple decodes a WritePropertyMultiple request payload.
func DecodeWritePropertyMultiple(payload []byte, limits bacnet.DecodeLimits) ([]WriteAccessSpecification, error) {
	elements, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return nil, err
	}
	if n != len(payload) {
		return nil, fmt.Errorf("%w: WritePropertyMultiple trailing data", bacnet.ErrTrailingData)
	}
	var specs []WriteAccessSpecification
	var cur *WriteAccessSpecification
	for _, el := range elements {
		switch {
		case el.TagNumber == 0 && !bacnet.IsContextConstructed(el):
			id, err := bacnet.ContextObjectID(el)
			if err != nil {
				return nil, err
			}
			specs = append(specs, WriteAccessSpecification{Object: id})
			cur = &specs[len(specs)-1]
		case el.TagNumber == 1 && bacnet.IsContextConstructed(el):
			if cur == nil {
				return nil, fmt.Errorf("%w: listOfProperties without object", bacnet.ErrMalformed)
			}
			props, err := decodeWPMPropertyValues(el.Value.Elements, limits)
			if err != nil {
				return nil, err
			}
			cur.Properties = props
		default:
			return nil, fmt.Errorf("%w: unexpected WritePropertyMultiple tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
	}
	if len(specs) == 0 {
		return nil, fmt.Errorf("%w: WritePropertyMultiple empty", bacnet.ErrMalformed)
	}
	for _, s := range specs {
		if len(s.Properties) == 0 {
			return nil, fmt.Errorf("%w: WriteAccessSpecification missing properties", bacnet.ErrMalformed)
		}
	}
	return specs, nil
}

func decodeWPMPropertyValues(elements []bacnet.Element, limits bacnet.DecodeLimits) ([]WritePropertyValue, error) {
	_ = limits
	var out []WritePropertyValue
	var cur WritePropertyValue
	var haveProp, haveValue bool
	flush := func() error {
		if !haveProp && !haveValue {
			return nil
		}
		if !haveProp || !haveValue {
			return fmt.Errorf("%w: WritePropertyValue missing property or value", bacnet.ErrMalformed)
		}
		out = append(out, cur)
		cur = WritePropertyValue{}
		haveProp = false
		haveValue = false
		return nil
	}
	for _, el := range elements {
		switch {
		case el.TagNumber == 0 && !bacnet.IsContextConstructed(el):
			if err := flush(); err != nil {
				return nil, err
			}
			u, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return nil, err
			}
			cur.Property.Identifier = bacnet.PropertyIdentifier(u)
			haveProp = true
		case el.TagNumber == 1 && !bacnet.IsContextConstructed(el):
			if !haveProp {
				return nil, fmt.Errorf("%w: arrayIndex without property", bacnet.ErrMalformed)
			}
			u, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return nil, err
			}
			idx := uint32(u)
			cur.Property.ArrayIndex = &idx
		case el.TagNumber == 2 && bacnet.IsContextConstructed(el):
			if !haveProp {
				return nil, fmt.Errorf("%w: propertyValue without property", bacnet.ErrMalformed)
			}
			if haveValue {
				return nil, fmt.Errorf("%w: duplicate propertyValue", bacnet.ErrMalformed)
			}
			if len(el.Value.Elements) == 0 {
				return nil, fmt.Errorf("%w: empty propertyValue wrapper", bacnet.ErrMalformed)
			} else if len(el.Value.Elements) == 1 && !el.Value.Elements[0].Context {
				cur.Value = el.Value.Elements[0].Value.Clone()
			} else {
				cur.Value = bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: el.Value.Elements}.Clone()
			}
			haveValue = true
		case el.TagNumber == 3 && !bacnet.IsContextConstructed(el):
			if !haveProp || !haveValue {
				return nil, fmt.Errorf("%w: priority without property value", bacnet.ErrMalformed)
			}
			u, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return nil, err
			}
			if u < 1 || u > 16 {
				return nil, fmt.Errorf("%w: WritePropertyMultiple priority out of range", bacnet.ErrMalformed)
			}
			p := uint8(u)
			cur.Priority = &p
		default:
			return nil, fmt.Errorf("%w: unexpected WritePropertyValue tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return out, nil
}

// WritePropertyMultipleError is a decoded WritePropertyMultiple-Error payload.
//
// BACnet reports only the first failed write. Properties encoded before
// FirstFailed may already have been applied at the peer.
type WritePropertyMultipleError struct {
	Class         uint16
	Code          uint16
	FirstFailed   bacnet.ObjectIdentifier
	FirstProperty bacnet.PropertyReference
}

func (e *WritePropertyMultipleError) Error() string {
	return fmt.Sprintf("bacnet: WritePropertyMultiple error class=%d code=%d first_failed=%v property=%d",
		e.Class, e.Code, e.FirstFailed, e.FirstProperty.Identifier)
}

// DecodeWritePropertyMultipleError decodes a WPM Error service payload
// (Error APDU payload after invokeID/serviceChoice).
func DecodeWritePropertyMultipleError(payload []byte, limits bacnet.DecodeLimits) (*WritePropertyMultipleError, error) {
	elements, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return nil, err
	}
	if n != len(payload) {
		return nil, fmt.Errorf("%w: WritePropertyMultiple-Error trailing data", bacnet.ErrTrailingData)
	}
	out := &WritePropertyMultipleError{}
	var haveErr, haveFirst bool
	for _, el := range elements {
		switch {
		case el.TagNumber == 0 && bacnet.IsContextConstructed(el):
			if haveErr {
				return nil, fmt.Errorf("%w: duplicate errorType", bacnet.ErrMalformed)
			}
			class, code, err := bacnet.DecodeBACnetError(el.Value.Elements)
			if err != nil {
				return nil, err
			}
			out.Class = class
			out.Code = code
			haveErr = true
		case el.TagNumber == 1 && bacnet.IsContextConstructed(el):
			if haveFirst {
				return nil, fmt.Errorf("%w: duplicate firstFailedWriteAttempt", bacnet.ErrMalformed)
			}
			obj, prop, err := decodeObjectPropertyReference(el.Value.Elements)
			if err != nil {
				return nil, err
			}
			out.FirstFailed = obj
			out.FirstProperty = prop
			haveFirst = true
		default:
			return nil, fmt.Errorf("%w: unexpected WritePropertyMultiple-Error tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
	}
	if !haveErr || !haveFirst {
		return nil, fmt.Errorf("%w: WritePropertyMultiple-Error missing fields", bacnet.ErrMalformed)
	}
	return out, nil
}

// EncodeWritePropertyMultipleError encodes a WPM Error payload (tests/helpers).
func EncodeWritePropertyMultipleError(e WritePropertyMultipleError) ([]byte, error) {
	var dst []byte
	dst = append(dst, 0x0E) // opening context 0
	var err error
	dst, err = bacnet.EncodeBACnetError(dst, e.Class, e.Code)
	if err != nil {
		return nil, err
	}
	dst = append(dst, 0x0F) // closing context 0
	dst = append(dst, 0x1E) // opening context 1
	dst, err = bacnet.AppendContextObjectID(dst, 0, e.FirstFailed)
	if err != nil {
		return nil, err
	}
	dst, err = bacnet.AppendContextUnsigned(dst, 1, uint64(e.FirstProperty.Identifier))
	if err != nil {
		return nil, err
	}
	if e.FirstProperty.ArrayIndex != nil {
		dst, err = bacnet.AppendContextUnsigned(dst, 2, uint64(*e.FirstProperty.ArrayIndex))
		if err != nil {
			return nil, err
		}
	}
	dst = append(dst, 0x1F) // closing context 1
	return dst, nil
}

func decodeObjectPropertyReference(elements []bacnet.Element) (bacnet.ObjectIdentifier, bacnet.PropertyReference, error) {
	var obj bacnet.ObjectIdentifier
	var prop bacnet.PropertyReference
	var haveObj, haveProp bool
	for _, el := range elements {
		switch {
		case el.TagNumber == 0 && !bacnet.IsContextConstructed(el):
			if haveObj {
				return obj, prop, fmt.Errorf("%w: duplicate objectIdentifier", bacnet.ErrMalformed)
			}
			id, err := bacnet.ContextObjectID(el)
			if err != nil {
				return obj, prop, err
			}
			obj = id
			haveObj = true
		case el.TagNumber == 1 && !bacnet.IsContextConstructed(el):
			if haveProp {
				return obj, prop, fmt.Errorf("%w: duplicate propertyIdentifier", bacnet.ErrMalformed)
			}
			u, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return obj, prop, err
			}
			prop.Identifier = bacnet.PropertyIdentifier(u)
			haveProp = true
		case el.TagNumber == 2 && !bacnet.IsContextConstructed(el):
			u, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return obj, prop, err
			}
			idx := uint32(u)
			prop.ArrayIndex = &idx
		default:
			return obj, prop, fmt.Errorf("%w: unexpected ObjectPropertyReference tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
	}
	if !haveObj || !haveProp {
		return obj, prop, fmt.Errorf("%w: ObjectPropertyReference missing fields", bacnet.ErrMalformed)
	}
	return obj, prop, nil
}

const ServiceWritePropertyMultiple = apdu.ServiceWritePropertyMultiple
