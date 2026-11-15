// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/server/object"
	"github.com/otfabric/go-bacnet/service"
)

// The lifecycle contract of Server (rows L2, L3, L9, L10, L11 of the table
// in TESTING.md) and what the server does with application code that does
// not return normally. Every test runs in a synctest bubble: it waits for
// events, the bound of a wait is virtual time, and the bubble ends only
// when every goroutine the server started has ended.

// within fails the test when ch does not deliver within d of virtual time.
func within[T any](t *testing.T, ch <-chan T, d time.Duration, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(d):
		t.Fatalf("%s: not within %v", what, d)
		var zero T
		return zero
	}
}

// appObject is an object of the application: a Base whose methods the test
// replaces.
type appObject struct {
	*object.Base
	read func(ref bacnet.PropertyReference) // called first by ReadProperty
}

func newAppObject(t *testing.T, instance uint32, name string) *appObject {
	t.Helper()
	b := object.NewBase(bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: instance}, name)
	b.Define(bacnet.PropertyPresentValue, &object.Property{Required: true, Writable: true, Value: bacnet.RealValue(1), Kinds: []bacnet.ValueKind{bacnet.ValueReal}})
	return &appObject{Base: b}
}

func (o *appObject) ReadProperty(ctx context.Context, ref bacnet.PropertyReference) (bacnet.ApplicationValue, error) {
	if o.read != nil {
		o.read(ref)
	}
	return o.Base.ReadProperty(ctx, ref)
}

// pollObject is an application object that acts on the server's tick.
type pollObject struct {
	*appObject
	poll func()
}

func (o *pollObject) Poll(context.Context, time.Time, *object.Device) { o.poll() }

// reportingObject is an application object with event detection of its own.
type reportingObject struct {
	*appObject
	evaluate func()
}

func (o *reportingObject) EvaluateEvents(time.Time, func(uint32, int) bool) []object.EventTransition {
	o.evaluate()
	return nil
}

func (o *reportingObject) EventInfo() (object.EventInfo, bool) { return object.EventInfo{}, false }

func (o *reportingObject) AcknowledgeAlarm(uint32, service.TimeStamp) (object.EventTransition, error) {
	return object.EventTransition{}, object.ErrNoAlarmConfigured
}

func readOf(t *testing.T, id bacnet.ObjectIdentifier) []byte {
	return mustEncode(t)(service.EncodeReadProperty(service.ReadPropertyRequest{Object: id, Property: pvRef}))
}

func writeOf(t *testing.T, id bacnet.ObjectIdentifier, v float32) []byte {
	return mustEncode(t)(service.EncodeWriteProperty(service.WritePropertyRequest{Object: id, Property: pvRef, Value: bacnet.RealValue(v)}))
}

func timeSyncPayload(t *testing.T) []byte {
	return mustEncode(t)(service.EncodeTimeSynchronization(service.TimeSynchronization{Date: bacnet.Date{Year: 126, Month: 1, Day: 1, Weekday: 4}, Time: bacnet.Time{Hour: 1}}))
}

var app2 = bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 2}

// L2: Close from two goroutines. Both calls return, and every goroutine of
// the server has ended when the bubble does.
func TestCloseFromTwoGoroutines(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		h := newHarness(t)
		h.confirmed(1, apdu.ServiceReadProperty, readPV(t))
		h.expectType(apdu.TypeComplexACK)
		errs := make(chan error, 2)
		for range 2 {
			go func() { errs <- h.s.Close() }()
		}
		for range 2 {
			if err := within(t, errs, 10*time.Second, "Close"); err != nil {
				t.Fatalf("Close: %v", err)
			}
		}
		if h.s.submit(func(context.Context) {}) {
			t.Fatal("work accepted after Close")
		}
	})
}

