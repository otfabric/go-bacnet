// SPDX-License-Identifier: MIT

//go:build linux || darwin

package bip

import (
	"syscall"
	"testing"
)

// TestInterfaceOOBParses checks the IP_PKTINFO control message that pins a
// broadcast to its interface, where interfaceOOB encodes one (pktinfo.go).
func TestInterfaceOOBParses(t *testing.T) {
	if interfaceOOB(-1) != nil {
		t.Error("negative ifindex encoded")
	}
	oob := interfaceOOB(7)
	msgs, err := syscall.ParseSocketControlMessage(oob)
	if err != nil {
		t.Fatalf("ParseSocketControlMessage: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("got %d control messages, want 1", len(msgs))
	}
	m := msgs[0]
	if m.Header.Level != syscall.IPPROTO_IP || m.Header.Type != syscall.IP_PKTINFO {
		t.Fatalf("header level/type = %d/%d", m.Header.Level, m.Header.Type)
	}
	if len(m.Data) < 4 || m.Data[0]|m.Data[1]|m.Data[2]|m.Data[3] == 0 {
		t.Fatalf("pktinfo data %x does not carry the interface index", m.Data)
	}
}
