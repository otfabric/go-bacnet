// SPDX-License-Identifier: MIT

package client

import (
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
)

func TestCompleteWinnerOnly(t *testing.T) {
	m := newTxManager(16, time.Now)
	tx := &pendingTx{
		invokeID: 7,
		result:   make(chan txResult, 1),
		timer:    nopTimer{},
	}
	m.register(tx)

	if !m.complete(7, txResult{err: nil}, time.Second) {
		t.Fatal("first complete should win")
	}
	if m.complete(7, txResult{err: bacnet.ErrTimeout}, time.Second) {
		t.Fatal("second complete must lose")
	}
	res := <-tx.result
	if res.err != nil {
		t.Fatalf("winner result lost: %v", res.err)
	}
}

func TestEnterSegmentedStopsRetransmit(t *testing.T) {
	m := newTxManager(16, time.Now)
	tm := &countingTimer{}
	tx := &pendingTx{
		invokeID:    3,
		retriesLeft: 2,
		result:      make(chan txResult, 1),
		timer:       tm,
		phase:       txAwaitingInitial,
	}
	m.register(tx)
	if !m.enterSegmented(3) {
		t.Fatal("expected enter")
	}
	phase, ok := m.phase(3)
	if !ok || phase != txReceivingSegments || tx.retriesLeft != 0 || tm.stopped == 0 {
		t.Fatalf("phase=%v ok=%v retries=%d stopped=%d", phase, ok, tx.retriesLeft, tm.stopped)
	}
	if act := m.onTimeout(3, true); act != timeoutIgnoreSegmented {
		t.Fatalf("onTimeout after enterSegmented: got %v, want ignore", act)
	}
}

func TestOnTimeoutConsumesRetryAtomically(t *testing.T) {
	m := newTxManager(16, time.Now)
	tx := &pendingTx{
		invokeID:    9,
		retriesLeft: 1,
		result:      make(chan txResult, 1),
		timer:       nopTimer{},
		phase:       txAwaitingInitial,
	}
	m.register(tx)
	if act := m.onTimeout(9, true); act != timeoutRetransmit {
		t.Fatalf("first timeout: got %v", act)
	}
	if tx.retriesLeft != 0 {
		t.Fatalf("retriesLeft=%d, want 0", tx.retriesLeft)
	}
	if act := m.onTimeout(9, true); act != timeoutFail {
		t.Fatalf("second timeout: got %v", act)
	}
	if !m.enterSegmented(9) {
		t.Fatal("enterSegmented")
	}
	if act := m.onTimeout(9, true); act != timeoutIgnoreSegmented {
		t.Fatalf("segmented timeout: got %v", act)
	}
}

type nopTimer struct{}

func (nopTimer) Stop() bool                 { return true }
func (nopTimer) Reset(d time.Duration) bool { return false }
func (nopTimer) C() <-chan time.Time        { return nil }

type countingTimer struct{ stopped int }

func (t *countingTimer) Stop() bool {
	t.stopped++
	return true
}
func (t *countingTimer) Reset(d time.Duration) bool { return false }
func (t *countingTimer) C() <-chan time.Time        { return nil }
