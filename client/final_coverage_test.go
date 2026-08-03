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

func TestWriteBDTEncodeErrorAndDeleteInvalid(t *testing.T) {
	env := newVirtualPair(t, WithNetworkManagementEnabled(NetworkManagementConfirm))
	bad := []bvlc.BDTEntry{{Endpoint: bip.Endpoint{}, Mask: bvlc.IPv4Mask{255, 255, 255, 255}}}
	if err := env.Client.WriteBroadcastDistributionTable(context.Background(), env.Peer, bad); err == nil {
		t.Fatal("expected encode error")
	}
	if err := env.Client.DeleteForeignDeviceTableEntry(context.Background(), env.Peer, bip.Endpoint{}); err == nil {
		t.Fatal("expected delete encode error")
	}
}

func TestWPMBatchedFailedReject(t *testing.T) {
	env := newVirtualPair(t)
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	errCh := make(chan error, 1)
	var res BatchWriteResult
	go func() {
		var err error
		res, err = env.Client.WritePropertyMultipleBatched(context.Background(), env.Target, []service.WriteAccessSpecification{{
			Object: obj,
			Properties: []service.WritePropertyValue{{
				Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
				Value:    bacnet.RealValue(1),
			}},
		}}, WPMBatchOptions{})
		errCh <- err
	}()
	invokeID, _ := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendReject(nil, apdu.RejectPDU{
		InvokeID: invokeID, Reason: 1,
	}))
	err := <-errCh
	if err == nil || res.Batches[0].State != BatchWriteFailed {
		t.Fatalf("%v %#v", err, res)
	}
}

func TestReadRangeAllRequestError(t *testing.T) {
	env := newVirtualPair(t, WithTransactionOptions(50*time.Millisecond, 0, 0))
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyObjectList}
	errCh := make(chan error, 1)
	var got ReadRangePageResult
	go func() {
		var err error
		got, err = env.Client.ReadRangeAll(context.Background(), env.Target, service.ReadRangeRequest{
			Object: obj, Property: prop, By: service.ReadRangeByPosition, ReferenceIndex: 1, Count: 1,
		}, ReadRangePageOptions{})
		errCh <- err
	}()
	_, _ = waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	env.Clk.Advance(100 * time.Millisecond)
	err := <-errCh
	if err == nil || !got.Partial {
		t.Fatalf("%v %#v", err, got)
	}
}

func TestNilClientRoutesAndBVLCWrongPeer(t *testing.T) {
	if Routes := (*Client)(nil).Routes(1); Routes != nil {
		t.Fatal(Routes)
	}
	clk := clock.NewManual(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	local := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.2:47808"))
	bbmd := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.1:47808"))
	other := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.9:47808"))
	tr := virtual.New(local, clk, 16)
	c, err := New(WithTransport(AdaptVirtual(tr)), withClock(clk), WithTransactionOptions(150*time.Millisecond, 0, 0))
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
			// Wrong peer Result should be ignored; then correct ACK.
			frameWrong, _ := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionResult, ResultCode: 0})
			tr.Inject(virtual.InboundPacket{Data: frameWrong, ImmediatePeer: other, ReceivedAt: clk.Now()})
			payload, _ := bvlc.EncodeBDTEntries(nil, []bvlc.BDTEntry{{Endpoint: bbmd, Mask: bvlc.IPv4Mask{255, 255, 255, 255}}})
			frame, _ := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionReadBroadcastDistributionTableAck, Payload: payload})
			tr.Inject(virtual.InboundPacket{Data: frame, ImmediatePeer: bbmd, ReceivedAt: clk.Now()})
			return
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	entries, err := c.ReadBroadcastDistributionTable(ctx, bbmd)
	if err != nil || len(entries) != 1 {
		t.Fatalf("%v %#v", err, entries)
	}
}

func TestRouterCacheMaxPerNet(t *testing.T) {
	r := newRouterCache()
	r.maxPerNet = 2
	r.maxGlobal = 100
	now := time.Now()
	for i := 0; i < 4; i++ {
		ep := bip.NewEndpoint(netip.MustParseAddrPort(netip.AddrFrom4([4]byte{10, 0, 0, byte(i + 1)}).String() + ":47808"))
		r.upsertLearned(1, ep, bip.Endpoint{}, now.Add(time.Duration(i)*time.Second))
	}
	if len(r.routes(1)) != 2 {
		t.Fatalf("%d", len(r.routes(1)))
	}
	_ = errors.New("")
}
