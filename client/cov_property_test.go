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

func TestSubscribeCOVPropertyAndConfirmedNotification(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go serveSimpleACK(ctx, env.PeerTr, env.Local)

	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 2}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	inc := float32(0.5)
	sub, err := env.Client.SubscribeCOVProperty(context.Background(), env.Target, obj, prop, COVOptions{
		Lifetime: 4, BufferSize: 4, IssueConfirmed: true,
	}, &inc)
	if err != nil {
		t.Fatal(err)
	}
	if ev := <-sub.Events(); ev.State != SubscriptionActive {
		t.Fatalf("initial %v", ev.State)
	}

	note := service.COVNotification{
		ProcessIdentifier: 1,
		InitiatingDevice:  bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		MonitoredObject:   obj,
		TimeRemaining:     10,
		Values: []service.PropertyValue{{
			Property: prop,
			Value:    bacnet.RealValue(9.5),
		}},
	}
	payload := encodeCOVNotification(t, note)
	apduBytes := apdu.AppendConfirmedRequest(nil, apdu.ConfirmedRequest{
		InvokeID: 7, ServiceChoice: 1, MaxAPDU: 5, Payload: payload,
	})
	before := len(env.ClientTr.Outbox())
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apduBytes)

	select {
	case ev := <-sub.Events():
		if ev.Notification == nil {
			t.Fatalf("expected notification %#v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for confirmed COV")
	}

	limits := bacnet.DefaultDecodeLimits()
	deadline := time.Now().Add(time.Second)
	acked := false
	for time.Now().Before(deadline) && !acked {
		out := env.ClientTr.Outbox()
		for _, pkt := range out[before:] {
			msg, err := bvlc.Parse(pkt.Data, limits)
			if err != nil {
				continue
			}
			n, _, err := npdu.Parse(msg.Payload, limits)
			if err != nil || len(n.APDU) == 0 {
				continue
			}
			pdu, err := apdu.Parse(n.APDU, limits)
			if err == nil && pdu.SimpleACK != nil && pdu.SimpleACK.InvokeID == 7 {
				acked = true
				break
			}
		}
		if !acked {
			time.Sleep(5 * time.Millisecond)
		}
	}
	if !acked {
		t.Fatal("expected SimpleACK for confirmed COV indication")
	}
	_ = sub.Close()
}

func TestSubscribeCOVPropertyRejectsNonSimpleACK(t *testing.T) {
	env := newVirtualPair(t)
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 8}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}

	errCh := make(chan error, 1)
	go func() {
		_, err := env.Client.SubscribeCOVProperty(context.Background(), env.Target, obj, prop, COVOptions{Lifetime: 60}, nil)
		errCh <- err
	}()

	invokeID, _ := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendComplexACK(nil, apdu.ComplexACK{
		InvokeID: invokeID, ServiceChoice: apdu.ServiceSubscribeCOVProperty, Payload: []byte{0x01},
	}))

	err := <-errCh
	if !errors.Is(err, bacnet.ErrProtocolViolation) {
		t.Fatalf("got %v", err)
	}
}

func TestSubscribeCOVInitialFailureClosesChannel(t *testing.T) {
	env := newVirtualPair(t, WithTransactionOptions(200*time.Millisecond, 0, 0))
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 9}

	errCh := make(chan error, 1)
	go func() {
		_, err := env.Client.SubscribeCOV(context.Background(), env.Target, obj, COVOptions{Lifetime: 60})
		errCh <- err
	}()
	_, _ = waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	env.Clk.Advance(300 * time.Millisecond)

	err := <-errCh
	if err == nil {
		t.Fatal("expected subscribe failure")
	}
}

func TestClientCloseClosesSubscriptions(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go serveSimpleACK(ctx, env.PeerTr, env.Local)

	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 3}
	sub, err := env.Client.SubscribeCOV(context.Background(), env.Target, obj, COVOptions{Lifetime: 60, BufferSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	<-sub.Events()

	if err := env.Client.Close(); err != nil {
		t.Fatal(err)
	}
	closed := false
	for ev := range sub.Events() {
		if ev.State == SubscriptionClosed {
			if errors.Is(ev.Err, bacnet.ErrClosed) {
				closed = true
			}
		}
	}
	if !closed {
		t.Fatal("expected SubscriptionClosed with ErrClosed")
	}
}

func TestSubscribeCOVRenewal(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go serveSimpleACK(ctx, env.PeerTr, env.Local)

	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeBinaryValue, Instance: 1}
	sub, err := env.Client.SubscribeCOV(context.Background(), env.Target, obj, COVOptions{Lifetime: 2})
	if err != nil {
		t.Fatal(err)
	}
	if ev := <-sub.Events(); ev.State != SubscriptionActive {
		t.Fatalf("%v", ev.State)
	}

	// Allow renewLoop to arm its timer before advancing the manual clock.
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
				// renew may briefly degrade or race lifecycle events; keep waiting for Active
			}
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	_ = sub.Close()
	if !sawRenewing {
		t.Fatal("expected renew cycle")
	}
}
