// SPDX-License-Identifier: MIT

// Package apdu implements BACnet Application Protocol Data Unit framing.
//
// apdu understands PDU structure, service choice and raw service payload only.
// It does not decode ReadProperty, Who-Is or COV payload structure.
//
// apdu is a sibling wire codec: it may import github.com/otfabric/go-bacnet
// but must not import bvlc or npdu. Payload may alias the input buffer.
package apdu

import (
	"fmt"

	"github.com/otfabric/go-bacnet"
)

// PDUType values in the APDU header high nibble / bits.
type PDUType uint8

const (
	TypeConfirmedRequest   PDUType = 0x00
	TypeUnconfirmedRequest PDUType = 0x10
	TypeSimpleACK          PDUType = 0x20
	TypeComplexACK         PDUType = 0x30
	TypeSegmentACK         PDUType = 0x40
	TypeError              PDUType = 0x50
	TypeReject             PDUType = 0x60
	TypeAbort              PDUType = 0x70
)

// Confirmed service choices (subset).
const (
	ServiceSubscribeCOV         = 5
	ServiceReadProperty         = 12
	ServiceReadPropertyMultiple = 14
	ServiceWriteProperty        = 15
	ServiceSubscribeCOVProperty = 28
)

// Unconfirmed service choices (subset).
const (
	ServiceIAm            = 0
	ServiceIHave          = 1
	ServiceUnconfirmedCOV = 2
	ServiceWhoIs          = 8
)

// ConfirmedRequest is a confirmed request APDU with raw service payload.
type ConfirmedRequest struct {
	SegmentedMessage          bool
	MoreFollows               bool
	SegmentedResponseAccepted bool
	MaxSegments               uint8
	MaxAPDU                   uint8
	InvokeID                  uint8
	SequenceNumber            uint8
	ProposedWindowSize        uint8
	ServiceChoice             uint8
	Payload                   []byte
}

// UnconfirmedRequest is an unconfirmed request APDU.
type UnconfirmedRequest struct {
	ServiceChoice uint8
	Payload       []byte
}

// SimpleACK is a simple acknowledgment.
type SimpleACK struct {
	InvokeID      uint8
	ServiceChoice uint8
}

// ComplexACK is a complex acknowledgment with raw service payload.
type ComplexACK struct {
	SegmentedMessage   bool
	MoreFollows        bool
	InvokeID           uint8
	SequenceNumber     uint8
	ProposedWindowSize uint8
	ServiceChoice      uint8
	Payload            []byte
}

// SegmentACK acknowledges segments.
type SegmentACK struct {
	NegativeACK      bool
	Server           bool
	InvokeID         uint8
	SequenceNumber   uint8
	ActualWindowSize uint8
}

// ErrorPDU is an Error APDU. Payload is the remaining encoded Error class/code
// and optional service data (typically two enumerated application tags).
type ErrorPDU struct {
	InvokeID      uint8
	ServiceChoice uint8
	Payload       []byte
}

// RejectPDU is a Reject APDU.
type RejectPDU struct {
	InvokeID uint8
	Reason   uint8
}

// AbortPDU is an Abort APDU.
type AbortPDU struct {
	Server   bool
	InvokeID uint8
	Reason   uint8
}

// PDU is a discriminated APDU.
type PDU struct {
	Type               PDUType
	ConfirmedRequest   *ConfirmedRequest
	UnconfirmedRequest *UnconfirmedRequest
	SimpleACK          *SimpleACK
	ComplexACK         *ComplexACK
	SegmentACK         *SegmentACK
	Error              *ErrorPDU
	Reject             *RejectPDU
	Abort              *AbortPDU
}

// Parse parses one APDU from data. Expects exact consumption of data.
func Parse(data []byte, limits bacnet.DecodeLimits) (PDU, error) {
	limits = limits.Normalize()
	if len(data) < 1 {
		return PDU{}, fmt.Errorf("%w: empty APDU", bacnet.ErrMalformed)
	}
	if len(data) > limits.MaxAPDUSize {
		return PDU{}, fmt.Errorf("%w: APDU size", bacnet.ErrLimitExceeded)
	}
	pduType := PDUType(data[0] & 0xF0)
	switch pduType {
	case TypeConfirmedRequest:
		return parseConfirmed(data)
	case TypeUnconfirmedRequest:
		return parseUnconfirmed(data)
	case TypeSimpleACK:
		return parseSimpleACK(data)
	case TypeComplexACK:
		return parseComplexACK(data)
	case TypeSegmentACK:
		return parseSegmentACK(data)
	case TypeError:
		return parseError(data)
	case TypeReject:
		return parseReject(data)
	case TypeAbort:
		return parseAbort(data)
	default:
		return PDU{}, fmt.Errorf("%w: APDU type 0x%02x", bacnet.ErrUnsupported, pduType)
	}
}

