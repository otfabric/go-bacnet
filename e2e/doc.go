// SPDX-License-Identifier: MIT

// Package e2e drives the public API of go-bacnet end to end: a client on a
// real UDP socket against a BACnet/IP device on another, over loopback. The
// device is built from the public codecs only (bvlc, npdu, apdu, service), so
// every frame crosses the wire encoded by one side and decoded by the other.
//
// Independent devices are the interop tier's job; this suite shows that the
// parts of the library work together without Docker.
package e2e
