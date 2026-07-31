// SPDX-License-Identifier: MIT

package client

import (
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
)

func TestTxAbortAll(t *testing.T) {
	m := newTxManager(4, time.Now)
	tx := &pendingTx{invokeID: 1, result: make(chan txResult, 1), timer: nopTimer{}}
	m.register(tx)
	m.abortAll(bacnet.ErrClosed)
	select {
	case res := <-tx.result:
		if !errors.Is(res.err, bacnet.ErrClosed) {
			t.Fatalf("got %v", res.err)
		}
	default:
		t.Fatal("expected abort result")
	}
}

func TestTxCompleteQuarantine(t *testing.T) {
	m := newTxManager(4, time.Now)
	tx := &pendingTx{invokeID: 2, result: make(chan txResult, 1), timer: nopTimer{}}
	m.register(tx)
	if !m.complete(2, txResult{}, time.Second) {
		t.Fatal("complete failed")
	}
	m.mu.Lock()
	_, quarantined := m.quarantine[2]
	m.mu.Unlock()
	if !quarantined {
		t.Fatal("expected invoke ID quarantine")
	}
}

func TestTxEnterSegmentedMissing(t *testing.T) {
	m := newTxManager(4, time.Now)
	if m.enterSegmented(8) {
		t.Fatal("expected false for missing tx")
	}
}

func TestTxOnTimeoutMissing(t *testing.T) {
	m := newTxManager(4, time.Now)
	if act := m.onTimeout(8, true); act != timeoutGone {
		t.Fatalf("got %v", act)
	}
}

func TestTxMatchSourceUsesTargetMatcher(t *testing.T) {
	m := newTxManager(4, time.Now)
	addr := bacnet.LocalStation(bacnet.MustMAC([]byte{10, 0, 0, 2, 0xBA, 0xC0}))
	peer := mustEndpoint("10.0.0.2:47808")
	tx := &pendingTx{address: addr, immediate: peer}
	src := packetSource{bacnetAddress: addr, immediate: peer}
	if !m.matchSource(tx, src) {
		t.Fatal("expected match")
	}
}

func TestNewWithLoopbackInterface(t *testing.T) {
	c, err := New(WithInterface("lo0"), WithLocalAddr("127.0.0.1:0"))
	if err != nil {
		t.Skip(err)
	}
	defer func() { _ = c.Close() }()
}

func mustEndpoint(s string) bip.Endpoint {
	ap, err := netip.ParseAddrPort(s)
	if err != nil {
		panic(err)
	}
	return bip.NewEndpoint(ap)
}
