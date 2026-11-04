// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/internal/virtual"
	"github.com/otfabric/go-bacnet/npdu"
	"github.com/otfabric/go-bacnet/service"
)

// serveFileWrites answers AtomicWriteFile like a stream file that already
// holds existing octets: a start of -1 appends at the end and the ACK reports
// where the chunk went. It records each requested start position.
func serveFileWrites(ctx context.Context, peerTr *virtual.Transport, dest bip.Endpoint, existing int32, mu *sync.Mutex, starts *[]int32) {
	limits := bacnet.DefaultDecodeLimits()
	size := existing
	for {
		pkt, err := peerTr.Recv(ctx)
		if err != nil {
			return
		}
		msg, err := bvlc.Parse(pkt.Data, limits)
		if err != nil {
			continue
		}
		n, _, err := npdu.Parse(msg.Payload, limits)
		if err != nil || len(n.APDU) == 0 {
			continue
		}
		pdu, err := apdu.Parse(n.APDU, limits)
		if err != nil || pdu.ConfirmedRequest == nil {
			continue
		}
		req, err := service.DecodeAtomicWriteFile(pdu.ConfirmedRequest.Payload, limits)
		if err != nil {
			continue
		}
		mu.Lock()
		*starts = append(*starts, req.StartPosition)
		mu.Unlock()
		at := req.StartPosition
		if at < 0 {
			at = size
		}
		if end := at + int32(len(req.Data)); end > size {
			size = end
		}
		ack, _ := service.EncodeAtomicWriteFileACK(service.AtomicWriteFileACK{Access: service.FileAccessStream, StartPosition: at})
		apduBytes := apdu.AppendComplexACK(nil, apdu.ComplexACK{
			InvokeID:      pdu.ConfirmedRequest.InvokeID,
			ServiceChoice: pdu.ConfirmedRequest.ServiceChoice,
			Payload:       ack,
		})
		nraw, _ := npdu.Append(nil, npdu.NPDU{Version: npdu.Version1, APDU: apduBytes})
		frame, _ := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionOriginalUnicastNPDU, Payload: nraw})
		_ = peerTr.Send(ctx, virtual.OutboundPacket{Data: frame, Destination: dest})
	}
}

func TestWriteFileStreamAppendFollowsACK(t *testing.T) {
	env := newVirtualPair(t)
	ctx := t.Context()
	var mu sync.Mutex
	var starts []int32
	go serveFileWrites(ctx, env.PeerTr, env.Local, 100, &mu, &starts)

	file := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 1}
	outcomes, err := env.Client.WriteFileStream(ctx, env.Target, file, -1, make([]byte, 10), FileWriteOptions{ChunkSize: 4})
	if err != nil {
		t.Fatalf("WriteFileStream: %v", err)
	}
	if len(outcomes) != 3 {
		t.Fatalf("got %d chunks, want 3", len(outcomes))
	}
	mu.Lock()
	defer mu.Unlock()
	// First chunk appends; the next ones continue where the device put it,
	// not at offset 3 (-1 + 4).
	want := []int32{-1, 104, 108}
	for i, w := range want {
		if starts[i] != w {
			t.Fatalf("chunk starts = %v, want %v", starts, want)
		}
	}
}

func TestWriteFileStreamRefusesPastInt32(t *testing.T) {
	env := newVirtualPair(t)
	file := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 1}
	outcomes, err := env.Client.WriteFileStream(t.Context(), env.Target, file, math.MaxInt32-2, make([]byte, 4), FileWriteOptions{})
	if !errors.Is(err, bacnet.ErrLimitExceeded) || len(outcomes) != 0 {
		t.Fatalf("outcomes=%d err=%v; want nothing sent and ErrLimitExceeded", len(outcomes), err)
	}
}

func TestReadFileStreamPositionWrapIsProtocolViolation(t *testing.T) {
	env := newVirtualPair(t)
	ctx := t.Context()
	go serveComplexACK(ctx, env.PeerTr, env.Local, func(uint8) ([]byte, error) {
		return service.EncodeAtomicReadFileACK(service.AtomicReadFileACK{
			Access: service.FileAccessStream, StartPosition: math.MaxInt32 - 1, Data: []byte("abcd"),
		})
	})
	file := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 1}
	out, err := env.Client.ReadFileStream(ctx, env.Target, file, FileReadOptions{})
	if !errors.Is(err, bacnet.ErrProtocolViolation) {
		t.Fatalf("err = %v, want ErrProtocolViolation", err)
	}
	if string(out) != "abcd" {
		t.Fatalf("data so far = %q", out)
	}
}

func TestReadFileRecordsCountWrapIsProtocolViolation(t *testing.T) {
	env := newVirtualPair(t)
	ctx := t.Context()
	go serveComplexACK(ctx, env.PeerTr, env.Local, func(uint8) ([]byte, error) {
		return service.EncodeAtomicReadFileACK(service.AtomicReadFileACK{
			Access: service.FileAccessRecord, StartPosition: 10, RecordCount: math.MaxUint32, Records: [][]byte{{1}},
		})
	})
	file := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 1}
	_, err := env.Client.ReadFileRecords(ctx, env.Target, file, 10, FileReadOptions{})
	if !errors.Is(err, bacnet.ErrProtocolViolation) {
		t.Fatalf("err = %v, want ErrProtocolViolation", err)
	}
}
