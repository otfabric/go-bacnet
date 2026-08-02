// SPDX-License-Identifier: MIT

package client

import (
	"context"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/service"
)

// AddListElement performs a confirmed AddListElement.
//
// Exact-APDU retransmission is disabled. After send, timeout/cancel returns
// *bacnet.OutcomeUnknownError.
func (c *Client) AddListElement(ctx context.Context, target Target, req service.ListElementRequest) error {
	return c.listElement(ctx, target, apdu.ServiceAddListElement, req)
}

// RemoveListElement performs a confirmed RemoveListElement.
func (c *Client) RemoveListElement(ctx context.Context, target Target, req service.ListElementRequest) error {
	return c.listElement(ctx, target, apdu.ServiceRemoveListElement, req)
}

func (c *Client) listElement(ctx context.Context, target Target, choice uint8, req service.ListElementRequest) error {
	payload, err := service.EncodeListElementRequest(req)
	if err != nil {
		return err
	}
	pdu, err := c.confirmedRequest(ctx, target, choice, payload, DefaultRetransmitPolicy(choice))
	if err != nil {
		return err
	}
	if pdu.Type != apdu.TypeSimpleACK {
		return bacnet.ErrProtocolViolation
	}
	return nil
}

// CreateObject performs a confirmed CreateObject.
func (c *Client) CreateObject(ctx context.Context, target Target, req service.CreateObjectRequest) (service.CreateObjectACK, error) {
	payload, err := service.EncodeCreateObject(req)
	if err != nil {
		return service.CreateObjectACK{}, err
	}
	pdu, err := c.confirmedRequest(ctx, target, apdu.ServiceCreateObject, payload, DefaultRetransmitPolicy(apdu.ServiceCreateObject))
	if err != nil {
		return service.CreateObjectACK{}, err
	}
	if pdu.Type != apdu.TypeComplexACK || pdu.ComplexACK == nil {
		return service.CreateObjectACK{}, bacnet.ErrProtocolViolation
	}
	return service.DecodeCreateObjectACK(pdu.ComplexACK.Payload, c.limits)
}

// DeleteObject performs a confirmed DeleteObject.
func (c *Client) DeleteObject(ctx context.Context, target Target, req service.DeleteObjectRequest) error {
	payload, err := service.EncodeDeleteObject(req)
	if err != nil {
		return err
	}
	pdu, err := c.confirmedRequest(ctx, target, apdu.ServiceDeleteObject, payload, DefaultRetransmitPolicy(apdu.ServiceDeleteObject))
	if err != nil {
		return err
	}
	if pdu.Type != apdu.TypeSimpleACK {
		return bacnet.ErrProtocolViolation
	}
	return nil
}

// CreateObjectAndReadback creates an object then reads a property for verification.
func (c *Client) CreateObjectAndReadback(ctx context.Context, target Target, req service.CreateObjectRequest, prop bacnet.PropertyReference) (service.CreateObjectACK, bacnet.ApplicationValue, error) {
	ack, err := c.CreateObject(ctx, target, req)
	if err != nil {
		return service.CreateObjectACK{}, bacnet.ApplicationValue{}, err
	}
	val, err := c.ReadProperty(ctx, target, ack.Object, prop)
	if err != nil {
		return ack, bacnet.ApplicationValue{}, err
	}
	return ack, val, nil
}
