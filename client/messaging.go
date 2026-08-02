// SPDX-License-Identifier: MIT

package client

import (
	"context"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/service"
)

// ConfirmedPrivateTransfer performs ConfirmedPrivateTransfer.
func (c *Client) ConfirmedPrivateTransfer(ctx context.Context, target Target, req service.PrivateTransfer) error {
	payload, err := service.EncodePrivateTransfer(req)
	if err != nil {
		return err
	}
	pdu, err := c.confirmedRequest(ctx, target, apdu.ServiceConfirmedPrivateTransfer, payload, DefaultRetransmitPolicy(apdu.ServiceConfirmedPrivateTransfer))
	if err != nil {
		return err
	}
	if pdu.Type != apdu.TypeSimpleACK && pdu.Type != apdu.TypeComplexACK {
		return bacnet.ErrProtocolViolation
	}
	return nil
}

// UnconfirmedPrivateTransfer sends UnconfirmedPrivateTransfer.
func (c *Client) UnconfirmedPrivateTransfer(ctx context.Context, dest bip.Endpoint, broadcast bool, req service.PrivateTransfer) error {
	payload, err := service.EncodePrivateTransfer(req)
	if err != nil {
		return err
	}
	return c.sendUnconfirmed(ctx, dest, broadcast, apdu.ServiceUnconfirmedPrivateTransfer, payload)
}

// ConfirmedTextMessage performs ConfirmedTextMessage.
func (c *Client) ConfirmedTextMessage(ctx context.Context, target Target, req service.TextMessage) error {
	payload, err := service.EncodeTextMessage(req)
	if err != nil {
		return err
	}
	pdu, err := c.confirmedRequest(ctx, target, apdu.ServiceConfirmedTextMessage, payload, DefaultRetransmitPolicy(apdu.ServiceConfirmedTextMessage))
	if err != nil {
		return err
	}
	if pdu.Type != apdu.TypeSimpleACK {
		return bacnet.ErrProtocolViolation
	}
	return nil
}

// UnconfirmedTextMessage sends UnconfirmedTextMessage.
func (c *Client) UnconfirmedTextMessage(ctx context.Context, dest bip.Endpoint, broadcast bool, req service.TextMessage) error {
	payload, err := service.EncodeTextMessage(req)
	if err != nil {
		return err
	}
	return c.sendUnconfirmed(ctx, dest, broadcast, apdu.ServiceUnconfirmedTextMessage, payload)
}

// TimeSynchronization sends TimeSynchronization.
func (c *Client) TimeSynchronization(ctx context.Context, dest bip.Endpoint, broadcast bool, t service.TimeSynchronization) error {
	payload, err := service.EncodeTimeSynchronization(t)
	if err != nil {
		return err
	}
	return c.sendUnconfirmed(ctx, dest, broadcast, apdu.ServiceTimeSynchronization, payload)
}

// UTCTimeSynchronization sends UTCTimeSynchronization.
func (c *Client) UTCTimeSynchronization(ctx context.Context, dest bip.Endpoint, broadcast bool, t service.TimeSynchronization) error {
	payload, err := service.EncodeTimeSynchronization(t)
	if err != nil {
		return err
	}
	return c.sendUnconfirmed(ctx, dest, broadcast, apdu.ServiceUTCTimeSynchronization, payload)
}

// WriteGroup sends Unconfirmed WriteGroup.
func (c *Client) WriteGroup(ctx context.Context, dest bip.Endpoint, broadcast bool, w service.WriteGroup) error {
	payload, err := service.EncodeWriteGroup(w)
	if err != nil {
		return err
	}
	return c.sendUnconfirmed(ctx, dest, broadcast, apdu.ServiceWriteGroup, payload)
}

func (c *Client) sendUnconfirmed(ctx context.Context, dest bip.Endpoint, broadcast bool, choice uint8, payload []byte) error {
	apduBytes := apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{ServiceChoice: choice, Payload: payload})
	addr := bacnet.LocalBroadcast()
	if !broadcast {
		if a, ok := bipMACAddress(dest); ok {
			addr = a
		} else {
			addr = bacnet.LocalStation(bacnet.MAC{})
		}
	}
	return c.sendAPDU(ctx, dest, broadcast, addr, false, apduBytes)
}
