// SPDX-License-Identifier: MIT

package client

import (
	"context"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/service"
)

// AcknowledgeAlarm performs a confirmed AcknowledgeAlarm.
//
// Exact-APDU retransmission is disabled. After the request has been sent,
// timeout/cancel returns *bacnet.OutcomeUnknownError.
func (c *Client) AcknowledgeAlarm(ctx context.Context, target Target, req service.AcknowledgeAlarmRequest) error {
	payload, err := service.EncodeAcknowledgeAlarm(req)
	if err != nil {
		return err
	}
	pdu, err := c.confirmedRequest(ctx, target, apdu.ServiceAcknowledgeAlarm, payload, DefaultRetransmitPolicy(apdu.ServiceAcknowledgeAlarm))
	if err != nil {
		return err
	}
	if pdu.Type != apdu.TypeSimpleACK {
		return bacnet.ErrProtocolViolation
	}
	return nil
}

// GetEventInformation performs a confirmed GetEventInformation.
//
// Exact-APDU retransmission is enabled (read-like). Large ACKs may arrive
// segmented. Summary EventTimeStamps / EventPriorities ownership transfers to
// the caller (cloned on decode).
func (c *Client) GetEventInformation(ctx context.Context, target Target, req service.GetEventInformationRequest) (service.GetEventInformationACK, error) {
	payload, err := service.EncodeGetEventInformation(req)
	if err != nil {
		return service.GetEventInformationACK{}, err
	}
	pdu, err := c.confirmedRequest(ctx, target, apdu.ServiceGetEventInformation, payload, DefaultRetransmitPolicy(apdu.ServiceGetEventInformation))
	if err != nil {
		return service.GetEventInformationACK{}, err
	}
	if pdu.Type != apdu.TypeComplexACK || pdu.ComplexACK == nil {
		return service.GetEventInformationACK{}, bacnet.ErrProtocolViolation
	}
	return service.DecodeGetEventInformationACK(pdu.ComplexACK.Payload, c.limits)
}
