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
type SubscriptionEvent struct {
	Sequence     uint64
	Notification *service.COVNotification
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
		if s.property != nil {
			payload, e := service.EncodeSubscribeCOVProperty(service.SubscribeCOVPropertyRequest{
				SubscribeCOVRequest: service.SubscribeCOVRequest{
					ProcessIdentifier: s.id,
					MonitoredObject:   s.object,
					Cancellation:      true,
				},
				Property: *s.property,
			})
			if e == nil {
				_, err = s.c.confirmedRequest(ctx, s.target, apdu.ServiceSubscribeCOVProperty, payload, DefaultRetransmitPolicy(apdu.ServiceSubscribeCOVProperty))
			} else {
				err = e
			}
		} else {
			err = s.c.subscribeCOV(ctx, s.target, service.SubscribeCOVRequest{
				ProcessIdentifier: s.id,
				MonitoredObject:   s.object,
				Cancellation:      true,
			})
		}
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
	if s == nil || ev.Notification == nil {
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
	Lifetime       uint32 // seconds
	IssueConfirmed bool
	BufferSize     int
}

// SubscribeCOV starts a COV subscription.
func (c *Client) SubscribeCOV(ctx context.Context, target Target, object bacnet.ObjectIdentifier, opts COVOptions) (Subscription, error) {
	if opts.Lifetime == 0 {
		opts.Lifetime = 60
	}
	if opts.BufferSize <= 0 {
		opts.BufferSize = 16
	}
	id := c.subs.nextID()
	subCtx, cancel := context.WithCancel(context.Background())
	s := &subscription{
		c:         c,
		id:        id,
		target:    target,
		object:    object,
		lifetime:  opts.Lifetime,
		confirmed: opts.IssueConfirmed,
		capacity:  opts.BufferSize,
		events:    make(chan SubscriptionEvent, opts.BufferSize+1), // +1 reserved for Closed
		cancel:    cancel,
		done:      make(chan struct{}),
	}
	s.state.Store(uint32(SubscriptionPending))
	c.subs.register(s)

	req := service.SubscribeCOVRequest{
		ProcessIdentifier: id,
		MonitoredObject:   object,
		IssueConfirmed:    opts.IssueConfirmed,
		Lifetime:          opts.Lifetime,
	}
	if err := c.subscribeCOV(ctx, target, req); err != nil {
		c.subs.unregister(id)
		cancel()
		close(s.done)
		s.mu.Lock()
		s.closed = true
		close(s.events)
		s.mu.Unlock()
		return nil, err
	}
	s.state.Store(uint32(SubscriptionActive))
	s.send(SubscriptionEvent{State: SubscriptionActive})

	c.wg.Add(1)
	go s.renewLoop(subCtx)
	return s, nil
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
	if opts.Lifetime == 0 {
		opts.Lifetime = 60
	}
	if opts.BufferSize <= 0 {
		opts.BufferSize = 16
	}
	id := c.subs.nextID()
	subCtx, cancel := context.WithCancel(context.Background())
	prop := property
	var incCopy *float32
	if covIncrement != nil {
		v := *covIncrement
		incCopy = &v
	}
	s := &subscription{
		c:            c,
		id:           id,
		target:       target,
		object:       object,
		property:     &prop,
		covIncrement: incCopy,
		lifetime:     opts.Lifetime,
		confirmed:    opts.IssueConfirmed,
		capacity:     opts.BufferSize,
		events:       make(chan SubscriptionEvent, opts.BufferSize+1),
		cancel:       cancel,
		done:         make(chan struct{}),
	}
	s.state.Store(uint32(SubscriptionPending))
	c.subs.register(s)

	req := service.SubscribeCOVPropertyRequest{
		SubscribeCOVRequest: service.SubscribeCOVRequest{
			ProcessIdentifier: id,
			MonitoredObject:   object,
			IssueConfirmed:    opts.IssueConfirmed,
			Lifetime:          opts.Lifetime,
		},
		Property:     property,
		COVIncrement: incCopy,
	}
	payload, err := service.EncodeSubscribeCOVProperty(req)
	if err != nil {
		c.subs.unregister(id)
		cancel()
		close(s.done)
		s.mu.Lock()
		s.closed = true
		close(s.events)
		s.mu.Unlock()
		return nil, err
	}
	pdu, err := c.confirmedRequest(ctx, target, apdu.ServiceSubscribeCOVProperty, payload, DefaultRetransmitPolicy(apdu.ServiceSubscribeCOVProperty))
	if err != nil {
		c.subs.unregister(id)
		cancel()
		close(s.done)
		s.mu.Lock()
		s.closed = true
		close(s.events)
		s.mu.Unlock()
		return nil, err
	}
	if pdu.Type != apdu.TypeSimpleACK {
		c.subs.unregister(id)
		cancel()
		close(s.done)
		s.mu.Lock()
		s.closed = true
		close(s.events)
		s.mu.Unlock()
		return nil, bacnet.ErrProtocolViolation
	}
	s.state.Store(uint32(SubscriptionActive))
	s.send(SubscriptionEvent{State: SubscriptionActive})
	c.wg.Add(1)
	go s.renewLoop(subCtx)
	return s, nil
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
			var err error
			var pdu apdu.PDU
			if s.property != nil {
				payload, e := service.EncodeSubscribeCOVProperty(service.SubscribeCOVPropertyRequest{
					SubscribeCOVRequest: service.SubscribeCOVRequest{
						ProcessIdentifier: s.id,
						MonitoredObject:   s.object,
						IssueConfirmed:    s.confirmed,
						Lifetime:          s.lifetime,
					},
					Property:     *s.property,
					COVIncrement: s.covIncrement,
				})
				if e != nil {
					err = e
				} else {
					pdu, err = s.c.confirmedRequest(rctx, s.target, apdu.ServiceSubscribeCOVProperty, payload, DefaultRetransmitPolicy(apdu.ServiceSubscribeCOVProperty))
					if err == nil && pdu.Type != apdu.TypeSimpleACK {
						err = bacnet.ErrProtocolViolation
					}
				}
			} else {
				err = s.c.subscribeCOV(rctx, s.target, service.SubscribeCOVRequest{
					ProcessIdentifier: s.id,
					MonitoredObject:   s.object,
					IssueConfirmed:    s.confirmed,
					Lifetime:          s.lifetime,
				})
			}
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
