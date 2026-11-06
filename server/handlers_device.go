// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"crypto/subtle"
	"sync"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/server/object"
	"github.com/otfabric/go-bacnet/service"
)

// dccState is the DeviceCommunicationControl state (clause 16.1).
type dccState struct {
	mu    sync.Mutex
	mode  service.EnableDisable
	until time.Time // zero: indefinite
}

func (d *dccState) current(now time.Time) service.EnableDisable {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.mode != service.EnableDisableEnable && !d.until.IsZero() && !now.Before(d.until) {
		d.mode = service.EnableDisableEnable
	}
	return d.mode
}

func (d *dccState) expire(now time.Time) { d.current(now) }

func (d *dccState) set(mode service.EnableDisable, until time.Time) {
	d.mu.Lock()
	d.mode, d.until = mode, until
	d.mu.Unlock()
}

// respondsTo reports whether a request may be answered. With "disable" the
// device answers only DeviceCommunicationControl and ReinitializeDevice;
// unconfirmed requests (service 0xFF here) are ignored (clause 16.1).
func (d *dccState) respondsTo(now time.Time, confirmedService uint8) bool {
	if d.current(now) != service.EnableDisableDisable {
		return true
	}
	return confirmedService == apdu.ServiceDeviceCommunicationControl || confirmedService == apdu.ServiceReinitializeDevice
}

// mayInitiate reports whether the device may initiate services (I-Am,
// notifications). "disable-initiation" stops them, but I-Am still answers
// Who-Is (clause 16.1).
func (d *dccState) mayInitiate(now time.Time) bool {
	return d.current(now) == service.EnableDisableEnable
}

// checkPassword compares a request password with the configured one; with
// none configured every request passes (clauses 16.1, 16.4).
func (s *Server) checkPassword(p *bacnet.CharacterString) error {
	if s.cfg.password == "" {
		return nil
	}
	if p == nil || subtle.ConstantTimeCompare([]byte(p.Value), []byte(s.cfg.password)) != 1 {
		return object.Err(bacnet.ErrorClassSecurity, bacnet.ErrorCodePasswordFailure)
	}
	return nil
}

func (s *Server) deviceCommunicationControl(_ context.Context, req *apdu.ConfirmedRequest, _ peer) result {
	r, err := service.DecodeDeviceCommunicationControl(req.Payload, s.limits)
	if err != nil {
		return rejectFor(err)
	}
	if err := s.checkPassword(r.Password); err != nil {
		return fail(err)
	}
	if r.EnableDisable == service.EnableDisableDisable && s.dev.Config().ProtocolRevision >= 20 {
		// DISABLE is deprecated from protocol revision 20 and answered with
		// services/service-request-denied (as bacnet-stack does, h_dcc.c).
		return fail(object.Err(bacnet.ErrorClassServices, bacnet.ErrorCodeServiceRequestDenied))
	}
	var until time.Time
	if r.TimeDuration != nil && r.EnableDisable != service.EnableDisableEnable {
		until = s.clock.Now().Add(time.Duration(*r.TimeDuration) * time.Minute)
	}
	s.dcc.set(r.EnableDisable, until)
	return ack()
}

func (s *Server) reinitializeDevice(ctx context.Context, req *apdu.ConfirmedRequest, _ peer) result {
	r, err := service.DecodeReinitializeDevice(req.Payload, s.limits)
	if err != nil {
		return rejectFor(err)
	}
	if err := s.checkPassword(r.Password); err != nil {
		return fail(err)
	}
	if h := s.cfg.hooks.Reinitialize; h != nil {
		if err := h(ctx, r.State); err != nil {
			return fail(err)
		}
	}
	return ack()
}

func (s *Server) timeSynchronization(_ context.Context, req *apdu.UnconfirmedRequest, _ peer) {
	s.timeSync(req, false)
}

func (s *Server) utcTimeSynchronization(_ context.Context, req *apdu.UnconfirmedRequest, _ peer) {
	s.timeSync(req, true)
}

func (s *Server) timeSync(req *apdu.UnconfirmedRequest, utc bool) {
	t, err := service.DecodeTimeSynchronization(req.Payload, s.limits)
	if err != nil {
		return
	}
	s.cfg.hooks.TimeSync(t, utc)
}

func (s *Server) confirmedPrivateTransfer(ctx context.Context, req *apdu.ConfirmedRequest, _ peer) result {
	p, err := service.DecodePrivateTransfer(req.Payload, s.limits)
	if err != nil {
		return rejectFor(err)
	}
	block, err := s.cfg.hooks.PrivateTransfer(ctx, p, true)
	if err != nil {
		return fail(err)
	}
	// ConfirmedPrivateTransfer-ACK: vendorID [0], serviceNumber [1],
	// resultBlock [2] OPTIONAL (clause 16.2).
	payload, err := bacnet.AppendContextUnsigned(nil, 0, uint64(p.VendorID))
	if err == nil {
		payload, err = bacnet.AppendContextUnsigned(payload, 1, uint64(p.ServiceNumber))
	}
	if err == nil && len(block) > 0 {
		payload, err = bacnet.AppendContextTagged(payload, 2, block)
	}
	if err != nil {
		return fail(err)
	}
	return complexACK(payload)
}

func (s *Server) unconfirmedPrivateTransfer(ctx context.Context, req *apdu.UnconfirmedRequest, _ peer) {
	p, err := service.DecodePrivateTransfer(req.Payload, s.limits)
	if err != nil {
		return
	}
	_, _ = s.cfg.hooks.PrivateTransfer(ctx, p, false)
}

func (s *Server) confirmedTextMessage(ctx context.Context, req *apdu.ConfirmedRequest, _ peer) result {
	m, err := service.DecodeTextMessage(req.Payload, s.limits)
	if err != nil {
		return rejectFor(err)
	}
	if err := s.cfg.hooks.TextMessage(ctx, m, true); err != nil {
		return fail(err)
	}
	return ack()
}

func (s *Server) unconfirmedTextMessage(ctx context.Context, req *apdu.UnconfirmedRequest, _ peer) {
	m, err := service.DecodeTextMessage(req.Payload, s.limits)
	if err != nil {
		return
	}
	_ = s.cfg.hooks.TextMessage(ctx, m, false)
}

func (s *Server) writeGroup(ctx context.Context, req *apdu.UnconfirmedRequest, _ peer) {
	w, err := service.DecodeWriteGroup(req.Payload, s.limits)
	if err != nil {
		return
	}
	s.cfg.hooks.WriteGroup(ctx, w)
}
