// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/npdu"
	"github.com/otfabric/go-bacnet/service"
)

func TestDeliverCOVNotificationMultipleSynthesizes(t *testing.T) {
	env := newVirtualPair(t)
	note := service.COVNotificationMultiple{
		SubscriberProcessIdentifier: 9,
		InitiatingDevice:            bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1234},
		TimeRemaining:               10,
		Objects: []service.COVNotificationMultipleObject{{
			Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
			Values: []service.COVNotificationMultipleValue{{
				Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
				Value:    bacnet.RealValue(22),
			}},
		}},
	}
	// Should not panic without matching subscription.
	env.Client.deliverCOVNotificationMultiple(note, true, packetSource{})
}

func TestGetAlarmSummaryProtocolViolation(t *testing.T) {
	env := newVirtualPair(t)
	errCh := make(chan error, 1)
	go func() {
		_, e := env.Client.GetAlarmSummary(context.Background(), env.Target)
		errCh <- e
	}()
	invokeID, _ := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendSimpleACK(nil, apdu.SimpleACK{
		InvokeID: invokeID, ServiceChoice: apdu.ServiceGetAlarmSummary,
	}))
	if err := <-errCh; err != bacnet.ErrProtocolViolation {
		t.Fatalf("got %v", err)
	}
}

func TestConfirmedCOVNotificationMultipleReceive(t *testing.T) {
	env := newVirtualPair(t)
	payload, err := service.EncodeCOVNotificationMultiple(service.COVNotificationMultiple{
		SubscriberProcessIdentifier: 42,
		InitiatingDevice:            bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		TimeRemaining:               5,
		Objects: []service.COVNotificationMultipleObject{{
			Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
			Values: []service.COVNotificationMultipleValue{{
				Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
				Value:    bacnet.RealValue(3),
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	before := len(env.ClientTr.Outbox())
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendConfirmedRequest(nil, apdu.ConfirmedRequest{
		InvokeID: 21, ServiceChoice: apdu.ServiceConfirmedCOVNotificationMultiple, MaxAPDU: 5, Payload: payload,
	}))
	limits := bacnet.DefaultDecodeLimits()
	deadline := time.Now().Add(time.Second)
	acked := false
	for time.Now().Before(deadline) && !acked {
		for _, pkt := range env.ClientTr.Outbox()[before:] {
			msg, err := bvlc.Parse(pkt.Data, limits)
			if err != nil {
				continue
			}
			n, _, err := npdu.Parse(msg.Payload, limits)
			if err != nil || len(n.APDU) == 0 {
				continue
			}
			pdu, err := apdu.Parse(n.APDU, limits)
			if err == nil && pdu.SimpleACK != nil && pdu.SimpleACK.InvokeID == 21 {
				acked = true
				break
			}
		}
		if !acked {
			time.Sleep(5 * time.Millisecond)
		}
	}
	if !acked {
		t.Fatal("expected SimpleACK for confirmed COVNotificationMultiple")
	}
}

func TestConfirmedCOVNotificationMultipleMalformed(t *testing.T) {
	env := newVirtualPair(t)
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendConfirmedRequest(nil, apdu.ConfirmedRequest{
		InvokeID: 22, ServiceChoice: apdu.ServiceConfirmedCOVNotificationMultiple, MaxAPDU: 5, Payload: []byte{0x09},
	}))
	time.Sleep(50 * time.Millisecond)
}

func TestUnconfirmedCOVNotificationMultipleReceive(t *testing.T) {
	env := newVirtualPair(t)
	payload, err := service.EncodeCOVNotificationMultiple(service.COVNotificationMultiple{
		SubscriberProcessIdentifier: 7,
		InitiatingDevice:            bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 2},
		TimeRemaining:               1,
		Objects: []service.COVNotificationMultipleObject{{
			Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
			Values: []service.COVNotificationMultipleValue{{
				Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
				Value:    bacnet.RealValue(1),
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{
		ServiceChoice: apdu.ServiceUnconfirmedCOVNotificationMultiple, Payload: payload,
	}))
	time.Sleep(50 * time.Millisecond)
}

func TestSubscribeCOVPropertyMultipleProtocolViolation(t *testing.T) {
	env := newVirtualPair(t)
	errCh := make(chan error, 1)
	go func() {
		errCh <- env.Client.SubscribeCOVPropertyMultiple(context.Background(), env.Target, service.SubscribeCOVPropertyMultipleRequest{
			SubscriberProcessIdentifier: 1,
			Subscriptions: []service.COVMultipleSubscription{{
				Object:     bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
				Properties: []service.COVPropertyReference{{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}}},
			}},
		})
	}()
	invokeID, _ := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendComplexACK(nil, apdu.ComplexACK{
		InvokeID: invokeID, ServiceChoice: apdu.ServiceSubscribeCOVPropertyMultiple,
	}))
	if err := <-errCh; err != bacnet.ErrProtocolViolation {
		t.Fatalf("got %v", err)
	}
}
