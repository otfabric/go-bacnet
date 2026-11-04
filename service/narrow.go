package service

import (
	"fmt"
	"math"

	bacnet "github.com/otfabric/go-bacnet"
)

// Range-checked narrowing of decoded values. A peer can encode any Unsigned or
// Signed in up to eight octets; a field whose Go type is narrower must reject
// the value instead of keeping its low bits (a VT session identifier of 256
// must not become 0, a file start position of 2^31 must not become -2^31).

func narrowU8(u uint64, what string) (uint8, error) {
	if u > math.MaxUint8 {
		return 0, fmt.Errorf("%w: %s %d exceeds 8 bits", bacnet.ErrMalformed, what, u)
	}
	return uint8(u), nil
}

func narrowU16(u uint64, what string) (uint16, error) {
	if u > math.MaxUint16 {
		return 0, fmt.Errorf("%w: %s %d exceeds 16 bits", bacnet.ErrMalformed, what, u)
	}
	return uint16(u), nil
}

func narrowU32(u uint64, what string) (uint32, error) {
	if u > math.MaxUint32 {
		return 0, fmt.Errorf("%w: %s %d exceeds 32 bits", bacnet.ErrMalformed, what, u)
	}
	return uint32(u), nil
}

func narrowS32(v int64, what string) (int32, error) {
	if v < math.MinInt32 || v > math.MaxInt32 {
		return 0, fmt.Errorf("%w: %s %d outside 32-bit signed range", bacnet.ErrMalformed, what, v)
	}
	return int32(v), nil
}
