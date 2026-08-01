// SPDX-License-Identifier: MIT

package service

import (
	"fmt"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
)

// AcknowledgeAlarmRequest is an AcknowledgeAlarm payload.
type AcknowledgeAlarmRequest struct {
	ProcessIdentifier    uint32
	EventObject          bacnet.ObjectIdentifier
	EventStateAcked      uint32
	TimeStamp            TimeStamp
	AcknowledgmentSource bacnet.CharacterString
	TimeOfAcknowledgment TimeStamp
}

// EncodeAcknowledgeAlarm encodes an AcknowledgeAlarm request.
func EncodeAcknowledgeAlarm(req AcknowledgeAlarmRequest) ([]byte, error) {
	var dst []byte
	var err error
	dst, err = bacnet.AppendContextUnsigned(dst, 0, uint64(req.ProcessIdentifier))
	if err != nil {
		return nil, err
	}
	dst, err = bacnet.AppendContextObjectID(dst, 1, req.EventObject)
	if err != nil {
		return nil, err
	}
	dst, err = bacnet.AppendContextUnsigned(dst, 2, uint64(req.EventStateAcked))
	if err != nil {
		return nil, err
	}
	dst, err = appendTimeStampField(dst, 3, req.TimeStamp)
	if err != nil {
		return nil, err
	}
	dst, err = bacnet.AppendContextCharacterString(dst, 4, req.AcknowledgmentSource)
	if err != nil {
		return nil, err
	}
	dst, err = appendTimeStampField(dst, 5, req.TimeOfAcknowledgment)
	return dst, err
}

// DecodeAcknowledgeAlarm decodes an AcknowledgeAlarm request.
func DecodeAcknowledgeAlarm(payload []byte, limits bacnet.DecodeLimits) (AcknowledgeAlarmRequest, error) {
	elements, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return AcknowledgeAlarmRequest{}, err
	}
	if n != len(payload) {
		return AcknowledgeAlarmRequest{}, fmt.Errorf("%w: AcknowledgeAlarm trailing data", bacnet.ErrTrailingData)
	}
	var req AcknowledgeAlarmRequest
	var havePID, haveObj, haveState, haveTS, haveSrc, haveAckTS bool
	for _, el := range elements {
		switch {
		case el.TagNumber == 0 && !bacnet.IsContextConstructed(el):
			u, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return AcknowledgeAlarmRequest{}, err
			}
			req.ProcessIdentifier = uint32(u)
			havePID = true
		case el.TagNumber == 1 && !bacnet.IsContextConstructed(el):
			req.EventObject, err = bacnet.ContextObjectID(el)
			haveObj = true
		case el.TagNumber == 2 && !bacnet.IsContextConstructed(el):
			u, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return AcknowledgeAlarmRequest{}, err
			}
			req.EventStateAcked = uint32(u)
			haveState = true
		case el.TagNumber == 3 && bacnet.IsContextConstructed(el):
			if len(el.Value.Elements) != 1 {
				return AcknowledgeAlarmRequest{}, fmt.Errorf("%w: TimeStamp wrapper", bacnet.ErrMalformed)
			}
			req.TimeStamp, err = DecodeTimeStamp(el.Value.Elements[0])
			haveTS = true
		case el.TagNumber == 4 && !bacnet.IsContextConstructed(el):
			req.AcknowledgmentSource, err = bacnet.ContextCharacterString(el)
			haveSrc = true
		case el.TagNumber == 5 && bacnet.IsContextConstructed(el):
			if len(el.Value.Elements) != 1 {
				return AcknowledgeAlarmRequest{}, fmt.Errorf("%w: timeOfAcknowledgment wrapper", bacnet.ErrMalformed)
			}
			req.TimeOfAcknowledgment, err = DecodeTimeStamp(el.Value.Elements[0])
			haveAckTS = true
		default:
			return AcknowledgeAlarmRequest{}, fmt.Errorf("%w: unexpected AcknowledgeAlarm tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
		if err != nil {
			return AcknowledgeAlarmRequest{}, err
		}
	}
	if !havePID || !haveObj || !haveState || !haveTS || !haveSrc || !haveAckTS {
		return AcknowledgeAlarmRequest{}, fmt.Errorf("%w: AcknowledgeAlarm missing required fields", bacnet.ErrMalformed)
	}
	return req, nil
}

const ServiceAcknowledgeAlarm = apdu.ServiceAcknowledgeAlarm
