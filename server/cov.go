// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/internal/clock"
	"github.com/otfabric/go-bacnet/internal/diag"
	"github.com/otfabric/go-bacnet/internal/link"
	"github.com/otfabric/go-bacnet/server/object"
	"github.com/otfabric/go-bacnet/service"
)

// covEngine holds COV subscriptions (clauses 13.1, 13.14–13.17) and sends
// their notifications. A subscription is identified by subscriber address,
// process identifier and monitored object (and property); subscribing again
// replaces it, and a cancellation without a match succeeds (clause 13.14,
// as bacnet-stack implements it). After every successful subscription an
// initial notification is sent.
type covEngine struct {
	s    *Server
	mu   sync.Mutex
	subs map[string]*covSub
}

type subKind int

const (
	subObject subKind = iota
	subProperty
	subMultiple
)

type covRef struct {
	prop        bacnet.PropertyReference
	increment   *float32
	timestamped bool
	last        bacnet.ApplicationValue
	haveLast    bool
	changedAt   bacnet.Time
}

type covTarget struct {
	obj  bacnet.ObjectIdentifier
	refs []*covRef
}

type covSub struct {
	kind      subKind
	key       string
	to        bip.Endpoint
	dest      bacnet.Address
	recipient peer
	pid       uint32
	confirmed bool
	lifetime  uint32
	expires   time.Time // zero: indefinite
	targets   []*covTarget

	mu      sync.Mutex
	dirty   bool
	initial bool
	running bool
	closed  bool
}

func newCOVEngine(s *Server) *covEngine {
	return &covEngine{s: s, subs: make(map[string]*covSub)}
}

func (e *covEngine) close() {
	e.mu.Lock()
	for _, sub := range e.subs {
		sub.mu.Lock()
		sub.closed = true
		sub.mu.Unlock()
	}
	e.subs = make(map[string]*covSub)
	e.mu.Unlock()
}

func subKey(kind subKind, p peer, pid uint32, obj bacnet.ObjectIdentifier, prop *bacnet.PropertyReference) string {
	k := fmt.Sprintf("%d|%s|%s|%d|%v", kind, p.origin, p.source, pid, obj)
	if prop != nil {
		k += fmt.Sprintf("|%d", prop.Identifier)
		if prop.ArrayIndex != nil {
			k += fmt.Sprintf("[%d]", *prop.ArrayIndex)
		}
	}
	return k
}

var (
	errCOVNotSupported = object.Err(bacnet.ErrorClassObject, bacnet.ErrorCodeOptionalFunctionalityNotSupported)
	errCOVNoSpace      = object.Err(bacnet.ErrorClassResources, bacnet.ErrorCodeNoSpaceToAddListElement)
	errCOVLifetime     = object.Err(bacnet.ErrorClassServices, bacnet.ErrorCodeParameterOutOfRange)
)

// upsert adds or replaces a subscription. It returns the subscription to
// notify initially.
func (e *covEngine) upsert(sub *covSub) (*covSub, error) {
	s := e.s
	if s.cfg.maxLifetime > 0 && (sub.lifetime == 0 || time.Duration(sub.lifetime)*time.Second > s.cfg.maxLifetime) {
		return nil, errCOVLifetime
	}
	if sub.lifetime > 0 {
		sub.expires = s.clock.Now().Add(time.Duration(sub.lifetime) * time.Second)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if old, ok := e.subs[sub.key]; ok {
		old.mu.Lock()
		old.closed = true
		old.mu.Unlock()
	} else if len(e.subs) >= s.cfg.maxCOV {
		return nil, errCOVNoSpace
	}
	e.subs[sub.key] = sub
	return sub, nil
}

func (e *covEngine) cancel(key string) {
	e.mu.Lock()
	if sub, ok := e.subs[key]; ok {
		sub.mu.Lock()
		sub.closed = true
		sub.mu.Unlock()
		delete(e.subs, key)
	}
	e.mu.Unlock()
}

func (e *covEngine) expire(now time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for k, sub := range e.subs {
		if !sub.expires.IsZero() && !now.Before(sub.expires) {
			sub.mu.Lock()
			sub.closed = true
			sub.mu.Unlock()
			delete(e.subs, k)
		}
	}
}

// objectRemoved drops a deleted object from the subscriptions; one that
// has no object left ends.
func (e *covEngine) objectRemoved(id bacnet.ObjectIdentifier) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for k, sub := range e.subs {
		sub.mu.Lock()
		kept := sub.targets[:0:0]
		for _, t := range sub.targets {
			if t.obj != id {
				kept = append(kept, t)
			}
		}
		sub.targets = kept
		if len(kept) == 0 {
			sub.closed = true
			delete(e.subs, k)
		}
		sub.mu.Unlock()
	}
}

