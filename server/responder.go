// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/internal/clock"
	"github.com/otfabric/go-bacnet/internal/diag"
	"github.com/otfabric/go-bacnet/internal/segment"
)

// responder is the responding side of confirmed transactions (clause 5.4,
// responding BACnet-user): segmented-request reassembly, segmented
// ComplexACK transmission and repeated requests.
//
// A request that arrives again with the same invoke ID, from the same
// requester and with identical content, while the first is still being
// executed, is dropped. Once the final reply is sent the transaction state
// machine returns to IDLE and is released (clause 5.4.5): a later request
// with the same requester and invoke ID is a new request and is executed.
// Requesters that start every run with the same invoke ID from the same
// address (the bacnet-stack command-line tools) depend on that; bacnet-stack
// keeps no cache of completed replies either.
//
// Segmented requests being reassembled and segmented replies being sent
// are bounded in number, per origin and (for reassembly) in buffered
// octets (SegmentLimits). A segmented reply is sent by its own goroutine,
// so a requester that acknowledges slowly or never does not hold a worker,
// and it ends at an overall deadline that acknowledgements do not extend.
type responder struct {
	s  *Server
	mu sync.Mutex
	tx map[string]*rtx

	// Reassembly accounting.
	open     int
	octets   int
	openFrom map[string]int
	// Segmented replies in progress.
	replies     int
	repliesFrom map[string]int
}

type rtx struct {
	key      string
	peer     peer
	invokeID uint8
	service  uint8
	content  string
	// Segmented request reassembly.
	segmented bool
	asm       *segment.Reassembly
	deadline  time.Time
	header    apdu.ConfirmedRequest
	// Segmented reply: SegmentACKs for it are delivered here; aborted is
	// closed when the requester aborts it.
	acks      chan *apdu.SegmentACK
	aborted   chan struct{}
	abortOnce sync.Once
	// counted: the reassembly holds a share of the reassembly limits, with
	// buffered octets; replying: a segmented reply holds a share of the
	// reply limits.
	counted  bool
	buffered int
	replying bool
}

func newResponder(s *Server) *responder {
	return &responder{s: s, tx: make(map[string]*rtx), openFrom: make(map[string]int), repliesFrom: make(map[string]int)}
}

func (r *responder) close() {
	r.mu.Lock()
	r.tx = make(map[string]*rtx)
	r.open, r.octets, r.openFrom = 0, 0, make(map[string]int)
	r.replies, r.repliesFrom = 0, make(map[string]int)
	r.mu.Unlock()
}

func originKey(p peer) string { return p.origin.String() }

// admitLocked takes a reassembly share for a request from p; it reports
// false when the limits are reached. The caller holds r.mu.
func (r *responder) admitLocked(p peer) bool {
	l := r.s.cfg.segLimits
	o := originKey(p)
	if r.open >= l.Reassemblies || r.openFrom[o] >= l.ReassembliesPerOrigin || r.octets >= l.ReassemblyOctets {
		return false
	}
	r.open++
	r.openFrom[o]++
	return true
}

// releaseLocked gives back the reassembly share of t. The caller holds
// r.mu.
func (r *responder) releaseLocked(t *rtx) {
	if !t.counted {
		return
	}
	t.counted = false
	r.open--
	r.octets -= t.buffered
	t.buffered = 0
	o := originKey(t.peer)
	if r.openFrom[o]--; r.openFrom[o] <= 0 {
		delete(r.openFrom, o)
	}
}

// admitReply takes a segmented-reply share for t; it reports false when
// the limits are reached.
func (r *responder) admitReply(t *rtx) bool {
	l := r.s.cfg.segLimits
	o := originKey(t.peer)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.replies >= l.Replies || r.repliesFrom[o] >= l.RepliesPerOrigin {
		return false
	}
	r.replies++
	r.repliesFrom[o]++
	t.replying = true
	return true
}

// done ends transaction t: it gives back its shares and releases its key.
func (r *responder) done(t *rtx) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.releaseLocked(t)
	if t.replying {
		t.replying = false
		r.replies--
		o := originKey(t.peer)
		if r.repliesFrom[o]--; r.repliesFrom[o] <= 0 {
			delete(r.repliesFrom, o)
		}
	}
	if r.tx[t.key] == t {
		delete(r.tx, t.key)
	}
}