// L3 and the Close contract: Close while a handler waits in a hook cancels
// the hook's context, returns when the hook has returned, and IAm then
// reports the closed server with an error that matches bacnet.ErrClosed
// (and context.Canceled, which it was before v0.9.1).
func TestCloseWhileHookWaits(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		entered := make(chan struct{})
		var hookErr atomic.Value
		h := newHarness(t, WithHooks(Hooks{BeforeWrite: func(ctx context.Context, _ bacnet.ObjectIdentifier, _ object.Write) error {
			close(entered)
			<-ctx.Done()
			hookErr.Store(ctx.Err())
			return ctx.Err()
		}}))
		h.confirmed(1, apdu.ServiceWriteProperty, writeOf(t, av1, 5))
		within(t, entered, 10*time.Second, "the hook is called")
		closed := make(chan error, 1)
		go func() { closed <- h.s.Close() }()
		if err := within(t, closed, 10*time.Second, "Close while a hook waits"); err != nil {
			t.Fatalf("Close: %v", err)
		}
		if err, _ := hookErr.Load().(error); !errors.Is(err, context.Canceled) {
			t.Fatalf("the hook's context ended with %v", err)
		}
		err := h.s.IAm()
		if !errors.Is(err, bacnet.ErrClosed) || !errors.Is(err, context.Canceled) {
			t.Fatalf("IAm after Close: %v; matches ErrClosed %v, context.Canceled %v", err, errors.Is(err, bacnet.ErrClosed), errors.Is(err, context.Canceled))
		}
		h.nothingSent()
	})
}

// L9: a hook may call the server's methods. The I-Am goes out, and the
// request the hook belongs to is answered.
func TestHookCallsServerMethods(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		var srv atomic.Pointer[Server]
		h := newHarness(t, WithHooks(Hooks{BeforeWrite: func(context.Context, bacnet.ObjectIdentifier, object.Write) error {
			s := srv.Load()
			if s.Device().Identifier().Instance != 100 || s.Local() != serverEP {
				return object.ErrWriteAccessDenied
			}
			return s.IAm()
		}}))
		srv.Store(h.s)
		h.confirmed(1, apdu.ServiceWriteProperty, writeOf(t, av1, 5))
		iam := h.expectType(apdu.TypeUnconfirmedRequest)
		if iam.pdu.UnconfirmedRequest.ServiceChoice != apdu.ServiceIAm || !iam.pkt.Broadcast {
			t.Fatalf("sent from the hook: %+v", iam.pdu)
		}
		h.expectType(apdu.TypeSimpleACK)
	})
}

