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

func TestInterfaceBroadcastInvalidInterface(t *testing.T) {
	ifi := &net.Interface{Name: "invalid-test-iface-does-not-exist-xyz"}
	addr := interfaceBroadcast(ifi, 47808)
	if addr == nil || addr.IP == nil {
		t.Fatal("expected fallback broadcast address")
	}
}

func TestInterfaceBroadcastIPv4Interface(t *testing.T) {
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for i := range ifaces {
		ifi := &ifaces[i]
		addrs, err := ifi.Addrs()
		if err != nil || len(addrs) == 0 {
			continue
		}
		hasV4 := false
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if ok && ipnet.IP.To4() != nil && len(ipnet.Mask) == 4 {
				hasV4 = true
				break
			}
		}
		if !hasV4 {
			continue
		}
		addr := interfaceBroadcast(ifi, 47808)
		if addr == nil || addr.IP == nil || addr.Port != 47808 {
			t.Fatalf("interface %q: unexpected broadcast %v", ifi.Name, addr)
		}
		return
	}
	t.Skip("no IPv4 interface with a /32–/0 mask")
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
