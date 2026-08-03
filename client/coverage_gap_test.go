// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/internal/clock"
	"github.com/otfabric/go-bacnet/internal/virtual"
	"github.com/otfabric/go-bacnet/service"
)

func TestReadRangeAllMaxBytesAndPagesAndSequence(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyObjectList}
	go serveComplexACK(ctx, env.PeerTr, env.Local, func(choice uint8) ([]byte, error) {
		return service.EncodeReadRangeACK(service.ReadRangeACK{
			Object: obj, Property: prop,
			ResultFlags: service.EncodeResultFlags(true, false, true),
			ItemCount:   1,
			ItemData: []bacnet.ApplicationValue{{
				Kind: bacnet.ValueOctetString, OctetString: make([]byte, 64),
			}},
		})
	})
	got, err := env.Client.ReadRangeAll(ctx, env.Target, service.ReadRangeRequest{
		Object: obj, Property: prop, By: service.ReadRangeByPosition, ReferenceIndex: 1, Count: 1,
	}, ReadRangePageOptions{MaxBytes: 32, MaxPages: 10})
	if err != nil || got.StoppedReason != "MaxBytes" {
		t.Fatalf("%v %#v", err, got)
	}

	env2 := newVirtualPair(t)
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	go serveComplexACK(ctx2, env2.PeerTr, env2.Local, func(choice uint8) ([]byte, error) {
		seq := uint32(10)
		return service.EncodeReadRangeACK(service.ReadRangeACK{
			Object: obj, Property: prop,
			ResultFlags:   service.EncodeResultFlags(true, false, true),
			ItemCount:     1,
			ItemData:      []bacnet.ApplicationValue{bacnet.UnsignedValue(1)},
			FirstSequence: &seq,
		})
	})
	got, err = env2.Client.ReadRangeAll(ctx2, env2.Target, service.ReadRangeRequest{
		Object: obj, Property: prop, By: service.ReadRangeBySequenceNumber, ReferenceIndex: 10, Count: 1,
	}, ReadRangePageOptions{MaxPages: 1, MaxItems: 100})
	if err != nil || got.StoppedReason != "MaxPages" {
		t.Fatalf("%v %#v", err, got)
	}
}

func TestReadBDTResultEmptyAndBusy(t *testing.T) {
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
			frame, _ := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionResult, ResultCode: 0})
			tr.Inject(virtual.InboundPacket{Data: frame, ImmediatePeer: bbmd, ReceivedAt: clk.Now()})
			return
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	entries, err := c.ReadBroadcastDistributionTable(ctx, bbmd)
	if err != nil || entries != nil {
		t.Fatalf("%v %#v", err, entries)
	}

	c.bvlcOps.pending = &bvlcPending{peer: bbmd, ch: make(chan bvlcPendingResult, 1)}
	if err := c.bvlcOps.begin(&bvlcPending{}); !errors.Is(err, bacnet.ErrBusy) {
		t.Fatalf("got %v", err)
	}
	c.bvlcOps.pending = nil
}

func TestInvokeConfirmedSideEffectingUnknown(t *testing.T) {
	env := newVirtualPair(t, WithTransactionOptions(50*time.Millisecond, 0, 0))
	errCh := make(chan error, 1)
	go func() {
		_, err := env.Client.InvokeConfirmed(context.Background(), env.Target, apdu.ServiceWriteProperty, []byte{0xc4, 0x02, 0x00, 0x00, 0x01}, ConfirmedInvokeOptions{
			SideEffecting: true,
		})
		errCh <- err
	}()
	_, _ = waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	env.Clk.Advance(100 * time.Millisecond)
	err := <-errCh
	var unk *bacnet.OutcomeUnknownError
	if !errors.As(err, &unk) {
		t.Fatalf("got %v", err)
	}
}
