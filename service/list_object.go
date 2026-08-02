// SPDX-License-Identifier: MIT

package service

import (
	"fmt"

	"github.com/otfabric/go-bacnet"
)

// ListElementRequest is AddListElement / RemoveListElement.
type ListElementRequest struct {
	Object   bacnet.ObjectIdentifier
	Property bacnet.PropertyReference
	Elements []bacnet.Element // listOfElements [3] ABSTRACT-SYNTAX.&Type
}

// EncodeListElementRequest encodes AddListElement or RemoveListElement.
func EncodeListElementRequest(req ListElementRequest) ([]byte, error) {
	dst, err := bacnet.AppendContextObjectID(nil, 0, req.Object)
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
	if len(req.Elements) == 0 {
		return nil, fmt.Errorf("%w: listOfElements required", bacnet.ErrMalformed)
	}
	return bacnet.AppendContextTagged(dst, 3, req.Elements)
}

// DecodeListElementRequest decodes Add/RemoveListElement.
func DecodeListElementRequest(payload []byte, limits bacnet.DecodeLimits) (ListElementRequest, error) {
	els, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return ListElementRequest{}, err
	}
	if n != len(payload) {
		return ListElementRequest{}, fmt.Errorf("%w: ListElement trailing", bacnet.ErrTrailingData)
	}
	var req ListElementRequest
	var haveObj, haveProp, haveList bool
	for _, el := range els {
		switch {
		case el.TagNumber == 0 && !bacnet.IsContextConstructed(el):
			req.Object, err = bacnet.ContextObjectID(el)
			haveObj = true
		case el.TagNumber == 1 && !bacnet.IsContextConstructed(el):
			u, e := bacnet.ContextUnsigned(el)
			err = e
			if err == nil {
				req.Property.Identifier = bacnet.PropertyIdentifier(u)
				haveProp = true
			}
		case el.TagNumber == 2 && !bacnet.IsContextConstructed(el):
			u, e := bacnet.ContextUnsigned(el)
			err = e
			if err == nil {
				idx := uint32(u)
				req.Property.ArrayIndex = &idx
			}
		case el.TagNumber == 3 && bacnet.IsContextConstructed(el):
			req.Elements = cloneElements(el.Value.Elements)
			haveList = true
		default:
			return ListElementRequest{}, fmt.Errorf("%w: ListElement tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
		if err != nil {
			return ListElementRequest{}, err
		}
	}
	if !haveObj || !haveProp || !haveList {
		return ListElementRequest{}, fmt.Errorf("%w: ListElement missing fields", bacnet.ErrMalformed)
	}
	return req, nil
}

// CreateObjectRequest is a CreateObject confirmed request.
//
// Wire shape (ASHRAE Clause 21):
//
//	objectSpecifier [0] CHOICE {
//	  objectType [0] Unsigned,
//	  objectIdentifier [1] ObjectIdentifier,
//	}
//	listOfInitialValues [1] SEQUENCE OF BACnetPropertyValue OPTIONAL
type CreateObjectRequest struct {
	ObjectType       *bacnet.ObjectType       // objectSpecifier CHOICE objectType [0]
	ObjectIdentifier *bacnet.ObjectIdentifier // or objectIdentifier [1]
	InitialValues    []PropertyValue          // listOfInitialValues [1] optional
}

// CreateObjectACK is a CreateObject ComplexACK (created objectIdentifier).
type CreateObjectACK struct {
	Object bacnet.ObjectIdentifier
}

// EncodeCreateObject encodes a CreateObject request.
func EncodeCreateObject(req CreateObjectRequest) ([]byte, error) {
	var choice []byte
	var err error
	switch {
	case req.ObjectIdentifier != nil:
		choice, err = bacnet.AppendContextObjectID(nil, 1, *req.ObjectIdentifier)
	case req.ObjectType != nil:
		choice, err = bacnet.AppendContextUnsigned(nil, 0, uint64(*req.ObjectType))
	default:
		return nil, fmt.Errorf("%w: CreateObject objectSpecifier required", bacnet.ErrMalformed)
	}
	if err != nil {
		return nil, err
	}
	dst := append([]byte{0x0E}, choice...) // opening objectSpecifier [0]
	dst = append(dst, 0x0F)                // closing objectSpecifier [0]
	if len(req.InitialValues) == 0 {
		return dst, nil
	}
	dst = append(dst, 0x1E) // opening listOfInitialValues [1]
	for _, pv := range req.InitialValues {
		dst, err = bacnet.AppendContextUnsigned(dst, 0, uint64(pv.Property.Identifier))
		if err != nil {
			return nil, err
		}
		if pv.Property.ArrayIndex != nil {
			dst, err = bacnet.AppendContextUnsigned(dst, 1, uint64(*pv.Property.ArrayIndex))
			if err != nil {
				return nil, err
			}
		}
		dst, err = bacnet.AppendContextTagged(dst, 2, []bacnet.Element{{Value: pv.Value}})
		if err != nil {
			return nil, err
		}
	}
	return append(dst, 0x1F), nil
}

// DecodeCreateObjectACK decodes a CreateObject ComplexACK.
func DecodeCreateObjectACK(payload []byte, limits bacnet.DecodeLimits) (CreateObjectACK, error) {
	els, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return CreateObjectACK{}, err
	}
	if n != len(payload) {
		return CreateObjectACK{}, fmt.Errorf("%w: CreateObjectACK trailing", bacnet.ErrTrailingData)
	}
	if len(els) != 1 {
		return CreateObjectACK{}, fmt.Errorf("%w: CreateObjectACK length", bacnet.ErrMalformed)
	}
	id, err := bacnet.AsObjectID(els[0].Value)
	if err != nil {
		// also accept context object id
		id, err = bacnet.ContextObjectID(els[0])
		if err != nil {
			return CreateObjectACK{}, err
		}
	}
	return CreateObjectACK{Object: id}, nil
}

// EncodeCreateObjectACK encodes a CreateObject ACK.
func EncodeCreateObjectACK(ack CreateObjectACK) ([]byte, error) {
	return bacnet.AppendApplicationValue(nil, bacnet.ObjectIDValue(ack.Object))
}

// DeleteObjectRequest is a DeleteObject confirmed request.
type DeleteObjectRequest struct {
	Object bacnet.ObjectIdentifier
}

// EncodeDeleteObject encodes a DeleteObject request.
func EncodeDeleteObject(req DeleteObjectRequest) ([]byte, error) {
	return bacnet.AppendApplicationValue(nil, bacnet.ObjectIDValue(req.Object))
}

// DecodeDeleteObject decodes a DeleteObject request.
func DecodeDeleteObject(payload []byte, limits bacnet.DecodeLimits) (DeleteObjectRequest, error) {
	els, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return DeleteObjectRequest{}, err
	}
	if n != len(payload) {
		return DeleteObjectRequest{}, fmt.Errorf("%w: DeleteObject trailing", bacnet.ErrTrailingData)
	}
	if len(els) != 1 {
		return DeleteObjectRequest{}, fmt.Errorf("%w: DeleteObject length", bacnet.ErrMalformed)
	}
	id, err := bacnet.AsObjectID(els[0].Value)
	if err != nil {
		return DeleteObjectRequest{}, err
	}
	return DeleteObjectRequest{Object: id}, nil
}
