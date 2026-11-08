// SPDX-License-Identifier: MIT

package capture

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Synthetic captures built here exercise the reader without the originals.

var bvll = []byte{0x81, 0x0b, 0x00, 0x0c, 0x01, 0x20, 0xff, 0xff, 0x00, 0xff, 0x10, 0x08} // Original-Broadcast Who-Is

func ipv4(src, dst [4]byte, id uint16, flagsOff uint16, payload []byte) []byte {
	h := make([]byte, 20)
	h[0] = 0x45
	binary.BigEndian.PutUint16(h[2:], uint16(20+len(payload)))
	binary.BigEndian.PutUint16(h[4:], id)
	binary.BigEndian.PutUint16(h[6:], flagsOff)
	h[8], h[9] = 64, protoUDP
	copy(h[12:], src[:])
	copy(h[16:], dst[:])
	return append(h, payload...)
}

func udp(sport, dport uint16, data []byte) []byte {
	h := make([]byte, 8)
	binary.BigEndian.PutUint16(h, sport)
	binary.BigEndian.PutUint16(h[2:], dport)
	binary.BigEndian.PutUint16(h[4:], uint16(8+len(data)))
	return append(h, data...)
}

func ether(typ uint16, payload []byte) []byte {
	h := make([]byte, 14)
	copy(h, []byte{1, 2, 3, 4, 5, 6})
	copy(h[6:], []byte{6, 5, 4, 3, 2, 1})
	binary.BigEndian.PutUint16(h[12:], typ)
	return append(h, payload...)
}

var (
	ipA = [4]byte{10, 0, 0, 1}
	ipB = [4]byte{10, 0, 0, 2}
)

func pcapFile(bo binary.ByteOrder, magic uint32, link uint32, frames ...[]byte) []byte {
	b := make([]byte, 24)
	bo.PutUint32(b, magic)
	bo.PutUint32(b[20:], link)
	for i, f := range frames {
		h := make([]byte, 16)
		bo.PutUint32(h, uint32(1000+i))
		bo.PutUint32(h[4:], 500)
		bo.PutUint32(h[8:], uint32(len(f)))
		bo.PutUint32(h[12:], uint32(len(f)))
		b = append(append(b, h...), f...)
	}
	return b
}

func block(typ uint32, body []byte) []byte {
	for len(body)%4 != 0 {
		body = append(body, 0)
	}
	n := uint32(12 + len(body))
	b := binary.LittleEndian.AppendUint32(nil, typ)
	b = binary.LittleEndian.AppendUint32(b, n)
	b = append(b, body...)
	return binary.LittleEndian.AppendUint32(b, n)
}

