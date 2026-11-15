// SPDX-License-Identifier: MIT

package capture

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"
)

// ngSection is a pcapng section header and one interface description.
func ngSection() []byte {
	shb := block(0x0a0d0d0a, []byte{0x4d, 0x3c, 0x2b, 0x1a, 1, 0, 0, 0, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
	return append(shb, block(1, []byte{linkRaw, 0, 0, 0, 0, 0, 0, 0})...)
}

// ngEnhanced is an enhanced packet block with the given interface and
// captured length fields in front of data.
func ngEnhanced(ifc, capLen uint32, data []byte) []byte {
	body := binary.LittleEndian.AppendUint32(nil, ifc)
	body = append(body, 0, 0, 0, 0, 0, 0, 0, 0) // time stamp
	body = binary.LittleEndian.AppendUint32(body, capLen)
	body = binary.LittleEndian.AppendUint32(body, capLen) // original length
	return block(6, append(body, data...))
}

// The 32-bit length and index fields of both formats at their bounds: 0, 1,
// what the file holds, one more, and the values around the range of a 32-bit
// int. A value that does not fit is ErrFormat on every platform; the nightly
// GOARCH=386 run executes the same table where int has 32 bits.
func TestLengthFieldBounds(t *testing.T) {
	const refused = -1
	data := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	type bound struct {
		value uint32
		want  int // octets of the one packet returned, or refused
	}
	// beyond are the values above anything a test file holds.
	beyond := func(want int) []bound {
		return []bound{{0x7FFFFFFF, want}, {0x80000000, want}, {0xFFFFFFF0, want}, {0xFFFFFFFF, want}}
	}
	// A pcap record carries min(value, 8) octets, so that 0, 1 and 8 are
	// records that end with the file and 9 is one octet more than it holds.
	pcap := func(bo binary.ByteOrder) func(uint32) []byte {
		return func(v uint32) []byte {
			b := pcapFile(bo, 0xa1b2c3d4, linkRaw, data[:min(v, 8)])
			bo.PutUint32(b[24+8:], v) // captured length of the first record
			return b
		}
	}
	enhanced := ngEnhanced(0, 8, data)

	for _, field := range []struct {
		name   string
		build  func(value uint32) []byte
		bounds []bound
	}{
		{
			name:   "pcap little-endian record length",
			build:  pcap(binary.LittleEndian),
			bounds: append([]bound{{0, 0}, {1, 1}, {8, 8}, {9, refused}}, beyond(refused)...),
		},
		{
			name:   "pcap big-endian record length",
			build:  pcap(binary.BigEndian),
			bounds: append([]bound{{0, 0}, {1, 1}, {8, 8}, {9, refused}}, beyond(refused)...),
		},
		{
			name: "pcapng enhanced packet captured length",
			build: func(v uint32) []byte {
				return append(ngSection(), ngEnhanced(0, v, data)...)
			},
			bounds: append([]bound{{0, 0}, {1, 1}, {8, 8}, {9, refused}}, beyond(refused)...),
		},
		{
			name: "pcapng enhanced packet interface",
			build: func(v uint32) []byte {
				return append(ngSection(), ngEnhanced(v, 8, data)...)
			},
			bounds: append([]bound{{0, 8}, {1, refused}}, beyond(refused)...),
		},
		{
			name: "pcapng block total length",
			build: func(v uint32) []byte {
				b := append(ngSection(), enhanced...)
				binary.LittleEndian.PutUint32(b[len(b)-len(enhanced)+4:], v)
				return b
			},
			bounds: append([]bound{
				{0, refused}, {1, refused}, {8, refused},
				{uint32(len(enhanced)), 8}, {uint32(len(enhanced)) + 1, refused}, {uint32(len(enhanced)) + 4, refused},
			}, beyond(refused)...),
		},
		{
			// The simple packet block states the original length; the
			// packet is at most what the block carries.
			name: "pcapng simple packet original length",
			build: func(v uint32) []byte {
				spb := binary.LittleEndian.AppendUint32(nil, v)
				return append(ngSection(), block(3, append(spb, data...))...)
			},
			bounds: append([]bound{{0, 0}, {1, 1}, {8, 8}, {9, 8}}, beyond(8)...),
		},
	} {
		for _, c := range field.bounds {
			t.Run(fmt.Sprintf("%s/%#x", field.name, c.value), func(t *testing.T) {
				raw := field.build(c.value)
				got, err := packets(raw)
				_, readErr := Read(raw)
				if c.want == refused {
					if !errors.Is(err, ErrFormat) || !errors.Is(readErr, ErrFormat) {
						t.Fatalf("packets: %v, Read: %v, want ErrFormat from both", err, readErr)
					}
					return
				}
				if err != nil || readErr != nil {
					t.Fatalf("packets: %v, Read: %v, want a packet of %d octets", err, readErr, c.want)
				}
				if len(got) != 1 || !bytes.Equal(got[0].data, data[:c.want]) {
					t.Fatalf("packets = %+v, want one packet with % x", got, data[:c.want])
				}
			})
		}
	}
}
