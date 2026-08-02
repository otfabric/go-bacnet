// SPDX-License-Identifier: MIT

package service

import (
	"fmt"

	"github.com/otfabric/go-bacnet"
)

// COVPropertyReference is one property in a COVPropertyMultiple subscription.
type COVPropertyReference struct {
	Property     bacnet.PropertyReference
	COVIncrement *float32
	Timestamped  bool
}

// COVMultipleSubscription is one object subscription in SubscribeCOVPropertyMultiple.
type COVMultipleSubscription struct {
	Object     bacnet.ObjectIdentifier
	Properties []COVPropertyReference
}

// SubscribeCOVPropertyMultipleRequest is a SubscribeCOVPropertyMultiple request.
type SubscribeCOVPropertyMultipleRequest struct {
	SubscriberProcessIdentifier uint32
	IssueConfirmedNotifications bool
	LifetimeRemaining           *uint32 // nil = cancel / unspecified per peer
	Subscriptions               []COVMultipleSubscription
}

// EncodeSubscribeCOVPropertyMultiple encodes the request payload.
func EncodeSubscribeCOVPropertyMultiple(req SubscribeCOVPropertyMultipleRequest) ([]byte, error) {
	if len(req.Subscriptions) == 0 {
		return nil, fmt.Errorf("%w: SubscribeCOVPropertyMultiple requires subscriptions", bacnet.ErrMalformed)
	}
	dst, err := bacnet.AppendContextUnsigned(nil, 0, uint64(req.SubscriberProcessIdentifier))
	if err != nil {
		return nil, err
	}
	dst, err = bacnet.AppendContextBool(dst, 1, req.IssueConfirmedNotifications)
	if err != nil {
		return nil, err
	}
	if req.LifetimeRemaining != nil {
		dst, err = bacnet.AppendContextUnsigned(dst, 2, uint64(*req.LifetimeRemaining))
		if err != nil {
			return nil, err
		}
	}
	dst = append(dst, 0x3E) // opening listOfCOVSubscriptionSpecifications [3]
	for _, sub := range req.Subscriptions {
		dst, err = bacnet.AppendContextObjectID(dst, 0, sub.Object)
		if err != nil {
			return nil, err
		}
		dst = append(dst, 0x1E) // opening listOfCOVReferences [1]
		for _, pref := range sub.Properties {
			dst, err = bacnet.AppendContextUnsigned(dst, 0, uint64(pref.Property.Identifier))
			if err != nil {
				return nil, err
			}
			if pref.Property.ArrayIndex != nil {
				dst, err = bacnet.AppendContextUnsigned(dst, 1, uint64(*pref.Property.ArrayIndex))
				if err != nil {
					return nil, err
				}
			}
			if pref.COVIncrement != nil {
				dst, err = bacnet.AppendContextTagged(dst, 2, []bacnet.Element{
					{Value: bacnet.RealValue(*pref.COVIncrement)},
				})
				if err != nil {
					return nil, err
				}
			}
			dst, err = bacnet.AppendContextBool(dst, 3, pref.Timestamped)
			if err != nil {
				return nil, err
			}
		}
		dst = append(dst, 0x1F)
	}
	dst = append(dst, 0x3F)
	return dst, nil
}

// COVNotificationMultipleValue is one changed value in a COVNotificationMultiple.
type COVNotificationMultipleValue struct {
	Property  bacnet.PropertyReference
	Value     bacnet.ApplicationValue
	TimeStamp *DateTime
}

// COVNotificationMultipleObject groups values for one monitored object.
type COVNotificationMultipleObject struct {
	Object bacnet.ObjectIdentifier
	Values []COVNotificationMultipleValue
}

// COVNotificationMultiple is a Confirmed/Unconfirmed COVNotificationMultiple payload.
type COVNotificationMultiple struct {
	SubscriberProcessIdentifier uint32
	InitiatingDevice            bacnet.ObjectIdentifier
	TimeRemaining               uint32
	Timestamp                   *DateTime
	Objects                     []COVNotificationMultipleObject
}

