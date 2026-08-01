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
)

// Abort reason codes used by the client receive path (ASHRAE 135).
const (
	abortReasonInvalidAPDU    uint8 = 2
	abortReasonOutOfResources uint8 = 9
	abortReasonTSMTimeout     uint8 = 10
	abortReasonAPDUTooLong    uint8 = 11
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
	invokeID     uint8
	service      uint8
	nextSeq      uint8
	window       uint8
	inWindow     uint8
	buf          []byte
	src          packetSource
	destAddr     bacnet.Address
	segmentCount int
	timer        clock.Timer
	done         chan struct{}
	c            *Client
}

func newSegmentReceiver(limits bacnet.DecodeLimits, d diag.Sink, clk clock.Clock, segmentTimeout time.Duration, localWindow uint8) *segmentReceiver {
	if segmentTimeout <= 0 {
		segmentTimeout = 2 * time.Second
	}
	if clk == nil {
		clk = clock.Real{}
	}
	if localWindow == 0 || localWindow > 127 {
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
	if tx.service != 0 && ack.ServiceChoice != 0 && tx.service != ack.ServiceChoice {
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
		window := r.localWindow
		if ack.ProposedWindowSize > 0 && ack.ProposedWindowSize < window {
			window = ack.ProposedWindowSize
		}
		if window == 0 {
			window = 1
		}
		st = &segState{
			invokeID: ack.InvokeID,
			service:  ack.ServiceChoice,
			nextSeq:  0,
			window:   window,
			src:      src,
			destAddr: tx.address,
			done:     make(chan struct{}),
			c:        c,
		}
		r.active[ack.InvokeID] = st
		st.timer = r.clock.NewTimer(r.segmentTimeout)
		c.wg.Add(1)
		go r.watchTimeout(st)
	} else if ack.ServiceChoice != 0 && st.service != ack.ServiceChoice {
		// Segment 0 establishes the service; later segments from common peers
		// repeat the same choice. A mismatch is a protocol violation.
		outbounds = append(outbounds, r.abortOutbound(st, abortReasonInvalidAPDU)...)
		r.removeLocked(ack.InvokeID)
		r.mu.Unlock()
		r.flush(outbounds)
		return apdu.PDU{}, false, bacnet.ErrProtocolViolation
	}

	if ack.SequenceNumber != st.nextSeq {
		if seqDuplicate(ack.SequenceNumber, st.nextSeq) {
			outbounds = append(outbounds, r.ackOutbound(st, false))
			r.mu.Unlock()
			r.flush(outbounds)
			return apdu.PDU{}, false, nil
		}
		outbounds = append(outbounds, r.ackOutbound(st, true))
		r.mu.Unlock()
		r.flush(outbounds)
		return apdu.PDU{}, false, nil
	}

	if st.segmentCount+1 > r.limits.MaxSegments {
		failErr := &bacnet.AbortError{InvokeID: ack.InvokeID, Server: false, Reason: abortReasonOutOfResources}
		outbounds = append(outbounds, r.abortOutbound(st, abortReasonOutOfResources)...)
		r.removeLocked(ack.InvokeID)
		r.mu.Unlock()
		r.flush(outbounds)
		return apdu.PDU{}, false, failErr
	}
	if len(st.buf)+len(ack.Payload) > r.limits.MaxReassembledAPDU {
		failErr := &bacnet.AbortError{InvokeID: ack.InvokeID, Server: false, Reason: abortReasonAPDUTooLong}
		outbounds = append(outbounds, r.abortOutbound(st, abortReasonAPDUTooLong)...)
		r.removeLocked(ack.InvokeID)
		r.mu.Unlock()
		r.flush(outbounds)
		return apdu.PDU{}, false, failErr
	}

	st.buf = append(st.buf, ack.Payload...)
	st.segmentCount++
	st.nextSeq++
	st.inWindow++
	st.src = src
	if st.timer != nil {
		st.timer.Reset(r.segmentTimeout)
	}

	atWindowEnd := st.inWindow >= st.window
	last := !ack.MoreFollows
	if atWindowEnd || last {
		outbounds = append(outbounds, r.ackOutbound(st, false))
		st.inWindow = 0
	}
	if !last {
		r.mu.Unlock()
		r.flush(outbounds)
		return apdu.PDU{}, false, nil
	}

	payload := append([]byte(nil), st.buf...)
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
	seq := st.nextSeq
	if seq > 0 {
		seq--
	}
	return segOutbound{
		destAddr: st.destAddr,
		peer:     st.src,
		c:        st.c,
		kind:     "SegmentACK",
		bytes: apdu.AppendSegmentACK(nil, apdu.SegmentACK{
			NegativeACK:      negative,
			Server:           false,
			InvokeID:         st.invokeID,
			SequenceNumber:   seq,
			ActualWindowSize: st.window,
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
		if err := a.c.sendAPDU(context.Background(), a.peer.immediate, false, a.destAddr, false, a.bytes); err != nil {
			r.diag.Report(diag.Event{
				Kind:    diag.KindUnexpectedAPDU,
				Message: a.kind + " send failed",
				Fields:  map[string]any{"error": err.Error()},
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
