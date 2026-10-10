// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"fmt"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/service"
)

// ReadProperty performs a confirmed ReadProperty.
//
// A primitive property comes back as that value. A list, array or constructed
// property comes back with Kind ValueConstructed and its items in Elements.
// The Device wildcard (bacnet.AnyDevice) is accepted as object.
func (c *Client) ReadProperty(ctx context.Context, target Target, object bacnet.ObjectIdentifier, property bacnet.PropertyReference) (bacnet.ApplicationValue, error) {
	payload, err := service.EncodeReadProperty(service.ReadPropertyRequest{
		Object:   object,
		Property: property,
	})
	if err != nil {
		return bacnet.ApplicationValue{}, err
	}
	pdu, err := c.confirmedRequest(ctx, target, apdu.ServiceReadProperty, payload, DefaultRetransmitPolicy(apdu.ServiceReadProperty))
	if err != nil {
		return bacnet.ApplicationValue{}, err
	}
	if pdu.Type != apdu.TypeComplexACK || pdu.ComplexACK == nil {
		return bacnet.ApplicationValue{}, bacnet.ErrProtocolViolation
	}
	ack, err := service.DecodeReadPropertyACK(pdu.ComplexACK.Payload, c.limits)
	if err != nil {
		return bacnet.ApplicationValue{}, err
	}
	if !object.Matches(ack.Object) || !ack.Property.Equal(property) {
		return bacnet.ApplicationValue{}, fmt.Errorf("%w: ReadProperty ACK is for %v %v, requested %v %v",
			bacnet.ErrProtocolViolation, ack.Object, ack.Property.Identifier, object, property.Identifier)
	}
	return ack.Value, nil
}

// ReadPropertyMultiple performs a confirmed ReadPropertyMultiple.
// Top-level error is only for whole-transaction failure; property errors are in results.
func (c *Client) ReadPropertyMultiple(ctx context.Context, target Target, specs []service.ReadAccessSpecification) ([]service.ReadAccessResult, error) {
	payload, err := service.EncodeReadPropertyMultiple(specs)
	if err != nil {
		return nil, err
	}
	pdu, err := c.confirmedRequest(ctx, target, apdu.ServiceReadPropertyMultiple, payload, DefaultRetransmitPolicy(apdu.ServiceReadPropertyMultiple))
	if err != nil {
		return nil, err
	}
	if pdu.Type != apdu.TypeComplexACK || pdu.ComplexACK == nil {
		return nil, bacnet.ErrProtocolViolation
	}
	return service.DecodeReadPropertyMultipleACK(pdu.ComplexACK.Payload, c.limits)
}
