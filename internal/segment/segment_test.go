// SPDX-License-Identifier: MIT

package segment_test

import (
	"bytes"
	"testing"

	"github.com/otfabric/go-bacnet/internal/segment"
)

func TestValidWindow(t *testing.T) {
	for w, want := range map[uint8]bool{0: false, 1: true, 64: true, 127: true, 128: false, 255: false} {
		if got := segment.ValidWindow(w); got != want {
			t.Errorf("ValidWindow(%d) = %v, want %v", w, got, want)
		}
	}
}

func TestWindow(t *testing.T) {
	cases := []struct{ local, proposed, want uint8 }{
		{4, 2, 2},   // the smaller proposal wins
		{4, 16, 4},  // the local window caps a larger proposal
		{4, 4, 4},   // equal
		{4, 0, 4},   // no proposal
		{0, 0, 1},   // never below 1
		{0, 3, 1},   // a zero local window is not raised by the proposal
		{127, 1, 1}, // smallest proposal
	}
	for _, c := range cases {
		if got := segment.Window(c.local, c.proposed); got != c.want {
			t.Errorf("Window(%d, %d) = %d, want %d", c.local, c.proposed, got, c.want)
		}
	}
}

func TestReassemblyInOrder(t *testing.T) {
	r := segment.NewReassembly(2, 10, 100)
	if r.Window() != 2 || r.Expected() != 0 || r.Last() != 255 || r.Segments() != 0 || len(r.Payload()) != 0 {
		t.Fatalf("fresh reassembly: window %d expected %d last %d segments %d", r.Window(), r.Expected(), r.Last(), r.Segments())
	}
	steps := []struct {
		seq  uint8
		more bool
		want segment.Outcome
	}{
		{0, true, segment.Stored},
		{1, true, segment.StoredAck}, // closes the window of two
		{2, true, segment.Stored},
		{3, true, segment.StoredAck},
		{4, false, segment.Complete}, // the last segment always acknowledges
	}
	for _, s := range steps {
		if got := r.Add(s.seq, s.more, []byte{s.seq}); got != s.want {
			t.Fatalf("Add(%d) = %v, want %v", s.seq, got, s.want)
		}
		if r.Last() != s.seq || r.Expected() != s.seq+1 {
			t.Fatalf("after %d: last %d expected %d", s.seq, r.Last(), r.Expected())
		}
	}
	if r.Segments() != 5 || !bytes.Equal(r.Payload(), []byte{0, 1, 2, 3, 4}) {
		t.Fatalf("segments %d payload % x", r.Segments(), r.Payload())
	}
}

func TestReassemblyWindowOfOne(t *testing.T) {
	r := segment.NewReassembly(0, 10, 100) // 0 is treated as 1
	if r.Window() != 1 {
		t.Fatalf("window %d", r.Window())
	}
	for i := range uint8(3) {
		if got := r.Add(i, true, nil); got != segment.StoredAck {
			t.Fatalf("Add(%d) = %v, want StoredAck", i, got)
		}
	}
}

func TestReassemblyWindowResetsAfterOutOfOrder(t *testing.T) {
	r := segment.NewReassembly(3, 10, 100)
	_ = r.Add(0, true, []byte{0})
	_ = r.Add(1, true, []byte{1})
	// A gap and a repeat change nothing.
	for _, seq := range []uint8{3, 1, 0, 200} {
		if got := r.Add(seq, true, []byte{0xFF}); got != segment.OutOfOrder {
			t.Fatalf("Add(%d) = %v, want OutOfOrder", seq, got)
		}
	}
	if r.Expected() != 2 || r.Last() != 1 || r.Segments() != 2 || !bytes.Equal(r.Payload(), []byte{0, 1}) {
		t.Fatalf("state changed: expected %d segments %d payload % x", r.Expected(), r.Segments(), r.Payload())
	}
	if got := r.Add(2, true, []byte{2}); got != segment.StoredAck {
		t.Fatalf("Add(2) = %v, want StoredAck (third of the window)", got)
	}
}

func TestReassemblyFirstSegmentMustBeZero(t *testing.T) {
	r := segment.NewReassembly(1, 10, 100)
	if got := r.Add(1, true, []byte{1}); got != segment.OutOfOrder {
		t.Fatalf("Add(1) on a fresh reassembly = %v, want OutOfOrder", got)
	}
}

func TestReassemblyTooManySegments(t *testing.T) {
	r := segment.NewReassembly(4, 2, 100)
	_ = r.Add(0, true, []byte{0})
	_ = r.Add(1, true, []byte{1})
	if got := r.Add(2, false, []byte{2}); got != segment.TooManySegments {
		t.Fatalf("third segment with a limit of two = %v", got)
	}
	if r.Segments() != 2 || r.Expected() != 2 || len(r.Payload()) != 2 {
		t.Fatalf("refused segment was stored: segments %d payload % x", r.Segments(), r.Payload())
	}
}

