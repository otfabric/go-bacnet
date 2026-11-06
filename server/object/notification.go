// SPDX-License-Identifier: MIT

package object

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/otfabric/go-bacnet"
)

// Event transitions (BACnetEventTransitionBits) as bit positions.
const (
	TransitionToOffNormal = 0
	TransitionToFault     = 1
	TransitionToNormal    = 2
)

// List errors (clause 15.1, 15.2).
var (
	ErrPropertyIsNotAList    = Err(bacnet.ErrorClassServices, bacnet.ErrorCodePropertyIsNotAList)
	ErrListElementNotFound   = Err(bacnet.ErrorClassServices, bacnet.ErrorCodeListElementNotFound)
	ErrNoSpaceForListElement = Err(bacnet.ErrorClassResources, bacnet.ErrorCodeNoSpaceToAddListElement)
)

// ListError is a list modification that failed at element First (1-based),
// for the ChangeList-Error of AddListElement and RemoveListElement.
type ListError struct {
	Err   error
	First uint32
}

func (e *ListError) Error() string { return fmt.Sprintf("list element %d: %v", e.First, e.Err) }
func (e *ListError) Unwrap() error { return e.Err }

// ListModifier is implemented by objects with list properties that
// AddListElement and RemoveListElement change. Both are all or nothing.
type ListModifier interface {
	AddListElements(ctx context.Context, prop bacnet.PropertyReference, elements []bacnet.Element) error
	RemoveListElements(ctx context.Context, prop bacnet.PropertyReference, elements []bacnet.Element) error
}

// Destination is a BACnetDestination of a Recipient_List (clause 21).
// Exactly one of Device and Address is set; an Address with an empty MAC
// is a broadcast on its network (network 0 is the local network).
type Destination struct {
	ValidDays   uint8 // bit 0 Monday … bit 6 Sunday
	From, To    bacnet.Time
	Device      *bacnet.ObjectIdentifier
	Network     uint16
	MAC         []byte
	ProcessID   uint32
	Confirmed   bool
	Transitions uint8 // bit TransitionTo…
}

// Address returns the network address of an address recipient.
func (d Destination) Address() (bacnet.Address, error) {
	if len(d.MAC) == 0 {
		if d.Network == 0 {
			return bacnet.LocalBroadcast(), nil
		}
		return bacnet.RemoteBroadcast(d.Network), nil
	}
	mac, err := bacnet.NewMAC(d.MAC)
	if err != nil {
		return bacnet.Address{}, err
	}
	if d.Network == 0 {
		return bacnet.LocalStation(mac), nil
	}
	return bacnet.RemoteStation(d.Network, mac), nil
}

// Active reports whether the destination receives a transition at t:
// the weekday is valid, the time is within From..To (inclusive), and the
// transition is selected.
func (d Destination) Active(t time.Time, transition int) bool {
	wd := (int(t.Weekday()) + 6) % 7 // Monday = 0
	if d.ValidDays&(1<<wd) == 0 || d.Transitions&(1<<transition) == 0 {
		return false
	}
	now := timeOf(t)
	return !timeBefore(now, d.From) && !timeBefore(d.To, now)
}

func timeBefore(a, b bacnet.Time) bool {
	av := [4]uint8{a.Hour, a.Minute, a.Second, a.Hundredths}
	bv := [4]uint8{b.Hour, b.Minute, b.Second, b.Hundredths}
	for i := range av {
		// A wildcard field matches anything.
		if av[i] == 0xFF || bv[i] == 0xFF {
			continue
		}
		if av[i] != bv[i] {
			return av[i] < bv[i]
		}
	}
	return false
}

// maskBits returns a bit string of n bits from a mask (bit i set when
// mask bit i is set).
func maskBits(n int, mask uint8) bacnet.ApplicationValue {
	return bitString(n, func(i int) bool { return mask&(1<<i) != 0 })
}

func bitsOf(bs bacnet.BitString, n int) uint8 {
	var v uint8
	for i := 0; i < n; i++ {
		if i/8 < len(bs.Bytes) && bs.Bytes[i/8]&(0x80>>(i%8)) != 0 {
			v |= 1 << i
		}
	}
	return v
}

