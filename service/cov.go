// SPDX-License-Identifier: MIT

package service

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
)

// SubscribeCOVRequest is a SubscribeCOV service request.
type SubscribeCOVRequest struct {
	ProcessIdentifier uint32
	MonitoredObject   bacnet.ObjectIdentifier
	IssueConfirmed    bool
	Lifetime          uint32 // seconds; ignored on cancellation
	Cancellation      bool
}

// EncodeSubscribeCOV encodes a SubscribeCOV payload.
func EncodeSubscribeCOV(req SubscribeCOVRequest) ([]byte, error) {
	var dst []byte
	var err error
	dst, err = bacnet.AppendContextUnsigned(dst, 0, uint64(req.ProcessIdentifier))
	if err != nil {
		return nil, err
	}
	dst, err = bacnet.AppendContextObjectID(dst, 1, req.MonitoredObject)
	if err != nil {
		return nil, err
	}
	if req.Cancellation {
		return dst, nil
	}
	dst, err = bacnet.AppendContextBool(dst, 2, req.IssueConfirmed)
	if err != nil {
		return nil, err
	}
	return bacnet.AppendContextUnsigned(dst, 3, uint64(req.Lifetime))
}

// DecodeSubscribeCOV decodes a SubscribeCOV payload.
func DecodeSubscribeCOV(payload []byte, limits bacnet.DecodeLimits) (SubscribeCOVRequest, error) {
	elements, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return SubscribeCOVRequest{}, err
	}
	if n != len(payload) {
		return SubscribeCOVRequest{}, fmt.Errorf("%w: SubscribeCOV trailing data", bacnet.ErrTrailingData)
	}
	var req SubscribeCOVRequest
	var havePID, haveObject, haveBool, haveLifetime bool
	for _, el := range elements {
		switch el.TagNumber {
		case 0:
			if havePID {
				return SubscribeCOVRequest{}, fmt.Errorf("%w: duplicate processIdentifier", bacnet.ErrMalformed)
			}
			u, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return SubscribeCOVRequest{}, err
			}
			if u > 0xFFFFFFFF {
				return SubscribeCOVRequest{}, fmt.Errorf("%w: processIdentifier overflow", bacnet.ErrMalformed)
			}
			req.ProcessIdentifier = uint32(u)
			havePID = true
		case 1:
			if haveObject {
				return SubscribeCOVRequest{}, fmt.Errorf("%w: duplicate monitoredObject", bacnet.ErrMalformed)
			}
			req.MonitoredObject, err = bacnet.ContextObjectID(el)
			if err != nil {
				return SubscribeCOVRequest{}, err
			}
			haveObject = true
		case 2:
			if haveBool {
				return SubscribeCOVRequest{}, fmt.Errorf("%w: duplicate issueConfirmedNotifications", bacnet.ErrMalformed)
			}
			req.IssueConfirmed, err = bacnet.ContextBool(el)
			if err != nil {
				return SubscribeCOVRequest{}, err
			}
			haveBool = true
		case 3:
			if haveLifetime {
				return SubscribeCOVRequest{}, fmt.Errorf("%w: duplicate lifetime", bacnet.ErrMalformed)
			}
			u, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return SubscribeCOVRequest{}, err
			}
			if u > 0xFFFFFFFF {
				return SubscribeCOVRequest{}, fmt.Errorf("%w: lifetime overflow", bacnet.ErrMalformed)
			}
			req.Lifetime = uint32(u)
			haveLifetime = true
		default:
			return SubscribeCOVRequest{}, fmt.Errorf("%w: unexpected SubscribeCOV tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
	}
	if !havePID || !haveObject {
		return SubscribeCOVRequest{}, fmt.Errorf("%w: SubscribeCOV missing required fields", bacnet.ErrMalformed)
	}
	req.Cancellation = !haveBool && !haveLifetime
	if !req.Cancellation && (!haveBool || !haveLifetime) {
		return SubscribeCOVRequest{}, fmt.Errorf("%w: SubscribeCOV incomplete subscription fields", bacnet.ErrMalformed)
	}
	return req, nil
}

// SubscribeCOVPropertyRequest extends SubscribeCOV with a property reference.
type SubscribeCOVPropertyRequest struct {
	SubscribeCOVRequest
	Property     bacnet.PropertyReference
	COVIncrement *float32
}

