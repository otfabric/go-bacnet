// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/internal/virtual"
	"github.com/otfabric/go-bacnet/npdu"
)

func TestHandlePacketEmptyNPDU(t *testing.T) {
	env := newVirtualPair(t)
	frame, err := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionOriginalUnicastNPDU, Payload: []byte{}})
	if err != nil {
		t.Fatal(err)
	}
	env.ClientTr.Inject(virtual.InboundPacket{Data: frame, ImmediatePeer: env.Peer, ReceivedAt: env.Clk.Now()})
	time.Sleep(10 * time.Millisecond)
}

func TestHandleUnexpectedAPDUType(t *testing.T) {
	env := newVirtualPair(t)
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), []byte{0xF0, 0x00})
	time.Sleep(10 * time.Millisecond)
}

func TestClientCloseDuringReadProperty(t *testing.T) {
	env := newVirtualPair(t, WithTransactionOptions(time.Second, 0, 0))
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogInput, Instance: 5}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}

	errCh := make(chan error, 1)
	go func() {
		_, err := env.Client.ReadProperty(context.Background(), env.Target, obj, prop)
		errCh <- err
	}()
	_, _ = waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	_ = env.Client.Close()

	err := <-errCh
	if !errors.Is(err, bacnet.ErrClosed) {
		t.Fatalf("got %v", err)
	}
}

func TestDirectedBroadcast(t *testing.T) {
	for cidr, want := range map[string]string{
		"192.168.1.10/24": "192.168.1.255",
		"10.1.2.3/20":     "10.1.15.255",
		"172.19.0.6/16":   "172.19.255.255",
		"10.0.0.1/30":     "10.0.0.3",
		"10.0.0.1/31":     "", // point-to-point: no broadcast address
		"10.0.0.1/32":     "",
		"fe80::1/64":      "",
	} {
		ip, ipnet, err := net.ParseCIDR(cidr)
		if err != nil {
			t.Fatal(err)
		}
		ipnet.IP = ip
		got, ok := directedBroadcast(ipnet)
		if (want == "") == ok || (ok && got.String() != want) {
			t.Errorf("directedBroadcast(%s) = %v, %v; want %q", cidr, got, ok, want)
		}
	}
}

// The per-subnet destinations of a wildcard-bound socket are directed
// broadcast addresses; the limited broadcast is appended separately.
func TestLocalBroadcastsAreDirected(t *testing.T) {
	for _, b := range localBroadcasts(net.IPv4zero, 47808) {
		if b.Port != 47808 || b.IP.Equal(net.IPv4bcast) || b.IP.To4() == nil {
			t.Fatalf("unexpected broadcast destination %v", b)
		}
	}
}

// The limited broadcast is pinned to each interface where the platform can
// do that, and sent once, unpinned, where it cannot or no interface is known.
func TestBroadcastDests(t *testing.T) {
	directed := []*net.UDPAddr{{IP: net.IPv4(10, 0, 0, 255), Port: 47808}, {IP: net.IPv4(192, 168, 1, 255), Port: 47808}}
	ifaces := []net.Interface{{Index: 3}, {Index: 7}}
	canPin := interfaceOOB(1) != nil

	dests := broadcastDests(directed, ifaces, 47808)
	wantLimited := 1
	if canPin {
		wantLimited = len(ifaces)
	}
	if len(dests) != len(directed)+wantLimited {
		t.Fatalf("%d destinations, want %d directed and %d limited", len(dests), len(directed), wantLimited)
	}
	for i, d := range dests {
		limited := d.addr.IP.Equal(net.IPv4bcast)
		if d.addr.Port != 47808 || limited != (i >= len(directed)) {
			t.Fatalf("destination %d is %v", i, d.addr)
		}
		if (d.oob != nil) != (limited && canPin) {
			t.Fatalf("destination %d (%v): pinned=%v", i, d.addr, d.oob != nil)
		}
	}
	if canPin && string(dests[2].oob) == string(dests[3].oob) {
		t.Fatal("both limited broadcasts are pinned to the same interface")
	}

	// No interface known: still one limited broadcast.
	dests = broadcastDests(nil, nil, 47808)
	if len(dests) != 1 || !dests[0].addr.IP.Equal(net.IPv4bcast) || dests[0].oob != nil {
		t.Fatalf("without interfaces: %+v", dests)
	}
}

// A broadcast on a real socket goes out on every interface without error.
func TestUDPTransportBroadcastOnEveryInterface(t *testing.T) {
	tr, err := newUDPTransport(config{localAddr: "0.0.0.0:0", port: 47808})
	if err != nil {
		t.Skipf("no UDP socket: %v", err)
	}
	defer func() { _ = tr.Close() }()
	if len(broadcastInterfaces(net.IPv4zero)) == 0 {
		t.Skip("no broadcast-capable interface")
	}
	// Port 9 (discard) instead of 47808, so that nothing on the network
	// takes this for BACnet.
	for i := range tr.bcast {
		tr.bcast[i].addr = &net.UDPAddr{IP: tr.bcast[i].addr.IP, Port: 9}
	}
	tr.port = 9
	if err := tr.Send(context.Background(), OutboundPacket{Data: []byte{0}, Broadcast: true}); err != nil {
		t.Fatalf("broadcast send: %v", err)
	}
}

func TestSendWhoIsClosedClient(t *testing.T) {
	env := newVirtualPair(t)
	if err := env.Client.Close(); err != nil {
		t.Fatal(err)
	}
	err := env.Client.SendWhoIs(context.Background(), env.Peer, false, DiscoveryOptions{})
	if !errors.Is(err, bacnet.ErrClosed) {
		t.Fatalf("got %v", err)
	}
}

func TestInjectForwardedNPDUWithSource(t *testing.T) {
	env := newVirtualPair(t)
	remote := bacnet.RemoteStation(2, bacnet.MustMAC([]byte{10, 0, 0, 2, 0xBA, 0xC0}))
	nraw, err := npdu.Append(nil, npdu.NPDU{Version: npdu.Version1, Source: remote, APDU: apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{
		ServiceChoice: apdu.ServiceWhoIs,
	})})
	if err != nil {
		t.Fatal(err)
	}
	frame, err := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionForwardedNPDU, Payload: nraw})
	if err != nil {
		t.Fatal(err)
	}
	env.ClientTr.Inject(virtual.InboundPacket{Data: frame, ImmediatePeer: env.Peer, ReceivedAt: env.Clk.Now()})
	time.Sleep(10 * time.Millisecond)
}

func TestHandlePacketRegisterForeignDeviceIgnored(t *testing.T) {
	env := newVirtualPair(t)
	frame, err := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionRegisterForeignDevice, Payload: []byte{0x00, 0x3c}})
	if err != nil {
		t.Fatal(err)
	}
	env.ClientTr.Inject(virtual.InboundPacket{Data: frame, ImmediatePeer: env.Peer, ReceivedAt: env.Clk.Now()})
	time.Sleep(10 * time.Millisecond)
}
