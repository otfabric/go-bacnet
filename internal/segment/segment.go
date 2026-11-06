// SPDX-License-Identifier: MIT

// Package segment holds the segmentation arithmetic that the requesting
// (client) and the responding (server) side of a confirmed transaction
// share: window sizes, the reassembly of a segmented message received in
// order, and the division of a payload into segments (ANSI/ASHRAE 135
// clause 5.2 and 5.4).
//
// The transaction state machines themselves stay in the client and the
// server: timers, retransmission, the answer to an out-of-order segment and
// the Abort reasons differ by role. This package has no I/O, no clock and
// no locking; callers serialise access to a Reassembly.
package segment

// MaxWindow is the largest window size; window sizes are 1..127 (clause
// 20.1.2 proposed-window-size, 20.1.6 actual-window-size).
const MaxWindow = 127

// MaxSegments is the number of distinct sequence numbers (0..255). A
// message that needs more segments would reuse sequence number 0.
const MaxSegments = 256

// ValidWindow reports whether w is a window size BACnet allows (1..127).
func ValidWindow(w uint8) bool { return w >= 1 && w <= MaxWindow }

// Window is the actual window of a segmented message the local side
// receives: the smaller of the local window and the sender's proposed
// window. A proposal of 0 (none) leaves the local window, and the result is
// never below 1.
func Window(local, proposed uint8) uint8 {
	w := local
	if proposed > 0 && proposed < w {
		w = proposed
	}
	if w == 0 {
		w = 1
	}
	return w
}

// Outcome is the result of adding one segment to a Reassembly.
type Outcome uint8

const (
	// Stored: the segment was the expected one and was stored; more
	// follow and the window is not full, so no SegmentACK is due.
	Stored Outcome = iota
	// StoredAck: the segment was stored and closed the window; a positive
	// SegmentACK for Last is due.
	StoredAck
	// Complete: the last segment was stored; a positive SegmentACK for Last
	// is due and Payload holds the whole message.
	Complete
	// OutOfOrder: the sequence number is not the expected one; nothing was
	// stored. Whether the answer is a repeated positive SegmentACK or a
	// negative one is for the caller's state machine.
	OutOfOrder
	// TooManySegments: storing the segment would exceed the segment limit;
	// nothing was stored and the reassembly must be abandoned.
	TooManySegments
	// TooLong: storing the segment would exceed the octet limit; nothing
	// was stored and the reassembly must be abandoned.
	TooLong
)

// Reassembly collects the segments of one segmented message, which arrive
// with consecutive sequence numbers modulo 256 starting at 0.
type Reassembly struct {
	window      uint8
	expected    uint8
	inWindow    uint8
	segments    int
	maxSegments int
	maxOctets   int
	buf         []byte
}

// NewReassembly starts a reassembly with the given actual window (see
// Window), at most maxSegments segments and at most maxOctets octets of
// payload. A window of 0 is treated as 1.
func NewReassembly(window uint8, maxSegments, maxOctets int) *Reassembly {
	if window == 0 {
		window = 1
	}
	return &Reassembly{window: window, maxSegments: maxSegments, maxOctets: maxOctets}
}

// Add offers the segment with sequence number seq. more is the segment's
// more-follows flag.
func (r *Reassembly) Add(seq uint8, more bool, payload []byte) Outcome {
	if seq != r.expected {
		return OutOfOrder
	}
	if r.segments+1 > r.maxSegments {
		return TooManySegments
	}
	if len(r.buf)+len(payload) > r.maxOctets {
		return TooLong
	}
	r.buf = append(r.buf, payload...)
	r.segments++
	r.expected++
	r.inWindow++
	if !more {
		r.inWindow = 0
		return Complete
	}
	if r.inWindow >= r.window {
		r.inWindow = 0
		return StoredAck
	}
	return Stored
}

// Window is the actual window of the reassembly.
func (r *Reassembly) Window() uint8 { return r.window }

// Expected is the sequence number of the next segment in order.
func (r *Reassembly) Expected() uint8 { return r.expected }

// Last is the sequence number of the last segment received in order, the
// sequence number a SegmentACK names (modulo 256: 255 after 256 segments).
// Before the first segment it is 255.
func (r *Reassembly) Last() uint8 { return r.expected - 1 }

// Segments is the number of segments stored.
func (r *Reassembly) Segments() int { return r.segments }

// Payload is the payload stored so far. It aliases the reassembly buffer.
func (r *Reassembly) Payload() []byte { return r.buf }

// Count is the number of segments of at most size octets that carry n
// octets; an empty payload still takes one segment. It is 0 when size is
// below 1.
func Count(n, size int) int {
	if size < 1 {
		return 0
	}
	if n <= 0 {
		return 1
	}
	return (n + size - 1) / size
}

// Bounds is the octet range [start, end) of segment i of an n-octet payload
// cut into segments of size octets.
func Bounds(i, size, n int) (start, end int) {
	start = min(i*size, n)
	end = min(start+size, n)
	return start, end
}
