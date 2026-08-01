// SPDX-License-Identifier: MIT

package service

import (
	"fmt"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
)

// ReinitializedStateOfDevice enumerates ReinitializeDevice states.
type ReinitializedStateOfDevice uint8

const (
	ReinitializedColdstart       ReinitializedStateOfDevice = 0
	ReinitializedWarmstart       ReinitializedStateOfDevice = 1
	ReinitializedStartBackup     ReinitializedStateOfDevice = 2
	ReinitializedEndBackup       ReinitializedStateOfDevice = 3
	ReinitializedStartRestore    ReinitializedStateOfDevice = 4
	ReinitializedEndRestore      ReinitializedStateOfDevice = 5
	ReinitializedAbortRestore    ReinitializedStateOfDevice = 6
	ReinitializedActivateChanges ReinitializedStateOfDevice = 7
)

// ReinitializeDeviceRequest is a ReinitializeDevice payload.
type ReinitializeDeviceRequest struct {
	State    ReinitializedStateOfDevice
	Password *bacnet.CharacterString // optional; max 20 characters
}

// EncodeReinitializeDevice encodes a ReinitializeDevice request payload.
func EncodeReinitializeDevice(req ReinitializeDeviceRequest) ([]byte, error) {
	if req.State > ReinitializedActivateChanges {
		return nil, fmt.Errorf("%w: ReinitializeDevice state out of range", bacnet.ErrMalformed)
	}
	if req.Password != nil && len(req.Password.Value) > 20 {
		return nil, fmt.Errorf("%w: ReinitializeDevice password longer than 20", bacnet.ErrMalformed)
	}
	dst, err := bacnet.AppendContextUnsigned(nil, 0, uint64(req.State))
	if err != nil {
		return nil, err
	}
	if req.Password != nil {
		dst, err = bacnet.AppendContextCharacterString(dst, 1, *req.Password)
	}
	return dst, err
}

// DecodeReinitializeDevice decodes a ReinitializeDevice request payload.
func DecodeReinitializeDevice(payload []byte, limits bacnet.DecodeLimits) (ReinitializeDeviceRequest, error) {
	elements, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return ReinitializeDeviceRequest{}, err
	}
	if n != len(payload) {
		return ReinitializeDeviceRequest{}, fmt.Errorf("%w: ReinitializeDevice trailing data", bacnet.ErrTrailingData)
	}
	var req ReinitializeDeviceRequest
	var haveState bool
	for _, el := range elements {
		switch {
		case el.TagNumber == 0 && !bacnet.IsContextConstructed(el):
			if haveState {
				return ReinitializeDeviceRequest{}, fmt.Errorf("%w: duplicate reinitializedStateOfDevice", bacnet.ErrMalformed)
			}
			u, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return ReinitializeDeviceRequest{}, err
			}
			if u > 7 {
				return ReinitializeDeviceRequest{}, fmt.Errorf("%w: reinitializedStateOfDevice out of range", bacnet.ErrMalformed)
			}
			req.State = ReinitializedStateOfDevice(u)
			haveState = true
		case el.TagNumber == 1 && !bacnet.IsContextConstructed(el):
			cs, err := bacnet.ContextCharacterString(el)
			if err != nil {
				return ReinitializeDeviceRequest{}, err
			}
			if len(cs.Value) > 20 {
				return ReinitializeDeviceRequest{}, fmt.Errorf("%w: password longer than 20", bacnet.ErrMalformed)
			}
			req.Password = &cs
		default:
			return ReinitializeDeviceRequest{}, fmt.Errorf("%w: unexpected ReinitializeDevice tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
	}
	if !haveState {
		return ReinitializeDeviceRequest{}, fmt.Errorf("%w: ReinitializeDevice missing state", bacnet.ErrMalformed)
	}
	return req, nil
}

const ServiceReinitializeDevice = apdu.ServiceReinitializeDevice
