// SPDX-License-Identifier: MIT

package capture

import "testing"

// Malformed variants of every recorded datagram decode or fail cleanly at
// every layer, and whatever still decodes encodes again without panicking.
func TestGoldenMutationsDecode(t *testing.T) {
	n := 0
	for _, g := range loadGolden(t) {
		for i, f := range g.Frames {
			dg, err := f.Datagram()
			if err != nil {
				t.Fatal(err)
			}
			d := NewDecoder()
			for _, m := range Mutations(dg.Data, uint64(i)) {
				dg.Data = m
				msg := d.Decode(dg)
				if msg.Err == nil {
					_, _, _ = Reencode(msg)
				}
				n++
			}
		}
	}
	if n < 10000 {
		t.Fatalf("only %d variants", n)
	}
}
