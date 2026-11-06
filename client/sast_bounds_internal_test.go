// SPDX-License-Identifier: MIT

package client

import (
	"errors"
	"math"
	"testing"

	"github.com/otfabric/go-bacnet"
)

func TestNextFilePosition(t *testing.T) {
	tests := []struct {
		start   int32
		n       int64
		want    int32
		wantErr bool
	}{
		{0, 0, 0, false},
		{10, 5, 15, false},
		{math.MaxInt32 - 4, 4, math.MaxInt32, false},
		{math.MaxInt32 - 4, 5, 0, true}, // would wrap to -2^31
		{0, math.MaxUint32, 0, true},    // record count from a hostile peer
		{math.MinInt32, 1, 0, true},     // negative position reported
		{-1, 1, 0, false},               // -1 + 1 = 0 is a valid offset
	}
	for _, tc := range tests {
		got, err := nextFilePosition(tc.start, tc.n)
		if tc.wantErr {
			if !errors.Is(err, bacnet.ErrProtocolViolation) {
				t.Errorf("nextFilePosition(%d, %d) err = %v, want ErrProtocolViolation", tc.start, tc.n, err)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("nextFilePosition(%d, %d) = %d, %v; want %d", tc.start, tc.n, got, err, tc.want)
		}
	}
}

func TestChunkCount(t *testing.T) {
	tests := []struct {
		chunk  uint32
		remain int
		want   uint32
	}{
		{256, 1000, 256},
		{256, 10, 10},
		{256, 0, 0},
		{256, -5, 0},
		{math.MaxUint32, math.MaxInt32, math.MaxInt32},
	}
	for _, tc := range tests {
		if got := chunkCount(tc.chunk, tc.remain); got != tc.want {
			t.Errorf("chunkCount(%d, %d) = %d, want %d", tc.chunk, tc.remain, got, tc.want)
		}
	}
}

func TestObjectListLength(t *testing.T) {
	tests := []struct {
		count    uint64
		maxItems int
		want     uint32
	}{
		{10, 4096, 10},
		{10000, 4096, 4096},
		{10, 0, 0},
		{10, -1, 0},
		{math.MaxUint64, math.MaxInt32, math.MaxInt32},
		{math.MaxUint64, 4096, 4096},
	}
	for _, tc := range tests {
		if got := objectListLength(tc.count, tc.maxItems); got != tc.want {
			t.Errorf("objectListLength(%d, %d) = %d, want %d", tc.count, tc.maxItems, got, tc.want)
		}
	}
	if math.MaxInt == math.MaxInt64 {
		// The uint32 index loop must end: never ask for index 2^32-1.
		if got := objectListLength(math.MaxUint64, math.MaxInt); got != math.MaxUint32-1 {
			t.Errorf("objectListLength(max, MaxInt) = %d, want %d", got, uint32(math.MaxUint32-1))
		}
	}
}

func TestStateOf(t *testing.T) {
	for _, st := range []SubscriptionState{SubscriptionClosed, SubscriptionDegraded} {
		if got := stateOf(uint32(st)); got != st {
			t.Errorf("stateOf(%d) = %v", st, got)
		}
	}
	if got := stateOf(256); got != SubscriptionClosed {
		t.Errorf("stateOf(256) = %v, want Closed (not a truncated 0)", got)
	}
}
