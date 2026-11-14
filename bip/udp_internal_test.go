// SPDX-License-Identifier: MIT

package bip

import (
	"context"
	"errors"
	"math"
	"net"
	"runtime"
	"strconv"
	"syscall"
	"testing"
	"time"
)

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
	tr, err := listenUDP(UDPConfig{LocalAddr: "0.0.0.0:0", Port: 47808})
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

func TestInterfaceOOBParses(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		if interfaceOOB(1) != nil {
			t.Fatal("interfaceOOB must be nil where pinning is unsupported")
		}
		return
	}
	if interfaceOOB(-1) != nil {
		t.Error("negative ifindex encoded")
	}
	oob := interfaceOOB(7)
	msgs, err := syscall.ParseSocketControlMessage(oob)
	if err != nil {
		t.Fatalf("ParseSocketControlMessage: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("got %d control messages, want 1", len(msgs))
	}
	m := msgs[0]
	if m.Header.Level != syscall.IPPROTO_IP || m.Header.Type != syscall.IP_PKTINFO {
		t.Fatalf("header level/type = %d/%d", m.Header.Level, m.Header.Type)
	}
	if len(m.Data) < 4 || m.Data[0]|m.Data[1]|m.Data[2]|m.Data[3] == 0 {
		t.Fatalf("pktinfo data %x does not carry the interface index", m.Data)
	}
}

