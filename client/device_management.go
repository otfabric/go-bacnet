// SPDX-License-Identifier: MIT

package client

import (
	"context"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/service"
)

// DeviceCommunicationControl performs a confirmed DeviceCommunicationControl.
//
// Requires WithDeviceManagementEnabled(DeviceManagementConfirm). Exact-APDU
// retransmission is disabled. After send, timeout/cancel returns
// *bacnet.OutcomeUnknownError — the peer may already have applied the control.
func (c *Client) DeviceCommunicationControl(ctx context.Context, target Target, req service.DeviceCommunicationControlRequest) error {
	if !c.cfg.deviceManagementEnabled {
		return bacnet.ErrDeviceManagementDisabled
	}
	payload, err := service.EncodeDeviceCommunicationControl(req)
	if err != nil {
		return err
	}
	pdu, err := c.confirmedRequest(ctx, target, apdu.ServiceDeviceCommunicationControl, payload, DefaultRetransmitPolicy(apdu.ServiceDeviceCommunicationControl))
	if err != nil {
		return err
	}
	if pdu.Type != apdu.TypeSimpleACK {
		return bacnet.ErrProtocolViolation
	}
	return nil
}

// ReinitializeDevice performs a confirmed ReinitializeDevice.
//
// Requires WithDeviceManagementEnabled(DeviceManagementConfirm). Exact-APDU
// retransmission is disabled. After send, timeout/cancel returns
// *bacnet.OutcomeUnknownError — the peer may already be reinitializing.
func (c *Client) ReinitializeDevice(ctx context.Context, target Target, req service.ReinitializeDeviceRequest) error {
	if !c.cfg.deviceManagementEnabled {
		return bacnet.ErrDeviceManagementDisabled
	}
	payload, err := service.EncodeReinitializeDevice(req)
	if err != nil {
		return err
	}
	pdu, err := c.confirmedRequest(ctx, target, apdu.ServiceReinitializeDevice, payload, DefaultRetransmitPolicy(apdu.ServiceReinitializeDevice))
	if err != nil {
		return err
	}
	if pdu.Type != apdu.TypeSimpleACK {
		return bacnet.ErrProtocolViolation
	}
	return nil
}