// expire drops stale reassemblies.
func (r *responder) expire(now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, t := range r.tx {
		if t.segmented && now.After(t.deadline) {
			// clause 5.4.5: the segment timer expired while waiting for a
			// segment; the transaction ends without an answer.
			r.releaseLocked(t)
			delete(r.tx, k)
		}
	}
}

// confirmed handles a Confirmed-Request PDU on the receive loop.
func (r *responder) confirmed(req *apdu.ConfirmedRequest, p peer) {
	if p.broadcast {
		// A confirmed service is never addressed to a broadcast.
		return
	}
	key := p.key(req.InvokeID)
	if req.SegmentedMessage {
		r.segment(req, p, key)
		return
	}
	content := string(append([]byte{req.ServiceChoice}, req.Payload...))
	r.mu.Lock()
	if t, busy := r.tx[key]; busy {
		r.mu.Unlock()
		if t.content == content || t.segmented {
			r.s.report(diag.KindTransaction, "repeated request dropped while executing", map[string]any{"invoke_id": req.InvokeID, "from": p.origin.String()})
		}
		return
	}
	t := &rtx{key: key, peer: p, invokeID: req.InvokeID, service: req.ServiceChoice, content: content, header: *req}
	t.header.Payload = nil // execute is given the payload; the datagram is not kept
	r.tx[key] = t
	r.mu.Unlock()
	r.execute(t, req.Payload)
}

// segment handles one segment of a segmented confirmed request.
func (r *responder) segment(req *apdu.ConfirmedRequest, p peer, key string) {
	s := r.s
	if !s.cfg.segmentation {
		s.sendAbort(p, req.InvokeID, bacnet.AbortReasonSegmentationNotSupported)
		return
	}
	now := s.clock.Now()
	r.mu.Lock()
	t, ok := r.tx[key]
	if !ok {
		if req.SequenceNumber != 0 {
			r.mu.Unlock()
			// A segment of a transaction that is not known (or already
			// done): clause 5.4.5 answers it with an Abort.
			s.sendAbort(p, req.InvokeID, bacnet.AbortReasonInvalidAPDUInThisState)
			return
		}
		if !r.admitLocked(p) {
			r.mu.Unlock()
			s.report(diag.KindTransaction, "segmented request refused: reassembly limits reached", map[string]any{"invoke_id": req.InvokeID, "from": p.origin.String()})
			s.sendAbort(p, req.InvokeID, bacnet.AbortReasonOutOfResources)
			return
		}
		asm := segment.NewReassembly(segment.Window(s.cfg.window, req.ProposedWindowSize), s.cfg.maxSegments, s.limits.MaxReassembledAPDU)
		t = &rtx{key: key, peer: p, invokeID: req.InvokeID, service: req.ServiceChoice, segmented: true, asm: asm, header: *req, counted: true}
		t.header.Payload = nil // the reassembly holds the segments; the datagram is not kept
		r.tx[key] = t
	}
	if !t.segmented || t.acks != nil {
		r.mu.Unlock()
		return
	}
	outcome := t.asm.Add(req.SequenceNumber, req.MoreFollows, req.Payload)
	switch outcome {
	case segment.OutOfOrder:
		// Out of order: a negative SegmentACK names the last segment received
		// in order; a repeat of that one re-acknowledges it.
		last := t.asm.Last()
		nak := req.SequenceNumber != last
		window := t.asm.Window()
		r.mu.Unlock()
		r.segmentACK(p, req.InvokeID, last, window, nak)
		return
	case segment.TooManySegments, segment.TooLong:
		r.releaseLocked(t)
		delete(r.tx, key)
		r.mu.Unlock()
		s.sendAbort(p, req.InvokeID, bacnet.AbortReasonBufferOverflow)
		return
	case segment.Stored, segment.StoredAck, segment.Complete:
	}
	if n := len(t.asm.Payload()); t.counted {
		r.octets += n - t.buffered
		t.buffered = n
		if r.octets > s.cfg.segLimits.ReassemblyOctets {
			r.releaseLocked(t)
			delete(r.tx, key)
			r.mu.Unlock()
			s.report(diag.KindTransaction, "segmented request refused: reassembly octet limit reached", map[string]any{"invoke_id": req.InvokeID, "from": p.origin.String()})
			s.sendAbort(p, req.InvokeID, bacnet.AbortReasonOutOfResources)
			return
		}
	}
	t.deadline = now.Add(2 * s.cfg.segmentTimeout)
	ack := outcome != segment.Stored
	last := outcome == segment.Complete
	payload := t.asm.Payload()
	window := t.asm.Window()
	if last {
		t.segmented = false
		r.releaseLocked(t) // the payload now belongs to the request being executed
		t.content = string(append([]byte{t.service}, payload...))
		t.header.MaxSegments = req.MaxSegments
		t.header.MaxAPDU = req.MaxAPDU
		t.header.SegmentedResponseAccepted = req.SegmentedResponseAccepted
	}
	r.mu.Unlock()
	if ack {
		r.segmentACK(p, req.InvokeID, req.SequenceNumber, window, false)
	}
	if last {
		r.execute(t, payload)
	}
}