// Elements encodes the destination as the seven elements of its SEQUENCE.
func (d Destination) Elements() []bacnet.Element {
	var recipient bacnet.Element
	if d.Device != nil {
		raw, _ := bacnet.AppendContextObjectID(nil, 0, *d.Device)
		el, _, _ := bacnet.ParseTag(raw, bacnet.DefaultDecodeLimits())
		recipient = el
	} else {
		recipient = bacnet.Element{Context: true, TagNumber: 1, Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{
			{Value: bacnet.UnsignedValue(uint64(d.Network))}, {Value: bacnet.ApplicationValue{Kind: bacnet.ValueOctetString, OctetString: append([]byte(nil), d.MAC...)}},
		}}}
	}
	return []bacnet.Element{
		{Value: maskBits(7, d.ValidDays)},
		{Value: bacnet.TimeValue(d.From)},
		{Value: bacnet.TimeValue(d.To)},
		recipient,
		{Value: bacnet.UnsignedValue(uint64(d.ProcessID))},
		{Value: bacnet.BoolValue(d.Confirmed)},
		{Value: maskBits(3, d.Transitions)},
	}
}

// ParseDestinations decodes a list of BACnetDestination from flat
// elements. It returns the 1-based number of the first element that is not
// a valid destination with the error.
func ParseDestinations(els []bacnet.Element) ([]Destination, uint32, error) {
	if len(els)%7 != 0 {
		return nil, uint32(len(els)/7 + 1), ErrInvalidDataType
	}
	out := make([]Destination, 0, len(els)/7)
	for i := 0; i < len(els); i += 7 {
		d, ok := parseDestination(els[i : i+7])
		if !ok {
			return nil, uint32(i/7 + 1), ErrInvalidDataType
		}
		out = append(out, d)
	}
	return out, 0, nil
}

func parseDestination(e []bacnet.Element) (Destination, bool) {
	app := func(i int, k bacnet.ValueKind) bool { return !e[i].Context && e[i].Value.Kind == k }
	if !app(0, bacnet.ValueBitString) || !app(1, bacnet.ValueTime) || !app(2, bacnet.ValueTime) ||
		!app(4, bacnet.ValueUnsigned) || !app(5, bacnet.ValueBoolean) || !app(6, bacnet.ValueBitString) ||
		e[4].Value.Unsigned > 0xFFFFFFFF {
		return Destination{}, false
	}
	d := Destination{
		ValidDays: bitsOf(e[0].Value.BitString, 7), From: e[1].Value.Time, To: e[2].Value.Time,
		ProcessID: uint32(e[4].Value.Unsigned), Confirmed: e[5].Value.Boolean, Transitions: bitsOf(e[6].Value.BitString, 3),
	}
	r := e[3]
	switch {
	case r.Context && r.TagNumber == 0 && !bacnet.IsContextConstructed(r):
		id, err := bacnet.ContextObjectID(r)
		if err != nil {
			return Destination{}, false
		}
		d.Device = &id
	case r.Context && r.TagNumber == 1 && bacnet.IsContextConstructed(r):
		in := r.Value.Elements
		if len(in) != 2 || in[0].Context || in[0].Value.Kind != bacnet.ValueUnsigned || in[0].Value.Unsigned > 0xFFFF ||
			in[1].Context || in[1].Value.Kind != bacnet.ValueOctetString {
			return Destination{}, false
		}
		d.Network = uint16(in[0].Value.Unsigned)
		d.MAC = append([]byte(nil), in[1].Value.OctetString...)
		if len(d.MAC) > 0 {
			if _, err := bacnet.NewMAC(d.MAC); err != nil {
				return Destination{}, false
			}
		}
	default:
		return Destination{}, false
	}
	return d, true
}

func (d Destination) equal(o Destination) bool {
	if (d.Device == nil) != (o.Device == nil) || (d.Device != nil && *d.Device != *o.Device) {
		return false
	}
	return d.ValidDays == o.ValidDays && d.From == o.From && d.To == o.To && d.Network == o.Network &&
		bytes.Equal(d.MAC, o.MAC) && d.ProcessID == o.ProcessID && d.Confirmed == o.Confirmed && d.Transitions == o.Transitions
}

// NotificationClassConfig configures a Notification Class object.
type NotificationClassConfig struct {
	Instance    uint32
	Name        string
	Description string
	// Priority holds the priorities of to-offnormal, to-fault and
	// to-normal notifications (clause 12.21 Priority: Unsigned 0..255, where 0
	// is the most urgent and a valid value). Nil starts all three at 255,
	// the lowest, as bacnet-stack does (nc.c Notification_Class_Init).
	Priority    *[3]uint8
	AckRequired uint8 // bit TransitionTo…
	Recipients  []Destination
	// MaxRecipients bounds Recipient_List; zero means 32.
	MaxRecipients int
}

// NotificationClass is a Notification Class object (clause 12.21). Its
// Notification_Class number is its instance.
type NotificationClass struct {
	*Base
	cfg        NotificationClassConfig
	priority   [3]uint8
	ackReq     uint8
	recipients []Destination
}

