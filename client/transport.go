// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/internal/virtual"
)

// Transport is the data-link send/receive abstraction. One Client owns one
// Transport and therefore one local endpoint.
//
// Send may be called from several goroutines at once and must not retain
// pkt.Data after it returns. Recv is called from a single goroutine; the Data
// it returns is owned by the caller. Close must unblock a pending Recv, after
// which Recv keeps returning an error. A Transport passed to WithTransport is
// closed by Client.Close.
type Transport interface {
	Local() bip.Endpoint
	Send(ctx context.Context, pkt OutboundPacket) error
	Recv(ctx context.Context) (InboundPacket, error)
	Close() error
}

// InboundPacket is a received datagram.
type InboundPacket struct {
	Data          []byte
	ImmediatePeer bip.Endpoint
	ReceivedAt    time.Time
}

// OutboundPacket is a datagram to send.
type OutboundPacket struct {
	Data        []byte
	Destination bip.Endpoint
	Broadcast   bool
}

// maxUDPPayload is the largest UDP payload an IPv4 datagram can carry.
const maxUDPPayload = 65535

// udpTransport is a BACnet/IP UDP socket transport.
type udpTransport struct {
	conn  *net.UDPConn
	local bip.Endpoint
	iface *net.Interface
	// bcast holds the destinations of a local broadcast: the subnet-directed
	// broadcast address of the selected interface, or of every eligible
	// interface when none was selected, followed by the limited broadcast on
	// each of those interfaces.
	bcast   []broadcastDest
	port    int
	mu      sync.Mutex
	closed  bool
	readCtx context.Context
	cancel  context.CancelFunc
	// rbuf is the receive scratch buffer; Recv has a single caller.
	rbuf []byte
}

// broadcastDest is one datagram of a local broadcast. oob, when set, pins it
// to an interface.
type broadcastDest struct {
	addr *net.UDPAddr
	oob  []byte
}

func newUDPTransport(cfg config) (*udpTransport, error) {
	port := cfg.port
	if port == 0 {
		port = bip.DefaultPort
	}
	bind := cfg.localAddr
	if bind == "" {
		bind = fmt.Sprintf(":%d", port)
	}
	addr, err := net.ResolveUDPAddr("udp4", bind)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return nil, err
	}
	laddr := conn.LocalAddr().(*net.UDPAddr)
	local, err := netip.ParseAddrPort(laddr.String())
	if err != nil {
		// IPv4 UDPAddr String may need conversion.
		ip, _ := netip.AddrFromSlice(laddr.IP.To4())
		if !ip.IsValid() {
			_ = conn.Close()
			return nil, fmt.Errorf("bacnet client: local address: %w", err)
		}
		local = netip.AddrPortFrom(ip, uint16(laddr.Port))
	}
	t := &udpTransport{
		conn:  conn,
		local: bip.NewEndpoint(local),
		port:  port,
		rbuf:  make([]byte, maxUDPPayload),
	}
	t.readCtx, t.cancel = context.WithCancel(context.Background())
	if cfg.iface != "" {
		ifi, err := net.InterfaceByName(cfg.iface)
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("bacnet client: interface %q: %w", cfg.iface, err)
		}
		t.iface = ifi
		t.bcast = broadcastDests(interfaceBroadcasts(ifi, port), []net.Interface{*ifi}, port)
	} else {
		t.bcast = broadcastDests(localBroadcasts(laddr.IP, port), broadcastInterfaces(laddr.IP), port)
	}
	return t, nil
}

