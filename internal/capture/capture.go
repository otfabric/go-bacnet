// SPDX-License-Identifier: MIT

// Package capture reads recorded BACnet traffic for tests: classic pcap and
// pcapng files with Ethernet (DIX and IEEE 802.3/802.2 LLC), VLAN-tagged
// Ethernet, Linux cooked (SLL) or raw IPv4 frames. It returns every IPv4 UDP
// datagram whose payload is a BVLL message of type 0x81 (BACnet/IP, Annex J),
// reassembling fragmented IPv4 datagrams first, whatever the UDP port: the
// recordings use 47808 to 47823 and others.
//
// BACnet carried on another data link (ISO 8802-2 Ethernet, MS/TP, ARCNET) is
// not BACnet/IP and is not decoded here: it is counted in File.NonIP so that a
// test can keep it apart from the BACnet/IP evidence, and the 802.2 Ethernet
// NPDUs are returned in File.Ethernet.
//
// It is test infrastructure for testdata/captures and is not part of the
// public API. It reads files from this repository or its capture cache, not
// untrusted input, but it still checks every length it reads.
package capture

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"time"
)

// Datagram is one BACnet/IP UDP datagram.
type Datagram struct {
	Frame int       // 1-based number of the frame that completed it
	Time  time.Time // capture time stamp of that frame (UTC)
	Src   netip.AddrPort
	Dst   netip.AddrPort
	Data  []byte // the UDP payload: BVLC header, NPDU, APDU
}

// EthernetNPDU is one BACnet NPDU on ISO 8802-2 (LLC DSAP/SSAP 0x82).
type EthernetNPDU struct {
	Frame    int
	Src, Dst [6]byte
	Data     []byte // the NPDU
}

// File is the BACnet content of one capture file.
type File struct {
	Frames    int            // frames in the file
	IP        []Datagram     // BACnet/IP datagrams in frame order
	Ethernet  []EthernetNPDU // BACnet on ISO 8802-2 Ethernet
	NonIP     map[string]int // frames of other data links, by name
	Fragments int            // IPv4 fragments merged into IP datagrams
}

// ErrFormat reports a file that is neither pcap nor pcapng, or is cut short.
var ErrFormat = errors.New("capture: unsupported or malformed capture file")

// Link-layer header types (tcpdump.org/linktypes.html).
const (
	linkEthernet = 1
	linkARCNET   = 7
	linkRaw      = 101
	linkSLL      = 113
	linkARCNETL  = 129
	linkMSTP     = 165
	linkIPv4     = 228
)

const (
	etherIPv4 = 0x0800
	etherVLAN = 0x8100
	protoUDP  = 17
)

// ReadFile reads a capture file.
func ReadFile(path string) (*File, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	b, err := root.ReadFile(filepath.Base(path))
	if err != nil {
		return nil, err
	}
	return Read(b)
}

// Read parses a capture held in memory.
func Read(b []byte) (*File, error) {
	frames, err := packets(b)
	if err != nil {
		return nil, err
	}
	r := &reader{f: &File{Frames: len(frames), NonIP: map[string]int{}}, frags: map[fragKey]*fragBuf{}}
	for i, p := range frames {
		r.frame(i+1, p)
	}
	return r.f, nil
}

// packet is one link-layer frame.
type packet struct {
	link uint32
	ts   time.Time
	data []byte
}

func packets(b []byte) ([]packet, error) {
	if len(b) < 4 {
		return nil, ErrFormat
	}
	switch binary.LittleEndian.Uint32(b) {
	case 0xa1b2c3d4:
		return pcapPackets(b, binary.LittleEndian, false)
	case 0xa1b23c4d:
		return pcapPackets(b, binary.LittleEndian, true)
	case 0xd4c3b2a1:
		return pcapPackets(b, binary.BigEndian, false)
	case 0x4d3cb2a1:
		return pcapPackets(b, binary.BigEndian, true)
	case 0x0a0d0d0a:
		return pcapngPackets(b)
	}
	return nil, ErrFormat
}

func pcapPackets(b []byte, bo binary.ByteOrder, nano bool) ([]packet, error) {
	if len(b) < 24 {
		return nil, ErrFormat
	}
	link := bo.Uint32(b[20:24]) & 0x0fffffff
	var out []packet
	for off := 24; off < len(b); {
		if len(b)-off < 16 {
			return nil, ErrFormat
		}
		sec, frac := int64(bo.Uint32(b[off:])), int64(bo.Uint32(b[off+4:]))
		n := int(bo.Uint32(b[off+8 : off+12]))
		off += 16
		if n > len(b)-off {
			return nil, ErrFormat
		}
		if !nano {
			frac *= 1000
		}
		out = append(out, packet{link: link, ts: time.Unix(sec, frac).UTC(), data: b[off : off+n]})
		off += n
	}
	return out, nil
}

