// SPDX-License-Identifier: MIT

package service_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

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
	raw, err = bacnet.AppendContextUnsigned(raw, 9, 1)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeAuditNotification(raw, bacnet.DefaultDecodeLimits())
	if err != nil || got.SourceObject == nil || got.TargetObject == nil || len(got.Remaining) != 1 {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestAuditNotificationTargetDeviceRoundTrip(t *testing.T) {
	td := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 9}
	raw, err := service.EncodeAuditNotification(service.AuditNotification{
		SourceDevice: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		TargetDevice: &td,
		Operation:    3,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeAuditNotification(raw, bacnet.DefaultDecodeLimits())
	if err != nil || got.TargetDevice == nil || got.TargetDevice.Instance != 9 {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestAuditLogQueryRoundTrip(t *testing.T) {
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
