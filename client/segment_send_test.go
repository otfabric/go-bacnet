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
	"github.com/otfabric/go-bacnet/internal/virtual"
	"github.com/otfabric/go-bacnet/npdu"
	"github.com/otfabric/go-bacnet/service"
)

func TestBuildConfirmedSegmentsRoundTrip(t *testing.T) {
	payload := make([]byte, 100)
	for i := range payload {
		payload[i] = byte(i)
	}
	segs, err := buildConfirmedSegments(apdu.ConfirmedRequest{
		SegmentedResponseAccepted: true,
		MaxSegments:               4,
		MaxAPDU:                   3,
		InvokeID:                  7,
		ServiceChoice:             apdu.ServiceWritePropertyMultiple,
		Payload:                   payload,
	}, 50, 16)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) < 2 {
		t.Fatalf("expected multiple segments, got %d", len(segs))
	}
	for i, raw := range segs {
		if len(raw) > 50 {
			t.Fatalf("segment %d len=%d exceeds remote max 50", i, len(raw))
		}
	}
	var reassembled []byte
	var serviceChoice uint8
	for i, raw := range segs {
		pdu, err := apdu.Parse(raw, bacnet.DefaultDecodeLimits())
		if err != nil {
			t.Fatal(err)
		}
		req := pdu.ConfirmedRequest
		if req == nil || !req.SegmentedMessage {
			t.Fatal("expected segmented confirmed request")
		}
		if uint8(i) != req.SequenceNumber {
			t.Fatalf("seq=%d want %d", req.SequenceNumber, i)
		}
		if i == 0 {
			serviceChoice = req.ServiceChoice
		} else if req.ServiceChoice != serviceChoice {
			t.Fatalf("service choice=%d want %d on segment %d", req.ServiceChoice, serviceChoice, i)
		}
		reassembled = append(reassembled, req.Payload...)
		if i == len(segs)-1 && req.MoreFollows {
			t.Fatal("last segment should clear MoreFollows")
		}
	}
	if serviceChoice != apdu.ServiceWritePropertyMultiple {
		t.Fatalf("service=%d", serviceChoice)
	}
	if string(reassembled) != string(payload) {
		t.Fatal("payload mismatch")
	}
}

func TestBuildConfirmedSegmentsExactFits(t *testing.T) {
	for _, remoteMax := range []int{50, 128, 206, 480} {
		payloadFit := make([]byte, remoteMax-confirmedSeg0Overhead)
		segs, err := buildConfirmedSegments(apdu.ConfirmedRequest{
			SegmentedResponseAccepted: true,
			MaxSegments:               7,
			MaxAPDU:                   5,
			InvokeID:                  1,
			ServiceChoice:             apdu.ServiceWritePropertyMultiple,
			Payload:                   payloadFit,
		}, remoteMax, 1)
		if err != nil {
			t.Fatalf("remoteMax=%d fit: %v", remoteMax, err)
		}
		if len(segs) != 1 {
			t.Fatalf("remoteMax=%d: want 1 segment for exact fit, got %d", remoteMax, len(segs))
		}
		if len(segs[0]) != remoteMax {
			t.Fatalf("remoteMax=%d: encoded len=%d want %d", remoteMax, len(segs[0]), remoteMax)
		}

		payloadOverflow := make([]byte, remoteMax-confirmedSeg0Overhead+1)
		segs, err = buildConfirmedSegments(apdu.ConfirmedRequest{
			SegmentedResponseAccepted: true,
			MaxSegments:               7,
			MaxAPDU:                   5,
			InvokeID:                  1,
			ServiceChoice:             apdu.ServiceWritePropertyMultiple,
			Payload:                   payloadOverflow,
		}, remoteMax, 1)
		if err != nil {
			t.Fatalf("remoteMax=%d overflow: %v", remoteMax, err)
		}
		if len(segs) != 2 {
			t.Fatalf("remoteMax=%d: want 2 segments for +1 overflow, got %d", remoteMax, len(segs))
		}
		for i, raw := range segs {
			if len(raw) > remoteMax {
				t.Fatalf("remoteMax=%d segment %d len=%d", remoteMax, i, len(raw))
			}
		}
	}
}

