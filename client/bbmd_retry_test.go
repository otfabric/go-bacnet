// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"errors"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/internal/clock"
	"github.com/otfabric/go-bacnet/internal/virtual"
)

// timerLog is a manual clock that reports the duration of every timer the
// client creates, so that a test advances time only once the client waits.
type timerLog struct {
	*clock.Manual
	created chan time.Duration
}

func (l timerLog) NewTimer(d time.Duration) clock.Timer {
	t := l.Manual.NewTimer(d)
	select {
	case l.created <- d:
	default:
		panic("timerLog: more than 1024 timers not read by the test")
	}
	return t
}

// diagLog collects the client's diagnostics.
type diagLog struct {
	mu  sync.Mutex
	all []Diagnostic
}

func (l *diagLog) add(d Diagnostic) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.all = append(l.all, d)
}

func (l *diagLog) with(msg string) []Diagnostic {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []Diagnostic
	for _, d := range l.all {
		if d.Message == msg {
			out = append(out, d)
		}
	}
	return out
}

const (
	fdTestTTL         = 60 * time.Second
	fdTestAPDUTimeout = 5 * time.Second
	fdFailedMsg       = "foreign-device registration failed; broadcasts unavailable"
)

type fdRig struct {
	c    *Client
	tr   *virtual.Transport
	bbmd bip.Endpoint
	clk  timerLog
	diag *diagLog
}

// newFDRig is a foreign-device client (TTL 60 s, APDU timeout 5 s) whose
// retry delays are not jittered unless jitter says otherwise.
func newFDRig(t *testing.T, jitter func(time.Duration) time.Duration) *fdRig {
	t.Helper()
	if jitter == nil {
		jitter = func(d time.Duration) time.Duration { return d }
	}
	clk := timerLog{Manual: clock.NewManual(time.Unix(0, 0).UTC()), created: make(chan time.Duration, 1024)}
	local := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.5:47808"))
	bbmd := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.1:47808"))
	tr := virtual.New(local, clk.Manual, 16)
	dl := &diagLog{}
	c, err := New(
		WithTransport(AdaptVirtual(tr)),
		withClock(clk),
		withFDJitter(jitter),
		WithDiagnosticFunc(dl.add),
		WithForeignDevice(ForeignDeviceConfig{BBMD: bbmd, TTL: fdTestTTL}),
		WithTransactionOptions(fdTestAPDUTimeout, 0, 0),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return &fdRig{c: c, tr: tr, bbmd: bbmd, clk: clk, diag: dl}
}

// nextTimer returns the duration of the next timer the client creates.
func (r *fdRig) nextTimer(t *testing.T) time.Duration {
	t.Helper()
	select {
	case d := <-r.clk.created:
		return d
	case <-time.After(2 * time.Second):
		t.Fatal("the client created no timer")
		return 0
	}
}

// created waits for the next timer and checks its duration, without moving
// the clock.
func (r *fdRig) created(t *testing.T, want time.Duration, what string) {
	t.Helper()
	if d := r.nextTimer(t); d != want {
		t.Fatalf("%s: timer of %v, want %v", what, d, want)
	}
}

// expectTimer waits for the next timer, checks its duration and lets it fire.
func (r *fdRig) expectTimer(t *testing.T, want time.Duration, what string) {
	t.Helper()
	if d := r.nextTimer(t); d != want {
		t.Fatalf("%s: timer of %v, want %v", what, d, want)
	}
	r.clk.Advance(want)
}

func (r *fdRig) registers() int {
	return count(functions(r.tr), bvlc.FunctionRegisterForeignDevice)
}

func (r *fdRig) result(t *testing.T, code uint16) {
	t.Helper()
	frame, err := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionResult, ResultCode: code})
	if err != nil {
		t.Fatal(err)
	}
	r.tr.Inject(virtual.InboundPacket{Data: frame, ImmediatePeer: r.bbmd, ReceivedAt: r.clk.Now()})
}

