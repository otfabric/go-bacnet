// SPDX-License-Identifier: MIT

package client

import (
	"sync"
	"sync/atomic"

	"github.com/otfabric/go-bacnet/service"
)

// EventStreamEvent is one queued EventNotification delivery.
//
// Sequence is monotonic for successfully queued events. Gap is sticky: when
// one or more events were dropped due to a full buffer, the next delivered
// event has Gap=true.
type EventStreamEvent struct {
	Sequence uint64
	Delivery EventNotificationDelivery
	Gap      bool
	Closed   bool
}

// EventStream is a bounded asynchronous EventNotification consumer.
type EventStream interface {
	Events() <-chan EventStreamEvent
	Close()
}

type eventStream struct {
	c         *Client
	capacity  int
	ch        chan EventStreamEvent
	seq       atomic.Uint64
	mu        sync.Mutex
	closed    bool
	gap       bool
	closeOnce sync.Once
}

func (s *eventStream) Events() <-chan EventStreamEvent { return s.ch }

func (s *eventStream) Close() {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		select {
		case s.ch <- EventStreamEvent{Closed: true, Sequence: s.seq.Add(1)}:
		default:
			select {
			case <-s.ch:
			default:
			}
			select {
			case s.ch <- EventStreamEvent{Closed: true, Sequence: s.seq.Add(1)}:
			default:
			}
		}
		close(s.ch)
		s.mu.Unlock()
		if s.c != nil {
			s.c.eventMu.Lock()
			if s.c.eventStream == s {
				s.c.eventStream = nil
			}
			s.c.eventMu.Unlock()
		}
	})
}

func (s *eventStream) publish(d EventNotificationDelivery) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	ev := EventStreamEvent{
		Sequence: s.seq.Add(1),
		Delivery: d,
		Gap:      s.gap,
	}
	select {
	case s.ch <- ev:
		s.gap = false
	default:
		s.gap = true
	}
}

// OpenEventStream installs a bounded EventNotification stream.
//
// BufferSize is the soft capacity for notification events (Closed uses an
// extra slot when possible). The synchronous handler from
// SetEventNotificationHandler / WithEventNotificationHandler still runs when
// set; the stream is additive.
//
// Only one stream may be open; OpenEventStream replaces a previous stream
// after closing it.
func (c *Client) OpenEventStream(bufferSize int) EventStream {
	if bufferSize < 1 {
		bufferSize = 1
	}
	s := &eventStream{
		c:        c,
		capacity: bufferSize,
		ch:       make(chan EventStreamEvent, bufferSize+1),
	}
	c.eventMu.Lock()
	prev := c.eventStream
	c.eventStream = s
	c.eventMu.Unlock()
	if prev != nil {
		prev.Close()
	}
	return s
}

// EventTransition describes FromState → ToState for an EventNotification.
type EventTransition struct {
	FromState   uint32
	ToState     uint32
	AckRequired bool
	EventType   uint32
	NotifyType  uint32
}

// TransitionOf extracts transition fields from a decoded EventNotification.
func TransitionOf(note service.EventNotification) EventTransition {
	var t EventTransition
	t.EventType = note.EventType
	t.NotifyType = note.NotifyType
	if note.FromState != nil {
		t.FromState = *note.FromState
	}
	t.ToState = note.ToState
	if note.AckRequired != nil {
		t.AckRequired = *note.AckRequired
	}
	return t
}
