// SPDX-License-Identifier: MIT

package object

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

// A start position near the top of the 32-bit range plus the data length
// is compared without wrapping: the file is full, nothing is written.
func TestFileWriteStreamStartBounds(t *testing.T) {
	f, err := NewFile(FileConfig{Instance: 1, Name: "f", Data: []byte("hello")})
	if err != nil {
		t.Fatal(err)
	}
	for _, start := range []int32{math.MaxInt32, math.MaxInt32 - 1, 1 << 20} {
		if _, err := f.WriteStream(start, []byte{1, 2}); !errors.Is(err, ErrFileFull) {
			t.Fatalf("start %d: %v", start, err)
		}
	}
	if string(f.Data()) != "hello" {
		t.Fatalf("data changed: %q", f.Data())
	}
	// At the bound itself the write still fits.
	g, _ := NewFile(FileConfig{Instance: 2, Name: "g", MaxSize: 4})
	if at, err := g.WriteStream(2, []byte{1, 2}); err != nil || at != 2 || len(g.Data()) != 4 {
		t.Fatalf("at %d err %v len %d", at, err, len(g.Data()))
	}
	if _, err := g.WriteStream(3, []byte{1, 2}); !errors.Is(err, ErrFileFull) {
		t.Fatal(err)
	}
}

// A by-time ReadRange with the largest positive or negative count returns
// the records from the reference on (or before it).
func TestTrendLogByTimeCountBounds(t *testing.T) {
	tl, err := NewTrendLog(TrendLogConfig{Instance: 1, Name: "tl", Enable: true})
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.Local)
	for i := range 4 {
		if err := tl.Log(base.Add(time.Duration(i)*time.Minute), bacnet.RealValue(float32(i)), nil); err != nil {
			t.Fatal(err)
		}
	}
	ref := service.DateTime{Date: bacnet.Date{Year: 126, Month: 1, Day: 1, Weekday: 0xFF}, Time: bacnet.Time{Hour: 12, Minute: 1, Second: 30}}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer}
	res, err := tl.ReadRange(context.Background(), prop, RangeQuery{By: service.ReadRangeByTime, Time: ref, Count: math.MaxInt32})
	if err != nil || res.Position != 3 || len(res.Records) != 2 {
		t.Fatalf("forward: position %d records %d err %v", res.Position, len(res.Records), err)
	}
	res, err = tl.ReadRange(context.Background(), prop, RangeQuery{By: service.ReadRangeByTime, Time: ref, Count: math.MinInt32})
	if err != nil || res.Position != 1 || len(res.Records) != 2 {
		t.Fatalf("backward: position %d records %d err %v", res.Position, len(res.Records), err)
	}
}
