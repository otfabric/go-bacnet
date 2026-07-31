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

// virtualPair is a linked client/peer transport pair for confirmed-service tests.
type virtualPair struct {
	Client   *Client
	ClientTr *virtual.Transport
	PeerTr   *virtual.Transport
	Local    bip.Endpoint
	Peer     bip.Endpoint
	Clk      *clock.Manual
	Target   Target
}

func newVirtualPair(t *testing.T, opts ...Option) *virtualPair {
	t.Helper()
	clk := clock.NewManual(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	local := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.1:47808"))
	peerEP := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.2:47808"))
	clientTr := virtual.New(local, clk, 32)
	peerTr := virtual.New(peerEP, clk, 32)
	virtual.Link(clientTr, peerTr)

	base := []Option{
		WithTransport(AdaptVirtual(clientTr)),
		withClock(clk),
		WithTransactionOptions(time.Second, 0, time.Second),
	}
	c, err := New(append(base, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	mac := []byte{10, 0, 0, 2, 0xBA, 0xC0}
	return &virtualPair{
		Client:   c,
		ClientTr: clientTr,
		PeerTr:   peerTr,
		Local:    local,
		Peer:     peerEP,
		Clk:      clk,
		Target: Target{
			Address:  bacnet.LocalStation(bacnet.MustMAC(mac)),
			Endpoint: peerEP,
			MaxAPDU:  480,
		},
	}
}

func injectUnicastNPDU(t *testing.T, tr *virtual.Transport, from bip.Endpoint, at time.Time, apduBytes []byte) {
	t.Helper()
	injectUnicastNPDUWithSource(t, tr, from, at, npdu.NPDU{Version: npdu.Version1, APDU: apduBytes})
}

func injectUnicastNPDUWithSource(t *testing.T, tr *virtual.Transport, from bip.Endpoint, at time.Time, n npdu.NPDU) {
	t.Helper()
	nraw, err := npdu.Append(nil, n)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionOriginalUnicastNPDU, Payload: nraw})
	if err != nil {
		t.Fatal(err)
	}
	tr.Inject(virtual.InboundPacket{Data: frame, ImmediatePeer: from, ReceivedAt: at})
}

func injectSegmentedComplexACK(t *testing.T, env *virtualPair, invokeID, seq uint8, more bool, serviceChoice uint8, payload []byte) {
	t.Helper()
	seg := apdu.AppendComplexACK(nil, apdu.ComplexACK{
		SegmentedMessage:   true,
		MoreFollows:        more,
		InvokeID:           invokeID,
		SequenceNumber:     seq,
		ProposedWindowSize: 1,
		ServiceChoice:      serviceChoice,
		Payload:            payload,
	})
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), seg)
}

func outboxSegmentACK(t *testing.T, tr *virtual.Transport, since int, invokeID uint8) (gotPositive, gotNegative bool) {
	t.Helper()
	limits := bacnet.DefaultDecodeLimits()
	for _, pkt := range tr.Outbox()[since:] {
		msg, err := bvlc.Parse(pkt.Data, limits)
		if err != nil {
			continue
		}
		n, _, err := npdu.Parse(msg.Payload, limits)
		if err != nil || len(n.APDU) == 0 {
			continue
		}
		pdu, err := apdu.Parse(n.APDU, limits)
		if err != nil || pdu.SegmentACK == nil || pdu.SegmentACK.InvokeID != invokeID {
			continue
		}
		if pdu.SegmentACK.NegativeACK {
			gotNegative = true
		} else {
			gotPositive = true
		}
	}
	return gotPositive, gotNegative
}

func injectBroadcastNPDU(t *testing.T, tr *virtual.Transport, from bip.Endpoint, at time.Time, apduBytes []byte) {
	t.Helper()
	nraw, err := npdu.Append(nil, npdu.NPDU{Version: npdu.Version1, APDU: apduBytes})
	if err != nil {
		t.Fatal(err)
	}
	frame, err := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionOriginalBroadcastNPDU, Payload: nraw})
	if err != nil {
		t.Fatal(err)
	}
	tr.Inject(virtual.InboundPacket{Data: frame, ImmediatePeer: from, ReceivedAt: at})
}

func waitConfirmedInvokeID(t *testing.T, clientTr *virtual.Transport, timeout time.Duration) (uint8, uint8) {
	t.Helper()
	return waitConfirmedInvokeIDSince(t, clientTr, 0, timeout)
}

