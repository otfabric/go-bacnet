// SPDX-License-Identifier: MIT

package client

import (
	"bytes"
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

// oversizedSegmentPeer answers the first ReadProperty with a segmented
// ComplexACK whose segments carry segmentData octets of service data each,
// plus the 5-octet segmented ComplexACK header, as legacy BACpypes 0.19.0 does:
// for a requester that accepts 1476 octets its segments are 1481 octets long
// (recorded by bacnet-interop's bacpypes image; clause 20.1.2.5 bounds an APDU
// by the receiver's Max_APDU_Length_Accepted). It sends the advertised max
// APDU code of the request on gotMaxAPDU.
func oversizedSegmentPeer(t *testing.T, peer *virtual.Transport, local bip.Endpoint, ack []byte, segmentData int, gotMaxAPDU chan<- uint8) {
	t.Helper()
	go func() {
		ctx := context.Background()
		for {
			pkt, err := peer.Recv(ctx)
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
			req, err := apdu.Parse(n.APDU, bacnet.DefaultDecodeLimits())
			if err != nil || req.ConfirmedRequest == nil {
				continue // SegmentACK
			}
			gotMaxAPDU <- req.ConfirmedRequest.MaxAPDU
			id := req.ConfirmedRequest.InvokeID
			for seq, off := 0, 0; off < len(ack); seq++ {
				end := min(off+segmentData, len(ack))
				raw, _ := npdu.Append(nil, npdu.NPDU{Version: npdu.Version1, APDU: apdu.AppendComplexACK(nil, apdu.ComplexACK{
					SegmentedMessage:   true,
					MoreFollows:        end < len(ack),
					InvokeID:           id,
					SequenceNumber:     uint8(seq),
					ProposedWindowSize: 1,
					ServiceChoice:      apdu.ServiceReadProperty,
					Payload:            ack[off:end],
				})})
				frame, _ := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionOriginalUnicastNPDU, Payload: raw})
				_ = peer.Send(ctx, virtual.OutboundPacket{Data: frame, Destination: local})
				off = end
			}
			return
		}
	}()
}

// TestSegmentLongerThanAdvertisedMaxAPDU: with the default decode limits the
// client still advertises 1476 octets (max APDU code 5) but receives segments
// longer than that, as the server does (server.WithDecodeLimits).
func TestSegmentLongerThanAdvertisedMaxAPDU(t *testing.T) {
	clk := clock.NewManual(time.Unix(0, 0).UTC())
	local := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.1:47808"))
	peerEP := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.2:47808"))
	tr := virtual.New(local, clk, 32)
	peer := virtual.New(peerEP, clk, 32)
	virtual.Link(tr, peer)

	c, err := New(WithTransport(AdaptVirtual(tr)), withClock(clk), WithTransactionOptions(2*time.Second, 0, time.Second))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 3}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyDescription}
	content := bytes.Repeat([]byte{0xA5}, 3000)
	ack, err := service.EncodeReadPropertyACK(service.ReadPropertyACK{
		Object: obj, Property: prop,
		Value: bacnet.ApplicationValue{Kind: bacnet.ValueOctetString, OctetString: content},
	})
	if err != nil {
		t.Fatal(err)
	}
	gotMax := make(chan uint8, 1)
	oversizedSegmentPeer(t, peer, local, ack, 1476, gotMax)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	v, err := c.ReadProperty(ctx, Target{
		Address:  bacnet.LocalStation(bacnet.MustMAC([]byte{10, 0, 0, 2, 0xBA, 0xC0})),
		Endpoint: peerEP,
		MaxAPDU:  1476,
	}, obj, prop)
	if err != nil {
		t.Fatalf("ReadProperty with 1481-octet segments: %v", err)
	}
	if !bytes.Equal(v.OctetString, content) {
		t.Fatalf("value: %d octets, want the 3000 sent", len(v.OctetString))
	}
	if code := <-gotMax; code != 5 {
		t.Fatalf("advertised max APDU code %d, want 5 (1476 octets)", code)
	}
}

func TestDefaultDecodeLimitsAndAdvertisedMaxAPDU(t *testing.T) {
	c, err := New(WithTransport(AdaptVirtual(virtual.New(bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.1:47808")), clock.Real{}, 1))))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if got, want := c.limits.MaxAPDUSize, bacnet.DefaultDecodeLimits().MaxDatagramSize; got != want {
		t.Fatalf("default MaxAPDUSize %d, want the datagram bound %d", got, want)
	}
	if got, err := c.advertisedMaxAPDU(); err != nil || got != 1476 {
		t.Fatalf("advertised %d, %v; want 1476", got, err)
	}

	// Strict limits are kept as given and advertised as they are.
	strict := bacnet.DefaultDecodeLimits()
	strict.MaxAPDUSize = 480
	s, err := New(WithTransport(AdaptVirtual(virtual.New(bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.1:47808")), clock.Real{}, 1))), WithDecodeLimits(strict))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if got, err := s.advertisedMaxAPDU(); err != nil || got != 480 {
		t.Fatalf("strict: advertised %d, %v; want 480", got, err)
	}
}
