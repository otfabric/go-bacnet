// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"fmt"
	"math"
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

	reg    atomic.Bool
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
		stopCh: make(chan struct{}),
	}, nil
}

func (f *fdState) registered() bool { return f.reg.Load() }

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

func (c *Client) fdLoop() {
	defer c.wg.Done()
	f := c.fd
	renewEvery := f.ttl / 2
	if renewEvery < time.Second {
		renewEvery = time.Second
	}
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
			f.reg.Store(false)
			f.diag.Report(diag.Event{
				Kind:    diag.KindForeignDevice,
				Message: "registration failed; cross-subnet broadcast degraded",
				Fields:  map[string]any{"error": err.Error()},
			})
			wait = f.ttl // single backoff, then retry (no stacked half-TTL wait)
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
	if err := c.tr.Send(ctx, OutboundPacket{Data: frame, Destination: f.bbmd}); err != nil {
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
		f.reg.Store(false)
		return bacnet.ErrTimeout
	case code := <-a.ch:
		if code != 0 {
			f.reg.Store(false)
			err := &BVLCResultError{Code: code}
			f.diag.Report(diag.Event{
				Kind:    diag.KindForeignDevice,
				Message: "BVLC-Result rejected registration",
				Fields:  map[string]any{"code": code},
			})
			return err
		}
		f.reg.Store(true)
		return nil
	}
}
