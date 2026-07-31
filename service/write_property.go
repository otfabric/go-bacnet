// SPDX-License-Identifier: MIT

package service

import (
	"fmt"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
)

// WritePropertyRequest is a WriteProperty service request.
type WritePropertyRequest struct {
	Object   bacnet.ObjectIdentifier
	Property bacnet.PropertyReference
	Value    bacnet.ApplicationValue
	Priority *uint8 // 1..16; nil means unspecified
}

// EncodeWriteProperty encodes a WriteProperty service payload.
func EncodeWriteProperty(req WritePropertyRequest) ([]byte, error) {
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
		if err != nil {
			return nil, err
		}
	}
	var body []byte
	body, err = bacnet.AppendApplicationValue(body, req.Value)
	if err != nil {
		return nil, err
	}
	dst = append(dst, 0x3E)
	dst = append(dst, body...)
	dst = append(dst, 0x3F)
	if req.Priority != nil {
		dst, err = bacnet.AppendContextUnsigned(dst, 4, uint64(*req.Priority))
	}
	return dst, err
}

// DecodeWriteProperty decodes a WriteProperty request payload.
func DecodeWriteProperty(payload []byte, limits bacnet.DecodeLimits) (WritePropertyRequest, error) {
	elements, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return WritePropertyRequest{}, err
	}
	if n != len(payload) {
		return WritePropertyRequest{}, fmt.Errorf("%w: WriteProperty trailing data", bacnet.ErrTrailingData)
	}
	var req WritePropertyRequest
	var haveObject, haveProperty, haveValue bool
	for _, el := range elements {
		switch {
		case el.TagNumber == 0 && !bacnet.IsContextConstructed(el):
			if haveObject {
				return WritePropertyRequest{}, fmt.Errorf("%w: duplicate objectIdentifier", bacnet.ErrMalformed)
			}
			req.Object, err = bacnet.ContextObjectID(el)
			haveObject = true
		case el.TagNumber == 1 && !bacnet.IsContextConstructed(el):
			if haveProperty {
				return WritePropertyRequest{}, fmt.Errorf("%w: duplicate propertyIdentifier", bacnet.ErrMalformed)
			}
			var u uint64
			u, err = bacnet.ContextUnsigned(el)
			req.Property.Identifier = bacnet.PropertyIdentifier(u)
			haveProperty = true
		case el.TagNumber == 2 && !bacnet.IsContextConstructed(el):
			var u uint64
			u, err = bacnet.ContextUnsigned(el)
			idx := uint32(u)
			req.Property.ArrayIndex = &idx
		case el.TagNumber == 3 && bacnet.IsContextConstructed(el):
			if haveValue {
				return WritePropertyRequest{}, fmt.Errorf("%w: duplicate propertyValue", bacnet.ErrMalformed)
			}
			if len(el.Value.Elements) == 0 {
				return WritePropertyRequest{}, fmt.Errorf("%w: empty propertyValue wrapper", bacnet.ErrMalformed)
			} else if len(el.Value.Elements) == 1 && !el.Value.Elements[0].Context {
				req.Value = el.Value.Elements[0].Value.Clone()
			} else {
				req.Value = bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: el.Value.Elements}.Clone()
			}
			haveValue = true
		case el.TagNumber == 4 && !bacnet.IsContextConstructed(el):
			var u uint64
			u, err = bacnet.ContextUnsigned(el)
			if err != nil {
				return WritePropertyRequest{}, err
			}
			if u < 1 || u > 16 {
				return WritePropertyRequest{}, fmt.Errorf("%w: WriteProperty priority out of range", bacnet.ErrMalformed)
			}
			p := uint8(u)
			req.Priority = &p
		default:
			return WritePropertyRequest{}, fmt.Errorf("%w: unexpected WriteProperty tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
		if err != nil {
			return WritePropertyRequest{}, err
		}
	}
	if !haveObject || !haveProperty || !haveValue {
		return WritePropertyRequest{}, fmt.Errorf("%w: WriteProperty missing required fields", bacnet.ErrMalformed)
	}
	return req, nil
}

const ServiceWriteProperty = apdu.ServiceWriteProperty
