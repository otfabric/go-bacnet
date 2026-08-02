// SPDX-License-Identifier: MIT

package service_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestAtomicFileStreamRoundTrip(t *testing.T) {
	req := service.AtomicReadFileRequest{
		File:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 1},
		Access: service.FileAccessStream, StartPosition: 0, Count: 16,
	}
	raw, err := service.EncodeAtomicReadFile(req)
	if err != nil || len(raw) == 0 {
		t.Fatalf("%v %#v", err, raw)
	}
	ack := service.AtomicReadFileACK{
		EndOfFile: true, Access: service.FileAccessStream, StartPosition: 0, Data: []byte("hello"),
	}
	ackRaw, err := service.EncodeAtomicReadFileACK(ack)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeAtomicReadFileACK(ackRaw, bacnet.DefaultDecodeLimits())
	if err != nil || string(got.Data) != "hello" || !got.EndOfFile {
		t.Fatalf("%v %+v", err, got)
	}
	wreq := service.AtomicWriteFileRequest{
		File: req.File, Access: service.FileAccessStream, StartPosition: 0, Data: []byte("world"),
	}
	wraw, err := service.EncodeAtomicWriteFile(wreq)
	if err != nil {
		t.Fatal(err)
	}
	wackRaw, err := service.EncodeAtomicWriteFileACK(service.AtomicWriteFileACK{Access: service.FileAccessStream, StartPosition: 0})
	if err != nil {
		t.Fatal(err)
	}
	wack, err := service.DecodeAtomicWriteFileACK(wackRaw, bacnet.DefaultDecodeLimits())
	if err != nil || wack.StartPosition != 0 {
		t.Fatalf("%v %+v", err, wack)
	}
	_ = wraw
	pos, count, eof := service.FileChunkBounds(100, 90, 20)
	if pos != 90 || count != 10 || !eof {
		t.Fatalf("%d %d %v", pos, count, eof)
	}
}

func TestAtomicFileRecordRoundTrip(t *testing.T) {
	ack := service.AtomicReadFileACK{
		EndOfFile: false, Access: service.FileAccessRecord, StartPosition: 1,
		RecordCount: 2, Records: [][]byte{[]byte("r1"), []byte("r2")},
	}
	raw, err := service.EncodeAtomicReadFileACK(ack)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeAtomicReadFileACK(raw, bacnet.DefaultDecodeLimits())
	if err != nil || got.RecordCount != 2 || len(got.Records) != 2 {
		t.Fatalf("%v %+v", err, got)
	}
	wreq := service.AtomicWriteFileRequest{
		File:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 2},
		Access: service.FileAccessRecord, StartPosition: 0, Records: [][]byte{[]byte("a")},
	}
	if _, err := service.EncodeAtomicWriteFile(wreq); err != nil {
		t.Fatal(err)
	}
	wackRaw, err := service.EncodeAtomicWriteFileACK(service.AtomicWriteFileACK{Access: service.FileAccessRecord, StartPosition: 3})
	if err != nil {
		t.Fatal(err)
	}
	wack, err := service.DecodeAtomicWriteFileACK(wackRaw, bacnet.DefaultDecodeLimits())
	if err != nil || wack.Access != service.FileAccessRecord || wack.StartPosition != 3 {
		t.Fatalf("%v %+v", err, wack)
	}
}

