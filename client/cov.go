// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/internal/clock"
	"github.com/otfabric/go-bacnet/internal/diag"
	"github.com/otfabric/go-bacnet/service"
)

// SubscriptionState is the lifecycle state of a COV subscription.
type SubscriptionState uint8

const (
	SubscriptionPending SubscriptionState = iota
	SubscriptionActive
	SubscriptionRenewing
	SubscriptionDegraded
	SubscriptionExpired
	SubscriptionClosed
)

// SubscriptionEvent is a unified COV event.
//
// Sequence is monotonic per subscription for successfully queued events.
// Gap is sticky: when one or more events were dropped due to a full queue, the
// next successfully delivered event has Gap=true so callers observe the loss.
//
// Notification is set for a COV notification of a SubscribeCOV or
// SubscribeCOVProperty subscription. Multiple is set when the device sent a
// COVNotificationMultiple: it is the whole notification, with its timestamp
// and each value's time of change, and is all an event of a
// SubscribeCOVMultiple subscription carries. Both point to values that are
// not modified after delivery; Multiple may be shared between events.
type SubscriptionEvent struct {
	Sequence     uint64
	Notification *service.COVNotification
	Multiple     *service.COVNotificationMultiple
	State        SubscriptionState
	Gap          bool
	Err          error
}

// Subscription is a COV subscription with explicit lifetime.
type Subscription interface {
	Events() <-chan SubscriptionEvent
	Close() error
}

type subscription struct {
	c            *Client
	id           uint32
	target       Target
	object       bacnet.ObjectIdentifier
	property     *bacnet.PropertyReference
	covIncrement *float32
	multi        []service.COVMultipleSubscription // SubscribeCOVPropertyMultiple
	maxDelay     *uint32
	lifetime     uint32
	confirmed    bool
	capacity     int // soft capacity for notifications; +1 reserved for terminal
	events       chan SubscriptionEvent
	state        atomic.Uint32
	cancel       context.CancelFunc
	done         chan struct{}
	closeOnce    sync.Once

	mu         sync.Mutex
	closed     bool
	seq        uint64
	gapPending bool
}

func (s *subscription) Events() <-chan SubscriptionEvent { return s.events }

func (s *subscription) Close() error {
	var err error
	s.closeOnce.Do(func() {
		prev := SubscriptionState(s.state.Swap(uint32(SubscriptionClosed)))
		if prev == SubscriptionClosed {
			return
		}
		s.cancel()
		s.c.subs.unregister(s.id)

		ctx, stop := clock.ContextWithTimeout(context.Background(), s.c.clock, s.c.cfg.apduTimeout)
		err = s.issue(ctx, true)
		stop()

		<-s.done
		s.publishClosed(nil)
	})
	return err
}

func (s *subscription) publishClosed(cause error) {
	s.send(SubscriptionEvent{State: SubscriptionClosed, Err: cause})
}

func (s *subscription) tryMarkDegraded() {
	for {
		cur := s.state.Load()
		st := SubscriptionState(cur)
		if st == SubscriptionClosed || st == SubscriptionDegraded {
			return
		}
		if s.state.CompareAndSwap(cur, uint32(SubscriptionDegraded)) {
			return
		}
	}
}

func (s *subscription) tryStoreState(next SubscriptionState) bool {
	for {
		cur := s.state.Load()
		if SubscriptionState(cur) == SubscriptionClosed {
			return false
		}
		if s.state.CompareAndSwap(cur, uint32(next)) {
			return true
		}
	}
}

