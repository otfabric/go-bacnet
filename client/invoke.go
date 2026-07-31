// SPDX-License-Identifier: MIT

package client

import (
	"context"

	"github.com/otfabric/go-bacnet/apdu"
)

// ConfirmedInvokeOptions controls a low-level confirmed request.
//
// Typed helpers (ReadProperty, WriteProperty, …) always accept segmented
// responses. This experimental API exists so callers (and interop tests) can
// observe Reject/Abort outcomes that depend on the confirmed-request header.
type ConfirmedInvokeOptions struct {
	// SegmentedResponseAccepted is written into the confirmed-request header.
	SegmentedResponseAccepted bool
}

// InvokeConfirmed is an experimental raw confirmed-request escape hatch.
//
// It sends an arbitrary service choice and payload with retransmission disabled.
// Callers own payload bytes through the send; Error, Reject, and Abort PDUs
// surface as *bacnet.ErrorResponse, *bacnet.RejectError, and *bacnet.AbortError.
// Prefer typed helpers for production traffic. The surface may change in v0.x.
func (c *Client) InvokeConfirmed(ctx context.Context, target Target, serviceChoice uint8, payload []byte, opts ConfirmedInvokeOptions) (apdu.PDU, error) {
	return c.confirmedRequestOpts(ctx, target, serviceChoice, payload, confirmedOpts{
		policy:                    RetransmitDisabled,
		segmentedResponseAccepted: opts.SegmentedResponseAccepted,
	})
}
