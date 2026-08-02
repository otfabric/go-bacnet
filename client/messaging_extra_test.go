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

func TestAtomicWriteFileComplexACK(t *testing.T) {
	env := newVirtualPair(t)
	ackPayload, err := service.EncodeAtomicWriteFileACK(service.AtomicWriteFileACK{Access: service.FileAccessStream, StartPosition: 4})
	if err != nil {
		t.Fatal(err)
	}
	errCh := make(chan error, 1)
	var got service.AtomicWriteFileACK
	go func() {
		var e error
		got, e = env.Client.AtomicWriteFile(context.Background(), env.Target, service.AtomicWriteFileRequest{
			File:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 1},
			Access: service.FileAccessStream, Data: []byte("ab"),
		})
		errCh <- e
	}()
	invokeID, choice := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	if choice != apdu.ServiceAtomicWriteFile {
		t.Fatalf("choice=%d", choice)
	}
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendComplexACK(nil, apdu.ComplexACK{
		InvokeID: invokeID, ServiceChoice: apdu.ServiceAtomicWriteFile, Payload: ackPayload,
	}))
	if err := <-errCh; err != nil || got.StartPosition != 4 {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestConfirmedPrivateTransferAndText(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go serveSimpleACK(ctx, env.PeerTr, env.Local)
	if err := env.Client.ConfirmedPrivateTransfer(ctx, env.Target, service.PrivateTransfer{VendorID: 1, ServiceNumber: 2}); err != nil {
		t.Fatal(err)
	}
	if err := env.Client.ConfirmedTextMessage(ctx, env.Target, service.TextMessage{
		TextMessageSourceDevice: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		MessagePriority:         0,
		Message:                 "ping",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestMessagingEncodeAndProtocolErrors(t *testing.T) {
	env := newVirtualPair(t)
	ctx := context.Background()
	if err := env.Client.WriteGroup(ctx, env.Target.Endpoint, true, service.WriteGroup{
		GroupNumber: 1, WritePriority: 8,
	}); err == nil {
		t.Fatal("expected empty change list error")
	}
	if err := env.Client.YouAre(ctx, env.Target.Endpoint, true, service.YouAre{
		VendorID: 1, ModelName: "m", SerialNumber: "s",
		Device: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: bacnet.MaxObjectInstance + 1},
	}); err == nil {
		t.Fatal("expected invalid object id encode error")
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- env.Client.ConfirmedTextMessage(ctx, env.Target, service.TextMessage{
			TextMessageSourceDevice: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
			Message:                 "x",
		})
	}()
	invokeID, choice := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	if choice != apdu.ServiceConfirmedTextMessage {
		t.Fatalf("choice=%d", choice)
	}
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendComplexACK(nil, apdu.ComplexACK{
		InvokeID: invokeID, ServiceChoice: apdu.ServiceConfirmedTextMessage,
	}))
	if err := <-errCh; err == nil {
		t.Fatal("expected protocol violation on ComplexACK for text message")
	}
}

func TestUnconfirmedMessagingUnicast(t *testing.T) {
	env := newVirtualPair(t)
	ctx := context.Background()
	if err := env.Client.UnconfirmedPrivateTransfer(ctx, env.Target.Endpoint, false, service.PrivateTransfer{VendorID: 1, ServiceNumber: 1}); err != nil {
		t.Fatal(err)
	}
	if err := env.Client.TimeSynchronization(ctx, env.Target.Endpoint, false, service.TimeSynchronization{
		Date: bacnet.Date{Year: 124, Month: 1, Day: 1},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestUnconfirmedMessagingSends(t *testing.T) {
	env := newVirtualPair(t)
	ctx := context.Background()
	before := len(env.ClientTr.Outbox())
	if err := env.Client.UnconfirmedPrivateTransfer(ctx, env.Target.Endpoint, true, service.PrivateTransfer{VendorID: 1, ServiceNumber: 1}); err != nil {
		t.Fatal(err)
	}
	if err := env.Client.UnconfirmedTextMessage(ctx, env.Target.Endpoint, true, service.TextMessage{
		TextMessageSourceDevice: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		Message:                 "x",
	}); err != nil {
		t.Fatal(err)
	}
	if err := env.Client.TimeSynchronization(ctx, env.Target.Endpoint, true, service.TimeSynchronization{
		Date: bacnet.Date{Year: 124, Month: 1, Day: 1}, Time: bacnet.Time{Hour: 1},
	}); err != nil {
		t.Fatal(err)
	}
	if err := env.Client.UTCTimeSynchronization(ctx, env.Target.Endpoint, true, service.TimeSynchronization{
		Date: bacnet.Date{Year: 124, Month: 1, Day: 1}, Time: bacnet.Time{Hour: 1},
	}); err != nil {
		t.Fatal(err)
	}
	if err := env.Client.WriteGroup(ctx, env.Target.Endpoint, true, service.WriteGroup{
		GroupNumber: 1, WritePriority: 8, ChangeList: []bacnet.Element{{Value: bacnet.UnsignedValue(1)}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := env.Client.WhoAmI(ctx, env.Target.Endpoint, true, service.WhoAmI{VendorID: 1, ModelName: "m", SerialNumber: "s"}); err != nil {
		t.Fatal(err)
	}
	if err := env.Client.YouAre(ctx, env.Target.Endpoint, true, service.YouAre{
		VendorID: 1, ModelName: "m", SerialNumber: "s",
		Device: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
	}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(env.ClientTr.Outbox()) > before+5 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("expected unconfirmed sends, outbox=%d", len(env.ClientTr.Outbox())-before)
}

func TestRemoveListElementAndVTCloseData(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go serveSimpleACK(ctx, env.PeerTr, env.Local)
	if err := env.Client.RemoveListElement(ctx, env.Target, service.ListElementRequest{
		Object:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		Property: bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName},
		Elements: []bacnet.Element{{Value: bacnet.UnsignedValue(1)}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := env.Client.VTClose(ctx, env.Target, service.VTCloseRequest{RemoteVTSessionIdentifiers: []uint8{1}}); err != nil {
		t.Fatal(err)
	}
	if err := env.Client.VTData(ctx, env.Target, service.VTDataRequest{VTSessionIdentifier: 1, VTNewData: []byte{9}, VTDataFlag: 0}); err != nil {
		t.Fatal(err)
	}
	if err := env.Client.AuthRequest(ctx, env.Target, service.AuthRequest{}); err != nil {
		t.Fatal(err)
	}
}

func TestConfirmedAuditNotificationReceive(t *testing.T) {
	env := newVirtualPair(t)
	stream := env.Client.OpenAuditStream(1)
	defer stream.Close()
	payload, err := service.EncodeAuditNotification(service.AuditNotification{
		SourceDevice: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		Operation:    9,
	})
	if err != nil {
		t.Fatal(err)
	}
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendConfirmedRequest(nil, apdu.ConfirmedRequest{
		InvokeID: 3, ServiceChoice: apdu.ServiceConfirmedAuditNotification, MaxAPDU: 5, Payload: payload,
	}))
	select {
	case ev := <-stream.Events():
		if ev.Notification.Operation != 9 || !ev.Confirmed {
			t.Fatalf("%+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}
