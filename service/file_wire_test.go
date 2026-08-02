// SPDX-License-Identifier: MIT

package service_test

import (
	"encoding/hex"
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestAtomicReadFileWireShape(t *testing.T) {
	req := service.AtomicReadFileRequest{
		File:          bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 1},
		Access:        service.FileAccessStream,
		StartPosition: 0,
		Count:         16,
	}
	raw, err := service.EncodeAtomicReadFile(req)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := hex.DecodeString("c4028000010e310021100f")
	if string(raw) != string(want) {
		t.Fatalf("got %x want %x", raw, want)
	}
	got, err := service.DecodeAtomicReadFile(raw, bacnet.DefaultDecodeLimits())
	if err != nil || got != req {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestAtomicWriteFileRecordWireShape(t *testing.T) {
	req := service.AtomicWriteFileRequest{
		File:          bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 2},
		Access:        service.FileAccessRecord,
		StartPosition: 0,
		Records:       [][]byte{[]byte("a"), []byte("b")},
	}
	raw, err := service.EncodeAtomicWriteFile(req)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := hex.DecodeString("c4028000021e31002102616161621f")
	if string(raw) != string(want) {
		t.Fatalf("got %x want %x", raw, want)
	}
	got, err := service.DecodeAtomicWriteFile(raw, bacnet.DefaultDecodeLimits())
	if err != nil || got.Access != service.FileAccessRecord || len(got.Records) != 2 {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestAtomicReadFileRejectsPre023ContextFileID(t *testing.T) {
	// v0.2.2 incorrectly emitted context-tagged fileIdentifier + CHOICE tag 1.
	old, _ := hex.DecodeString("0c028000011e310021101f")
	_, err := service.DecodeAtomicReadFile(old, bacnet.DefaultDecodeLimits())
	if !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("got %v", err)
	}
}

func TestAtomicReadFileRejectsDuplicatesAndMissing(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	good, err := service.EncodeAtomicReadFile(service.AtomicReadFileRequest{
		File:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 1},
		Access: service.FileAccessStream, Count: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Duplicate fileIdentifier.
	dupFile := append(append([]byte{}, good[:5]...), good...)
	if _, err := service.DecodeAtomicReadFile(dupFile, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("dup file: %v", err)
	}
	// Duplicate access CHOICE (append second stream CHOICE).
	streamTail := good[5:]
	dupAccess := append(append([]byte{}, good...), streamTail...)
	if _, err := service.DecodeAtomicReadFile(dupAccess, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("dup access: %v", err)
	}
	// File only.
	if _, err := service.DecodeAtomicReadFile(good[:5], limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("missing access: %v", err)
	}
}

func TestAtomicWriteFileRejectsRecordCountMismatch(t *testing.T) {
	// start=0, recordCount=2, but only one octet-string record.
	raw, _ := hex.DecodeString("c4028000021e3100210261611f")
	_, err := service.DecodeAtomicWriteFile(raw, bacnet.DefaultDecodeLimits())
	if !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("got %v", err)
	}
}

func TestAtomicReadFileACKMalformedStream(t *testing.T) {
	// EOF true + stream CHOICE with wrong inner shape.
	raw, _ := hex.DecodeString("110e31000f") // bool true + stream CHOICE missing fileData
	_, err := service.DecodeAtomicReadFileACK(raw, bacnet.DefaultDecodeLimits())
	if !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("got %v", err)
	}
}

func TestAtomicWriteFileACKMalformed(t *testing.T) {
	_, err := service.DecodeAtomicWriteFileACK([]byte{0x21, 0x01}, bacnet.DefaultDecodeLimits())
	if !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("got %v", err)
	}
}

func TestAtomicWriteFileRejectsDuplicatesAndBadParams(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	good, err := service.EncodeAtomicWriteFile(service.AtomicWriteFileRequest{
		File:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 1},
		Access: service.FileAccessStream, Data: []byte{9},
	})
	if err != nil {
		t.Fatal(err)
	}
	dupFile := append(append([]byte{}, good[:5]...), good...)
	if _, err := service.DecodeAtomicWriteFile(dupFile, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("dup file: %v", err)
	}
	streamTail := good[5:]
	dupAccess := append(append([]byte{}, good...), streamTail...)
	if _, err := service.DecodeAtomicWriteFile(dupAccess, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("dup access: %v", err)
	}
	// Stream CHOICE with only start position.
	if _, err := service.DecodeAtomicWriteFile(append(good[:5], 0x0e, 0x31, 0x00, 0x0f), limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("short stream: %v", err)
	}
	// Record CHOICE with bad recordCount type (boolean instead of unsigned).
	badRec, _ := hex.DecodeString("c4028000021e3100111f")
	if _, err := service.DecodeAtomicWriteFile(badRec, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("bad recordCount: %v", err)
	}
}

func TestAtomicReadFileAccessParamErrors(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	// Stream CHOICE with three inners.
	raw, _ := hex.DecodeString("c4028000010e3100211021100f")
	if _, err := service.DecodeAtomicReadFile(raw, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("extra params: %v", err)
	}
	// Stream CHOICE with unsigned start (must be signed).
	raw, _ = hex.DecodeString("c4028000010e211021100f")
	if _, err := service.DecodeAtomicReadFile(raw, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("bad start: %v", err)
	}
}

func TestAtomicFileTrailingAndWriteACKChoice(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	good, err := service.EncodeAtomicReadFile(service.AtomicReadFileRequest{
		File:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 1},
		Access: service.FileAccessRecord, Count: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeAtomicReadFile(append(good, 0x00), limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("extra null: %v", err)
	}
	wack, err := service.EncodeAtomicWriteFileACK(service.AtomicWriteFileACK{
		Access: service.FileAccessRecord, StartPosition: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeAtomicWriteFileACK(wack, limits)
	if err != nil || got.Access != service.FileAccessRecord || got.StartPosition != 3 {
		t.Fatalf("%v %+v", err, got)
	}
	// Unknown ACK CHOICE tag 2 (constructed) and old constructed stream form.
	if _, err := service.DecodeAtomicWriteFileACK([]byte{0x2e, 0x31, 0x00, 0x2f}, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("bad choice: %v", err)
	}
	if _, err := service.DecodeAtomicWriteFileACK([]byte{0x0e, 0x31, 0x05, 0x0f}, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("legacy constructed ACK: %v", err)
	}
	// BACnet4J 6.1.0 / bacnet-stack context-primitive stream ACK (position 0).
	got, err = service.DecodeAtomicWriteFileACK([]byte{0x09, 0x00}, limits)
	if err != nil || got.Access != service.FileAccessStream || got.StartPosition != 0 {
		t.Fatalf("peer stream ACK: %v %+v", err, got)
	}
}
