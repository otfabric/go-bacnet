// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"sync"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/internal/clock"
	"github.com/otfabric/go-bacnet/internal/diag"
	"github.com/otfabric/go-bacnet/internal/segment"
)

// Abort reason codes used by the client receive path (ASHRAE 135).
const (
	abortReasonInvalidAPDU    = bacnet.AbortReasonInvalidAPDUInThisState
	abortReasonOutOfResources = bacnet.AbortReasonOutOfResources
	abortReasonTSMTimeout     = bacnet.AbortReasonTSMTimeout
	abortReasonAPDUTooLong    = bacnet.AbortReasonAPDUTooLong
)

type segOutbound struct {
	destAddr bacnet.Address
	peer     packetSource
	c        *Client
	bytes    []byte
	kind     string
}

// segmentReceiver reassembles segmented ComplexACK PDUs for pending transactions.
type segmentReceiver struct {
	mu             sync.Mutex
	limits         bacnet.DecodeLimits
	diag           diag.Sink
	segmentTimeout time.Duration
	clock          clock.Clock
	active         map[uint8]*segState
	localWindow    uint8
	send           *segmentSender
}

type segState struct {
	invokeID uint8
	service  uint8
	asm      *segment.Reassembly
	src      packetSource
	destAddr bacnet.Address
	timer    clock.Timer
	done     chan struct{}
	c        *Client
}

func newSegmentReceiver(limits bacnet.DecodeLimits, d diag.Sink, clk clock.Clock, segmentTimeout time.Duration, localWindow uint8) *segmentReceiver {
	if segmentTimeout <= 0 {
		segmentTimeout = 2 * time.Second
	}
	if clk == nil {
		clk = clock.Real{}
	}
	if !segment.ValidWindow(localWindow) {
		localWindow = defaultSegmentReceiveWindow
	}
	return &segmentReceiver{
		limits:         limits.Normalize(),
		diag:           d,
		segmentTimeout: segmentTimeout,
		clock:          clk,
		active:         make(map[uint8]*segState),
		localWindow:    localWindow,
		send:           newSegmentSender(),
	}
}

func (r *segmentReceiver) accept(ack *apdu.ComplexACK, src packetSource, c *Client) (full apdu.PDU, complete bool, err error) {
	tx := c.tx.lookup(ack.InvokeID)
	if tx == nil {
		c.diag.Report(diag.Event{Kind: diag.KindUnknownInvokeID, Message: "segmented ComplexACK without transaction"})
		return apdu.PDU{}, false, nil
	}
	if !c.tx.matchSource(tx, src) {
		c.diag.Report(diag.Event{Kind: diag.KindWrongSource, Message: "segmented ComplexACK source mismatch"})
		return apdu.PDU{}, false, nil
	}
	if tx.service != ack.ServiceChoice {
		c.diag.Report(diag.Event{Kind: diag.KindWrongService, Message: "segmented ComplexACK service mismatch"})
		return apdu.PDU{}, false, bacnet.ErrProtocolViolation
	}

	var outbounds []segOutbound
	r.mu.Lock()
	st, ok := r.active[ack.InvokeID]
	if !ok {
		if ack.SequenceNumber != 0 {
			r.mu.Unlock()
			r.diag.Report(diag.Event{Kind: diag.KindUnexpectedAPDU, Message: "first segment sequence not zero"})
			return apdu.PDU{}, false, bacnet.ErrProtocolViolation
		}
		if !c.tx.enterSegmented(ack.InvokeID) {
			r.mu.Unlock()
			return apdu.PDU{}, false, nil
		}
		window := segment.Window(r.localWindow, ack.ProposedWindowSize)
		st = &segState{
			invokeID: ack.InvokeID,
			service:  ack.ServiceChoice,
			asm:      segment.NewReassembly(window, r.limits.MaxSegments, r.limits.MaxReassembledAPDU),
			src:      src,
			destAddr: tx.address,
			done:     make(chan struct{}),
			c:        c,
		}
		r.active[ack.InvokeID] = st
		st.timer = r.clock.NewTimer(r.segmentTimeout)
		c.wg.Add(1)
		go r.watchTimeout(st)
	} else if st.service != ack.ServiceChoice {
		// Every segment carries service choice; a mismatch is a protocol violation.
		outbounds = append(outbounds, r.abortOutbound(st, abortReasonInvalidAPDU)...)
		r.removeLocked(ack.InvokeID)
		r.mu.Unlock()
		r.flush(outbounds)
		return apdu.PDU{}, false, bacnet.ErrProtocolViolation
	}

	outcome := st.asm.Add(ack.SequenceNumber, ack.MoreFollows, ack.Payload)
	switch outcome {
	case segment.OutOfOrder:
		// A segment already received is acknowledged again; any other is
		// answered with a negative SegmentACK naming the last in order.
		outbounds = append(outbounds, r.ackOutbound(st, !seqDuplicate(ack.SequenceNumber, st.asm.Expected())))
		r.mu.Unlock()
		r.flush(outbounds)
		return apdu.PDU{}, false, nil
	case segment.TooManySegments:
		failErr := &bacnet.AbortError{InvokeID: ack.InvokeID, Server: false, Reason: abortReasonOutOfResources}
		outbounds = append(outbounds, r.abortOutbound(st, abortReasonOutOfResources)...)
		r.removeLocked(ack.InvokeID)
		r.mu.Unlock()
		r.flush(outbounds)
		return apdu.PDU{}, false, failErr
	case segment.TooLong:
		failErr := &bacnet.AbortError{InvokeID: ack.InvokeID, Server: false, Reason: abortReasonAPDUTooLong}
		outbounds = append(outbounds, r.abortOutbound(st, abortReasonAPDUTooLong)...)
		r.removeLocked(ack.InvokeID)
		r.mu.Unlock()
		r.flush(outbounds)
		return apdu.PDU{}, false, failErr
	case segment.Stored, segment.StoredAck:
		st.src = src
		st.timer.Reset(r.segmentTimeout)
		if outcome == segment.StoredAck {
			outbounds = append(outbounds, r.ackOutbound(st, false))
		}
		r.mu.Unlock()
		r.flush(outbounds)
		return apdu.PDU{}, false, nil
	default: // segment.Complete
		st.src = src
		outbounds = append(outbounds, r.ackOutbound(st, false))
	}

	payload := append([]byte(nil), st.asm.Payload()...)
	service := st.service
	r.removeLocked(ack.InvokeID)
	r.mu.Unlock()
	r.flush(outbounds)
	return apdu.PDU{
		Type: apdu.TypeComplexACK,
		ComplexACK: &apdu.ComplexACK{
			InvokeID:      ack.InvokeID,
			ServiceChoice: service,
			Payload:       payload,
		},
	}, true, nil
}

