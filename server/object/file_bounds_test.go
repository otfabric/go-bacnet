// SPDX-License-Identifier: MIT

package object

import (
	"errors"
	"runtime"
	"testing"
)

func recordFile(t *testing.T, cfg FileConfig) *File {
	t.Helper()
	cfg.Instance, cfg.Name, cfg.Record = 1, "records", true
	f, err := NewFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func recordOctetsOf(f *File) int {
	n := 0
	for _, r := range f.Records() {
		n += len(r)
	}
	return n
}

// A record file is bounded in octets as well as in records: a write that
// would take its records past recordFileOctets is answered with file-full
// and changes nothing.
func TestFileRecordOctetBound(t *testing.T) {
	const bound = recordFileOctets
	for _, tc := range []struct {
		name    string
		initial []int // sizes of the records the file starts with
		start   int32
		write   []int // sizes of the records written
		full    bool
	}{
		{name: "empty record into an empty file", write: []int{0}},
		{name: "one octet", write: []int{1}},
		{name: "one record of bound-1", write: []int{bound - 1}},
		{name: "one record of the bound", write: []int{bound}},
		{name: "one record of bound+1", write: []int{bound + 1}, full: true},
		{name: "append up to the bound", initial: []int{bound - 10}, start: -1, write: []int{4, 6}},
		{name: "append one octet past the bound", initial: []int{bound - 10}, start: -1, write: []int{4, 7}, full: true},
		{name: "append an empty record at the bound", initial: []int{bound}, start: -1, write: []int{0}},
		{name: "append one octet at the bound", initial: []int{bound}, start: -1, write: []int{1}, full: true},
		{name: "replace a record by one of the same size at the bound", initial: []int{bound - 8, 8}, start: 1, write: []int{8}},
		{name: "replace a record by a larger one at the bound", initial: []int{bound - 8, 8}, start: 1, write: []int{9}, full: true},
		{name: "replace a record by a smaller one and append", initial: []int{bound - 8, 8}, start: 1, write: []int{3, 5}},
		{name: "replace a record by a smaller one and append too much", initial: []int{bound - 8, 8}, start: 1, write: []int{3, 6}, full: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cfg FileConfig
			for _, n := range tc.initial {
				cfg.Records = append(cfg.Records, make([]byte, n))
			}
			f := recordFile(t, cfg)
			before := f.Records()
			var recs [][]byte
			for _, n := range tc.write {
				recs = append(recs, make([]byte, n))
			}
			_, err := f.WriteRecords(tc.start, recs)
			if tc.full {
				if !errors.Is(err, ErrFileFull) {
					t.Fatalf("error %v, want file-full", err)
				}
				if after := f.Records(); len(after) != len(before) || recordOctetsOf(f) > bound {
					t.Fatalf("a refused write changed the file: %d records, %d octets", len(after), recordOctetsOf(f))
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if n := recordOctetsOf(f); n > bound {
				t.Fatalf("file holds %d octets, bound %d", n, bound)
			}
		})
	}
}

// Records of the largest size one request carries are appended until the
// file refuses them: it refuses within the octet bound, and the writes
// allocate no more than a small multiple of it.
func TestFileRecordOctetBoundAllocation(t *testing.T) {
	const size = 65535
	fit := recordFileOctets / size
	f := recordFile(t, FileConfig{})
	rec := make([]byte, size)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for i := 0; i <= fit; i++ {
		_, err := f.WriteRecords(-1, [][]byte{rec})
		switch {
		case i < fit && err != nil:
			t.Fatalf("record %d of %d refused: %v", i+1, fit, err)
		case i == fit && !errors.Is(err, ErrFileFull):
			t.Fatalf("record %d accepted (%v): the file holds %d octets, bound %d", i+1, err, recordOctetsOf(f), recordFileOctets)
		}
	}
	runtime.ReadMemStats(&after)
	if got, budget := after.TotalAlloc-before.TotalAlloc, uint64(4*recordFileOctets); got > budget {
		t.Fatalf("writes allocated %d octets, budget %d", got, budget)
	}
	if n := recordOctetsOf(f); n != fit*size {
		t.Fatalf("file holds %d octets, want %d", n, fit*size)
	}
}

// The records an application gives a file may hold more than the default
// bound: the file then keeps that size as its bound, so the content can be
// replaced but not grown.
func TestFileRecordOctetBoundOfInitialContent(t *testing.T) {
	big := make([]byte, recordFileOctets+100)
	f := recordFile(t, FileConfig{Records: [][]byte{big, {1, 2, 3}}})
	if _, err := f.WriteRecords(1, [][]byte{{4, 5, 6}}); err != nil {
		t.Fatalf("replace by the same size: %v", err)
	}
	if _, err := f.WriteRecords(1, [][]byte{{4, 5, 6, 7}}); !errors.Is(err, ErrFileFull) {
		t.Fatalf("grow past the initial size: %v, want file-full", err)
	}
	if _, err := f.WriteRecords(-1, [][]byte{{}}); err != nil {
		t.Fatalf("append an empty record: %v", err)
	}
}

// FileConfig.MaxRecordOctets sets the octet bound of a record file: a
// write up to it is stored, one octet more is answered with file-full and
// changes nothing. Zero keeps the default.
func TestFileMaxRecordOctets(t *testing.T) {
	for _, tc := range []struct {
		name  string
		bound int // FileConfig.MaxRecordOctets
		want  int // the bound in effect
	}{
		{name: "zero keeps the default", bound: 0, want: recordFileOctets},
		{name: "one octet", bound: 1, want: 1},
		{name: "small", bound: 100, want: 100},
		{name: "default-1", bound: recordFileOctets - 1, want: recordFileOctets - 1},
		{name: "default+1", bound: recordFileOctets + 1, want: recordFileOctets + 1},
		{name: "larger than the default", bound: 4 * recordFileOctets, want: 4 * recordFileOctets},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := recordFile(t, FileConfig{MaxRecordOctets: tc.bound})
			if _, err := f.WriteRecords(-1, [][]byte{make([]byte, tc.want-1)}); err != nil {
				t.Fatalf("bound-1: %v", err)
			}
			if _, err := f.WriteRecords(-1, [][]byte{make([]byte, 2)}); !errors.Is(err, ErrFileFull) {
				t.Fatalf("bound+1: %v, want file-full", err)
			}
			if _, err := f.WriteRecords(-1, [][]byte{make([]byte, 1)}); err != nil {
				t.Fatalf("the bound: %v", err)
			}
			if n := recordOctetsOf(f); n != tc.want {
				t.Fatalf("file holds %d octets, want %d", n, tc.want)
			}
			if _, err := f.WriteRecords(-1, [][]byte{{}}); err != nil {
				t.Fatalf("an empty record at the bound: %v", err)
			}
			if _, err := f.WriteRecords(-1, [][]byte{{0}}); !errors.Is(err, ErrFileFull) {
				t.Fatalf("one octet at the bound: %v, want file-full", err)
			}
		})
	}
}

// A negative MaxRecordOctets, or one below what the initial records hold,
// is a configuration error; one that equals it is not.
func TestFileMaxRecordOctetsConfig(t *testing.T) {
	initial := [][]byte{make([]byte, 60), make([]byte, 40)}
	for _, tc := range []struct {
		name  string
		bound int
		ok    bool
	}{
		{name: "negative", bound: -1},
		{name: "below the initial content", bound: 99},
		{name: "the initial content", bound: 100, ok: true},
		{name: "above the initial content", bound: 101, ok: true},
		{name: "zero", bound: 0, ok: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewFile(FileConfig{Instance: 1, Name: "records", Record: true, MaxRecordOctets: tc.bound, Records: initial})
			if (err == nil) != tc.ok {
				t.Fatalf("NewFile: %v, want ok=%v", err, tc.ok)
			}
		})
	}
	if _, err := NewFile(FileConfig{Instance: 1, Name: "stream", MaxRecordOctets: -1}); err == nil {
		t.Fatal("a negative MaxRecordOctets accepted for a stream file")
	}
}
