// SPDX-License-Identifier: MIT

//go:build linux || darwin

package client

import (
	"encoding/binary"
	"syscall"
	"unsafe"
)

// interfaceOOB returns the ancillary data (IP_PKTINFO) that makes one
// datagram leave through the interface with the given index, whatever the
// routing table says about its destination.
func interfaceOOB(ifindex int) []byte {
	oob := make([]byte, syscall.CmsgSpace(syscall.SizeofInet4Pktinfo))
	h := (*syscall.Cmsghdr)(unsafe.Pointer(&oob[0]))
	h.Level = syscall.IPPROTO_IP
	h.Type = syscall.IP_PKTINFO
	h.SetLen(syscall.CmsgLen(syscall.SizeofInet4Pktinfo))
	// in_pktinfo starts with the interface index; source and destination
	// address stay zero.
	binary.NativeEndian.PutUint32(oob[syscall.CmsgLen(0):], uint32(ifindex))
	return oob
}