// EncodeCOVNotificationMultiple encodes a Confirmed/Unconfirmed COVNotificationMultiple payload.
func EncodeCOVNotificationMultiple(note COVNotificationMultiple) ([]byte, error) {
	dst, err := bacnet.AppendContextUnsigned(nil, 0, uint64(note.SubscriberProcessIdentifier))
	if err != nil {
		return nil, err
	}
	dst, err = bacnet.AppendContextObjectID(dst, 1, note.InitiatingDevice)
	if err != nil {
		return nil, err
	}
	dst, err = bacnet.AppendContextUnsigned(dst, 2, uint64(note.TimeRemaining))
	if err != nil {
		return nil, err
	}
	if note.Timestamp != nil {
		dst, err = bacnet.AppendContextTagged(dst, 3, []bacnet.Element{
			{Value: bacnet.ApplicationValue{Kind: bacnet.ValueDate, Date: note.Timestamp.Date}},
			{Value: bacnet.ApplicationValue{Kind: bacnet.ValueTime, Time: note.Timestamp.Time}},
		})
		if err != nil {
			return nil, err
		}
	}
	dst = append(dst, 0x4E) // opening listOfCOVNotifications [4]
	for _, obj := range note.Objects {
		dst, err = bacnet.AppendContextObjectID(dst, 0, obj.Object)
		if err != nil {
			return nil, err
		}
		dst = append(dst, 0x1E) // opening listOfValues [1]
		for _, v := range obj.Values {
			dst, err = bacnet.AppendContextUnsigned(dst, 0, uint64(v.Property.Identifier))
			if err != nil {
				return nil, err
			}
			if v.Property.ArrayIndex != nil {
				dst, err = bacnet.AppendContextUnsigned(dst, 1, uint64(*v.Property.ArrayIndex))
				if err != nil {
					return nil, err
				}
			}
			dst, err = bacnet.AppendContextTagged(dst, 2, []bacnet.Element{{Value: v.Value}})
			if err != nil {
				return nil, err
			}
			if v.TimeStamp != nil {
				dst, err = bacnet.AppendContextTagged(dst, 3, []bacnet.Element{
					{Value: bacnet.ApplicationValue{Kind: bacnet.ValueDate, Date: v.TimeStamp.Date}},
					{Value: bacnet.ApplicationValue{Kind: bacnet.ValueTime, Time: v.TimeStamp.Time}},
				})
				if err != nil {
					return nil, err
				}
			}
		}
		dst = append(dst, 0x1F)
	}
	return append(dst, 0x4F), nil
}

// DecodeCOVNotificationMultiple decodes a COVNotificationMultiple payload.
func DecodeCOVNotificationMultiple(payload []byte, limits bacnet.DecodeLimits) (COVNotificationMultiple, error) {
	els, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return COVNotificationMultiple{}, err
	}
	if n != len(payload) {
		return COVNotificationMultiple{}, fmt.Errorf("%w: COVNotificationMultiple trailing", bacnet.ErrTrailingData)
	}
	var note COVNotificationMultiple
	var havePID, haveDev, haveTime bool
	for _, el := range els {
		switch {
		case el.TagNumber == 0 && !bacnet.IsContextConstructed(el):
			u, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return COVNotificationMultiple{}, err
			}
			note.SubscriberProcessIdentifier = uint32(u)
			havePID = true
		case el.TagNumber == 1 && !bacnet.IsContextConstructed(el):
			id, err := bacnet.ContextObjectID(el)
			if err != nil {
				return COVNotificationMultiple{}, err
			}
			note.InitiatingDevice = id
			haveDev = true
		case el.TagNumber == 2 && !bacnet.IsContextConstructed(el):
			u, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return COVNotificationMultiple{}, err
			}
			note.TimeRemaining = uint32(u)
			haveTime = true
		case el.TagNumber == 3 && bacnet.IsContextConstructed(el):
			// timestamp optional BACnetDateTime
			if len(el.Value.Elements) >= 2 {
				ts := DateTime{}
				if el.Value.Elements[0].Value.Kind == bacnet.ValueDate {
					ts.Date = el.Value.Elements[0].Value.Date
				}
				if el.Value.Elements[1].Value.Kind == bacnet.ValueTime {
					ts.Time = el.Value.Elements[1].Value.Time
				}
				note.Timestamp = &ts
			}
		case el.TagNumber == 4 && bacnet.IsContextConstructed(el):
			objs, err := decodeCOVMultipleObjects(el.Value.Elements, limits)
			if err != nil {
				return COVNotificationMultiple{}, err
			}
			note.Objects = objs
		default:
			return COVNotificationMultiple{}, fmt.Errorf("%w: unexpected COVNotificationMultiple tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
	}
	if !havePID || !haveDev || !haveTime {
		return COVNotificationMultiple{}, fmt.Errorf("%w: COVNotificationMultiple missing required fields", bacnet.ErrMalformed)
	}
	return note, nil
}

