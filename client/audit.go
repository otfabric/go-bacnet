// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/service"
)

// AuditStreamEvent is one queued audit notification.
type AuditStreamEvent struct {
	Sequence     uint64
	Notification service.AuditNotification
	Confirmed    bool
	Gap          bool
	Closed       bool
}

// AuditStream is a bounded asynchronous audit notification consumer.
type AuditStream interface {
	Events() <-chan AuditStreamEvent
	Close()
}

type auditStream struct {
	c         *Client
	ch        chan AuditStreamEvent
	seq       atomic.Uint64
	mu        sync.Mutex
	closed    bool
	gap       bool
	closeOnce sync.Once
}

func (s *auditStream) Events() <-chan AuditStreamEvent { return s.ch }

func (s *auditStream) Close() {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		select {
		case s.ch <- AuditStreamEvent{Closed: true, Sequence: s.seq.Add(1)}:
		default:
		}
		close(s.ch)
		s.mu.Unlock()
		if s.c != nil {
			s.c.auditMu.Lock()
			if s.c.auditStream == s {
				s.c.auditStream = nil
			}
			s.c.auditMu.Unlock()
		}
	})
}

func (s *auditStream) publish(n service.AuditNotification, confirmed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	ev := AuditStreamEvent{Sequence: s.seq.Add(1), Notification: n, Confirmed: confirmed, Gap: s.gap}
	select {
	case s.ch <- ev:
		s.gap = false
	default:
		s.gap = true
	}
}

// OpenAuditStream installs a bounded AuditNotification stream.
func (c *Client) OpenAuditStream(bufferSize int) AuditStream {
	if bufferSize < 1 {
		bufferSize = 1
	}
	s := &auditStream{c: c, ch: make(chan AuditStreamEvent, bufferSize+1)}
	c.auditMu.Lock()
	prev := c.auditStream
	c.auditStream = s
	c.auditMu.Unlock()
	if prev != nil {
		prev.Close()
	}
	return s
}

func (c *Client) deliverAuditNotification(n service.AuditNotification, confirmed bool) {
	c.auditMu.Lock()
	s := c.auditStream
	c.auditMu.Unlock()
	if s != nil {
		s.publish(n, confirmed)
	}
}

// AuditLogQuery performs a confirmed AuditLogQuery.
func (c *Client) AuditLogQuery(ctx context.Context, target Target, req service.AuditLogQueryRequest) (service.AuditLogQueryACK, error) {
	payload, err := service.EncodeAuditLogQuery(req)
	if err != nil {
		return service.AuditLogQueryACK{}, err
	}
	pdu, err := c.confirmedRequest(ctx, target, apdu.ServiceAuditLogQuery, payload, DefaultRetransmitPolicy(apdu.ServiceAuditLogQuery))
	if err != nil {
		return service.AuditLogQueryACK{}, err
	}
	if pdu.Type != apdu.TypeComplexACK || pdu.ComplexACK == nil {
		return service.AuditLogQueryACK{}, bacnet.ErrProtocolViolation
	}
	return service.DecodeAuditLogQueryACK(pdu.ComplexACK.Payload, c.limits)
}

// AuthRequest performs a confirmed AuthRequest.
func (c *Client) AuthRequest(ctx context.Context, target Target, req service.AuthRequest) error {
	payload, err := service.EncodeAuthRequest(req)
	if err != nil {
		return err
	}
	pdu, err := c.confirmedRequest(ctx, target, apdu.ServiceAuthRequest, payload, DefaultRetransmitPolicy(apdu.ServiceAuthRequest))
	if err != nil {
		return err
	}
	if pdu.Type != apdu.TypeSimpleACK && pdu.Type != apdu.TypeComplexACK {
		return bacnet.ErrProtocolViolation
	}
	return nil
}

// WhoAmI sends Unconfirmed Who-Am-I.
func (c *Client) WhoAmI(ctx context.Context, dest bip.Endpoint, broadcast bool, w service.WhoAmI) error {
	payload, err := service.EncodeWhoAmI(w)
	if err != nil {
		return err
	}
	return c.sendUnconfirmed(ctx, dest, broadcast, apdu.ServiceWhoAmI, payload)
}

// YouAre sends Unconfirmed You-Are.
func (c *Client) YouAre(ctx context.Context, dest bip.Endpoint, broadcast bool, y service.YouAre) error {
	payload, err := service.EncodeYouAre(y)
	if err != nil {
		return err
	}
	return c.sendUnconfirmed(ctx, dest, broadcast, apdu.ServiceYouAre, payload)
}