func waitConfirmedInvokeIDSince(t *testing.T, clientTr *virtual.Transport, since int, timeout time.Duration) (uint8, uint8) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	limits := bacnet.DefaultDecodeLimits()
	for time.Now().Before(deadline) {
		out := clientTr.Outbox()
		if since > len(out) {
			since = len(out)
		}
		for _, pkt := range out[since:] {
			msg, err := bvlc.Parse(pkt.Data, limits)
			if err != nil {
				continue
			}
			n, _, err := npdu.Parse(msg.Payload, limits)
			if err != nil || len(n.APDU) == 0 {
				continue
			}
			pdu, err := apdu.Parse(n.APDU, limits)
			if err != nil || pdu.ConfirmedRequest == nil {
				continue
			}
			return pdu.ConfirmedRequest.InvokeID, pdu.ConfirmedRequest.ServiceChoice
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for confirmed request on peer outbox")
	return 0, 0
}

// serveSimpleACK runs until ctx is done, answering confirmed requests with SimpleACK.
func serveSimpleACK(ctx context.Context, peerTr *virtual.Transport, dest bip.Endpoint) {
	limits := bacnet.DefaultDecodeLimits()
	for {
		pkt, err := peerTr.Recv(ctx)
		if err != nil {
			return
		}
		msg, err := bvlc.Parse(pkt.Data, limits)
		if err != nil {
			continue
		}
		n, _, err := npdu.Parse(msg.Payload, limits)
		if err != nil || len(n.APDU) == 0 {
			continue
		}
		pdu, err := apdu.Parse(n.APDU, limits)
		if err != nil || pdu.ConfirmedRequest == nil {
			continue
		}
		req := pdu.ConfirmedRequest
		apduBytes := apdu.AppendSimpleACK(nil, apdu.SimpleACK{
			InvokeID:      req.InvokeID,
			ServiceChoice: req.ServiceChoice,
		})
		nraw, _ := npdu.Append(nil, npdu.NPDU{Version: npdu.Version1, APDU: apduBytes})
		frame, _ := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionOriginalUnicastNPDU, Payload: nraw})
		_ = peerTr.Send(ctx, virtual.OutboundPacket{Data: frame, Destination: dest})
	}
}

// serveComplexACK runs until ctx is done, answering confirmed requests with ComplexACK.
func serveComplexACK(ctx context.Context, peerTr *virtual.Transport, dest bip.Endpoint, payload func(serviceChoice uint8) ([]byte, error)) {
	limits := bacnet.DefaultDecodeLimits()
	for {
		pkt, err := peerTr.Recv(ctx)
		if err != nil {
			return
		}
		msg, err := bvlc.Parse(pkt.Data, limits)
		if err != nil {
			continue
		}
		n, _, err := npdu.Parse(msg.Payload, limits)
		if err != nil || len(n.APDU) == 0 {
			continue
		}
		pdu, err := apdu.Parse(n.APDU, limits)
		if err != nil || pdu.ConfirmedRequest == nil {
			continue
		}
		req := pdu.ConfirmedRequest
		ackPayload, err := payload(req.ServiceChoice)
		if err != nil {
			continue
		}
		apduBytes := apdu.AppendComplexACK(nil, apdu.ComplexACK{
			InvokeID:      req.InvokeID,
			ServiceChoice: req.ServiceChoice,
			Payload:       ackPayload,
		})
		nraw, _ := npdu.Append(nil, npdu.NPDU{Version: npdu.Version1, APDU: apduBytes})
		frame, _ := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionOriginalUnicastNPDU, Payload: nraw})
		_ = peerTr.Send(ctx, virtual.OutboundPacket{Data: frame, Destination: dest})
	}
}

func encodeCOVNotification(t *testing.T, note service.COVNotification) []byte {
	t.Helper()
	var dst []byte
	var err error
	dst, err = bacnet.AppendContextUnsigned(dst, 0, uint64(note.ProcessIdentifier))
	if err != nil {
		t.Fatal(err)
	}
	dst, err = bacnet.AppendContextObjectID(dst, 1, note.InitiatingDevice)
	if err != nil {
		t.Fatal(err)
	}
	dst, err = bacnet.AppendContextObjectID(dst, 2, note.MonitoredObject)
	if err != nil {
		t.Fatal(err)
	}
	dst, err = bacnet.AppendContextUnsigned(dst, 3, uint64(note.TimeRemaining))
	if err != nil {
		t.Fatal(err)
	}
	dst = append(dst, 0x4E)
	for _, pv := range note.Values {
		dst, err = bacnet.AppendContextUnsigned(dst, 0, uint64(pv.Property.Identifier))
		if err != nil {
			t.Fatal(err)
		}
		if pv.Property.ArrayIndex != nil {
			dst, err = bacnet.AppendContextUnsigned(dst, 1, uint64(*pv.Property.ArrayIndex))
			if err != nil {
				t.Fatal(err)
			}
		}
		dst = append(dst, 0x2E)
		dst, err = bacnet.AppendTag(dst, bacnet.Element{Value: pv.Value})
		if err != nil {
			t.Fatal(err)
		}
		dst = append(dst, 0x2F)
	}
	dst = append(dst, 0x4F)
	return dst
}
