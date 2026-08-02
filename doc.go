// SPDX-License-Identifier: MIT

// Package bacnet provides transport-neutral BACnet leaf types and helpers.
//
// This package holds addresses, object/property identifiers, application
// values, decode limits and error sentinels. It does not open sockets and does
// not import BVLC, NPDU, APDU, service or client packages.
//
// Wire codecs live in sibling packages (bvlc, npdu, apdu). The client runtime
// composes those layers. BACnet/IP endpoints live in package bip.
//
// The library is a BACnet/IP IPv4 supervisory client developed against
// ANSI/ASHRAE 135-2024 (Protocol Revision 31 baseline). The client adapts to
// known remote capabilities and does not require Revision 31 features from
// remote devices.
//
// Out of scope:
//
//   - native MS/TP, BACnet/IPv6, BACnet/SC
//   - full BBMD server / multi-BBMD failover
//   - full BACnet server/device object model
//   - BTL certification
//
// Status: container-interoperability validated against pinned open-source
// peers; field validation pending. See docs/CLIENT_SUPPORT.md and INTEROP.md.
package bacnet
