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
)

// requester sends the server's own confirmed requests (Confirmed COV and
// Event notifications) and matches their answers (clause 5.4.4). Requests
// are never segmented; a notification that does not fit is not sent.
type requester struct {
	s       *Server
	mu      sync.Mutex
	next    uint8
	pending map[uint8]*pendingReq
	wg      sync.WaitGroup
	gmu     sync.Mutex
	closing bool
}

type pendingReq struct {
	to  bip.Endpoint
	svc uint8
	ch  chan apdu.PDU
}

var errNoInvokeID = errors.New("server: no free invoke ID")

func newRequester(s *Server) *requester {
	return &requester{s: s, pending: make(map[uint8]*pendingReq)}
}

func (r *requester) close() {
	r.gmu.Lock()
	r.closing = true
	r.gmu.Unlock()
	r.wg.Wait()
}

// invoke sends a confirmed request and waits for its answer, retrying on
// timeout. A SimpleACK returns nil; Error, Reject and Abort are returned
// as the client package's error types.
func (r *requester) invoke(ctx context.Context, to bip.Endpoint, dest bacnet.Address, svc uint8, payload []byte) error {
	s := r.s
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

// response delivers an answer to a pending request (receive loop).
func (r *requester) response(pdu apdu.PDU, p peer) {
	var id uint8
	switch pdu.Type {
	case apdu.TypeSimpleACK:
		id = pdu.SimpleACK.InvokeID
	case apdu.TypeComplexACK:
		id = pdu.ComplexACK.InvokeID
	case apdu.TypeError:
		id = pdu.Error.InvokeID
	case apdu.TypeReject:
		id = pdu.Reject.InvokeID
	case apdu.TypeAbort:
		id = pdu.Abort.InvokeID
	default:
		return
	}
	r.mu.Lock()
	pr, ok := r.pending[id]
	r.mu.Unlock()
	if !ok || !pr.to.Equal(p.immediate) {
		return
	}
	select {
	case pr.ch <- pdu:
	default:
	}
}

// async runs fn on its own goroutine, tracked so that Close waits for it.
func (r *requester) async(fn func()) {
	r.gmu.Lock()
	defer r.gmu.Unlock()
	if r.closing || r.s.closed.Load() {
		return
	}
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		fn()
	}()
}
