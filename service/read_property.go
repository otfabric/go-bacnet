// SPDX-License-Identifier: MIT

package service

import (
	"fmt"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
)

// ReadPropertyRequest is a ReadProperty service request.
type ReadPropertyRequest struct {
	Object   bacnet.ObjectIdentifier
	Property bacnet.PropertyReference
}

// EncodeReadProperty encodes a ReadProperty service payload.
func EncodeReadProperty(req ReadPropertyRequest) ([]byte, error) {
	var dst []byte
	var err error
	dst, err = bacnet.AppendContextObjectID(dst, 0, req.Object)
	if err != nil {
		return nil, err
	}
	dst, err = bacnet.AppendContextUnsigned(dst, 1, uint64(req.Property.Identifier))
	if err != nil {
		return nil, err
	}
	if req.Property.ArrayIndex != nil {
		dst, err = bacnet.AppendContextUnsigned(dst, 2, uint64(*req.Property.ArrayIndex))
	}
	return dst, err
}

// DecodeReadProperty decodes a ReadProperty request payload.
func DecodeReadProperty(payload []byte, limits bacnet.DecodeLimits) (ReadPropertyRequest, error) {
	elements, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return ReadPropertyRequest{}, err
	}
	if n != len(payload) {
		return ReadPropertyRequest{}, fmt.Errorf("%w: ReadProperty trailing data", bacnet.ErrTrailingData)
	}
	var req ReadPropertyRequest
	var haveObject, haveProperty, haveIndex bool
	for _, el := range elements {
		switch el.TagNumber {
		case 0:
			if haveObject {
				return ReadPropertyRequest{}, fmt.Errorf("%w: duplicate objectIdentifier", bacnet.ErrMalformed)
			}
			req.Object, err = bacnet.ContextObjectID(el)
			haveObject = true
		case 1:
			if haveProperty {
				return ReadPropertyRequest{}, fmt.Errorf("%w: duplicate propertyIdentifier", bacnet.ErrMalformed)
			}
			var u uint64
			u, err = bacnet.ContextUnsigned(el)
			if err == nil {
				if u > 0xFFFFFFFF {
					return ReadPropertyRequest{}, fmt.Errorf("%w: propertyIdentifier overflow", bacnet.ErrMalformed)
				}
				req.Property.Identifier = bacnet.PropertyIdentifier(u)
				haveProperty = true
			}
		case 2:
			if haveIndex {
				return ReadPropertyRequest{}, fmt.Errorf("%w: duplicate arrayIndex", bacnet.ErrMalformed)
			}
			var u uint64
			u, err = bacnet.ContextUnsigned(el)
			if err == nil {
				if u > 0xFFFFFFFF {
					return ReadPropertyRequest{}, fmt.Errorf("%w: arrayIndex overflow", bacnet.ErrMalformed)
				}
				idx := uint32(u)
				req.Property.ArrayIndex = &idx
				haveIndex = true
			}
		default:
			return ReadPropertyRequest{}, fmt.Errorf("%w: unexpected ReadProperty tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
		if err != nil {
			return ReadPropertyRequest{}, err
		}
	}
	if !haveObject || !haveProperty {
		return ReadPropertyRequest{}, fmt.Errorf("%w: ReadProperty missing required fields", bacnet.ErrMalformed)
	}
	return req, nil
}

// ReadPropertyACK is a ReadProperty complex ACK payload.
type ReadPropertyACK struct {
	Object   bacnet.ObjectIdentifier
	Property bacnet.PropertyReference
	Value    bacnet.ApplicationValue
}

// EncodeReadPropertyACK encodes a ReadProperty ACK payload.
func EncodeReadPropertyACK(ack ReadPropertyACK) ([]byte, error) {
	var dst []byte
	var err error
	dst, err = bacnet.AppendContextObjectID(dst, 0, ack.Object)
	if err != nil {
		return nil, err
	}
	dst, err = bacnet.AppendContextUnsigned(dst, 1, uint64(ack.Property.Identifier))
	if err != nil {
		return nil, err
	}
	if ack.Property.ArrayIndex != nil {
		dst, err = bacnet.AppendContextUnsigned(dst, 2, uint64(*ack.Property.ArrayIndex))
		if err != nil {
			return nil, err
		}
	}
	var body []byte
	body, err = bacnet.AppendApplicationValue(body, ack.Value)
	if err != nil {
		return nil, err
	}
	// Context tag 3 opening/closing around value.
	dst = append(dst, 0x3E) // opening context 3
	dst = append(dst, body...)
	dst = append(dst, 0x3F) // closing context 3
	return dst, nil
}

// DecodeReadPropertyACK decodes a ReadProperty ACK payload.
func DecodeReadPropertyACK(payload []byte, limits bacnet.DecodeLimits) (ReadPropertyACK, error) {
	elements, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return ReadPropertyACK{}, err
	}
	if n != len(payload) {
		return ReadPropertyACK{}, fmt.Errorf("%w: ReadPropertyACK trailing data", bacnet.ErrTrailingData)
	}
	var ack ReadPropertyACK
	var haveObject, haveProperty, haveValue bool
	for _, el := range elements {
		switch {
		case el.TagNumber == 0 && !bacnet.IsContextConstructed(el):
			if haveObject {
				return ReadPropertyACK{}, fmt.Errorf("%w: duplicate objectIdentifier", bacnet.ErrMalformed)
			}
			ack.Object, err = bacnet.ContextObjectID(el)
			haveObject = true
		case el.TagNumber == 1 && !bacnet.IsContextConstructed(el):
			if haveProperty {
				return ReadPropertyACK{}, fmt.Errorf("%w: duplicate propertyIdentifier", bacnet.ErrMalformed)
			}
			var u uint64
			u, err = bacnet.ContextUnsigned(el)
			ack.Property.Identifier = bacnet.PropertyIdentifier(u)
			haveProperty = true
		case el.TagNumber == 2 && !bacnet.IsContextConstructed(el):
			var u uint64
			u, err = bacnet.ContextUnsigned(el)
			idx := uint32(u)
			ack.Property.ArrayIndex = &idx
		case el.TagNumber == 3 && bacnet.IsContextConstructed(el):
			if haveValue {
				return ReadPropertyACK{}, fmt.Errorf("%w: duplicate propertyValue", bacnet.ErrMalformed)
			}
			if len(el.Value.Elements) == 0 {
				return ReadPropertyACK{}, fmt.Errorf("%w: empty propertyValue wrapper", bacnet.ErrMalformed)
			} else if len(el.Value.Elements) == 1 && !el.Value.Elements[0].Context {
				ack.Value = el.Value.Elements[0].Value.Clone()
			} else {
				ack.Value = bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: el.Value.Elements}.Clone()
			}
			haveValue = true
		default:
			return ReadPropertyACK{}, fmt.Errorf("%w: unexpected ReadPropertyACK tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
		if err != nil {
			return ReadPropertyACK{}, err
		}
	}
	if !haveObject || !haveProperty || !haveValue {
		return ReadPropertyACK{}, fmt.Errorf("%w: ReadPropertyACK missing required fields", bacnet.ErrMalformed)
	}
	return ack, nil
}

// EncodeReadPropertyAPDU builds a confirmed ReadProperty APDU (invoke ID set by caller).
func EncodeReadPropertyPayload(req ReadPropertyRequest) ([]byte, error) {
	return EncodeReadProperty(req)
}

// Service choice constant re-export convenience.
const ServiceReadProperty = apdu.ServiceReadProperty
