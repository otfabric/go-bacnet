// SPDX-License-Identifier: MIT

package client

import (
	"net/netip"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
)

func TestMatchTargetSourceRemoteRequiresNextHop(t *testing.T) {
	remote := bacnet.RemoteStation(2, bacnet.MustMAC([]byte{10, 0, 0, 2, 0xBA, 0xC0}))
	hopA := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.1:47808"))
	hopB := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.2:47808"))
	target := Target{Address: remote, Endpoint: hopA}

	ok := matchTargetSource(target, packetSource{
		bacnetAddress: remote,
		immediate:     hopA,
	})
	if !ok {
		t.Fatal("expected match for correct next hop")
	}
	bad := matchTargetSource(target, packetSource{
		bacnetAddress: remote,
		immediate:     hopB,
	})
	if bad {
		t.Fatal("accepted remote SADR via wrong next hop")
	}
}

func TestMatchTargetSourceLocalOrigin(t *testing.T) {
	local := bacnet.LocalStation(bacnet.MustMAC([]byte{1, 2, 3, 4, 0xBA, 0xC0}))
	origin := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.5:47808"))
	bbmd := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.1:47808"))
	other := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.9:47808"))
	target := Target{Address: local, Endpoint: bbmd, Origin: origin}

	if !matchTargetSource(target, packetSource{
		bacnetAddress: local,
		origin:        origin,
		immediate:     bbmd,
	}) {
		t.Fatal("expected forwarded path match")
	}
	if matchTargetSource(target, packetSource{
		bacnetAddress: local,
		origin:        origin,
		immediate:     other,
	}) {
		t.Fatal("accepted forwarded NPDU from wrong BBMD hop")
	}
}

func TestMatchTargetSourceLocalEndpointOnly(t *testing.T) {
	local := bacnet.LocalStation(bacnet.MustMAC([]byte{10, 0, 0, 2, 0xBA, 0xC0}))
	peer := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.2:47808"))
	other := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.99:47808"))
	target := Target{Address: local, Endpoint: peer}

	if !matchTargetSource(target, packetSource{bacnetAddress: local, immediate: peer}) {
		t.Fatal("expected local endpoint match")
	}
	if matchTargetSource(target, packetSource{bacnetAddress: local, immediate: other}) {
		t.Fatal("wrong hop should not match")
	}
}

func TestMatchTargetSourceRemoteBroadcast(t *testing.T) {
	dnet := uint16(3)
	remote := bacnet.RemoteBroadcast(dnet)
	hop := bip.NewEndpoint(netip.MustParseAddrPort("192.168.0.1:47808"))
	target := Target{Address: remote, Endpoint: hop}
	if !matchTargetSource(target, packetSource{bacnetAddress: remote, immediate: hop}) {
		t.Fatal("expected remote broadcast match")
	}
}