func (s *subscription) send(ev SubscriptionEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}

	isTerminal := ev.State == SubscriptionClosed
	cur := SubscriptionState(s.state.Load())
	if cur == SubscriptionClosed && !isTerminal {
		// Never replace Closed with Degraded or other non-terminal states.
		return
	}

	if !isTerminal && len(s.events) >= s.capacity {
		s.gapPending = true
		s.c.diag.Report(diag.Event{Kind: diag.KindQueueDrop, Message: "COV event dropped"})
		s.tryMarkDegraded()
		return
	}

	s.seq++
	ev.Sequence = s.seq
	if s.gapPending && !isTerminal {
		ev.Gap = true
		s.gapPending = false
	}

	select {
	case s.events <- ev:
		if isTerminal {
			s.closed = true
			close(s.events)
		}
	default:
		if isTerminal {
			// Reserved slot should normally admit Closed; still close the channel.
			s.closed = true
			close(s.events)
			return
		}
		s.gapPending = true
		s.c.diag.Report(diag.Event{Kind: diag.KindQueueDrop, Message: "COV event dropped"})
		s.tryMarkDegraded()
	}
}

type subscriptionManager struct {
	mu   sync.Mutex
	byID map[uint32]*subscription
	diag diag.Sink
	next uint32
}

func newSubscriptionManager(d diag.Sink) *subscriptionManager {
	return &subscriptionManager{byID: make(map[uint32]*subscription), diag: d, next: 1}
}

func (m *subscriptionManager) register(s *subscription) {
	m.mu.Lock()
	m.byID[s.id] = s
	m.mu.Unlock()
}

func (m *subscriptionManager) unregister(id uint32) {
	m.mu.Lock()
	delete(m.byID, id)
	m.mu.Unlock()
}

func (m *subscriptionManager) deliver(ev SubscriptionEvent, processID uint32, src packetSource) {
	m.mu.Lock()
	s := m.byID[processID]
	m.mu.Unlock()
	if s == nil || ev.Notification == nil || s.multi != nil {
		return
	}
	note := ev.Notification
	if note.MonitoredObject != s.object {
		return
	}
	if !matchTargetSource(s.target, src) {
		return
	}
	s.send(ev)
}

// monitors reports whether a SubscribeCOVMultiple subscription covers object.
func (s *subscription) monitors(object bacnet.ObjectIdentifier) bool {
	for _, spec := range s.multi {
		if spec.Object == object {
			return true
		}
	}
	return false
}

// deliverMultiple hands a COVNotificationMultiple to the subscription with
// its process identifier. A SubscribeCOVMultiple subscription gets the
// objects it subscribed to.
// A single-object subscription gets one event per entry for its object, with
// that entry as an ordinary notification next to the whole one.
func (m *subscriptionManager) deliverMultiple(note *service.COVNotificationMultiple, src packetSource) {
	m.mu.Lock()
	s := m.byID[note.SubscriberProcessIdentifier]
	m.mu.Unlock()
	if s == nil || !matchTargetSource(s.target, src) {
		return
	}
	if s.multi != nil {
		// Only the objects this subscription asked for: a device may still
		// hold a subscription of an earlier client with the same process
		// identifier.
		var kept []service.COVNotificationMultipleObject
		for _, obj := range note.Objects {
			if s.monitors(obj.Object) {
				kept = append(kept, obj)
			}
		}
		if len(kept) == 0 {
			return
		}
		if len(kept) != len(note.Objects) {
			filtered := *note
			filtered.Objects = kept
			note = &filtered
		}
		s.send(SubscriptionEvent{Multiple: note, State: SubscriptionActive})
		return
	}
	for _, obj := range note.Objects {
		if obj.Object != s.object {
			continue
		}
		vals := make([]service.PropertyValue, 0, len(obj.Values))
		for _, v := range obj.Values {
			vals = append(vals, service.PropertyValue{Property: v.Property, Value: v.Value})
		}
		s.send(SubscriptionEvent{
			Notification: &service.COVNotification{
				ProcessIdentifier: note.SubscriberProcessIdentifier,
				InitiatingDevice:  note.InitiatingDevice,
				MonitoredObject:   obj.Object,
				TimeRemaining:     note.TimeRemaining,
				Values:            vals,
			},
			Multiple: note,
			State:    SubscriptionActive,
		})
	}
}