func (e *covEngine) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.subs)
}

// changed is the Device observer: it marks subscriptions on the changed
// object and starts their senders. It never blocks.
func (e *covEngine) changed(c object.Change) {
	e.mu.Lock()
	var hit []*covSub
	for _, sub := range e.subs {
		for _, t := range sub.targets {
			if t.obj == c.Object {
				hit = append(hit, sub)
				break
			}
		}
	}
	e.mu.Unlock()
	for _, sub := range hit {
		e.kick(sub, false)
	}
}

// kick schedules an evaluation of sub; initial forces a notification.
func (e *covEngine) kick(sub *covSub, initial bool) {
	sub.mu.Lock()
	if sub.closed {
		sub.mu.Unlock()
		return
	}
	sub.dirty = true
	sub.initial = sub.initial || initial
	if sub.running {
		sub.mu.Unlock()
		return
	}
	sub.running = true
	sub.mu.Unlock()
	e.s.req.async(func() { e.run(sub) })
}

// run evaluates and sends until nothing is pending: notifications of one
// subscription go out in order, and changes made meanwhile coalesce.
func (e *covEngine) run(sub *covSub) {
	for {
		sub.mu.Lock()
		if !sub.dirty || sub.closed {
			sub.running = false
			sub.mu.Unlock()
			return
		}
		sub.dirty = false
		initial := sub.initial
		sub.initial = false
		sub.mu.Unlock()
		e.evaluate(sub, initial)
	}
}

func (e *covEngine) evaluate(sub *covSub, initial bool) {
	s := e.s
	ctx := s.ctx
	now := s.clock.Now()
	type objValues struct {
		obj    bacnet.ObjectIdentifier
		values []service.PropertyValue
		refs   []*covRef
	}
	var send []objValues
	trigger := initial
	sub.mu.Lock()
	targets := sub.targets // replaced, never changed in place
	sub.mu.Unlock()
	for _, t := range targets {
		o, ok := s.dev.Object(t.obj)
		if !ok {
			continue
		}
		ov := objValues{obj: t.obj}
		inc, analog := float32(0), false
		if r, ok := o.(object.COVReporter); ok {
			inc, analog = r.COVIncrement()
		}
		for _, ref := range t.refs {
			v, err := o.ReadProperty(ctx, ref.prop)
			if err != nil {
				continue
			}
			if !ref.haveLast || significant(ref, v, inc, analog) {
				trigger = true
				ref.changedAt = timeOfDay(now)
			}
			ov.values = append(ov.values, service.PropertyValue{Property: ref.prop, Value: v})
			ov.refs = append(ov.refs, ref)
		}
		send = append(send, ov)
	}
	if !trigger || !s.dcc.mayInitiate(now) {
		return
	}
	for _, ov := range send {
		for i, ref := range ov.refs {
			ref.last, ref.haveLast = ov.values[i].Value, true
		}
	}
	remaining := uint32(0)
	if !sub.expires.IsZero() {
		if d := sub.expires.Sub(now); d > 0 {
			remaining = uint32(math.Ceil(d.Seconds()))
		}
	}
	var payload []byte
	var err error
	var confirmedSvc, unconfirmedSvc uint8
	if sub.kind == subMultiple {
		note := service.COVNotificationMultiple{
			SubscriberProcessIdentifier: sub.pid, InitiatingDevice: s.dev.Identifier(), TimeRemaining: remaining,
			Timestamp: &service.DateTime{Date: dateOfDay(now), Time: timeOfDay(now)},
		}
		for _, ov := range send {
			obj := service.COVNotificationMultipleObject{Object: ov.obj}
			for i, v := range ov.values {
				mv := service.COVNotificationMultipleValue{Property: v.Property, Value: v.Value}
				if ov.refs[i].timestamped {
					t := ov.refs[i].changedAt
					mv.TimeOfChange = &t
				}
				obj.Values = append(obj.Values, mv)
			}
			note.Objects = append(note.Objects, obj)
		}
		payload, err = service.EncodeCOVNotificationMultiple(note)
		confirmedSvc, unconfirmedSvc = apdu.ServiceConfirmedCOVNotificationMultiple, apdu.ServiceUnconfirmedCOVNotificationMultiple
	} else {
		if len(send) == 0 {
			return
		}
		payload, err = service.EncodeCOVNotification(service.COVNotification{
			ProcessIdentifier: sub.pid, InitiatingDevice: s.dev.Identifier(), MonitoredObject: send[0].obj,
			TimeRemaining: remaining, Values: send[0].values,
		})
		confirmedSvc, unconfirmedSvc = apdu.ServiceConfirmedCOVNotification, apdu.ServiceUnconfirmedCOV
	}
	if err != nil {
		s.report(diag.KindCOV, "COV notification not encodable", map[string]any{"error": err.Error()})
		return
	}
	if sub.confirmed {
		if err := s.req.invoke(ctx, sub.to, sub.dest, confirmedSvc, payload); err != nil && ctx.Err() == nil {
			s.report(diag.KindCOV, "confirmed COV notification failed", map[string]any{"error": err.Error(), "to": sub.to.String()})
		}
		return
	}
	sctx, cancel := clock.ContextWithTimeout(ctx, s.clock, s.cfg.apduTimeout)
	defer cancel()
	_ = s.sendAPDU(sctx, sub.to, false, sub.dest, false, apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{ServiceChoice: unconfirmedSvc, Payload: payload}))
}

