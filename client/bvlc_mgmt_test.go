// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/internal/clock"
	"github.com/otfabric/go-bacnet/internal/virtual"
)

func TestReadBDTVirtual(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	local := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.2:47808"))
	bbmd := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.1:47808"))
	tr := virtual.New(local, clk, 16)
	c, err := New(WithTransport(AdaptVirtual(tr)), withClock(clk), WithTransactionOptions(200*time.Millisecond, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	go func() {
		for {
			out := tr.Outbox()
			if len(out) == 0 {
				time.Sleep(5 * time.Millisecond)
				continue
			}
			msg, err := bvlc.Parse(out[len(out)-1].Data, bacnet.DefaultDecodeLimits())
			if err != nil || msg.Function != bvlc.FunctionReadBroadcastDistributionTable {
				time.Sleep(5 * time.Millisecond)
				continue
			}
			entry := bvlc.BDTEntry{
				Endpoint: bbmd,
				Mask:     bvlc.IPv4Mask{255, 255, 255, 255},
			}
			payload, _ := bvlc.EncodeBDTEntries(nil, []bvlc.BDTEntry{entry})
			frame, _ := bvlc.Append(nil, bvlc.Message{
				Function: bvlc.FunctionReadBroadcastDistributionTableAck,
				Payload:  payload,
			})
			tr.Inject(virtual.InboundPacket{Data: frame, ImmediatePeer: bbmd, ReceivedAt: clk.Now()})
			return
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	entries, err := c.ReadBroadcastDistributionTable(ctx, bbmd)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !entries[0].Endpoint.Equal(bbmd) {
		t.Fatalf("entries=%#v", entries)
	}
}

func TestWriteBDTRequiresNetworkManagement(t *testing.T) {
	env := newVirtualPair(t)
	err := env.Client.WriteBroadcastDistributionTable(context.Background(), env.Peer, nil)
	if !errors.Is(err, ErrNetworkManagementDisabled) {
		t.Fatalf("got %v", err)
	}
}

func TestWriteBDTResultNAK(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	local := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.2:47808"))
	bbmd := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.1:47808"))
	tr := virtual.New(local, clk, 16)
	c, err := New(
		WithTransport(AdaptVirtual(tr)),
		withClock(clk),
		WithNetworkManagementEnabled(NetworkManagementConfirm),
		WithTransactionOptions(200*time.Millisecond, 0, 0),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	go func() {
		for {
			out := tr.Outbox()
			if len(out) == 0 {
				time.Sleep(5 * time.Millisecond)
				continue
			}
			msg, err := bvlc.Parse(out[len(out)-1].Data, bacnet.DefaultDecodeLimits())
			if err != nil || msg.Function != bvlc.FunctionWriteBroadcastDistributionTable {
				time.Sleep(5 * time.Millisecond)
				continue
			}
			frame, _ := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionResult, ResultCode: 0x0030})
			tr.Inject(virtual.InboundPacket{Data: frame, ImmediatePeer: bbmd, ReceivedAt: clk.Now()})
			return
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = c.WriteBroadcastDistributionTable(ctx, bbmd, nil)
	var opErr *BVLCOperationError
	if !errors.As(err, &opErr) || opErr.Code != 0x0030 {
		t.Fatalf("got %v", err)
	}
}
