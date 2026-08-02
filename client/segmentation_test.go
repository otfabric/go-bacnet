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
	"github.com/otfabric/go-bacnet/internal/clock"
	"github.com/otfabric/go-bacnet/internal/diag"
	"github.com/otfabric/go-bacnet/internal/virtual"
	"github.com/otfabric/go-bacnet/npdu"
	"github.com/otfabric/go-bacnet/service"
)

func TestSegmentedComplexACKReassembly(t *testing.T) {
	clk := clock.NewManual(time.Unix(0, 0).UTC())
	local := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.1:47808"))
	peerEP := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.2:47808"))
	tr := virtual.New(local, clk, 32)
	peer := virtual.New(peerEP, clk, 32)
	virtual.Link(tr, peer)

	c, err := New(
		WithTransport(AdaptVirtual(tr)),
		withClock(clk),
		WithTransactionOptions(2*time.Second, 0, time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}

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
				continue // SegmentACK or other
			}
			id := req.ConfirmedRequest.InvokeID
			ackPayload, err := service.EncodeReadPropertyACK(service.ReadPropertyACK{
				Object:   obj,
				Property: prop,
				Value:    bacnet.RealValue(3.5),
			})
			if err != nil {
				return
			}
			mid := len(ackPayload) / 2
			if mid < 1 {
				mid = 1
			}
			send := func(apduBytes []byte) {
				raw, _ := npdu.Append(nil, npdu.NPDU{Version: npdu.Version1, APDU: apduBytes})
				frame, _ := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionOriginalUnicastNPDU, Payload: raw})
				_ = peer.Send(ctx, virtual.OutboundPacket{Data: frame, Destination: local})
			}
			send(apdu.AppendComplexACK(nil, apdu.ComplexACK{
				SegmentedMessage:   true,
				MoreFollows:        true,
				InvokeID:           id,
				SequenceNumber:     0,
				ProposedWindowSize: 1,
				ServiceChoice:      apdu.ServiceReadProperty,
				Payload:            ackPayload[:mid],
			}))
			send(apdu.AppendComplexACK(nil, apdu.ComplexACK{
				SegmentedMessage:   true,
				MoreFollows:        false,
				InvokeID:           id,
				SequenceNumber:     1,
				ProposedWindowSize: 1,
				ServiceChoice:      apdu.ServiceReadProperty,
				Payload:            ackPayload[mid:],
			}))
			return
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	v, err := c.ReadProperty(ctx, Target{
		Address:  bacnet.LocalStation(bacnet.MustMAC([]byte{10, 0, 0, 2, 0xBA, 0xC0})),
		Endpoint: peerEP,
		MaxAPDU:  480,
	}, obj, prop)
	if err != nil {
		t.Fatal(err)
	}
	f, err := bacnet.AsReal(v)
	if err != nil || f != 3.5 {
		t.Fatalf("%v %v", f, err)
	}
}

func TestSegmentedRejectsWithoutTransaction(t *testing.T) {
	clk := clock.NewManual(time.Unix(0, 0).UTC())
	local := bip.NewEndpoint(netip.MustParseAddrPort("127.0.0.1:47808"))
	tr := virtual.New(local, clk, 16)
	c, err := New(WithTransport(AdaptVirtual(tr)), withClock(clk))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	seg := apdu.AppendComplexACK(nil, apdu.ComplexACK{
		SegmentedMessage:   true,
		MoreFollows:        true,
		InvokeID:           9,
		SequenceNumber:     0,
		ProposedWindowSize: 1,
		ServiceChoice:      apdu.ServiceReadProperty,
		Payload:            []byte{0x21, 0x01},
	})
	raw, _ := npdu.Append(nil, npdu.NPDU{Version: npdu.Version1, APDU: seg})
	frame, _ := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionOriginalUnicastNPDU, Payload: raw})
	tr.Inject(virtual.InboundPacket{
		Data:          frame,
		ImmediatePeer: bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.2:47808")),
	})
	time.Sleep(20 * time.Millisecond)
}

func TestNewSegmentReceiverDefaults(t *testing.T) {
	r := newSegmentReceiver(bacnet.DefaultDecodeLimits(), diag.Discard{}, nil, 0, defaultSegmentReceiveWindow)
	if r == nil {
		t.Fatal("nil receiver")
	}
	if _, ok := r.clock.(clock.Real); !ok {
		t.Fatalf("clock type %T", r.clock)
	}
	if r.segmentTimeout != 2*time.Second {
		t.Fatalf("segmentTimeout=%v", r.segmentTimeout)
	}
	if r.localWindow != 1 {
		t.Fatalf("localWindow=%d, want 1 (ACK every segment for peer compatibility)", r.localWindow)
	}
	if defaultSegmentSendWindow != 16 {
		t.Fatalf("defaultSegmentSendWindow=%d, want 16", defaultSegmentSendWindow)
	}
	cfg := defaultConfig()
	if cfg.segmentSendWindow != 16 || cfg.segmentReceiveWindow != 1 {
		t.Fatalf("default windows send=%d receive=%d, want 16/1", cfg.segmentSendWindow, cfg.segmentReceiveWindow)
	}
}