func TestReassemblyTooLong(t *testing.T) {
	r := segment.NewReassembly(4, 10, 5)
	if got := r.Add(0, true, []byte{1, 2, 3}); got != segment.Stored {
		t.Fatalf("first = %v", got)
	}
	// Exactly at the limit is allowed.
	if got := r.Add(1, true, []byte{4, 5}); got != segment.Stored {
		t.Fatalf("at limit = %v", got)
	}
	if got := r.Add(2, false, []byte{6}); got != segment.TooLong {
		t.Fatalf("past limit = %v", got)
	}
	if r.Segments() != 2 || len(r.Payload()) != 5 {
		t.Fatalf("refused segment was stored: segments %d len %d", r.Segments(), len(r.Payload()))
	}
}

func TestReassemblySegmentLimitCheckedFirst(t *testing.T) {
	r := segment.NewReassembly(1, 0, 0)
	if got := r.Add(0, false, []byte{1}); got != segment.TooManySegments {
		t.Fatalf("both limits exceeded = %v, want TooManySegments", got)
	}
}

// Sequence numbers are modulo 256: after 256 segments the next expected is
// 0 again and the last received is 255.
func TestReassemblySequenceWrap(t *testing.T) {
	r := segment.NewReassembly(127, 1000, 1<<20)
	for i := range 256 {
		if got := r.Add(uint8(i), true, []byte{byte(i)}); got == segment.OutOfOrder {
			t.Fatalf("segment %d out of order", i)
		}
	}
	if r.Expected() != 0 || r.Last() != 255 || r.Segments() != 256 {
		t.Fatalf("after 256: expected %d last %d segments %d", r.Expected(), r.Last(), r.Segments())
	}
	if got := r.Add(255, true, nil); got != segment.OutOfOrder {
		t.Fatalf("repeat of 255 = %v", got)
	}
	if got := r.Add(0, false, []byte{0}); got != segment.Complete {
		t.Fatalf("segment 0 after the wrap = %v", got)
	}
	if r.Segments() != 257 || r.Last() != 0 {
		t.Fatalf("after wrap: segments %d last %d", r.Segments(), r.Last())
	}
}

func TestCount(t *testing.T) {
	cases := []struct{ n, size, want int }{
		{0, 10, 1}, // an empty payload still takes one segment
		{-1, 10, 1},
		{1, 10, 1},
		{10, 10, 1},
		{11, 10, 2},
		{20, 10, 2},
		{21, 10, 3},
		{2560, 10, 256},
		{2561, 10, 257},
		{5, 0, 0}, // no room for payload
		{5, -3, 0},
	}
	for _, c := range cases {
		if got := segment.Count(c.n, c.size); got != c.want {
			t.Errorf("Count(%d, %d) = %d, want %d", c.n, c.size, got, c.want)
		}
	}
}

func TestBounds(t *testing.T) {
	const n, size = 25, 10
	want := [][2]int{{0, 10}, {10, 20}, {20, 25}}
	var joined int
	for i, w := range want {
		start, end := segment.Bounds(i, size, n)
		if start != w[0] || end != w[1] {
			t.Errorf("Bounds(%d) = [%d, %d), want [%d, %d)", i, start, end, w[0], w[1])
		}
		joined += end - start
	}
	if joined != n {
		t.Fatalf("segments cover %d of %d octets", joined, n)
	}
	// Past the end and for an empty payload the range is empty, never
	// out of bounds.
	if start, end := segment.Bounds(5, size, n); start != n || end != n {
		t.Fatalf("past end [%d, %d)", start, end)
	}
	if start, end := segment.Bounds(0, size, 0); start != 0 || end != 0 {
		t.Fatalf("empty [%d, %d)", start, end)
	}
}

// Every split of Count segments covers the payload exactly once.
func TestCountBoundsCover(t *testing.T) {
	for n := 0; n <= 64; n++ {
		for size := 1; size <= 9; size++ {
			count := segment.Count(n, size)
			next := 0
			for i := range count {
				start, end := segment.Bounds(i, size, n)
				if start != next || end-start > size || (i < count-1 && end-start != size) {
					t.Fatalf("n=%d size=%d segment %d [%d, %d)", n, size, i, start, end)
				}
				next = end
			}
			if next != n {
				t.Fatalf("n=%d size=%d covered %d", n, size, next)
			}
		}
	}
}
