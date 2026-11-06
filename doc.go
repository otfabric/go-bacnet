// SPDX-License-Identifier: MIT

// Package bacnet provides transport-neutral BACnet leaf types and helpers.
//
// This package holds addresses, object/property identifiers, application
// values, decode limits and error sentinels. It does not open sockets and does
// not import BVLC, NPDU, APDU, service or client packages.
//
// Wire codecs live in sibling packages (bvlc, npdu, apdu, service).
// BACnet/IP endpoints and the UDP transport live in package bip.
//
// The module is a BACnet/IP (IPv4) client and server developed against
// ANSI/ASHRAE 135-2024 (Protocol Revision 31 baseline):
//
//   - package client is a supervisory client; it adapts to known remote
//     capabilities and does not require Revision 31 features from remote
//     devices;
//   - package server serves a device of your own, with the object model in
//     package server/object and an optional BBMD in package bbmd.
//
// Out of scope: MS/TP, BACnet/IPv6, BACnet/SC, routing between networks,
// multi-BBMD failover in the client, BTL certification.
//
// Status: the client is tested against pinned open-source peers in
// containers, the server against the go-bacnet client and the bacnet-stack
// command-line tools; no physical devices yet. See docs/CLIENT_SUPPORT.md,
// docs/SERVER_SUPPORT.md and INTEROP.md.
package bacnet
