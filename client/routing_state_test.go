// SPDX-License-Identifier: MIT

package client

import (
	"net/netip"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/internal/clock"
	"github.com/otfabric/go-bacnet/internal/virtual"
	"github.com/otfabric/go-bacnet/npdu"
)

func injectNetMsg(t *testing.T, tr *virtual.Transport, clk *clock.Manual, from bip.Endpoint, msgType uint8, data []byte) {
	t.Helper()
	nraw, err := npdu.Append(nil, npdu.NPDU{
		Version: npdu.Version1, NetworkMessage: true, NetMsgType: msgType, NetMsgData: data,
	})
	if err != nil {
		t.Fatal(err)
	}
	frame, err := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionOriginalUnicastNPDU, Payload: nraw})
	if err != nil {
		t.Fatal(err)
	}
	tr.Inject(virtual.InboundPacket{Data: frame, ImmediatePeer: from, ReceivedAt: clk.Now()})
	time.Sleep(20 * time.Millisecond)
}

func TestRouteBusySelectsAlternate(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	local := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.2:47808"))
	r1 := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.1:47808"))
	r2 := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.3:47808"))
	tr := virtual.New(local, clk, 16)
	c, err := New(WithTransport(AdaptVirtual(tr)), withClock(clk))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	injectNetMsg(t, tr, clk, r1, npdu.NetMsgIAmRouterToNetwork, []byte{0x00, 0x02})
	injectNetMsg(t, tr, clk, r2, npdu.NetMsgIAmRouterToNetwork, []byte{0x00, 0x02})
	injectNetMsg(t, tr, clk, r1, npdu.NetMsgRouterBusyToNetwork, []byte{0x00, 0x02})

	remote := bacnet.RemoteStation(2, bacnet.MustMAC([]byte{10, 0, 0, 5, 0xBA, 0xC0}))
	got, err := c.ResolveTarget(remote, bip.Endpoint{})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Endpoint.Equal(r2) {
		t.Fatalf("got %v want alternate %v; routes=%v", got.Endpoint, r2, c.Routes(2))
	}
}

func TestRouteAllBusyThenAvailable(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	local := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.2:47808"))
	r1 := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.1:47808"))
	tr := virtual.New(local, clk, 16)
	c, err := New(WithTransport(AdaptVirtual(tr)), withClock(clk))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	injectNetMsg(t, tr, clk, r1, npdu.NetMsgIAmRouterToNetwork, []byte{0x00, 0x02})
	injectNetMsg(t, tr, clk, r1, npdu.NetMsgRouterBusyToNetwork, []byte{0x00, 0x02})
	remote := bacnet.RemoteStation(2, bacnet.MustMAC([]byte{10, 0, 0, 5, 0xBA, 0xC0}))
	if _, err := c.ResolveTarget(remote, bip.Endpoint{}); err == nil {
		t.Fatal("expected unsupported when all routes busy")
	}
	injectNetMsg(t, tr, clk, r1, npdu.NetMsgRouterAvailableToNetwork, []byte{0x00, 0x02})
	got, err := c.ResolveTarget(remote, bip.Endpoint{})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Endpoint.Equal(r1) {
		t.Fatalf("got %v want %v", got.Endpoint, r1)
	}
}

func TestRouteRejectMarksReason(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	local := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.2:47808"))
	r1 := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.1:47808"))
	tr := virtual.New(local, clk, 16)
	c, err := New(WithTransport(AdaptVirtual(tr)), withClock(clk))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	injectNetMsg(t, tr, clk, r1, npdu.NetMsgIAmRouterToNetwork, []byte{0x00, 0x02})
	// Reject-Message-To-Network: reason=1, network=2
	injectNetMsg(t, tr, clk, r1, npdu.NetMsgRejectMessageToNetwork, []byte{0x01, 0x00, 0x02})
	routes := c.Routes(2)
	if len(routes) != 1 || routes[0].State != RouteRejected || routes[0].RejectReason != 1 {
		t.Fatalf("routes=%#v", routes)
	}
}

func TestRouteMalformedNetworkMessages(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	local := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.2:47808"))
	r1 := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.1:47808"))
	tr := virtual.New(local, clk, 16)
	c, err := New(WithTransport(AdaptVirtual(tr)), withClock(clk))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	// Odd-length network lists / truncated bodies hit malformed diag paths.
	injectNetMsg(t, tr, clk, r1, npdu.NetMsgRouterBusyToNetwork, []byte{0x00})
	injectNetMsg(t, tr, clk, r1, npdu.NetMsgRouterAvailableToNetwork, []byte{0x00})
	injectNetMsg(t, tr, clk, r1, npdu.NetMsgRejectMessageToNetwork, []byte{0x01})
	injectNetMsg(t, tr, clk, r1, npdu.NetMsgICouldBeRouterToNetwork, []byte{0x00})
}

func TestResolveTargetStableAcrossLaterRouteChange(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	local := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.2:47808"))
	r1 := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.1:47808"))
	r2 := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.3:47808"))
	tr := virtual.New(local, clk, 16)
	c, err := New(WithTransport(AdaptVirtual(tr)), withClock(clk))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	injectNetMsg(t, tr, clk, r1, npdu.NetMsgIAmRouterToNetwork, []byte{0x00, 0x02})
	remote := bacnet.RemoteStation(2, bacnet.MustMAC([]byte{10, 0, 0, 5, 0xBA, 0xC0}))
	first, err := c.ResolveTarget(remote, bip.Endpoint{})
	if err != nil {
		t.Fatal(err)
	}
	if !first.Endpoint.Equal(r1) {
		t.Fatalf("first=%v", first.Endpoint)
	}
	// Later learning does not mutate the already-selected Target value.
	injectNetMsg(t, tr, clk, r2, npdu.NetMsgIAmRouterToNetwork, []byte{0x00, 0x02})
	if !first.Endpoint.Equal(r1) {
		t.Fatalf("in-flight target mutated to %v", first.Endpoint)
	}
}
