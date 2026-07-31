// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/service"
)

func TestSubscribeCOVEventsAndClose(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go serveSimpleACK(ctx, env.PeerTr, env.Local)

	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 7}
	sub, err := env.Client.SubscribeCOV(context.Background(), env.Target, obj, COVOptions{Lifetime: 60, BufferSize: 4})
	if err != nil {
		t.Fatal(err)
	}

	ev := <-sub.Events()
	if ev.State != SubscriptionActive {
		t.Fatalf("initial state %v", ev.State)
	}

	// First subscription receives process identifier 1 (subscriptionManager.next starts at 1).
	note := service.COVNotification{
		ProcessIdentifier: 1,
		InitiatingDevice:  bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		MonitoredObject:   obj,
		TimeRemaining:     45,
		Values: []service.PropertyValue{{
			Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
			Value:    bacnet.RealValue(12.5),
		}},
	}
	payload := encodeCOVNotification(t, note)
	apduBytes := apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{
		ServiceChoice: apdu.ServiceUnconfirmedCOV,
		Payload:       payload,
	})
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apduBytes)

	select {
	case ev = <-sub.Events():
		if ev.Notification == nil || ev.State != SubscriptionActive {
			t.Fatalf("notification event %#v", ev)
		}
		if ev.Notification.MonitoredObject != obj {
			t.Fatalf("object %#v", ev.Notification.MonitoredObject)
		}
		f, err := bacnet.AsReal(ev.Notification.Values[0].Value)
		if err != nil || f != 12.5 {
			t.Fatalf("value %v err=%v", f, err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for COV notification")
	}

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
		t.Fatal("expected SubscriptionClosed event")
	}
}