// broadcastDests returns the datagrams of a local broadcast: one to each
// subnet-directed address and the limited broadcast on each interface.
//
// Both forms go out because devices differ in which their socket receives:
// against the peer stacks one hears only the subnet-directed address and
// another only 255.255.255.255. A device that hears both answers twice, which
// the registries absorb.
//
// The operating system sends 255.255.255.255 on one interface only (that of
// the default route), so on a host with several interfaces it is pinned to
// each of them in turn. Where that is not possible (interfaceOOB returns nil)
// it is sent once, unpinned.
func broadcastDests(directed []*net.UDPAddr, ifaces []net.Interface, port int) []broadcastDest {
	out := make([]broadcastDest, 0, len(directed)+len(ifaces)+1)
	for _, addr := range directed {
		out = append(out, broadcastDest{addr: addr})
	}
	limited := &net.UDPAddr{IP: net.IPv4bcast, Port: port}
	pinned := false
	for i := range ifaces {
		if oob := interfaceOOB(ifaces[i].Index); oob != nil {
			out = append(out, broadcastDest{addr: limited, oob: oob})
			pinned = true
		}
	}
	if !pinned {
		out = append(out, broadcastDest{addr: limited})
	}
	return out
}

// broadcastInterfaces returns the interfaces a local broadcast goes out on
// when none was selected: every interface that is up, broadcast-capable, not
// a loopback and has an IPv4 subnet, or only the one whose subnet bind
// belongs to when the socket is bound to a specific address.
func broadcastInterfaces(bind net.IP) []net.Interface {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	specific := bind != nil && !bind.IsUnspecified()
	var out []net.Interface
	for i := range ifaces {
		ifi := &ifaces[i]
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagBroadcast == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		if len(interfaceBroadcastsFor(ifi, 0, bind, specific)) > 0 {
			out = append(out, *ifi)
		}
	}
	return out
}

// localBroadcasts returns the subnet-directed broadcast addresses a local
// broadcast is sent to when no interface was selected: those of every
// interface that is up, broadcast-capable and not a loopback, or only that of
// the subnet bind belongs to when the socket is bound to a specific address.
//
// BACnet/IP devices listen for the broadcast address of their subnet
// (Annex J); many do not receive the limited broadcast 255.255.255.255.
func localBroadcasts(bind net.IP, port int) []*net.UDPAddr {
	ifaces := broadcastInterfaces(bind)
	specific := bind != nil && !bind.IsUnspecified()
	var out []*net.UDPAddr
	for i := range ifaces {
		ifi := &ifaces[i]
		for _, b := range interfaceBroadcastsFor(ifi, port, bind, specific) {
			dup := false
			for _, have := range out {
				if have.IP.Equal(b.IP) {
					dup = true
				}
			}
			if !dup {
				out = append(out, b)
			}
		}
	}
	return out
}

func interfaceBroadcasts(ifi *net.Interface, port int) []*net.UDPAddr {
	return interfaceBroadcastsFor(ifi, port, nil, false)
}

// directedBroadcast returns the broadcast address of an IPv4 subnet. A /31 or
// /32 has none, and neither has an IPv6 prefix.
func directedBroadcast(ipnet *net.IPNet) (net.IP, bool) {
	ip := ipnet.IP.To4()
	mask := ipnet.Mask
	if ip == nil {
		return nil, false
	}
	if len(mask) == net.IPv6len {
		mask = mask[12:]
	}
	ones, bits := mask.Size()
	if len(mask) != net.IPv4len || bits != 32 || ones > 30 {
		return nil, false
	}
	return net.IPv4(ip[0]|^mask[0], ip[1]|^mask[1], ip[2]|^mask[2], ip[3]|^mask[3]), true
}

// interfaceBroadcastsFor returns the directed broadcast address of each IPv4
// subnet on ifi; with only set, just of the subnet that contains bind.
func interfaceBroadcastsFor(ifi *net.Interface, port int, bind net.IP, only bool) []*net.UDPAddr {
	addrs, err := ifi.Addrs()
	if err != nil {
		return nil
	}
	var out []*net.UDPAddr
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		if only && !ipnet.Contains(bind) {
			continue
		}
		if ip, ok := directedBroadcast(ipnet); ok {
			out = append(out, &net.UDPAddr{IP: ip, Port: port})
		}
	}
	return out
}

