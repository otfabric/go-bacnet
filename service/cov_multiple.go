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

// SubscribeCOVPropertyMultipleRequest is a SubscribeCOVPropertyMultiple request:
//
//	subscriber-process-identifier [0] Unsigned32
//	issue-confirmed-notifications [1] BOOLEAN OPTIONAL
//	lifetime                      [2] Unsigned OPTIONAL
//	max-notification-delay        [3] Unsigned OPTIONAL
//	list-of-cov-subscription-specifications [4] SEQUENCE OF SEQUENCE {
//	  monitored-object-identifier [0] BACnetObjectIdentifier
//	  list-of-cov-references      [1] SEQUENCE OF SEQUENCE {
//	    monitored-property [0] BACnetPropertyReference
//	    cov-increment      [1] REAL OPTIONAL
//	    timestamped        [2] BOOLEAN } }
//
// A request without issue-confirmed-notifications and without lifetime
// cancels the subscription; with only one of the two it is inconsistent and
// a device refuses it.
//
// The layout is checked against Wireshark's dissector (make dissector) and
// against the request parsers of BACnet4J and BACpypes3 (INTEROP.md).
type SubscribeCOVPropertyMultipleRequest struct {
	SubscriberProcessIdentifier uint32
	// Cancellation omits issue-confirmed-notifications and lifetime, which
	// asks the device to cancel the subscription. IssueConfirmedNotifications
	// and LifetimeRemaining are then ignored.
	Cancellation                bool
	IssueConfirmedNotifications bool
	LifetimeRemaining           *uint32 // seconds; nil = absent
	MaxNotificationDelay        *uint32 // seconds; nil = absent
	Subscriptions               []COVMultipleSubscription
}

// EncodeSubscribeCOVPropertyMultiple encodes the request payload (see
// SubscribeCOVPropertyMultipleRequest for the layout).
func EncodeSubscribeCOVPropertyMultiple(req SubscribeCOVPropertyMultipleRequest) ([]byte, error) {
	if len(req.Subscriptions) == 0 {
		return nil, fmt.Errorf("%w: SubscribeCOVPropertyMultiple requires subscriptions", bacnet.ErrMalformed)
	}
	dst, err := bacnet.AppendContextUnsigned(nil, 0, uint64(req.SubscriberProcessIdentifier))
	if err != nil {
		return nil, err
	}
	if !req.Cancellation {
		dst, err = bacnet.AppendContextBool(dst, 1, req.IssueConfirmedNotifications)
		if err != nil {
			return nil, err
		}
	}
	if !req.Cancellation && req.LifetimeRemaining != nil {
		dst, err = bacnet.AppendContextUnsigned(dst, 2, uint64(*req.LifetimeRemaining))
		if err != nil {
			return nil, err
		}
	}
	if req.MaxNotificationDelay != nil {
		dst, err = bacnet.AppendContextUnsigned(dst, 3, uint64(*req.MaxNotificationDelay))
		if err != nil {
			return nil, err
		}
	}
	dst = append(dst, 0x4E) // opening list-of-cov-subscription-specifications [4]
	for _, sub := range req.Subscriptions {
		if len(sub.Properties) == 0 {
			return nil, fmt.Errorf("%w: SubscribeCOVPropertyMultiple subscription requires properties", bacnet.ErrMalformed)
		}
		dst, err = bacnet.AppendContextObjectID(dst, 0, sub.Object)
		if err != nil {
			return nil, err
		}
		dst = append(dst, 0x1E) // opening list-of-cov-references [1]
		for _, pref := range sub.Properties {
			dst = append(dst, 0x0E) // opening monitored-property [0]
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
			dst = append(dst, 0x0F) // closing monitored-property [0]
			if pref.COVIncrement != nil {
				dst, err = bacnet.AppendContextReal(dst, 1, *pref.COVIncrement)
				if err != nil {
					return nil, err
				}
			}
			dst, err = bacnet.AppendContextBool(dst, 2, pref.Timestamped)
			if err != nil {
				return nil, err
			}
		}
		dst = append(dst, 0x1F)
	}
	return append(dst, 0x4F), nil
}

