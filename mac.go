// SPDX-License-Identifier: MIT

package bacnet

import (
	"encoding/hex"
	"fmt"
)

// MaxMACLength is the maximum BACnet MAC length retained for BACnet/IP IPv4
// addressing (covers BACnet/IP 6-octet MAC and common shorter MACs).
const MaxMACLength = 7

// MAC is an immutable, comparable BACnet MAC address.
type MAC struct {
	length uint8
	bytes  [MaxMACLength]byte
}

// NewMAC copies b into a MAC. Empty input yields a zero MAC.
func NewMAC(b []byte) (MAC, error) {
	if len(b) > MaxMACLength {
		return MAC{}, fmt.Errorf("%w: MAC length %d exceeds %d", ErrMalformed, len(b), MaxMACLength)
	}
	var m MAC
	m.length = uint8(len(b))
	copy(m.bytes[:], b)
	return m, nil
}

// MustMAC panics if NewMAC fails.
func MustMAC(b []byte) MAC {
	m, err := NewMAC(b)
	if err != nil {
		panic(err)
	}
	return m
}

// Bytes returns a copy of the MAC octets.
func (m MAC) Bytes() []byte {
	if m.length == 0 {
		return nil
	}
	out := make([]byte, m.length)
	copy(out, m.bytes[:m.length])
	return out
}

// Len returns the MAC length in octets.
func (m MAC) Len() int { return int(m.length) }

// IsZero reports whether the MAC is empty.
func (m MAC) IsZero() bool { return m.length == 0 }

// String returns a hex representation.
func (m MAC) String() string {
	if m.length == 0 {
		return ""
	}
	return hex.EncodeToString(m.bytes[:m.length])
}
