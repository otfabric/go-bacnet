// SPDX-License-Identifier: MIT

package service

import (
	"fmt"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
)

// TimeStampChoice discriminates BACnetTimeStamp.
type TimeStampChoice uint8

const (
	TimeStampTime TimeStampChoice = iota
	TimeStampSequence
	TimeStampDateTime
)

// TimeStamp is a BACnetTimeStamp CHOICE.
type TimeStamp struct {
	Choice   TimeStampChoice
	Time     bacnet.Time
	Sequence uint32
	DateTime DateTime
}

// EventNotification is a Confirmed/Unconfirmed EventNotification payload.
//
// NotificationParams retains the constructed CHOICE body when present (opaque
// application values). Parameters holds a typed decode of common alternatives
// (change-of-state, change-of-value, out-of-range, change-of-bitstring) when
// recognized. Encode prefers Parameters when set, otherwise NotificationParams.
type EventNotification struct {
	ProcessIdentifier  uint32
	InitiatingDevice   bacnet.ObjectIdentifier
	EventObject        bacnet.ObjectIdentifier
	TimeStamp          TimeStamp
	NotificationClass  uint32
	Priority           uint8
	EventType          uint32
	MessageText        *bacnet.CharacterString
	NotifyType         uint32
	AckRequired        *bool
	FromState          *uint32
	ToState            uint32
	NotificationParams *bacnet.ApplicationValue // constructed CHOICE body; nil if absent
	Parameters         *NotificationParameters  // typed CHOICE when recognized
}

// EncodeTimeStamp encodes a BACnetTimeStamp CHOICE.
func EncodeTimeStamp(ts TimeStamp) ([]byte, error) {
	switch ts.Choice {
	case TimeStampTime:
		return bacnet.AppendContextTime(nil, 0, ts.Time)
	case TimeStampSequence:
		return bacnet.AppendContextUnsigned(nil, 1, uint64(ts.Sequence))
	case TimeStampDateTime:
		return bacnet.AppendContextTagged(nil, 2, []bacnet.Element{
			{Value: bacnet.ApplicationValue{Kind: bacnet.ValueDate, Date: ts.DateTime.Date}},
			{Value: bacnet.ApplicationValue{Kind: bacnet.ValueTime, Time: ts.DateTime.Time}},
		})
	default:
		return nil, fmt.Errorf("%w: unknown TimeStamp choice", bacnet.ErrMalformed)
	}
}

// DecodeTimeStamp decodes a BACnetTimeStamp from a single context element.
func DecodeTimeStamp(el bacnet.Element) (TimeStamp, error) {
	switch {
	case el.TagNumber == 0 && !bacnet.IsContextConstructed(el):
		t, err := bacnet.ContextTime(el)
		if err != nil {
			return TimeStamp{}, err
		}
		return TimeStamp{Choice: TimeStampTime, Time: t}, nil
	case el.TagNumber == 1 && !bacnet.IsContextConstructed(el):
		u, err := bacnet.ContextUnsigned(el)
		if err != nil {
			return TimeStamp{}, err
		}
		if u > 0xFFFFFFFF {
			return TimeStamp{}, fmt.Errorf("%w: TimeStamp sequence overflow", bacnet.ErrMalformed)
		}
		return TimeStamp{Choice: TimeStampSequence, Sequence: uint32(u)}, nil
	case el.TagNumber == 2 && bacnet.IsContextConstructed(el):
		if len(el.Value.Elements) != 2 {
			return TimeStamp{}, fmt.Errorf("%w: TimeStamp dateTime contents", bacnet.ErrMalformed)
		}
		d, t := el.Value.Elements[0], el.Value.Elements[1]
		if d.Context || d.Value.Kind != bacnet.ValueDate || t.Context || t.Value.Kind != bacnet.ValueTime {
			return TimeStamp{}, fmt.Errorf("%w: TimeStamp dateTime kinds", bacnet.ErrMalformed)
		}
		return TimeStamp{Choice: TimeStampDateTime, DateTime: DateTime{Date: d.Value.Date, Time: t.Value.Time}}, nil
	default:
		return TimeStamp{}, fmt.Errorf("%w: unexpected TimeStamp tag %d", bacnet.ErrMalformed, el.TagNumber)
	}
}