type ngInterface struct {
	link uint32
	unit int64 // time stamp units per second
}

func pcapngPackets(b []byte) ([]packet, error) {
	var (
		bo  binary.ByteOrder = binary.LittleEndian
		ifs []ngInterface
		out []packet
	)
	for off := 0; off < len(b); {
		if len(b)-off < 12 {
			return nil, ErrFormat
		}
		typ := bo.Uint32(b[off:])
		if typ == 0x0a0d0d0a { // section header: byte order from its magic
			switch binary.LittleEndian.Uint32(b[off+8:]) {
			case 0x1a2b3c4d:
				bo = binary.LittleEndian
			case 0x4d3c2b1a:
				bo = binary.BigEndian
			default:
				return nil, ErrFormat
			}
			ifs = ifs[:0]
		}
		n := int(bo.Uint32(b[off+4:]))
		if n < 12 || n%4 != 0 || n > len(b)-off {
			return nil, ErrFormat
		}
		body := b[off+8 : off+n-4]
		switch typ {
		case 1: // interface description
			if len(body) < 8 {
				return nil, ErrFormat
			}
			ifs = append(ifs, ngInterface{link: uint32(bo.Uint16(body)), unit: tsResolution(body[8:], bo)})
		case 6: // enhanced packet
			if len(body) < 20 {
				return nil, ErrFormat
			}
			ifc := int(bo.Uint32(body))
			capLen := int(bo.Uint32(body[12:]))
			if ifc >= len(ifs) || capLen > len(body)-20 {
				return nil, ErrFormat
			}
			ts := int64(bo.Uint32(body[4:]))<<32 | int64(bo.Uint32(body[8:]))
			u := ifs[ifc].unit
			out = append(out, packet{link: ifs[ifc].link, ts: time.Unix(ts/u, (ts%u)*(1e9/u)).UTC(), data: body[20 : 20+capLen]})
		case 3: // simple packet, interface 0, no time stamp
			if len(body) < 4 || len(ifs) == 0 {
				return nil, ErrFormat
			}
			capLen := min(int(bo.Uint32(body)), len(body)-4)
			out = append(out, packet{link: ifs[0].link, data: body[4 : 4+capLen]})
		}
		off += n
	}
	return out, nil
}

// tsResolution reads the if_tsresol option of an interface description
// block (default microseconds).
func tsResolution(opts []byte, bo binary.ByteOrder) int64 {
	for len(opts) >= 4 {
		code, n := bo.Uint16(opts), int(bo.Uint16(opts[2:]))
		if code == 0 || 4+n > len(opts) {
			break
		}
		if code == 9 && n >= 1 {
			v := opts[4]
			if v&0x80 == 0 && v <= 9 {
				u := int64(1)
				for range v {
					u *= 10
				}
				return u
			}
		}
		opts = opts[4+(n+3)&^3:]
	}
	return 1e6
}

type fragKey struct {
	src, dst [4]byte
	id       uint16
}

type fragBuf struct {
	header []byte // IPv4 header of the first fragment
	data   []byte
	have   []bool
	total  int // payload length, known once the last fragment arrived
}

type reader struct {
	f     *File
	frags map[fragKey]*fragBuf
}

func (r *reader) frame(num int, p packet) {
	d := p.data
	switch p.link {
	case linkEthernet:
		r.ethernet(num, p.ts, d)
	case linkSLL:
		if len(d) >= 16 && binary.BigEndian.Uint16(d[14:16]) == etherIPv4 {
			r.ipv4(num, p.ts, d[16:])
		}
	case linkRaw, linkIPv4:
		r.ipv4(num, p.ts, d)
	case linkMSTP:
		r.f.NonIP["ms/tp"]++
	case linkARCNET, linkARCNETL:
		r.f.NonIP["arcnet"]++
	default:
		r.f.NonIP[fmt.Sprintf("link %d", p.link)]++
	}
}

