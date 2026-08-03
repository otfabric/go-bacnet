// SPDX-License-Identifier: MIT

package client

import (
	"net/netip"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/internal/clock"
	"github.com/otfabric/go-bacnet/internal/virtual"
	"github.com/otfabric/go-bacnet/npdu"
)

func TestRouterCacheEnforceGlobalAndExpiry(t *testing.T) {
	r := newRouterCache()
	r.maxGlobal = 3
	r.maxPerNet = 8
	r.ttl = time.Minute
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		ep := bip.NewEndpoint(netip.MustParseAddrPort(netip.AddrFrom4([4]byte{10, 0, 0, byte(i + 1)}).String() + ":47808"))
		r.upsertLearned(uint16(i+1), ep, bip.Endpoint{}, now.Add(time.Duration(i)*time.Second))
	}
	total := 0
	for n := uint16(1); n <= 5; n++ {
		total += len(r.routes(n))
	}
	if total > 3 {
		t.Fatalf("global cap failed: %d", total)
	}
	ep := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.9:47808"))
	r.upsertLearned(9, ep, bip.Endpoint{}, now)
	r.upsertLearned(9, ep, bip.Endpoint{}, now.Add(time.Second)) // refresh existing
	if hop, ok := r.selectNextHop(9, now.Add(2*time.Minute)); ok {
		t.Fatalf("expected expired, got %v", hop)
	}
}

func TestHandleNetworkMessageICouldBeAndUnhandled(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	local := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.2:47808"))
	from := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.1:47808"))
	tr := virtual.New(local, clk, 16)
	c, err := New(WithTransport(AdaptVirtual(tr)), withClock(clk))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	injectNetMsg(t, tr, clk, from, npdu.NetMsgICouldBeRouterToNetwork, []byte{0x00, 0x07, 0x01})
	injectNetMsg(t, tr, clk, from, npdu.NetMsgWhoIsRouterToNetwork, []byte{0x00, 0x07})
	injectNetMsg(t, tr, clk, from, 0x7f, []byte{0x00}) // proprietary / unhandled
	// malformed busy list
	nraw, _ := npdu.Append(nil, npdu.NPDU{
		Version: npdu.Version1, NetworkMessage: true, NetMsgType: npdu.NetMsgRouterBusyToNetwork, NetMsgData: []byte{0x00},
	})
	frame, _ := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionOriginalUnicastNPDU, Payload: nraw})
	tr.Inject(virtual.InboundPacket{Data: frame, ImmediatePeer: from, ReceivedAt: clk.Now()})
	time.Sleep(30 * time.Millisecond)
}
