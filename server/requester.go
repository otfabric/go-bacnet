// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"errors"
	"sync"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/internal/clock"
	"github.com/otfabric/go-bacnet/internal/diag"
)

// requester sends the server's own confirmed requests (Confirmed COV and
// Event notifications) and matches their answers (clause 5.4.4). Requests
// are never segmented; a notification that does not fit is not sent.
//
// The 256 invoke IDs are one pool for all destinations. One destination
// holds at most RecipientLimits.PendingPerDestination of them at a time
// (WithRecipientLimits), so that a
// destination that does not answer leaves the rest of the pool to the
// others; what happens to a request beyond that is the caller's choice
// (invoke).
type requester struct {
	s       *Server
	mu      sync.Mutex
	next    uint8
	pending map[uint8]*pendingReq
	dests   map[string]*destination
}

// maxPendingPerDestination is the default of
// RecipientLimits.PendingPerDestination: the confirmed requests of the
// server that wait for an answer from one destination.
const maxPendingPerDestination = 8

// destination is the share of one destination in the requester: sem holds
// one token per request in flight, users counts the invocations that hold
// or wait for a token (the entry is removed with the last one).
type destination struct {
	sem   chan struct{}
	users int
}

type pendingReq struct {
	to  bip.Endpoint
	svc uint8
	ch  chan apdu.PDU
}

var (
	errNoInvokeID      = errors.New("server: no free invoke ID")
	errDestinationBusy = errors.New("server: too many requests pending for the destination")
)

func newRequester(s *Server) *requester {
	return &requester{s: s, pending: make(map[uint8]*pendingReq), dests: make(map[string]*destination)}
}

// acquire takes one of the destination's places. With wait it blocks until
// one is free or ctx ends; without, a destination with no free place is
// errDestinationBusy. The returned function gives the place back.
func (r *requester) acquire(ctx context.Context, to bip.Endpoint, dest bacnet.Address, wait bool) (release func(), err error) {
	key := to.String() + "|" + dest.String()
	r.mu.Lock()
	d := r.dests[key]
	if d == nil {
		d = &destination{sem: make(chan struct{}, r.s.cfg.recipLimits.PendingPerDestination)}
		r.dests[key] = d
	}
	d.users++
	r.mu.Unlock()
	leave := func() {
		r.mu.Lock()
		if d.users--; d.users == 0 {
			delete(r.dests, key)
		}
		r.mu.Unlock()
	}
	select {
	case d.sem <- struct{}{}:
	default:
		if !wait {
			leave()
			return nil, errDestinationBusy
		}
		select {
		case d.sem <- struct{}{}:
		case <-ctx.Done():
			leave()
			return nil, ctx.Err()
		}
	}
	return func() {
		<-d.sem
		leave()
	}, nil
}

// invoke sends a confirmed request and waits for its answer, retrying on
// timeout. A SimpleACK returns nil; Error, Reject and Abort are returned
// as the client package's error types.
//
// When the destination already has RecipientLimits.PendingPerDestination
// requests waiting for an answer, invoke waits for one of them to end (wait true:
// the caller is one of a bounded number, a COV subscription) or returns
// errDestinationBusy at once (wait false: event notifications, whose
// number the peers drive).
func (r *requester) invoke(ctx context.Context, to bip.Endpoint, dest bacnet.Address, svc uint8, payload []byte, wait bool) error {
	s := r.s
	release, err := r.acquire(ctx, to, dest, wait)
	if err != nil {
		return err
	}
	defer release()
	r.mu.Lock()
	var id uint8
	found := false
	for i := 0; i < 256; i++ {
		cand := r.next
		r.next++
		if _, used := r.pending[cand]; !used {
			id, found = cand, true
			break
		}
	}
	if !found {
		r.mu.Unlock()
		return errNoInvokeID
	}
	pr := &pendingReq{to: to, svc: svc, ch: make(chan apdu.PDU, 1)}
	r.pending[id] = pr
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.pending, id)
		r.mu.Unlock()
	}()

	code, _ := apdu.EncodeMaxAPDUSize(s.cfg.maxAPDU)
	frame := apdu.AppendConfirmedRequest(nil, apdu.ConfirmedRequest{
		MaxAPDU: uint8(code), InvokeID: id, ServiceChoice: svc, Payload: payload,
	})
	for attempt := 0; attempt <= s.cfg.retries; attempt++ {
		sctx, cancel := clock.ContextWithTimeout(ctx, s.clock, s.cfg.apduTimeout)
		if err := s.sendAPDU(sctx, to, false, dest, true, frame); err != nil {
			cancel()
			return err
		}
		select {
		case <-ctx.Done():
			cancel()
			return ctx.Err()
		case pdu := <-pr.ch:
			cancel()
			return responseError(pdu, id)
		case <-sctx.Done():
			cancel()
		}
	}
	return bacnet.ErrTimeout
}

func responseError(pdu apdu.PDU, id uint8) error {
	switch pdu.Type {
	case apdu.TypeSimpleACK, apdu.TypeComplexACK:
		return nil
	case apdu.TypeError:
		class, code, err := apdu.DecodeErrorClassCode(pdu.Error.Payload, bacnet.DefaultDecodeLimits())
		if err != nil {
			return err
		}
		return &bacnet.ErrorResponse{InvokeID: id, Service: pdu.Error.ServiceChoice, Class: class, Code: code}
	case apdu.TypeReject:
		return &bacnet.RejectError{InvokeID: id, Reason: pdu.Reject.Reason}
	default:
		return &bacnet.AbortError{InvokeID: id, Server: pdu.Abort.Server, Reason: pdu.Abort.Reason}
	}
}

// response delivers an answer to a pending request (receive loop). An
// answer completes the request it belongs to: same invoke ID, from the
// node the request went to and, for the PDUs that name a service (ACKs
// and Error), for the service that was requested. Any other is dropped.
func (r *requester) response(pdu apdu.PDU, p peer) {
	var id, svc uint8
	named := true
	switch pdu.Type {
	case apdu.TypeSimpleACK:
		id, svc = pdu.SimpleACK.InvokeID, pdu.SimpleACK.ServiceChoice
	case apdu.TypeComplexACK:
		id, svc = pdu.ComplexACK.InvokeID, pdu.ComplexACK.ServiceChoice
	case apdu.TypeError:
		id, svc = pdu.Error.InvokeID, pdu.Error.ServiceChoice
	case apdu.TypeReject:
		id, named = pdu.Reject.InvokeID, false
	case apdu.TypeAbort:
		id, named = pdu.Abort.InvokeID, false
	default:
		return
	}
	r.mu.Lock()
	pr, ok := r.pending[id]
	r.mu.Unlock()
	if !ok || !pr.to.Equal(p.immediate) {
		return
	}
	if named && svc != pr.svc {
		r.s.report(diag.KindWrongService, "answer for another service than the pending request dropped", map[string]any{"invoke_id": id, "service": svc, "from": p.immediate.String()})
		return
	}
	select {
	case pr.ch <- pdu:
	default:
	}
}

// async runs fn on its own goroutine, which Close waits for. Nothing is
// started once Close has begun.
func (r *requester) async(fn func()) {
	r.s.spawn(fn)
}
