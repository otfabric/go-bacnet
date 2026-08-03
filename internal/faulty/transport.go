// SPDX-License-Identifier: MIT

// Package faulty provides deterministic transport fault injection for tests.
package faulty

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/client"
)

// ErrInjected is returned when a send is failed by policy.
var ErrInjected = errors.New("faulty: injected send failure")

// Policy controls send/receive faults.
type Policy struct {
	FailSendsAfter int  // fail Send after this many successful sends (0 = never)
	DropSendsAfter int  // drop (succeed silently) after N successful sends (0 = never)
	DropRecvsAfter int  // drop inbound after N successful Recv deliveries (0 = never)
	FailNextSend   bool // fail the next Send once
	DropNextSend   bool // drop the next Send once
}

// Transport wraps a client.Transport with injectable faults.
type Transport struct {
	Inner  client.Transport
	mu     sync.Mutex
	policy Policy
	sends  atomic.Int64
	recvs  atomic.Int64
}

// New wraps inner.
func New(inner client.Transport) *Transport {
	return &Transport{Inner: inner}
}

// SetPolicy replaces the active fault policy.
func (t *Transport) SetPolicy(p Policy) {
	t.mu.Lock()
	t.policy = p
	t.mu.Unlock()
}

func (t *Transport) Local() bip.Endpoint { return t.Inner.Local() }

func (t *Transport) Close() error { return t.Inner.Close() }

func (t *Transport) Send(ctx context.Context, pkt client.OutboundPacket) error {
	t.mu.Lock()
	p := t.policy
	t.mu.Unlock()
	n := int(t.sends.Add(1))
	if p.FailNextSend {
		t.mu.Lock()
		t.policy.FailNextSend = false
		t.mu.Unlock()
		return ErrInjected
	}
	if p.DropNextSend {
		t.mu.Lock()
		t.policy.DropNextSend = false
		t.mu.Unlock()
		return nil
	}
	if p.FailSendsAfter > 0 && n > p.FailSendsAfter {
		return ErrInjected
	}
	if p.DropSendsAfter > 0 && n > p.DropSendsAfter {
		return nil
	}
	return t.Inner.Send(ctx, pkt)
}

func (t *Transport) Recv(ctx context.Context) (client.InboundPacket, error) {
	for {
		pkt, err := t.Inner.Recv(ctx)
		if err != nil {
			return pkt, err
		}
		t.mu.Lock()
		p := t.policy
		t.mu.Unlock()
		n := int(t.recvs.Add(1))
		if p.DropRecvsAfter > 0 && n > p.DropRecvsAfter {
			continue
		}
		return pkt, nil
	}
}
