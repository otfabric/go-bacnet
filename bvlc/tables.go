// SPDX-License-Identifier: MIT

package bvlc

import (
	"encoding/binary"
	"fmt"
	"net/netip"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
)

// IPv4Mask is a BACnet/IP broadcast distribution mask (4 octets).
type IPv4Mask [4]byte

// Contiguous reports whether the mask is a contiguous left-justified bit mask
// (standard subnet-style). All-zero and all-ones are allowed.
func (m IPv4Mask) Contiguous() bool {
	seenZero := false
	for _, b := range m {
		for i := 7; i >= 0; i-- {
			bit := (b >> uint(i)) & 1
			if bit == 0 {
				seenZero = true
			} else if seenZero {
				return false
			}
		}
	}
	return true
}

// Addr returns the mask as an IPv4 netip.Addr.
func (m IPv4Mask) Addr() netip.Addr {
	return netip.AddrFrom4(m)
}

// BDTEntry is one Broadcast Distribution Table entry (IP + port + mask).
type BDTEntry struct {
	Endpoint bip.Endpoint
	Mask     IPv4Mask
}

// FDTEntry is one Foreign Device Table entry.
type FDTEntry struct {
	Address   bip.Endpoint
	TTL       uint16
	Remaining uint16
}

// NewBDTEntry validates and constructs a BDT entry.
func NewBDTEntry(ep bip.Endpoint, mask IPv4Mask) (BDTEntry, error) {
	if !ep.IsValid() {
		return BDTEntry{}, fmt.Errorf("%w: BDT endpoint must be IPv4", bacnet.ErrMalformed)
	}
	if !mask.Contiguous() {
		return BDTEntry{}, fmt.Errorf("%w: BDT mask must be contiguous", bacnet.ErrMalformed)
	}
	return BDTEntry{Endpoint: ep, Mask: mask}, nil
}

// EncodeBDTEntries appends the wire encoding of BDT entries (10 octets each).
func EncodeBDTEntries(dst []byte, entries []BDTEntry) ([]byte, error) {
	for i, e := range entries {
		if !e.Endpoint.IsValid() {
			return dst, fmt.Errorf("%w: BDT entry %d invalid endpoint", bacnet.ErrMalformed, i)
		}
		if !e.Mask.Contiguous() {
			return dst, fmt.Errorf("%w: BDT entry %d non-contiguous mask", bacnet.ErrMalformed, i)
		}
		ip := e.Endpoint.Addr.Addr().As4()
		dst = append(dst, ip[:]...)
		port := e.Endpoint.Addr.Port()
		dst = append(dst, byte(port>>8), byte(port))
		dst = append(dst, e.Mask[:]...)
	}
	return dst, nil
}

// DecodeBDTEntries parses a BDT Ack / Write-BDT payload.
// Duplicate entries are wire-valid and returned as-is.
func DecodeBDTEntries(payload []byte, limits bacnet.DecodeLimits) ([]BDTEntry, error) {
	limits = limits.Normalize()
	if len(payload)%10 != 0 {
		return nil, fmt.Errorf("%w: BDT payload length %d", bacnet.ErrMalformed, len(payload))
	}
	n := len(payload) / 10
	if n > limits.MaxElements {
		return nil, fmt.Errorf("%w: BDT entry count", bacnet.ErrLimitExceeded)
	}
	out := make([]BDTEntry, 0, n)
	for i := 0; i < n; i++ {
		off := i * 10
		var ip [4]byte
		copy(ip[:], payload[off:off+4])
		port := binary.BigEndian.Uint16(payload[off+4 : off+6])
		var mask IPv4Mask
		copy(mask[:], payload[off+6:off+10])
		ep := bip.NewEndpoint(netip.AddrPortFrom(netip.AddrFrom4(ip), port))
		if !ep.IsValid() {
			return nil, fmt.Errorf("%w: BDT entry %d invalid endpoint", bacnet.ErrMalformed, i)
		}
		out = append(out, BDTEntry{Endpoint: ep, Mask: mask})
	}
	return out, nil
}

// EncodeFDTEntries appends the wire encoding of FDT entries (10 octets each).
func EncodeFDTEntries(dst []byte, entries []FDTEntry) ([]byte, error) {
	for i, e := range entries {
		if !e.Address.IsValid() {
			return dst, fmt.Errorf("%w: FDT entry %d invalid address", bacnet.ErrMalformed, i)
		}
		ip := e.Address.Addr.Addr().As4()
		dst = append(dst, ip[:]...)
		port := e.Address.Addr.Port()
		dst = append(dst, byte(port>>8), byte(port))
		dst = append(dst, byte(e.TTL>>8), byte(e.TTL))
		dst = append(dst, byte(e.Remaining>>8), byte(e.Remaining))
	}
	return dst, nil
}

// DecodeFDTEntries parses a Read-FDT-Ack payload.
func DecodeFDTEntries(payload []byte, limits bacnet.DecodeLimits) ([]FDTEntry, error) {
	limits = limits.Normalize()
	if len(payload)%10 != 0 {
		return nil, fmt.Errorf("%w: FDT payload length %d", bacnet.ErrMalformed, len(payload))
	}
	n := len(payload) / 10
	if n > limits.MaxElements {
		return nil, fmt.Errorf("%w: FDT entry count", bacnet.ErrLimitExceeded)
	}
	out := make([]FDTEntry, 0, n)
	for i := 0; i < n; i++ {
		off := i * 10
		var ip [4]byte
		copy(ip[:], payload[off:off+4])
		port := binary.BigEndian.Uint16(payload[off+4 : off+6])
		ttl := binary.BigEndian.Uint16(payload[off+6 : off+8])
		rem := binary.BigEndian.Uint16(payload[off+8 : off+10])
		ep := bip.NewEndpoint(netip.AddrPortFrom(netip.AddrFrom4(ip), port))
		if !ep.IsValid() {
			return nil, fmt.Errorf("%w: FDT entry %d invalid address", bacnet.ErrMalformed, i)
		}
		out = append(out, FDTEntry{Address: ep, TTL: ttl, Remaining: rem})
	}
	return out, nil
}

// EncodeDeleteFDTEntry encodes the 6-octet Delete-FDT-Entry body.
func EncodeDeleteFDTEntry(dst []byte, ep bip.Endpoint) ([]byte, error) {
	if !ep.IsValid() {
		return dst, fmt.Errorf("%w: Delete-FDT endpoint must be IPv4", bacnet.ErrMalformed)
	}
	ip := ep.Addr.Addr().As4()
	dst = append(dst, ip[:]...)
	port := ep.Addr.Port()
	dst = append(dst, byte(port>>8), byte(port))
	return dst, nil
}

// DecodeDeleteFDTEntry parses a Delete-FDT-Entry body.
func DecodeDeleteFDTEntry(payload []byte) (bip.Endpoint, error) {
	if len(payload) != 6 {
		return bip.Endpoint{}, fmt.Errorf("%w: Delete-FDT length", bacnet.ErrMalformed)
	}
	var ip [4]byte
	copy(ip[:], payload[:4])
	port := binary.BigEndian.Uint16(payload[4:6])
	ep := bip.NewEndpoint(netip.AddrPortFrom(netip.AddrFrom4(ip), port))
	if !ep.IsValid() {
		return bip.Endpoint{}, fmt.Errorf("%w: Delete-FDT invalid endpoint", bacnet.ErrMalformed)
	}
	return ep, nil
}
