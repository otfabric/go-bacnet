// SPDX-License-Identifier: MIT

package client

import (
	"context"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/service"
)

// ReadRange performs a confirmed ReadRange.
//
// Exact-APDU retransmission is enabled by default (same as ReadProperty).
// Large ACKs may arrive segmented; the transaction-owned reassembler handles
// that path. ItemData ownership transfers to the caller (cloned on decode).
func (c *Client) ReadRange(ctx context.Context, target Target, req service.ReadRangeRequest) (service.ReadRangeACK, error) {
	payload, err := service.EncodeReadRange(req)
	if err != nil {
		return service.ReadRangeACK{}, err
	}
	pdu, err := c.confirmedRequest(ctx, target, apdu.ServiceReadRange, payload, DefaultRetransmitPolicy(apdu.ServiceReadRange))
	if err != nil {
		return service.ReadRangeACK{}, err
	}
	if pdu.Type != apdu.TypeComplexACK || pdu.ComplexACK == nil {
		return service.ReadRangeACK{}, bacnet.ErrProtocolViolation
	}
	ack, err := service.DecodeReadRangeACK(pdu.ComplexACK.Payload, c.limits)
	if err != nil {
		return service.ReadRangeACK{}, err
	}
	if ack.Object != req.Object || !ack.Property.Equal(req.Property) {
		return service.ReadRangeACK{}, bacnet.ErrProtocolViolation
	}
	return ack, nil
}
