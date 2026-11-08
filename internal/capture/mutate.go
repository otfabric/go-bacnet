// SPDX-License-Identifier: MIT

package capture

import "math/rand/v2"

// Mutations returns deterministic malformed variants of a recorded
// datagram: every truncation, the BVLC length field off by one either way
// and set to the maximum, and seeded bit flips.
func Mutations(b []byte, seed uint64) [][]byte {
	var out [][]byte
	for n := 0; n < len(b); n++ {
		out = append(out, b[:n])
	}
	for _, d := range []int{-1, 1, 0xFFFF} {
		m := append([]byte(nil), b...)
		if len(m) >= 4 {
			l := int(m[2])<<8 | int(m[3])
			if d == 0xFFFF {
				l = d
			} else {
				l += d
			}
			m[2], m[3] = byte(l>>8), byte(l)
		}
		out = append(out, m)
	}
	r := rand.New(rand.NewPCG(seed, 0x6361707475726573))
	for range 16 {
		m := append([]byte(nil), b...)
		for range 1 + r.IntN(3) {
			i := r.IntN(len(m))
			m[i] ^= 1 << r.IntN(8)
		}
		out = append(out, m)
	}
	return append(out, append(append([]byte(nil), b...), b...)) // a datagram twice in one
}
