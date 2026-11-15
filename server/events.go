// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/internal/diag"
	"github.com/otfabric/go-bacnet/server/object"
	"github.com/otfabric/go-bacnet/service"
)

// maxBindings bounds the device address cache filled from I-Am, and the
// Who-Is requests for unbound recipients that are outstanding at once.
const maxBindings = 256

// eventEngine runs intrinsic reporting: event detection on objects that
// changed and once a second for time delays, and the delivery of event
// notifications to the recipients of the Notification Class (clause 13.2,
// 13.8, 13.9).
//
// Device recipients are found through the address cache (bindings). An
// I-Am is recorded when the device is an event recipient: a Recipient_List
// names it, or a Who-Is was sent for it. The address of a device nobody
// reports to is not kept. A recorded device is refreshed by its later
// I-Ams, and when the cache is full the entry heard from longest ago makes
// room for the new one (reported as a diagnostic).
type eventEngine struct {
	s     *Server
	mu    sync.Mutex
	dirty map[bacnet.ObjectIdentifier]struct{}
	kick  chan struct{}
	// Device addresses learnt from I-Am, for device recipients, and the
	// time a Who-Is was last sent for a recipient that is not bound.
	bmu      sync.Mutex
	bindings map[uint32]binding
	asked    map[uint32]time.Time
	// named holds the device instances that the Recipient_Lists name. It is
	// built on demand and again after namedGen moved: every change of a
	// Notification Class or of the object table moves it.
	nmu      sync.Mutex
	named    map[uint32]struct{}
	namedAt  uint64
	namedGen atomic.Uint64
}

type binding struct {
	to   bip.Endpoint
	addr bacnet.Address
	seen time.Time // the last I-Am of the device
}

func newEventEngine(s *Server) *eventEngine {
	return &eventEngine{
		s: s, dirty: make(map[bacnet.ObjectIdentifier]struct{}), kick: make(chan struct{}, 1),
		bindings: make(map[uint32]binding), asked: make(map[uint32]time.Time),
	}
}

// changed is a Device observer; it never blocks.
func (e *eventEngine) changed(c object.Change) {
	if c.Object.Type == bacnet.ObjectTypeNotificationClass || c.Object.Type == bacnet.ObjectTypeDevice {
		e.namedGen.Add(1)
	}
	e.mark(c.Object)
}

// answerWindow is how long an answer to a request the server broadcast
// (Who-Is, Who-Is-Router-To-Network) is waited for before the request is
// sent again: as long as a confirmed request waits with its retries.
func (s *Server) answerWindow() time.Duration {
	return time.Duration(s.cfg.retries+1) * s.cfg.apduTimeout
}

// isNamed reports whether a Recipient_List of a Notification Class names
// the device.
func (e *eventEngine) isNamed(instance uint32) bool {
	e.nmu.Lock()
	defer e.nmu.Unlock()
	if gen := e.namedGen.Load(); e.named == nil || e.namedAt != gen {
		e.named, e.namedAt = e.recipientDevices(), gen
	}
	_, ok := e.named[instance]
	return ok
}

// recipientDevices collects the device recipients of every Notification
// Class.
func (e *eventEngine) recipientDevices() map[uint32]struct{} {
	out := make(map[uint32]struct{})
	for _, id := range e.s.dev.ObjectIdentifiers() {
		if id.Type != bacnet.ObjectTypeNotificationClass {
			continue
		}
		nc, ok := e.notificationClass(id.Instance)
		if !ok {
			continue
		}
		v, err := nc.ReadProperty(e.s.ctx, bacnet.PropertyReference{Identifier: bacnet.PropertyRecipientList})
		if err != nil {
			continue
		}
		dests, _, err := object.ParseDestinations(v.Elements)
		if err != nil {
			continue
		}
		for _, d := range dests {
			if d.Device != nil {
				out[d.Device.Instance] = struct{}{}
			}
		}
	}
	return out
}

// askLocked reports whether a Who-Is for the unbound recipient is due, and
// records it: one is sent per recipient and answer window, and at most
// maxBindings recipients are waited for at once. The caller holds e.bmu.
func (e *eventEngine) askLocked(instance uint32, now time.Time) bool {
	window := e.s.answerWindow()
	at, pending := e.asked[instance]
	if pending && now.Sub(at) < window {
		return false
	}
	if !pending && len(e.asked) >= maxBindings {
		for k, at := range e.asked {
			if now.Sub(at) >= window {
				delete(e.asked, k)
			}
		}
		if len(e.asked) >= maxBindings {
			return false
		}
	}
	e.asked[instance] = now
	return true
}

