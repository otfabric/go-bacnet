// SPDX-License-Identifier: MIT

package virtual_test

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/internal/clock"
	"github.com/otfabric/go-bacnet/internal/virtual"
)

func endpoints() (bip.Endpoint, bip.Endpoint) {
	a := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.1:47808"))
	b := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.2:47808"))
	return a, b
}

func TestLinkUnicastAndOutbox(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	aEP, bEP := endpoints()
	a := virtual.New(aEP, clk, 4)
	b := virtual.New(bEP, clk, 4)
	virtual.Link(a, b)

	ctx := context.Background()
	payload := []byte{0x81, 0x0A, 0x00, 0x08, 0x01, 0x00, 0x10, 0x08}
	if err := a.Send(ctx, virtual.OutboundPacket{Data: payload, Destination: bEP}); err != nil {
		t.Fatal(err)
	}
	out := a.Outbox()
	if len(out) != 1 || string(out[0].Data) != string(payload) || out[0].Broadcast {
		t.Fatalf("outbox %#v", out)
	}
	pkt, err := b.Recv(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if string(pkt.Data) != string(payload) || !pkt.ImmediatePeer.Equal(aEP) {
		t.Fatalf("recv %#v", pkt)
	}
	if !a.Local().Equal(aEP) {
		t.Fatal("Local mismatch")
	}
}

func TestBroadcastAndInject(t *testing.T) {
	clk := clock.NewManual(time.Now())
	aEP, bEP := endpoints()
	a := virtual.New(aEP, clk, 2)
	b := virtual.New(bEP, nil, 0) // defaults: Real clock, inbox 64
	virtual.Link(a, b)

	ctx := context.Background()
	if err := a.Send(ctx, virtual.OutboundPacket{
		Data:        []byte{1, 2, 3},
		Destination: bip.Endpoint{},
		Broadcast:   true,
	}); err != nil {
		t.Fatal(err)
	}
	pkt, err := b.Recv(ctx)
	if err != nil || string(pkt.Data) != "\x01\x02\x03" {
		t.Fatalf("broadcast recv %#v err=%v", pkt, err)
	}

	b.Inject(virtual.InboundPacket{Data: []byte{9}, ImmediatePeer: aEP})
	pkt, err = b.Recv(ctx)
	if err != nil || pkt.Data[0] != 9 || pkt.ReceivedAt.IsZero() {
		t.Fatalf("inject %#v err=%v", pkt, err)
	}
}

func TestClosedAndCancel(t *testing.T) {
	clk := clock.NewManual(time.Now())
	aEP, bEP := endpoints()
	a := virtual.New(aEP, clk, 1)
	b := virtual.New(bEP, clk, 1)
	virtual.Link(a, b)

	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	err := a.Send(context.Background(), virtual.OutboundPacket{Data: []byte{1}, Destination: bEP})
	if err == nil {
		t.Fatal("expected closed send error")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	alive := virtual.New(aEP, clk, 1)
	if err := alive.Send(ctx, virtual.OutboundPacket{Data: []byte{1}, Destination: bEP}); err == nil {
		t.Fatal("expected ctx cancel on send")
	}
	if _, err := alive.Recv(ctx); err == nil {
		t.Fatal("expected ctx cancel on recv")
	}
}
