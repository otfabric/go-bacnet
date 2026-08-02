// SPDX-License-Identifier: MIT

package service

import (
	"fmt"

	"github.com/otfabric/go-bacnet"
)

// PrivateTransfer carries a confirmed/unconfirmed private transfer payload.
//
// ServiceParameters is preserved as raw ASN.1 elements (vendor extension).
type PrivateTransfer struct {
	VendorID          uint16
	ServiceNumber     uint32
	ServiceParameters []bacnet.Element
}

// EncodePrivateTransfer encodes Confirmed/UnconfirmedPrivateTransfer.
func EncodePrivateTransfer(p PrivateTransfer) ([]byte, error) {
	dst, err := bacnet.AppendContextUnsigned(nil, 0, uint64(p.VendorID))
	if err != nil {
		return nil, err
	}
	dst, err = bacnet.AppendContextUnsigned(dst, 1, uint64(p.ServiceNumber))
	if err != nil {
		return nil, err
	}
	if len(p.ServiceParameters) == 0 {
		return dst, nil
	}
	return bacnet.AppendContextTagged(dst, 2, p.ServiceParameters)
}

// DecodePrivateTransfer decodes Confirmed/UnconfirmedPrivateTransfer.
func DecodePrivateTransfer(payload []byte, limits bacnet.DecodeLimits) (PrivateTransfer, error) {
	els, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return PrivateTransfer{}, err
	}
	if n != len(payload) {
		return PrivateTransfer{}, fmt.Errorf("%w: PrivateTransfer trailing", bacnet.ErrTrailingData)
	}
	var p PrivateTransfer
	var haveVendor, haveSvc bool
	for _, el := range els {
		switch {
		case el.TagNumber == 0 && !bacnet.IsContextConstructed(el):
			u, e := bacnet.ContextUnsigned(el)
			err = e
			if err == nil {
				if u > 0xFFFF {
					return PrivateTransfer{}, fmt.Errorf("%w: vendorID overflow", bacnet.ErrMalformed)
				}
				p.VendorID = uint16(u)
				haveVendor = true
			}
		case el.TagNumber == 1 && !bacnet.IsContextConstructed(el):
			u, e := bacnet.ContextUnsigned(el)
			err = e
			if err == nil {
				p.ServiceNumber = uint32(u)
				haveSvc = true
			}
		case el.TagNumber == 2 && bacnet.IsContextConstructed(el):
			p.ServiceParameters = cloneElements(el.Value.Elements)
		default:
			return PrivateTransfer{}, fmt.Errorf("%w: PrivateTransfer tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
		if err != nil {
			return PrivateTransfer{}, err
		}
	}
	if !haveVendor || !haveSvc {
		return PrivateTransfer{}, fmt.Errorf("%w: PrivateTransfer missing fields", bacnet.ErrMalformed)
	}
	return p, nil
}

// TextMessage is Confirmed/UnconfirmedTextMessage.
type TextMessage struct {
	TextMessageSourceDevice bacnet.ObjectIdentifier
	MessageClass            *uint32 // optional numeric class
	MessagePriority         uint8   // 0=normal, 1=urgent
	Message                 string
}

// EncodeTextMessage encodes Confirmed/UnconfirmedTextMessage.
func EncodeTextMessage(m TextMessage) ([]byte, error) {
	dst, err := bacnet.AppendContextObjectID(nil, 0, m.TextMessageSourceDevice)
	if err != nil {
		return nil, err
	}
	if m.MessageClass != nil {
		dst, err = bacnet.AppendContextUnsigned(dst, 1, uint64(*m.MessageClass))
		if err != nil {
			return nil, err
		}
	}
	dst, err = bacnet.AppendContextUnsigned(dst, 2, uint64(m.MessagePriority))
	if err != nil {
		return nil, err
	}
	return bacnet.AppendContextTagged(dst, 3, []bacnet.Element{{
		Value: bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: m.Message}},
	}})
}

