// SPDX-License-Identifier: MIT

package apdu

import "fmt"

// MaxAPDUCode is the confirmed-request max-APDU-length-accepted nibble encoding.
type MaxAPDUCode uint8

const (
	MaxAPDU50   MaxAPDUCode = 0
	MaxAPDU128  MaxAPDUCode = 1
	MaxAPDU206  MaxAPDUCode = 2
	MaxAPDU480  MaxAPDUCode = 3
	MaxAPDU1024 MaxAPDUCode = 4
	MaxAPDU1476 MaxAPDUCode = 5
)

// MaxSegmentsCode is the confirmed-request max-segments-accepted nibble encoding.
type MaxSegmentsCode uint8

const (
	MaxSegmentsUnspecified MaxSegmentsCode = 0
	MaxSegments2           MaxSegmentsCode = 1
	MaxSegments4           MaxSegmentsCode = 2
	MaxSegments8           MaxSegmentsCode = 3
	MaxSegments16          MaxSegmentsCode = 4
	MaxSegments32          MaxSegmentsCode = 5
	MaxSegments64          MaxSegmentsCode = 6
	MaxSegmentsGT64        MaxSegmentsCode = 7
)

// EncodeMaxAPDUSize maps a byte capacity to the wire MaxAPDU code.
func EncodeMaxAPDUSize(size uint16) (MaxAPDUCode, error) {
	switch {
	case size >= 1476:
		return MaxAPDU1476, nil
	case size >= 1024:
		return MaxAPDU1024, nil
	case size >= 480:
		return MaxAPDU480, nil
	case size >= 206:
		return MaxAPDU206, nil
	case size >= 128:
		return MaxAPDU128, nil
	case size >= 50:
		return MaxAPDU50, nil
	default:
		return 0, fmt.Errorf("apdu: max APDU size %d below minimum 50", size)
	}
}

// DecodeMaxAPDUSize maps a wire MaxAPDU code to its nominal capacity.
func DecodeMaxAPDUSize(code MaxAPDUCode) (uint16, bool) {
	switch code {
	case MaxAPDU50:
		return 50, true
	case MaxAPDU128:
		return 128, true
	case MaxAPDU206:
		return 206, true
	case MaxAPDU480:
		return 480, true
	case MaxAPDU1024:
		return 1024, true
	case MaxAPDU1476:
		return 1476, true
	default:
		return 0, false
	}
}

// EncodeMaxSegments maps a segment count limit to the wire code.
// count 0 means unspecified. The code never over-advertises relative to count:
// a limit of 3 encodes as 2 (not 4). Values above 64 encode as GT64.
func EncodeMaxSegments(count int) MaxSegmentsCode {
	switch {
	case count <= 0:
		return MaxSegmentsUnspecified
	case count < 2:
		// Smallest non-unspecified advertisement is 2; callers with MaxSegments=1
		// should set MaxSegmentsUnspecified or raise the bound.
		return MaxSegmentsUnspecified
	case count < 4:
		return MaxSegments2
	case count < 8:
		return MaxSegments4
	case count < 16:
		return MaxSegments8
	case count < 32:
		return MaxSegments16
	case count < 64:
		return MaxSegments32
	case count == 64:
		return MaxSegments64
	default:
		return MaxSegmentsGT64
	}
}