// After lost Results the client retries after 2, 4, 8, 16, then every 30 s,
// with exactly one attempt outstanding, and reports each failure.
func TestFDRetryBackoffAfterTimeouts(t *testing.T) {
	r := newFDRig(t, nil)
	for i, wait := range []time.Duration{2, 4, 8, 16, 30, 30} {
		attempt := i + 1
		r.expectTimer(t, fdTestAPDUTimeout, "Result timeout")
		if d := r.nextTimer(t); d != wait*time.Second {
			t.Fatalf("after failure %d: retry in %v, want %v", attempt, d, wait*time.Second)
		}
		if n := r.registers(); n != attempt {
			t.Fatalf("after failure %d: %d Register-Foreign-Device sent", attempt, n)
		}
		r.clk.Advance(wait * time.Second)
		waitRegisterFD(t, r.tr, attempt+1)
	}
	if r.c.ForeignDeviceRegistered() {
		t.Fatal("registered without a Result")
	}
	failed := r.diag.with(fdFailedMsg)
	if len(failed) != 6 {
		t.Fatalf("%d failure diagnostics, want 6", len(failed))
	}
	last := failed[5].Fields
	if last["failure"] != "timeout" || last["attempts"] != 6 || last["retry_in"] != "30s" {
		t.Fatalf("last failure diagnostic %v", last)
	}
}

// A NAK is retried after 60 s each time, not on the timeout schedule, and
// never given up.
func TestFDRetryAfterNAK(t *testing.T) {
	r := newFDRig(t, nil)
	for attempt := 1; attempt <= 3; attempt++ {
		waitRegisterFD(t, r.tr, attempt)
		if d := r.nextTimer(t); d != fdTestAPDUTimeout {
			t.Fatalf("Result timer %v", d)
		}
		r.result(t, 0x0030) // Register-Foreign-Device NAK
		r.expectTimer(t, fdRetryNAK, "retry after NAK")
	}
	waitRegisterFD(t, r.tr, 4)
	failed := r.diag.with(fdFailedMsg)
	if len(failed) != 3 || failed[2].Fields["failure"] != "nak" || failed[2].Fields["retry_in"] != "1m0s" {
		t.Fatalf("NAK diagnostics %v", failed)
	}
}