// learn records the address a device announced with I-Am, when the device
// is an event recipient (see eventEngine).
func (e *eventEngine) learn(instance uint32, p peer) {
	e.bmu.Lock()
	_, known := e.bindings[instance]
	_, asked := e.asked[instance]
	e.bmu.Unlock()
	if !known && !asked && !e.isNamed(instance) {
		return
	}
	evicted, full := uint32(0), false
	e.bmu.Lock()
	if _, known := e.bindings[instance]; !known && len(e.bindings) >= maxBindings {
		evicted, full = e.evictLocked(), true
	}
	e.bindings[instance] = binding{to: p.immediate, addr: p.source, seen: e.s.clock.Now()}
	delete(e.asked, instance)
	e.bmu.Unlock()
	if full {
		e.s.report(diag.KindRegistryEviction, "event recipient address dropped for a newer one: cache full", map[string]any{"device": evicted, "limit": maxBindings})
	}
}

// evictLocked removes the binding heard from longest ago and returns its
// device instance. The caller holds e.bmu; the cache is not empty.
func (e *eventEngine) evictLocked() uint32 {
	var oldest uint32
	var at time.Time
	first := true
	for inst, b := range e.bindings {
		if first || b.seen.Before(at) || (b.seen.Equal(at) && inst < oldest) {
			oldest, at, first = inst, b.seen, false
		}
	}
	delete(e.bindings, oldest)
	return oldest
}

func (e *eventEngine) mark(id bacnet.ObjectIdentifier) {
	e.mu.Lock()
	e.dirty[id] = struct{}{}
	e.mu.Unlock()
	select {
	case e.kick <- struct{}{}:
	default:
	}
}

// tick marks every reporting object, for time delays.
func (e *eventEngine) tick() {
	for _, id := range e.s.dev.ObjectIdentifiers() {
		if o, ok := e.s.dev.Object(id); ok {
			if _, ok := o.(object.EventReporter); ok {
				e.mark(id)
			}
		}
	}
}

func (e *eventEngine) run() {
	for {
		select {
		case <-e.s.ctx.Done():
			return
		case <-e.kick:
		}
		e.mu.Lock()
		ids := make([]bacnet.ObjectIdentifier, 0, len(e.dirty))
		for id := range e.dirty {
			ids = append(ids, id)
		}
		clear(e.dirty)
		e.mu.Unlock()
		for _, id := range ids {
			if e.s.ctx.Err() != nil {
				return
			}
			// EvaluateEvents is the object's code: one object that does
			// not return normally does not stop detection for the others.
			e.s.guard("event evaluation", map[string]any{"object": id.String()}, func() { e.evaluate(id) })
		}
	}
}

func (e *eventEngine) notificationClass(n uint32) (*object.NotificationClass, bool) {
	o, ok := e.s.dev.Object(bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeNotificationClass, Instance: n})
	if !ok {
		return nil, false
	}
	nc, ok := o.(*object.NotificationClass)
	return nc, ok
}

func (e *eventEngine) ackRequired(class uint32, transition int) bool {
	nc, ok := e.notificationClass(class)
	return ok && nc.AckRequired()&(1<<transition) != 0
}

func (e *eventEngine) evaluate(id bacnet.ObjectIdentifier) {
	o, ok := e.s.dev.Object(id)
	if !ok {
		return
	}
	r, ok := o.(object.EventReporter)
	if !ok {
		return
	}
	for _, tr := range r.EvaluateEvents(e.s.clock.Now(), e.ackRequired) {
		e.report(tr)
	}
}