// EncodeSubscribeCOVProperty encodes SubscribeCOVProperty.
// Cancellation still includes the monitored property reference (context 4).
func EncodeSubscribeCOVProperty(req SubscribeCOVPropertyRequest) ([]byte, error) {
	base := req.SubscribeCOVRequest
	var dst []byte
	var err error
	dst, err = bacnet.AppendContextUnsigned(dst, 0, uint64(base.ProcessIdentifier))
	if err != nil {
		return nil, err
	}
	dst, err = bacnet.AppendContextObjectID(dst, 1, base.MonitoredObject)
	if err != nil {
		return nil, err
	}
	if !base.Cancellation {
		dst, err = bacnet.AppendContextBool(dst, 2, base.IssueConfirmed)
		if err != nil {
			return nil, err
		}
		dst, err = bacnet.AppendContextUnsigned(dst, 3, uint64(base.Lifetime))
		if err != nil {
			return nil, err
		}
	}
	dst = append(dst, 0x4E) // opening context 4
	dst, err = bacnet.AppendContextUnsigned(dst, 0, uint64(req.Property.Identifier))
	if err != nil {
		return nil, err
	}
	if req.Property.ArrayIndex != nil {
		dst, err = bacnet.AppendContextUnsigned(dst, 1, uint64(*req.Property.ArrayIndex))
		if err != nil {
			return nil, err
		}
	}
	dst = append(dst, 0x4F) // closing context 4
	if !base.Cancellation && req.COVIncrement != nil {
		var buf [4]byte
		binary.BigEndian.PutUint32(buf[:], math.Float32bits(*req.COVIncrement))
		el := bacnet.Element{
			Context:   true,
			TagNumber: 5,
			Value: bacnet.ApplicationValue{
				Kind:        bacnet.ValueContext,
				OctetString: buf[:],
			},
		}
		dst, err = bacnet.AppendTag(dst, el)
		if err != nil {
			return nil, err
		}
	}
	return dst, nil
}

// DecodeSubscribeCOVProperty decodes SubscribeCOVProperty (including cancellation with property).
func DecodeSubscribeCOVProperty(payload []byte, limits bacnet.DecodeLimits) (SubscribeCOVPropertyRequest, error) {
	elements, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return SubscribeCOVPropertyRequest{}, err
	}
	if n != len(payload) {
		return SubscribeCOVPropertyRequest{}, fmt.Errorf("%w: SubscribeCOVProperty trailing data", bacnet.ErrTrailingData)
	}
	var req SubscribeCOVPropertyRequest
	var havePID, haveObject, haveBool, haveLifetime, haveProp bool
	for _, el := range elements {
		switch {
		case el.TagNumber == 0 && !bacnet.IsContextConstructed(el):
			if havePID {
				return SubscribeCOVPropertyRequest{}, fmt.Errorf("%w: duplicate processIdentifier", bacnet.ErrMalformed)
			}
			u, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return SubscribeCOVPropertyRequest{}, err
			}
			if u > 0xFFFFFFFF {
				return SubscribeCOVPropertyRequest{}, fmt.Errorf("%w: processIdentifier overflow", bacnet.ErrMalformed)
			}
			req.ProcessIdentifier = uint32(u)
			havePID = true
		case el.TagNumber == 1 && !bacnet.IsContextConstructed(el):
			if haveObject {
				return SubscribeCOVPropertyRequest{}, fmt.Errorf("%w: duplicate monitoredObject", bacnet.ErrMalformed)
			}
			req.MonitoredObject, err = bacnet.ContextObjectID(el)
			if err != nil {
				return SubscribeCOVPropertyRequest{}, err
			}
			haveObject = true
		case el.TagNumber == 2 && !bacnet.IsContextConstructed(el):
			if haveBool {
				return SubscribeCOVPropertyRequest{}, fmt.Errorf("%w: duplicate issueConfirmedNotifications", bacnet.ErrMalformed)
			}
			req.IssueConfirmed, err = bacnet.ContextBool(el)
			if err != nil {
				return SubscribeCOVPropertyRequest{}, err
			}
			haveBool = true
		case el.TagNumber == 3 && !bacnet.IsContextConstructed(el):
			if haveLifetime {
				return SubscribeCOVPropertyRequest{}, fmt.Errorf("%w: duplicate lifetime", bacnet.ErrMalformed)
			}
			u, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return SubscribeCOVPropertyRequest{}, err
			}
			if u > 0xFFFFFFFF {
				return SubscribeCOVPropertyRequest{}, fmt.Errorf("%w: lifetime overflow", bacnet.ErrMalformed)
			}
			req.Lifetime = uint32(u)
			haveLifetime = true
		case el.TagNumber == 4 && bacnet.IsContextConstructed(el):
			if haveProp {
				return SubscribeCOVPropertyRequest{}, fmt.Errorf("%w: duplicate monitoredProperty", bacnet.ErrMalformed)
			}
			prop, err := decodePropertyReference(el.Value.Elements)
			if err != nil {
				return SubscribeCOVPropertyRequest{}, err
			}
			req.Property = prop
			haveProp = true
		case el.TagNumber == 5 && !bacnet.IsContextConstructed(el):
			if len(el.Value.OctetString) != 4 {
				return SubscribeCOVPropertyRequest{}, fmt.Errorf("%w: COVIncrement length", bacnet.ErrMalformed)
			}
			bits := binary.BigEndian.Uint32(el.Value.OctetString)
			f := math.Float32frombits(bits)
			req.COVIncrement = &f
		default:
			return SubscribeCOVPropertyRequest{}, fmt.Errorf("%w: unexpected SubscribeCOVProperty tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
	}
	if !havePID || !haveObject || !haveProp {
		return SubscribeCOVPropertyRequest{}, fmt.Errorf("%w: SubscribeCOVProperty missing required fields", bacnet.ErrMalformed)
	}
	req.Cancellation = !haveBool && !haveLifetime
	if !req.Cancellation && (!haveBool || !haveLifetime) {
		return SubscribeCOVPropertyRequest{}, fmt.Errorf("%w: SubscribeCOVProperty incomplete subscription fields", bacnet.ErrMalformed)
	}
	return req, nil
}