func TestSegmentedWritePropertyMultipleSimpleACK(t *testing.T) {
	env := newVirtualPair(t, WithTransactionOptions(time.Second, 0, time.Second), WithSegmentWindow(1))
	seedPeerSegmentation(env, 50, segmentationReceive)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go serveSegmentedConfirmedSimpleACK(ctx, env.PeerTr, env.Local)

	// Force segmentation: small remote max + large WPM payload.
	target := env.Target
	target.MaxAPDU = 50

	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogOutput, Instance: 1}
	props := make([]service.WritePropertyValue, 0, 8)
	for i := 0; i < 8; i++ {
		props = append(props, service.WritePropertyValue{
			Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
			Value: bacnet.ApplicationValue{
				Kind:      bacnet.ValueCharacterString,
				Character: bacnet.CharacterString{Encoding: 0, Value: string(make([]byte, 20))},
			},
		})
	}
	specs := []service.WriteAccessSpecification{{Object: obj, Properties: props}}

	if err := env.Client.WritePropertyMultiple(ctx, target, specs); err != nil {
		t.Fatal(err)
	}
}

func TestSegmentedWritePropertyMultipleWindow4(t *testing.T) {
	env := newVirtualPair(t, WithTransactionOptions(time.Second, 0, time.Second), WithSegmentWindow(4))
	seedPeerSegmentation(env, 50, segmentationReceive)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go serveSegmentedConfirmedWindowedACK(ctx, env.PeerTr, env.Local, 4)

	target := env.Target
	target.MaxAPDU = 50

	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogOutput, Instance: 1}
	props := make([]service.WritePropertyValue, 0, 8)
	for i := 0; i < 8; i++ {
		props = append(props, service.WritePropertyValue{
			Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
			Value: bacnet.ApplicationValue{
				Kind:      bacnet.ValueCharacterString,
				Character: bacnet.CharacterString{Encoding: 0, Value: string(make([]byte, 20))},
			},
		})
	}
	if err := env.Client.WritePropertyMultiple(ctx, target, []service.WriteAccessSpecification{
		{Object: obj, Properties: props},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSegmentedSendTimeoutAborts(t *testing.T) {
	env := newVirtualPair(t, WithTransactionOptions(time.Second, 0, 200*time.Millisecond), WithSegmentWindow(1))
	seedPeerSegmentation(env, 50, segmentationReceive)
	target := env.Target
	target.MaxAPDU = 50

	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogOutput, Instance: 1}
	props := make([]service.WritePropertyValue, 0, 8)
	for i := 0; i < 8; i++ {
		props = append(props, service.WritePropertyValue{
			Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
			Value: bacnet.ApplicationValue{
				Kind:      bacnet.ValueCharacterString,
				Character: bacnet.CharacterString{Encoding: 0, Value: string(make([]byte, 20))},
			},
		})
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- env.Client.WritePropertyMultiple(context.Background(), target, []service.WriteAccessSpecification{
			{Object: obj, Properties: props},
		})
	}()
	_, _ = waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	env.Clk.Advance(250 * time.Millisecond)

	err := <-errCh
	var unknown *bacnet.OutcomeUnknownError
	if !errors.As(err, &unknown) || unknown.Operation != "WritePropertyMultiple" {
		t.Fatalf("expected outcome-unknown WPM, got %v", err)
	}
	var abort *bacnet.AbortError
	if !errors.As(err, &abort) || abort.Reason != abortReasonTSMTimeout {
		t.Fatalf("expected TSM Abort cause, got %v", err)
	}
}

func TestPeerAcceptsSegmentedRequestsTransmitOnly(t *testing.T) {
	env := newVirtualPair(t)
	seedPeerSegmentation(env, 480, segmentationTransmit)
	if env.Client.peerAcceptsSegmentedRequests(env.Target) {
		t.Fatal("segmented-transmit peer must not accept segmented requests")
	}
	seedPeerSegmentation(env, 480, segmentationBoth)
	if !env.Client.peerAcceptsSegmentedRequests(env.Target) {
		t.Fatal("segmented-both peer should accept segmented requests")
	}
}

func TestBuildConfirmedSegmentsRemoteMaxTooSmall(t *testing.T) {
	_, err := buildConfirmedSegments(apdu.ConfirmedRequest{
		InvokeID: 1, ServiceChoice: 16, Payload: []byte{1, 2, 3},
	}, 6, 16)
	if !errors.Is(err, bacnet.ErrAPDUTooLarge) {
		t.Fatalf("got %v", err)
	}
}

func TestSegmentSenderDeliverIgnores(t *testing.T) {
	env := newVirtualPair(t)
	env.Client.seg.send.deliver(nil, packetSource{})
	tx := &pendingTx{
		invokeID: 7, address: env.Target.Address, immediate: env.Peer,
	}
	ch := env.Client.seg.send.register(tx)
	env.Client.seg.send.deliver(&apdu.SegmentACK{InvokeID: 7, Server: true}, packetSource{
		immediate: bip.NewEndpoint(netip.MustParseAddrPort("10.9.9.9:47808")),
	})
	select {
	case <-ch:
		t.Fatal("mismatched source must not deliver")
	default:
	}
	// Client-direction SegmentACK (Server=false) must not advance send state.
	env.Client.seg.send.deliver(&apdu.SegmentACK{
		InvokeID: 7, Server: false, SequenceNumber: 0, ActualWindowSize: 1,
	}, packetSource{immediate: env.Peer, bacnetAddress: env.Target.Address})
	select {
	case <-ch:
		t.Fatal("client SegmentACK must not deliver to send state machine")
	default:
	}
	env.Client.seg.send.unregister(7)
}

func TestSegmentedRequestRefusedWithoutPeerCapability(t *testing.T) {
	env := newVirtualPair(t)
	target := env.Target
	target.MaxAPDU = 50

	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogOutput, Instance: 1}
	props := make([]service.WritePropertyValue, 0, 8)
	for i := 0; i < 8; i++ {
		props = append(props, service.WritePropertyValue{
			Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
			Value: bacnet.ApplicationValue{
				Kind:      bacnet.ValueCharacterString,
				Character: bacnet.CharacterString{Encoding: 0, Value: string(make([]byte, 20))},
			},
		})
	}
	err := env.Client.WritePropertyMultiple(context.Background(), target, []service.WriteAccessSpecification{
		{Object: obj, Properties: props},
	})
	var tooLarge *bacnet.APDUTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("expected APDUTooLargeError, got %v", err)
	}
	if tooLarge.SegmentationSupported {
		t.Fatal("segmentation should not be claimed without peer evidence")
	}
}

