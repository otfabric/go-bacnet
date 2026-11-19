// SPDX-License-Identifier: MIT

package client

import (
	"context"

	"github.com/otfabric/go-bacnet/apdu"
)

// ConfirmedInvokeOptions controls a low-level confirmed request.
//
// Typed helpers always look up ConfirmedServicePolicy. This escape hatch
// requires the caller to state retransmission and side-effect intent explicitly
// (or accept the safe default: no retransmit, outcome-unknown after send).
type ConfirmedInvokeOptions struct {
	// SegmentedResponseAccepted is written into the confirmed-request header.
	SegmentedResponseAccepted bool
	// Retransmit controls exact-APDU retransmission. Zero means RetransmitDisabled.
	Retransmit RetransmitPolicy
	// SideEffecting, when true, wraps ambiguous post-send failures as
	// *bacnet.OutcomeUnknownError with Operation "InvokeConfirmed".
	SideEffecting bool
}

// InvokeConfirmed is a raw confirmed-request escape hatch.
//
// Prefer typed helpers for production traffic. Default policy is non-retransmitting
// and outcome-unknown after send when SideEffecting is true.
func (c *Client) InvokeConfirmed(ctx context.Context, target Target, serviceChoice uint8, payload []byte, opts ConfirmedInvokeOptions) (apdu.PDU, error) {
	policy := opts.Retransmit
	if policy != RetransmitEnabled {
		policy = RetransmitDisabled
	}
	pdu, err := c.confirmedRequestOpts(ctx, target, serviceChoice, payload, confirmedOpts{
		policy:                    policy,
		segmentedResponseAccepted: opts.SegmentedResponseAccepted,
		forceOutcomeUnknown:       opts.SideEffecting,
		outcomeUnknownName:        "InvokeConfirmed",
	})
	return pdu, err
}

// InvokeUnconfirmed sends an unconfirmed request to one device. The device's
// station address goes in the NPDU (DNET/DADR) when it is behind a router, as
// for a confirmed request; the typed unconfirmed helpers take a B/IP endpoint
// and reach a device on the local network or a broadcast. payload is the
// service request (service.EncodeTimeSynchronization, EncodeWriteGroup …).
func (c *Client) InvokeUnconfirmed(ctx context.Context, target Target, serviceChoice uint8, payload []byte) error {
	target, err := normalizeTarget(target)
	if err != nil {
		return err
	}
	apduBytes := apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{ServiceChoice: serviceChoice, Payload: payload})
	return c.sendAPDU(ctx, target.Endpoint, false, target.Address, false, apduBytes)
}