// NewNotificationClass returns a Notification Class object.
func NewNotificationClass(cfg NotificationClassConfig) (*NotificationClass, error) {
	if cfg.Name == "" {
		return nil, errorf("object name required")
	}
	if cfg.Instance >= bacnet.MaxObjectInstance {
		return nil, errorf("instance %d out of range", cfg.Instance)
	}
	if cfg.MaxRecipients <= 0 {
		cfg.MaxRecipients = 32
	}
	if len(cfg.Recipients) > cfg.MaxRecipients {
		return nil, errorf("%d recipients, at most %d", len(cfg.Recipients), cfg.MaxRecipients)
	}
	nc := &NotificationClass{Base: NewBase(bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeNotificationClass, Instance: cfg.Instance}, cfg.Name), cfg: cfg, ackReq: cfg.AckRequired & 7}
	nc.priority = [3]uint8{255, 255, 255}
	if cfg.Priority != nil {
		nc.priority = *cfg.Priority
	}
	nc.recipients = append(nc.recipients, cfg.Recipients...)
	if cfg.Description != "" {
		nc.Define(bacnet.PropertyDescription, &Property{Value: charString(cfg.Description), Writable: true, Kinds: []bacnet.ValueKind{bacnet.ValueCharacterString}})
	}
	nc.Define(bacnet.PropertyNotificationClass, &Property{Required: true, Value: bacnet.UnsignedValue(uint64(cfg.Instance))})
	nc.Define(bacnet.PropertyPriority, &Property{Required: true, Array: true, Writable: true,
		Get: func() bacnet.ApplicationValue {
			els := make([]bacnet.Element, 3)
			for i, p := range nc.priority {
				els[i] = bacnet.Element{Value: bacnet.UnsignedValue(uint64(p))}
			}
			return bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: els}
		},
		Set: func(w Write) ([]bacnet.PropertyIdentifier, error) {
			var vals []bacnet.ApplicationValue
			switch {
			case w.Property.ArrayIndex != nil:
				i := *w.Property.ArrayIndex
				if i == 0 || i > 3 {
					return nil, ErrInvalidArrayIndex
				}
				vals = []bacnet.ApplicationValue{w.Value}
			case w.Value.Kind == bacnet.ValueConstructed && len(w.Value.Elements) == 3:
				for _, el := range w.Value.Elements {
					if el.Context {
						return nil, ErrInvalidDataType
					}
					vals = append(vals, el.Value)
				}
			default:
				return nil, ErrInvalidDataType
			}
			next := nc.priority
			for i, v := range vals {
				if v.Kind != bacnet.ValueUnsigned {
					return nil, ErrInvalidDataType
				}
				if v.Unsigned > 255 {
					return nil, ErrValueOutOfRange
				}
				slot := i
				if w.Property.ArrayIndex != nil {
					slot = int(*w.Property.ArrayIndex) - 1
				}
				next[slot] = uint8(v.Unsigned)
			}
			nc.priority = next
			return []bacnet.PropertyIdentifier{bacnet.PropertyPriority}, nil
		}})
	nc.Define(bacnet.PropertyAckRequired, &Property{Required: true, Writable: true,
		Get: func() bacnet.ApplicationValue { return maskBits(3, nc.ackReq) },
		Set: func(w Write) ([]bacnet.PropertyIdentifier, error) {
			if w.Property.ArrayIndex != nil {
				return nil, ErrNotAnArray
			}
			if w.Value.Kind != bacnet.ValueBitString {
				return nil, ErrInvalidDataType
			}
			nc.ackReq = bitsOf(w.Value.BitString, 3)
			return []bacnet.PropertyIdentifier{bacnet.PropertyAckRequired}, nil
		}})
	nc.Define(bacnet.PropertyRecipientList, &Property{Required: true, Writable: true,
		Get: func() bacnet.ApplicationValue {
			var els []bacnet.Element
			for _, d := range nc.recipients {
				els = append(els, d.Elements()...)
			}
			return bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: els}
		},
		Set: func(w Write) ([]bacnet.PropertyIdentifier, error) {
			if w.Property.ArrayIndex != nil {
				return nil, ErrNotAnArray
			}
			if w.Value.Kind != bacnet.ValueConstructed {
				return nil, ErrInvalidDataType
			}
			ds, _, err := ParseDestinations(w.Value.Elements)
			if err != nil {
				return nil, err
			}
			if len(ds) > nc.cfg.MaxRecipients {
				return nil, ErrNoSpaceToWriteProperty
			}
			nc.recipients = ds
			return []bacnet.PropertyIdentifier{bacnet.PropertyRecipientList}, nil
		}})
	return nc, nil
}

