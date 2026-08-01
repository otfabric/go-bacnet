// SPDX-License-Identifier: MIT

package service

import (
	"fmt"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
)

// EnableDisable is the DeviceCommunicationControl enable/disable choice.
type EnableDisable uint8

const (
	EnableDisableEnable            EnableDisable = 0
	EnableDisableDisable           EnableDisable = 1
	EnableDisableDisableInitiation EnableDisable = 2
)

// DeviceCommunicationControlRequest is a DeviceCommunicationControl payload.
type DeviceCommunicationControlRequest struct {
	TimeDuration  *uint16 // minutes; nil = indefinite / unused for enable
	EnableDisable EnableDisable
	Password      *bacnet.CharacterString // optional; max 20 characters
}

// EncodeDeviceCommunicationControl encodes a DCC request payload.
func EncodeDeviceCommunicationControl(req DeviceCommunicationControlRequest) ([]byte, error) {
	if req.EnableDisable > EnableDisableDisableInitiation {
		return nil, fmt.Errorf("%w: DeviceCommunicationControl enableDisable out of range", bacnet.ErrMalformed)
	}
	if req.Password != nil && len(req.Password.Value) > 20 {
		return nil, fmt.Errorf("%w: DeviceCommunicationControl password longer than 20", bacnet.ErrMalformed)
	}
	var dst []byte
	var err error
	if req.TimeDuration != nil {
		dst, err = bacnet.AppendContextUnsigned(dst, 0, uint64(*req.TimeDuration))
		if err != nil {
			return nil, err
		}
	}
	dst, err = bacnet.AppendContextUnsigned(dst, 1, uint64(req.EnableDisable))
	if err != nil {
		return nil, err
	}
	if req.Password != nil {
		dst, err = bacnet.AppendContextCharacterString(dst, 2, *req.Password)
	}
	return dst, err
}

// DecodeDeviceCommunicationControl decodes a DCC request payload.
func DecodeDeviceCommunicationControl(payload []byte, limits bacnet.DecodeLimits) (DeviceCommunicationControlRequest, error) {
	elements, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return DeviceCommunicationControlRequest{}, err
	}
	if n != len(payload) {
		return DeviceCommunicationControlRequest{}, fmt.Errorf("%w: DeviceCommunicationControl trailing data", bacnet.ErrTrailingData)
	}
	var req DeviceCommunicationControlRequest
	var haveEnable bool
	for _, el := range elements {
		switch {
		case el.TagNumber == 0 && !bacnet.IsContextConstructed(el):
			u, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return DeviceCommunicationControlRequest{}, err
			}
			if u > 0xFFFF {
				return DeviceCommunicationControlRequest{}, fmt.Errorf("%w: timeDuration overflow", bacnet.ErrMalformed)
			}
			d := uint16(u)
			req.TimeDuration = &d
		case el.TagNumber == 1 && !bacnet.IsContextConstructed(el):
			if haveEnable {
				return DeviceCommunicationControlRequest{}, fmt.Errorf("%w: duplicate enableDisable", bacnet.ErrMalformed)
			}
			u, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return DeviceCommunicationControlRequest{}, err
			}
			if u > 2 {
				return DeviceCommunicationControlRequest{}, fmt.Errorf("%w: enableDisable out of range", bacnet.ErrMalformed)
			}
			req.EnableDisable = EnableDisable(u)
			haveEnable = true
		case el.TagNumber == 2 && !bacnet.IsContextConstructed(el):
			cs, err := bacnet.ContextCharacterString(el)
			if err != nil {
				return DeviceCommunicationControlRequest{}, err
			}
			if len(cs.Value) > 20 {
				return DeviceCommunicationControlRequest{}, fmt.Errorf("%w: password longer than 20", bacnet.ErrMalformed)
			}
			req.Password = &cs
		default:
			return DeviceCommunicationControlRequest{}, fmt.Errorf("%w: unexpected DeviceCommunicationControl tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
	}
	if !haveEnable {
		return DeviceCommunicationControlRequest{}, fmt.Errorf("%w: DeviceCommunicationControl missing enableDisable", bacnet.ErrMalformed)
	}
	return req, nil
}

const ServiceDeviceCommunicationControl = apdu.ServiceDeviceCommunicationControl
