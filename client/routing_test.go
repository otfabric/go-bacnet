// SPDX-License-Identifier: MIT

package client

import (
	"context"
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

func TestIAmRouterResolveTargetVirtual(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	local := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.2:47808"))
	router := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.1:47808"))
	tr := virtual.New(local, clk, 16)
	c, err := New(WithTransport(AdaptVirtual(tr)), withClock(clk))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	remoteMAC := bacnet.MustMAC([]byte{10, 0, 0, 5, 0xBA, 0xC0})
	remoteAddr := bacnet.RemoteStation(2, remoteMAC)

	// Before I-Am-Router, ResolveTarget keeps the provided direct hop.
	direct := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.99:47808"))
	got, err := c.ResolveTarget(remoteAddr, direct)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Endpoint.Equal(direct) {
		t.Fatalf("endpoint=%v, want direct %v", got.Endpoint, direct)
	}

	// Inject I-Am-Router-To-Network for net 2 from the router.
	netmsg := []byte{0x00, 0x02} // network 2
	nraw, err := npdu.Append(nil, npdu.NPDU{
		Version:        npdu.Version1,
		NetworkMessage: true,
		NetMsgType:     npdu.NetMsgIAmRouterToNetwork,
		NetMsgData:     netmsg,
	})
	if err != nil {
		t.Fatal(err)
	}
	frame, err := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionOriginalBroadcastNPDU, Payload: nraw})
	if err != nil {
		t.Fatal(err)
	}
	tr.Inject(virtual.InboundPacket{Data: frame, ImmediatePeer: router, ReceivedAt: clk.Now()})
	time.Sleep(20 * time.Millisecond)

	got, err = c.ResolveTarget(remoteAddr, bip.Endpoint{})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Endpoint.Equal(router) {
		t.Fatalf("endpoint=%v, want router %v", got.Endpoint, router)
	}

	// Who-Is-Router-To-Network should emit a local broadcast network message.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	netn := uint16(2)
	if err := c.WhoIsRouterToNetwork(ctx, &netn); err != nil {
		t.Fatal(err)
	}
	out := tr.Outbox()
	if len(out) == 0 {
		t.Fatal("expected outbound Who-Is-Router")
	}
	pkt := out[len(out)-1]
	if !pkt.Broadcast {
		t.Fatal("Who-Is-Router should be broadcast")
	}
	msg, err := bvlc.Parse(pkt.Data, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if msg.Function != bvlc.FunctionOriginalBroadcastNPDU {
		t.Fatalf("BVLC function %v, want Original-Broadcast", msg.Function)
	}
	n, _, err := npdu.Parse(msg.Payload, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if !n.NetworkMessage || n.NetMsgType != npdu.NetMsgWhoIsRouterToNetwork {
		t.Fatalf("NPDU %#v", n)
	}
	if len(n.NetMsgData) != 2 || n.NetMsgData[0] != 0 || n.NetMsgData[1] != 2 {
		t.Fatalf("Who-Is-Router data %x", n.NetMsgData)
	}
}