func appendTimeStampField(dst []byte, tagNumber uint8, ts TimeStamp) ([]byte, error) {
	body, err := EncodeTimeStamp(ts)
	if err != nil {
		return dst, err
	}
	dst = append(dst, (tagNumber<<4)|0x08|6) // opening
	dst = append(dst, body...)
	dst = append(dst, (tagNumber<<4)|0x08|7) // closing
	return dst, nil
}

// EncodeEventNotification encodes an EventNotification payload.
func EncodeEventNotification(note EventNotification) ([]byte, error) {
	var dst []byte
	var err error
	dst, err = bacnet.AppendContextUnsigned(dst, 0, uint64(note.ProcessIdentifier))
	if err != nil {
		return nil, err
	}
	dst, err = bacnet.AppendContextObjectID(dst, 1, note.InitiatingDevice)
	if err != nil {
		return nil, err
	}
	dst, err = bacnet.AppendContextObjectID(dst, 2, note.EventObject)
	if err != nil {
		return nil, err
	}
	dst, err = appendTimeStampField(dst, 3, note.TimeStamp)
	if err != nil {
		return nil, err
	}
	dst, err = bacnet.AppendContextUnsigned(dst, 4, uint64(note.NotificationClass))
	if err != nil {
		return nil, err
	}
	dst, err = bacnet.AppendContextUnsigned(dst, 5, uint64(note.Priority))
	if err != nil {
		return nil, err
	}
	dst, err = bacnet.AppendContextUnsigned(dst, 6, uint64(note.EventType))
	if err != nil {
		return nil, err
	}
	if note.MessageText != nil {
		dst, err = bacnet.AppendContextCharacterString(dst, 7, *note.MessageText)
		if err != nil {
			return nil, err
		}
	}
	dst, err = bacnet.AppendContextUnsigned(dst, 8, uint64(note.NotifyType))
	if err != nil {
		return nil, err
	}
	if note.AckRequired != nil {
		dst, err = bacnet.AppendContextBool(dst, 9, *note.AckRequired)
		if err != nil {
			return nil, err
		}
	}
	if note.FromState != nil {
		dst, err = bacnet.AppendContextUnsigned(dst, 10, uint64(*note.FromState))
		if err != nil {
			return nil, err
		}
	}
	dst, err = bacnet.AppendContextUnsigned(dst, 11, uint64(note.ToState))
	if err != nil {
		return nil, err
	}
	switch {
	case note.Parameters != nil:
		els, encErr := EncodeNotificationParameters(*note.Parameters)
		if encErr != nil {
			return nil, encErr
		}
		dst, err = bacnet.AppendContextTagged(dst, 12, els)
	case note.NotificationParams != nil:
		if note.NotificationParams.Kind != bacnet.ValueConstructed {
			return nil, fmt.Errorf("%w: notificationParameters must be constructed", bacnet.ErrMalformed)
		}
		dst, err = bacnet.AppendContextTagged(dst, 12, note.NotificationParams.Elements)
	}
	return dst, err
}