func pcapngFile(link uint16, tsresol byte, frames ...[]byte) []byte {
	shb := block(0x0a0d0d0a, []byte{0x4d, 0x3c, 0x2b, 0x1a, 1, 0, 0, 0, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
	idb := binary.LittleEndian.AppendUint16(nil, link)
	idb = append(idb, 0, 0, 0, 0, 0, 0) // reserved, snaplen
	if tsresol != 0 {
		idb = append(idb, 9, 0, 1, 0, tsresol, 0, 0, 0) // if_tsresol
		idb = append(idb, 0, 0, 0, 0)                   // opt_endofopt
	}
	out := append(shb, block(1, idb)...)
	for i, f := range frames {
		epb := binary.LittleEndian.AppendUint32(nil, 0)
		epb = binary.LittleEndian.AppendUint32(epb, 0)
		epb = binary.LittleEndian.AppendUint32(epb, uint32(1000+i))
		epb = binary.LittleEndian.AppendUint32(epb, uint32(len(f)))
		epb = binary.LittleEndian.AppendUint32(epb, uint32(len(f)))
		out = append(out, block(6, append(epb, f...))...)
	}
	spb := binary.LittleEndian.AppendUint32(nil, uint32(len(frames[0])))
	return append(out, block(3, append(spb, frames[0]...))...)
}

func TestReadEthernetVariants(t *testing.T) {
	plain := ether(etherIPv4, ipv4(ipA, ipB, 1, 0, udp(47808, 47808, bvll)))
	vlan := ether(etherVLAN, append([]byte{0x00, 0x05, 0x08, 0x00}, ipv4(ipA, ipB, 2, 0, udp(47809, 47808, bvll))...))
	llc := ether(uint16(3+len(bvll[4:])), append([]byte{0x82, 0x82, 0x03}, bvll[4:]...))
	netbios := ether(etherIPv4, ipv4(ipA, ipB, 3, 0, udp(137, 137, []byte{0x81, 0xd3, 0x01, 0x10, 0, 1, 0, 0})))
	other := ether(etherIPv4, ipv4(ipA, ipB, 4, 0, udp(5000, 5001, bvll))) // well-formed BVLL elsewhere
	short := ether(etherIPv4, []byte{0x45})
	arp := ether(0x0806, make([]byte, 28))
	// One UDP datagram in two fragments, the second first.
	whole := udp(47808, 47808, append(append([]byte(nil), bvll[:2]...), 0x00, 0x18, 0x01, 0x20, 0xff, 0xff, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15))
	fragB := ether(etherIPv4, ipv4(ipA, ipB, 9, 2, whole[16:]))
	fragA := ether(etherIPv4, ipv4(ipA, ipB, 9, 0x2000, whole[:16]))

	for name, raw := range map[string][]byte{
		"pcap LE":      pcapFile(binary.LittleEndian, 0xa1b2c3d4, linkEthernet, plain, vlan, llc, netbios, other, short, arp, fragB, fragA),
		"pcap BE nano": pcapFile(binary.BigEndian, 0xa1b23c4d, linkEthernet, plain, vlan, llc, netbios, other, short, arp, fragB, fragA),
		"pcapng":       pcapngFile(linkEthernet, 9, plain, vlan, llc, netbios, other, short, arp, fragB, fragA),
	} {
		f, err := Read(raw)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if f.Frames != 9 && f.Frames != 10 {
			t.Fatalf("%s: %d frames", name, f.Frames)
		}
		want := 4
		if name == "pcapng" {
			want = 5 // and the simple packet block repeating the first frame
		}
		if len(f.IP) != want || f.Fragments != 1 || len(f.Ethernet) != 1 || f.NonIP["ethernet"] != 1 {
			t.Fatalf("%s: %+v", name, f)
		}
		if f.IP[0].Src.Port() != 47808 || f.IP[1].Src.Port() != 47809 || f.IP[2].Src.Port() != 5000 || f.IP[3].Frame != 9 || len(f.IP[3].Data) != len(whole)-8 {
			t.Fatalf("%s: %+v", name, f.IP)
		}
		if f.IP[0].Time.IsZero() {
			t.Fatalf("%s: no time stamp", name)
		}
	}
}

func TestReadOtherLinks(t *testing.T) {
	ip := ipv4(ipA, ipB, 1, 0, udp(47808, 47808, bvll))
	sll := append(make([]byte, 14), 0x08, 0x00)
	for _, c := range []struct {
		link  uint32
		frame []byte
		ip    int
		nonIP string
	}{
		{linkSLL, append(sll, ip...), 1, ""},
		{linkRaw, ip, 1, ""},
		{linkIPv4, ip, 1, ""},
		{linkMSTP, []byte{0x55, 0xff}, 0, "ms/tp"},
		{linkARCNETL, []byte{1}, 0, "arcnet"},
		{147, []byte{1}, 0, "link 147"},
	} {
		f, err := Read(pcapFile(binary.LittleEndian, 0xa1b2c3d4, c.link, c.frame))
		if err != nil {
			t.Fatal(err)
		}
		if len(f.IP) != c.ip || (c.nonIP != "" && f.NonIP[c.nonIP] != 1) {
			t.Errorf("link %d: %+v", c.link, f)
		}
	}
}

func TestReadMalformed(t *testing.T) {
	good := pcapFile(binary.LittleEndian, 0xa1b2c3d4, linkEthernet, []byte{1, 2, 3})
	ng := pcapngFile(linkEthernet, 0, []byte{1, 2, 3})
	for name, raw := range map[string][]byte{
		"empty":            nil,
		"unknown magic":    {1, 2, 3, 4, 5, 6},
		"pcap header":      good[:20],
		"pcap record":      good[:30],
		"pcap length":      good[:len(good)-1],
		"pcapng short":     ng[:8],
		"pcapng length":    ng[:len(ng)-4],
		"pcapng bad magic": append(append([]byte(nil), ng[:8]...), 0, 0, 0, 0, 12, 0, 0, 0),
	} {
		if _, err := Read(raw); !errors.Is(err, ErrFormat) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// An enhanced packet naming an interface that was not described.
	bad := append(pcapngFile(linkEthernet, 0, []byte{1})[:28], block(6, make([]byte, 20))...)
	if _, err := Read(bad); !errors.Is(err, ErrFormat) {
		t.Errorf("unknown interface: %v", err)
	}
}

func TestReadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.pcap")
	if err := os.WriteFile(path, pcapFile(binary.LittleEndian, 0xa1b2c3d4, linkRaw, ipv4(ipA, ipB, 1, 0, udp(47808, 47808, bvll))), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := ReadFile(path)
	if err != nil || len(f.IP) != 1 {
		t.Fatalf("%v %+v", err, f)
	}
	if want := time.Unix(1000, 500000).UTC(); !f.IP[0].Time.Equal(want) {
		t.Errorf("time %v, want %v", f.IP[0].Time, want)
	}
	if _, err := ReadFile(filepath.Join(dir, "missing")); err == nil {
		t.Error("missing file read")
	}
	if _, err := ReadFile(filepath.Join(dir, "no", "dir")); err == nil {
		t.Error("missing directory read")
	}
}

// The decoder reassembles a segmented ComplexACK only in order and from
// segment 0, and reports refusals by layer.
func TestDecoderSegmentsAndLayers(t *testing.T) {
	seg := func(seq uint8, more bool) Datagram {
		ctl := byte(0x38) // ComplexACK, segmented
		if more {
			ctl |= 0x04 // more follows
		}
		apduB := []byte{ctl, 7, seq, 4, 12}
		apduB = append(apduB, []byte{0x0c, 0x00, 0x80, 0x00, 0x01, 0x19, 0x55, 0x3e, 0x44, 0x41, 0xac, 0x00, 0x00, 0x3f}[seq*7:seq*7+7]...)
		data := append([]byte{0x81, 0x0a, 0, 0, 0x01, 0x00}, apduB...)
		binary.BigEndian.PutUint16(data[2:], uint16(len(data)))
		return Datagram{Frame: int(seq) + 1, Data: data}
	}
	d := NewDecoder()
	if m := d.Decode(seg(1, false)); !m.Segment || m.Reassembled || m.Service != nil {
		t.Fatalf("segment 1 without 0: %+v", m)
	}
	if m := d.Decode(seg(0, true)); !m.Segment || m.Service != nil {
		t.Fatalf("segment 0: %+v", m)
	}
	if m := d.Decode(seg(1, false)); !m.Reassembled || m.Err != nil || m.Service == nil {
		t.Fatalf("segment 1: %+v", m)
	}
	for layer, data := range map[Layer][]byte{
		LayerBVLC: {0x81, 0x0a, 0x00, 0x09, 0x01},
		LayerNPDU: {0x81, 0x0a, 0x00, 0x05, 0x02},
		LayerAPDU: {0x81, 0x0a, 0x00, 0x07, 0x01, 0x00, 0x80},
	} {
		if m := d.Decode(Datagram{Data: data}); m.ErrLayer != layer || m.Err == nil {
			t.Errorf("%s: %+v", layer, m)
		}
	}
	if m := d.Decode(Datagram{Data: []byte{0x81, 0x00, 0x00, 0x06, 0x00, 0x00}}); m.Err != nil || m.NPDU != nil {
		t.Errorf("BVLC-Result: %+v", m)
	}
	if _, _, err := Reencode(Message{Err: ErrFormat}); err == nil {
		t.Error("refused message re-encoded")
	}
}

func TestAnalyzeAndCacheDir(t *testing.T) {
	bad := []byte{0x81, 0x0a, 0x00, 0x05, 0x02}
	f, err := Read(pcapFile(binary.LittleEndian, 0xa1b2c3d4, linkRaw,
		ipv4(ipA, ipB, 1, 0, udp(47808, 47808, bvll)), ipv4(ipA, ipB, 2, 0, udp(47808, 47808, bad))))
	if err != nil {
		t.Fatal(err)
	}
	st, msgs := Analyze(f)
	if st.Datagrams != 2 || st.Decoded != 1 || st.Rejected[string(LayerNPDU)] != 1 || len(msgs) != 2 || st.Transactions != 0 {
		t.Fatalf("%+v", st)
	}
	t.Setenv("BACNET_CAPTURE_CACHE", "/elsewhere")
	if CacheDir() != "/elsewhere" {
		t.Error(CacheDir())
	}
	t.Setenv("BACNET_CAPTURE_CACHE", "")
	if filepath.Base(CacheDir()) != "cache" {
		t.Error(CacheDir())
	}
	if err := Report("no-such-part", 1); err == nil {
		t.Error("missing report part accepted")
	}
}
