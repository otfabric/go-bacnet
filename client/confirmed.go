// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"fmt"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/internal/clock"
	"github.com/otfabric/go-bacnet/internal/diag"
)

// wrapOutcomeUnknown marks side-effecting confirmed services whose request may
// have executed remotely when no definitive response was observed after send.
func wrapOutcomeUnknown(serviceChoice uint8, cause error) error {
	var op string
	switch serviceChoice {
	case apdu.ServiceWriteProperty:
		op = "WriteProperty"
	case apdu.ServiceSubscribeCOV:
		op = "SubscribeCOV"
	case apdu.ServiceSubscribeCOVProperty:
		op = "SubscribeCOVProperty"
	default:
		return cause
	}
	return &bacnet.OutcomeUnknownError{Operation: op, Cause: cause}
}

// Target identifies a remote BACnet device for confirmed requests.
type Target struct {
	Address  bacnet.Address
	Endpoint bip.Endpoint // immediate B/IP peer (device or router)
	Origin   bip.Endpoint // optional forwarded origin expectation
	MaxAPDU  uint16       // 0 = use registry/fallback
}

func (c *Client) confirmedRequest(ctx context.Context, target Target, serviceChoice uint8, payload []byte, policy RetransmitPolicy) (apdu.PDU, error) {
	return c.confirmedRequestOpts(ctx, target, serviceChoice, payload, confirmedOpts{
		policy:                    policy,
		segmentedResponseAccepted: true,
	})
}

type confirmedOpts struct {
	policy                    RetransmitPolicy
	segmentedResponseAccepted bool
}

func (c *Client) confirmedRequestOpts(ctx context.Context, target Target, serviceChoice uint8, payload []byte, opts confirmedOpts) (apdu.PDU, error) {
	if c.isClosed() {
		return apdu.PDU{}, bacnet.ErrClosed
	}
	// MaxAPDU in the confirmed-request header advertises *local* receive capacity.
	localMax, err := c.advertisedMaxAPDU()
	if err != nil {
		return apdu.PDU{}, err
	}
	localCode, err := apdu.EncodeMaxAPDUSize(localMax)
	if err != nil {
		return apdu.PDU{}, err
	}
	remoteMax := c.resolveRemoteMaxAPDU(target)
	maxSegCode := apdu.EncodeMaxSegments(c.limits.MaxSegments)
	encoded := apdu.AppendConfirmedRequest(nil, apdu.ConfirmedRequest{
		SegmentedResponseAccepted: opts.segmentedResponseAccepted,
		MaxSegments:               uint8(maxSegCode),
		MaxAPDU:                   uint8(localCode),
		ServiceChoice:             serviceChoice,
		Payload:                   payload,
		InvokeID:                  0,
	})
	if len(encoded) > int(remoteMax) {
		return apdu.PDU{}, &bacnet.APDUTooLargeError{
			EncodedSize:           len(encoded),
			RemoteMax:             int(remoteMax),
			SegmentationSupported: false,
		}
	}

	id, err := c.allocateInvokeID(ctx)
	if err != nil {
		return apdu.PDU{}, err
	}
	req := apdu.ConfirmedRequest{
		SegmentedResponseAccepted: opts.segmentedResponseAccepted,
		MaxSegments:               uint8(maxSegCode),
		MaxAPDU:                   uint8(localCode),
		InvokeID:                  id,
		ServiceChoice:             serviceChoice,
		Payload:                   payload,
	}
	encoded = apdu.AppendConfirmedRequest(nil, req)

	retries := 0
	if opts.policy == RetransmitEnabled {
		retries = c.cfg.retryCount
	}
	timer := c.clock.NewTimer(c.cfg.apduTimeout)
	tx := &pendingTx{
		invokeID:    id,
		service:     serviceChoice,
		address:     target.Address,
		origin:      target.Origin,
		immediate:   target.Endpoint,
		encodedAPDU: append([]byte(nil), encoded...),
		retriesLeft: retries,
		result:      make(chan txResult, 1),
		timer:       timer,
		sent:        true,
		phase:       txAwaitingInitial,
	}
	c.tx.register(tx)
	defer func() {
		timer.Stop()
		c.seg.remove(id)
	}()

	if err := c.sendAPDU(ctx, target.Endpoint, false, target.Address, true, encoded); err != nil {
		if c.finishTx(id, txResult{err: err}, 0) {
			return apdu.PDU{}, err
		}
		res := <-tx.result
		return res.pdu, res.err
	}

	for {
		select {
		case <-ctx.Done():
			cause := ctx.Err()
			if tx.sent {
				cause = wrapOutcomeUnknown(serviceChoice, cause)
			}
			if c.finishTx(id, txResult{err: cause}, c.cfg.apduTimeout) {
				return apdu.PDU{}, cause
			}
			res := <-tx.result
			return res.pdu, res.err
		case <-c.closeCh:
			if c.finishTx(id, txResult{err: bacnet.ErrClosed}, 0) {
				return apdu.PDU{}, bacnet.ErrClosed
			}
			res := <-tx.result
			return res.pdu, res.err
		case <-timer.C():
			switch c.tx.onTimeout(id, opts.policy == RetransmitEnabled) {
			case timeoutGone:
				res := <-tx.result
				return res.pdu, res.err
			case timeoutIgnoreSegmented:
				// APDU timer ownership transferred; ignore late fire.
				continue
			case timeoutRetransmit:
				_ = c.sendAPDU(ctx, target.Endpoint, false, target.Address, true, tx.encodedAPDU)
				timer.Reset(c.cfg.apduTimeout)
				continue
			default: // timeoutFail
				err := bacnet.ErrTimeout
				if tx.sent {
					err = wrapOutcomeUnknown(serviceChoice, err)
				}
				if c.finishTx(id, txResult{err: err}, c.cfg.apduTimeout) {
					return apdu.PDU{}, err
				}
				res := <-tx.result
				return res.pdu, res.err
			}
		case res := <-tx.result:
			return res.pdu, res.err
		}
	}
}

