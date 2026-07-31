// SPDX-License-Identifier: MIT

// Package bip provides BACnet/IP data-link endpoint types.
//
// BACnet addresses remain in the root bacnet package. Concrete B/IP endpoints
// live here so the root API stays transport-neutral for future MS/TP, IPv6 and
// BACnet/SC modules.
package bip

import (
	"net/netip"
)

// DefaultPort is the BACnet/IP UDP port (47808 / 0xBAC0).
const DefaultPort = 47808

// Endpoint is a BACnet/IP UDP endpoint.
type Endpoint struct {
	Addr netip.AddrPort
}

// NewEndpoint returns an Endpoint for addr.
func NewEndpoint(addr netip.AddrPort) Endpoint {
	return Endpoint{Addr: addr}
}

// String returns the AddrPort string.
func (e Endpoint) String() string {
	if !e.Addr.IsValid() {
		return ""
	}
	return e.Addr.String()
}

// IsValid reports whether the endpoint has a valid address.
func (e Endpoint) IsValid() bool { return e.Addr.IsValid() }

// Equal reports whether e and o are the same endpoint.
func (e Endpoint) Equal(o Endpoint) bool { return e.Addr == o.Addr }
