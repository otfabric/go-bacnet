// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/npdu"
	"github.com/otfabric/go-bacnet/service"
)

func TestSendWhoIsOutboundFrame(t *testing.T) {
	env := newVirtualPair(t)
	ctx := context.Background()

	bcast := bip.NewEndpoint(netip.MustParseAddrPort("255.255.255.255:47808"))
	if err := env.Client.SendWhoIs(ctx, bcast, true, DiscoveryOptions{}); err != nil {
		t.Fatal(err)
	}

	out := env.ClientTr.Outbox()
	if len(out) == 0 {
		t.Fatal("expected outbound Who-Is")
	}
	pkt := out[len(out)-1]
	if !pkt.Broadcast {
		t.Fatal("Who-Is should be broadcast")
	}
	msg, err := bvlc.Parse(pkt.Data, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if msg.Function != bvlc.FunctionOriginalBroadcastNPDU {
		t.Fatalf("BVLC function %v", msg.Function)
	}
	n, _, err := npdu.Parse(msg.Payload, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if len(n.APDU) == 0 {
		t.Fatal("empty APDU")
	}
	pdu, err := apdu.Parse(n.APDU, bacnet.DefaultDecodeLimits())
	if err != nil || pdu.UnconfirmedRequest == nil {
		t.Fatalf("APDU %#v err=%v", pdu, err)
	}
	if pdu.UnconfirmedRequest.ServiceChoice != apdu.ServiceWhoIs {
		t.Fatalf("service %d", pdu.UnconfirmedRequest.ServiceChoice)
	}
}

func TestDiscoverCollectsIAm(t *testing.T) {
	env := newVirtualPair(t)

	iamPayload, err := service.EncodeIAm(service.IAm{
		Device:        bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 100},
		MaxAPDULength: 1024,
		Segmentation:  0,
		VendorID:      42,
	})
	if err != nil {
		t.Fatal(err)
	}
	apduBytes := apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{
		ServiceChoice: apdu.ServiceIAm,
		Payload:       iamPayload,
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		obs, err := env.Client.Discover(ctx, DiscoveryOptions{})
		if len(obs) != 1 || obs[0].Instance != 100 {
			t.Errorf("observations %#v", obs)
		}
		if err == nil || err != context.Canceled {
			// Discover returns ctx.Err() on cancel.
			if err != context.Canceled {
				t.Errorf("Discover err=%v", err)
			}
		}
	}()

	time.Sleep(20 * time.Millisecond)
	injectBroadcastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apduBytes)
	time.Sleep(20 * time.Millisecond)
	cancel()
	<-done

	obs := env.Client.Devices()
	if len(obs) != 1 || obs[0].Instance != 100 {
		t.Fatalf("Devices() %#v", obs)
	}
	if !obs[0].Capabilities.MaxAPDULengthAccepted.Known || obs[0].Capabilities.MaxAPDULengthAccepted.Value != 1024 {
		t.Fatalf("capabilities %#v", obs[0].Capabilities)
	}
}

func TestSendWhoIsRemoteBroadcastAddress(t *testing.T) {
	env := newVirtualPair(t)
	dnet := uint16(2)
	addr := bacnet.RemoteBroadcast(dnet)
	if err := env.Client.SendWhoIs(context.Background(), env.Peer, false, DiscoveryOptions{Address: addr}); err != nil {
		t.Fatal(err)
	}
	out := env.ClientTr.Outbox()
	if len(out) == 0 {
		t.Fatal("expected outbound frame")
	}
	msg, err := bvlc.Parse(out[len(out)-1].Data, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	n, _, err := npdu.Parse(msg.Payload, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if !n.Destination.Equal(addr) {
		t.Fatalf("destination %#v", n.Destination)
	}
}

func TestDiscoverReturnsClosedWhenClientClosed(t *testing.T) {
	env := newVirtualPair(t)
	if err := env.Client.Close(); err != nil {
		t.Fatal(err)
	}
	_, err := env.Client.Discover(context.Background(), DiscoveryOptions{})
	if !errors.Is(err, bacnet.ErrClosed) {
		t.Fatalf("got %v", err)
	}
}

func TestHandleUnconfirmedMalformedIAm(t *testing.T) {
	env := newVirtualPair(t)
	injectBroadcastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{
		ServiceChoice: apdu.ServiceIAm,
		Payload:       []byte{0x09, 0x01},
	}))
	time.Sleep(20 * time.Millisecond)
	if len(env.Client.Devices()) != 0 {
		t.Fatal("malformed I-Am should not register device")
	}
}

func TestHandleUnconfirmedCOVDelivery(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go serveSimpleACK(ctx, env.PeerTr, env.Local)

	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 4}
	sub, err := env.Client.SubscribeCOV(context.Background(), env.Target, obj, COVOptions{Lifetime: 60, BufferSize: 4})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sub.Close() }()
	<-sub.Events()

	note := service.COVNotification{
		ProcessIdentifier: 1,
		InitiatingDevice:  bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		MonitoredObject:   obj,
		TimeRemaining:     20,
		Values: []service.PropertyValue{{
			Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
			Value:    bacnet.RealValue(7.0),
		}},
	}
	payload := encodeCOVNotification(t, note)
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{
		ServiceChoice: apdu.ServiceUnconfirmedCOV,
		Payload:       payload,
	}))

	select {
	case ev := <-sub.Events():
		if ev.Notification == nil {
			t.Fatal("expected COV notification")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}
