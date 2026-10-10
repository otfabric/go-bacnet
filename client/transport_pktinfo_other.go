// SPDX-License-Identifier: MIT

//go:build !linux && !darwin

package client

// interfaceOOB returns nil: on this platform a datagram cannot be pinned to
// an interface, and the limited broadcast goes where the routing table sends
// it.
func interfaceOOB(int) []byte { return nil }
