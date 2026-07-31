// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet/bip"
)

func TestUDPTransportLoopback(t *testing.T) {
	peer, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = peer.Close() }()
	peerAddr := peer.LocalAddr().(*net.UDPAddr)

	c, err := New(
		WithLocalAddr("127.0.0.1:0"),
		WithTransactionOptions(time.Second, 0, time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	local := c.LocalEndpoint()
	if !local.IsValid() {
		t.Fatal("expected valid local endpoint")
	}

	payload := []byte{0x81, 0x0A, 0x00, 0x08, 0x01, 0x00, 0x10, 0x08}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 2048)
		_ = peer.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, _, err := peer.ReadFromUDP(buf)
		if err != nil {
			done <- nil
			return
		}
		done <- append([]byte(nil), buf[:n]...)
	}()

	dest := bip.NewEndpoint(netip.AddrPortFrom(
		netip.AddrFrom4([4]byte{127, 0, 0, 1}),
		uint16(peerAddr.Port),
	))
	if err := c.tr.Send(ctx, OutboundPacket{Data: payload, Destination: dest}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	got := <-done
	if string(got) != string(payload) {
		t.Fatalf("peer got %x want %x", got, payload)
	}

	_, _ = peer.WriteToUDP(payload, &net.UDPAddr{
		IP:   net.IP(local.Addr.Addr().AsSlice()),
		Port: int(local.Addr.Port()),
	})
	time.Sleep(20 * time.Millisecond)
}

func TestUDPTransportBroadcastSend(t *testing.T) {
	c, err := New(WithLocalAddr("127.0.0.1:0"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = c.tr.Send(ctx, OutboundPacket{
		Data:      []byte{0x81, 0x0B, 0x00, 0x06, 0x01, 0x00},
		Broadcast: true,
	})
}