func decodeCOVMultipleObjects(els []bacnet.Element, limits bacnet.DecodeLimits) ([]COVNotificationMultipleObject, error) {
	var out []COVNotificationMultipleObject
	var cur *COVNotificationMultipleObject
	for _, el := range els {
		switch {
		case el.TagNumber == 0 && !bacnet.IsContextConstructed(el):
			id, err := bacnet.ContextObjectID(el)
			if err != nil {
				return nil, err
			}
			out = append(out, COVNotificationMultipleObject{Object: id})
			cur = &out[len(out)-1]
		case el.TagNumber == 1 && bacnet.IsContextConstructed(el):
			if cur == nil {
				return nil, fmt.Errorf("%w: listOfValues without object", bacnet.ErrMalformed)
			}
			vals, err := decodeCOVMultipleValues(el.Value.Elements, limits)
			if err != nil {
				return nil, err
			}
			cur.Values = vals
		default:
			return nil, fmt.Errorf("%w: unexpected object list tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
	}
	return out, nil
}

func decodeCOVMultipleValues(els []bacnet.Element, _ bacnet.DecodeLimits) ([]COVNotificationMultipleValue, error) {
	var out []COVNotificationMultipleValue
	var cur COVNotificationMultipleValue
	var haveProp bool
	flush := func() {
		if haveProp {
			out = append(out, cur)
			cur = COVNotificationMultipleValue{}
			haveProp = false
		}
	}
	for _, el := range els {
		switch {
		case el.TagNumber == 0 && !bacnet.IsContextConstructed(el):
			flush()
			u, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return nil, err
			}
			cur.Property.Identifier = bacnet.PropertyIdentifier(u)
			haveProp = true
		case el.TagNumber == 1 && !bacnet.IsContextConstructed(el):
			u, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return nil, err
			}
			idx := uint32(u)
			cur.Property.ArrayIndex = &idx
		case el.TagNumber == 2 && bacnet.IsContextConstructed(el):
			if len(el.Value.Elements) == 1 {
				cur.Value = el.Value.Elements[0].Value
			} else {
				cur.Value = bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: el.Value.Elements}
			}
		case el.TagNumber == 3 && bacnet.IsContextConstructed(el):
			if len(el.Value.Elements) >= 2 {
				ts := DateTime{}
				if el.Value.Elements[0].Value.Kind == bacnet.ValueDate {
					ts.Date = el.Value.Elements[0].Value.Date
				}
				if el.Value.Elements[1].Value.Kind == bacnet.ValueTime {
					ts.Time = el.Value.Elements[1].Value.Time
				}
				cur.TimeStamp = &ts
			}
		default:
			return nil, fmt.Errorf("%w: unexpected value tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
	}
	flush()
	return out, nil
}
