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
//
// The application's code (Hooks, objects, the diagnostic function) runs on
// the server's goroutines. Code that does not return normally is stopped
// there, reported as a diagnostic with its stack, and the server goes on
// (see Hooks). Close may be called from such code.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/internal/clock"
	"github.com/otfabric/go-bacnet/internal/diag"
	"github.com/otfabric/go-bacnet/internal/goid"
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
	closed atomic.Bool
	once   sync.Once

	// gr counts the server's goroutines for Close. gmu guards closing,
	// which stops new goroutines once Close has begun, so that none is
	// added while Close waits.
	gr      goroutines
	gmu     sync.Mutex
	closing bool

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
	s.gr.byID = make(map[uint64]*share)
	s.resp = newResponder(s)
	s.req = newRequester(s)
	s.cov = newCOVEngine(s)
	s.events = newEventEngine(s)
	s.routes = newRouteTable(cfg.recipLimits)
	dev.Bind(runtimeInfo{s})
	unCOV := dev.Observe(s.cov.changed)
	unEvents := dev.Observe(s.events.changed)
	s.unobs = func() { unCOV(); unEvents() }

	s.spawn(s.receiveLoop)
	for i := 0; i < cfg.workers; i++ {
		s.spawn(s.worker)
	}
	s.spawn(s.housekeeping)
	s.spawn(s.events.run)
	s.events.tick() // initial detection
	return s, nil
}

// goroutines counts the goroutines a server started and knows each by its
// goroutine identifier, so that Close can leave out the one it is called on.
type goroutines struct {
	wg   sync.WaitGroup
	mu   sync.Mutex
	byID map[uint64]*share
}

// share is the count one goroutine holds in goroutines.wg.
type share struct {
	wg   *sync.WaitGroup
	once sync.Once
}

// done gives the count back, once: when the goroutine ends, or earlier when
// Close is called on it.
func (sh *share) done() { sh.once.Do(sh.wg.Done) }

// start runs fn on a new goroutine counted in wg.
func (g *goroutines) start(fn func()) {
	sh := &share{wg: &g.wg}
	g.wg.Add(1)
	go func() {
		id := goid.ID()
		if id != 0 {
			g.mu.Lock()
			g.byID[id] = sh
			g.mu.Unlock()
		}
		defer func() {
			if id != 0 {
				g.mu.Lock()
				delete(g.byID, id)
				g.mu.Unlock()
			}
			sh.done()
		}()
		fn()
	}()
}

// release gives back the count of the calling goroutine when it is one of
// the counted ones, so that a wait on that goroutine does not wait for
// itself.
func (g *goroutines) release() {
	id := goid.ID()
	if id == 0 {
		return
	}
	g.mu.Lock()
	sh := g.byID[id]
	g.mu.Unlock()
	if sh != nil {
		sh.done()
	}
}

// spawn runs fn on a goroutine that Close waits for. It reports false, and
// starts nothing, once Close has begun.
func (s *Server) spawn(fn func()) bool {
	s.gmu.Lock()
	defer s.gmu.Unlock()
	if s.closing {
		return false
	}
	s.gr.start(fn)
	return true
}

// guard runs fn, which calls the application's code named name (a hook, an
// object, a BBMD). When that code does not return normally, guard stops it
// here, reports a diagnostic with what it ended with and its stack, and
// returns false; the server's goroutine goes on. fn is called with no lock
// of the server held, and the objects of package object release theirs
// before guard sees the failure.
func (s *Server) guard(name string, fields map[string]any, fn func()) (ok bool) {
	defer func() {
		if ok {
			return
		}
		s.diag.Report(callbackEvent(name, recover(), fields))
	}()
	fn()
	return true
}

// callbackEvent is the diagnostic for application code that did not return
// normally. It is built on the goroutine that ran the code, so the stack is
// the one that code ended on.
func callbackEvent(name string, value any, fields map[string]any) diag.Event {
	f := map[string]any{"callback": name, "value": fmt.Sprint(value), "stack": string(debug.Stack())}
	for k, v := range fields {
		f[k] = v
	}
	return diag.Event{Kind: diag.KindCallback, Message: "application code did not return normally", Fields: f}
}

// Device returns the served Device.
func (s *Server) Device() *object.Device { return s.dev }

