// SPDX-License-Identifier: MIT

package service_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func mustAppendJunk(t *testing.T, raw []byte) []byte {
	t.Helper()
	return append(append([]byte{}, raw...), 0xff)
}

func TestCoverageBoostTrailingEverywhere(t *testing.T) {
	id := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
	cases := []struct {
		name string
		raw  []byte
		fn   func([]byte) error
	}{
		{"private", mustEncode(t, func() ([]byte, error) {
			return service.EncodePrivateTransfer(service.PrivateTransfer{VendorID: 1, ServiceNumber: 1})
		}), func(b []byte) error { _, e := service.DecodePrivateTransfer(b, bacnet.DefaultDecodeLimits()); return e }},
		{"text", mustEncode(t, func() ([]byte, error) {
			return service.EncodeTextMessage(service.TextMessage{TextMessageSourceDevice: id, Message: "m"})
		}), func(b []byte) error { _, e := service.DecodeTextMessage(b, bacnet.DefaultDecodeLimits()); return e }},
		{"time", mustEncode(t, func() ([]byte, error) {
			return service.EncodeTimeSynchronization(service.TimeSynchronization{})
		}), func(b []byte) error {
			_, e := service.DecodeTimeSynchronization(b, bacnet.DefaultDecodeLimits())
			return e
		}},
		{"wg", mustEncode(t, func() ([]byte, error) {
			return service.EncodeWriteGroup(service.WriteGroup{GroupNumber: 1, WritePriority: 1, ChangeList: []bacnet.Element{{Value: bacnet.UnsignedValue(1)}}})
		}), func(b []byte) error { _, e := service.DecodeWriteGroup(b, bacnet.DefaultDecodeLimits()); return e }},
		{"list", mustEncode(t, func() ([]byte, error) {
			return service.EncodeListElementRequest(service.ListElementRequest{
				Object: id, Property: bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName},
				Elements: []bacnet.Element{{Value: bacnet.UnsignedValue(1)}},
			})
		}), func(b []byte) error {
			_, e := service.DecodeListElementRequest(b, bacnet.DefaultDecodeLimits())
			return e
		}},
		{"delete", mustEncode(t, func() ([]byte, error) {
			return service.EncodeDeleteObject(service.DeleteObjectRequest{Object: id})
		}), func(b []byte) error { _, e := service.DecodeDeleteObject(b, bacnet.DefaultDecodeLimits()); return e }},
		{"who", mustEncode(t, func() ([]byte, error) { return service.EncodeWhoAmI(service.WhoAmI{VendorID: 1}) }),
			func(b []byte) error { _, e := service.DecodeWhoAmI(b, bacnet.DefaultDecodeLimits()); return e }},
		{"you", mustEncode(t, func() ([]byte, error) {
			return service.EncodeYouAre(service.YouAre{VendorID: 1, Device: id})
		}), func(b []byte) error { _, e := service.DecodeYouAre(b, bacnet.DefaultDecodeLimits()); return e }},
		{"audit", mustEncode(t, func() ([]byte, error) {
			return service.EncodeAuditNotification(service.AuditNotification{SourceDevice: id, Operation: 1})
		}), func(b []byte) error {
			_, e := service.DecodeAuditNotification(b, bacnet.DefaultDecodeLimits())
			return e
		}},
		{"life", mustEncode(t, func() ([]byte, error) {
			return service.EncodeLifeSafetyOperation(service.LifeSafetyOperationRequest{RequestingProcessIdentifier: 1, RequestingSource: "a", Request: 1})
		}), func(b []byte) error {
			_, e := service.DecodeLifeSafetyOperation(b, bacnet.DefaultDecodeLimits())
			return e
		}},
		{"vt", mustEncode(t, func() ([]byte, error) {
			return service.EncodeVTData(service.VTDataRequest{VTSessionIdentifier: 1, VTNewData: []byte{1}})
		}), func(b []byte) error { _, e := service.DecodeVTData(b, bacnet.DefaultDecodeLimits()); return e }},
		{"file", mustEncode(t, func() ([]byte, error) {
			return service.EncodeAtomicReadFileACK(service.AtomicReadFileACK{EndOfFile: true, Access: service.FileAccessStream, Data: []byte{1}})
		}), func(b []byte) error {
			_, e := service.DecodeAtomicReadFileACK(b, bacnet.DefaultDecodeLimits())
			return e
		}},
		{"wfile", mustEncode(t, func() ([]byte, error) {
			return service.EncodeAtomicWriteFileACK(service.AtomicWriteFileACK{Access: service.FileAccessStream})
		}), func(b []byte) error {
			_, e := service.DecodeAtomicWriteFileACK(b, bacnet.DefaultDecodeLimits())
			return e
		}},
		{"aq", []byte{0x21, 0x01}, func(b []byte) error {
			_, e := service.DecodeAuditLogQueryACK(mustAppendJunk(t, b), bacnet.DefaultDecodeLimits())
			return e
		}},
	}
	for _, tc := range cases {
		if err := tc.fn(mustAppendJunk(t, tc.raw)); err == nil {
			t.Fatalf("%s trailing", tc.name)
		}
	}
}

func mustEncode(t *testing.T, fn func() ([]byte, error)) []byte {
	t.Helper()
	raw, err := fn()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestCoverageBoostEncodeBadObjectIDs(t *testing.T) {
	bad := bacnet.ObjectIdentifier{Type: 0xFFFF, Instance: 1}
	if _, err := service.EncodePrivateTransfer(service.PrivateTransfer{VendorID: 1, ServiceNumber: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.EncodeTextMessage(service.TextMessage{TextMessageSourceDevice: bad, Message: "x"}); err == nil {
		t.Fatal("text")
	}
	if _, err := service.EncodeListElementRequest(service.ListElementRequest{
		Object: bad, Property: bacnet.PropertyReference{Identifier: 1}, Elements: []bacnet.Element{{Value: bacnet.UnsignedValue(1)}},
	}); err == nil {
		t.Fatal("list")
	}
	if _, err := service.EncodeAtomicReadFile(service.AtomicReadFileRequest{File: bad, Access: service.FileAccessStream, Count: 1}); err == nil {
		t.Fatal("arf")
	}
	if _, err := service.EncodeAtomicWriteFile(service.AtomicWriteFileRequest{File: bad, Access: service.FileAccessStream, Data: []byte{1}}); err == nil {
		t.Fatal("awf")
	}
	if _, err := service.EncodeWhoAmI(service.WhoAmI{}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.EncodeYouAre(service.YouAre{Device: bad}); err == nil {
		t.Fatal("you")
	}
	if _, err := service.EncodeAuditNotification(service.AuditNotification{SourceDevice: bad, Operation: 1}); err == nil {
		t.Fatal("audit")
	}
	if _, err := service.EncodeLifeSafetyOperation(service.LifeSafetyOperationRequest{
		RequestingProcessIdentifier: 1, RequestingSource: "a", Request: 1, Object: &bad,
	}); err == nil {
		t.Fatal("life")
	}
	if _, err := service.EncodeWriteGroup(service.WriteGroup{GroupNumber: 1, WritePriority: 1, ChangeList: []bacnet.Element{{Value: bacnet.UnsignedValue(1)}}, InhibitDelay: boolPtr(true)}); err != nil {
		t.Fatal(err)
	}
}

func boolPtr(b bool) *bool { return &b }