func TestSegmentedSendNAKThenSucceeds(t *testing.T) {
	env := newVirtualPair(t, WithTransactionOptions(time.Second, 2, time.Second), WithSegmentWindow(1))
	seedPeerSegmentation(env, 50, segmentationBoth)
	target := env.Target
	target.MaxAPDU = 50

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go serveSegmentedConfirmedSimpleACKOnceNAK(ctx, env.PeerTr, env.Local)

	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogOutput, Instance: 1}
	props := make([]service.WritePropertyValue, 0, 8)
	for i := 0; i < 8; i++ {
		props = append(props, service.WritePropertyValue{
			Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
			Value: bacnet.ApplicationValue{
				Kind:      bacnet.ValueCharacterString,
				Character: bacnet.CharacterString{Encoding: 0, Value: string(make([]byte, 20))},
			},
		})
	}
	if err := env.Client.WritePropertyMultiple(ctx, target, []service.WriteAccessSpecification{
		{Object: obj, Properties: props},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSegmentedSendClosedDuringWait(t *testing.T) {
	env := newVirtualPair(t, WithTransactionOptions(time.Second, 0, time.Second), WithSegmentWindow(1))
	seedPeerSegmentation(env, 50, segmentationReceive)
	target := env.Target
	target.MaxAPDU = 50

	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogOutput, Instance: 1}
	props := make([]service.WritePropertyValue, 0, 8)
	for i := 0; i < 8; i++ {
		props = append(props, service.WritePropertyValue{
			Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
			Value: bacnet.ApplicationValue{
				Kind:      bacnet.ValueCharacterString,
				Character: bacnet.CharacterString{Encoding: 0, Value: string(make([]byte, 20))},
			},
		})
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- env.Client.WritePropertyMultiple(context.Background(), target, []service.WriteAccessSpecification{
			{Object: obj, Properties: props},
		})
	}()
	_, _ = waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	_ = env.Client.Close()
	err := <-errCh
	var unknown *bacnet.OutcomeUnknownError
	if !errors.As(err, &unknown) || !errors.Is(err, bacnet.ErrClosed) {
		t.Fatalf("got %v, want OutcomeUnknown wrapping ErrClosed", err)
	}
}

func TestSegmentedSendCancelOutcomeUnknown(t *testing.T) {
	env := newVirtualPair(t, WithTransactionOptions(time.Second, 0, time.Second), WithSegmentWindow(1))
	seedPeerSegmentation(env, 50, segmentationReceive)
	target := env.Target
	target.MaxAPDU = 50

	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogOutput, Instance: 1}
	props := make([]service.WritePropertyValue, 0, 8)
	for i := 0; i < 8; i++ {
		props = append(props, service.WritePropertyValue{
			Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
			Value: bacnet.ApplicationValue{
				Kind:      bacnet.ValueCharacterString,
				Character: bacnet.CharacterString{Encoding: 0, Value: string(make([]byte, 20))},
			},
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- env.Client.WritePropertyMultiple(ctx, target, []service.WriteAccessSpecification{
			{Object: obj, Properties: props},
		})
	}()
	_, _ = waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	cancel()
	err := <-errCh
	var unknown *bacnet.OutcomeUnknownError
	if !errors.As(err, &unknown) {
		t.Fatalf("expected outcome-unknown, got %v", err)
	}
}

func TestSegmentedSendExceedsMaxSegments(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	limits.MaxSegments = 1
	env := newVirtualPair(t, WithDecodeLimits(limits), WithSegmentWindow(1))
	seedPeerSegmentation(env, 50, segmentationReceive)
	target := env.Target
	target.MaxAPDU = 50

	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogOutput, Instance: 1}
	props := make([]service.WritePropertyValue, 0, 8)
	for i := 0; i < 8; i++ {
		props = append(props, service.WritePropertyValue{
			Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
			Value: bacnet.ApplicationValue{
				Kind:      bacnet.ValueCharacterString,
				Character: bacnet.CharacterString{Encoding: 0, Value: string(make([]byte, 20))},
			},
		})
	}
	err := env.Client.WritePropertyMultiple(context.Background(), target, []service.WriteAccessSpecification{
		{Object: obj, Properties: props},
	})
	var tooLarge *bacnet.APDUTooLargeError
	if !errors.As(err, &tooLarge) || !tooLarge.SegmentationSupported {
		t.Fatalf("expected APDUTooLarge with segmentation supported, got %v", err)
	}
}

func TestWritePropertyMultipleEncodeError(t *testing.T) {
	env := newVirtualPair(t)
	err := env.Client.WritePropertyMultiple(context.Background(), env.Target, nil)
	if !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("got %v", err)
	}
}

