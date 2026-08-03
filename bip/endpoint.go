// SPDX-License-Identifier: MIT

// Package bip provides BACnet/IP data-link endpoint types.
//
// BACnet addresses remain in the root bacnet package. Concrete B/IP endpoints
// live here so the root API stays transport-neutral for future MS/TP, IPv6 and
// BACnet/SC modules.
//
// BACnet/IP is IPv4-only: IsValid requires a usable IPv4 AddrPort. IPv6
// addresses may be constructed but are not treated as valid B/IP endpoints.
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
// BACnet/IP treats only IPv4 endpoints as valid (see IsValid).
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

// IsValid reports whether the endpoint is a usable BACnet/IP IPv4 B/IP endpoint
// (valid IPv4 AddrPort). IPv6 and zero values are not valid.
func (e Endpoint) IsValid() bool {
	return e.Addr.IsValid() && e.Addr.Addr().Is4()
}

// Equal reports whether e and o are the same endpoint.
func (e Endpoint) Equal(o Endpoint) bool { return e.Addr == o.Addr }
