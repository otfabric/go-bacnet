// SPDX-License-Identifier: MIT

package client

import (
	"context"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/service"
)

// WritePropertyMultiple performs a confirmed WritePropertyMultiple.
//
// Exact-APDU retransmission is disabled. After any segment or unsegmented
// request has been sent, timeout/cancel returns *bacnet.OutcomeUnknownError:
// earlier properties in the request may already have been applied.
//
// Full success is a SimpleACK. Failure is a WritePropertyMultiple-Error PDU
// decoded as *service.WritePropertyMultipleError (first-failed write only;
// BACnet does not return per-property success lists for WPM).
func (c *Client) WritePropertyMultiple(ctx context.Context, target Target, specs []service.WriteAccessSpecification) error {
	payload, err := service.EncodeWritePropertyMultiple(specs)
	if err != nil {
		return err
	}
	pdu, err := c.confirmedRequest(ctx, target, apdu.ServiceWritePropertyMultiple, payload, DefaultRetransmitPolicy(apdu.ServiceWritePropertyMultiple))
	if err != nil {
		return err
	}
	if pdu.Type != apdu.TypeSimpleACK {
		return bacnet.ErrProtocolViolation
	}
	return nil
}
