// SPDX-License-Identifier: MIT

package service

import (
	"fmt"

	"github.com/otfabric/go-bacnet"
)

// DecodeReadPropertyMultiple decodes a ReadPropertyMultiple request payload
// (clause 15.7): a list of ReadAccessSpecification, each an object
// identifier [0] and a list of BACnetPropertyReference [1].
func DecodeReadPropertyMultiple(payload []byte, limits bacnet.DecodeLimits) ([]ReadAccessSpecification, error) {
	elements, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return nil, err
	}
	if n != len(payload) {
		return nil, fmt.Errorf("%w: ReadPropertyMultiple trailing data", bacnet.ErrTrailingData)
	}
	var specs []ReadAccessSpecification
	var cur *ReadAccessSpecification
	haveProps := false
	for _, el := range elements {
		switch {
		case el.TagNumber == 0 && !bacnet.IsContextConstructed(el):
			if cur != nil && !haveProps {
				return nil, fmt.Errorf("%w: ReadAccessSpecification missing listOfPropertyReferences", bacnet.ErrMalformed)
			}
			id, err := bacnet.ContextObjectID(el)
			if err != nil {
				return nil, err
			}
			specs = append(specs, ReadAccessSpecification{Object: id})
			cur = &specs[len(specs)-1]
			haveProps = false
		case el.TagNumber == 1 && bacnet.IsContextConstructed(el):
			if cur == nil {
				return nil, fmt.Errorf("%w: listOfPropertyReferences without object", bacnet.ErrMalformed)
			}
			if haveProps {
				return nil, fmt.Errorf("%w: duplicate listOfPropertyReferences", bacnet.ErrMalformed)
			}
			refs, err := decodePropertyReferences(el.Value.Elements)
			if err != nil {
				return nil, err
			}
			cur.Properties = refs
			haveProps = true
		default:
			return nil, fmt.Errorf("%w: unexpected ReadPropertyMultiple tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
	}
	if len(specs) == 0 {
		return nil, fmt.Errorf("%w: ReadPropertyMultiple empty", bacnet.ErrMalformed)
	}
	if !haveProps {
		return nil, fmt.Errorf("%w: ReadAccessSpecification missing listOfPropertyReferences", bacnet.ErrMalformed)
	}
	return specs, nil
}

// decodePropertyReferences decodes a SEQUENCE OF BACnetPropertyReference:
// propertyIdentifier [0], propertyArrayIndex [1] OPTIONAL.
func decodePropertyReferences(elements []bacnet.Element) ([]bacnet.PropertyReference, error) {
	var out []bacnet.PropertyReference
	for _, el := range elements {
		if bacnet.IsContextConstructed(el) || !el.Context {
			return nil, fmt.Errorf("%w: BACnetPropertyReference element", bacnet.ErrMalformed)
		}
		switch el.TagNumber {
		case 0:
			u, err := bacnet.ContextUnsigned32(el)
			if err != nil {
				return nil, err
			}
			out = append(out, bacnet.PropertyReference{Identifier: bacnet.PropertyIdentifier(u)})
		case 1:
			if len(out) == 0 || out[len(out)-1].ArrayIndex != nil {
				return nil, fmt.Errorf("%w: propertyArrayIndex without propertyIdentifier", bacnet.ErrMalformed)
			}
			u, err := bacnet.ContextUnsigned32(el)
			if err != nil {
				return nil, err
			}
			idx := u
			out[len(out)-1].ArrayIndex = &idx
		default:
			return nil, fmt.Errorf("%w: unexpected BACnetPropertyReference tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: empty listOfPropertyReferences", bacnet.ErrMalformed)
	}
	return out, nil
}

// EncodeCOVNotification encodes a Confirmed/UnconfirmedCOVNotification
// payload (clauses 13.6, 13.7): subscriberProcessIdentifier [0],
// initiatingDeviceIdentifier [1], monitoredObjectIdentifier [2],
// timeRemaining [3], listOfValues [4].
func EncodeCOVNotification(note COVNotification) ([]byte, error) {
	dst, err := bacnet.AppendContextUnsigned(nil, 0, uint64(note.ProcessIdentifier))
	if err != nil {
		return nil, err
	}
	if dst, err = bacnet.AppendContextObjectID(dst, 1, note.InitiatingDevice); err != nil {
		return nil, err
	}
	if dst, err = bacnet.AppendContextObjectID(dst, 2, note.MonitoredObject); err != nil {
		return nil, err
	}
	if dst, err = bacnet.AppendContextUnsigned(dst, 3, uint64(note.TimeRemaining)); err != nil {
		return nil, err
	}
	if len(note.Values) == 0 {
		return nil, fmt.Errorf("%w: COV notification without values", bacnet.ErrMalformed)
	}
	dst = append(dst, 0x4E) // opening listOfValues [4]
	if dst, err = appendPropertyValues(dst, note.Values, "COV notification"); err != nil {
		return nil, err
	}
	return append(dst, 0x4F), nil
}

// appendPropertyValues appends a SEQUENCE OF BACnetPropertyValue (the
// enclosing tags are the caller's).
func appendPropertyValues(dst []byte, values []PropertyValue, what string) ([]byte, error) {
	var err error
	for _, pv := range values {
		if dst, err = bacnet.AppendContextUnsigned(dst, 0, uint64(pv.Property.Identifier)); err != nil {
			return nil, err
		}
		if pv.Property.ArrayIndex != nil {
			if dst, err = bacnet.AppendContextUnsigned(dst, 1, uint64(*pv.Property.ArrayIndex)); err != nil {
				return nil, err
			}
		}
		if dst, err = bacnet.AppendContextTagged(dst, 2, []bacnet.Element{{Value: pv.Value}}); err != nil {
			return nil, err
		}
		if pv.Priority != nil {
			if *pv.Priority < 1 || *pv.Priority > 16 {
				return nil, fmt.Errorf("%w: %s priority out of range", bacnet.ErrMalformed, what)
			}
			if dst, err = bacnet.AppendContextUnsigned(dst, 3, uint64(*pv.Priority)); err != nil {
				return nil, err
			}
		}
	}
	return dst, nil
}