// finishTx completes the transaction and clears segmentation state when this
// caller wins. Returns whether this caller published the terminal outcome.
func (c *Client) finishTx(invokeID uint8, res txResult, quarantine time.Duration) bool {
	won := c.tx.complete(invokeID, res, quarantine)
	c.seg.remove(invokeID)
	return won
}

func (c *Client) resolveRemoteMaxAPDU(target Target) uint16 {
	if target.MaxAPDU != 0 {
		return target.MaxAPDU
	}
	if caps, ok := c.reg.ResolveCapabilities(target); ok {
		return caps.MaxAPDUOr(480)
	}
	return 480
}

func (c *Client) allocateInvokeID(ctx context.Context) (uint8, error) {
	for {
		c.tx.mu.Lock()
		id, ok := c.tx.tryAllocLocked()
		c.tx.mu.Unlock()
		if ok {
			return id, nil
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-c.closeCh:
			return 0, bacnet.ErrClosed
		case <-c.clock.After(5 * time.Millisecond):
		}
	}
}

func (c *Client) handleConfirmedResponse(pdu apdu.PDU, src packetSource) {
	var invokeID uint8
	var service uint8
	switch pdu.Type {
	case apdu.TypeSimpleACK:
		invokeID = pdu.SimpleACK.InvokeID
		service = pdu.SimpleACK.ServiceChoice
	case apdu.TypeComplexACK:
		if pdu.ComplexACK.SegmentedMessage {
			full, done, segErr := c.seg.accept(pdu.ComplexACK, src, c)
			if segErr != nil {
				c.finishTx(pdu.ComplexACK.InvokeID, txResult{err: segErr}, c.cfg.apduTimeout)
				return
			}
			if !done {
				return
			}
			pdu = full
		}
		invokeID = pdu.ComplexACK.InvokeID
		service = pdu.ComplexACK.ServiceChoice
	case apdu.TypeError:
		invokeID = pdu.Error.InvokeID
		service = pdu.Error.ServiceChoice
	case apdu.TypeReject:
		invokeID = pdu.Reject.InvokeID
	case apdu.TypeAbort:
		invokeID = pdu.Abort.InvokeID
	default:
		return
	}

	tx := c.tx.lookup(invokeID)
	if tx == nil {
		c.diag.Report(diag.Event{Kind: diag.KindUnknownInvokeID, Message: fmt.Sprintf("invoke %d", invokeID)})
		return
	}
	if !c.tx.matchSource(tx, src) {
		c.diag.Report(diag.Event{Kind: diag.KindWrongSource, Message: fmt.Sprintf("invoke %d", invokeID)})
		return
	}
	if tx.service != 0 && service != 0 && tx.service != service && pdu.Type != apdu.TypeReject && pdu.Type != apdu.TypeAbort {
		c.diag.Report(diag.Event{Kind: diag.KindWrongService, Message: fmt.Sprintf("invoke %d", invokeID)})
		return
	}

	var err error
	switch pdu.Type {
	case apdu.TypeError:
		class, code, e := apdu.DecodeErrorClassCode(pdu.Error.Payload, c.limits)
		if e != nil {
			err = e
		} else {
			err = &bacnet.ErrorResponse{InvokeID: invokeID, Service: service, Class: class, Code: code}
		}
	case apdu.TypeReject:
		err = &bacnet.RejectError{InvokeID: invokeID, Reason: pdu.Reject.Reason}
	case apdu.TypeAbort:
		err = &bacnet.AbortError{InvokeID: invokeID, Server: pdu.Abort.Server, Reason: pdu.Abort.Reason}
	case apdu.TypeSimpleACK, apdu.TypeComplexACK:
	default:
		return
	}
	c.finishTx(invokeID, txResult{pdu: pdu, src: src, err: err}, c.cfg.apduTimeout)
}