func decodePropertyReference(elements []bacnet.Element) (bacnet.PropertyReference, error) {
	var prop bacnet.PropertyReference
	var haveID bool
	for _, el := range elements {
		if bacnet.IsContextConstructed(el) || el.Opening || el.Closing {
			return prop, fmt.Errorf("%w: nested constructed in property reference", bacnet.ErrMalformed)
		}
		u, err := bacnet.ContextUnsigned(el)
		if err != nil {
			return prop, err
		}
		switch el.TagNumber {
		case 0:
			if haveID {
				return prop, fmt.Errorf("%w: duplicate propertyIdentifier", bacnet.ErrMalformed)
			}
			prop.Identifier = bacnet.PropertyIdentifier(u)
			haveID = true
		case 1:
			if prop.ArrayIndex != nil {
				return prop, fmt.Errorf("%w: duplicate arrayIndex", bacnet.ErrMalformed)
			}
			if u > 0xFFFFFFFF {
				return prop, fmt.Errorf("%w: arrayIndex overflow", bacnet.ErrMalformed)
			}
			idx := uint32(u)
			prop.ArrayIndex = &idx
		default:
			return prop, fmt.Errorf("%w: unexpected property reference tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
	}
	if !haveID {
		return prop, fmt.Errorf("%w: property reference missing identifier", bacnet.ErrMalformed)
	}
	return prop, nil
}

// COVNotification is a confirmed or unconfirmed COV notification payload.
type COVNotification struct {
	ProcessIdentifier uint32
	InitiatingDevice  bacnet.ObjectIdentifier
	MonitoredObject   bacnet.ObjectIdentifier
	TimeRemaining     uint32
	Values            []PropertyValue
}

// PropertyValue is a property identifier + value pair in a COV notification.
type PropertyValue struct {
	Property bacnet.PropertyReference
	Value    bacnet.ApplicationValue
}

// DecodeCOVNotification decodes a COV notification payload.
func DecodeCOVNotification(payload []byte, limits bacnet.DecodeLimits) (COVNotification, error) {
	elements, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return COVNotification{}, err
	}
	if n != len(payload) {
		return COVNotification{}, fmt.Errorf("%w: COV notification trailing data", bacnet.ErrTrailingData)
	}
	var note COVNotification
	var havePID, haveDevice, haveObject, haveTime, haveValues bool
	for _, el := range elements {
		switch {
		case el.TagNumber == 0 && !bacnet.IsContextConstructed(el):
			if havePID {
				return COVNotification{}, fmt.Errorf("%w: duplicate processIdentifier", bacnet.ErrMalformed)
			}
			u, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return COVNotification{}, err
			}
			if u > 0xFFFFFFFF {
				return COVNotification{}, fmt.Errorf("%w: processIdentifier overflow", bacnet.ErrMalformed)
			}
			note.ProcessIdentifier = uint32(u)
			havePID = true
		case el.TagNumber == 1 && !bacnet.IsContextConstructed(el):
			if haveDevice {
				return COVNotification{}, fmt.Errorf("%w: duplicate initiatingDevice", bacnet.ErrMalformed)
			}
			note.InitiatingDevice, err = bacnet.ContextObjectID(el)
			if err != nil {
				return COVNotification{}, err
			}
			haveDevice = true
		case el.TagNumber == 2 && !bacnet.IsContextConstructed(el):
			if haveObject {
				return COVNotification{}, fmt.Errorf("%w: duplicate monitoredObject", bacnet.ErrMalformed)
			}
			note.MonitoredObject, err = bacnet.ContextObjectID(el)
			if err != nil {
				return COVNotification{}, err
			}
			haveObject = true
		case el.TagNumber == 3 && !bacnet.IsContextConstructed(el):
			if haveTime {
				return COVNotification{}, fmt.Errorf("%w: duplicate timeRemaining", bacnet.ErrMalformed)
			}
			u, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return COVNotification{}, err
			}
			if u > 0xFFFFFFFF {
				return COVNotification{}, fmt.Errorf("%w: timeRemaining overflow", bacnet.ErrMalformed)
			}
			note.TimeRemaining = uint32(u)
			haveTime = true
		case el.TagNumber == 4 && bacnet.IsContextConstructed(el):
			if haveValues {
				return COVNotification{}, fmt.Errorf("%w: duplicate listOfValues", bacnet.ErrMalformed)
			}
			note.Values, err = decodeCOVValues(el.Value.Elements)
			if err != nil {
				return COVNotification{}, err
			}
			haveValues = true
		default:
			return COVNotification{}, fmt.Errorf("%w: unexpected COV notification tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
	}
	if !havePID || !haveDevice || !haveObject || !haveTime || !haveValues {
		return COVNotification{}, fmt.Errorf("%w: COV notification missing required fields", bacnet.ErrMalformed)
	}
	return note, nil
}

func decodeCOVValues(elements []bacnet.Element) ([]PropertyValue, error) {
	var out []PropertyValue
	var cur PropertyValue
	have := false
	flush := func() {
		if have {
			out = append(out, cur)
			cur = PropertyValue{}
			have = false
		}
	}
	for _, el := range elements {
		switch {
		case el.TagNumber == 0 && !bacnet.IsContextConstructed(el):
			flush()
			u, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return nil, err
			}
			cur.Property.Identifier = bacnet.PropertyIdentifier(u)
			have = true
		case el.TagNumber == 1 && !bacnet.IsContextConstructed(el):
			u, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return nil, err
			}
			idx := uint32(u)
			cur.Property.ArrayIndex = &idx
		case el.TagNumber == 2 && bacnet.IsContextConstructed(el):
			if len(el.Value.Elements) == 0 {
				return nil, fmt.Errorf("%w: empty propertyValue wrapper", bacnet.ErrMalformed)
			} else if len(el.Value.Elements) == 1 && !el.Value.Elements[0].Context {
				cur.Value = el.Value.Elements[0].Value.Clone()
			} else {
				cur.Value = bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: el.Value.Elements}.Clone()
			}
		default:
			return nil, fmt.Errorf("%w: unexpected COV value tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
	}
	flush()
	return out, nil
}

const (
	ServiceSubscribeCOV         = apdu.ServiceSubscribeCOV
	ServiceSubscribeCOVProperty = apdu.ServiceSubscribeCOVProperty
	ServiceUnconfirmedCOV       = apdu.ServiceUnconfirmedCOV
)
