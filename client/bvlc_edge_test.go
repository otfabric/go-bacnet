// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/internal/clock"
	"github.com/otfabric/go-bacnet/internal/diag"
	"github.com/otfabric/go-bacnet/internal/virtual"
	"github.com/otfabric/go-bacnet/service"
)

func TestBVLCExchangeClosedAndInvalid(t *testing.T) {
	env := newVirtualPair(t)
	_ = env.Client.Close()
	if _, err := env.Client.ReadBroadcastDistributionTable(context.Background(), env.Peer); err != bacnet.ErrClosed {
		t.Fatalf("got %v", err)
	}
	env2 := newVirtualPair(t)
	if _, err := env2.Client.ReadForeignDeviceTable(context.Background(), bip.Endpoint{}); err == nil {
		t.Fatal("expected invalid endpoint")
	}
}

func TestReadFDTResultEmpty(t *testing.T) {
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
			if err != nil || msg.Function != bvlc.FunctionReadForeignDeviceTable {
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
	entries, err := c.ReadForeignDeviceTable(ctx, bbmd)
	if err != nil || entries != nil {
		t.Fatalf("%v %#v", err, entries)
	}
}

func TestEventDispatcherPanicRecovered(t *testing.T) {
	d := newEventDispatcher(EventDispatcherConfig{
		Workers: 1, BufferSize: 2, Overflow: EventOverflowDropNewest,
		Handler: func(EventNotificationDelivery) { panic("boom") },
	}, diag.Discard{})
	d.publish(EventNotificationDelivery{})
	time.Sleep(30 * time.Millisecond)
	d.close()
}

func TestBVLCExchangeBusyTimeoutCancelAndDeliver(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	local := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.2:47808"))
	bbmd := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.1:47808"))
	other := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.9:47808"))
	tr := virtual.New(local, clk, 16)
	c, err := New(WithTransport(AdaptVirtual(tr)), withClock(clk), WithTransactionOptions(50*time.Millisecond, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	p := &bvlcPending{peer: bbmd, want: bvlc.FunctionReadBroadcastDistributionTableAck, op: "test", ch: make(chan bvlcPendingResult, 1)}
	if err := c.bvlcOps.begin(p); err != nil {
		t.Fatal(err)
	}
	if err := c.bvlcOps.begin(p); err == nil {
		t.Fatal("expected busy")
	}
	// Wrong peer / wrong function ignored; Result fills the buffered channel.
	if c.bvlcOps.deliver(bvlc.Message{Function: bvlc.FunctionResult}, other) {
		t.Fatal("wrong peer")
	}
	if c.bvlcOps.deliver(bvlc.Message{Function: bvlc.FunctionReadForeignDeviceTableAck}, bbmd) {
		t.Fatal("wrong function")
	}
	if !c.bvlcOps.deliver(bvlc.Message{Function: bvlc.FunctionResult, ResultCode: 0}, bbmd) {
		t.Fatal("expected result deliver")
	}
	// Channel full → default branch.
	if !c.bvlcOps.deliver(bvlc.Message{Function: bvlc.FunctionResult, ResultCode: 1}, bbmd) {
		t.Fatal("expected result deliver on full channel")
	}
	c.bvlcOps.clear(p)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.ReadBroadcastDistributionTable(ctx, bbmd); err == nil {
		t.Fatal("expected canceled")
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	errCh := make(chan error, 1)
	go func() {
		_, err := c.ReadBroadcastDistributionTable(ctx2, bbmd)
		errCh <- err
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(tr.Outbox()) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	clk.Advance(200 * time.Millisecond)
	select {
	case err := <-errCh:
		if err != bacnet.ErrTimeout {
			t.Fatalf("got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting")
	}
}

func TestEventDispatcherDefaultsAndClosedPublish(t *testing.T) {
	d := newEventDispatcher(EventDispatcherConfig{
		Handler: func(EventNotificationDelivery) {},
	}, diag.Discard{})
	if d.cfg.Workers != 1 || d.cfg.BufferSize != 128 {
		t.Fatalf("defaults %#v", d.cfg)
	}
	d.close()
	d.publish(EventNotificationDelivery{}) // closed path
}

func TestClientEventDispatcherReceivePath(t *testing.T) {
	clk := clock.NewManual(time.Now())
	local := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.1:47808"))
	tr := virtual.New(local, clk, 4)
	got := make(chan struct{}, 1)
	c, err := New(
		WithTransport(AdaptVirtual(tr)),
		withClock(clk),
		WithEventDispatcher(EventDispatcherConfig{
			Workers: 1, BufferSize: 4, Overflow: EventOverflowDropNewest,
			Handler: func(EventNotificationDelivery) {
				select {
				case got <- struct{}{}:
				default:
				}
			},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	c.deliverEventNotification(service.EventNotification{ProcessIdentifier: 7}, false, packetSource{})
	select {
	case <-got:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("dispatcher handler not called")
	}
}
