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

func TestCreateObjectAndReadback(t *testing.T) {
	env := newVirtualPair(t)
	ackPayload, err := service.EncodeCreateObjectACK(service.CreateObjectACK{
		Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 101},
	})
	if err != nil {
		t.Fatal(err)
	}
	errCh := make(chan error, 1)
	go func() {
		ot := bacnet.ObjectTypeAnalogValue
		_, _, e := env.Client.CreateObjectAndReadback(context.Background(), env.Target, service.CreateObjectRequest{ObjectType: &ot}, bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName})
		errCh <- e
	}()
	invokeID, choice := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	if choice != apdu.ServiceCreateObject {
		t.Fatalf("choice=%d", choice)
	}
	since := len(env.ClientTr.Outbox())
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendComplexACK(nil, apdu.ComplexACK{
		InvokeID: invokeID, ServiceChoice: apdu.ServiceCreateObject, Payload: ackPayload,
	}))
	invokeID2, choice2 := waitConfirmedInvokeIDSince(t, env.ClientTr, since, time.Second)
	if choice2 != apdu.ServiceReadProperty {
		t.Fatalf("choice2=%d", choice2)
	}
	rpAck, err := service.EncodeReadPropertyACK(service.ReadPropertyACK{
		Object:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 101},
		Property: bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName},
		Value:    bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: "AV-101"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendComplexACK(nil, apdu.ComplexACK{
		InvokeID: invokeID2, ServiceChoice: apdu.ServiceReadProperty, Payload: rpAck,
	}))
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
}

func TestUnconfirmedAuditNotificationReceive(t *testing.T) {
	env := newVirtualPair(t)
	stream := env.Client.OpenAuditStream(1)
	defer stream.Close()
	payload, err := service.EncodeAuditNotification(service.AuditNotification{
		SourceDevice: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		Operation:    3,
	})
	if err != nil {
		t.Fatal(err)
	}
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{
		ServiceChoice: apdu.ServiceUnconfirmedAuditNotification, Payload: payload,
	}))
	select {
	case ev := <-stream.Events():
		if ev.Confirmed || ev.Notification.Operation != 3 {
			t.Fatalf("%+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestProtocolViolationsAdvanced(t *testing.T) {
	env := newVirtualPair(t)
	errCh := make(chan error, 1)
	go func() {
		_, e := env.Client.AtomicReadFile(context.Background(), env.Target, service.AtomicReadFileRequest{
			File:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 1},
			Access: service.FileAccessStream, Count: 1,
		})
		errCh <- e
	}()
	invokeID, _ := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendSimpleACK(nil, apdu.SimpleACK{
		InvokeID: invokeID, ServiceChoice: apdu.ServiceAtomicReadFile,
	}))
	if err := <-errCh; err != bacnet.ErrProtocolViolation {
		t.Fatalf("%v", err)
	}
}
