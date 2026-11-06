// SPDX-License-Identifier: MIT

// Package server is a BACnet/IP server: it serves one Device (package
// server/object) on one BACnet/IP endpoint.
//
// One receive loop parses every frame. Confirmed and unconfirmed requests
// are executed by a bounded pool of workers; the receive loop itself only
// answers what needs no handler (Reject, Abort, repeated requests) and
// drives segmentation. COV and event notifications are sent by the server's
// own requester, with retries for confirmed ones.
//
// The server is silent unless WithLogger or WithDiagnosticFunc is given,
// keeps no package-level state, and Close waits for every goroutine it
// started.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/internal/clock"
	"github.com/otfabric/go-bacnet/internal/diag"
	"github.com/otfabric/go-bacnet/server/object"
)

// ErrInvalidConfig wraps option errors returned by New.
var ErrInvalidConfig = bacnet.ErrInvalidConfig

// Server serves one Device.
type Server struct {
	cfg    config
	dev    *object.Device
	tr     bip.Transport
	limits bacnet.DecodeLimits
	diag   diag.Sink
	clock  clock.Clock

	ctx    context.Context
	cancel context.CancelFunc
	jobs   chan job
	wg     sync.WaitGroup
	closed atomic.Bool
	once   sync.Once

	resp   *responder
	req    *requester
	cov    *covEngine
	dcc    dccState
	events *eventEngine
	routes *routeTable
	unobs  func()
}

type job struct {
	run func(ctx context.Context)
}

// New starts a server for dev. It opens a UDP socket unless WithTransport
// is given, and starts the receive loop and the workers.
func New(dev *object.Device, opts ...Option) (*Server, error) {
	if dev == nil {
		return nil, fmt.Errorf("%w: nil device", ErrInvalidConfig)
	}
	cfg := defaultConfig()
	for _, o := range opts {
		o(&cfg)
	}
	if len(cfg.errs) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrInvalidConfig, strings.Join(cfg.errs, "; "))
	}
	tr := cfg.transport
	if tr == nil {
		var err error
		tr, err = bip.ListenUDP(bip.UDPConfig{Port: cfg.port, LocalAddr: cfg.localAddr, Interface: cfg.iface})
		if err != nil {
			return nil, err
		}
	}
	s := &Server{
		cfg:    cfg,
		dev:    dev,
		tr:     tr,
		limits: cfg.limits,
		diag:   newDiagSink(cfg.logger, cfg.diagFn),
		clock:  cfg.clock,
		jobs:   make(chan job, cfg.queue),
	}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.resp = newResponder(s)
	s.req = newRequester(s)
	s.cov = newCOVEngine(s)
	s.events = newEventEngine(s)
	s.routes = newRouteTable()
	dev.Bind(runtimeInfo{s})
	unCOV := dev.Observe(s.cov.changed)
	unEvents := dev.Observe(s.events.changed)
	s.unobs = func() { unCOV(); unEvents() }

	s.wg.Add(1)
	go s.receiveLoop()
	for i := 0; i < cfg.workers; i++ {
		s.wg.Add(1)
		go s.worker()
	}
	s.wg.Add(2)
	go s.housekeeping()
	go s.events.run()
	s.events.tick() // initial detection
	return s, nil
}

// Device returns the served Device.
func (s *Server) Device() *object.Device { return s.dev }

// Local returns the local BACnet/IP endpoint.
func (s *Server) Local() bip.Endpoint { return s.tr.Local() }

// Close stops the server: it stops receiving, ends in-flight transactions
// and subscriptions, waits for every goroutine and closes the transport.
// Close is idempotent.
func (s *Server) Close() error {
	var err error
	s.once.Do(func() {
		s.closed.Store(true)
		s.unobs()
		s.cancel()
		err = s.tr.Close()
		s.wg.Wait()
		s.resp.close()
		s.req.close()
		s.cov.close()
		s.dev.Bind(nil)
	})
	return err
}

func (s *Server) receiveLoop() {
	defer s.wg.Done()
	backoff := time.Duration(0)
	for {
		pkt, err := s.tr.Recv(s.ctx)
		if err != nil {
			if s.ctx.Err() != nil || s.closed.Load() || errors.Is(err, bacnet.ErrClosed) {
				return
			}
			s.report(diag.KindTransport, "receive failed", map[string]any{"error": err.Error()})
			// A failing socket must not spin: back off up to one second.
			if backoff == 0 {
				backoff = 10 * time.Millisecond
			} else if backoff < time.Second {
				backoff *= 2
			}
			select {
			case <-s.ctx.Done():
				return
			case <-s.clock.After(backoff):
			}
			continue
		}
		backoff = 0
		s.handlePacket(pkt)
	}
}

func (s *Server) worker() {
	defer s.wg.Done()
	for {
		select {
		case <-s.ctx.Done():
			return
		case j := <-s.jobs:
			j.run(s.ctx)
		}
	}
}

// submit queues work for a worker; it reports false when the queue is full
// or the server is closing.
func (s *Server) submit(fn func(ctx context.Context)) bool {
	if s.closed.Load() {
		return false
	}
	select {
	case s.jobs <- job{run: fn}:
		return true
	default:
		return false
	}
}

// poll gives objects that act on time (polled Trend Logs, event time
// delays) their tick.
func (s *Server) poll(now time.Time) {
	for _, id := range s.dev.ObjectIdentifiers() {
		if o, ok := s.dev.Object(id); ok {
			if p, ok := o.(object.Poller); ok {
				p.Poll(s.ctx, now, s.dev)
			}
		}
	}
}

// housekeeping expires COV subscriptions, DCC durations and stale reassemblies.
func (s *Server) housekeeping() {
	defer s.wg.Done()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.clock.After(time.Second):
			now := s.clock.Now()
			s.cov.expire(now)
			s.dcc.expire(now)
			s.resp.expire(now)
			s.poll(now)
			s.events.tick()
		}
	}
}

func (s *Server) report(kind diag.Kind, msg string, fields map[string]any) {
	s.diag.Report(diag.Event{Kind: kind, Message: msg, Fields: fields})
}

func newDiagSink(l *slog.Logger, fn func(Diagnostic)) diag.Sink {
	if l == nil && fn == nil {
		return diag.Discard{}
	}
	return diag.Func(func(e diag.Event) {
		if fn != nil {
			fn(Diagnostic{Kind: string(e.Kind), Message: e.Message, Fields: e.Fields})
		}
		if l != nil {
			level := slog.LevelDebug
			if e.Kind == diag.KindTransport || e.Kind == diag.KindQueueDrop {
				level = slog.LevelWarn
			}
			if !l.Enabled(context.Background(), level) {
				return
			}
			keys := make([]string, 0, len(e.Fields))
			for k := range e.Fields {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			attrs := []slog.Attr{slog.String("kind", string(e.Kind))}
			for _, k := range keys {
				attrs = append(attrs, slog.Any(k, e.Fields[k]))
			}
			l.LogAttrs(context.Background(), level, e.Message, attrs...)
		}
	})
}