func (m *subscriptionManager) closeAll() {
	m.mu.Lock()
	subs := make([]*subscription, 0, len(m.byID))
	for _, s := range m.byID {
		subs = append(subs, s)
	}
	m.byID = make(map[uint32]*subscription)
	m.mu.Unlock()
	for _, s := range subs {
		s.closeOnce.Do(func() {
			s.state.Store(uint32(SubscriptionClosed))
			s.cancel()
			<-s.done
			s.publishClosed(bacnet.ErrClosed)
		})
	}
}

func (m *subscriptionManager) nextID() uint32 {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := m.next
	m.next++
	if m.next == 0 {
		m.next = 1
	}
	return id
}

// COVOptions configures a COV subscription.
type COVOptions struct {
	// Lifetime is the subscription lifetime in seconds requested from the
	// device. Zero selects 60. The client resubscribes at half the lifetime
	// for as long as the Subscription is open; an indefinite (lifetime 0)
	// subscription is not offered because it would outlive the client.
	Lifetime uint32
	// IssueConfirmed asks for confirmed notifications, which the client
	// acknowledges on receipt.
	IssueConfirmed bool
	// BufferSize is the capacity of the Events channel (default 16). When
	// the consumer falls behind, notifications are dropped and the next
	// delivered event has Gap set.
	BufferSize int
}

// SubscribeCOV subscribes to change-of-value notifications for object and
// returns once the device has accepted the subscription.
//
// Read Events until it is closed. The first event reports the Active state;
// later events carry notifications or state changes (Renewing, Degraded when
// a renewal failed or events were dropped). Close cancels the subscription on
// the device and ends the stream with a Closed event; closing the Client ends
// it without cancelling. The Subscription is safe for concurrent use.
func (c *Client) SubscribeCOV(ctx context.Context, target Target, object bacnet.ObjectIdentifier, opts COVOptions) (Subscription, error) {
	return c.startSubscription(ctx, target, &subscription{object: object}, opts)
}

func (c *Client) subscribeCOV(ctx context.Context, target Target, req service.SubscribeCOVRequest) error {
	payload, err := service.EncodeSubscribeCOV(req)
	if err != nil {
		return err
	}
	pdu, err := c.confirmedRequest(ctx, target, apdu.ServiceSubscribeCOV, payload, DefaultRetransmitPolicy(apdu.ServiceSubscribeCOV))
	if err != nil {
		return err
	}
	if !req.Cancellation && pdu.Type != apdu.TypeSimpleACK {
		return bacnet.ErrProtocolViolation
	}
	return nil
}

// SubscribeCOVProperty starts a property-scoped COV subscription.
func (c *Client) SubscribeCOVProperty(ctx context.Context, target Target, object bacnet.ObjectIdentifier, property bacnet.PropertyReference, opts COVOptions, covIncrement *float32) (Subscription, error) {
	s := &subscription{object: object, property: &property}
	if covIncrement != nil {
		v := *covIncrement
		s.covIncrement = &v
	}
	return c.startSubscription(ctx, target, s, opts)
}

// startSubscription completes s from opts, sends its subscription request
// and, once the device has accepted it, starts the renewal loop.
func (c *Client) startSubscription(ctx context.Context, target Target, s *subscription, opts COVOptions) (Subscription, error) {
	// Notifications are matched against the stored target, so it must carry
	// the same address a request would use.
	target, err := normalizeTarget(target)
	if err != nil {
		return nil, err
	}
	if opts.Lifetime == 0 {
		opts.Lifetime = 60
	}
	if opts.BufferSize <= 0 {
		opts.BufferSize = 16
	}
	subCtx, cancel := context.WithCancel(context.Background())
	s.c = c
	s.id = c.subs.nextID()
	s.target = target
	s.lifetime = opts.Lifetime
	s.confirmed = opts.IssueConfirmed
	s.capacity = opts.BufferSize
	s.events = make(chan SubscriptionEvent, opts.BufferSize+1) // +1 reserved for Closed
	s.cancel = cancel
	s.done = make(chan struct{})
	s.state.Store(uint32(SubscriptionPending))
	c.subs.register(s)

	if err = s.issue(ctx, false); err != nil {
		c.subs.unregister(s.id)
		cancel()
		close(s.done)
		// Close of the client may be ending this subscription at the same
		// moment (it waits for done, then publishes Closed).
		s.mu.Lock()
		if !s.closed {
			s.closed = true
			close(s.events)
		}
		s.mu.Unlock()
		return nil, err
	}
	s.state.Store(uint32(SubscriptionActive))
	s.send(SubscriptionEvent{State: SubscriptionActive})

	c.wg.Add(1)
	go s.renewLoop(subCtx)
	return s, nil
}

