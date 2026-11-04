// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/internal/clock"
	"github.com/otfabric/go-bacnet/internal/virtual"
	"github.com/otfabric/go-bacnet/npdu"
	"github.com/otfabric/go-bacnet/service"
)

var (
	fuzzPeer   = bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.2:47808"))
	fuzzObject = bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	fuzzProp   = bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
)

// fuzzFrame wraps an APDU in a local NPDU and an Original-Unicast BVLC.
func fuzzFrame(pdu []byte) []byte {
	n, err := npdu.Append(nil, npdu.NPDU{Version: 1, APDU: pdu})
	if err != nil {
		panic(err)
	}
	f, err := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionOriginalUnicastNPDU, Payload: n})
	if err != nil {
		panic(err)
	}
	return f
}

func fuzzSeeds() [][2][]byte {
	rp, _ := service.EncodeReadPropertyACK(service.ReadPropertyACK{Object: fuzzObject, Property: fuzzProp, Value: bacnet.RealValue(1)})
	ack := fuzzFrame(apdu.AppendComplexACK(nil, apdu.ComplexACK{InvokeID: 0, ServiceChoice: apdu.ServiceReadProperty, Payload: rp}))
	seg := func(seq uint8, more bool) []byte {
		return fuzzFrame(apdu.AppendComplexACK(nil, apdu.ComplexACK{
			SegmentedMessage: true, MoreFollows: more, InvokeID: 0, SequenceNumber: seq, ProposedWindowSize: 2,
			ServiceChoice: apdu.ServiceReadProperty, Payload: rp[:len(rp)/2],
		}))
	}
	iam, _ := service.EncodeIAm(service.IAm{Device: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 7}, MaxAPDULength: 1476, VendorID: 1})
	iamFrame := fuzzFrame(apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{ServiceChoice: apdu.ServiceIAm, Payload: iam}))
	errPDU := fuzzFrame(apdu.AppendError(nil, apdu.ErrorPDU{InvokeID: 0, ServiceChoice: apdu.ServiceReadProperty, Payload: []byte{0x91, 0x02, 0x91, 0x20}}))
	abort := fuzzFrame(apdu.AppendAbort(nil, apdu.AbortPDU{InvokeID: 0, Server: true, Reason: 4}))
	segACK := fuzzFrame(apdu.AppendSegmentACK(nil, apdu.SegmentACK{InvokeID: 0, Server: true, ActualWindowSize: 1}))
	cov := fuzzFrame(apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{ServiceChoice: apdu.ServiceUnconfirmedCOV,
		Payload: []byte{0x09, 0x01, 0x1C, 0x02, 0x00, 0x00, 0x07, 0x2C, 0x00, 0x80, 0x00, 0x01, 0x39, 0x3C, 0x4E, 0x09, 0x55, 0x2E, 0x44, 0x41, 0xA8, 0x00, 0x00, 0x2F, 0x4F}}))
	confirmedCOV := fuzzFrame(apdu.AppendConfirmedRequest(nil, apdu.ConfirmedRequest{InvokeID: 3, ServiceChoice: apdu.ServiceConfirmedCOVNotification, MaxAPDU: 5,
		Payload: []byte{0x09, 0x01, 0x1C, 0x02, 0x00, 0x00, 0x07, 0x2C, 0x00, 0x80, 0x00, 0x01, 0x39, 0x3C, 0x4E, 0x09, 0x55, 0x2E, 0x44, 0x41, 0xA8, 0x00, 0x00, 0x2F, 0x4F}}))
	routed := []byte{0x81, 0x0A, 0x00, 0x0F, 0x01, 0x08, 0x00, 0x07, 0x01, 0x2A, 0x30, 0x00, 0x0C, 0x0C, 0x00}
	netMsg := []byte{0x81, 0x0A, 0x00, 0x09, 0x01, 0x80, 0x01, 0x00, 0x05}
	forwarded := []byte{0x81, 0x04, 0x00, 0x0E, 10, 0, 0, 2, 0xBA, 0xC0, 0x01, 0x00, 0x10, 0x08}
	bvlcResult := []byte{0x81, 0x00, 0x00, 0x06, 0x00, 0x30}
	return [][2][]byte{
		{ack, nil},
		{seg(0, true), seg(1, false)},
		{seg(0, true), seg(0, true)},
		{iamFrame, iamFrame},
		{errPDU, ack},
		{abort, nil},
		{segACK, nil},
		{cov, confirmedCOV},
		{routed, netMsg},
		{forwarded, bvlcResult},
		{{0x81, 0x0A, 0xFF, 0xFF}, {}},
	}
}

// FuzzClientReceive drives the client's whole receive path — BVLC, NPDU,
// APDU, transaction matching, segment reassembly, the device registry, COV
// and event handlers — with two arbitrary datagrams from a peer, while a
// ReadProperty to that peer is pending. The client must not panic, the
// pending call must end once its context is cancelled, and Close must
// return.
func FuzzClientReceive(f *testing.F) {
	for _, s := range fuzzSeeds() {
		f.Add(s[0], s[1])
	}
	f.Fuzz(func(t *testing.T, first, second []byte) {
		clk := clock.NewManual(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
		local := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.1:47808"))
		tr := virtual.New(local, clk, 4)
		c, err := New(
			WithTransport(AdaptVirtual(tr)),
			withClock(clk),
			WithTransactionOptions(time.Second, 0, time.Second),
			WithEventNotificationHandler(func(EventNotificationDelivery) {}),
		)
		if err != nil {
			t.Fatal(err)
		}
		stream := c.OpenEventStream(4)

		target := Target{
			Address:  bacnet.LocalStation(bacnet.MustMAC([]byte{10, 0, 0, 2, 0xBA, 0xC0})),
			Endpoint: fuzzPeer,
			MaxAPDU:  1476,
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, _ = c.ReadProperty(ctx, target, fuzzObject, fuzzProp)
		}()
		// Wait until the request is on the wire, so invoke ID 0 is pending.
		for deadline := time.Now().Add(5 * time.Second); len(tr.Outbox()) == 0; {
			if time.Now().After(deadline) {
				t.Fatal("request never sent")
			}
			time.Sleep(50 * time.Microsecond)
		}

		c.handlePacket(InboundPacket{Data: first, ImmediatePeer: fuzzPeer, ReceivedAt: clk.Now()})
		c.handlePacket(InboundPacket{Data: second, ImmediatePeer: fuzzPeer, ReceivedAt: clk.Now()})

		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("ReadProperty did not end after its context was cancelled")
		}
		stream.Close()
		closed := make(chan struct{})
		go func() { _ = c.Close(); close(closed) }()
		select {
		case <-closed:
		case <-time.After(5 * time.Second):
			t.Fatal("Close did not return")
		}
	})
}
