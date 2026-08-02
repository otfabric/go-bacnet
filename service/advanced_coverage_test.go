// SPDX-License-Identifier: MIT

package service_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestPrivateTransferMinimalAndOverflow(t *testing.T) {
	raw, err := service.EncodePrivateTransfer(service.PrivateTransfer{VendorID: 1, ServiceNumber: 2})
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodePrivateTransfer(raw, bacnet.DefaultDecodeLimits())
	if err != nil || len(got.ServiceParameters) != 0 {
		t.Fatalf("%v %+v", err, got)
	}
	// vendor overflow via crafted payload
	bad, err := bacnet.AppendContextUnsigned(nil, 0, 0x10000)
	if err != nil {
		t.Fatal(err)
	}
	bad, err = bacnet.AppendContextUnsigned(bad, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodePrivateTransfer(bad, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected overflow")
	}
}

func TestTextMessageWithoutClass(t *testing.T) {
	raw, err := service.EncodeTextMessage(service.TextMessage{
		TextMessageSourceDevice: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		Message:                 "ok",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeTextMessage(raw, bacnet.DefaultDecodeLimits())
	if err != nil || got.Message != "ok" || got.MessageClass != nil {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestListElementWithArrayIndex(t *testing.T) {
	idx := uint32(2)
	raw, err := service.EncodeListElementRequest(service.ListElementRequest{
		Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		Property: bacnet.PropertyReference{
			Identifier: bacnet.PropertyObjectName,
			ArrayIndex: &idx,
		},
		Elements: []bacnet.Element{{Value: bacnet.UnsignedValue(9)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeListElementRequest(raw, bacnet.DefaultDecodeLimits())
	if err != nil || got.Property.ArrayIndex == nil || *got.Property.ArrayIndex != 2 {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestAuditNotificationOptionalObjects(t *testing.T) {
	srcObj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	tgtObj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 2}
	raw, err := bacnet.AppendContextObjectID(nil, 0, bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1})
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextObjectID(raw, 2, srcObj)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextObjectID(raw, 3, tgtObj)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextUnsigned(raw, 4, 7)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextUnsigned(raw, 9, 1) // remaining
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeAuditNotification(raw, bacnet.DefaultDecodeLimits())
	if err != nil || got.SourceObject == nil || got.TargetObject == nil || len(got.Remaining) != 1 {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestAuditLogQueryWithQuery(t *testing.T) {
	raw, err := service.EncodeAuditLogQuery(service.AuditLogQueryRequest{
		AuditLog: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAuditLog, Instance: 1},
	})
	if err != nil || len(raw) == 0 {
		t.Fatal(err)
	}
	ackRaw, err := bacnet.AppendApplicationValue(nil, bacnet.UnsignedValue(1))
	if err != nil {
		t.Fatal(err)
	}
	ack, err := service.DecodeAuditLogQueryACK(ackRaw, bacnet.DefaultDecodeLimits())
	if err != nil || len(ack.Records) != 1 {
		t.Fatalf("%v %+v", err, ack)
	}
}

func TestLifeSafetyWithoutObject(t *testing.T) {
	raw, err := service.EncodeLifeSafetyOperation(service.LifeSafetyOperationRequest{
		RequestingProcessIdentifier: 1, RequestingSource: "a", Request: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeLifeSafetyOperation(raw, bacnet.DefaultDecodeLimits())
	if err != nil || got.Object != nil {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestEncodeCreateObjectWithIdentifierAndIndex(t *testing.T) {
	id := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 55}
	idx := uint32(0)
	raw, err := service.EncodeCreateObject(service.CreateObjectRequest{
		ObjectIdentifier: &id,
		InitialValues: []service.PropertyValue{{
			Property: bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName, ArrayIndex: &idx},
			Value:    bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: "x"}},
		}},
	})
	if err != nil || len(raw) < 8 {
		t.Fatalf("%v len=%d", err, len(raw))
	}
}

func TestFileACKMissingEOF(t *testing.T) {
	body, err := bacnet.AppendApplicationValue(nil, bacnet.SignedValue(0))
	if err != nil {
		t.Fatal(err)
	}
	body, err = bacnet.AppendApplicationValue(body, bacnet.ApplicationValue{Kind: bacnet.ValueOctetString, OctetString: []byte{1}})
	if err != nil {
		t.Fatal(err)
	}
	els, n, err := bacnet.ParseSequence(body, bacnet.DefaultDecodeLimits(), -1)
	if err != nil || n != len(body) {
		t.Fatal(err)
	}
	raw, err := bacnet.AppendContextTagged(nil, 0, els)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeAtomicReadFileACK(raw, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected missing EOF")
	}
}
