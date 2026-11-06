// SPDX-License-Identifier: MIT

package client

import (
	"errors"
	"net/netip"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
)

func TestMatchTargetSourceMismatches(t *testing.T) {
	mac := bacnet.MustMAC([]byte{10, 0, 0, 2, 0xBA, 0xC0})
	hop := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.1:47808"))
	origin := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.5:47808"))
	other := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.9:47808"))
	// A remote station answers from its own network and MAC only.
	remote := Target{Address: bacnet.RemoteStation(2, mac), Endpoint: hop}
	if matchTargetSource(remote, packetSource{bacnetAddress: bacnet.RemoteStation(3, mac), immediate: hop}) {
		t.Error("remote station on another network accepted")
	}
	// A forwarded target needs the claimed origin.
	fwd := Target{Address: bacnet.LocalStation(mac), Endpoint: hop, Origin: origin}
	if matchTargetSource(fwd, packetSource{bacnetAddress: bacnet.LocalStation(mac), immediate: hop}) {
		t.Error("forwarded target answered without an origin")
	}
	if matchTargetSource(fwd, packetSource{bacnetAddress: bacnet.LocalStation(mac), origin: other, immediate: hop}) {
		t.Error("forwarded target answered from another origin")
	}
}

func TestAdvertisedMaxAPDUDefaults(t *testing.T) {
	if err := validateAdvertisedMaxAPDU(0, 0); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("parser max 0: %v", err)
	}
	// Neither an advertised size nor a parser limit: the B/IP maximum.
	var c Client
	if max, err := c.advertisedMaxAPDU(); err != nil || max != 1476 {
		t.Fatalf("max=%d err=%v", max, err)
	}
}