func TestListCreateDeleteRoundTrip(t *testing.T) {
	req := service.ListElementRequest{
		Object:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		Property: bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName},
		Elements: []bacnet.Element{{Value: bacnet.UnsignedValue(1)}},
	}
	raw, err := service.EncodeListElementRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeListElementRequest(raw, bacnet.DefaultDecodeLimits())
	if err != nil || len(got.Elements) != 1 {
		t.Fatalf("%v %+v", err, got)
	}
	ot := bacnet.ObjectTypeAnalogValue
	craw, err := service.EncodeCreateObject(service.CreateObjectRequest{
		ObjectType: &ot,
		InitialValues: []service.PropertyValue{{
			Property: bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName},
			Value:    bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: "AV-9"}},
		}},
	})
	if err != nil || len(craw) == 0 {
		t.Fatalf("%v", err)
	}
	ackRaw, err := service.EncodeCreateObjectACK(service.CreateObjectACK{
		Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 9},
	})
	if err != nil {
		t.Fatal(err)
	}
	ack, err := service.DecodeCreateObjectACK(ackRaw, bacnet.DefaultDecodeLimits())
	if err != nil || ack.Object.Instance != 9 {
		t.Fatalf("%v %+v", err, ack)
	}
	draw, err := service.EncodeDeleteObject(service.DeleteObjectRequest(ack))
	if err != nil {
		t.Fatal(err)
	}
	dreq, err := service.DecodeDeleteObject(draw, bacnet.DefaultDecodeLimits())
	if err != nil || dreq.Object.Instance != 9 {
		t.Fatalf("%v %+v", err, dreq)
	}
	if _, err := service.EncodeListElementRequest(service.ListElementRequest{}); err == nil {
		t.Fatal("expected empty list error")
	}
}

func TestMessagingRoundTrip(t *testing.T) {
	p := service.PrivateTransfer{VendorID: 999, ServiceNumber: 7, ServiceParameters: []bacnet.Element{{Value: bacnet.UnsignedValue(1)}}}
	raw, err := service.EncodePrivateTransfer(p)
	if err != nil {
		t.Fatal(err)
	}
	pgot, err := service.DecodePrivateTransfer(raw, bacnet.DefaultDecodeLimits())
	if err != nil || pgot.VendorID != 999 || len(pgot.ServiceParameters) != 1 {
		t.Fatalf("%v %+v", err, pgot)
	}
	cls := uint32(3)
	tm := service.TextMessage{
		TextMessageSourceDevice: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		MessageClass:            &cls,
		MessagePriority:         1,
		Message:                 "hi",
	}
	traw, err := service.EncodeTextMessage(tm)
	if err != nil {
		t.Fatal(err)
	}
	tgot, err := service.DecodeTextMessage(traw, bacnet.DefaultDecodeLimits())
	if err != nil || tgot.Message != "hi" || tgot.MessageClass == nil {
		t.Fatalf("%v %+v", err, tgot)
	}
	ts := service.TimeSynchronization{Date: bacnet.Date{Year: 124, Month: 1, Day: 2}, Time: bacnet.Time{Hour: 3}}
	tsraw, err := service.EncodeTimeSynchronization(ts)
	if err != nil {
		t.Fatal(err)
	}
	tsgot, err := service.DecodeTimeSynchronization(tsraw, bacnet.DefaultDecodeLimits())
	if err != nil || tsgot.Date.Day != 2 {
		t.Fatalf("%v %+v", err, tsgot)
	}
	inh := true
	wg := service.WriteGroup{GroupNumber: 1, WritePriority: 8, ChangeList: []bacnet.Element{{Value: bacnet.UnsignedValue(1)}}, InhibitDelay: &inh}
	wraw, err := service.EncodeWriteGroup(wg)
	if err != nil {
		t.Fatal(err)
	}
	wgot, err := service.DecodeWriteGroup(wraw, bacnet.DefaultDecodeLimits())
	if err != nil || wgot.GroupNumber != 1 || wgot.InhibitDelay == nil || !*wgot.InhibitDelay {
		t.Fatalf("%v %+v", err, wgot)
	}
}

