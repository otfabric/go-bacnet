// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/server/object"
	"github.com/otfabric/go-bacnet/service"
)

// readAll asks for every property of the Device: a reply larger than a
// 50-octet Max APDU.
func readAll(t *testing.T) []byte {
	return mustEncode(t)(service.EncodeReadPropertyMultiple([]service.ReadAccessSpecification{{
		Object:     bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 100},
		Properties: []bacnet.PropertyReference{{Identifier: bacnet.PropertyAll}},
	}}))
}

func (h *harness) request(req apdu.ConfirmedRequest) {
	h.inject(apdu.AppendConfirmedRequest(nil, req))
}

func (h *harness) segmentACK(invokeID, seq, window uint8, nak bool) {
	h.inject(apdu.AppendSegmentACK(nil, apdu.SegmentACK{NegativeACK: nak, InvokeID: invokeID, SequenceNumber: seq, ActualWindowSize: window}))
}

func (h *harness) segment() *apdu.ComplexACK {
	h.t.Helper()
	got := h.expectType(apdu.TypeComplexACK)
	if !got.pdu.ComplexACK.SegmentedMessage {
		h.t.Fatalf("unsegmented reply %+v", got.pdu.ComplexACK)
	}
	return got.pdu.ComplexACK
}

func TestSegmentedResponse(t *testing.T) {
	h := newHarness(t)
	h.request(apdu.ConfirmedRequest{SegmentedResponseAccepted: true, MaxSegments: 0, MaxAPDU: 0, InvokeID: 1, ServiceChoice: apdu.ServiceReadPropertyMultiple, Payload: readAll(t)})
	var payload []byte
	first := h.segment()
	if first.SequenceNumber != 0 || !first.MoreFollows {
		t.Fatalf("first %+v", first)
	}
	payload = append(payload, first.Payload...)
	h.segmentACK(1, 0, 3, false)
	// A window of three; the requester NAKs after the first of them.
	w := []*apdu.ComplexACK{h.segment(), h.segment(), h.segment()}
	if w[0].SequenceNumber != 1 || w[2].SequenceNumber != 3 {
		t.Fatalf("window %d..%d", w[0].SequenceNumber, w[2].SequenceNumber)
	}
	payload = append(payload, w[0].Payload...)
	// A duplicate acknowledgement of segment 0 is ignored.
	h.segmentACK(1, 0, 3, false)
	h.segmentACK(1, 1, 3, true)
	seq := uint8(2)
	for {
		s := h.segment()
		if s.SequenceNumber != seq {
			t.Fatalf("got segment %d, want %d", s.SequenceNumber, seq)
		}
		payload = append(payload, s.Payload...)
		if !s.MoreFollows {
			h.segmentACK(1, seq, 3, false)
			break
		}
		if (seq-1)%3 == 0 {
			h.segmentACK(1, seq, 3, false)
		}
		seq++
	}
	h.quiet(30 * time.Millisecond)
	ack, err := service.DecodeReadPropertyMultipleACK(payload, bacnet.DefaultDecodeLimits())
	if err != nil || len(ack) != 1 || len(ack[0].Properties) < 20 {
		t.Fatalf("reassembled reply: %v %+v", err, ack)
	}
}

func TestSegmentedResponseTimeoutAndAbort(t *testing.T) {
	h := newHarness(t)
	big := apdu.ConfirmedRequest{SegmentedResponseAccepted: true, MaxAPDU: 0, InvokeID: 2, ServiceChoice: apdu.ServiceReadPropertyMultiple, Payload: readAll(t)}
	h.request(big)
	h.segment()
	// One retry (harness), then Abort TSM timeout.
	if s := h.segment(); s.SequenceNumber != 0 {
		t.Fatalf("resent %d", s.SequenceNumber)
	}
	h.expectAbort(bacnet.AbortReasonTSMTimeout)

	big.InvokeID = 3
	h.request(big)
	h.segment()
	h.inject(apdu.AppendAbort(nil, apdu.AbortPDU{InvokeID: 3, Reason: bacnet.AbortReasonOther}))
	h.quiet(300 * time.Millisecond)
	waitUntil(t, func() bool { return h.diagnosed("transaction") })

	// The requester does not accept a segmented reply.
	big.InvokeID, big.SegmentedResponseAccepted = 4, false
	h.request(big)
	h.expectAbort(bacnet.AbortReasonSegmentationNotSupported)
	// The reply needs more segments than the requester accepts.
	big.InvokeID, big.SegmentedResponseAccepted, big.MaxSegments = 5, true, 1
	h.request(big)
	h.expectAbort(bacnet.AbortReasonBufferOverflow)
	// A SegmentACK from a server, or for an unknown transaction, is ignored.
	h.inject(apdu.AppendSegmentACK(nil, apdu.SegmentACK{Server: true, InvokeID: 5}))
	h.segmentACK(77, 0, 1, false)
	h.quiet(20 * time.Millisecond)
}