// L10: Close called from the application's code on one of the server's
// goroutines returns, on whichever goroutine that code runs, and the
// server's goroutines end.
func TestCloseFromApplicationCode(t *testing.T) {
	for _, tc := range []struct {
		name string
		// arrange builds the server so that call runs on the named
		// goroutine once trigger is done.
		arrange func(t *testing.T, call func()) (h *harness, trigger func())
	}{
		{"hook BeforeWrite on a worker", func(t *testing.T, call func()) (*harness, func()) {
			h := newHarness(t, WithHooks(Hooks{BeforeWrite: func(context.Context, bacnet.ObjectIdentifier, object.Write) error {
				call()
				return nil
			}}))
			return h, func() { h.confirmed(1, apdu.ServiceWriteProperty, writeOf(t, av1, 5)) }
		}},
		{"hook TimeSync on a worker", func(t *testing.T, call func()) (*harness, func()) {
			h := newHarness(t, WithHooks(Hooks{TimeSync: func(service.TimeSynchronization, bool) { call() }}))
			return h, func() { h.unconfirmed(apdu.ServiceTimeSynchronization, timeSyncPayload(t)) }
		}},
		{"object ReadProperty on a worker", func(t *testing.T, call func()) (*harness, func()) {
			h := newHarness(t)
			o := newAppObject(t, 2, "APP-2")
			o.read = func(bacnet.PropertyReference) { call() }
			if err := h.dev.Add(o); err != nil {
				t.Fatal(err)
			}
			return h, func() { h.confirmed(1, apdu.ServiceReadProperty, readOf(t, app2)) }
		}},
		{"diagnostic function on the receive loop", func(t *testing.T, call func()) (*harness, func()) {
			h := newHarness(t, WithDiagnosticFunc(func(d Diagnostic) {
				if d.Kind == "malformed" {
					call()
				}
			}))
			return h, func() { h.injectRaw([]byte{0x81, 0x0A, 0x00}) }
		}},
		{"object Poll on the housekeeping goroutine", func(t *testing.T, call func()) (*harness, func()) {
			h := newHarness(t)
			o := &pollObject{appObject: newAppObject(t, 2, "APP-2"), poll: call}
			if err := h.dev.Add(o); err != nil {
				t.Fatal(err)
			}
			return h, func() { time.Sleep(time.Second) } // virtual: the next tick
		}},
		{"object EvaluateEvents on the event goroutine", func(t *testing.T, call func()) (*harness, func()) {
			h := newHarness(t)
			armed := atomic.Bool{}
			o := &reportingObject{appObject: newAppObject(t, 2, "APP-2")}
			o.evaluate = func() {
				if armed.Load() {
					call()
				}
			}
			if err := h.dev.Add(o); err != nil {
				t.Fatal(err)
			}
			return h, func() {
				armed.Store(true)
				h.confirmed(1, apdu.ServiceWriteProperty, writeOf(t, app2, 5)) // the change marks the object
			}
		}},
		{"object ReadProperty for a COV notification", func(t *testing.T, call func()) (*harness, func()) {
			h := newHarness(t)
			armed := atomic.Bool{}
			o := newAppObject(t, 2, "APP-2")
			o.read = func(bacnet.PropertyReference) {
				if armed.Load() {
					call()
				}
			}
			if err := h.dev.Add(o); err != nil {
				t.Fatal(err)
			}
			sub := mustEncode(t)(service.EncodeSubscribeCOVProperty(service.SubscribeCOVPropertyRequest{
				SubscribeCOVRequest: service.SubscribeCOVRequest{ProcessIdentifier: 1, MonitoredObject: app2, Lifetime: 60},
				Property:            pvRef,
			}))
			h.confirmed(1, apdu.ServiceSubscribeCOVProperty, sub)
			h.expectType(apdu.TypeSimpleACK)
			h.expectType(apdu.TypeUnconfirmedRequest) // the initial notification
			idle()
			return h, func() {
				armed.Store(true)
				// A change from the application: the notification is built on
				// a goroutine of the server.
				if err := o.WriteProperty(context.Background(), object.Write{Property: pvRef, Value: bacnet.RealValue(7)}); err != nil {
					t.Error(err)
				}
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inBubble(t, func(t *testing.T) {
				// The code under test runs on the server's goroutines: it
				// gets the server through an atomic pointer, set before
				// the trigger.
				var srv atomic.Pointer[Server]
				var once sync.Once
				closed := make(chan error, 1)
				h, trigger := tc.arrange(t, func() { once.Do(func() { closed <- srv.Load().Close() }) })
				srv.Store(h.s)
				idle()
				trigger()
				if err := within(t, closed, 30*time.Second, "Close called from "+tc.name); err != nil {
					t.Fatalf("Close: %v", err)
				}
				// The server is closed for everyone, and a later Close returns.
				if err := h.s.IAm(); !errors.Is(err, bacnet.ErrClosed) {
					t.Fatalf("IAm after Close: %v", err)
				}
				again := make(chan error, 1)
				go func() { again <- h.s.Close() }()
				if err := within(t, again, 10*time.Second, "a second Close"); err != nil {
					t.Fatalf("second Close: %v", err)
				}
			})
		})
	}
}

// Two hooks on two workers call Close at the same time: both return.
func TestCloseFromTwoHooks(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		var srv atomic.Pointer[Server]
		var inHook sync.WaitGroup
		inHook.Add(2)
		release := make(chan struct{})
		closed := make(chan error, 2)
		h := newHarness(t, WithWorkers(2, 4), WithHooks(Hooks{BeforeWrite: func(context.Context, bacnet.ObjectIdentifier, object.Write) error {
			inHook.Done()
			<-release
			closed <- srv.Load().Close()
			return nil
		}}))
		srv.Store(h.s)
		h.confirmed(1, apdu.ServiceWriteProperty, writeOf(t, av1, 5))
		h.confirmed(2, apdu.ServiceWriteProperty, writeOf(t, av1, 6))
		inHook.Wait()
		close(release)
		for range 2 {
			if err := within(t, closed, 30*time.Second, "Close called from two hooks"); err != nil {
				t.Fatalf("Close: %v", err)
			}
		}
	})
}

