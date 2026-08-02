// SPDX-License-Identifier: MIT

package client

import (
	"context"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/service"
)

// LifeSafetyOperation performs a confirmed LifeSafetyOperation.
func (c *Client) LifeSafetyOperation(ctx context.Context, target Target, req service.LifeSafetyOperationRequest) error {
	payload, err := service.EncodeLifeSafetyOperation(req)
	if err != nil {
		return err
	}
	pdu, err := c.confirmedRequest(ctx, target, apdu.ServiceLifeSafetyOperation, payload, DefaultRetransmitPolicy(apdu.ServiceLifeSafetyOperation))
	if err != nil {
		return err
	}
	if pdu.Type != apdu.TypeSimpleACK {
		return bacnet.ErrProtocolViolation
	}
	return nil
}

// VTOpen performs VT-Open.
func (c *Client) VTOpen(ctx context.Context, target Target, req service.VTOpenRequest) (service.VTOpenACK, error) {
	payload, err := service.EncodeVTOpen(req)
	if err != nil {
		return service.VTOpenACK{}, err
	}
	pdu, err := c.confirmedRequest(ctx, target, apdu.ServiceVTOpen, payload, DefaultRetransmitPolicy(apdu.ServiceVTOpen))
	if err != nil {
		return service.VTOpenACK{}, err
	}
	if pdu.Type != apdu.TypeComplexACK || pdu.ComplexACK == nil {
		return service.VTOpenACK{}, bacnet.ErrProtocolViolation
	}
	return service.DecodeVTOpenACK(pdu.ComplexACK.Payload, c.limits)
}

// VTClose performs VT-Close.
func (c *Client) VTClose(ctx context.Context, target Target, req service.VTCloseRequest) error {
	payload, err := service.EncodeVTClose(req)
	if err != nil {
		return err
	}
	pdu, err := c.confirmedRequest(ctx, target, apdu.ServiceVTClose, payload, DefaultRetransmitPolicy(apdu.ServiceVTClose))
	if err != nil {
		return err
	}
	if pdu.Type != apdu.TypeSimpleACK {
		return bacnet.ErrProtocolViolation
	}
	return nil
}

// VTData performs VT-Data.
func (c *Client) VTData(ctx context.Context, target Target, req service.VTDataRequest) error {
	payload, err := service.EncodeVTData(req)
	if err != nil {
		return err
	}
	pdu, err := c.confirmedRequest(ctx, target, apdu.ServiceVTData, payload, DefaultRetransmitPolicy(apdu.ServiceVTData))
	if err != nil {
		return err
	}
	if pdu.Type != apdu.TypeSimpleACK && pdu.Type != apdu.TypeComplexACK {
		return bacnet.ErrProtocolViolation
	}
	return nil
}
