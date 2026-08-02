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

func TestEventStreamGapAndReplace(t *testing.T) {
	env := newVirtualPair(t)
	s1 := env.Client.OpenEventStream(1)
	// Fill buffer then overflow to set gap.
	env.Client.deliverEventNotification(service.EventNotification{EventType: 1, ToState: 1}, false, packetSource{})
	env.Client.deliverEventNotification(service.EventNotification{EventType: 2, ToState: 2}, false, packetSource{})
	env.Client.deliverEventNotification(service.EventNotification{EventType: 3, ToState: 3}, false, packetSource{})
	// Replace stream closes previous.
	s2 := env.Client.OpenEventStream(2)
	s1.Close() // idempotent after replace
	env.Client.deliverEventNotification(service.EventNotification{EventType: 9, ToState: 4}, true, packetSource{})
	select {
	case ev := <-s2.Events():
		if ev.Delivery.Notification.EventType != 9 || !ev.Delivery.Confirmed {
			t.Fatalf("%+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
	from := uint32(1)
	ack := true
	tr := TransitionOf(service.EventNotification{
		EventType: 5, NotifyType: 0, FromState: &from, ToState: 2, AckRequired: &ack,
	})
	if tr.FromState != 1 || tr.ToState != 2 || !tr.AckRequired || tr.EventType != 5 {
		t.Fatalf("%+v", tr)
	}
	s2.Close()
	// publish after close is no-op
	env.Client.deliverEventNotification(service.EventNotification{EventType: 1}, false, packetSource{})
}

func TestGetEnrollmentSummaryProtocolViolation(t *testing.T) {
	env := newVirtualPair(t)
	errCh := make(chan error, 1)
	go func() {
		_, e := env.Client.GetEnrollmentSummary(context.Background(), env.Target, service.GetEnrollmentSummaryRequest{})
		errCh <- e
	}()
	invokeID, _ := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendSimpleACK(nil, apdu.SimpleACK{
		InvokeID: invokeID, ServiceChoice: apdu.ServiceGetEnrollmentSummary,
	}))
	if err := <-errCh; err != bacnet.ErrProtocolViolation {
		t.Fatalf("got %v", err)
	}
}

func TestOpenEventStreamDefaultCapacity(t *testing.T) {
	env := newVirtualPair(t)
	s := env.Client.OpenEventStream(0)
	defer s.Close()
	env.Client.deliverEventNotification(service.EventNotification{EventType: 7}, false, packetSource{})
	select {
	case <-s.Events():
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}