// issue sends the request that establishes or renews s, or the one that
// cancels it.
func (s *subscription) issue(ctx context.Context, cancellation bool) error {
	base := service.SubscribeCOVRequest{
		ProcessIdentifier: s.id,
		MonitoredObject:   s.object,
		Cancellation:      cancellation,
	}
	if !cancellation {
		base.IssueConfirmed = s.confirmed
		base.Lifetime = s.lifetime
	}
	var (
		choice  uint8
		payload []byte
		err     error
	)
	switch {
	case s.multi != nil:
		choice = apdu.ServiceSubscribeCOVPropertyMultiple
		req := service.SubscribeCOVPropertyMultipleRequest{
			SubscriberProcessIdentifier: s.id,
			Cancellation:                cancellation,
			Subscriptions:               s.multi,
		}
		if !cancellation {
			req.IssueConfirmedNotifications = s.confirmed
			req.LifetimeRemaining = &s.lifetime
			req.MaxNotificationDelay = s.maxDelay
		}
		payload, err = service.EncodeSubscribeCOVPropertyMultiple(req)
	case s.property != nil:
		choice = apdu.ServiceSubscribeCOVProperty
		req := service.SubscribeCOVPropertyRequest{SubscribeCOVRequest: base, Property: *s.property}
		if !cancellation {
			req.COVIncrement = s.covIncrement
		}
		payload, err = service.EncodeSubscribeCOVProperty(req)
	default:
		return s.c.subscribeCOV(ctx, s.target, base)
	}
	if err != nil {
		return err
	}
	pdu, err := s.c.confirmedRequest(ctx, s.target, choice, payload, DefaultRetransmitPolicy(choice))
	if err != nil {
		return err
	}
	if !cancellation && pdu.Type != apdu.TypeSimpleACK {
		return bacnet.ErrProtocolViolation
	}
	return nil
}

func (s *subscription) renewLoop(ctx context.Context) {
	defer s.c.wg.Done()
	defer close(s.done)

	interval := time.Duration(s.lifetime) * time.Second / 2
	if interval < time.Second {
		interval = time.Second
	}
	for {
		timer := s.c.clock.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-s.c.closeCh:
			timer.Stop()
			return
		case <-timer.C():
			if SubscriptionState(s.state.Load()) == SubscriptionClosed {
				return
			}
			if !s.tryStoreState(SubscriptionRenewing) {
				return
			}
			s.send(SubscriptionEvent{State: SubscriptionRenewing})
			rctx, stop := clock.ContextWithTimeout(ctx, s.c.clock, s.c.cfg.apduTimeout)
			err := s.issue(rctx, false)
			stop()
			if SubscriptionState(s.state.Load()) == SubscriptionClosed {
				return
			}
			if err != nil {
				s.tryMarkDegraded()
				s.send(SubscriptionEvent{State: SubscriptionDegraded, Err: err})
				continue
			}
			if !s.tryStoreState(SubscriptionActive) {
				return
			}
			s.send(SubscriptionEvent{State: SubscriptionActive})
		}
	}
}

func decodeCOVNotification(payload []byte, limits bacnet.DecodeLimits) (*service.COVNotification, error) {
	n, err := service.DecodeCOVNotification(payload, limits)
	if err != nil {
		return nil, err
	}
	return &n, nil
}