// DecodeTextMessage decodes Confirmed/UnconfirmedTextMessage.
func DecodeTextMessage(payload []byte, limits bacnet.DecodeLimits) (TextMessage, error) {
	els, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return TextMessage{}, err
	}
	if n != len(payload) {
		return TextMessage{}, fmt.Errorf("%w: TextMessage trailing", bacnet.ErrTrailingData)
	}
	var m TextMessage
	var haveSrc, havePrio, haveMsg bool
	for _, el := range els {
		switch {
		case el.TagNumber == 0 && !bacnet.IsContextConstructed(el):
			m.TextMessageSourceDevice, err = bacnet.ContextObjectID(el)
			haveSrc = true
		case el.TagNumber == 1 && !bacnet.IsContextConstructed(el):
			u, e := bacnet.ContextUnsigned(el)
			err = e
			if err == nil {
				v := uint32(u)
				m.MessageClass = &v
			}
		case el.TagNumber == 2 && !bacnet.IsContextConstructed(el):
			u, e := bacnet.ContextUnsigned(el)
			err = e
			if err == nil {
				m.MessagePriority = uint8(u)
				havePrio = true
			}
		case el.TagNumber == 3 && bacnet.IsContextConstructed(el):
			if len(el.Value.Elements) == 1 && el.Value.Elements[0].Value.Kind == bacnet.ValueCharacterString {
				m.Message = el.Value.Elements[0].Value.Character.Value
				haveMsg = true
			} else {
				err = fmt.Errorf("%w: messageText", bacnet.ErrMalformed)
			}
		default:
			return TextMessage{}, fmt.Errorf("%w: TextMessage tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
		if err != nil {
			return TextMessage{}, err
		}
	}
	if !haveSrc || !havePrio || !haveMsg {
		return TextMessage{}, fmt.Errorf("%w: TextMessage missing fields", bacnet.ErrMalformed)
	}
	return m, nil
}

// TimeSynchronization is TimeSynchronization / UTCTimeSynchronization (Date + Time).
type TimeSynchronization struct {
	Date bacnet.Date
	Time bacnet.Time
}

// EncodeTimeSynchronization encodes Time/UTCTimeSynchronization.
func EncodeTimeSynchronization(t TimeSynchronization) ([]byte, error) {
	dst, err := bacnet.AppendApplicationValue(nil, bacnet.ApplicationValue{Kind: bacnet.ValueDate, Date: t.Date})
	if err != nil {
		return nil, err
	}
	return bacnet.AppendApplicationValue(dst, bacnet.ApplicationValue{Kind: bacnet.ValueTime, Time: t.Time})
}

// DecodeTimeSynchronization decodes Time/UTCTimeSynchronization.
func DecodeTimeSynchronization(payload []byte, limits bacnet.DecodeLimits) (TimeSynchronization, error) {
	els, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return TimeSynchronization{}, err
	}
	if n != len(payload) {
		return TimeSynchronization{}, fmt.Errorf("%w: TimeSynchronization trailing", bacnet.ErrTrailingData)
	}
	if len(els) != 2 || els[0].Value.Kind != bacnet.ValueDate || els[1].Value.Kind != bacnet.ValueTime {
		return TimeSynchronization{}, fmt.Errorf("%w: TimeSynchronization fields", bacnet.ErrMalformed)
	}
	return TimeSynchronization{Date: els[0].Value.Date, Time: els[1].Value.Time}, nil
}

// WriteGroup is an Unconfirmed WriteGroup request (simplified: group + change list as raw elements).
type WriteGroup struct {
	GroupNumber   uint32
	WritePriority uint8
	ChangeList    []bacnet.Element
	InhibitDelay  *bool
}

// EncodeWriteGroup encodes WriteGroup.
func EncodeWriteGroup(w WriteGroup) ([]byte, error) {
	dst, err := bacnet.AppendContextUnsigned(nil, 0, uint64(w.GroupNumber))
	if err != nil {
		return nil, err
	}
	dst, err = bacnet.AppendContextUnsigned(dst, 1, uint64(w.WritePriority))
	if err != nil {
		return nil, err
	}
	if len(w.ChangeList) == 0 {
		return nil, fmt.Errorf("%w: WriteGroup changeList required", bacnet.ErrMalformed)
	}
	dst, err = bacnet.AppendContextTagged(dst, 2, w.ChangeList)
	if err != nil {
		return nil, err
	}
	if w.InhibitDelay != nil {
		dst, err = bacnet.AppendContextBool(dst, 3, *w.InhibitDelay)
	}
	return dst, err
}

// DecodeWriteGroup decodes WriteGroup.
func DecodeWriteGroup(payload []byte, limits bacnet.DecodeLimits) (WriteGroup, error) {
	els, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return WriteGroup{}, err
	}
	if n != len(payload) {
		return WriteGroup{}, fmt.Errorf("%w: WriteGroup trailing", bacnet.ErrTrailingData)
	}
	var w WriteGroup
	var haveGroup, havePrio, haveList bool
	for _, el := range els {
		switch {
		case el.TagNumber == 0 && !bacnet.IsContextConstructed(el):
			u, e := bacnet.ContextUnsigned(el)
			err = e
			if err == nil {
				w.GroupNumber = uint32(u)
				haveGroup = true
			}
		case el.TagNumber == 1 && !bacnet.IsContextConstructed(el):
			u, e := bacnet.ContextUnsigned(el)
			err = e
			if err == nil {
				w.WritePriority = uint8(u)
				havePrio = true
			}
		case el.TagNumber == 2 && bacnet.IsContextConstructed(el):
			w.ChangeList = cloneElements(el.Value.Elements)
			haveList = true
		case el.TagNumber == 3 && !bacnet.IsContextConstructed(el):
			b, e := bacnet.ContextBool(el)
			err = e
			if err == nil {
				w.InhibitDelay = &b
			}
		default:
			return WriteGroup{}, fmt.Errorf("%w: WriteGroup tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
		if err != nil {
			return WriteGroup{}, err
		}
	}
	if !haveGroup || !havePrio || !haveList {
		return WriteGroup{}, fmt.Errorf("%w: WriteGroup missing fields", bacnet.ErrMalformed)
	}
	return w, nil
}