// ErrNoSpaceToWriteProperty answers a write of a list longer than the
// object holds.
var ErrNoSpaceToWriteProperty = Err(bacnet.ErrorClassResources, bacnet.ErrorCodeNoSpaceToWriteProperty)

// Class returns the Notification_Class number.
func (nc *NotificationClass) Class() uint32 { return nc.cfg.Instance }

// Routing returns the notification priority of a transition, whether it
// needs an acknowledgment, and the destinations that receive it at t.
func (nc *NotificationClass) Routing(t time.Time, transition int) (priority uint8, ackRequired bool, to []Destination) {
	nc.View(func() {
		priority = nc.priority[transition]
		ackRequired = nc.ackReq&(1<<transition) != 0
		for _, d := range nc.recipients {
			if d.Active(t, transition) {
				to = append(to, d)
			}
		}
	})
	return priority, ackRequired, to
}

// AckRequired returns Ack_Required as transition bits.
func (nc *NotificationClass) AckRequired() uint8 {
	var v uint8
	nc.View(func() { v = nc.ackReq })
	return v
}

// Priorities returns Priority.
func (nc *NotificationClass) Priorities() [3]uint8 {
	var v [3]uint8
	nc.View(func() { v = nc.priority })
	return v
}

// HasRecipient reports whether Recipient_List holds a destination for the
// recipient process: the same recipient (the same device, or the same
// network and MAC) and process identifier, whatever its days, times,
// confirmation and transitions. It is the enrollment filter of
// GetEnrollmentSummary (clause 13.11.1.2).
func (nc *NotificationClass) HasRecipient(r Destination) bool {
	var found bool
	nc.View(func() {
		for _, d := range nc.recipients {
			if d.sameRecipientProcess(r) {
				found = true
				return
			}
		}
	})
	return found
}

func (d Destination) sameRecipientProcess(o Destination) bool {
	if d.ProcessID != o.ProcessID || (d.Device == nil) != (o.Device == nil) {
		return false
	}
	if d.Device != nil {
		return *d.Device == *o.Device
	}
	return d.Network == o.Network && bytes.Equal(d.MAC, o.MAC)
}

func (nc *NotificationClass) listProperty(prop bacnet.PropertyReference) error {
	if prop.Identifier != bacnet.PropertyRecipientList {
		if _, err := nc.ReadProperty(context.Background(), bacnet.PropertyReference{Identifier: prop.Identifier}); err != nil {
			return err
		}
		return ErrPropertyIsNotAList
	}
	if prop.ArrayIndex != nil {
		return ErrNotAnArray
	}
	return nil
}

// AddListElements implements ListModifier for Recipient_List; a
// destination already in the list is not added again (clause 15.1).
func (nc *NotificationClass) AddListElements(_ context.Context, prop bacnet.PropertyReference, els []bacnet.Element) error {
	if err := nc.listProperty(prop); err != nil {
		return err
	}
	ds, first, err := ParseDestinations(els)
	if err != nil {
		return &ListError{Err: err, First: first}
	}
	var err2 error
	nc.Update(func() []bacnet.PropertyIdentifier {
		next := append([]Destination(nil), nc.recipients...)
	outer:
		for i, d := range ds {
			for _, have := range next {
				if have.equal(d) {
					continue outer
				}
			}
			if len(next) >= nc.cfg.MaxRecipients {
				err2 = &ListError{Err: ErrNoSpaceForListElement, First: uint32(i + 1)}
				return nil
			}
			next = append(next, d)
		}
		nc.recipients = next
		return []bacnet.PropertyIdentifier{bacnet.PropertyRecipientList}
	})
	return err2
}

// RemoveListElements implements ListModifier for Recipient_List; a
// destination that is not in the list fails the request and nothing is
// removed (clause 15.2).
func (nc *NotificationClass) RemoveListElements(_ context.Context, prop bacnet.PropertyReference, els []bacnet.Element) error {
	if err := nc.listProperty(prop); err != nil {
		return err
	}
	ds, first, err := ParseDestinations(els)
	if err != nil {
		return &ListError{Err: err, First: first}
	}
	var err2 error
	nc.Update(func() []bacnet.PropertyIdentifier {
		next := append([]Destination(nil), nc.recipients...)
		for i, d := range ds {
			found := -1
			for j, have := range next {
				if have.equal(d) {
					found = j
					break
				}
			}
			if found < 0 {
				err2 = &ListError{Err: ErrListElementNotFound, First: uint32(i + 1)}
				return nil
			}
			next = append(next[:found], next[found+1:]...)
		}
		nc.recipients = next
		return []bacnet.PropertyIdentifier{bacnet.PropertyRecipientList}
	})
	return err2
}
