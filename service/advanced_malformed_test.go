// SPDX-License-Identifier: MIT

package service_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestAdvancedTrailingAndMalformed(t *testing.T) {
	// Trailing data on happy payloads.
	checks := []struct {
		name string
		raw  []byte
		fn   func([]byte) error
	}{}
	add := func(name string, raw []byte, fn func([]byte) error) {
		checks = append(checks, struct {
			name string
			raw  []byte
			fn   func([]byte) error
		}{name, append(append([]byte{}, raw...), 0x00), fn})
	}

	p, _ := service.EncodePrivateTransfer(service.PrivateTransfer{VendorID: 1, ServiceNumber: 1})
	add("private", p, func(b []byte) error {
		_, err := service.DecodePrivateTransfer(b, bacnet.DefaultDecodeLimits())
		return err
	})
	tm, _ := service.EncodeTextMessage(service.TextMessage{
		TextMessageSourceDevice: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}, Message: "a",
	})
	add("text", tm, func(b []byte) error { _, err := service.DecodeTextMessage(b, bacnet.DefaultDecodeLimits()); return err })
	ts, _ := service.EncodeTimeSynchronization(service.TimeSynchronization{Date: bacnet.Date{Year: 1}, Time: bacnet.Time{}})
	add("time", ts, func(b []byte) error {
		_, err := service.DecodeTimeSynchronization(b, bacnet.DefaultDecodeLimits())
		return err
	})
	wg, _ := service.EncodeWriteGroup(service.WriteGroup{GroupNumber: 1, WritePriority: 1, ChangeList: []bacnet.Element{{Value: bacnet.UnsignedValue(1)}}})
	add("write-group", wg, func(b []byte) error { _, err := service.DecodeWriteGroup(b, bacnet.DefaultDecodeLimits()); return err })
	le, _ := service.EncodeListElementRequest(service.ListElementRequest{
		Object:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		Property: bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName},
		Elements: []bacnet.Element{{Value: bacnet.UnsignedValue(1)}},
	})
	add("list", le, func(b []byte) error {
		_, err := service.DecodeListElementRequest(b, bacnet.DefaultDecodeLimits())
		return err
	})
	ack, _ := service.EncodeAtomicReadFileACK(service.AtomicReadFileACK{EndOfFile: true, Access: service.FileAccessStream, Data: []byte{1}})
	add("file-ack", ack, func(b []byte) error {
		_, err := service.DecodeAtomicReadFileACK(b, bacnet.DefaultDecodeLimits())
		return err
	})
	wack, _ := service.EncodeAtomicWriteFileACK(service.AtomicWriteFileACK{Access: service.FileAccessStream, StartPosition: 0})
	add("write-ack", wack, func(b []byte) error {
		_, err := service.DecodeAtomicWriteFileACK(b, bacnet.DefaultDecodeLimits())
		return err
	})
	ls, _ := service.EncodeLifeSafetyOperation(service.LifeSafetyOperationRequest{RequestingProcessIdentifier: 1, RequestingSource: "a", Request: 1})
	add("life", ls, func(b []byte) error {
		_, err := service.DecodeLifeSafetyOperation(b, bacnet.DefaultDecodeLimits())
		return err
	})
	vd, _ := service.EncodeVTData(service.VTDataRequest{VTSessionIdentifier: 1, VTNewData: []byte{1}, VTDataFlag: 0})
	add("vt-data", vd, func(b []byte) error { _, err := service.DecodeVTData(b, bacnet.DefaultDecodeLimits()); return err })
	del, _ := service.EncodeDeleteObject(service.DeleteObjectRequest{Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}})
	add("delete", del, func(b []byte) error {
		_, err := service.DecodeDeleteObject(b, bacnet.DefaultDecodeLimits())
		return err
	})
	cack, _ := service.EncodeCreateObjectACK(service.CreateObjectACK{Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}})
	add("create-ack", cack, func(b []byte) error {
		_, err := service.DecodeCreateObjectACK(b, bacnet.DefaultDecodeLimits())
		return err
	})
	an, _ := service.EncodeAuditNotification(service.AuditNotification{
		SourceDevice: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}, Operation: 1,
	})
	add("audit", an, func(b []byte) error {
		_, err := service.DecodeAuditNotification(b, bacnet.DefaultDecodeLimits())
		return err
	})
	who, _ := service.EncodeWhoAmI(service.WhoAmI{VendorID: 1, ModelName: "m", SerialNumber: "s"})
	add("who", who, func(b []byte) error { _, err := service.DecodeWhoAmI(b, bacnet.DefaultDecodeLimits()); return err })
	you, _ := service.EncodeYouAre(service.YouAre{
		VendorID: 1, ModelName: "m", SerialNumber: "s",
		Device: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
	})
	add("you", you, func(b []byte) error { _, err := service.DecodeYouAre(b, bacnet.DefaultDecodeLimits()); return err })
	vto, _ := service.EncodeVTOpenACK(service.VTOpenACK{RemoteVTSessionIdentifier: 1})
	add("vt-open-ack", vto, func(b []byte) error { _, err := service.DecodeVTOpenACK(b, bacnet.DefaultDecodeLimits()); return err })

	for _, tc := range checks {
		if err := tc.fn(tc.raw); err == nil {
			t.Fatalf("%s: expected trailing error", tc.name)
		}
	}
	if _, err := service.DecodeAuthRequest([]byte{0xff}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("auth malformed")
	}
}

func TestFileStreamRecordFieldErrors(t *testing.T) {
	// stream result wrong kinds
	body, _ := bacnet.AppendApplicationValue(nil, bacnet.UnsignedValue(1))
	body, _ = bacnet.AppendApplicationValue(body, bacnet.UnsignedValue(1))
	els, n, err := bacnet.ParseSequence(body, bacnet.DefaultDecodeLimits(), -1)
	if err != nil || n != len(body) {
		t.Fatal(err)
	}
	raw, _ := bacnet.AppendApplicationValue(nil, bacnet.BoolValue(false))
	raw, _ = bacnet.AppendContextTagged(raw, 0, els)
	if _, err := service.DecodeAtomicReadFileACK(raw, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("stream field error")
	}
	// record missing fields
	body2, _ := bacnet.AppendApplicationValue(nil, bacnet.SignedValue(0))
	els2, n, err := bacnet.ParseSequence(body2, bacnet.DefaultDecodeLimits(), -1)
	if err != nil || n != len(body2) {
		t.Fatal(err)
	}
	raw2, _ := bacnet.AppendApplicationValue(nil, bacnet.BoolValue(true))
	raw2, _ = bacnet.AppendContextTagged(raw2, 1, els2)
	if _, err := service.DecodeAtomicReadFileACK(raw2, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("record field error")
	}
}
