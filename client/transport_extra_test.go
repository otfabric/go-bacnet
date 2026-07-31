// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestUDPTransportRecvAfterClose(t *testing.T) {
	c, err := New(WithLocalAddr("127.0.0.1:0"))
	if err != nil {
		t.Fatal(err)
	}
	ut, ok := c.tr.(*udpTransport)
	if !ok {
		t.Skip("not UDP transport")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = ut.Recv(ctx)
	if !errors.Is(err, net.ErrClosed) {
		t.Fatalf("got %v", err)
	}
}

func TestUDPTransportSendAfterClose(t *testing.T) {
	c, err := New(WithLocalAddr("127.0.0.1:0"))
	if err != nil {
		t.Fatal(err)
	}
	ut, ok := c.tr.(*udpTransport)
	if !ok {
		t.Skip("not UDP transport")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	err = ut.Send(context.Background(), OutboundPacket{Data: []byte{0x81, 0x0a, 0x00, 0x06, 0x01, 0x00}})
	if !errors.Is(err, net.ErrClosed) {
		t.Fatalf("got %v", err)
	}
}
