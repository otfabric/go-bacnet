// SPDX-License-Identifier: MIT

package faulty_test

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/internal/clock"
	"github.com/otfabric/go-bacnet/internal/faulty"
	"github.com/otfabric/go-bacnet/internal/virtual"
)

func TestFailNextSend(t *testing.T) {
	clk := clock.NewManual(time.Now())
	local := bip.NewEndpoint(netip.MustParseAddrPort("127.0.0.1:47808"))
	inner := client.AdaptVirtual(virtual.New(local, clk, 4))
	tr := faulty.New(inner)
	tr.SetPolicy(faulty.Policy{FailNextSend: true})
	err := tr.Send(context.Background(), client.OutboundPacket{
		Data: []byte{0x81, 0x0a, 0x00, 0x06, 0x01, 0x00}, Destination: local,
	})
	if !errors.Is(err, faulty.ErrInjected) {
		t.Fatalf("got %v", err)
	}
	if err := tr.Send(context.Background(), client.OutboundPacket{
		Data: []byte{0x81, 0x0a, 0x00, 0x06, 0x01, 0x00}, Destination: local,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestDropNextSend(t *testing.T) {
	clk := clock.NewManual(time.Now())
	local := bip.NewEndpoint(netip.MustParseAddrPort("127.0.0.1:47808"))
	v := virtual.New(local, clk, 4)
	tr := faulty.New(client.AdaptVirtual(v))
	tr.SetPolicy(faulty.Policy{DropNextSend: true})
	if err := tr.Send(context.Background(), client.OutboundPacket{
		Data: []byte{0x81, 0x0a, 0x00, 0x06, 0x01, 0x00}, Destination: local,
	}); err != nil {
		t.Fatal(err)
	}
	if len(v.Outbox()) != 0 {
		t.Fatalf("expected dropped send, outbox=%d", len(v.Outbox()))
	}
}
