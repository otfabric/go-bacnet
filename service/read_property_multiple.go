// SPDX-License-Identifier: MIT

package service

import (
	"fmt"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
)

// ReadAccessSpecification selects properties of one object for RPM.
type ReadAccessSpecification struct {
	Object     bacnet.ObjectIdentifier
	Properties []bacnet.PropertyReference
}

// EncodeReadPropertyMultiple encodes an RPM request payload.
func EncodeReadPropertyMultiple(specs []ReadAccessSpecification) ([]byte, error) {
	var dst []byte
	for _, spec := range specs {
		var err error
		dst, err = bacnet.AppendContextObjectID(dst, 0, spec.Object)
		if err != nil {
			return nil, err
		}
		dst = append(dst, 0x1E) // opening context 1 (list of properties)
		for _, p := range spec.Properties {
			dst, err = bacnet.AppendContextUnsigned(dst, 0, uint64(p.Identifier))
			if err != nil {
				return nil, err
			}
			if p.ArrayIndex != nil {
				dst, err = bacnet.AppendContextUnsigned(dst, 1, uint64(*p.ArrayIndex))
				if err != nil {
					return nil, err
				}
			}
		}
		dst = append(dst, 0x1F) // closing context 1
	}
	return dst, nil
}

// EncodeReadPropertyMultipleACK encodes an RPM complex ACK payload (test/helpers).
func EncodeReadPropertyMultipleACK(results []ReadAccessResult) ([]byte, error) {
	var dst []byte
	for _, r := range results {
		var err error
		dst, err = bacnet.AppendContextObjectID(dst, 0, r.Object)
		if err != nil {
			return nil, err
		}
		dst = append(dst, 0x1E) // opening context 1
		for _, p := range r.Properties {
			dst, err = bacnet.AppendContextUnsigned(dst, 2, uint64(p.Property.Identifier))
			if err != nil {
				return nil, err
			}
			if p.Property.ArrayIndex != nil {
				dst, err = bacnet.AppendContextUnsigned(dst, 3, uint64(*p.Property.ArrayIndex))
				if err != nil {
					return nil, err
				}
			}
			if p.Err != nil {
				er, ok := p.Err.(*bacnet.ErrorResponse)
				if !ok {
					return nil, fmt.Errorf("%w: property Err must be *ErrorResponse", bacnet.ErrMalformed)
				}
				dst = append(dst, 0x5E) // opening context 5
				dst, err = bacnet.EncodeBACnetError(dst, er.Class, er.Code)
				if err != nil {
					return nil, err
				}
				dst = append(dst, 0x5F) // closing context 5
				continue
			}
			dst = append(dst, 0x4E) // opening context 4
			dst, err = bacnet.AppendApplicationValue(dst, p.Value)
			if err != nil {
				return nil, err
			}
			dst = append(dst, 0x4F) // closing context 4
		}
		dst = append(dst, 0x1F) // closing context 1
	}
	return dst, nil
}

// PropertyResult is one property outcome in an RPM response.
type PropertyResult struct {
	Property bacnet.PropertyReference
	Value    bacnet.ApplicationValue
	Err      error // property-level BACnet error when non-nil
}

// ReadAccessResult is per-object RPM results.
type ReadAccessResult struct {
	Object     bacnet.ObjectIdentifier
	Properties []PropertyResult
}

// DecodeReadPropertyMultipleACK decodes an RPM complex ACK payload.
// Property-level errors are returned inside PropertyResult.Err.
func DecodeReadPropertyMultipleACK(payload []byte, limits bacnet.DecodeLimits) ([]ReadAccessResult, error) {
	elements, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return nil, err
	}
	if n != len(payload) {
		return nil, fmt.Errorf("%w: RPM ACK trailing data", bacnet.ErrTrailingData)
	}
	var results []ReadAccessResult
	var cur *ReadAccessResult
	for _, el := range elements {
		switch {
		case el.TagNumber == 0 && !bacnet.IsContextConstructed(el):
			id, err := bacnet.ContextObjectID(el)
			if err != nil {
				return nil, err
			}
			results = append(results, ReadAccessResult{Object: id})
			cur = &results[len(results)-1]
		case el.TagNumber == 1 && bacnet.IsContextConstructed(el):
			if cur == nil {
				return nil, fmt.Errorf("%w: RPM results without object", bacnet.ErrMalformed)
			}
			props, err := decodeRPMPropertyResults(el.Value.Elements, limits)
			if err != nil {
				return nil, err
			}
			cur.Properties = props
		default:
			return nil, fmt.Errorf("%w: unexpected RPM ACK tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
	}
	return results, nil
}

func decodeRPMPropertyResults(elements []bacnet.Element, limits bacnet.DecodeLimits) ([]PropertyResult, error) {
	_ = limits
	var out []PropertyResult
	var cur PropertyResult
	haveProp := false
	haveOutcome := false
	flush := func() error {
		if !haveProp {
			return nil
		}
		if !haveOutcome {
			return fmt.Errorf("%w: RPM property missing value or error", bacnet.ErrMalformed)
		}
		out = append(out, cur)
		cur = PropertyResult{}
		haveProp = false
		haveOutcome = false
		return nil
	}
	for _, el := range elements {
		switch {
		case el.TagNumber == 2 && !bacnet.IsContextConstructed(el):
			if err := flush(); err != nil {
				return nil, err
			}
			u, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return nil, err
			}
			cur.Property.Identifier = bacnet.PropertyIdentifier(u)
			haveProp = true
		case el.TagNumber == 3 && !bacnet.IsContextConstructed(el):
			if !haveProp {
				return nil, fmt.Errorf("%w: arrayIndex without property", bacnet.ErrMalformed)
			}
			u, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return nil, err
			}
			idx := uint32(u)
			cur.Property.ArrayIndex = &idx
		case el.TagNumber == 4 && bacnet.IsContextConstructed(el):
			if !haveProp {
				return nil, fmt.Errorf("%w: propertyValue without property", bacnet.ErrMalformed)
			}
			if haveOutcome {
				return nil, fmt.Errorf("%w: duplicate property outcome", bacnet.ErrMalformed)
			}
			if len(el.Value.Elements) == 0 {
				return nil, fmt.Errorf("%w: empty propertyValue wrapper", bacnet.ErrMalformed)
			} else if len(el.Value.Elements) == 1 && !el.Value.Elements[0].Context {
				cur.Value = el.Value.Elements[0].Value.Clone()
			} else {
				cur.Value = bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: el.Value.Elements}.Clone()
			}
			haveOutcome = true
		case el.TagNumber == 5 && bacnet.IsContextConstructed(el):
			if !haveProp {
				return nil, fmt.Errorf("%w: propertyAccessError without property", bacnet.ErrMalformed)
			}
			if haveOutcome {
				return nil, fmt.Errorf("%w: duplicate property outcome", bacnet.ErrMalformed)
			}
			class, code, err := bacnet.DecodeBACnetError(el.Value.Elements)
			if err != nil {
				return nil, err
			}
			cur.Err = &bacnet.ErrorResponse{Class: class, Code: code}
			haveOutcome = true
		default:
			return nil, fmt.Errorf("%w: unexpected RPM property tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return out, nil
}

const ServiceReadPropertyMultiple = apdu.ServiceReadPropertyMultiple