func TestUDPTransportRecvAfterClose(t *testing.T) {
	tr, err := ListenUDP(UDPConfig{LocalAddr: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Close(); err != nil {
		t.Fatal(err)
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := tr.Recv(ctx); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("got %v", err)
	}
}

func TestUDPTransportSendAfterClose(t *testing.T) {
	tr, err := ListenUDP(UDPConfig{LocalAddr: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	_ = tr.Close()
	err = tr.Send(context.Background(), OutboundPacket{Data: []byte{0x81, 0x0a, 0x00, 0x06, 0x01, 0x00}})
	if !errors.Is(err, net.ErrClosed) {
		t.Fatalf("got %v", err)
	}
}

func TestUDPTransportLoopback(t *testing.T) {
	a, err := ListenUDP(UDPConfig{LocalAddr: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = a.Close() }()
	b, err := ListenUDP(UDPConfig{LocalAddr: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := a.Send(ctx, OutboundPacket{Data: []byte{1, 2, 3}, Destination: b.Local()}); err != nil {
		t.Fatal(err)
	}
	pkt, err := b.Recv(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if string(pkt.Data) != "\x01\x02\x03" || !pkt.ImmediatePeer.Equal(a.Local()) {
		t.Fatalf("got %x from %v, want 010203 from %v", pkt.Data, pkt.ImmediatePeer, a.Local())
	}
}

func TestListenUDPErrors(t *testing.T) {
	if _, err := ListenUDP(UDPConfig{LocalAddr: "not-an-address"}); err == nil {
		t.Error("bad address accepted")
	}
	if _, err := ListenUDP(UDPConfig{LocalAddr: "127.0.0.1:0", Interface: "no-such-interface0"}); err == nil {
		t.Error("unknown interface accepted")
	}
}

func TestListenUDPPortInUse(t *testing.T) {
	a, err := listenUDP(UDPConfig{LocalAddr: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = a.Close() }()
	if _, err := ListenUDP(UDPConfig{LocalAddr: a.Local().String()}); err == nil {
		t.Fatalf("second socket on %v accepted", a.Local())
	}
}

// loopbackInterface returns the host's IPv4 loopback interface.
func loopbackInterface(t *testing.T) net.Interface {
	t.Helper()
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagLoopback != 0 && ifi.Flags&net.FlagUp != 0 {
			return ifi
		}
	}
	t.Skip("no loopback interface")
	return net.Interface{}
}

func TestListenUDPOnInterface(t *testing.T) {
	lo := loopbackInterface(t)
	tr, err := listenUDP(UDPConfig{LocalAddr: "127.0.0.1:0", Interface: lo.Name})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tr.Close() }()
	if tr.iface == nil || tr.iface.Name != lo.Name {
		t.Fatalf("interface %+v, want %s", tr.iface, lo.Name)
	}
	// The loopback subnet 127.0.0.0/8 has a directed broadcast address; the
	// interface is the one selected, so it is used although it is a
	// loopback.
	found := false
	for _, d := range tr.bcast {
		if d.addr.IP.Equal(net.IPv4(127, 255, 255, 255)) {
			found = true
		}
	}
	if !found {
		t.Fatalf("broadcast destinations %+v lack 127.255.255.255", tr.bcast)
	}
}

func TestLocalBroadcastsSkipLoopback(t *testing.T) {
	// Bound to the loopback address, no eligible interface holds it: a
	// loopback is never a broadcast interface when none was selected.
	if got := localBroadcasts(net.IPv4(127, 0, 0, 1), 47808); len(got) != 0 {
		t.Fatalf("loopback bind gave broadcasts %v", got)
	}
	if got := broadcastInterfaces(net.IPv4(127, 0, 0, 1)); len(got) != 0 {
		t.Fatalf("loopback bind gave interfaces %v", got)
	}
}

func TestLocalBroadcastsForBoundAddress(t *testing.T) {
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for i := range ifaces {
		ifi := &ifaces[i]
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagBroadcast == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifi.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok || ipnet.IP.To4() == nil {
				continue
			}
			want, ok := directedBroadcast(ipnet)
			if !ok {
				continue
			}
			got := localBroadcasts(ipnet.IP, 47808)
			if len(got) != 1 || !got[0].IP.Equal(want) || got[0].Port != 47808 {
				t.Fatalf("bound to %v: broadcasts %v, want only %v", ipnet.IP, got, want)
			}
			return
		}
	}
	t.Skip("no broadcast-capable IPv4 interface on this host")
}

func TestUDPTransportContextEnds(t *testing.T) {
	tr, err := ListenUDP(UDPConfig{LocalAddr: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tr.Close() }()
	done, cancel := context.WithCancel(context.Background())
	cancel()
	if err := tr.Send(done, OutboundPacket{Data: []byte{1}, Destination: tr.Local()}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Send with a cancelled context: %v", err)
	}
	// Nothing arrives: Recv polls past its read deadline and then returns
	// when the context ends.
	ctx, stop := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer stop()
	if _, err := tr.Recv(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Recv: %v", err)
	}
	// A write deadline from the context is set and cleared around the write.
	ctx2, stop2 := context.WithTimeout(context.Background(), time.Second)
	defer stop2()
	if err := tr.Send(ctx2, OutboundPacket{Data: []byte{1}, Destination: tr.Local()}); err != nil {
		t.Fatal(err)
	}
	if pkt, err := tr.Recv(ctx2); err != nil || len(pkt.Data) != 1 {
		t.Fatalf("Recv own datagram: %v %+v", err, pkt)
	}
}

// A broadcast succeeds when at least one destination took the datagram, and
// reports the first error when none did.
func TestUDPTransportBroadcastFanOut(t *testing.T) {
	rx, err := ListenUDP(UDPConfig{LocalAddr: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rx.Close() }()
	tx, err := listenUDP(UDPConfig{LocalAddr: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Close() }()
	good := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: int(rx.Local().Addr.Port())}
	bad := &net.UDPAddr{IP: net.IPv4zero, Port: 0} // no host accepts port 0
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	tx.bcast = []broadcastDest{{addr: bad}, {addr: good}}
	if err := tx.Send(ctx, OutboundPacket{Data: []byte{9}, Broadcast: true}); err != nil {
		t.Fatalf("one good destination: %v", err)
	}
	if pkt, err := rx.Recv(ctx); err != nil || string(pkt.Data) != "\x09" {
		t.Fatalf("Recv: %v %+v", err, pkt)
	}
	tx.bcast = []broadcastDest{{addr: bad}}
	if err := tx.Send(ctx, OutboundPacket{Data: []byte{9}, Broadcast: true}); err == nil {
		t.Fatal("no destination took the broadcast, but Send reported success")
	}
}

func TestDirectedBroadcastMaskForms(t *testing.T) {
	// Interface addresses can carry a 16-octet mask for an IPv4 subnet.
	ipnet := &net.IPNet{IP: net.IPv4(192, 168, 7, 9), Mask: net.CIDRMask(120, 128)}
	if got, ok := directedBroadcast(ipnet); !ok || !got.Equal(net.IPv4(192, 168, 7, 255)) {
		t.Fatalf("16-octet /24 mask: %v %v", got, ok)
	}
	// A mask that is not a prefix has no broadcast address.
	odd := &net.IPNet{IP: net.IPv4(10, 0, 0, 1).To4(), Mask: net.IPMask{255, 0, 255, 0}}
	if got, ok := directedBroadcast(odd); ok {
		t.Fatalf("non-prefix mask gave %v", got)
	}
}

func TestInterfaceOOBIndexBeyond32Bits(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return
	}
	if strconv.IntSize < 64 {
		return
	}
	wide := int64(math.MaxUint32) + 1
	big := int(wide)
	if interfaceOOB(big) != nil {
		t.Fatal("an interface index beyond 32 bits was encoded")
	}
}