// outOfRange ends the calling code the way an index beyond a slice does.
func outOfRange(i int) int { return []int{0}[i] }

// Application code that does not return normally is stopped at the server:
// a confirmed request is answered with Abort (other), a diagnostic of kind
// "callback" names the code with its stack, the object's lock is free, and
// the server serves the next request.
func TestApplicationCodeThatDoesNotReturnNormally(t *testing.T) {
	for _, tc := range []struct {
		name string
		// arrange builds the server; fail is the code that does not return
		// normally. trigger makes the server call it; abort says that the
		// trigger is a confirmed request with invoke ID 1.
		arrange func(t *testing.T, fail func()) (h *harness, trigger func())
		abort   bool
	}{
		{name: "hook BeforeWrite", abort: true, arrange: func(t *testing.T, fail func()) (*harness, func()) {
			h := newHarness(t, WithHooks(Hooks{BeforeWrite: func(_ context.Context, id bacnet.ObjectIdentifier, _ object.Write) error {
				if id == av1 {
					fail()
				}
				return nil
			}}))
			return h, func() { h.confirmed(1, apdu.ServiceWriteProperty, writeOf(t, av1, 5)) }
		}},
		{name: "hook TimeSync", arrange: func(t *testing.T, fail func()) (*harness, func()) {
			h := newHarness(t, WithHooks(Hooks{TimeSync: func(service.TimeSynchronization, bool) { fail() }}))
			return h, func() { h.unconfirmed(apdu.ServiceTimeSynchronization, timeSyncPayload(t)) }
		}},
		{name: "object ReadProperty", abort: true, arrange: func(t *testing.T, fail func()) (*harness, func()) {
			h := newHarness(t)
			o := newAppObject(t, 2, "APP-2")
			o.read = func(bacnet.PropertyReference) { fail() }
			if err := h.dev.Add(o); err != nil {
				t.Fatal(err)
			}
			return h, func() { h.confirmed(1, apdu.ServiceReadProperty, readOf(t, app2)) }
		}},
		{name: "property Set, with the object's lock held", abort: true, arrange: func(t *testing.T, fail func()) (*harness, func()) {
			h := newHarness(t)
			h.av.Define(bacnet.PropertyDescription, &object.Property{Writable: true, Value: object.CharString(""),
				Set: func(object.Write) ([]bacnet.PropertyIdentifier, error) { fail(); return nil, nil }})
			w := mustEncode(t)(service.EncodeWriteProperty(service.WritePropertyRequest{Object: av1,
				Property: bacnet.PropertyReference{Identifier: bacnet.PropertyDescription}, Value: object.CharString("x")}))
			return h, func() { h.confirmed(1, apdu.ServiceWriteProperty, w) }
		}},
		{name: "object Poll", arrange: func(t *testing.T, fail func()) (*harness, func()) {
			h := newHarness(t)
			o := &pollObject{appObject: newAppObject(t, 2, "APP-2"), poll: fail}
			if err := h.dev.Add(o); err != nil {
				t.Fatal(err)
			}
			return h, func() { time.Sleep(time.Second) }
		}},
		{name: "object EvaluateEvents", arrange: func(t *testing.T, fail func()) (*harness, func()) {
			h := newHarness(t)
			o := &reportingObject{appObject: newAppObject(t, 2, "APP-2"), evaluate: fail}
			if err := h.dev.Add(o); err != nil {
				t.Fatal(err)
			}
			return h, func() { h.s.events.mark(app2) }
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inBubble(t, func(t *testing.T) {
				armed := atomic.Bool{}
				h, trigger := tc.arrange(t, func() {
					if armed.Load() {
						outOfRange(3)
					}
				})
				idle()
				armed.Store(true)
				trigger()
				if tc.abort {
					got := h.expectType(apdu.TypeAbort)
					if a := got.pdu.Abort; a.InvokeID != 1 || !a.Server || a.Reason != bacnet.AbortReasonOther {
						t.Fatalf("answer %+v, want Abort (other) for invoke ID 1", a)
					}
				}
				idle()
				armed.Store(false)
				ds := h.diagnostics("callback")
				if len(ds) == 0 {
					t.Fatal("no diagnostic of kind callback")
				}
				d := ds[0]
				stack, _ := d.Fields["stack"].(string)
				if d.Message != "application code did not return normally" || d.Fields["callback"] == "" ||
					!strings.Contains(stack, "outOfRange") || !strings.Contains(d.Fields["value"].(string), "index out of range") {
					t.Fatalf("diagnostic %q, fields callback=%v value=%v, stack names the code: %v", d.Message, d.Fields["callback"], d.Fields["value"], strings.Contains(stack, "outOfRange"))
				}
				// The server goes on: the object answers, a write is applied.
				h.confirmed(2, apdu.ServiceReadProperty, readPV(t))
				h.expectType(apdu.TypeComplexACK)
				h.confirmed(3, apdu.ServiceWriteProperty, writeOf(t, av1, 9))
				h.expectType(apdu.TypeSimpleACK)
				h.confirmed(4, apdu.ServiceReadProperty, mustEncode(t)(service.EncodeReadProperty(service.ReadPropertyRequest{Object: h.dev.Identifier(), Property: bacnet.PropertyReference{Identifier: bacnet.PropertyObjectList}})))
				h.expectType(apdu.TypeComplexACK)
			})
		})
	}
}

