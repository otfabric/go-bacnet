// SPDX-License-Identifier: MIT

package service_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func FuzzDecodeAtomicReadFileACK(f *testing.F) {
	ack, _ := service.EncodeAtomicReadFileACK(service.AtomicReadFileACK{
		EndOfFile: true, Access: service.FileAccessStream, Data: []byte("x"),
	})
	f.Add(ack)
	f.Add([]byte{0x10})
	f.Fuzz(func(t *testing.T, payload []byte) {
		_, _ = service.DecodeAtomicReadFileACK(payload, bacnet.DefaultDecodeLimits())
	})
}

func FuzzDecodePrivateTransfer(f *testing.F) {
	raw, _ := service.EncodePrivateTransfer(service.PrivateTransfer{VendorID: 1, ServiceNumber: 2})
	f.Add(raw)
	f.Fuzz(func(t *testing.T, payload []byte) {
		_, _ = service.DecodePrivateTransfer(payload, bacnet.DefaultDecodeLimits())
	})
}

func FuzzDecodeAuditNotification(f *testing.F) {
	raw, _ := service.EncodeAuditNotification(service.AuditNotification{
		SourceDevice: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		Operation:    1,
	})
	f.Add(raw)
	f.Fuzz(func(t *testing.T, payload []byte) {
		_, _ = service.DecodeAuditNotification(payload, bacnet.DefaultDecodeLimits())
	})
}

func FuzzDecodeLifeSafetyOperation(f *testing.F) {
	raw, _ := service.EncodeLifeSafetyOperation(service.LifeSafetyOperationRequest{
		RequestingProcessIdentifier: 1, RequestingSource: "a", Request: 1,
	})
	f.Add(raw)
	f.Fuzz(func(t *testing.T, payload []byte) {
		_, _ = service.DecodeLifeSafetyOperation(payload, bacnet.DefaultDecodeLimits())
	})
}

func FuzzDecodeListElementRequest(f *testing.F) {
	raw, _ := service.EncodeListElementRequest(service.ListElementRequest{
		Object:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		Property: bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName},
		Elements: []bacnet.Element{{Value: bacnet.UnsignedValue(1)}},
	})
	f.Add(raw)
	f.Fuzz(func(t *testing.T, payload []byte) {
		_, _ = service.DecodeListElementRequest(payload, bacnet.DefaultDecodeLimits())
	})
}
