// SPDX-License-Identifier: MIT

package capture

import "encoding/binary"

// Mutations returns deterministic malformed variants of a recorded
// datagram: every truncation, the BVLC length field off by one either way
// and set to the maximum, seeded bit flips, and the datagram twice in one.
func Mutations(b []byte, seed uint64) [][]byte {
	out := make([][]byte, 0, len(b)+20)
	for n := 0; n < len(b); n++ {
		out = append(out, b[:n])
	}
	if len(b) >= 4 {
		l := binary.BigEndian.Uint16(b[2:4])
		for _, v := range []uint16{l - 1, l + 1, 0xFFFF} {
			m := append([]byte(nil), b...)
			binary.BigEndian.PutUint16(m[2:4], v)
			out = append(out, m)
		}
	}
	r := splitmix{seed}
	for range 16 {
		m := append([]byte(nil), b...)
		if len(m) > 0 {
			for range 1 + r.below(3) {
				m[r.below(uint64(len(m)))] ^= 1 << r.below(8)
			}
		}
		out = append(out, m)
	}
	return append(out, append(append([]byte(nil), b...), b...))
}

// splitmix is SplitMix64: a reproducible sequence for test mutations, not
// randomness.
type splitmix struct{ s uint64 }

func (r *splitmix) below(n uint64) uint64 {
	r.s += 0x9e3779b97f4a7c15
	z := r.s
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return (z ^ (z >> 31)) % n
}