// A subscription whose monitored object does not return normally for one
// notification still notifies the next change.
func TestCOVNotificationGoesOnAfterObjectFailure(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		h := newHarness(t)
		armed := atomic.Bool{}
		o := newAppObject(t, 2, "APP-2")
		o.read = func(bacnet.PropertyReference) {
			if armed.Load() {
				outOfRange(3)
			}
		}
		if err := h.dev.Add(o); err != nil {
			t.Fatal(err)
		}
		sub := mustEncode(t)(service.EncodeSubscribeCOVProperty(service.SubscribeCOVPropertyRequest{
			SubscribeCOVRequest: service.SubscribeCOVRequest{ProcessIdentifier: 1, MonitoredObject: app2, Lifetime: 60},
			Property:            pvRef,
		}))
		h.confirmed(1, apdu.ServiceSubscribeCOVProperty, sub)
		h.expectType(apdu.TypeSimpleACK)
		h.expectType(apdu.TypeUnconfirmedRequest) // the initial notification
		idle()
		set := func(v float32) {
			if err := o.WriteProperty(context.Background(), object.Write{Property: pvRef, Value: bacnet.RealValue(v)}); err != nil {
				t.Fatal(err)
			}
		}
		armed.Store(true)
		set(2)
		h.silent(0)
		if len(h.diagnostics("callback")) != 1 {
			t.Fatalf("diagnostics %v", h.diagnostics("callback"))
		}
		armed.Store(false)
		set(3)
		note := h.expectType(apdu.TypeUnconfirmedRequest)
		n, err := service.DecodeCOVNotification(note.pdu.UnconfirmedRequest.Payload, bacnet.DefaultDecodeLimits())
		if err != nil || len(n.Values) == 0 || n.Values[0].Value.Real != 3 {
			t.Fatalf("notification %+v, %v", n, err)
		}
	})
}