func (r *reader) ethernet(num int, ts time.Time, d []byte) {
	if len(d) < 14 {
		return
	}
	var src, dst [6]byte
	copy(dst[:], d[0:6])
	copy(src[:], d[6:12])
	typ := binary.BigEndian.Uint16(d[12:14])
	if typ == etherVLAN {
		if len(d) < 18 {
			return
		}
		typ = binary.BigEndian.Uint16(d[16:18])
		d = d[4:]
	}
	switch {
	case typ == etherIPv4:
		r.ipv4(num, ts, d[14:])
	case typ <= 1500: // IEEE 802.3 length, LLC follows (clause 7)
		n := int(typ)
		if len(d) < 17 || 14+n > len(d) || n < 3 {
			return
		}
		llc := d[14 : 14+n]
		if llc[0] == 0x82 && llc[1] == 0x82 && llc[2] == 0x03 {
			r.f.Ethernet = append(r.f.Ethernet, EthernetNPDU{Frame: num, Src: src, Dst: dst, Data: llc[3:]})
			r.f.NonIP["ethernet"]++
		}
	}
}

func (r *reader) ipv4(num int, ts time.Time, d []byte) {
	if len(d) < 20 || d[0]>>4 != 4 {
		return
	}
	ihl := int(d[0]&0x0f) * 4
	total := int(binary.BigEndian.Uint16(d[2:4]))
	if ihl < 20 || total < ihl || total > len(d) || d[9] != protoUDP {
		return
	}
	payload := d[ihl:total]
	flags := binary.BigEndian.Uint16(d[6:8])
	if flags&0x3fff != 0 { // a fragment: more fragments or a non-zero offset
		k := fragKey{id: binary.BigEndian.Uint16(d[4:6])}
		copy(k.src[:], d[12:16])
		copy(k.dst[:], d[16:20])
		var ok bool
		if payload, ok = r.fragment(k, d[:ihl], payload, int(flags&0x1fff)*8, flags&0x2000 != 0); !ok {
			return
		}
	}
	if len(payload) < 8 {
		return
	}
	udpLen := int(binary.BigEndian.Uint16(payload[4:6]))
	if udpLen < 8 || udpLen > len(payload) {
		return
	}
	data := payload[8:udpLen]
	src := netip.AddrPortFrom(netip.AddrFrom4([4]byte(d[12:16])), binary.BigEndian.Uint16(payload[0:2]))
	dst := netip.AddrPortFrom(netip.AddrFrom4([4]byte(d[16:20])), binary.BigEndian.Uint16(payload[2:4]))
	if !isBVLL(data, src.Port(), dst.Port()) {
		return
	}
	r.f.IP = append(r.f.IP, Datagram{Frame: num, Time: ts, Src: src, Dst: dst, Data: data})
}

// isBVLL reports whether a UDP payload is a BACnet/IP BVLL message. On the
// BACnet/IP ports (0xBAC0 to 0xBACF, Annex J.1) any payload of type 0x81
// is, even a malformed one; on another port only a well-formed header
// counts (a known function and the BVLC length equal to the payload), so
// that, say, a NetBIOS name query whose transaction ID starts with 0x81 is
// not taken for BACnet.
func isBVLL(b []byte, sport, dport uint16) bool {
	if len(b) < 4 || b[0] != 0x81 {
		return false
	}
	if sport&0xfff0 == 0xbac0 || dport&0xfff0 == 0xbac0 {
		return true
	}
	return b[1] <= 0x0c && int(binary.BigEndian.Uint16(b[2:4])) == len(b)
}

// fragment adds one IPv4 fragment and returns the whole UDP datagram once
// every fragment of it has been seen.
func (r *reader) fragment(k fragKey, hdr, p []byte, off int, more bool) ([]byte, bool) {
	const maxIP = 65535
	if off+len(p) > maxIP {
		return nil, false
	}
	b := r.frags[k]
	if b == nil {
		b = &fragBuf{total: -1}
		r.frags[k] = b
	}
	if off == 0 {
		b.header = hdr
	}
	if end := off + len(p); end > len(b.data) {
		b.data = append(b.data, make([]byte, end-len(b.data))...)
		b.have = append(b.have, make([]bool, end-len(b.have))...)
	}
	copy(b.data[off:], p)
	for i := off; i < off+len(p); i++ {
		b.have[i] = true
	}
	if !more {
		b.total = off + len(p)
	}
	if b.total < 0 || b.header == nil {
		return nil, false
	}
	for _, h := range b.have[:b.total] {
		if !h {
			return nil, false
		}
	}
	delete(r.frags, k)
	r.f.Fragments++
	return b.data[:b.total], true
}