// report sends a transition to the destinations of its Notification
// Class that are active now. Without the Notification Class the
// transition is only recorded in the object.
func (e *eventEngine) report(tr object.EventTransition) {
	s := e.s
	nc, ok := e.notificationClass(tr.NotificationClass)
	if !ok {
		s.report(diag.KindEvent, "event not reported: no notification class", map[string]any{"object": tr.Object.String(), "class": tr.NotificationClass})
		return
	}
	now := s.clock.Now()
	if !s.dcc.mayInitiate(now) {
		return
	}
	priority, ackRequired, dests := nc.Routing(now, tr.Transition)
	note := service.EventNotification{
		InitiatingDevice: s.dev.Identifier(), EventObject: tr.Object, TimeStamp: object.TimeStampOf(tr.TimeStamp),
		NotificationClass: tr.NotificationClass, Priority: priority, EventType: tr.EventType,
		NotifyType: tr.NotifyType, ToState: tr.To, Parameters: tr.Params,
	}
	if tr.NotifyType != object.NotifyAckNotification {
		from := tr.From
		note.FromState = &from
		note.AckRequired = &ackRequired
	}
	for _, d := range dests {
		note.ProcessIdentifier = d.ProcessID
		payload, err := service.EncodeEventNotification(note)
		if err != nil {
			s.report(diag.KindEvent, "event notification not encoded", map[string]any{"error": err.Error()})
			return
		}
		to, dest, broadcast, ok := e.resolve(d)
		if !ok {
			continue
		}
		if d.Confirmed && broadcast {
			// A confirmed request is a transaction with one peer, identified
			// by its address and invoke ID (clause 5.4), so it is not sent
			// to a broadcast recipient; a BBMD discards a broadcast
			// confirmed request as well (bbmd.confirmedRequest).
			// bacnet-stack sends it anyway (nc.c, s_cevent.c).
			s.report(diag.KindEvent, "confirmed event notification to a broadcast recipient not sent", map[string]any{"to": dest.String()})
			continue
		}
		if d.Confirmed {
			s.req.async(func() {
				if err := s.req.invoke(s.ctx, to, dest, apdu.ServiceConfirmedEventNotification, payload, false); err != nil {
					s.report(diag.KindEvent, "confirmed event notification failed", map[string]any{"to": to.String(), "error": err.Error()})
				}
			})
			continue
		}
		frame := apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{ServiceChoice: apdu.ServiceUnconfirmedEventNotification, Payload: payload})
		_ = s.sendAPDU(s.ctx, to, broadcast, dest, false, frame)
	}
}

// resolve finds where a destination is. A device recipient whose address
// is not known yet gets a Who-Is and this notification is skipped, as
// bacnet-stack does (nc.c). One Who-Is is sent per recipient and answer
// window, however many notifications are skipped meanwhile.
func (e *eventEngine) resolve(d object.Destination) (to bip.Endpoint, dest bacnet.Address, broadcast, ok bool) {
	s := e.s
	if d.Device != nil {
		inst := d.Device.Instance
		e.bmu.Lock()
		b, known := e.bindings[inst]
		ask := !known && e.askLocked(inst, s.clock.Now())
		e.bmu.Unlock()
		if known {
			return b.to, b.addr, false, true
		}
		if !ask {
			s.report(diag.KindEvent, "event recipient not bound", map[string]any{"device": inst})
			return bip.Endpoint{}, bacnet.Address{}, false, false
		}
		payload, err := service.EncodeWhoIs(service.WhoIs{LowLimit: &inst, HighLimit: &inst})
		if err == nil {
			frame := apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{ServiceChoice: apdu.ServiceWhoIs, Payload: payload})
			_ = s.broadcastAPDU(nil, frame)
		}
		s.report(diag.KindEvent, "event recipient not bound; Who-Is sent", map[string]any{"device": inst})
		return bip.Endpoint{}, bacnet.Address{}, false, false
	}
	addr, err := d.Address()
	if err != nil {
		return bip.Endpoint{}, bacnet.Address{}, false, false
	}
	switch {
	case addr.IsBroadcast():
		// A remote or global broadcast goes out as a local broadcast with
		// its DNET, for the routers to forward.
		return bip.Endpoint{}, addr, true, true
	case addr.Scope() == bacnet.AddressRemoteStation:
		// Through the router of the network (routeTable). When it is not
		// known yet a Who-Is-Router-To-Network asks for it (clause 6.4.1),
		// as bacnet-stack does for an address recipient (nc.c
		// Notification_Class_find_recipient). A confirmed notification is
		// then skipped, like one to an unbound device; an unconfirmed one
		// still goes out as a local broadcast with its DNET, as
		// bacnet-stack sends a remote destination without a router MAC
		// (h_bbmd.c bvlc_send_pdu).
		if router, known := s.routes.lookup(addr.Network()); known {
			return router, addr, false, true
		}
		msg := "event recipient network has no known router"
		if s.routes.ask(addr.Network(), s.clock.Now(), s.answerWindow()) {
			s.whoIsRouter(addr.Network())
			msg += "; Who-Is-Router-To-Network sent"
		}
		if d.Confirmed {
			s.report(diag.KindEvent, msg, map[string]any{"to": addr.String()})
			return bip.Endpoint{}, bacnet.Address{}, false, false
		}
		return bip.Endpoint{}, addr, true, true
	}
	ep, ok := endpointFromMAC(addr.MAC().Bytes())
	return ep, addr, false, ok
}