// COVNotificationMultipleValue is one changed value in a COVNotificationMultiple:
//
//	property-identifier  [0]
//	property-array-index [1] OPTIONAL
//	property-value       [2]
//	time-of-change       [3] Time OPTIONAL
type COVNotificationMultipleValue struct {
	Property bacnet.PropertyReference
	Value    bacnet.ApplicationValue
	// TimeOfChange is time-of-change [3], a Time; nil when absent.
	TimeOfChange *bacnet.Time
	// TimeStamp is populated only when a peer sends the non-standard
	// constructed date+time form this library emitted up to v0.3.0. On encode
	// its Time is used as time-of-change when TimeOfChange is nil. Prefer
	// TimeOfChange; this field is expected to be removed.
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
			toc := v.TimeOfChange
			if toc == nil && v.TimeStamp != nil {
				toc = &v.TimeStamp.Time
			}
			if toc != nil {
				dst, err = bacnet.AppendContextTime(dst, 3, *toc)
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
			u, err := bacnet.ContextUnsigned32(el)
			if err != nil {
				return COVNotificationMultiple{}, err
			}
			note.SubscriberProcessIdentifier = u
			havePID = true
		case el.TagNumber == 1 && !bacnet.IsContextConstructed(el):
			id, err := bacnet.ContextObjectID(el)
			if err != nil {
				return COVNotificationMultiple{}, err
			}
			note.InitiatingDevice = id
			haveDev = true
		case el.TagNumber == 2 && !bacnet.IsContextConstructed(el):
			u, err := bacnet.ContextUnsigned32(el)
			if err != nil {
				return COVNotificationMultiple{}, err
			}
			note.TimeRemaining = u
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
	var haveProp, haveValue bool
	flush := func() error {
		if !haveProp {
			return nil
		}
		if !haveValue {
			return fmt.Errorf("%w: COVNotificationMultiple value missing", bacnet.ErrMalformed)
		}
		out = append(out, cur)
		cur = COVNotificationMultipleValue{}
		haveProp, haveValue = false, false
		return nil
	}
	for _, el := range els {
		switch {
		case el.TagNumber == 0 && !bacnet.IsContextConstructed(el):
			if err := flush(); err != nil {
				return nil, err
			}
			u, err := bacnet.ContextUnsigned32(el)
			if err != nil {
				return nil, err
			}
			cur.Property.Identifier = bacnet.PropertyIdentifier(u)
			haveProp = true
		case el.TagNumber == 1 && !bacnet.IsContextConstructed(el):
			if !haveProp {
				return nil, fmt.Errorf("%w: arrayIndex without property", bacnet.ErrMalformed)
			}
			idx, err := bacnet.ContextUnsigned32(el)
			if err != nil {
				return nil, err
			}
			cur.Property.ArrayIndex = &idx
		case el.TagNumber == 2 && bacnet.IsContextConstructed(el):
			if !haveProp || haveValue {
				return nil, fmt.Errorf("%w: misplaced property value", bacnet.ErrMalformed)
			}
			// Values own their memory: the payload buffer may be reused.
			if len(el.Value.Elements) == 1 && !el.Value.Elements[0].Context {
				cur.Value = el.Value.Elements[0].Value.Clone()
			} else {
				cur.Value = bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: el.Value.Elements}.Clone()
			}
			haveValue = true
		case el.TagNumber == 3 && !bacnet.IsContextConstructed(el):
			// time-of-change [3] Time.
			if !haveProp {
				return nil, fmt.Errorf("%w: time-of-change without property", bacnet.ErrMalformed)
			}
			tm, err := bacnet.ContextTime(el)
			if err != nil {
				return nil, err
			}
			cur.TimeOfChange = &tm
		case el.TagNumber == 3 && bacnet.IsContextConstructed(el):
			// Non-standard date+time form emitted by this library up to v0.3.0.
			if !haveProp {
				return nil, fmt.Errorf("%w: time-of-change without property", bacnet.ErrMalformed)
			}
			if len(el.Value.Elements) >= 2 {
				ts := DateTime{}
				if el.Value.Elements[0].Value.Kind == bacnet.ValueDate {
					ts.Date = el.Value.Elements[0].Value.Date
				}
				if el.Value.Elements[1].Value.Kind == bacnet.ValueTime {
					ts.Time = el.Value.Elements[1].Value.Time
				}
				cur.TimeStamp = &ts
				tm := ts.Time
				cur.TimeOfChange = &tm
			}
		default:
			return nil, fmt.Errorf("%w: unexpected value tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return out, nil
}
