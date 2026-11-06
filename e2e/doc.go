// SPDX-License-Identifier: MIT

// Package e2e drives the public API of go-bacnet end to end: a client on a
// real UDP socket against a BACnet/IP device on another, over loopback. The
// device is built from the public codecs only (bvlc, npdu, apdu, service), so
// every frame crosses the wire encoded by one side and decoded by the other.
//
// The server_*_test.go files reverse the roles: the go-bacnet client against
// the go-bacnet server (package server) over loopback.
//
// Independent devices and clients are the interop tier's job; this suite
// shows that the parts of the library work together without Docker.
package e2e