func (t *udpTransport) Local() bip.Endpoint { return t.local }

func (t *udpTransport) Send(ctx context.Context, pkt OutboundPacket) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return net.ErrClosed
	}
	deadline, ok := ctx.Deadline()
	if ok {
		_ = t.conn.SetWriteDeadline(deadline)
		defer func() { _ = t.conn.SetWriteDeadline(time.Time{}) }()
	}
	if !pkt.Broadcast {
		a := pkt.Destination.Addr
		_, err := t.conn.WriteToUDP(pkt.Data, &net.UDPAddr{IP: net.IP(a.Addr().AsSlice()), Port: int(a.Port())})
		return err
	}
	// One datagram per local subnet and one limited broadcast per interface.
	// The broadcast succeeds when it went out on at least one of them: an
	// interface that is down or has no route must not hide the others.
	var firstErr error
	sent, pinned, pinnedSent := false, false, false
	for _, dest := range t.bcast {
		var err error
		if dest.oob != nil {
			pinned = true
			_, _, err = t.conn.WriteMsgUDP(pkt.Data, dest.oob, dest.addr)
		} else {
			_, err = t.conn.WriteToUDP(pkt.Data, dest.addr)
		}
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		sent = true
		pinnedSent = pinnedSent || dest.oob != nil
	}
	if pinned && !pinnedSent {
		// The system refused to pin the limited broadcast: send it the way
		// the routing table has it.
		if _, err := t.conn.WriteToUDP(pkt.Data, &net.UDPAddr{IP: net.IPv4bcast, Port: t.port}); err == nil {
			sent = true
		}
	}
	if sent {
		return nil
	}
	return firstErr
}

func (t *udpTransport) Recv(ctx context.Context) (InboundPacket, error) {
	buf := t.rbuf
	for {
		select {
		case <-ctx.Done():
			return InboundPacket{}, ctx.Err()
		case <-t.readCtx.Done():
			return InboundPacket{}, net.ErrClosed
		default:
		}
		_ = t.conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		n, addr, err := t.conn.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			t.mu.Lock()
			closed := t.closed
			t.mu.Unlock()
			if closed {
				return InboundPacket{}, net.ErrClosed
			}
			return InboundPacket{}, err
		}
		ip, ok := netip.AddrFromSlice(addr.IP.To4())
		if !ok {
			continue
		}
		data := make([]byte, n)
		copy(data, buf[:n])
		return InboundPacket{
			Data:          data,
			ImmediatePeer: bip.NewEndpoint(netip.AddrPortFrom(ip, uint16(addr.Port))),
			ReceivedAt:    time.Now(),
		}, nil
	}
}

func (t *udpTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	t.cancel()
	return t.conn.Close()
}

// AdaptVirtual wraps a virtual.Transport as client.Transport.
// virtual.Transport is internal; this helper is for in-module tests only.
func AdaptVirtual(t *virtual.Transport) Transport {
	return &virtualAdapter{t: t}
}

type virtualAdapter struct{ t *virtual.Transport }

func (a *virtualAdapter) Local() bip.Endpoint { return a.t.Local() }

func (a *virtualAdapter) Send(ctx context.Context, pkt OutboundPacket) error {
	return a.t.Send(ctx, virtual.OutboundPacket{
		Data:        pkt.Data,
		Destination: pkt.Destination,
		Broadcast:   pkt.Broadcast,
	})
}

func (a *virtualAdapter) Recv(ctx context.Context) (InboundPacket, error) {
	pkt, err := a.t.Recv(ctx)
	if err != nil {
		return InboundPacket{}, err
	}
	return InboundPacket{
		Data:          pkt.Data,
		ImmediatePeer: pkt.ImmediatePeer,
		ReceivedAt:    pkt.ReceivedAt,
	}, nil
}

func (a *virtualAdapter) Close() error { return a.t.Close() }
