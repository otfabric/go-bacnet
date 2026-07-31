// SPDX-License-Identifier: MIT

// Package virtual provides an in-memory BACnet/IP transport for deterministic tests.
package virtual

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/internal/clock"
)

// InboundPacket is a received datagram with path metadata.
type InboundPacket struct {
	Data          []byte
	ImmediatePeer bip.Endpoint
	ReceivedAt    time.Time
}

// OutboundPacket is a datagram to send.
type OutboundPacket struct {
	Data        []byte
	Destination bip.Endpoint
	Broadcast   bool
}

// Transport is a bidirectional in-memory link.
type Transport struct {
	mu     sync.Mutex
	closed bool
	local  bip.Endpoint
	inbox  chan InboundPacket
	outbox []OutboundPacket
	clock  clock.Clock
	peers  map[string]*Transport // optional mesh by endpoint string
}

// New returns a Transport for local with buffered inbox.
func New(local bip.Endpoint, clk clock.Clock, inboxSize int) *Transport {
	if clk == nil {
		clk = clock.Real{}
	}
	if inboxSize <= 0 {
		inboxSize = 64
	}
	return &Transport{
		local: local,
		inbox: make(chan InboundPacket, inboxSize),
		clock: clk,
		peers: make(map[string]*Transport),
	}
}

// Local returns the local endpoint.
func (t *Transport) Local() bip.Endpoint { return t.local }

// Link connects two transports for unicast delivery.
func Link(a, b *Transport) {
	a.mu.Lock()
	b.mu.Lock()
	a.peers[b.local.String()] = b
	b.peers[a.local.String()] = a
	b.mu.Unlock()
	a.mu.Unlock()
}

// Send delivers a packet to a linked peer or records it in the outbox.
// Peer delivery happens after unlocking to avoid A→B / B→A lock inversion.
func (t *Transport) Send(ctx context.Context, pkt OutboundPacket) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return errors.New("virtual: closed")
	}
	data := append([]byte(nil), pkt.Data...)
	now := t.clock.Now()
	var deliveries []struct {
		peer *Transport
		pkt  InboundPacket
	}
	if peer, ok := t.peers[pkt.Destination.String()]; ok && !pkt.Broadcast {
		deliveries = append(deliveries, struct {
			peer *Transport
			pkt  InboundPacket
		}{peer, InboundPacket{
			Data:          data,
			ImmediatePeer: t.local,
			ReceivedAt:    now,
		}})
	} else if pkt.Broadcast {
		for _, peer := range t.peers {
			deliveries = append(deliveries, struct {
				peer *Transport
				pkt  InboundPacket
			}{peer, InboundPacket{
				Data:          append([]byte(nil), data...),
				ImmediatePeer: t.local,
				ReceivedAt:    now,
			}})
		}
	}
	t.outbox = append(t.outbox, OutboundPacket{
		Data:        data,
		Destination: pkt.Destination,
		Broadcast:   pkt.Broadcast,
	})
	t.mu.Unlock()

	for _, d := range deliveries {
		d.peer.deliver(d.pkt)
	}
	return nil
}

func (t *Transport) deliver(pkt InboundPacket) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return
	}
	select {
	case t.inbox <- pkt:
	default:
		// Drop when full — mirrors bounded real sockets under load.
	}
}

// Inject injects a packet as if received from peer.
func (t *Transport) Inject(pkt InboundPacket) {
	if pkt.ReceivedAt.IsZero() {
		pkt.ReceivedAt = t.clock.Now()
	}
	pkt.Data = append([]byte(nil), pkt.Data...)
	t.deliver(pkt)
}

// Recv returns the next inbound packet or ctx/closed error.
func (t *Transport) Recv(ctx context.Context) (InboundPacket, error) {
	select {
	case <-ctx.Done():
		return InboundPacket{}, ctx.Err()
	case pkt, ok := <-t.inbox:
		if !ok {
			return InboundPacket{}, errors.New("virtual: closed")
		}
		return pkt, nil
	}
}

// Outbox returns a copy of recorded outbound packets.
func (t *Transport) Outbox() []OutboundPacket {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]OutboundPacket, len(t.outbox))
	copy(out, t.outbox)
	return out
}

// Close closes the transport.
func (t *Transport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	close(t.inbox)
	return nil
}
