// SPDX-License-Identifier: MIT

package service_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestAtomicFileDecodeErrors(t *testing.T) {
	if _, err := service.DecodeAtomicReadFileACK([]byte{0x21, 0x01}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.DecodeAtomicWriteFileACK(nil, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.DecodeAtomicWriteFileACK([]byte{0x21, 0x01}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected error")
	}
}

func TestListObjectErrors(t *testing.T) {
	if _, err := service.EncodeCreateObject(service.CreateObjectRequest{}); err == nil {
		t.Fatal("expected specifier error")
	}
	id := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	raw, err := service.EncodeCreateObject(service.CreateObjectRequest{ObjectIdentifier: &id})
	if err != nil || len(raw) == 0 {
		t.Fatal(err)
	}
	if _, err := service.DecodeListElementRequest([]byte{0x21, 0x01}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.DecodeDeleteObject([]byte{0x21, 0x01}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.DecodeCreateObjectACK([]byte{0x21, 0x01}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected error")
	}
}

func TestMessagingDecodeErrors(t *testing.T) {
	if _, err := service.DecodePrivateTransfer(nil, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.DecodeTextMessage([]byte{0x21, 0x01}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.DecodeTimeSynchronization([]byte{0x21, 0x01}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.EncodeWriteGroup(service.WriteGroup{GroupNumber: 1, WritePriority: 1}); err == nil {
		t.Fatal("expected change list")
	}
	if _, err := service.DecodeWriteGroup([]byte{0x21, 0x01}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected error")
	}
}

func TestAuditLifeSafetyErrors(t *testing.T) {
	if _, err := service.DecodeAuditNotification([]byte{0x21, 0x01}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.DecodeLifeSafetyOperation(nil, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.EncodeVTClose(service.VTCloseRequest{}); err == nil {
		t.Fatal("expected sessions")
	}
	if _, err := service.DecodeVTData([]byte{0x21, 0x01}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected error")
	}
	if _, err := service.DecodeVTOpenACK([]byte{0x21, 0x01, 0x21, 0x02}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected length")
	}
	empty, err := service.EncodeAuthRequest(service.AuthRequest{})
	if err != nil || empty != nil {
		t.Fatalf("%v %v", err, empty)
	}
	if _, err := service.DecodeWhoAmI([]byte{0xff}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected error")
	}
}

func TestAtomicReadFileRecordEncode(t *testing.T) {
	raw, err := service.EncodeAtomicReadFile(service.AtomicReadFileRequest{
		File:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 2},
		Access: service.FileAccessRecord, StartPosition: 1, Count: 3,
	})
	if err != nil || len(raw) < 8 {
		t.Fatalf("%v len=%d", err, len(raw))
	}
	pos, count, eof := service.FileChunkBounds(10, -1, 0)
	if pos != 0 || count != 10 || !eof {
		t.Fatalf("%d %d %v", pos, count, eof)
	}
	pos, count, eof = service.FileChunkBounds(10, 10, 5)
	if count != 0 || !eof {
		t.Fatalf("%d %d %v", pos, count, eof)
	}
}