// The diagnostic function and the logger's handler are the application's
// code too: one that does not return normally is stopped, the function's
// failure is logged at Error level, and the frame is handled as usual.
func TestDiagnosticSinksThatDoNotReturnNormally(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		var buf syncBuffer
		armed := atomic.Bool{}
		h := newHarness(t,
			WithDiagnosticFunc(func(Diagnostic) {
				if armed.Load() {
					outOfRange(3)
				}
			}),
			WithLogger(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError}))))
		armed.Store(true)
		h.injectRaw([]byte{0x81, 0x0A, 0x00}) // a malformed frame: one diagnostic
		idle()
		armed.Store(false)
		log := buf.String()
		for _, want := range []string{"level=ERROR", "application code did not return normally", "kind=callback", `callback="diagnostic function"`, "outOfRange"} {
			if !strings.Contains(log, want) {
				t.Fatalf("log lacks %q:\n%s", want, log)
			}
		}
		h.confirmed(1, apdu.ServiceReadProperty, readPV(t))
		h.expectType(apdu.TypeComplexACK)
	})

	// A logger whose handler fails is left out; the diagnostic function
	// still gets the diagnostic.
	sink := newDiagSink(slog.New(failingHandler{}), nil)
	sink.Report(callbackEvent("x", "y", nil))
	var got []string
	sink = newDiagSink(slog.New(failingHandler{}), func(d Diagnostic) { got = append(got, d.Kind) })
	sink.Report(callbackEvent("x", "y", map[string]any{"object": "z"}))
	if len(got) != 1 || got[0] != "callback" {
		t.Fatalf("diagnostic function got %v", got)
	}
}

type failingHandler struct{}

func (failingHandler) Enabled(context.Context, slog.Level) bool  { return true }
func (failingHandler) Handle(context.Context, slog.Record) error { outOfRange(3); return nil }
func (h failingHandler) WithAttrs([]slog.Attr) slog.Handler      { return h }
func (h failingHandler) WithGroup(string) slog.Handler           { return h }

// L11: a hook that takes its time holds its worker and nothing else. With
// every worker in a hook and the queue full, a further confirmed request is
// answered with Abort (out-of-resources) at once, and the waiting ones are
// served when the hooks return.
func TestSlowHookHoldsOnlyItsWorker(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		release := make(chan struct{})
		var inHook atomic.Int32
		h := newHarness(t, WithWorkers(2, 1), WithHooks(Hooks{BeforeWrite: func(ctx context.Context, _ bacnet.ObjectIdentifier, _ object.Write) error {
			inHook.Add(1)
			select {
			case <-release:
			case <-ctx.Done():
			}
			return nil
		}}))
		// One request at a time, so that each is with a worker before the
		// next arrives: the queue holds one.
		h.confirmed(1, apdu.ServiceWriteProperty, writeOf(t, av1, 1))
		idle()
		h.confirmed(2, apdu.ServiceWriteProperty, writeOf(t, av1, 2))
		idle()
		if inHook.Load() != 2 {
			t.Fatalf("%d hooks running, want both workers in one", inHook.Load())
		}
		h.confirmed(3, apdu.ServiceReadProperty, readPV(t)) // waits in the queue
		idle()
		h.confirmed(4, apdu.ServiceReadProperty, readPV(t)) // no room
		got := h.expectType(apdu.TypeAbort)
		if got.pdu.Abort.InvokeID != 4 || got.pdu.Abort.Reason != bacnet.AbortReasonOutOfResources {
			t.Fatalf("abort %+v", got.pdu.Abort)
		}
		h.silent(0)
		close(release)
		answered := map[uint8]apdu.PDUType{}
		for range 3 {
			got := h.next()
			id, _ := invokeIDOf(got.pdu)
			answered[id] = got.pdu.Type
		}
		if answered[1] != apdu.TypeSimpleACK || answered[2] != apdu.TypeSimpleACK || answered[3] != apdu.TypeComplexACK {
			t.Fatalf("answers %v", answered)
		}
	})
}