func TestSegmentationDisabled(t *testing.T) {
	h := newHarness(t, WithSegmentation(0, 0))
	h.request(apdu.ConfirmedRequest{SegmentedResponseAccepted: true, MaxAPDU: 0, InvokeID: 1, ServiceChoice: apdu.ServiceReadPropertyMultiple, Payload: readAll(t)})
	h.expectAbort(bacnet.AbortReasonSegmentationNotSupported)
	h.request(apdu.ConfirmedRequest{SegmentedMessage: true, MoreFollows: true, ProposedWindowSize: 1, MaxAPDU: 5, InvokeID: 2, ServiceChoice: apdu.ServiceReadProperty, Payload: []byte{0x0C}})
	h.expectAbort(bacnet.AbortReasonSegmentationNotSupported)
}

func TestSegmentedRequest(t *testing.T) {
	h := newHarness(t)
	p := readPV(t)
	seg := func(inv, seq uint8, part []byte, more bool) {
		h.request(apdu.ConfirmedRequest{SegmentedMessage: true, MoreFollows: more, SegmentedResponseAccepted: true, MaxSegments: 7, MaxAPDU: 5,
			InvokeID: inv, SequenceNumber: seq, ProposedWindowSize: 2, ServiceChoice: apdu.ServiceReadProperty, Payload: part})
	}
	expectSegACK := func(seq uint8, nak bool) {
		t.Helper()
		got := h.expectType(apdu.TypeSegmentACK)
		if a := got.pdu.SegmentACK; a.SequenceNumber != seq || a.NegativeACK != nak || !a.Server || a.ActualWindowSize != 2 {
			t.Fatalf("segment ack %+v, want %d nak %v", a, seq, nak)
		}
	}
	// Segment 0 is acknowledged at once (clause 5.4.5, IDLE): a requester
	// sends nothing more until it has the actual window. The window of two
	// then counts from segment 0, and the last segment is acknowledged.
	seg(1, 0, p[:2], true)
	expectSegACK(0, false)
	seg(1, 1, p[2:4], true)
	h.quiet(20 * time.Millisecond)
	seg(1, 2, p[4:], false)
	expectSegACK(2, false)
	h.expectType(apdu.TypeComplexACK)

	// Out of order: NAK naming the last segment received in order; a repeat
	// of that one is acknowledged again.
	seg(2, 0, p[:2], true)
	expectSegACK(0, false)
	seg(2, 2, p[4:], false)
	expectSegACK(0, true)
	seg(2, 0, p[:2], true)
	expectSegACK(0, false)
	// The requester aborts the reassembly; its next segment is unknown.
	h.inject(apdu.AppendAbort(nil, apdu.AbortPDU{InvokeID: 2, Reason: bacnet.AbortReasonOther}))
	seg(2, 1, p[2:4], true)
	h.expectAbort(bacnet.AbortReasonInvalidAPDUInThisState)

	// A reassembly whose segment timer expired is dropped.
	seg(3, 0, p[:2], true)
	expectSegACK(0, false)
	h.s.resp.expire(time.Now().Add(time.Minute))
	seg(3, 1, p[2:4], true)
	h.expectAbort(bacnet.AbortReasonInvalidAPDUInThisState)
}

func TestSegmentedRequestOverflow(t *testing.T) {
	h := newHarness(t, WithSegmentation(2, 1))
	for seq := uint8(0); seq < 3; seq++ {
		h.request(apdu.ConfirmedRequest{SegmentedMessage: true, MoreFollows: true, MaxAPDU: 5, InvokeID: 9, SequenceNumber: seq, ProposedWindowSize: 4, ServiceChoice: apdu.ServiceReadProperty, Payload: []byte{0x0C}})
		if seq < 2 {
			got := h.expectType(apdu.TypeSegmentACK)
			if got.pdu.SegmentACK.ActualWindowSize != 1 {
				t.Fatalf("window %d not capped", got.pdu.SegmentACK.ActualWindowSize)
			}
		}
	}
	h.expectAbort(bacnet.AbortReasonBufferOverflow)
}