// significant reports whether v differs enough from the value last sent:
// for REAL values by at least the increment (clause 13.1), else any change.
func significant(ref *covRef, v bacnet.ApplicationValue, objInc float32, analog bool) bool {
	if v.Kind == bacnet.ValueReal && ref.last.Kind == bacnet.ValueReal {
		inc := float32(0)
		switch {
		case ref.increment != nil:
			inc = *ref.increment
		case analog && ref.prop.Identifier == bacnet.PropertyPresentValue:
			inc = objInc
		}
		d := math.Abs(float64(v.Real - ref.last.Real))
		if inc == 0 {
			return d != 0
		}
		return d >= float64(inc)
	}
	a, err1 := bacnet.AppendApplicationValue(nil, v)
	b, err2 := bacnet.AppendApplicationValue(nil, ref.last)
	return err1 != nil || err2 != nil || string(a) != string(b)
}

func timeOfDay(t time.Time) bacnet.Time {
	return bacnet.Time{Hour: uint8(t.Hour()), Minute: uint8(t.Minute()), Second: uint8(t.Second()), Hundredths: uint8(t.Nanosecond() / 1e7)}
}

func dateOfDay(t time.Time) bacnet.Date {
	wd := uint8(t.Weekday())
	if wd == 0 {
		wd = 7
	}
	return bacnet.Date{Year: uint8(t.Year() - 1900), Month: uint8(t.Month()), Day: uint8(t.Day()), Weekday: wd}
}

// activeSubscriptions encodes Active_COV_Subscriptions: a list of
// BACnetCOVSubscription (recipient [0], monitoredPropertyReference [1],
// issueConfirmedNotifications [2], timeRemaining [3], covIncrement [4]).
func (e *covEngine) activeSubscriptions() []bacnet.Element {
	e.mu.Lock()
	subs := make([]*covSub, 0, len(e.subs))
	for _, sub := range e.subs {
		subs = append(subs, sub)
	}
	e.mu.Unlock()
	sort.Slice(subs, func(i, j int) bool { return subs[i].key < subs[j].key })
	now := e.s.clock.Now()
	var out []bacnet.Element
	for _, sub := range subs {
		remaining := uint64(0)
		if !sub.expires.IsZero() {
			if d := sub.expires.Sub(now); d > 0 {
				remaining = uint64(math.Ceil(d.Seconds()))
			}
		}
		network, mac := recipientAddress(sub)
		recipient := []bacnet.Element{
			ctxConstructed(0, ctxConstructed(1,
				bacnet.Element{Value: bacnet.UnsignedValue(uint64(network))},
				bacnet.Element{Value: bacnet.ApplicationValue{Kind: bacnet.ValueOctetString, OctetString: mac}},
			)),
			ctxUnsigned(1, uint64(sub.pid)),
		}
		sub.mu.Lock()
		targets := sub.targets
		sub.mu.Unlock()
		for _, t := range targets {
			for i, ref := range t.refs {
				// SubscribeCOV monitors Present_Value; SubscribeCOVProperty its
				// first reference. The Status_Flags reference that rides along
				// for the notification content is not a monitored property.
				if (sub.kind == subObject && ref.prop.Identifier != bacnet.PropertyPresentValue) || (sub.kind == subProperty && i > 0) {
					continue
				}
				mpr := []bacnet.Element{ctxObjectID(0, t.obj), ctxUnsigned(1, uint64(ref.prop.Identifier))}
				if ref.prop.ArrayIndex != nil {
					mpr = append(mpr, ctxUnsigned(2, uint64(*ref.prop.ArrayIndex)))
				}
				out = append(out,
					ctxConstructed(0, recipient...),
					ctxConstructed(1, mpr...),
					ctxBool(2, sub.confirmed),
					ctxUnsigned(3, remaining),
				)
				if ref.increment != nil {
					out = append(out, ctxReal(4, *ref.increment))
				}
			}
		}
	}
	return out
}

