// SPDX-License-Identifier: MIT

package bacnet_test

import (
	"math"
	"testing"

	"github.com/otfabric/go-bacnet"
)

// Signed values round-trip at every width boundary (clause 20.2.5, minimal
// two's complement octets).
func TestSignedRoundTripWidthBoundaries(t *testing.T) {
	var vals []int64
	for w := 1; w < 8; w++ {
		lim := int64(1) << uint(8*w-1)
		vals = append(vals, lim-1, lim, -lim, -lim-1)
	}
	vals = append(vals, 0, -1, 1, math.MaxInt64, math.MinInt64)
	for _, v := range vals {
		raw, err := bacnet.AppendApplicationValue(nil, bacnet.SignedValue(v))
		if err != nil {
			t.Fatalf("encode %d: %v", v, err)
		}
		got, n, err := bacnet.ParseApplicationValue(raw, bacnet.DefaultDecodeLimits())
		if err != nil || n != len(raw) {
			t.Fatalf("decode %d (% x): n=%d err=%v", v, raw, n, err)
		}
		if got.Signed != v {
			t.Errorf("round trip %d -> % x -> %d", v, raw, got.Signed)
		}
		// Minimal: the content length is the fewest octets that hold v.
		want := 1
		for want < 8 {
			lim := int64(1) << uint(8*want-1)
			if v >= -lim && v < lim {
				break
			}
			want++
		}
		header := 1
		if want > 4 {
			header = 2 // LVT 5 plus one length octet
		}
		if got := len(raw) - header; got != want {
			t.Errorf("%d encoded in %d octets, want %d", v, got, want)
		}
	}
}

func TestUnsignedRoundTripWidthBoundaries(t *testing.T) {
	for _, v := range []uint64{0, 0xFF, 0x100, 0xFFFF, 0x10000, math.MaxUint32, math.MaxUint32 + 1, math.MaxUint64} {
		raw, err := bacnet.AppendApplicationValue(nil, bacnet.UnsignedValue(v))
		if err != nil {
			t.Fatal(err)
		}
		got, _, err := bacnet.ParseApplicationValue(raw, bacnet.DefaultDecodeLimits())
		if err != nil || got.Unsigned != v {
			t.Errorf("round trip %d -> % x -> %d, %v", v, raw, got.Unsigned, err)
		}
	}
}

// Octet strings across the extended-length boundaries (clause 20.2.1.3.1).
func TestOctetStringLengthBoundaries(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	limits.MaxAPDUSize = 1 << 20
	limits.MaxOctetStringLength = 1 << 20
	for _, n := range []int{0, 4, 5, 253, 254, 0xFFFF, 0x10000} {
		v := bacnet.ApplicationValue{Kind: bacnet.ValueOctetString, OctetString: make([]byte, n)}
		raw, err := bacnet.AppendApplicationValue(nil, v)
		if err != nil {
			t.Fatalf("encode %d: %v", n, err)
		}
		got, used, err := bacnet.ParseApplicationValue(raw, limits)
		if err != nil || used != len(raw) || len(got.OctetString) != n {
			t.Errorf("length %d: used=%d/%d got=%d err=%v", n, used, len(raw), len(got.OctetString), err)
		}
	}
}
