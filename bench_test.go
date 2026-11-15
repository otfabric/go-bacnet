// SPDX-License-Identifier: MIT

package bacnet_test

import (
	"fmt"
	"testing"

	"github.com/otfabric/go-bacnet"
)

// benchSequence is n application-tagged Unsigned values, one to three octets
// of content each.
func benchSequence(b *testing.B, n int) []byte {
	b.Helper()
	var (
		data []byte
		err  error
	)
	for i := range n {
		if data, err = bacnet.AppendApplicationValue(data, bacnet.UnsignedValue(uint64(i)*37)); err != nil {
			b.Fatal(err)
		}
	}
	return data
}

// BenchmarkParseSequence parses flat sequences of growing length; the longest
// holds as many elements as the default limit admits.
func BenchmarkParseSequence(b *testing.B) {
	limits := bacnet.DefaultDecodeLimits()
	for _, n := range []int{1, 16, 256, limits.MaxElements} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			data := benchSequence(b, n)
			var (
				elements []bacnet.Element
				used     int
				err      error
			)
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				if elements, used, err = bacnet.ParseSequence(data, limits, -1); err != nil {
					b.Fatal(err)
				}
			}
			if len(elements) != n || used != len(data) {
				b.Fatalf("parsed %d elements and %d of %d octets, want %d elements", len(elements), used, len(data), n)
			}
		})
	}
}
