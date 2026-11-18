// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/internal/clock"
	"github.com/otfabric/go-bacnet/internal/diag"
)

// BVLCResultError is a non-zero BVLC-Result for foreign-device registration.
type BVLCResultError struct {
	Code uint16
}

func (e *BVLCResultError) Error() string {
	return fmt.Sprintf("bacnet: BVLC-Result code=%d", e.Code)
}

func (e *BVLCResultError) Unwrap() error { return bacnet.ErrProtocolViolation }

// fdState manages foreign-device registration with one BBMD.
type fdState struct {
	bbmd   bip.Endpoint
	ttl    time.Duration
	ttlSec uint16
	clock  clock.Clock
	diag   diag.Sink
	jitter func(time.Duration) time.Duration // retry delay -> jittered delay

	reg    atomic.Bool
	ready  chan struct{} // closed while registered; replaced when the registration lapses (mu)
	stopCh chan struct{}

	mu      sync.Mutex
	gen     uint64
	pending *fdAttempt
}

type fdAttempt struct {
	gen  uint64
	bbmd bip.Endpoint
	ch   chan uint16
}

func newFDState(cfg ForeignDeviceConfig, clk clock.Clock, d diag.Sink) (*fdState, error) {
	if !cfg.BBMD.IsValid() {
		return nil, fmt.Errorf("%w: foreign-device BBMD endpoint invalid", bacnet.ErrMalformed)
	}
	ttl := cfg.TTL
	if ttl <= 0 {
		ttl = 60 * time.Second
	}
	if ttl%time.Second != 0 {
		return nil, fmt.Errorf("%w: foreign-device TTL must be a whole number of seconds", bacnet.ErrMalformed)
	}
	sec := ttl / time.Second
	if sec < 2 {
		return nil, fmt.Errorf("%w: foreign-device TTL must be at least 2s", bacnet.ErrMalformed)
	}
	if sec > time.Duration(math.MaxUint16) {
		return nil, fmt.Errorf("%w: foreign-device TTL exceeds uint16 seconds", bacnet.ErrMalformed)
	}
	// Normalize scheduling to the wire-encoded whole-second duration.
	ttl = sec * time.Second
	return &fdState{
		bbmd:   cfg.BBMD,
		ttl:    ttl,
		ttlSec: uint16(sec),
		clock:  clk,
		diag:   d,
		jitter: equalJitter,
		ready:  make(chan struct{}),
		stopCh: make(chan struct{}),
	}, nil
}

func (f *fdState) registered() bool { return f.reg.Load() }

// setRegistered records the outcome of a registration and wakes the waiters
// of WaitForeignDeviceRegistered when it was accepted.
func (f *fdState) setRegistered(ok bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.reg.Swap(ok) == ok {
		return
	}
	if ok {
		close(f.ready)
	} else {
		f.ready = make(chan struct{})
	}
}

func (f *fdState) readyCh() <-chan struct{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ready
}

// WaitForeignDeviceRegistered returns once the BBMD has accepted the client's
// foreign-device registration: at once when it is registered, ctx.Err() when
// ctx ends first, bacnet.ErrClosed when the client is closed, and an error
// matching bacnet.ErrInvalidConfig when the client is not a foreign device
// (WithForeignDevice). Until then a broadcast fails with
// bacnet.ErrNotRegistered.
func (c *Client) WaitForeignDeviceRegistered(ctx context.Context) error {
	if c.fd == nil {
		return fmt.Errorf("%w: not a foreign device (WithForeignDevice)", bacnet.ErrInvalidConfig)
	}
	select {
	case <-c.fd.readyCh():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closeCh:
		return bacnet.ErrClosed
	}
}

// ForeignDeviceRegistered reports whether foreign-device registration with the
// configured BBMD currently succeeded. False when FD mode is not enabled.
func (c *Client) ForeignDeviceRegistered() bool {
	if c == nil || c.fd == nil {
		return false
	}
	return c.fd.registered()
}

func (f *fdState) stop() {
	select {
	case <-f.stopCh:
	default:
		close(f.stopCh)
	}
}

func (f *fdState) beginAttempt() *fdAttempt {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gen++
	a := &fdAttempt{
		gen:  f.gen,
		bbmd: f.bbmd,
		ch:   make(chan uint16, 1),
	}
	f.pending = a
	return a
}

func (f *fdState) clearAttempt(a *fdAttempt) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pending == a {
		f.pending = nil
	}
}

// attemptPendingFrom reports whether a registration attempt with from is
// awaiting its BVLC-Result.
func (f *fdState) attemptPendingFrom(from bip.Endpoint) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pending != nil && from.Equal(f.pending.bbmd)
}

func (f *fdState) handleResult(code uint16, from bip.Endpoint) {
	f.mu.Lock()
	a := f.pending
	f.mu.Unlock()
	if a == nil {
		f.diag.Report(diag.Event{
			Kind:    diag.KindForeignDevice,
			Message: "BVLC-Result with no pending registration",
			Fields:  map[string]any{"code": code, "from": from.String()},
		})
		return
	}
	if !from.Equal(a.bbmd) {
		f.diag.Report(diag.Event{
			Kind:    diag.KindForeignDevice,
			Message: "BVLC-Result from unexpected peer",
			Fields:  map[string]any{"code": code, "from": from.String(), "bbmd": a.bbmd.String()},
		})
		return
	}
	select {
	case a.ch <- code:
	default:
		// Drop if attempt already consumed a result.
	}
}

