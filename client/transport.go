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

// Transport is the data-link send/receive abstraction.
// Client: one Client owns one local endpoint.
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

// udpTransport is a BACnet/IP UDP socket transport.
type udpTransport struct {
	conn    *net.UDPConn
	local   bip.Endpoint
	iface   *net.Interface
	bcast   *net.UDPAddr
	port    int
	mu      sync.Mutex
	closed  bool
	readCtx context.Context
	cancel  context.CancelFunc
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
	}
	t.readCtx, t.cancel = context.WithCancel(context.Background())
	if cfg.iface != "" {
		ifi, err := net.InterfaceByName(cfg.iface)
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("bacnet client: interface %q: %w", cfg.iface, err)
		}
		t.iface = ifi
		t.bcast = interfaceBroadcast(ifi, port)
	}
	return t, nil
}

func interfaceBroadcast(ifi *net.Interface, port int) *net.UDPAddr {
	addrs, err := ifi.Addrs()
	if err != nil {
		return &net.UDPAddr{IP: net.IPv4bcast, Port: port}
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.To4() == nil {
			continue
		}
		ip := ipnet.IP.To4()
		mask := ipnet.Mask
		if len(mask) != 4 {
			continue
		}
		bcast := net.IPv4(ip[0]|^mask[0], ip[1]|^mask[1], ip[2]|^mask[2], ip[3]|^mask[3])
		return &net.UDPAddr{IP: bcast, Port: port}
	}
	return &net.UDPAddr{IP: net.IPv4bcast, Port: port}
}

func (t *udpTransport) Local() bip.Endpoint { return t.local }

func (t *udpTransport) Send(ctx context.Context, pkt OutboundPacket) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return net.ErrClosed
	}
	var addr *net.UDPAddr
	if pkt.Broadcast {
		if t.bcast != nil {
			addr = t.bcast
		} else {
			port := t.port
			if port == 0 {
				port = bip.DefaultPort
			}
			addr = &net.UDPAddr{IP: net.IPv4bcast, Port: port}
		}
	} else {
		a := pkt.Destination.Addr
		addr = &net.UDPAddr{IP: net.IP(a.Addr().AsSlice()), Port: int(a.Port())}
	}
	deadline, ok := ctx.Deadline()
	if ok {
		_ = t.conn.SetWriteDeadline(deadline)
		defer func() { _ = t.conn.SetWriteDeadline(time.Time{}) }()
	}
	_, err := t.conn.WriteToUDP(pkt.Data, addr)
	return err
}

func (t *udpTransport) Recv(ctx context.Context) (InboundPacket, error) {
	buf := make([]byte, 65535)
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