func TestAuditIdentityRoundTrip(t *testing.T) {
	td := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 2}
	n := service.AuditNotification{
		SourceDevice: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		TargetDevice: &td,
		Operation:    5,
	}
	raw, err := service.EncodeAuditNotification(n)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeAuditNotification(raw, bacnet.DefaultDecodeLimits())
	if err != nil || got.Operation != 5 || got.TargetDevice == nil {
		t.Fatalf("%v %+v", err, got)
	}
	qraw, err := service.EncodeAuditLogQuery(service.AuditLogQueryRequest{
		AuditLog: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAuditLog, Instance: 1},
		Query:    []bacnet.Element{{Value: bacnet.UnsignedValue(1)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ack, err := service.DecodeAuditLogQueryACK(qraw[0:0], bacnet.DefaultDecodeLimits()) // empty ACK
	if err != nil || len(ack.Records) != 0 {
		t.Fatalf("%v %+v", err, ack)
	}
	_ = qraw
	w := service.WhoAmI{VendorID: 1, ModelName: "m", SerialNumber: "s"}
	wraw, err := service.EncodeWhoAmI(w)
	if err != nil {
		t.Fatal(err)
	}
	wgot, err := service.DecodeWhoAmI(wraw, bacnet.DefaultDecodeLimits())
	if err != nil || wgot.ModelName != "m" {
		t.Fatalf("%v %+v", err, wgot)
	}
	y := service.YouAre{VendorID: 1, ModelName: "m", SerialNumber: "s", Device: td}
	yraw, err := service.EncodeYouAre(y)
	if err != nil {
		t.Fatal(err)
	}
	ygot, err := service.DecodeYouAre(yraw, bacnet.DefaultDecodeLimits())
	if err != nil || ygot.Device.Instance != 2 {
		t.Fatalf("%v %+v", err, ygot)
	}
	araw, err := service.EncodeAuthRequest(service.AuthRequest{Parameters: []bacnet.Element{{Value: bacnet.UnsignedValue(9)}}})
	if err != nil {
		t.Fatal(err)
	}
	agot, err := service.DecodeAuthRequest(araw, bacnet.DefaultDecodeLimits())
	if err != nil || len(agot.Parameters) != 1 {
		t.Fatalf("%v %+v", err, agot)
	}
}

func TestLifeSafetyVTRoundTrip(t *testing.T) {
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeLifeSafetyPoint, Instance: 1}
	req := service.LifeSafetyOperationRequest{
		RequestingProcessIdentifier: 1, RequestingSource: "ops", Request: 2, Object: &obj,
	}
	raw, err := service.EncodeLifeSafetyOperation(req)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeLifeSafetyOperation(raw, bacnet.DefaultDecodeLimits())
	if err != nil || got.Request != 2 || got.Object == nil {
		t.Fatalf("%v %+v", err, got)
	}
	vraw, err := service.EncodeVTOpen(service.VTOpenRequest{VTClass: 1, LocalVTSessionIdentifier: 3})
	if err != nil {
		t.Fatal(err)
	}
	_ = vraw
	ackRaw, err := service.EncodeVTOpenACK(service.VTOpenACK{RemoteVTSessionIdentifier: 4})
	if err != nil {
		t.Fatal(err)
	}
	ack, err := service.DecodeVTOpenACK(ackRaw, bacnet.DefaultDecodeLimits())
	if err != nil || ack.RemoteVTSessionIdentifier != 4 {
		t.Fatalf("%v %+v", err, ack)
	}
	craw, err := service.EncodeVTClose(service.VTCloseRequest{RemoteVTSessionIdentifiers: []uint8{4, 5}})
	if err != nil || len(craw) == 0 {
		t.Fatal(err)
	}
	draw, err := service.EncodeVTData(service.VTDataRequest{VTSessionIdentifier: 4, VTNewData: []byte{1, 2}, VTDataFlag: 1})
	if err != nil {
		t.Fatal(err)
	}
	dgot, err := service.DecodeVTData(draw, bacnet.DefaultDecodeLimits())
	if err != nil || dgot.VTDataFlag != 1 || len(dgot.VTNewData) != 2 {
		t.Fatalf("%v %+v", err, dgot)
	}
}
