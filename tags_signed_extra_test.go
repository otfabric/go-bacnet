// SPDX-License-Identifier: MIT

package bacnet_test

import (
	"math"
	"testing"

	"github.com/otfabric/go-bacnet"
)

func TestEncodeSignedExtremeWidths(t *testing.T) {
	for _, v := range []int64{math.MinInt64, math.MaxInt64, -0x80000000000000, 0x7fffffffffffff} {
		raw, err := bacnet.AppendApplicationValue(nil, bacnet.SignedValue(v))
		if err != nil || len(raw) == 0 {
			t.Fatalf("%d: %v %#v", v, err, raw)
		}
		el, n, err := bacnet.ParseTag(raw, bacnet.DefaultDecodeLimits())
		if err != nil || n != len(raw) || el.Value.Signed != v {
			t.Fatalf("%d decode: %v n=%d got=%d", v, err, n, el.Value.Signed)
		}
	}
}
