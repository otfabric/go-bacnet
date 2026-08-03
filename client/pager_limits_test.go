// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/internal/clock"
	"github.com/otfabric/go-bacnet/internal/virtual"
	"github.com/otfabric/go-bacnet/service"
)

func TestReadRangeAllMaxItems(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyObjectList}
	go serveComplexACK(ctx, env.PeerTr, env.Local, func(choice uint8) ([]byte, error) {
		return service.EncodeReadRangeACK(service.ReadRangeACK{
			Object: obj, Property: prop,
			ResultFlags: service.EncodeResultFlags(true, false, true),
			ItemCount:   2,
			ItemData:    []bacnet.ApplicationValue{bacnet.UnsignedValue(1), bacnet.UnsignedValue(2)},
		})
	})
	got, err := env.Client.ReadRangeAll(ctx, env.Target, service.ReadRangeRequest{
		Object: obj, Property: prop, By: service.ReadRangeByPosition, ReferenceIndex: 1, Count: 2,
	}, ReadRangePageOptions{MaxItems: 2, MaxPages: 10})
	if err != nil || !got.Partial || got.StoppedReason != "MaxItems" {
		t.Fatalf("%v %#v", err, got)
	}
}

func TestReadRangeAllErrorAndSequenceAdvance(t *testing.T) {
	env := newVirtualPair(t)
	_ = env.Client.Close()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyObjectList}
	if _, err := env.Client.ReadRangeAll(context.Background(), env.Target, service.ReadRangeRequest{
		Object: obj, Property: prop, By: service.ReadRangeByPosition,
	}, ReadRangePageOptions{}); err == nil {
		t.Fatal("expected closed error")
	}

	env2 := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	seq := uint32(100)
	n := 0
	go serveComplexACK(ctx, env2.PeerTr, env2.Local, func(choice uint8) ([]byte, error) {
		n++
		more := n == 1
		var first *uint32
		if n == 1 {
			first = &seq
		}
		return service.EncodeReadRangeACK(service.ReadRangeACK{
			Object: obj, Property: prop,
			ResultFlags:   service.EncodeResultFlags(n == 1, !more, more),
			ItemCount:     1,
			ItemData:      []bacnet.ApplicationValue{{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{{Value: bacnet.UnsignedValue(1)}}}},
			FirstSequence: first,
		})
	})
	got, err := env2.Client.ReadRangeAll(ctx, env2.Target, service.ReadRangeRequest{
		Object: obj, Property: prop, By: service.ReadRangeBySequenceNumber, ReferenceIndex: 100, Count: 0,
	}, ReadRangePageOptions{MaxPages: 5, PageCount: 1})
	if err != nil || len(got.Pages) != 2 {
		t.Fatalf("%v pages=%d reason=%s", err, len(got.Pages), got.StoppedReason)
	}
}

func TestEventDispatcherDropOldestAndClientOption(t *testing.T) {
	clk := clock.NewManual(time.Now())
	local := bip.NewEndpoint(netip.MustParseAddrPort("127.0.0.1:47808"))
	tr := virtual.New(local, clk, 4)
	done := make(chan struct{}, 1)
	c, err := New(
		WithTransport(AdaptVirtual(tr)),
		withClock(clk),
		WithEventDispatcher(EventDispatcherConfig{
			Workers: 1, BufferSize: 1, Overflow: EventOverflowDropOldest,
			Handler: func(EventNotificationDelivery) {
				time.Sleep(40 * time.Millisecond)
				select {
				case done <- struct{}{}:
				default:
				}
			},
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	c.eventDispatcher.publish(EventNotificationDelivery{Notification: service.EventNotification{ProcessIdentifier: 1}})
	c.eventDispatcher.publish(EventNotificationDelivery{Notification: service.EventNotification{ProcessIdentifier: 2}})
	c.eventDispatcher.publish(EventNotificationDelivery{Notification: service.EventNotification{ProcessIdentifier: 3}})
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
	}
	if c.eventDispatcher.Dropped() == 0 {
		t.Fatal("expected drops")
	}
}
