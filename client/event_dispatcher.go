// SPDX-License-Identifier: MIT

package client

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/otfabric/go-bacnet/internal/diag"
)

// EventOverflowPolicy controls dispatcher queue overflow.
type EventOverflowPolicy uint8

const (
	EventOverflowDropNewest EventOverflowPolicy = iota
	EventOverflowDropOldest
)

// EventDispatcherConfig configures an optional asynchronous event dispatcher.
//
// When unset, EventNotificationHandler remains synchronous on the receive path.
// With Workers=1, delivery order matches enqueue order. Panic recovery is
// always applied. Close drains or abandons the queue and stops workers.
type EventDispatcherConfig struct {
	Workers    int
	BufferSize int
	Overflow   EventOverflowPolicy
	Handler    EventNotificationHandler // required when enabling the dispatcher
}

type eventDispatcher struct {
	cfg       EventDispatcherConfig
	ch        chan EventNotificationDelivery
	diag      diag.Sink
	wg        sync.WaitGroup
	mu        sync.Mutex
	closed    bool
	dropped   atomic.Uint64
	closeOnce sync.Once
}

func newEventDispatcher(cfg EventDispatcherConfig, sink diag.Sink) *eventDispatcher {
	if cfg.Workers <= 0 {
		cfg.Workers = 1
	}
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = 128
	}
	d := &eventDispatcher{
		cfg:  cfg,
		ch:   make(chan EventNotificationDelivery, cfg.BufferSize),
		diag: sink,
	}
	for i := 0; i < cfg.Workers; i++ {
		d.wg.Add(1)
		go d.loop()
	}
	return d
}

func (d *eventDispatcher) loop() {
	defer d.wg.Done()
	for delivery := range d.ch {
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					d.diag.Report(diag.Event{
						Kind:    diag.KindUnexpectedAPDU,
						Message: "EventDispatcher handler panic",
						Fields:  map[string]any{"panic": fmt.Sprint(rec)},
					})
				}
			}()
			if d.cfg.Handler != nil {
				d.cfg.Handler(delivery)
			}
		}()
	}
}

func (d *eventDispatcher) publish(delivery EventNotificationDelivery) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}
	select {
	case d.ch <- delivery:
		return
	default:
	}
	d.dropped.Add(1)
	d.diag.Report(diag.Event{
		Kind:    diag.KindUnexpectedAPDU,
		Message: "EventDispatcher overflow",
		Fields:  map[string]any{"dropped_total": d.dropped.Load(), "policy": d.cfg.Overflow},
	})
	switch d.cfg.Overflow {
	case EventOverflowDropOldest:
		select {
		case <-d.ch:
		default:
		}
		select {
		case d.ch <- delivery:
		default:
		}
	case EventOverflowDropNewest:
		// Drop the new delivery.
	}
}

func (d *eventDispatcher) close() {
	d.closeOnce.Do(func() {
		d.mu.Lock()
		d.closed = true
		close(d.ch)
		d.mu.Unlock()
		d.wg.Wait()
	})
}

// Dropped returns the number of overflow drops observed.
func (d *eventDispatcher) Dropped() uint64 { return d.dropped.Load() }