// DecodeEventNotification decodes an EventNotification payload.
func DecodeEventNotification(payload []byte, limits bacnet.DecodeLimits) (EventNotification, error) {
	elements, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return EventNotification{}, err
	}
	if n != len(payload) {
		return EventNotification{}, fmt.Errorf("%w: EventNotification trailing data", bacnet.ErrTrailingData)
	}
	var note EventNotification
	var havePID, haveDev, haveObj, haveTS, haveClass, havePrio, haveType, haveNotify, haveTo bool
	for _, el := range elements {
		switch {
		case el.TagNumber == 0 && !bacnet.IsContextConstructed(el):
			u, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return EventNotification{}, err
			}
			if u > 0xFFFFFFFF {
				return EventNotification{}, fmt.Errorf("%w: processIdentifier overflow", bacnet.ErrMalformed)
			}
			note.ProcessIdentifier = uint32(u)
			havePID = true
		case el.TagNumber == 1 && !bacnet.IsContextConstructed(el):
			note.InitiatingDevice, err = bacnet.ContextObjectID(el)
			haveDev = true
		case el.TagNumber == 2 && !bacnet.IsContextConstructed(el):
			note.EventObject, err = bacnet.ContextObjectID(el)
			haveObj = true
		case el.TagNumber == 3 && bacnet.IsContextConstructed(el):
			if len(el.Value.Elements) != 1 {
				return EventNotification{}, fmt.Errorf("%w: TimeStamp wrapper", bacnet.ErrMalformed)
			}
			note.TimeStamp, err = DecodeTimeStamp(el.Value.Elements[0])
			haveTS = true
		case el.TagNumber == 4 && !bacnet.IsContextConstructed(el):
			u, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return EventNotification{}, err
			}
			if u > 0xFFFFFFFF {
				return EventNotification{}, fmt.Errorf("%w: notificationClass overflow", bacnet.ErrMalformed)
			}
			note.NotificationClass = uint32(u)
			haveClass = true
		case el.TagNumber == 5 && !bacnet.IsContextConstructed(el):
			u, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return EventNotification{}, err
			}
			if u > 0xFF {
				return EventNotification{}, fmt.Errorf("%w: priority overflow", bacnet.ErrMalformed)
			}
			note.Priority = uint8(u)
			havePrio = true
		case el.TagNumber == 6 && !bacnet.IsContextConstructed(el):
			u, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return EventNotification{}, err
			}
			note.EventType = uint32(u)
			haveType = true
		case el.TagNumber == 7 && !bacnet.IsContextConstructed(el):
			cs, err := bacnet.ContextCharacterString(el)
			if err != nil {
				return EventNotification{}, err
			}
			note.MessageText = &cs
		case el.TagNumber == 8 && !bacnet.IsContextConstructed(el):
			u, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return EventNotification{}, err
			}
			note.NotifyType = uint32(u)
			haveNotify = true
		case el.TagNumber == 9 && !bacnet.IsContextConstructed(el):
			b, err := bacnet.ContextBool(el)
			if err != nil {
				return EventNotification{}, err
			}
			note.AckRequired = &b
		case el.TagNumber == 10 && !bacnet.IsContextConstructed(el):
			u, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return EventNotification{}, err
			}
			v := uint32(u)
			note.FromState = &v
		case el.TagNumber == 11 && !bacnet.IsContextConstructed(el):
			u, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return EventNotification{}, err
			}
			note.ToState = uint32(u)
			haveTo = true
		case el.TagNumber == 12 && bacnet.IsContextConstructed(el):
			v := bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: el.Value.Elements}.Clone()
			note.NotificationParams = &v
			if params, pErr := DecodeNotificationParameters(el.Value.Elements); pErr == nil {
				note.Parameters = &params
			}
		default:
			return EventNotification{}, fmt.Errorf("%w: unexpected EventNotification tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
		if err != nil {
			return EventNotification{}, err
		}
	}
	if !havePID || !haveDev || !haveObj || !haveTS || !haveClass || !havePrio || !haveType || !haveNotify || !haveTo {
		return EventNotification{}, fmt.Errorf("%w: EventNotification missing required fields", bacnet.ErrMalformed)
	}
	return note, nil
}

const (
	ServiceConfirmedEventNotification   = apdu.ServiceConfirmedEventNotification
	ServiceUnconfirmedEventNotification = apdu.ServiceUnconfirmedEventNotification
)
