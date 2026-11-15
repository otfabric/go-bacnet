// SPDX-License-Identifier: MIT

package server

import (
	"math"
	"sync/atomic"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/server/object"
	"github.com/otfabric/go-bacnet/service"
)

// countingFile is a File that records the count of the last read.
type countingFile struct {
	*object.File
	asked atomic.Uint64
}

func (f *countingFile) ReadStream(start int32, count uint32) ([]byte, bool, error) {
	f.asked.Store(uint64(count))
	return f.File.ReadStream(start, count)
}

func (f *countingFile) ReadRecords(start int32, count uint32) ([][]byte, bool, error) {
	f.asked.Store(uint64(count))
	return f.File.ReadRecords(start, count)
}

// AtomicReadFile asks the file for no more than one octet, or one record,
// beyond what the reply to this requester can carry: a larger reply is not
// sent, so nothing more is copied for it. The answer is the same as for the
// count the requester named.
func TestAtomicReadFileCountWithinReplyBudget(t *testing.T) {
	stream := make([]byte, 1<<20)
	records := make([][]byte, 4096)
	for i := range records {
		records[i] = []byte{byte(i)}
	}
	for _, tc := range []struct {
		name      string
		record    bool
		segmented bool
		count     uint32
		wantAsked func(budget int) uint64
		abort     uint8 // 0: a ComplexACK is expected
	}{
		{name: "stream, count 0", count: 0, wantAsked: func(int) uint64 { return 0 }},
		{name: "stream, count 1", count: 1, wantAsked: func(int) uint64 { return 1 }},
		{name: "stream, count of the budget", count: 0, wantAsked: func(b int) uint64 { return uint64(b) }, abort: bacnet.AbortReasonSegmentationNotSupported},
		{name: "stream, count of the budget plus one", count: 1, wantAsked: func(b int) uint64 { return uint64(b) + 1 }, abort: bacnet.AbortReasonSegmentationNotSupported},
		{name: "stream, count 2^31", count: 1 << 31, wantAsked: func(b int) uint64 { return uint64(b) + 1 }, abort: bacnet.AbortReasonSegmentationNotSupported},
		{name: "stream, count 2^32-1", count: math.MaxUint32, wantAsked: func(b int) uint64 { return uint64(b) + 1 }, abort: bacnet.AbortReasonSegmentationNotSupported},
		{name: "stream, segmented reply, count 2^32-1", segmented: true, count: math.MaxUint32, wantAsked: func(b int) uint64 { return uint64(b) + 1 }, abort: bacnet.AbortReasonBufferOverflow},
		{name: "records, count 1", record: true, count: 1, wantAsked: func(int) uint64 { return 1 }},
		{name: "records, count 2^32-1", record: true, count: math.MaxUint32, wantAsked: func(b int) uint64 { return uint64(b) + 1 }, abort: bacnet.AbortReasonSegmentationNotSupported},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			cfg := object.FileConfig{Instance: 7, Name: "F-7", Data: stream}
			if tc.record {
				cfg = object.FileConfig{Instance: 7, Name: "F-7", Record: true, Records: records}
			}
			inner, err := object.NewFile(cfg)
			if err != nil {
				t.Fatal(err)
			}
			f := &countingFile{File: inner}
			if err := h.dev.Add(f); err != nil {
				t.Fatal(err)
			}
			req := apdu.ConfirmedRequest{SegmentedResponseAccepted: tc.segmented, MaxSegments: 7, MaxAPDU: 5, InvokeID: 1, ServiceChoice: apdu.ServiceAtomicReadFile}
			budget := h.s.responseBudget(&req)
			count := tc.count
			switch tc.name {
			case "stream, count of the budget":
				count = uint32(budget)
			case "stream, count of the budget plus one":
				count = uint32(budget) + 1
			}
			access := service.FileAccessStream
			if tc.record {
				access = service.FileAccessRecord
			}
			req.Payload = mustEncode(t)(service.EncodeAtomicReadFile(service.AtomicReadFileRequest{File: f.Identifier(), Access: access, Count: count}))
			h.inject(apdu.AppendConfirmedRequest(nil, req))
			if tc.abort == 0 {
				h.expectType(apdu.TypeComplexACK)
			} else {
				h.expectAbort(tc.abort)
			}
			if got, want := f.asked.Load(), tc.wantAsked(budget); got != want {
				t.Fatalf("the file was asked for %d, want %d (reply budget %d)", got, want, budget)
			}
		})
	}
}
