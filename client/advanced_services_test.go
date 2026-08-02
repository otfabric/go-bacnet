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

func TestAtomicReadFileComplexACK(t *testing.T) {
	env := newVirtualPair(t)
	ackPayload, err := service.EncodeAtomicReadFileACK(service.AtomicReadFileACK{
		EndOfFile: true, Access: service.FileAccessStream, Data: []byte("x"),
	})
	if err != nil {
		t.Fatal(err)
	}
	errCh := make(chan error, 1)
	var got service.AtomicReadFileACK
	go func() {
		var e error
		got, e = env.Client.AtomicReadFile(context.Background(), env.Target, service.AtomicReadFileRequest{
			File:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 1},
			Access: service.FileAccessStream, Count: 1,
		})
		errCh <- e
	}()
	invokeID, choice := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	if choice != apdu.ServiceAtomicReadFile {
		t.Fatalf("choice=%d", choice)
	}
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendComplexACK(nil, apdu.ComplexACK{
		InvokeID: invokeID, ServiceChoice: apdu.ServiceAtomicReadFile, Payload: ackPayload,
	}))
	if err := <-errCh; err != nil || string(got.Data) != "x" {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestCreateObjectComplexACK(t *testing.T) {
	env := newVirtualPair(t)
	ackPayload, err := service.EncodeCreateObjectACK(service.CreateObjectACK{
		Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 9},
	})
	if err != nil {
		t.Fatal(err)
	}
	errCh := make(chan error, 1)
	var got service.CreateObjectACK
	go func() {
		ot := bacnet.ObjectTypeAnalogValue
		var e error
		got, e = env.Client.CreateObject(context.Background(), env.Target, service.CreateObjectRequest{ObjectType: &ot})
		errCh <- e
	}()
	invokeID, choice := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	if choice != apdu.ServiceCreateObject {
		t.Fatalf("choice=%d", choice)
	}
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendComplexACK(nil, apdu.ComplexACK{
		InvokeID: invokeID, ServiceChoice: apdu.ServiceCreateObject, Payload: ackPayload,
	}))
	if err := <-errCh; err != nil || got.Object.Instance != 9 {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestDeleteObjectAndListElementSimpleACK(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go serveSimpleACK(ctx, env.PeerTr, env.Local)
	if err := env.Client.DeleteObject(ctx, env.Target, service.DeleteObjectRequest{
		Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 9},
	}); err != nil {
		t.Fatal(err)
	}
	if err := env.Client.AddListElement(ctx, env.Target, service.ListElementRequest{
		Object:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		Property: bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName},
		Elements: []bacnet.Element{{Value: bacnet.UnsignedValue(1)}},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAuditStreamAndQuery(t *testing.T) {
	env := newVirtualPair(t)
	stream := env.Client.OpenAuditStream(2)
	defer stream.Close()
	env.Client.deliverAuditNotification(service.AuditNotification{
		SourceDevice: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		Operation:    2,
	}, true)
	select {
	case ev := <-stream.Events():
		if ev.Notification.Operation != 2 || !ev.Confirmed {
			t.Fatalf("%+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
	ackPayload := []byte{}
	errCh := make(chan error, 1)
	go func() {
		_, e := env.Client.AuditLogQuery(context.Background(), env.Target, service.AuditLogQueryRequest{
			AuditLog: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAuditLog, Instance: 1},
		})
		errCh <- e
	}()
	invokeID, choice := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	if choice != apdu.ServiceAuditLogQuery {
		t.Fatalf("choice=%d", choice)
	}
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendComplexACK(nil, apdu.ComplexACK{
		InvokeID: invokeID, ServiceChoice: apdu.ServiceAuditLogQuery, Payload: ackPayload,
	}))
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
}

func TestLifeSafetySimpleACK(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go serveSimpleACK(ctx, env.PeerTr, env.Local)
	if err := env.Client.LifeSafetyOperation(ctx, env.Target, service.LifeSafetyOperationRequest{
		RequestingProcessIdentifier: 1, RequestingSource: "ops", Request: 1,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestVTOpenComplexACK(t *testing.T) {
	env := newVirtualPair(t)
	ackPayload, err := service.EncodeVTOpenACK(service.VTOpenACK{RemoteVTSessionIdentifier: 7})
	if err != nil {
		t.Fatal(err)
	}
	errCh := make(chan error, 1)
	var got service.VTOpenACK
	go func() {
		var e error
		got, e = env.Client.VTOpen(context.Background(), env.Target, service.VTOpenRequest{VTClass: 1, LocalVTSessionIdentifier: 1})
		errCh <- e
	}()
	invokeID, choice := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	if choice != apdu.ServiceVTOpen {
		t.Fatalf("choice=%d", choice)
	}
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendComplexACK(nil, apdu.ComplexACK{
		InvokeID: invokeID, ServiceChoice: apdu.ServiceVTOpen, Payload: ackPayload,
	}))
	if err := <-errCh; err != nil || got.RemoteVTSessionIdentifier != 7 {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestDefaultRetransmitPolicyAdvanced(t *testing.T) {
	if DefaultRetransmitPolicy(apdu.ServiceAtomicReadFile) != RetransmitEnabled {
		t.Fatal("read file")
	}
	if DefaultRetransmitPolicy(apdu.ServiceAtomicWriteFile) != RetransmitDisabled {
		t.Fatal("write file")
	}
	if DefaultRetransmitPolicy(apdu.ServiceCreateObject) != RetransmitDisabled {
		t.Fatal("create")
	}
}
