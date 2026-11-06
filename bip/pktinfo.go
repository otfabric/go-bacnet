// SPDX-License-Identifier: MIT

//go:build linux || darwin

package bip

import (
	"encoding/binary"
	"math"
	"syscall"
)

// interfaceOOB returns the ancillary data (IP_PKTINFO) that makes one
// datagram leave through the interface with the given index, whatever the
// routing table says about its destination. It returns nil when the index
// cannot be encoded; the caller then sends without pinning.
func interfaceOOB(ifindex int) []byte {
	if ifindex < 0 {
		return nil
	}
	if int64(ifindex) > math.MaxUint32 {
		return nil
	}
	oob := make([]byte, syscall.CmsgSpace(syscall.SizeofInet4Pktinfo))
	var h syscall.Cmsghdr
	h.Level = syscall.IPPROTO_IP
	h.Type = syscall.IP_PKTINFO
	h.SetLen(syscall.CmsgLen(syscall.SizeofInet4Pktinfo))
	// The header is fixed-size fields in native order, so it encodes without
	// unsafe; in_pktinfo starts with the interface index, source and
	// destination address stay zero.
	if _, err := binary.Encode(oob, binary.NativeEndian, h); err != nil {
		return nil
	}
	binary.NativeEndian.PutUint32(oob[syscall.CmsgLen(0):], uint32(ifindex))
	return oob
}
