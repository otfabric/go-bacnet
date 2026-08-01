// SPDX-License-Identifier: MIT

package service

import (
	"fmt"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
)

// GetEventInformationRequest is a GetEventInformation request.
// LastReceived nil means start of list.
type GetEventInformationRequest struct {
	LastReceived *bacnet.ObjectIdentifier
}

// EventSummary is one GetEventInformation ACK list entry (v1 subset).
//
// EventTimeStamps and EventPriorities are required constructed SEQUENCE OF
// bodies (may be empty). Ownership of those values transfers to the caller
// on decode (cloned).
type EventSummary struct {
	Object                  bacnet.ObjectIdentifier
	EventState              uint32
	AcknowledgedTransitions bacnet.BitString
	EventTimeStamps         bacnet.ApplicationValue // constructed SEQUENCE OF
	NotifyType              uint32
	EventEnable             bacnet.BitString
	EventPriorities         bacnet.ApplicationValue // constructed SEQUENCE OF
}

// GetEventInformationACK is a GetEventInformation ComplexACK.
type GetEventInformationACK struct {
	Summaries  []EventSummary
	MoreEvents bool
}

// EncodeGetEventInformation encodes a GetEventInformation request.
func EncodeGetEventInformation(req GetEventInformationRequest) ([]byte, error) {
	if req.LastReceived == nil {
		return nil, nil
	}
	return bacnet.AppendContextObjectID(nil, 0, *req.LastReceived)
}

// DecodeGetEventInformation decodes a GetEventInformation request.
func DecodeGetEventInformation(payload []byte, limits bacnet.DecodeLimits) (GetEventInformationRequest, error) {
	if len(payload) == 0 {
		return GetEventInformationRequest{}, nil
	}
	elements, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return GetEventInformationRequest{}, err
	}
	if n != len(payload) {
		return GetEventInformationRequest{}, fmt.Errorf("%w: GetEventInformation trailing data", bacnet.ErrTrailingData)
	}
	if len(elements) != 1 || elements[0].TagNumber != 0 || bacnet.IsContextConstructed(elements[0]) {
		return GetEventInformationRequest{}, fmt.Errorf("%w: GetEventInformation unexpected contents", bacnet.ErrMalformed)
	}
	id, err := bacnet.ContextObjectID(elements[0])
	if err != nil {
		return GetEventInformationRequest{}, err
	}
	return GetEventInformationRequest{LastReceived: &id}, nil
}

func encodeEventSummary(s EventSummary) ([]byte, error) {
	if s.EventTimeStamps.Kind != bacnet.ValueConstructed {
		return nil, fmt.Errorf("%w: eventTimeStamps must be constructed", bacnet.ErrMalformed)
	}
	if s.EventPriorities.Kind != bacnet.ValueConstructed {
		return nil, fmt.Errorf("%w: eventPriorities must be constructed", bacnet.ErrMalformed)
	}
	var sum []byte
	var err error
	sum, err = bacnet.AppendContextObjectID(sum, 0, s.Object)
	if err != nil {
		return nil, err
	}
	sum, err = bacnet.AppendContextUnsigned(sum, 1, uint64(s.EventState))
	if err != nil {
		return nil, err
	}
	sum, err = bacnet.AppendContextBitString(sum, 2, s.AcknowledgedTransitions)
	if err != nil {
		return nil, err
	}
	sum, err = bacnet.AppendContextTagged(sum, 3, s.EventTimeStamps.Elements)
	if err != nil {
		return nil, err
	}
	sum, err = bacnet.AppendContextUnsigned(sum, 4, uint64(s.NotifyType))
	if err != nil {
		return nil, err
	}
	sum, err = bacnet.AppendContextBitString(sum, 5, s.EventEnable)
	if err != nil {
		return nil, err
	}
	return bacnet.AppendContextTagged(sum, 6, s.EventPriorities.Elements)
}

// EncodeGetEventInformationACK encodes a GetEventInformation ACK.
func EncodeGetEventInformationACK(ack GetEventInformationACK) ([]byte, error) {
	dst := []byte{0x0E} // opening listOfEventSummaries [0]
	for _, s := range ack.Summaries {
		sum, err := encodeEventSummary(s)
		if err != nil {
			return nil, err
		}
		dst = append(dst, sum...)
	}
	dst = append(dst, 0x0F)
	return bacnet.AppendContextBool(dst, 1, ack.MoreEvents)
}

