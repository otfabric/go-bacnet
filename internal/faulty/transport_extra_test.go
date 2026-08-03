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

func TestFaultyFailAndDropSends(t *testing.T) {
	clk := clock.NewManual(time.Now())
	local := bip.NewEndpoint(netip.MustParseAddrPort("127.0.0.1:47808"))
	peer := bip.NewEndpoint(netip.MustParseAddrPort("127.0.0.1:47809"))
	v := virtual.New(local, clk, 4)
	tr := faulty.New(client.AdaptVirtual(v))
	tr.SetPolicy(faulty.Policy{FailSendsAfter: 1})
	ctx := context.Background()
	pkt := client.OutboundPacket{Data: []byte{0x81, 0x0a, 0x00, 0x06, 0x01, 0x00}, Destination: peer}
	if err := tr.Send(ctx, pkt); err != nil {
		t.Fatal(err)
	}
	if err := tr.Send(ctx, pkt); !errors.Is(err, faulty.ErrInjected) {
		t.Fatalf("got %v", err)
	}
	tr.SetPolicy(faulty.Policy{DropSendsAfter: 1})
	if err := tr.Send(ctx, pkt); err != nil {
		t.Fatal(err)
	}
	if err := tr.Send(ctx, pkt); err != nil {
		t.Fatalf("drop should succeed silently: %v", err)
	}
}

func TestFaultyLocalCloseRecvDrop(t *testing.T) {
	clk := clock.NewManual(time.Now())
	local := bip.NewEndpoint(netip.MustParseAddrPort("127.0.0.1:47808"))
	v := virtual.New(local, clk, 4)
	tr := faulty.New(client.AdaptVirtual(v))
	if !tr.Local().Equal(local) {
		t.Fatal(tr.Local())
	}
	tr.SetPolicy(faulty.Policy{DropRecvsAfter: 1})
	v.Inject(virtual.InboundPacket{
		Data: []byte{0x81, 0x0a, 0x00, 0x06, 0x01, 0x00}, ImmediatePeer: local, ReceivedAt: clk.Now(),
	})
	v.Inject(virtual.InboundPacket{
		Data: []byte{0x81, 0x0a, 0x00, 0x06, 0x02, 0x00}, ImmediatePeer: local, ReceivedAt: clk.Now(),
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	pkt, err := tr.Recv(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if pkt.Data[4] != 0x01 {
		t.Fatalf("%x", pkt.Data)
	}
	cancel()
	if err := tr.Close(); err != nil {
		t.Fatal(err)
	}
}