func recipientAddress(sub *covSub) (uint16, []byte) {
	if sub.dest.Scope() == bacnet.AddressRemoteStation {
		return sub.dest.Network(), sub.dest.MAC().Bytes()
	}
	mac, _ := link.MAC(sub.to)
	return 0, mac.Bytes()
}

func ctxConstructed(tag uint8, els ...bacnet.Element) bacnet.Element {
	return bacnet.Element{Context: true, TagNumber: tag, Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: els}}
}

func ctxUnsigned(tag uint8, u uint64) bacnet.Element {
	return bacnet.Element{Context: true, TagNumber: tag, Value: bacnet.UnsignedValue(u)}
}

func ctxBool(tag uint8, b bool) bacnet.Element {
	return bacnet.Element{Context: true, TagNumber: tag, Value: bacnet.BoolValue(b)}
}

func ctxReal(tag uint8, f float32) bacnet.Element {
	return bacnet.Element{Context: true, TagNumber: tag, Value: bacnet.RealValue(f)}
}

func ctxObjectID(tag uint8, id bacnet.ObjectIdentifier) bacnet.Element {
	return bacnet.Element{Context: true, TagNumber: tag, Value: bacnet.ObjectIDValue(id)}
}

// subscribeCOV serves SubscribeCOV (clause 13.14).
func (s *Server) subscribeCOV(_ context.Context, req *apdu.ConfirmedRequest, p peer) result {
	r, err := service.DecodeSubscribeCOV(req.Payload, s.limits)
	if err != nil {
		return rejectFor(err)
	}
	o, ok := s.dev.Object(r.MonitoredObject)
	if !ok {
		return fail(object.ErrUnknownObject)
	}
	if _, ok := o.(object.COVReporter); !ok {
		return fail(errCOVNotSupported)
	}
	id := o.Identifier()
	key := subKey(subObject, p, r.ProcessIdentifier, id, nil)
	if r.Cancellation {
		s.cov.cancel(key)
		return ack()
	}
	sub := &covSub{
		kind: subObject, key: key, to: p.immediate, dest: p.source, recipient: p, pid: r.ProcessIdentifier,
		confirmed: r.IssueConfirmed, lifetime: r.Lifetime,
		targets: []*covTarget{{obj: id, refs: []*covRef{
			{prop: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}},
			{prop: bacnet.PropertyReference{Identifier: bacnet.PropertyStatusFlags}},
		}}},
	}
	return s.startSubscription(sub)
}

func (s *Server) startSubscription(sub *covSub) result {
	sub, err := s.cov.upsert(sub)
	if err != nil {
		return fail(err)
	}
	res := ack()
	res.after = func() { s.cov.kick(sub, true) }
	return res
}

// subscribeCOVProperty serves SubscribeCOVProperty (clause 13.15). The
// notification carries the monitored property and, where the object has
// it, Status_Flags.
func (s *Server) subscribeCOVProperty(ctx context.Context, req *apdu.ConfirmedRequest, p peer) result {
	r, err := service.DecodeSubscribeCOVProperty(req.Payload, s.limits)
	if err != nil {
		return rejectFor(err)
	}
	o, ok := s.dev.Object(r.MonitoredObject)
	if !ok {
		return fail(object.ErrUnknownObject)
	}
	id := o.Identifier()
	key := subKey(subProperty, p, r.ProcessIdentifier, id, &r.Property)
	if r.Cancellation {
		s.cov.cancel(key)
		return ack()
	}
	if _, err := o.ReadProperty(ctx, r.Property); err != nil {
		return fail(err)
	}
	if r.COVIncrement != nil && (*r.COVIncrement < 0 || math.IsNaN(float64(*r.COVIncrement))) {
		return fail(object.ErrValueOutOfRange)
	}
	refs := []*covRef{{prop: r.Property, increment: r.COVIncrement}}
	if r.Property.Identifier != bacnet.PropertyStatusFlags {
		if _, err := o.ReadProperty(ctx, bacnet.PropertyReference{Identifier: bacnet.PropertyStatusFlags}); err == nil {
			refs = append(refs, &covRef{prop: bacnet.PropertyReference{Identifier: bacnet.PropertyStatusFlags}})
		}
	}
	sub := &covSub{
		kind: subProperty, key: key, to: p.immediate, dest: p.source, recipient: p, pid: r.ProcessIdentifier,
		confirmed: r.IssueConfirmed, lifetime: r.Lifetime,
		targets: []*covTarget{{obj: id, refs: refs}},
	}
	return s.startSubscription(sub)
}

