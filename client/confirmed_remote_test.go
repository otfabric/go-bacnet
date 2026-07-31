// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/npdu"
	"github.com/otfabric/go-bacnet/service"
)

func TestResponseDestinationRemoteStation(t *testing.T) {
	mac := bacnet.MustMAC([]byte{10, 0, 0, 5, 0xBA, 0xC0})
	remote := bacnet.RemoteStation(2, mac)
	if !responseDestination(packetSource{bacnetAddress: remote}).Equal(remote) {
		t.Fatal("remote station should round-trip as destination")
	}
	local := bacnet.LocalStation(mac)
	if !responseDestination(packetSource{bacnetAddress: local}).MAC().IsZero() {
		t.Fatal("local station should map to empty destination")
	}
}

func TestResolveRemoteMaxAPDU(t *testing.T) {
	env := newVirtualPair(t)
	caps := DeviceCapabilities{}
	caps.SetIAmFields(1024, 0, 42)
	env.Client.reg.Upsert(DeviceObservation{
		Instance:      1,
		Address:       env.Target.Address,
		ImmediatePeer: env.Peer,
		LastSeen:      env.Clk.Now(),
		Capabilities:  caps,
	})

	if got := env.Client.resolveRemoteMaxAPDU(Target{Address: env.Target.Address, Endpoint: env.Peer}); got != 1024 {
		t.Fatalf("registry max=%d", got)
	}
	if got := env.Client.resolveRemoteMaxAPDU(Target{MaxAPDU: 480}); got != 480 {
		t.Fatalf("override max=%d", got)
	}
	if got := env.Client.resolveRemoteMaxAPDU(Target{Address: bacnet.LocalStation(bacnet.MustMAC([]byte{1, 2, 3, 4, 5, 6}))}); got != 480 {
		t.Fatalf("fallback max=%d", got)
	}
}

func TestConfirmedCOVRepliesToRemoteSource(t *testing.T) {
	env := newVirtualPair(t)
	remoteMAC := bacnet.MustMAC([]byte{10, 0, 0, 9, 0xBA, 0xC0})
	remote := bacnet.RemoteStation(2, remoteMAC)

	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 11}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go serveSimpleACK(ctx, env.PeerTr, env.Local)
	sub, err := env.Client.SubscribeCOVProperty(context.Background(), env.Target, obj, bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}, COVOptions{Lifetime: 60, BufferSize: 4, IssueConfirmed: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sub.Close() }()
	<-sub.Events()

	note := service.COVNotification{
		ProcessIdentifier: 1,
		InitiatingDevice:  bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		MonitoredObject:   obj,
		TimeRemaining:     30,
		Values: []service.PropertyValue{{
			Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
			Value:    bacnet.RealValue(4.0),
		}},
	}
	payload := encodeCOVNotification(t, note)
	apduBytes := apdu.AppendConfirmedRequest(nil, apdu.ConfirmedRequest{
		InvokeID: 12, ServiceChoice: 1, MaxAPDU: 5, Payload: payload,
	})
	before := len(env.ClientTr.Outbox())
	injectUnicastNPDUWithSource(t, env.ClientTr, env.Peer, env.Clk.Now(), npdu.NPDU{
		Version: npdu.Version1,
		Source:  remote,
		APDU:    apduBytes,
	})
	time.Sleep(20 * time.Millisecond)

	limits := bacnet.DefaultDecodeLimits()
	foundDest := false
	for _, pkt := range env.ClientTr.Outbox()[before:] {
		msg, err := bvlc.Parse(pkt.Data, limits)
		if err != nil {
			continue
		}
		n, _, err := npdu.Parse(msg.Payload, limits)
		if err != nil {
			continue
		}
		if n.Destination.Equal(remote) {
			foundDest = true
			break
		}
	}
	if !foundDest {
		t.Fatal("SimpleACK should use remote-station destination from indication source")
	}
}

func TestConfirmedCOVMalformedIndication(t *testing.T) {
	env := newVirtualPair(t)
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendConfirmedRequest(nil, apdu.ConfirmedRequest{
		InvokeID: 5, ServiceChoice: 1, MaxAPDU: 5, Payload: []byte{0x09, 0x01},
	}))
	time.Sleep(10 * time.Millisecond)
}

func TestReadPropertyContextCancelWrapsOutcomeUnknown(t *testing.T) {
	env := newVirtualPair(t, WithTransactionOptions(time.Second, 0, time.Second))
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogOutput, Instance: 1}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- env.Client.WriteProperty(ctx, env.Target, obj, prop, bacnet.RealValue(1.0), nil)
	}()
	_, _ = waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	cancel()

	err := <-errCh
	var unknown *bacnet.OutcomeUnknownError
	if !errors.As(err, &unknown) || unknown.Operation != "WriteProperty" {
		t.Fatalf("got %v", err)
	}
}

func TestReadPropertyRetransmitOnTimeout(t *testing.T) {
	env := newVirtualPair(t, WithTransactionOptions(200*time.Millisecond, 1, time.Second))
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogInput, Instance: 9}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}

	errCh := make(chan error, 1)
	go func() {
		ctx := context.Background()
		_, err := env.Client.ReadProperty(ctx, env.Target, obj, prop)
		errCh <- err
	}()

	firstID, _ := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	before := len(env.ClientTr.Outbox())
	env.Clk.Advance(250 * time.Millisecond)
	time.Sleep(20 * time.Millisecond)

	invokeID := firstID
	for _, pkt := range env.ClientTr.Outbox()[before:] {
		msg, _ := bvlc.Parse(pkt.Data, bacnet.DefaultDecodeLimits())
		n, _, _ := npdu.Parse(msg.Payload, bacnet.DefaultDecodeLimits())
		pdu, _ := apdu.Parse(n.APDU, bacnet.DefaultDecodeLimits())
		if pdu.ConfirmedRequest != nil {
			invokeID = pdu.ConfirmedRequest.InvokeID
		}
	}
	if invokeID != firstID {
		t.Fatalf("retransmit should reuse invoke ID %d got %d", firstID, invokeID)
	}

	ackPayload, _ := service.EncodeReadPropertyACK(service.ReadPropertyACK{
		Object: obj, Property: prop, Value: bacnet.RealValue(2.0),
	})
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendComplexACK(nil, apdu.ComplexACK{
		InvokeID: invokeID, ServiceChoice: apdu.ServiceReadProperty, Payload: ackPayload,
	}))

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}