// serveSegmentedConfirmedSimpleACKOnceNAK NAKs the first segment once, then
// behaves like serveSegmentedConfirmedSimpleACK.
func serveSegmentedConfirmedSimpleACKOnceNAK(ctx context.Context, peerTr *virtual.Transport, dest bip.Endpoint) {
	limits := bacnet.DefaultDecodeLimits()
	type pending struct {
		service uint8
		next    uint8
	}
	active := map[uint8]*pending{}
	naked := map[uint8]bool{}

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
		if !req.SegmentedMessage {
			continue
		}
		if !naked[req.InvokeID] && req.SequenceNumber == 0 {
			naked[req.InvokeID] = true
			// No segment accepted yet: NAK with 255 so the client restarts at 0.
			nak := apdu.AppendSegmentACK(nil, apdu.SegmentACK{
				NegativeACK: true, Server: true, InvokeID: req.InvokeID,
				SequenceNumber: 255, ActualWindowSize: 1,
			})
			nraw, _ := npdu.Append(nil, npdu.NPDU{Version: npdu.Version1, APDU: nak})
			frame, _ := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionOriginalUnicastNPDU, Payload: nraw})
			_ = peerTr.Send(ctx, virtual.OutboundPacket{Data: frame, Destination: dest})
			continue
		}
		st, ok := active[req.InvokeID]
		if !ok {
			if req.SequenceNumber != 0 {
				continue
			}
			st = &pending{service: req.ServiceChoice, next: 0}
			active[req.InvokeID] = st
		}
		if req.SequenceNumber != st.next {
			continue
		}
		st.next++
		ack := apdu.AppendSegmentACK(nil, apdu.SegmentACK{
			Server: true, InvokeID: req.InvokeID,
			SequenceNumber: req.SequenceNumber, ActualWindowSize: 1,
		})
		nraw, _ := npdu.Append(nil, npdu.NPDU{Version: npdu.Version1, APDU: ack})
		frame, _ := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionOriginalUnicastNPDU, Payload: nraw})
		_ = peerTr.Send(ctx, virtual.OutboundPacket{Data: frame, Destination: dest})
		if !req.MoreFollows {
			serviceChoice := st.service
			delete(active, req.InvokeID)
			apduBytes := apdu.AppendSimpleACK(nil, apdu.SimpleACK{
				InvokeID: req.InvokeID, ServiceChoice: serviceChoice,
			})
			nraw, _ = npdu.Append(nil, npdu.NPDU{Version: npdu.Version1, APDU: apduBytes})
			frame, _ = bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionOriginalUnicastNPDU, Payload: nraw})
			_ = peerTr.Send(ctx, virtual.OutboundPacket{Data: frame, Destination: dest})
		}
	}
}