func parseConfirmed(data []byte) (PDU, error) {
	if len(data) < 4 {
		return PDU{}, fmt.Errorf("%w: confirmed request truncated", bacnet.ErrMalformed)
	}
	b0 := data[0]
	req := &ConfirmedRequest{
		SegmentedMessage:          b0&0x08 != 0,
		MoreFollows:               b0&0x04 != 0,
		SegmentedResponseAccepted: b0&0x02 != 0,
		MaxSegments:               (data[1] >> 4) & 0x07,
		MaxAPDU:                   data[1] & 0x0F,
		InvokeID:                  data[2],
	}
	off := 3
	if req.SegmentedMessage {
		if len(data) < 6 {
			return PDU{}, fmt.Errorf("%w: segmented confirmed truncated", bacnet.ErrMalformed)
		}
		req.SequenceNumber = data[3]
		req.ProposedWindowSize = data[4]
		off = 5
	}
	if off >= len(data) {
		return PDU{}, fmt.Errorf("%w: missing service choice", bacnet.ErrMalformed)
	}
	req.ServiceChoice = data[off]
	req.Payload = data[off+1:]
	return PDU{Type: TypeConfirmedRequest, ConfirmedRequest: req}, nil
}

func parseUnconfirmed(data []byte) (PDU, error) {
	if len(data) < 2 {
		return PDU{}, fmt.Errorf("%w: unconfirmed truncated", bacnet.ErrMalformed)
	}
	return PDU{
		Type: TypeUnconfirmedRequest,
		UnconfirmedRequest: &UnconfirmedRequest{
			ServiceChoice: data[1],
			Payload:       data[2:],
		},
	}, nil
}

func parseSimpleACK(data []byte) (PDU, error) {
	if len(data) != 3 {
		return PDU{}, fmt.Errorf("%w: simple ACK length", bacnet.ErrMalformed)
	}
	return PDU{
		Type: TypeSimpleACK,
		SimpleACK: &SimpleACK{
			InvokeID:      data[1],
			ServiceChoice: data[2],
		},
	}, nil
}

func parseComplexACK(data []byte) (PDU, error) {
	if len(data) < 3 {
		return PDU{}, fmt.Errorf("%w: complex ACK truncated", bacnet.ErrMalformed)
	}
	b0 := data[0]
	ack := &ComplexACK{
		SegmentedMessage: b0&0x08 != 0,
		MoreFollows:      b0&0x04 != 0,
		InvokeID:         data[1],
	}
	off := 2
	if ack.SegmentedMessage {
		if len(data) < 5 {
			return PDU{}, fmt.Errorf("%w: segmented complex ACK truncated", bacnet.ErrMalformed)
		}
		ack.SequenceNumber = data[2]
		ack.ProposedWindowSize = data[3]
		off = 4
	}
	if off >= len(data) {
		return PDU{}, fmt.Errorf("%w: missing service choice", bacnet.ErrMalformed)
	}
	ack.ServiceChoice = data[off]
	ack.Payload = data[off+1:]
	return PDU{Type: TypeComplexACK, ComplexACK: ack}, nil
}

func parseSegmentACK(data []byte) (PDU, error) {
	if len(data) != 4 {
		return PDU{}, fmt.Errorf("%w: segment ACK length", bacnet.ErrMalformed)
	}
	return PDU{
		Type: TypeSegmentACK,
		SegmentACK: &SegmentACK{
			NegativeACK:      data[0]&0x02 != 0,
			Server:           data[0]&0x01 != 0,
			InvokeID:         data[1],
			SequenceNumber:   data[2],
			ActualWindowSize: data[3],
		},
	}, nil
}

func parseError(data []byte) (PDU, error) {
	if len(data) < 3 {
		return PDU{}, fmt.Errorf("%w: error PDU truncated", bacnet.ErrMalformed)
	}
	return PDU{
		Type: TypeError,
		Error: &ErrorPDU{
			InvokeID:      data[1],
			ServiceChoice: data[2],
			Payload:       data[3:],
		},
	}, nil
}

