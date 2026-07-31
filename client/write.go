// SPDX-License-Identifier: MIT

package client

import (
	"context"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/service"
)

// WriteProperty performs a confirmed WriteProperty.
// Exact-APDU retransmission is disabled by default for WriteProperty.
// Priority may be nil; use NullValue for relinquish.
func (c *Client) WriteProperty(ctx context.Context, target Target, object bacnet.ObjectIdentifier, property bacnet.PropertyReference, value bacnet.ApplicationValue, priority *uint8) error {
	payload, err := service.EncodeWriteProperty(service.WritePropertyRequest{
		Object:   object,
		Property: property,
		Value:    value,
		Priority: priority,
	})
	if err != nil {
		return err
	}
	pdu, err := c.confirmedRequest(ctx, target, apdu.ServiceWriteProperty, payload, DefaultRetransmitPolicy(apdu.ServiceWriteProperty))
	if err != nil {
		return err
	}
	if pdu.Type != apdu.TypeSimpleACK {
		return bacnet.ErrProtocolViolation
	}
	return nil
}
