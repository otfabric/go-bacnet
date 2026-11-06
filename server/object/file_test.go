// SPDX-License-Identifier: MIT

package object

import (
	"bytes"
	"context"
	"errors"
	"math"
	"testing"

	"github.com/otfabric/go-bacnet"
)

func TestFileStream(t *testing.T) {
	f, err := NewFile(FileConfig{Instance: 1, Name: "F", FileType: "text", Data: []byte("hello"), MaxSize: 12, Description: "d"})
	if err != nil {
		t.Fatal(err)
	}
	if !f.StreamAccess() {
		t.Fatal("stream file reports record access")
	}
	if d, eof, err := f.ReadStream(1, 3); err != nil || string(d) != "ell" || eof {
		t.Fatalf("read: %q %v %v", d, eof, err)
	}
	if d, eof, err := f.ReadStream(5, 3); err != nil || len(d) != 0 || !eof {
		t.Fatalf("read at end: %q %v %v", d, eof, err)
	}
	for _, start := range []int32{-1, 6} {
		if _, _, err := f.ReadStream(start, 1); !errors.Is(err, ErrInvalidFileStartPosition) {
			t.Fatalf("read at %d: %v", start, err)
		}
	}
	if at, err := f.WriteStream(-1, []byte(" you")); err != nil || at != 5 || string(f.Data()) != "hello you" {
		t.Fatalf("append: %d %v %q", at, err, f.Data())
	}
	if at, err := f.WriteStream(0, []byte("J")); err != nil || at != 0 || string(f.Data()) != "Jello you" {
		t.Fatalf("overwrite: %d %v %q", at, err, f.Data())
	}
	if _, err := f.WriteStream(7, []byte("rselfs")); !errors.Is(err, ErrFileFull) {
		t.Fatalf("full: %v", err)
	}
	if at, err := f.WriteStream(8, []byte("ur")); err != nil || at != 8 || string(f.Data()) != "Jello your" {
		t.Fatalf("extend: %v %q", err, f.Data())
	}
	if _, err := f.WriteStream(-2, []byte("x")); !errors.Is(err, ErrInvalidFileStartPosition) {
		t.Fatalf("write at -2: %v", err)
	}
	// Past the end the file grows with zero octets, as bacnet-stack's
	// storage does; within MaxSize only.
	if at, err := f.WriteStream(11, []byte("x")); err != nil || at != 11 || string(f.Data()) != "Jello your\x00x" {
		t.Fatalf("write past the end: %d %v %q", at, err, f.Data())
	}
	if _, err := f.WriteStream(12, []byte("x")); !errors.Is(err, ErrFileFull) {
		t.Fatalf("write past the end and the maximum: %v", err)
	}
	if _, _, err := f.ReadRecords(0, 1); !errors.Is(err, ErrInvalidFileAccessMethod) {
		t.Fatal(err)
	}
	if _, err := f.WriteRecords(0, nil); !errors.Is(err, ErrInvalidFileAccessMethod) {
		t.Fatal(err)
	}
	ctx := context.Background()
	if v, err := f.ReadProperty(ctx, bacnet.PropertyReference{Identifier: bacnet.PropertyFileSize}); err != nil || v.Unsigned != 12 {
		t.Fatalf("File_Size: %v %+v", err, v)
	}
	if v, err := f.ReadProperty(ctx, bacnet.PropertyReference{Identifier: bacnet.PropertyFileAccessMethod}); err != nil || v.Enumerated != FileStreamAccess {
		t.Fatalf("File_Access_Method: %v %+v", err, v)
	}
	if v, err := f.ReadProperty(ctx, bacnet.PropertyReference{Identifier: bacnet.PropertyModificationDate}); err != nil || len(v.Elements) != 2 {
		t.Fatalf("Modification_Date: %v %+v", err, v)
	}
	archive := func(v bacnet.ApplicationValue, idx *uint32) error {
		return f.WriteProperty(ctx, Write{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyArchive, ArrayIndex: idx}, Value: v})
	}
	if err := archive(bacnet.BoolValue(true), nil); err != nil {
		t.Fatal(err)
	}
	if v, _ := f.ReadProperty(ctx, bacnet.PropertyReference{Identifier: bacnet.PropertyArchive}); !v.Boolean {
		t.Fatal("archive not set")
	}
	if err := archive(bacnet.RealValue(1), nil); !errors.Is(err, ErrInvalidDataType) {
		t.Fatal(err)
	}
	one := uint32(1)
	if err := archive(bacnet.BoolValue(true), &one); err == nil {
		t.Fatal("archive with index accepted")
	}
}

