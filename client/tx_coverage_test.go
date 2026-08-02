// SPDX-License-Identifier: MIT

package client

import (
	"testing"
	"time"
)

type stubTimer struct{ resets int }

func (s *stubTimer) Stop() bool                 { return true }
func (s *stubTimer) Reset(d time.Duration) bool { s.resets++; return true }

func TestEnterAwaitingResponseMissing(t *testing.T) {
	m := newTxManager(8, time.Now)
	if m.enterAwaitingResponse(42, time.Second) {
		t.Fatal("expected missing invoke")
	}
}

func TestEnterAwaitingResponseSuccess(t *testing.T) {
	m := newTxManager(8, time.Now)
	timer := &stubTimer{}
	tx := &pendingTx{invokeID: 7, result: make(chan txResult, 1), timer: timer}
	m.register(tx)
	if !m.enterAwaitingResponse(7, time.Second) {
		t.Fatal("expected success")
	}
	if timer.resets != 1 {
		t.Fatalf("resets=%d", timer.resets)
	}
	if !m.enterAwaitingResponse(7, 0) {
		t.Fatal("expected success with zero duration")
	}
	if !m.enterSegmented(7) {
		t.Fatal("segmented")
	}
	if !m.enterSendingSegments(7) {
		t.Fatal("sending")
	}
}

func TestOnTimeoutGone(t *testing.T) {
	m := newTxManager(8, time.Now)
	if m.onTimeout(9, true) != timeoutGone {
		t.Fatal("expected gone")
	}
}

func TestOnTimeoutRetransmitAndFail(t *testing.T) {
	m := newTxManager(8, time.Now)
	tx := &pendingTx{invokeID: 3, result: make(chan txResult, 1), retriesLeft: 1, phase: txAwaitingInitial}
	m.register(tx)
	if m.onTimeout(3, true) != timeoutRetransmit {
		t.Fatal("retransmit")
	}
	if m.onTimeout(3, true) != timeoutFail {
		t.Fatal("fail")
	}
	tx2 := &pendingTx{invokeID: 4, result: make(chan txResult, 1), phase: txReceivingSegments}
	m.register(tx2)
	if m.onTimeout(4, true) != timeoutIgnoreSegmented {
		t.Fatal("ignore segmented")
	}
}

func TestTryAllocQuarantineAndExhausted(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m := newTxManager(1, func() time.Time { return now })
	m.mu.Lock()
	id, ok := m.tryAllocLocked()
	if !ok {
		m.mu.Unlock()
		t.Fatal("alloc")
	}
	m.inUse[id] = &pendingTx{invokeID: id, result: make(chan txResult, 1)}
	m.quarantine[id+1] = now.Add(time.Hour)
	if _, ok := m.tryAllocLocked(); ok {
		m.mu.Unlock()
		t.Fatal("expected max full")
	}
	// Fill all invoke IDs used or quarantined to force exhaustion path.
	m.max = 255
	for i := 0; i < 256; i++ {
		m.inUse[uint8(i)] = &pendingTx{invokeID: uint8(i), result: make(chan txResult, 1)}
	}
	if _, ok := m.tryAllocLocked(); ok {
		m.mu.Unlock()
		t.Fatal("expected exhausted")
	}
	m.mu.Unlock()
	if !m.enterSendingSegments(200) {
		t.Fatal("sending segments")
	}
	if !m.enterSendingSegments(201) {
		t.Fatal("expected enterSendingSegments for in-use invoke id")
	}
	delete(m.inUse, 201)
	if m.enterSendingSegments(201) {
		t.Fatal("expected missing")
	}
}
