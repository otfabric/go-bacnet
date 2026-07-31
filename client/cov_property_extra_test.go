// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/npdu"
	"github.com/otfabric/go-bacnet/service"
)

func TestSubscribeCOVPropertyEncodeErrorCleanup(t *testing.T) {
	env := newVirtualPair(t)
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: bacnet.MaxObjectInstance + 1}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}

	_, err := env.Client.SubscribeCOVProperty(context.Background(), env.Target, obj, prop, COVOptions{Lifetime: 60}, nil)
	if !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("got %v", err)
	}
}

func TestSubscribeCOVPropertyInitialTimeout(t *testing.T) {
	env := newVirtualPair(t, WithTransactionOptions(200*time.Millisecond, 0, 0))
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 11}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}

	errCh := make(chan error, 1)
	go func() {
		_, err := env.Client.SubscribeCOVProperty(context.Background(), env.Target, obj, prop, COVOptions{Lifetime: 60}, nil)
		errCh <- err
	}()
	_, _ = waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	env.Clk.Advance(300 * time.Millisecond)

	if err := <-errCh; err == nil {
		t.Fatal("expected subscribe failure")
	}
}

func TestSubscribeCOVPropertyDefaults(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go serveSimpleACK(ctx, env.PeerTr, env.Local)

	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 12}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}

	errCh := make(chan error, 1)
	go func() {
		sub, err := env.Client.SubscribeCOVProperty(context.Background(), env.Target, obj, prop, COVOptions{}, nil)
		if err != nil {
			errCh <- err
			return
		}
		if ev := <-sub.Events(); ev.State != SubscriptionActive {
			t.Errorf("initial %v", ev.State)
		}
		_ = sub.Close()
		errCh <- nil
	}()

	invokeID, _ := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	limits := bacnet.DefaultDecodeLimits()
	foundLifetime := false
	for _, pkt := range env.ClientTr.Outbox() {
		msg, err := bvlc.Parse(pkt.Data, limits)
		if err != nil {
			continue
		}
		n, _, err := npdu.Parse(msg.Payload, limits)
		if err != nil || len(n.APDU) == 0 {
			continue
		}
		pdu, err := apdu.Parse(n.APDU, limits)
		if err != nil || pdu.ConfirmedRequest == nil {
			continue
		}
		if pdu.ConfirmedRequest.InvokeID != invokeID {
			continue
		}
		req, err := service.DecodeSubscribeCOVProperty(pdu.ConfirmedRequest.Payload, limits)
		if err != nil {
			continue
		}
		if req.Lifetime == 60 {
			foundLifetime = true
		}
	}
	if !foundLifetime {
		t.Fatal("did not find SubscribeCOVProperty request")
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
}

func TestSubscribeCOVPropertyRenewal(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go serveSimpleACK(ctx, env.PeerTr, env.Local)

	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 13}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	sub, err := env.Client.SubscribeCOVProperty(context.Background(), env.Target, obj, prop, COVOptions{Lifetime: 2}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ev := <-sub.Events(); ev.State != SubscriptionActive {
		t.Fatalf("%v", ev.State)
	}

	time.Sleep(20 * time.Millisecond)
	env.Clk.Advance(1100 * time.Millisecond)

	sawRenewing := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case ev := <-sub.Events():
			switch ev.State {
			case SubscriptionRenewing:
				sawRenewing = true
			case SubscriptionActive:
				if sawRenewing {
					_ = sub.Close()
					return
				}
			case SubscriptionPending, SubscriptionDegraded, SubscriptionExpired, SubscriptionClosed:
			}
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	_ = sub.Close()
	if !sawRenewing {
		t.Fatal("expected property renew cycle")
	}
}

func TestSubscribeCOVPropertyRenewalDegrade(t *testing.T) {
	// Manual ACK for the initial subscribe only. Do not call sub.Close(): that
	// sends a cancel confirmed-request and blocks forever on the manual clock
	// when no peer ACKs. Client cleanup uses closeAll (cancel + wait), which is safe.
	env := newVirtualPair(t)
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 14}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}

	errCh := make(chan error, 1)
	subCh := make(chan Subscription, 1)
	go func() {
		sub, err := env.Client.SubscribeCOVProperty(context.Background(), env.Target, obj, prop, COVOptions{Lifetime: 2}, nil)
		if err != nil {
			errCh <- err
			return
		}
		subCh <- sub
	}()

	invokeID, _ := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendSimpleACK(nil, apdu.SimpleACK{
		InvokeID: invokeID, ServiceChoice: apdu.ServiceSubscribeCOVProperty,
	}))

	var sub Subscription
	select {
	case err := <-errCh:
		t.Fatalf("initial subscribe: %v", err)
	case sub = <-subCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for property subscription")
	}
	if ev := <-sub.Events(); ev.State != SubscriptionActive {
		t.Fatalf("initial %v", ev.State)
	}

	time.Sleep(20 * time.Millisecond)
	before := len(env.ClientTr.Outbox())
	env.Clk.Advance(1100 * time.Millisecond)

	renewID, _ := waitConfirmedInvokeIDSince(t, env.ClientTr, before, time.Second)
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendComplexACK(nil, apdu.ComplexACK{
		InvokeID: renewID, ServiceChoice: apdu.ServiceSubscribeCOVProperty, Payload: []byte{0x00},
	}))

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case ev := <-sub.Events():
			if ev.State == SubscriptionDegraded {
				return
			}
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	t.Fatal("expected property renewal degrade")
}

func TestSubscribeCOVPropertyCloseCancellation(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go serveSimpleACK(ctx, env.PeerTr, env.Local)

	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 15}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	sub, err := env.Client.SubscribeCOVProperty(context.Background(), env.Target, obj, prop, COVOptions{Lifetime: 60}, nil)
	if err != nil {
		t.Fatal(err)
	}
	<-sub.Events()

	if err := sub.Close(); err != nil {
		t.Fatal(err)
	}
	closed := false
	for ev := range sub.Events() {
		if ev.State == SubscriptionClosed {
			closed = true
		}
	}
	if !closed {
		t.Fatal("expected SubscriptionClosed after property Close")
	}
}

func TestClientCloseClosesPropertySubscriptions(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go serveSimpleACK(ctx, env.PeerTr, env.Local)

	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 16}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	sub, err := env.Client.SubscribeCOVProperty(context.Background(), env.Target, obj, prop, COVOptions{Lifetime: 60, BufferSize: 2}, nil)
	if err != nil {
		t.Fatal(err)
	}
	<-sub.Events()

	if err := env.Client.Close(); err != nil {
		t.Fatal(err)
	}
	closed := false
	for ev := range sub.Events() {
		if ev.State == SubscriptionClosed && errors.Is(ev.Err, bacnet.ErrClosed) {
			closed = true
		}
	}
	if !closed {
		t.Fatal("expected SubscriptionClosed with ErrClosed for property subscription")
	}
}
