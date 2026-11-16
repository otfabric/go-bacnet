// SPDX-License-Identifier: MIT

//go:build !linux && !darwin

package bip

import "testing"

// TestInterfaceOOBParses checks that interfaceOOB encodes nothing where
// pinning a broadcast to its interface is not supported (pktinfo_other.go).
func TestInterfaceOOBParses(t *testing.T) {
	if interfaceOOB(1) != nil {
		t.Fatal("interfaceOOB must be nil where pinning is unsupported")
	}
}