func seedPeerSegmentation(env *virtualPair, maxAPDU uint16, segmentation uint8) {
	caps := DeviceCapabilities{}
	caps.SetIAmFields(maxAPDU, segmentation, 1)
	env.Client.reg.Upsert(DeviceObservation{
		Instance:      1,
		Address:       env.Target.Address,
		ImmediatePeer: env.Peer,
		LastSeen:      env.Clk.Now(),
		Capabilities:  caps,
	})
}

// serveSegmentedConfirmedWindowedACK reassembles segmented confirmed requests
// using the given actual window size (ACK at window end or last segment).
func serveSegmentedConfirmedWindowedACK(ctx context.Context, peerTr *virtual.Transport, dest bip.Endpoint, window uint8) {
	if window == 0 {
		window = 1
	}
	limits := bacnet.DefaultDecodeLimits()
	type pending struct {
		service  uint8
		buf      []byte
		next     uint8
		inWindow uint8
	}
	active := map[uint8]*pending{}

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
		if !req.SegmentedMessage {
			apduBytes := apdu.AppendSimpleACK(nil, apdu.SimpleACK{
				InvokeID: req.InvokeID, ServiceChoice: req.ServiceChoice,
			})
			nraw, _ := npdu.Append(nil, npdu.NPDU{Version: npdu.Version1, APDU: apduBytes})
			frame, _ := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionOriginalUnicastNPDU, Payload: nraw})
			_ = peerTr.Send(ctx, virtual.OutboundPacket{Data: frame, Destination: dest})
			continue
		}

		st, ok := active[req.InvokeID]
		if !ok {
			if req.SequenceNumber != 0 {
				continue
			}
			st = &pending{service: req.ServiceChoice, next: 0}
			active[req.InvokeID] = st
		}
		if req.SequenceNumber != st.next {
			nak := apdu.AppendSegmentACK(nil, apdu.SegmentACK{
				NegativeACK: true, Server: true, InvokeID: req.InvokeID,
				SequenceNumber: st.next - 1, ActualWindowSize: window,
			})
			if st.next == 0 {
				nak = apdu.AppendSegmentACK(nil, apdu.SegmentACK{
					NegativeACK: true, Server: true, InvokeID: req.InvokeID,
					SequenceNumber: 0, ActualWindowSize: window,
				})
			}
			nraw, _ := npdu.Append(nil, npdu.NPDU{Version: npdu.Version1, APDU: nak})
			frame, _ := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionOriginalUnicastNPDU, Payload: nraw})
			_ = peerTr.Send(ctx, virtual.OutboundPacket{Data: frame, Destination: dest})
			continue
		}
		st.buf = append(st.buf, req.Payload...)
		st.next++
		st.inWindow++
		last := !req.MoreFollows
		if st.inWindow >= window || last {
			ack := apdu.AppendSegmentACK(nil, apdu.SegmentACK{
				Server: true, InvokeID: req.InvokeID,
				SequenceNumber: req.SequenceNumber, ActualWindowSize: window,
			})
			nraw, _ := npdu.Append(nil, npdu.NPDU{Version: npdu.Version1, APDU: ack})
			frame, _ := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionOriginalUnicastNPDU, Payload: nraw})
			_ = peerTr.Send(ctx, virtual.OutboundPacket{Data: frame, Destination: dest})
			st.inWindow = 0
		}
		if last {
			serviceChoice := st.service
			delete(active, req.InvokeID)
			apduBytes := apdu.AppendSimpleACK(nil, apdu.SimpleACK{
				InvokeID: req.InvokeID, ServiceChoice: serviceChoice,
			})
			nraw, _ := npdu.Append(nil, npdu.NPDU{Version: npdu.Version1, APDU: apduBytes})
			frame, _ := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionOriginalUnicastNPDU, Payload: nraw})
			_ = peerTr.Send(ctx, virtual.OutboundPacket{Data: frame, Destination: dest})
		}
	}
}