// Local returns the local BACnet/IP endpoint.
func (s *Server) Local() bip.Endpoint { return s.tr.Local() }

// Close stops the server: it stops receiving, ends in-flight transactions
// and subscriptions, waits for every goroutine and closes the transport.
// Close is idempotent and safe to call from several goroutines; every call
// returns once the server is closed. After Close, IAm returns an error that
// matches bacnet.ErrClosed.
//
// Close may be called from the application's code that the server runs: a
// hook, a method of an object, the diagnostic function, an observer. It
// then waits for every goroutine of the server but the one it was called
// on, and returns. That goroutine ends by itself when the code that called
// Close returns: the request it was serving is completed without an answer
// on the network (the transport is closed), and no further request, poll
// or event evaluation is started on it. Do not call Close from a Property
// Get or Set function: those run with the object's lock held, which the
// goroutines Close waits for may need.
func (s *Server) Close() error {
	// On one of the server's own goroutines: give its count back first, so
	// that the wait below (in this call or in a Close that is already
	// waiting) does not include it.
	s.gr.release()
	var err error
	s.once.Do(func() {
		s.closed.Store(true)
		s.gmu.Lock()
		s.closing = true
		s.gmu.Unlock()
		s.unobs()
		s.cancel()
		err = s.tr.Close()
		s.gr.wg.Wait()
		s.resp.close()
		s.cov.close()
		s.dev.Bind(nil)
	})
	return err
}

// errClosed is what the server's methods return after Close. It matches
// bacnet.ErrClosed and, as these methods returned the cancelled server
// context before v0.9.1, context.Canceled.
var errClosed error = closedError{}

type closedError struct{}

func (closedError) Error() string { return "server: closed" }

func (closedError) Is(target error) bool {
	return target == bacnet.ErrClosed || target == context.Canceled
}

func (s *Server) receiveLoop() {
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
		// A BBMD given with WithBBMD and the diagnostic function are called
		// from here.
		s.guard("packet handling", nil, func() { s.handlePacket(pkt) })
	}
}

func (s *Server) worker() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case j := <-s.jobs:
			// Both cases can be ready: nothing is started once the server
			// is closing.
			if s.ctx.Err() != nil {
				return
			}
			s.guard("request handling", nil, func() { j.run(s.ctx) })
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
		if s.ctx.Err() != nil {
			return
		}
		if o, ok := s.dev.Object(id); ok {
			if p, ok := o.(object.Poller); ok {
				// One object that does not return normally does not keep
				// the others from their tick.
				s.guard("Poller.Poll", map[string]any{"object": id.String()}, func() { p.Poll(s.ctx, now, s.dev) })
			}
		}
	}
}

// housekeeping expires COV subscriptions, DCC durations and stale
// reassemblies, and gives the objects their tick. A Schedule writes its
// references from here, so the BeforeWrite hook also runs on this
// goroutine (see Hooks).
func (s *Server) housekeeping() {
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
			s.guard("event detection", nil, s.events.tick)
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
	log := func(e diag.Event) {
		level := slog.LevelDebug
		switch e.Kind {
		case diag.KindTransport, diag.KindQueueDrop, diag.KindRegistryEviction:
			level = slog.LevelWarn
		case diag.KindCallback:
			level = slog.LevelError
		default:
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
	// The diagnostic function and the logger's handler are the
	// application's code too. One that does not return normally is stopped
	// here: the function's failure goes to the logger, the handler's to
	// nobody, and the server goes on either way.
	return diag.Func(func(e diag.Event) {
		if fn != nil {
			failed := callSink(func() { fn(Diagnostic{Kind: string(e.Kind), Message: e.Message, Fields: e.Fields}) }, "diagnostic function")
			if failed != nil && l != nil {
				_ = callSink(func() { log(*failed) }, "logger")
			}
		}
		if l != nil {
			_ = callSink(func() { log(e) }, "logger")
		}
	})
}

// callSink calls a diagnostic sink of the application. It returns nil when
// the sink returned normally, else the diagnostic that reports it.
func callSink(call func(), name string) (failed *diag.Event) {
	done := false
	defer func() {
		if !done {
			e := callbackEvent(name, recover(), nil)
			failed = &e
		}
	}()
	call()
	done = true
	return nil
}