func seqDuplicate(got, expected uint8) bool {
	delta := expected - got
	return delta != 0 && delta <= 128
}

func (r *segmentReceiver) ackOutbound(st *segState, negative bool) segOutbound {
	kind := "SegmentACK+"
	if negative {
		kind = "SegmentACK-"
	}
	return segOutbound{
		destAddr: st.destAddr,
		peer:     st.src,
		c:        st.c,
		kind:     kind,
		bytes: apdu.AppendSegmentACK(nil, apdu.SegmentACK{
			NegativeACK:      negative,
			Server:           false,
			InvokeID:         st.invokeID,
			SequenceNumber:   st.asm.Last(),
			ActualWindowSize: st.asm.Window(),
		}),
	}
}

func (r *segmentReceiver) abortOutbound(st *segState, reason uint8) []segOutbound {
	return []segOutbound{{
		destAddr: st.destAddr,
		peer:     st.src,
		c:        st.c,
		kind:     "Abort",
		bytes: apdu.AppendAbort(nil, apdu.AbortPDU{
			Server:   false,
			InvokeID: st.invokeID,
			Reason:   reason,
		}),
	}}
}

func (r *segmentReceiver) flush(actions []segOutbound) {
	for _, a := range actions {
		if a.c == nil {
			continue
		}
		timeout := a.c.cfg.apduTimeout
		if timeout <= 0 {
			timeout = r.segmentTimeout
		}
		ctx, cancel := clock.ContextWithTimeout(context.Background(), a.c.clock, timeout)
		err := a.c.sendAPDU(ctx, a.peer.immediate, false, a.destAddr, false, a.bytes)
		cancel()
		if err != nil {
			r.diag.Report(diag.Event{
				Kind:    diag.KindUnexpectedAPDU,
				Message: a.kind + " send failed",
				Fields:  map[string]any{"error": err.Error(), "control": a.kind},
			})
		}
	}
}

func (r *segmentReceiver) watchTimeout(st *segState) {
	defer st.c.wg.Done()
	select {
	case <-st.timer.C():
		r.mu.Lock()
		cur, ok := r.active[st.invokeID]
		if !ok || cur != st {
			r.mu.Unlock()
			return
		}
		err := &bacnet.AbortError{InvokeID: st.invokeID, Server: false, Reason: abortReasonTSMTimeout}
		out := r.abortOutbound(st, abortReasonTSMTimeout)
		c := st.c
		id := st.invokeID
		timeout := c.cfg.apduTimeout
		r.removeLocked(st.invokeID)
		r.mu.Unlock()
		r.flush(out)
		c.finishTx(id, txResult{err: err}, timeout)
	case <-st.done:
	}
}

func (r *segmentReceiver) remove(invokeID uint8) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.removeLocked(invokeID)
}

func (r *segmentReceiver) removeLocked(invokeID uint8) {
	st, ok := r.active[invokeID]
	if !ok {
		return
	}
	if st.timer != nil {
		st.timer.Stop()
	}
	select {
	case <-st.done:
	default:
		close(st.done)
	}
	delete(r.active, invokeID)
}

func (r *segmentReceiver) abortAll() {
	r.mu.Lock()
	for id := range r.active {
		r.removeLocked(id)
	}
	r.mu.Unlock()
	if r.send != nil {
		r.send.abortAll()
	}
}

func (r *segmentReceiver) handleSegmentACK(ack *apdu.SegmentACK, src packetSource) {
	if r.send != nil {
		r.send.deliver(ack, src)
	}
}