// subscribeCOVPropertyMultiple serves SubscribeCOVPropertyMultiple
// (clause 13.16): one subscription, keyed by subscriber and process
// identifier, over every listed object and property.
func (s *Server) subscribeCOVPropertyMultiple(ctx context.Context, req *apdu.ConfirmedRequest, p peer) result {
	r, err := service.DecodeSubscribeCOVPropertyMultiple(req.Payload, s.limits)
	if err != nil {
		return rejectFor(err)
	}
	key := subKey(subMultiple, p, r.SubscriberProcessIdentifier, bacnet.ObjectIdentifier{}, nil)
	if r.Cancellation {
		s.cov.cancel(key)
		return ack()
	}
	sub := &covSub{
		kind: subMultiple, key: key, to: p.immediate, dest: p.source, recipient: p,
		pid: r.SubscriberProcessIdentifier, confirmed: r.IssueConfirmedNotifications,
	}
	if r.LifetimeRemaining != nil {
		sub.lifetime = *r.LifetimeRemaining
	}
	for _, spec := range r.Subscriptions {
		o, ok := s.dev.Object(spec.Object)
		if !ok {
			return fail(&covMultipleError{err: object.AsError(object.ErrUnknownObject), obj: spec.Object, prop: firstProp(spec)})
		}
		t := &covTarget{obj: o.Identifier()}
		for _, ref := range spec.Properties {
			if _, err := o.ReadProperty(ctx, ref.Property); err != nil {
				return fail(&covMultipleError{err: object.AsError(err), obj: spec.Object, prop: ref.Property})
			}
			t.refs = append(t.refs, &covRef{prop: ref.Property, increment: ref.COVIncrement, timestamped: ref.Timestamped})
		}
		sub.targets = append(sub.targets, t)
	}
	return s.startSubscription(sub)
}

func firstProp(spec service.COVMultipleSubscription) bacnet.PropertyReference {
	if len(spec.Properties) > 0 {
		return spec.Properties[0].Property
	}
	return bacnet.PropertyReference{}
}

// covMultipleError is a SubscribeCOVPropertyMultiple-Error: errorType [0]
// and firstFailedSubscription [1] { monitoredObjectIdentifier [0],
// monitoredPropertyReference [1], errorType [2] } (clause 13.16).
type covMultipleError struct {
	err  *bacnet.ErrorResponse
	obj  bacnet.ObjectIdentifier
	prop bacnet.PropertyReference
}

func (e *covMultipleError) Error() string { return "subscribe COV property multiple failed" }

func (e *covMultipleError) encode() ([]byte, error) {
	errBody, err := bacnet.EncodeBACnetError(nil, e.err.Class, e.err.Code)
	if err != nil {
		return nil, err
	}
	dst := append([]byte{0x0E}, errBody...)
	dst = append(dst, 0x0F, 0x1E)
	if dst, err = bacnet.AppendContextObjectID(dst, 0, e.obj); err != nil {
		return nil, err
	}
	dst = append(dst, 0x1E)
	if dst, err = bacnet.AppendContextUnsigned(dst, 0, uint64(e.prop.Identifier)); err != nil {
		return nil, err
	}
	if e.prop.ArrayIndex != nil {
		if dst, err = bacnet.AppendContextUnsigned(dst, 1, uint64(*e.prop.ArrayIndex)); err != nil {
			return nil, err
		}
	}
	dst = append(dst, 0x1F, 0x2E)
	dst = append(dst, errBody...)
	return append(dst, 0x2F, 0x1F), nil
}