func parseReject(data []byte) (PDU, error) {
	if len(data) != 3 {
		return PDU{}, fmt.Errorf("%w: reject length", bacnet.ErrMalformed)
	}
	return PDU{Type: TypeReject, Reject: &RejectPDU{InvokeID: data[1], Reason: data[2]}}, nil
}

func parseAbort(data []byte) (PDU, error) {
	if len(data) != 3 {
		return PDU{}, fmt.Errorf("%w: abort length", bacnet.ErrMalformed)
	}
	return PDU{
		Type: TypeAbort,
		Abort: &AbortPDU{
			Server:   data[0]&0x01 != 0,
			InvokeID: data[1],
			Reason:   data[2],
		},
	}, nil
}

// AppendConfirmedRequest encodes a confirmed request.
func AppendConfirmedRequest(dst []byte, req ConfirmedRequest) []byte {
	b0 := byte(TypeConfirmedRequest)
	if req.SegmentedMessage {
		b0 |= 0x08
	}
	if req.MoreFollows {
		b0 |= 0x04
	}
	if req.SegmentedResponseAccepted {
		b0 |= 0x02
	}
	maxInfo := ((req.MaxSegments & 0x07) << 4) | (req.MaxAPDU & 0x0F)
	dst = append(dst, b0, maxInfo, req.InvokeID)
	if req.SegmentedMessage {
		dst = append(dst, req.SequenceNumber, req.ProposedWindowSize)
	}
	dst = append(dst, req.ServiceChoice)
	return append(dst, req.Payload...)
}

// AppendUnconfirmedRequest encodes an unconfirmed request.
func AppendUnconfirmedRequest(dst []byte, req UnconfirmedRequest) []byte {
	dst = append(dst, byte(TypeUnconfirmedRequest), req.ServiceChoice)
	return append(dst, req.Payload...)
}

// AppendSimpleACK encodes a simple ACK.
func AppendSimpleACK(dst []byte, ack SimpleACK) []byte {
	return append(dst, byte(TypeSimpleACK), ack.InvokeID, ack.ServiceChoice)
}

// AppendComplexACK encodes a complex ACK.
func AppendComplexACK(dst []byte, ack ComplexACK) []byte {
	b0 := byte(TypeComplexACK)
	if ack.SegmentedMessage {
		b0 |= 0x08
	}
	if ack.MoreFollows {
		b0 |= 0x04
	}
	dst = append(dst, b0, ack.InvokeID)
	if ack.SegmentedMessage {
		dst = append(dst, ack.SequenceNumber, ack.ProposedWindowSize)
	}
	dst = append(dst, ack.ServiceChoice)
	return append(dst, ack.Payload...)
}

// AppendSegmentACK encodes a SegmentACK.
func AppendSegmentACK(dst []byte, ack SegmentACK) []byte {
	b0 := byte(TypeSegmentACK)
	if ack.NegativeACK {
		b0 |= 0x02
	}
	if ack.Server {
		b0 |= 0x01
	}
	return append(dst, b0, ack.InvokeID, ack.SequenceNumber, ack.ActualWindowSize)
}

// AppendAbort encodes an Abort APDU.
func AppendAbort(dst []byte, a AbortPDU) []byte {
	b0 := byte(TypeAbort)
	if a.Server {
		b0 |= 0x01
	}
	return append(dst, b0, a.InvokeID, a.Reason)
}

// AppendReject encodes a Reject APDU.
func AppendReject(dst []byte, r RejectPDU) []byte {
	return append(dst, byte(TypeReject), r.InvokeID, r.Reason)
}

// AppendError encodes an Error APDU.
func AppendError(dst []byte, e ErrorPDU) []byte {
	dst = append(dst, byte(TypeError), e.InvokeID, e.ServiceChoice)
	return append(dst, e.Payload...)
}

// DecodeErrorClassCode parses a BACnet-Error (context [0]/[1] or application ENUMERATED pair).
func DecodeErrorClassCode(payload []byte, limits bacnet.DecodeLimits) (class, code uint16, err error) {
	elements, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return 0, 0, err
	}
	if n != len(payload) {
		return 0, 0, fmt.Errorf("%w: error payload trailing data", bacnet.ErrTrailingData)
	}
	return bacnet.DecodeBACnetError(elements)
}