// serveSegmentedConfirmedSimpleACK reassembles segmented confirmed requests
// (window=1) and answers with SimpleACK.
func serveSegmentedConfirmedSimpleACK(ctx context.Context, peerTr *virtual.Transport, dest bip.Endpoint) {
	limits := bacnet.DefaultDecodeLimits()
	type pending struct {
		service uint8
		buf     []byte
		next    uint8
	}
	active := map[uint8]*pending{}

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
		if !req.SegmentedMessage {
			apduBytes := apdu.AppendSimpleACK(nil, apdu.SimpleACK{
				InvokeID: req.InvokeID, ServiceChoice: req.ServiceChoice,
			})
			nraw, _ := npdu.Append(nil, npdu.NPDU{Version: npdu.Version1, APDU: apduBytes})
			frame, _ := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionOriginalUnicastNPDU, Payload: nraw})
			_ = peerTr.Send(ctx, virtual.OutboundPacket{Data: frame, Destination: dest})
			continue
		}

		st, ok := active[req.InvokeID]
		if !ok {
			if req.SequenceNumber != 0 {
				continue
			}
			st = &pending{service: req.ServiceChoice, next: 0}
			active[req.InvokeID] = st
		}
		if req.SequenceNumber != st.next {
			nak := apdu.AppendSegmentACK(nil, apdu.SegmentACK{
				NegativeACK: true, Server: true, InvokeID: req.InvokeID,
				SequenceNumber: st.next - 1, ActualWindowSize: 1,
			})
			if st.next == 0 {
				nak = apdu.AppendSegmentACK(nil, apdu.SegmentACK{
					NegativeACK: true, Server: true, InvokeID: req.InvokeID,
					SequenceNumber: 0, ActualWindowSize: 1,
				})
			}
			nraw, _ := npdu.Append(nil, npdu.NPDU{Version: npdu.Version1, APDU: nak})
			frame, _ := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionOriginalUnicastNPDU, Payload: nraw})
			_ = peerTr.Send(ctx, virtual.OutboundPacket{Data: frame, Destination: dest})
			continue
		}
		st.buf = append(st.buf, req.Payload...)
		st.next++
		ack := apdu.AppendSegmentACK(nil, apdu.SegmentACK{
			Server: true, InvokeID: req.InvokeID,
			SequenceNumber: req.SequenceNumber, ActualWindowSize: 1,
		})
		nraw, _ := npdu.Append(nil, npdu.NPDU{Version: npdu.Version1, APDU: ack})
		frame, _ := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionOriginalUnicastNPDU, Payload: nraw})
		_ = peerTr.Send(ctx, virtual.OutboundPacket{Data: frame, Destination: dest})

		if !req.MoreFollows {
			serviceChoice := st.service
			delete(active, req.InvokeID)
			apduBytes := apdu.AppendSimpleACK(nil, apdu.SimpleACK{
				InvokeID: req.InvokeID, ServiceChoice: serviceChoice,
			})
			nraw, _ = npdu.Append(nil, npdu.NPDU{Version: npdu.Version1, APDU: apduBytes})
			frame, _ = bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionOriginalUnicastNPDU, Payload: nraw})
			_ = peerTr.Send(ctx, virtual.OutboundPacket{Data: frame, Destination: dest})
		}
	}
}