// Retry delays after a failed foreign-device registration. Annex J sets the
// TTL and its 30 s grace period (J.5.2.3) but no retry policy, so these are
// the library's: a timeout or a send failure (the BBMD may not have received
// the request, or its Result was lost) is retried after 2, 4, 8, 16, then
// every 30 s; a NAK (BVLC-Result with a non-zero code: the BBMD refused) after
// 60 s, as its administrator may enable registrations later. Each delay is
// jittered (equalJitter) and a success resets the sequence. WithForeignDevice
// opts into this lifecycle, which runs until Close.
const (
	fdRetryMin = 2 * time.Second
	fdRetryMax = 30 * time.Second
	fdRetryNAK = 60 * time.Second
)

// equalJitter returns a delay drawn uniformly from [d/2, d] ("equal jitter"):
// clients that failed together spread out, and at least d/2 passes between
// two attempts, so that a BVLC-Result for an earlier attempt, which carries
// no attempt identifier, is less likely to arrive while the next is pending.
func equalJitter(d time.Duration) time.Duration {
	half := d / 2
	return half + rand.N(d-half+1)
}

// fdRetryDelay is the nominal delay before the next attempt after failures
// consecutive failed registrations ending in err.
func fdRetryDelay(err error, failures int) time.Duration {
	var nak *BVLCResultError
	if errors.As(err, &nak) {
		return fdRetryNAK
	}
	d := fdRetryMin
	for i := 1; i < failures && d < fdRetryMax; i++ {
		d *= 2
	}
	return min(d, fdRetryMax)
}

// fdFailureKind names the class of a registration failure for diagnostics.
func fdFailureKind(err error) string {
	var nak *BVLCResultError
	switch {
	case errors.As(err, &nak):
		return "nak"
	case errors.Is(err, bacnet.ErrTimeout):
		return "timeout"
	default:
		return "send"
	}
}

// fdLoop registers with the BBMD at once, renews an accepted registration
// every TTL/2, and retries a failed one after fdRetryDelay. Exactly one
// attempt is outstanding at a time; Close and stop end the loop at any wait.
func (c *Client) fdLoop() {
	defer c.wg.Done()
	f := c.fd
	renewEvery := max(f.ttl/2, time.Second)
	failures := 0
	for {
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			select {
			case <-c.closeCh:
				cancel()
			case <-f.stopCh:
				cancel()
			case <-ctx.Done():
			}
		}()
		err := c.registerFD(ctx)
		cancel()
		wait := renewEvery
		if err != nil {
			if errors.Is(err, bacnet.ErrClosed) || errors.Is(err, context.Canceled) {
				return
			}
			f.setRegistered(false)
			failures++
			wait = f.jitter(fdRetryDelay(err, failures))
			f.diag.Report(diag.Event{
				Kind:    diag.KindForeignDevice,
				Message: "foreign-device registration failed; broadcasts unavailable",
				Fields: map[string]any{
					"error":    err.Error(),
					"failure":  fdFailureKind(err),
					"attempts": failures,
					"retry_in": wait.String(),
				},
			})
		} else {
			if failures > 0 {
				f.diag.Report(diag.Event{
					Kind:    diag.KindForeignDevice,
					Message: "foreign-device registration recovered",
					Fields:  map[string]any{"attempts": failures + 1},
				})
			}
			failures = 0
		}
		timer := c.clock.NewTimer(wait)
		select {
		case <-c.closeCh:
			timer.Stop()
			return
		case <-f.stopCh:
			timer.Stop()
			return
		case <-timer.C():
		}
	}
}

func (c *Client) registerFD(ctx context.Context) error {
	f := c.fd
	a := f.beginAttempt()
	defer f.clearAttempt(a)

	frame, err := bvlc.Append(nil, bvlc.Message{
		Function: bvlc.FunctionRegisterForeignDevice,
		TTL:      f.ttlSec,
	})
	if err != nil {
		return err
	}
	if err := c.send(ctx, OutboundPacket{Data: frame, Destination: f.bbmd}); err != nil {
		return err
	}
	timer := c.clock.NewTimer(c.cfg.apduTimeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closeCh:
		return bacnet.ErrClosed
	case <-timer.C():
		// BACnet BVLC-Result has no attempt identifier. A delayed Result for an
		// earlier registration to the same BBMD is indistinguishable from a
		// Result for the current attempt once a later attempt is pending.
		f.setRegistered(false)
		return bacnet.ErrTimeout
	case code := <-a.ch:
		if code != 0 {
			f.setRegistered(false)
			err := &BVLCResultError{Code: code}
			f.diag.Report(diag.Event{
				Kind:    diag.KindForeignDevice,
				Message: "BVLC-Result rejected registration",
				Fields:  map[string]any{"code": code},
			})
			return err
		}
		f.setRegistered(true)
		return nil
	}
}