// DecodeGetEventInformationACK decodes a GetEventInformation ComplexACK.
func DecodeGetEventInformationACK(payload []byte, limits bacnet.DecodeLimits) (GetEventInformationACK, error) {
	elements, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return GetEventInformationACK{}, err
	}
	if n != len(payload) {
		return GetEventInformationACK{}, fmt.Errorf("%w: GetEventInformationACK trailing data", bacnet.ErrTrailingData)
	}
	var ack GetEventInformationACK
	var haveList, haveMore bool
	for _, el := range elements {
		switch {
		case el.TagNumber == 0 && bacnet.IsContextConstructed(el):
			if haveList {
				return GetEventInformationACK{}, fmt.Errorf("%w: duplicate listOfEventSummaries", bacnet.ErrMalformed)
			}
			summaries, err := decodeEventSummaries(el.Value.Elements)
			if err != nil {
				return GetEventInformationACK{}, err
			}
			ack.Summaries = summaries
			haveList = true
		case el.TagNumber == 1 && !bacnet.IsContextConstructed(el):
			if haveMore {
				return GetEventInformationACK{}, fmt.Errorf("%w: duplicate moreEvents", bacnet.ErrMalformed)
			}
			ack.MoreEvents, err = bacnet.ContextBool(el)
			haveMore = true
		default:
			return GetEventInformationACK{}, fmt.Errorf("%w: unexpected GetEventInformationACK tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
		if err != nil {
			return GetEventInformationACK{}, err
		}
	}
	if !haveList || !haveMore {
		return GetEventInformationACK{}, fmt.Errorf("%w: GetEventInformationACK missing fields", bacnet.ErrMalformed)
	}
	return ack, nil
}

func decodeEventSummaries(els []bacnet.Element) ([]EventSummary, error) {
	var out []EventSummary
	var cur []bacnet.Element
	flush := func() error {
		if len(cur) == 0 {
			return nil
		}
		s, err := decodeEventSummary(cur)
		if err != nil {
			return err
		}
		out = append(out, s)
		cur = nil
		return nil
	}
	for _, el := range els {
		// ObjectIdentifier [0] starts a new summary in SEQUENCE OF SEQUENCE.
		if el.TagNumber == 0 && !bacnet.IsContextConstructed(el) && len(cur) > 0 {
			if err := flush(); err != nil {
				return nil, err
			}
		}
		cur = append(cur, el)
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return out, nil
}

func decodeEventSummary(els []bacnet.Element) (EventSummary, error) {
	var s EventSummary
	var haveObj, haveState, haveAck, haveTS, haveNotify, haveEnable, havePrio bool
	for _, el := range els {
		switch {
		case el.TagNumber == 0 && !bacnet.IsContextConstructed(el):
			id, err := bacnet.ContextObjectID(el)
			if err != nil {
				return s, err
			}
			s.Object = id
			haveObj = true
		case el.TagNumber == 1 && !bacnet.IsContextConstructed(el):
			u, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return s, err
			}
			s.EventState = uint32(u)
			haveState = true
		case el.TagNumber == 2 && !bacnet.IsContextConstructed(el):
			bs, err := bacnet.ContextBitString(el)
			if err != nil {
				return s, err
			}
			s.AcknowledgedTransitions = bs
			haveAck = true
		case el.TagNumber == 3 && bacnet.IsContextConstructed(el):
			s.EventTimeStamps = bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: el.Value.Elements}.Clone()
			haveTS = true
		case el.TagNumber == 4 && !bacnet.IsContextConstructed(el):
			u, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return s, err
			}
			s.NotifyType = uint32(u)
			haveNotify = true
		case el.TagNumber == 5 && !bacnet.IsContextConstructed(el):
			bs, err := bacnet.ContextBitString(el)
			if err != nil {
				return s, err
			}
			s.EventEnable = bs
			haveEnable = true
		case el.TagNumber == 6 && bacnet.IsContextConstructed(el):
			s.EventPriorities = bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: el.Value.Elements}.Clone()
			havePrio = true
		default:
			return s, fmt.Errorf("%w: unexpected EventSummary tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
	}
	if !haveObj || !haveState || !haveAck || !haveTS || !haveNotify || !haveEnable || !havePrio {
		return s, fmt.Errorf("%w: EventSummary missing required fields", bacnet.ErrMalformed)
	}
	return s, nil
}

const ServiceGetEventInformation = apdu.ServiceGetEventInformation