func TestSegmentedDuplicateSequenceACK(t *testing.T) {
	env := newVirtualPair(t, WithTransactionOptions(time.Second, 0, time.Second))
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	ackPayload, err := service.EncodeReadPropertyACK(service.ReadPropertyACK{
		Object: obj, Property: prop, Value: bacnet.RealValue(2.5),
	})
	if err != nil {
		t.Fatal(err)
	}
	mid := len(ackPayload) / 2
	if mid < 1 {
		mid = 1
	}

	errCh := make(chan error, 1)
	go func() {
		ctx := context.Background()
		_, err := env.Client.ReadProperty(ctx, env.Target, obj, prop)
		errCh <- err
	}()

	invokeID, _ := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	before := len(env.ClientTr.Outbox())
	injectSegmentedComplexACK(t, env, invokeID, 0, true, apdu.ServiceReadProperty, ackPayload[:mid])
	time.Sleep(10 * time.Millisecond)
	injectSegmentedComplexACK(t, env, invokeID, 0, true, apdu.ServiceReadProperty, ackPayload[:mid])
	time.Sleep(10 * time.Millisecond)

	gotPositive, _ := outboxSegmentACK(t, env.ClientTr, before, invokeID)
	if !gotPositive {
		t.Fatal("expected non-negative SegmentACK for duplicate sequence")
	}

	injectSegmentedComplexACK(t, env, invokeID, 1, false, apdu.ServiceReadProperty, ackPayload[mid:])

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for ReadProperty")
	}
}

func TestSegmentedWrongSequenceNegativeACK(t *testing.T) {
	env := newVirtualPair(t, WithTransactionOptions(300*time.Millisecond, 0, 200*time.Millisecond))
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 2}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}

	errCh := make(chan error, 1)
	go func() {
		ctx := context.Background()
		_, err := env.Client.ReadProperty(ctx, env.Target, obj, prop)
		errCh <- err
	}()

	invokeID, _ := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	before := len(env.ClientTr.Outbox())
	injectSegmentedComplexACK(t, env, invokeID, 0, true, apdu.ServiceReadProperty, []byte("a"))
	time.Sleep(10 * time.Millisecond)
	injectSegmentedComplexACK(t, env, invokeID, 3, true, apdu.ServiceReadProperty, []byte("b"))
	time.Sleep(10 * time.Millisecond)

	_, gotNegative := outboxSegmentACK(t, env.ClientTr, before, invokeID)
	if !gotNegative {
		t.Fatal("expected negative SegmentACK for out-of-window sequence")
	}

	env.Clk.Advance(400 * time.Millisecond)
	err := <-errCh
	var ab *bacnet.AbortError
	if !errors.As(err, &ab) || ab.Reason != abortReasonTSMTimeout {
		t.Fatalf("expected segment timeout abort, got %v", err)
	}
}

func TestSegmentedFirstSequenceNotZero(t *testing.T) {
	env := newVirtualPair(t, WithTransactionOptions(300*time.Millisecond, 0, time.Second))
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 3}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}

	errCh := make(chan error, 1)
	go func() {
		ctx := context.Background()
		_, err := env.Client.ReadProperty(ctx, env.Target, obj, prop)
		errCh <- err
	}()

	invokeID, _ := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	injectSegmentedComplexACK(t, env, invokeID, 1, true, apdu.ServiceReadProperty, []byte("x"))
	time.Sleep(20 * time.Millisecond)

	err := <-errCh
	if !errors.Is(err, bacnet.ErrProtocolViolation) {
		t.Fatalf("got %v", err)
	}
}

func TestSegmentedServiceMismatchAborts(t *testing.T) {
	env := newVirtualPair(t, WithTransactionOptions(300*time.Millisecond, 0, time.Second))
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 4}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}

	errCh := make(chan error, 1)
	go func() {
		ctx := context.Background()
		_, err := env.Client.ReadProperty(ctx, env.Target, obj, prop)
		errCh <- err
	}()

	invokeID, _ := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	// Service choice must match the transaction on every segment.
	injectSegmentedComplexACK(t, env, invokeID, 0, true, apdu.ServiceReadPropertyMultiple, []byte("part"))
	time.Sleep(20 * time.Millisecond)

	err := <-errCh
	if !errors.Is(err, bacnet.ErrProtocolViolation) {
		t.Fatalf("got %v", err)
	}
}

func TestSegmentedMaxSegmentsExceeded(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	limits.MaxSegments = 1
	env := newVirtualPair(t,
		WithDecodeLimits(limits),
		WithTransactionOptions(300*time.Millisecond, 0, time.Second),
	)
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 5}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}

	errCh := make(chan error, 1)
	go func() {
		ctx := context.Background()
		_, err := env.Client.ReadProperty(ctx, env.Target, obj, prop)
		errCh <- err
	}()

	invokeID, _ := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	injectSegmentedComplexACK(t, env, invokeID, 0, true, apdu.ServiceReadProperty, []byte("first"))
	time.Sleep(10 * time.Millisecond)
	injectSegmentedComplexACK(t, env, invokeID, 1, false, apdu.ServiceReadProperty, []byte("second"))
	time.Sleep(10 * time.Millisecond)

	err := <-errCh
	var ab *bacnet.AbortError
	if !errors.As(err, &ab) || ab.Reason != abortReasonOutOfResources {
		t.Fatalf("got %v", err)
	}
}