func TestFileRecords(t *testing.T) {
	f, err := NewFile(FileConfig{Instance: 2, Name: "R", Record: true, Records: [][]byte{[]byte("a"), []byte("bb")}, MaxSize: 4})
	if err != nil {
		t.Fatal(err)
	}
	if r, eof, err := f.ReadRecords(1, 5); err != nil || len(r) != 1 || !bytes.Equal(r[0], []byte("bb")) || !eof {
		t.Fatalf("read: %q %v %v", r, eof, err)
	}
	if r, eof, err := f.ReadRecords(0, 1); err != nil || len(r) != 1 || eof {
		t.Fatalf("read first: %q %v %v", r, eof, err)
	}
	if _, _, err := f.ReadRecords(3, 1); !errors.Is(err, ErrInvalidFileStartPosition) {
		t.Fatal(err)
	}
	if at, err := f.WriteRecords(-1, [][]byte{[]byte("c")}); err != nil || at != 2 || len(f.Records()) != 3 {
		t.Fatalf("append: %d %v", at, err)
	}
	if at, err := f.WriteRecords(2, [][]byte{[]byte("C"), []byte("d")}); err != nil || at != 2 || string(f.Records()[2]) != "C" || len(f.Records()) != 4 {
		t.Fatalf("replace: %d %v %q", at, err, f.Records())
	}
	if _, err := f.WriteRecords(-1, [][]byte{[]byte("e")}); !errors.Is(err, ErrFileFull) {
		t.Fatal(err)
	}
	if _, err := f.WriteRecords(-2, nil); !errors.Is(err, ErrInvalidFileStartPosition) {
		t.Fatal(err)
	}
	// Past the end the records are appended after the last one.
	if at, err := f.WriteRecords(9, nil); err != nil || at != 4 || len(f.Records()) != 4 {
		t.Fatalf("write past the end: %d %v", at, err)
	}
	if _, err := f.WriteRecords(9, [][]byte{[]byte("f")}); !errors.Is(err, ErrFileFull) {
		t.Fatal(err)
	}
	if _, _, err := f.ReadStream(0, 1); !errors.Is(err, ErrInvalidFileAccessMethod) {
		t.Fatal(err)
	}
	if _, err := f.WriteStream(0, nil); !errors.Is(err, ErrInvalidFileAccessMethod) {
		t.Fatal(err)
	}
	ctx := context.Background()
	if v, _ := f.ReadProperty(ctx, bacnet.PropertyReference{Identifier: bacnet.PropertyRecordCount}); v.Unsigned != 4 {
		t.Fatalf("Record_Count %+v", v)
	}
	if v, _ := f.ReadProperty(ctx, bacnet.PropertyReference{Identifier: bacnet.PropertyFileSize}); v.Unsigned != 5 {
		t.Fatalf("File_Size %+v", v)
	}
	if v, _ := f.ReadProperty(ctx, bacnet.PropertyReference{Identifier: bacnet.PropertyFileAccessMethod}); v.Enumerated != FileRecordAccess {
		t.Fatalf("File_Access_Method %+v", v)
	}
}

func TestFileReadOnlyAndConfig(t *testing.T) {
	ro, _ := NewFile(FileConfig{Instance: 3, Name: "RO", ReadOnly: true})
	if _, err := ro.WriteStream(0, []byte("x")); !errors.Is(err, ErrFileAccessDenied) {
		t.Fatal(err)
	}
	rr, _ := NewFile(FileConfig{Instance: 4, Name: "RR", ReadOnly: true, Record: true})
	if _, err := rr.WriteRecords(0, nil); !errors.Is(err, ErrFileAccessDenied) {
		t.Fatal(err)
	}
	for _, cfg := range []FileConfig{{}, {Name: "x", Instance: bacnet.MaxObjectInstance}, {Name: "x", MaxSize: 1, Data: []byte("ab")}, {Name: "x", Record: true, MaxSize: 1, Records: [][]byte{{1}, {2}}}, {Name: "x", MaxSize: math.MaxInt32 + 1}} {
		if _, err := NewFile(cfg); err == nil {
			t.Errorf("accepted %+v", cfg)
		}
	}
}
