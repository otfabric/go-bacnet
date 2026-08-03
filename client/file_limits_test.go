// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/service"
)

func TestReadFileStreamMaxTotal(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	file := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 1}
	go serveComplexACK(ctx, env.PeerTr, env.Local, func(choice uint8) ([]byte, error) {
		return service.EncodeAtomicReadFileACK(service.AtomicReadFileACK{
			EndOfFile: false, Access: service.FileAccessStream, Data: []byte("abcdefgh"),
		})
	})
	_, err := env.Client.ReadFileStream(ctx, env.Target, file, FileReadOptions{ChunkSize: 8, MaxTotal: 8})
	if !errors.Is(err, bacnet.ErrLimitExceeded) {
		t.Fatalf("got %v", err)
	}
}

func TestReadFileRecordsMaxTotal(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	file := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 1}
	go serveComplexACK(ctx, env.PeerTr, env.Local, func(choice uint8) ([]byte, error) {
		return service.EncodeAtomicReadFileACK(service.AtomicReadFileACK{
			EndOfFile: false, Access: service.FileAccessRecord,
			Records: [][]byte{[]byte("r")}, RecordCount: 1, StartPosition: 0,
		})
	})
	_, err := env.Client.ReadFileRecords(ctx, env.Target, file, 0, FileReadOptions{ChunkSize: 1, MaxTotal: 1})
	if !errors.Is(err, bacnet.ErrLimitExceeded) {
		t.Fatalf("got %v", err)
	}
}

func TestReadFileAccessMismatch(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	file := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 1}
	go serveComplexACK(ctx, env.PeerTr, env.Local, func(choice uint8) ([]byte, error) {
		return service.EncodeAtomicReadFileACK(service.AtomicReadFileACK{
			EndOfFile: true, Access: service.FileAccessRecord, Records: [][]byte{[]byte("x")}, RecordCount: 1,
		})
	})
	_, err := env.Client.ReadFileStream(ctx, env.Target, file, FileReadOptions{})
	if !errors.Is(err, bacnet.ErrProtocolViolation) {
		t.Fatalf("got %v", err)
	}

	env2 := newVirtualPair(t)
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	go serveComplexACK(ctx2, env2.PeerTr, env2.Local, func(choice uint8) ([]byte, error) {
		return service.EncodeAtomicReadFileACK(service.AtomicReadFileACK{
			EndOfFile: true, Access: service.FileAccessStream, Data: []byte("x"),
		})
	})
	_, err = env2.Client.ReadFileRecords(ctx2, env2.Target, file, 0, FileReadOptions{})
	if !errors.Is(err, bacnet.ErrProtocolViolation) {
		t.Fatalf("records mismatch: %v", err)
	}
	_ = apdu.ServiceAtomicReadFile
}

func TestFileHelperRequestErrors(t *testing.T) {
	env := newVirtualPair(t)
	_ = env.Client.Close()
	file := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 1}
	if _, err := env.Client.ReadFileStream(context.Background(), env.Target, file, FileReadOptions{}); err == nil {
		t.Fatal("stream closed")
	}
	if _, err := env.Client.ReadFileRecords(context.Background(), env.Target, file, 0, FileReadOptions{}); err == nil {
		t.Fatal("records closed")
	}
	if _, err := env.Client.WriteFileStream(context.Background(), env.Target, file, 0, []byte("x"), FileWriteOptions{}); err == nil {
		t.Fatal("write closed")
	}
}
