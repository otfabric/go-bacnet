// SPDX-License-Identifier: MIT

package bip_test

import (
	"net/netip"
	"testing"

	"github.com/otfabric/go-bacnet/bip"
)

func TestEndpointBasics(t *testing.T) {
	var zero bip.Endpoint
	if zero.IsValid() || zero.String() != "" {
		t.Fatalf("zero endpoint: valid=%v string=%q", zero.IsValid(), zero.String())
	}

	ap := netip.MustParseAddrPort("192.0.2.10:47808")
	ep := bip.NewEndpoint(ap)
	if !ep.IsValid() || ep.String() != ap.String() {
		t.Fatalf("got valid=%v string=%q", ep.IsValid(), ep.String())
	}
	if !ep.Equal(bip.NewEndpoint(ap)) {
		t.Fatal("equal same endpoint")
	}
	other := bip.NewEndpoint(netip.MustParseAddrPort("192.0.2.11:47808"))
	if ep.Equal(other) {
		t.Fatal("equal different endpoint")
	}
	if bip.DefaultPort != 47808 {
		t.Fatalf("DefaultPort=%d", bip.DefaultPort)
	}
}