// After the final reply the transaction is released (clause 5.4.5): the
// same requester and invoke ID with the same content is a new request and is
// executed again, as when a bacnet-stack command-line tool is run twice.
func TestRepeatAfterCompletionIsNewRequest(t *testing.T) {
	var writes atomic.Int32
	h := newHarness(t, WithHooks(Hooks{BeforeWrite: func(context.Context, bacnet.ObjectIdentifier, object.Write) error {
		writes.Add(1)
		return nil
	}}))
	h.confirmed(1, apdu.ServiceReadProperty, readPV(t))
	first := h.expectType(apdu.TypeComplexACK)
	write := func(invokeID uint8, v float32) {
		wp := mustEncode(t)(service.EncodeWriteProperty(service.WritePropertyRequest{Object: av1, Property: pvRef, Value: bacnet.RealValue(v)}))
		h.confirmed(invokeID, apdu.ServiceWriteProperty, wp)
		h.expectType(apdu.TypeSimpleACK)
	}
	write(2, 42)
	h.confirmed(1, apdu.ServiceReadProperty, readPV(t))
	again := h.expectType(apdu.TypeComplexACK)
	if string(first.npdu.APDU) == string(again.npdu.APDU) {
		t.Fatal("repeat after completion answered with the earlier reply")
	}
	// An identical write with the same invoke ID is executed each time.
	write(3, 7)
	write(3, 7)
	if n := writes.Load(); n != 3 {
		t.Fatalf("%d writes executed, want 3", n)
	}
	if h.diagnosed("transaction") {
		t.Fatal("a request after completion was treated as a repeat")
	}
	h.s.resp.mu.Lock()
	open := len(h.s.resp.tx)
	h.s.resp.mu.Unlock()
	if open != 0 {
		t.Fatalf("%d transactions open after their replies", open)
	}
}

// A repeat while the request is executing is within the open transaction
// and is dropped.
func TestRepeatWhileExecutingDropped(t *testing.T) {
	release := make(chan struct{})
	var writes atomic.Int32
	h := newHarness(t, WithWorkers(1, 0), WithHooks(Hooks{BeforeWrite: func(ctx context.Context, _ bacnet.ObjectIdentifier, _ object.Write) error {
		writes.Add(1)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil
	}}))
	// With one worker and no queue, a second request finds the server busy.
	wp := mustEncode(t)(service.EncodeWriteProperty(service.WritePropertyRequest{Object: av1, Property: pvRef, Value: bacnet.RealValue(5)}))
	// With no queue, a request that arrives before the worker waits for
	// work is refused; send again until the worker has taken it.
	for attempt := 0; writes.Load() == 0; attempt++ {
		if attempt == 50 {
			t.Fatal("the worker never took the write")
		}
		h.confirmed(2, apdu.ServiceWriteProperty, wp)
		deadline := time.Now().Add(100 * time.Millisecond)
		for writes.Load() == 0 && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if writes.Load() == 0 {
			h.expectAbort(bacnet.AbortReasonOutOfResources)
		}
	}
	h.confirmed(2, apdu.ServiceWriteProperty, wp)
	waitUntil(t, func() bool { return h.diagnosed("transaction") })
	h.confirmed(3, apdu.ServiceReadProperty, readPV(t))
	h.expectAbort(bacnet.AbortReasonOutOfResources)
	waitUntil(t, func() bool { return h.diagnosed("queue_drop") })
	close(release)
	// One reply: the dropped repeat is not executed and not answered.
	h.expectType(apdu.TypeSimpleACK)
	h.quiet(30 * time.Millisecond)
	if n := writes.Load(); n != 1 {
		t.Fatalf("%d writes executed, want 1", n)
	}
}

// BACpypes3 0.0.110 sends segments with Max APDU octets of service data
// after the 6-octet header: 1482 octets to a 1476-octet device. The default
// limits serve them; a configured MaxAPDUSize drops them.
func TestSegmentLongerThanMaxAPDU(t *testing.T) {
	seg0 := append([]byte{0x0e, 0x44, 0x01, 0x00, 0x02, apdu.ServiceAtomicWriteFile}, make([]byte, 1476)...)
	h := newHarness(t)
	h.inject(seg0)
	if got := h.expectType(apdu.TypeSegmentACK).pdu.SegmentACK; got.SequenceNumber != 0 || got.ActualWindowSize != 2 {
		t.Fatalf("segment ack %+v", got)
	}

	strict := bacnet.DefaultDecodeLimits()
	h = newHarness(t, WithDecodeLimits(strict))
	h.inject(seg0)
	h.quiet(50 * time.Millisecond)
}