// iAm records the address of a device that is an event recipient.
func (s *Server) iAm(_ context.Context, req *apdu.UnconfirmedRequest, p peer) {
	r, err := service.DecodeIAm(req.Payload, s.limits)
	if err != nil || r.Device.Type != bacnet.ObjectTypeDevice || r.Device == s.dev.Identifier() {
		return
	}
	s.events.learn(r.Device.Instance, p)
}

// acknowledgeAlarm serves AcknowledgeAlarm (clause 13.5). The
// acknowledgment notification goes to the recipients after the SimpleACK,
// when the request acknowledged a transition that was not acknowledged
// before. A request that names an acknowledged transition is answered the
// same way and changes nothing, so there is nothing to report.
func (s *Server) acknowledgeAlarm(_ context.Context, req *apdu.ConfirmedRequest, _ peer) result {
	r, err := service.DecodeAcknowledgeAlarm(req.Payload, s.limits)
	if err != nil {
		return rejectFor(err)
	}
	o, ok := s.dev.Object(r.EventObject)
	if !ok {
		return fail(object.ErrUnknownObject)
	}
	er, ok := o.(object.EventReporter)
	if !ok {
		return fail(object.ErrNoAlarmConfigured)
	}
	before, ok := er.EventInfo()
	if !ok {
		return fail(object.ErrNoAlarmConfigured)
	}
	tr, err := er.AcknowledgeAlarm(r.EventStateAcked, r.TimeStamp)
	if err != nil {
		return fail(err)
	}
	res := ack()
	if before.Acked&(1<<transitionOf(r.EventStateAcked)) == 0 {
		res.after = func() { s.events.report(tr) }
	}
	return res
}

// eventInfos returns the event status of every reporting object, in
// object order.
func (s *Server) eventInfos() []object.EventInfo {
	var out []object.EventInfo
	for _, id := range s.dev.ObjectIdentifiers() {
		o, ok := s.dev.Object(id)
		if !ok {
			continue
		}
		if r, ok := o.(object.EventReporter); ok {
			if info, ok := r.EventInfo(); ok {
				out = append(out, info)
			}
		}
	}
	return out
}

// getAlarmSummary serves GetAlarmSummary (clause 13.10): objects not in
// NORMAL whose Notify_Type is ALARM, as bacnet-stack does (ai.c).
func (s *Server) getAlarmSummary(_ context.Context, req *apdu.ConfirmedRequest, _ peer) result {
	if err := service.DecodeGetAlarmSummary(req.Payload, s.limits); err != nil {
		return rejectFor(err)
	}
	var ack service.GetAlarmSummaryACK
	for _, info := range s.eventInfos() {
		if info.EventState != object.EventStateNormal && info.NotifyType == object.NotifyAlarm {
			ack.Entries = append(ack.Entries, service.AlarmSummaryEntry{Object: info.Object, AlarmState: info.EventState, AckedTransitions: transitionBits(info.Acked)})
		}
	}
	payload, err := service.EncodeGetAlarmSummaryACK(ack)
	if err != nil {
		return fail(err)
	}
	return complexACK(payload)
}

func transitionBits(mask uint8) bacnet.BitString {
	b := byte(0)
	for i := 0; i < 3; i++ {
		if mask&(1<<i) != 0 {
			b |= 0x80 >> i
		}
	}
	return bacnet.BitString{UnusedBits: 5, Bytes: []byte{b}}
}

func (s *Server) priorities(class uint32) [3]uint8 {
	if nc, ok := s.events.notificationClass(class); ok {
		return nc.Priorities()
	}
	return [3]uint8{255, 255, 255}
}