// A registration that succeeds after failures makes the client registered,
// reports the recovery, renews at TTL/2 and starts the backoff afresh at the
// next failure.
func TestFDRetryRecoveryResetsBackoff(t *testing.T) {
	r := newFDRig(t, nil)
	r.expectTimer(t, fdTestAPDUTimeout, "Result timeout 1")
	r.expectTimer(t, 2*time.Second, "retry 1")
	r.expectTimer(t, fdTestAPDUTimeout, "Result timeout 2")
	r.expectTimer(t, 4*time.Second, "retry 2")
	waitRegisterFD(t, r.tr, 3)
	if d := r.nextTimer(t); d != fdTestAPDUTimeout {
		t.Fatalf("Result timer %v", d)
	}
	r.result(t, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := r.c.WaitForeignDeviceRegistered(ctx); err != nil {
		t.Fatal(err)
	}
	r.created(t, fdTestTTL, "expiry") // attempt 3, sent and accepted at 16 s
	r.expectTimer(t, 30*time.Second, "renewal")
	if got := r.diag.with("foreign-device registration recovered"); len(got) != 1 || got[0].Fields["attempts"] != 3 {
		t.Fatalf("recovery diagnostics %v", got)
	}
	// The renewal is lost: the backoff starts again at 2 s.
	r.expectTimer(t, fdTestAPDUTimeout, "renewal Result timeout")
	r.expectTimer(t, 2*time.Second, "retry after the lost renewal")
	waitRegisterFD(t, r.tr, 5)
}

// Close ends the wait for a retry at once; nothing is sent afterwards.
func TestFDRetryStopsOnClose(t *testing.T) {
	r := newFDRig(t, nil)
	r.expectTimer(t, fdTestAPDUTimeout, "Result timeout")
	if d := r.nextTimer(t); d != 2*time.Second {
		t.Fatalf("retry in %v", d)
	}
	done := make(chan error, 1)
	go func() { done <- r.c.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close blocked on the retry wait")
	}
	r.clk.Advance(time.Hour)
	if n := r.registers(); n != 1 {
		t.Fatalf("%d Register-Foreign-Device after Close, want 1", n)
	}
	if err := r.c.WaitForeignDeviceRegistered(context.Background()); !errors.Is(err, bacnet.ErrClosed) {
		t.Fatalf("WaitForeignDeviceRegistered after Close: %v", err)
	}
}

// A Result that arrives after its attempt timed out, while the client waits
// to retry, matches no attempt: it is dropped and does not register the
// client. (A Result has no attempt identifier, so one arriving while the next
// attempt is pending would be taken for that attempt's; the retry delay keeps
// that window apart from the timeout.)
func TestFDRetryDelayedResultIgnored(t *testing.T) {
	r := newFDRig(t, nil)
	r.expectTimer(t, fdTestAPDUTimeout, "Result timeout")
	if d := r.nextTimer(t); d != 2*time.Second {
		t.Fatalf("retry in %v", d)
	}
	r.result(t, 0) // the late Result of attempt 1
	deadline := time.Now().Add(2 * time.Second)
	for len(r.diag.with("BVLC-Result with no pending registration")) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the late Result was not reported")
		}
		time.Sleep(time.Millisecond)
	}
	if r.c.ForeignDeviceRegistered() {
		t.Fatal("a late Result registered the client")
	}
	r.clk.Advance(2 * time.Second)
	waitRegisterFD(t, r.tr, 2)
	if r.c.ForeignDeviceRegistered() {
		t.Fatal("registered before attempt 2 got its Result")
	}
}

// The retry delay goes through the jitter.
func TestFDRetryUsesJitter(t *testing.T) {
	r := newFDRig(t, func(d time.Duration) time.Duration { return d / 2 })
	r.expectTimer(t, fdTestAPDUTimeout, "Result timeout 1")
	r.expectTimer(t, time.Second, "jittered retry 1")
	r.expectTimer(t, fdTestAPDUTimeout, "Result timeout 2")
	r.expectTimer(t, 2*time.Second, "jittered retry 2")
}

func TestEqualJitterBounds(t *testing.T) {
	for _, d := range []time.Duration{fdRetryMin, fdRetryMax, fdRetryNAK, time.Nanosecond} {
		for range 1000 {
			if j := equalJitter(d); j < d/2 || j > d {
				t.Fatalf("equalJitter(%v) = %v, outside [%v, %v]", d, j, d/2, d)
			}
		}
	}
}

func TestFDRetryDelay(t *testing.T) {
	nak := &BVLCResultError{Code: 0x0030}
	for _, c := range []struct {
		err      error
		failures int
		want     time.Duration
	}{
		{bacnet.ErrTimeout, 1, 2 * time.Second},
		{bacnet.ErrTimeout, 4, 16 * time.Second},
		{bacnet.ErrTimeout, 5, 30 * time.Second},
		{bacnet.ErrTimeout, 1000, 30 * time.Second},
		{errors.New("network unreachable"), 2, 4 * time.Second},
		{nak, 1, time.Minute},
		{nak, 9, time.Minute},
	} {
		if got := fdRetryDelay(c.err, c.failures); got != c.want {
			t.Errorf("fdRetryDelay(%v, %d) = %v, want %v", c.err, c.failures, got, c.want)
		}
	}
	for err, want := range map[error]string{nak: "nak", bacnet.ErrTimeout: "timeout", errors.New("x"): "send"} {
		if got := fdFailureKind(err); got != want {
			t.Errorf("fdFailureKind(%v) = %q, want %q", err, got, want)
		}
	}
}