func TestSegmentedReassemblyTooLarge(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	limits.MaxReassembledAPDU = 8
	env := newVirtualPair(t,
		WithDecodeLimits(limits),
		WithTransactionOptions(300*time.Millisecond, 0, time.Second),
	)
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 6}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}

	errCh := make(chan error, 1)
	go func() {
		ctx := context.Background()
		_, err := env.Client.ReadProperty(ctx, env.Target, obj, prop)
		errCh <- err
	}()

	invokeID, _ := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	injectSegmentedComplexACK(t, env, invokeID, 0, true, apdu.ServiceReadProperty, []byte("123456"))
	time.Sleep(10 * time.Millisecond)
	injectSegmentedComplexACK(t, env, invokeID, 1, false, apdu.ServiceReadProperty, []byte("789"))
	time.Sleep(10 * time.Millisecond)

	err := <-errCh
	var ab *bacnet.AbortError
	if !errors.As(err, &ab) || ab.Reason != abortReasonAPDUTooLong {
		t.Fatalf("got %v", err)
	}
}

func TestSegmentedWrongSourceIgnored(t *testing.T) {
	env := newVirtualPair(t, WithTransactionOptions(300*time.Millisecond, 0, 200*time.Millisecond))
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 7}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}

	errCh := make(chan error, 1)
	go func() {
		ctx := context.Background()
		_, err := env.Client.ReadProperty(ctx, env.Target, obj, prop)
		errCh <- err
	}()

	invokeID, _ := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	wrongPeer := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.99:47808"))
	seg := apdu.AppendComplexACK(nil, apdu.ComplexACK{
		SegmentedMessage: true, MoreFollows: true, InvokeID: invokeID,
		SequenceNumber: 0, ProposedWindowSize: 1,
		ServiceChoice: apdu.ServiceReadProperty, Payload: []byte("x"),
	})
	injectUnicastNPDU(t, env.ClientTr, wrongPeer, env.Clk.Now(), seg)

	env.Clk.Advance(400 * time.Millisecond)
	err := <-errCh
	if !errors.Is(err, bacnet.ErrTimeout) {
		t.Fatalf("expected timeout, got %v", err)
	}
}

func TestSegmentACKIndicationNoOp(t *testing.T) {
	env := newVirtualPair(t)
	segACK := apdu.AppendSegmentACK(nil, apdu.SegmentACK{
		InvokeID: 1, SequenceNumber: 0, ActualWindowSize: 1,
	})
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), segACK)
	time.Sleep(10 * time.Millisecond)
}

func TestSegmentReceiverAbortAllClearsActive(t *testing.T) {
	env := newVirtualPair(t, WithTransactionOptions(300*time.Millisecond, 0, 200*time.Millisecond))
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 20}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}

	go func() {
		ctx := context.Background()
		_, _ = env.Client.ReadProperty(ctx, env.Target, obj, prop)
	}()

	invokeID, _ := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	injectSegmentedComplexACK(t, env, invokeID, 0, true, apdu.ServiceReadProperty, []byte("partial"))
	time.Sleep(10 * time.Millisecond)
	env.Client.seg.abortAll()
	time.Sleep(10 * time.Millisecond)
}

func TestReadPropertyMalformedACKPayload(t *testing.T) {
	env := newVirtualPair(t)
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogInput, Instance: 11}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}

	errCh := make(chan error, 1)
	go func() {
		_, err := env.Client.ReadProperty(context.Background(), env.Target, obj, prop)
		errCh <- err
	}()

	invokeID, _ := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendComplexACK(nil, apdu.ComplexACK{
		InvokeID: invokeID, ServiceChoice: apdu.ServiceReadProperty, Payload: []byte{0xFF},
	}))

	if <-errCh == nil {
		t.Fatal("expected decode error")
	}
}

func TestSegmentedComplexACKWrongServiceOnFirstSegment(t *testing.T) {
	env := newVirtualPair(t, WithTransactionOptions(300*time.Millisecond, 0, time.Second))
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 8}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}

	errCh := make(chan error, 1)
	go func() {
		ctx := context.Background()
		_, err := env.Client.ReadProperty(ctx, env.Target, obj, prop)
		errCh <- err
	}()

	invokeID, _ := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	injectSegmentedComplexACK(t, env, invokeID, 0, true, apdu.ServiceWriteProperty, []byte("x"))
	time.Sleep(20 * time.Millisecond)

	err := <-errCh
	if !errors.Is(err, bacnet.ErrProtocolViolation) {
		t.Fatalf("got %v", err)
	}
}