func (r *responder) segmentACK(p peer, invokeID, seq, window uint8, nak bool) {
	_ = r.s.reply(p, apdu.AppendSegmentACK(nil, apdu.SegmentACK{
		NegativeACK: nak, Server: true, InvokeID: invokeID, SequenceNumber: seq, ActualWindowSize: window,
	}))
}

// execute hands a complete request to a worker.
func (r *responder) execute(t *rtx, payload []byte) {
	s := r.s
	req := t.header
	req.Payload = payload
	ok := s.submit(func(ctx context.Context) {
		res := s.handleConfirmed(ctx, &req, t.peer)
		r.finish(ctx, t, &req, res)
	})
	if !ok {
		r.done(t)
		s.report(diag.KindQueueDrop, "confirmed request refused: workers busy", map[string]any{"service": req.ServiceChoice, "from": t.peer.origin.String()})
		s.sendAbort(t.peer, req.InvokeID, bacnet.AbortReasonOutOfResources)
	}
}

// finish sends the reply of a completed request, segmenting a ComplexACK
// that does not fit the requester's Max APDU. A segmented reply is handed
// to its own goroutine, which then ends the transaction.
func (r *responder) finish(ctx context.Context, t *rtx, req *apdu.ConfirmedRequest, res result) {
	s := r.s
	handedOff := false
	defer func() {
		if !handedOff {
			r.done(t)
		}
	}()
	if res.silent {
		return
	}
	reply, payload, complex := res.encode(req.InvokeID, req.ServiceChoice)
	remoteMax, _ := apdu.DecodeMaxAPDUSize(apdu.MaxAPDUCode(req.MaxAPDU))
	limit := int(min(remoteMax, s.cfg.maxAPDU))
	if !complex || len(reply) <= limit {
		if err := s.reply(t.peer, reply); err == nil && res.after != nil {
			res.after()
		}
		return
	}
	// The reply needs segmentation (clause 5.2, 5.4.5).
	if !req.SegmentedResponseAccepted || !s.cfg.segmentation {
		s.sendAbort(t.peer, req.InvokeID, bacnet.AbortReasonSegmentationNotSupported)
		return
	}
	segSize := limit - 5 // segmented ComplexACK header
	count := segment.Count(len(payload), segSize)
	maxSegs := decodeMaxSegments(req.MaxSegments)
	if (maxSegs > 0 && count > maxSegs) || count > s.cfg.maxSegments || count > segment.MaxSegments {
		s.sendAbort(t.peer, req.InvokeID, bacnet.AbortReasonBufferOverflow)
		return
	}
	if ctx.Err() != nil {
		return
	}
	if !r.admitReply(t) {
		s.report(diag.KindTransaction, "segmented reply refused: reply limits reached", map[string]any{"invoke_id": req.InvokeID, "from": t.peer.origin.String()})
		s.sendAbort(t.peer, req.InvokeID, bacnet.AbortReasonOutOfResources)
		return
	}
	acks := make(chan *apdu.SegmentACK, 16)
	r.mu.Lock()
	t.acks = acks
	t.aborted = make(chan struct{})
	r.mu.Unlock()
	// The worker is free again; the server's WaitGroup still counts this
	// worker, so the Add cannot race with Close's Wait.
	handedOff = true
	hdr := *req
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer r.done(t)
		// Every window waits at most one segment timeout, plus the retries:
		// progress does not extend this deadline.
		limit := time.Duration(count+s.cfg.retries+1) * s.cfg.segmentTimeout
		sctx, cancel := clock.ContextWithTimeout(ctx, s.clock, limit)
		defer cancel()
		if err := r.sendSegments(sctx, t, &hdr, payload, segSize, count, acks); err != nil {
			if ctx.Err() == nil && sctx.Err() != nil {
				s.sendAbort(t.peer, hdr.InvokeID, bacnet.AbortReasonTSMTimeout)
			}
			s.report(diag.KindTransaction, "segmented reply ended", map[string]any{"invoke_id": hdr.InvokeID, "error": err.Error()})
		}
	}()
}

