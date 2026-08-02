// SPDX-License-Identifier: MIT

package client

import (
	"context"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/service"
)

// AtomicReadFile performs a confirmed AtomicReadFile.
//
// Exact-APDU retransmission is enabled (read-like). Large ACKs may arrive segmented.
func (c *Client) AtomicReadFile(ctx context.Context, target Target, req service.AtomicReadFileRequest) (service.AtomicReadFileACK, error) {
	payload, err := service.EncodeAtomicReadFile(req)
	if err != nil {
		return service.AtomicReadFileACK{}, err
	}
	pdu, err := c.confirmedRequest(ctx, target, apdu.ServiceAtomicReadFile, payload, DefaultRetransmitPolicy(apdu.ServiceAtomicReadFile))
	if err != nil {
		return service.AtomicReadFileACK{}, err
	}
	if pdu.Type != apdu.TypeComplexACK || pdu.ComplexACK == nil {
		return service.AtomicReadFileACK{}, bacnet.ErrProtocolViolation
	}
	return service.DecodeAtomicReadFileACK(pdu.ComplexACK.Payload, c.limits)
}

// AtomicWriteFile performs a confirmed AtomicWriteFile.
//
// Exact-APDU retransmission is disabled. After the request has been sent,
// timeout/cancel returns *bacnet.OutcomeUnknownError.
func (c *Client) AtomicWriteFile(ctx context.Context, target Target, req service.AtomicWriteFileRequest) (service.AtomicWriteFileACK, error) {
	payload, err := service.EncodeAtomicWriteFile(req)
	if err != nil {
		return service.AtomicWriteFileACK{}, err
	}
	pdu, err := c.confirmedRequest(ctx, target, apdu.ServiceAtomicWriteFile, payload, DefaultRetransmitPolicy(apdu.ServiceAtomicWriteFile))
	if err != nil {
		return service.AtomicWriteFileACK{}, err
	}
	if pdu.Type != apdu.TypeComplexACK || pdu.ComplexACK == nil {
		return service.AtomicWriteFileACK{}, bacnet.ErrProtocolViolation
	}
	return service.DecodeAtomicWriteFileACK(pdu.ComplexACK.Payload, c.limits)
}