func (c *Client) handleConfirmedIndication(req *apdu.ConfirmedRequest, src packetSource) {
	if req == nil {
		return
	}
	const confirmedCOVNotification = 1
	switch req.ServiceChoice {
	case confirmedCOVNotification:
		note, err := decodeCOVNotification(req.Payload, c.limits)
		if err != nil {
			c.diag.Report(diag.Event{Kind: diag.KindMalformed, Message: err.Error()})
			return
		}
		ack := apdu.AppendSimpleACK(nil, apdu.SimpleACK{InvokeID: req.InvokeID, ServiceChoice: req.ServiceChoice})
		destAddr := responseDestination(src)
		ackCtx, cancel := clock.ContextWithTimeout(context.Background(), c.clock, c.cfg.apduTimeout)
		if err := c.sendAPDU(ackCtx, src.immediate, false, destAddr, false, ack); err != nil {
			c.diag.Report(diag.Event{
				Kind:    diag.KindCOV,
				Message: "confirmed COV SimpleACK send failed",
				Fields:  map[string]any{"error": err.Error(), "invoke": req.InvokeID},
			})
		}
		cancel()
		c.subs.deliver(SubscriptionEvent{
			Notification: note,
			State:        SubscriptionActive,
		}, note.ProcessIdentifier, src)
	default:
		c.diag.Report(diag.Event{Kind: diag.KindUnexpectedAPDU, Message: fmt.Sprintf("confirmed service %d", req.ServiceChoice)})
	}
}

// responseDestination returns the BACnet destination for replies to an indication.
func responseDestination(src packetSource) bacnet.Address {
	switch src.bacnetAddress.Scope() {
	case bacnet.AddressRemoteStation, bacnet.AddressRemoteBroadcast:
		return src.bacnetAddress
	default:
		return bacnet.Address{}
	}
}