func decodeMaxSegments(code uint8) int {
	switch code {
	case 1:
		return 2
	case 2:
		return 4
	case 3:
		return 8
	case 4:
		return 16
	case 5:
		return 32
	case 6:
		return 64
	default:
		// 0 unspecified, 7 more than 64.
		return 0
	}
}

var (
	errSegmentTimeout = errors.New("server: segment timeout")
	errAborted        = errors.New("server: aborted by the requester")
)

// sendSegments transmits a segmented ComplexACK: windows of segments, each
// acknowledged by a SegmentACK; a negative ACK or a timeout resends from
// the segment after the last acknowledged one. The first segment goes out
// alone, because the requester's window is known from its first SegmentACK.
func (r *responder) sendSegments(ctx context.Context, t *rtx, req *apdu.ConfirmedRequest, payload []byte, segSize, count int, acks <-chan *apdu.SegmentACK) error {
	s := r.s
	encode := func(i int) []byte {
		start, end := segment.Bounds(i, segSize, len(payload))
		return apdu.AppendComplexACK(nil, apdu.ComplexACK{
			SegmentedMessage: true, MoreFollows: i < count-1, InvokeID: req.InvokeID,
			SequenceNumber: uint8(i), ProposedWindowSize: s.cfg.window, ServiceChoice: req.ServiceChoice,
			Payload: payload[start:end],
		})
	}
	window := 1
	acked := -1 // highest segment acknowledged
	retries := 0
	for acked < count-1 {
		end := min(acked+1+window, count)
		for i := acked + 1; i < end; i++ {
			if err := s.reply(t.peer, encode(i)); err != nil {
				return err
			}
		}
		timer := s.clock.NewTimer(s.cfg.segmentTimeout)
		progressed := false
		for !progressed {
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-t.aborted:
				timer.Stop()
				return errAborted
			case <-timer.C():
				retries++
				if retries > s.cfg.retries {
					s.sendAbort(t.peer, req.InvokeID, bacnet.AbortReasonTSMTimeout)
					return errSegmentTimeout
				}
				progressed = true // resend the window
			case ack := <-acks:
				if segment.ValidWindow(ack.ActualWindowSize) {
					window = int(ack.ActualWindowSize)
				}
				// Sequence numbers are modulo 256; segments in flight are
				// always within one window above acked.
				seq := int(ack.SequenceNumber)
				base := acked + 1
				off := (seq - base%256 + 256) % 256
				n := base + off
				if n >= end {
					// Not inside the window just sent: a duplicate of an
					// earlier acknowledgement.
					continue
				}
				// A positive ACK acknowledges every segment up to n; a
				// negative one says n is the last received in order, so the
				// window restarts after it.
				if n > acked {
					acked = n
					retries = 0
				} else if !ack.NegativeACK {
					continue
				}
				timer.Stop()
				progressed = true
			}
		}
	}
	// The last segment is acknowledged; the transaction is complete.
	return nil
}

// segmentACK delivers a SegmentACK from a requester to the reply it
// acknowledges.
func (r *responder) deliverSegmentACK(ack *apdu.SegmentACK, p peer) {
	if ack.Server {
		return
	}
	r.mu.Lock()
	t, ok := r.tx[p.key(ack.InvokeID)]
	var ch chan *apdu.SegmentACK
	if ok {
		ch = t.acks
	}
	r.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- ack:
	default:
	}
}

// abortFromClient ends a transaction the requester aborted.
func (r *responder) abortFromClient(invokeID uint8, p peer) {
	r.mu.Lock()
	t, ok := r.tx[p.key(invokeID)]
	var aborted chan struct{}
	if ok {
		aborted = t.aborted
		if aborted == nil && t.segmented {
			// Reassembly in progress: drop it. A request already being
			// executed finishes; its reply then goes unacknowledged.
			r.releaseLocked(t)
			delete(r.tx, t.key)
		}
	}
	r.mu.Unlock()
	if aborted != nil {
		t.abortOnce.Do(func() { close(aborted) })
	}
}
