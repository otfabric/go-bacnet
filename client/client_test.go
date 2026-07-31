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

func TestDiscoverIAmVirtual(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	local := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.2:47808"))
	peer := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.10:47808"))
	tr := virtual.New(local, clk, 16)
	c, err := New(WithTransport(AdaptVirtual(tr)), withClock(clk))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	iamPayload, err := service.EncodeIAm(service.IAm{
		Device:        bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 42},
		MaxAPDULength: 480,
		Segmentation:  0,
		VendorID:      999,
	})
	if err != nil {
		t.Fatal(err)
	}
	apduBytes := apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{
		ServiceChoice: apdu.ServiceIAm,
		Payload:       iamPayload,
	})
	nraw, err := npdu.Append(nil, npdu.NPDU{Version: 1, APDU: apduBytes})
	if err != nil {
		t.Fatal(err)
	}
	frame, err := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionOriginalBroadcastNPDU, Payload: nraw})
	if err != nil {
		t.Fatal(err)
	}
	tr.Inject(virtual.InboundPacket{Data: frame, ImmediatePeer: peer, ReceivedAt: clk.Now()})

	// Allow recv loop to process.
	time.Sleep(20 * time.Millisecond)
	obs := c.Devices()
	if len(obs) != 1 || obs[0].Instance != 42 {
		t.Fatalf("observations %#v", obs)
	}
	if !obs[0].Capabilities.MaxAPDULengthAccepted.Known || obs[0].Capabilities.MaxAPDULengthAccepted.Source != CapabilityFromIAm {
		t.Fatalf("capabilities %#v", obs[0].Capabilities)
	}
	if obs[0].Capabilities.ProtocolRevision.Known {
		t.Fatal("protocol revision must remain unknown after I-Am")
	}
}

func TestReadPropertyVirtual(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	local := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.1:47808"))
	peerEP := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.2:47808"))
	clientTr := virtual.New(local, clk, 32)
	peerTr := virtual.New(peerEP, clk, 32)
	virtual.Link(clientTr, peerTr)

	c, err := New(
		WithTransport(AdaptVirtual(clientTr)),
		withClock(clk),
		WithTransactionOptions(time.Second, 0, time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	// Peer responder.
	go func() {
		ctx := context.Background()
		for {
			pkt, err := peerTr.Recv(ctx)
			if err != nil {
				return
			}
			msg, err := bvlc.Parse(pkt.Data, bacnet.DefaultDecodeLimits())
			if err != nil {
				continue
			}
			n, _, err := npdu.Parse(msg.Payload, bacnet.DefaultDecodeLimits())
			if err != nil || len(n.APDU) == 0 {
				continue
			}
			pdu, err := apdu.Parse(n.APDU, bacnet.DefaultDecodeLimits())
			if err != nil || pdu.ConfirmedRequest == nil {
				continue
			}
			ackPayload, err := service.EncodeReadPropertyACK(service.ReadPropertyACK{
				Object:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogInput, Instance: 3},
				Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
				Value:    bacnet.RealValue(21.5),
			})
			if err != nil {
				continue
			}
			apduBytes := apdu.AppendComplexACK(nil, apdu.ComplexACK{
				InvokeID:      pdu.ConfirmedRequest.InvokeID,
				ServiceChoice: apdu.ServiceReadProperty,
				Payload:       ackPayload,
			})
			nraw, _ := npdu.Append(nil, npdu.NPDU{Version: 1, APDU: apduBytes})
			frame, _ := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionOriginalUnicastNPDU, Payload: nraw})
			_ = peerTr.Send(ctx, virtual.OutboundPacket{Data: frame, Destination: local})
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	val, err := c.ReadProperty(ctx, Target{
		Address:  bacnet.LocalStation(bacnet.MustMAC([]byte{10, 0, 0, 2, 0xBA, 0xC0})),
		Endpoint: peerEP,
		MaxAPDU:  480,
	}, bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogInput, Instance: 3}, bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue})
	if err != nil {
		t.Fatal(err)
	}
	f, err := bacnet.AsReal(val)
	if err != nil || f != 21.5 {
		t.Fatalf("value %v err=%v", val, err)
	}
}

func TestCloseIdempotent(t *testing.T) {
	clk := clock.NewManual(time.Now())
	tr := virtual.New(bip.NewEndpoint(netip.MustParseAddrPort("127.0.0.1:47808")), clk, 8)
	c, err := New(WithTransport(AdaptVirtual(tr)), withClock(clk))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRetransmitPolicyWriteDisabled(t *testing.T) {
	if DefaultRetransmitPolicy(apdu.ServiceWriteProperty) != RetransmitDisabled {
		t.Fatal("WriteProperty retransmission should be disabled by default")
	}
	if DefaultRetransmitPolicy(apdu.ServiceReadProperty) != RetransmitEnabled {
		t.Fatal("ReadProperty retransmission should be enabled")
	}
}