// getEventInformation serves GetEventInformation (clause 13.12): objects
// in an event state other than NORMAL or with an unacknowledged
// transition, after Last Received Object Identifier, as many as fit the
// reply, with More Events set when some were left out (h_getevent.c).
func (s *Server) getEventInformation(ctx context.Context, req *apdu.ConfirmedRequest, _ peer) result {
	r, err := service.DecodeGetEventInformation(req.Payload, s.limits)
	if err != nil {
		return rejectFor(err)
	}
	var all []service.EventSummary
	skipping := r.LastReceived != nil
	for _, info := range s.eventInfos() {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		if skipping {
			if info.Object == *r.LastReceived {
				skipping = false
			}
			continue
		}
		if info.EventState == object.EventStateNormal && info.Acked == 7 {
			continue
		}
		stamps := make([]bacnet.Element, 3)
		for i, t := range info.TimeStamps {
			ts := object.TimeStampOf(t)
			if t.IsZero() {
				ts.DateTime = service.DateTime{Date: bacnet.Date{Year: 0xFF, Month: 0xFF, Day: 0xFF, Weekday: 0xFF}, Time: bacnet.Time{Hour: 0xFF, Minute: 0xFF, Second: 0xFF, Hundredths: 0xFF}}
			}
			raw, _ := service.EncodeTimeStamp(ts)
			el, _, _ := bacnet.ParseSequence(raw, bacnet.DefaultDecodeLimits(), -1)
			stamps[i] = el[0]
		}
		pr := s.priorities(info.NotificationClass)
		prios := make([]bacnet.Element, 3)
		for i, p := range pr {
			prios[i] = bacnet.Element{Value: bacnet.UnsignedValue(uint64(p))}
		}
		all = append(all, service.EventSummary{
			Object: info.Object, EventState: info.EventState, AcknowledgedTransitions: transitionBits(info.Acked),
			EventTimeStamps: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: stamps},
			NotifyType:      info.NotifyType, EventEnable: transitionBits(info.EventEnable),
			EventPriorities: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: prios},
		})
	}
	budget := s.responseBudget(req)
	build := func(n int) ([]byte, error) {
		return service.EncodeGetEventInformationACK(service.GetEventInformationACK{Summaries: all[:n], MoreEvents: n < len(all)})
	}
	// The largest prefix that fits, by binary search as readRange does: the
	// encoded size grows with the number of summaries. n is the first count
	// whose next summary no longer fits, or all of them.
	n := sort.Search(len(all), func(k int) bool {
		p, err := build(k + 1)
		return err != nil || len(p) > budget
	})
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	payload, err := build(n)
	if err != nil {
		return fail(err)
	}
	return complexACK(payload)
}

// getEnrollmentSummary serves GetEnrollmentSummary (clause 13.11) with
// all its filters. The enrollment filter (clause 13.11.1.2) keeps the
// objects that have the recipient process as a notification recipient:
// the Recipient_List of their Notification Class holds a destination with
// that recipient and process identifier. An object whose Notification
// Class does not exist has no recipients.
func (s *Server) getEnrollmentSummary(_ context.Context, req *apdu.ConfirmedRequest, _ peer) result {
	r, err := service.DecodeGetEnrollmentSummary(req.Payload, s.limits)
	if err != nil {
		return rejectFor(err)
	}
	var enrolled *object.Destination
	if f := r.EnrollmentFilter; f != nil {
		d := object.Destination{ProcessID: f.ProcessIdentifier, Device: f.Recipient.Device}
		if a := f.Recipient.Address; a != nil {
			d.Network, d.MAC = a.Network, a.MAC
		}
		enrolled = &d
	}
	var ack service.GetEnrollmentSummaryACK
	for _, info := range s.eventInfos() {
		if enrolled != nil {
			if nc, ok := s.events.notificationClass(info.NotificationClass); !ok || !nc.HasRecipient(*enrolled) {
				continue
			}
		}
		switch r.AcknowledgmentFilter {
		case 1: // acked
			if info.Acked != 7 {
				continue
			}
		case 2: // not-acked
			if info.Acked == 7 {
				continue
			}
		default: // all
		}
		if f := r.EventStateFilter; f != nil && !eventStateMatches(*f, info.EventState) {
			continue
		}
		if f := r.EventTypeFilter; f != nil && *f != info.EventType {
			continue
		}
		pr := s.priorities(info.NotificationClass)[transitionOf(info.EventState)]
		if f := r.PriorityFilter; f != nil && (uint32(pr) < f[0] || uint32(pr) > f[1]) {
			continue
		}
		if f := r.NotificationClassFilter; f != nil && *f != info.NotificationClass {
			continue
		}
		ack.Entries = append(ack.Entries, service.EnrollmentSummaryEntry{
			Object: info.Object, EventType: info.EventType, EventState: info.EventState,
			Priority: uint32(pr), NotificationClass: info.NotificationClass,
		})
	}
	payload, err := service.EncodeGetEnrollmentSummaryACK(ack)
	if err != nil {
		return fail(err)
	}
	return complexACK(payload)
}

func transitionOf(state uint32) int {
	switch state {
	case object.EventStateNormal:
		return object.TransitionToNormal
	case object.EventStateFault:
		return object.TransitionToFault
	default:
		return object.TransitionToOffNormal
	}
}

// eventStateMatches applies the eventStateFilter: offnormal (0), fault
// (1), normal (2), all (3), active (4, any state but normal).
func eventStateMatches(filter, state uint32) bool {
	switch filter {
	case 0:
		return state != object.EventStateNormal && state != object.EventStateFault
	case 1:
		return state == object.EventStateFault
	case 2:
		return state == object.EventStateNormal
	case 4:
		return state != object.EventStateNormal
	default:
		return true
	}
}
