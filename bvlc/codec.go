// SPDX-License-Identifier: MIT

// Package bvlc implements BACnet Virtual Link Control (BACnet/IP) framing.
//
// bvlc is a sibling wire codec: it may import github.com/otfabric/go-bacnet
// but must not import npdu or apdu. Payload bytes may alias the input datagram.
package bvlc

import (
	"encoding/binary"
	"fmt"
	"net/netip"

	"github.com/otfabric/go-bacnet"
)

// TypeBACnetIP is the BVLC type for BACnet/IP.
const TypeBACnetIP = 0x81

// Function identifies a BVLC function.
type Function uint8

const (
	FunctionResult                            Function = 0x00
	FunctionWriteBroadcastDistributionTable   Function = 0x01
	FunctionReadBroadcastDistributionTable    Function = 0x02
	FunctionReadBroadcastDistributionTableAck Function = 0x03
	FunctionForwardedNPDU                     Function = 0x04
	FunctionRegisterForeignDevice             Function = 0x05
	FunctionReadForeignDeviceTable            Function = 0x06
	FunctionReadForeignDeviceTableAck         Function = 0x07
	FunctionDeleteForeignDeviceTableEntry     Function = 0x08
	FunctionDistributeBroadcastToNetwork      Function = 0x09
	FunctionOriginalUnicastNPDU               Function = 0x0A
	FunctionOriginalBroadcastNPDU             Function = 0x0B
)

// Message is a parsed BVLC message. Payload aliases the input buffer.
type Message struct {
	Function Function
	Payload  []byte

	// ForwardedNPDU origin (set when Function == FunctionForwardedNPDU).
	OriginIP   [4]byte
	OriginPort uint16

	// Result code (set when Function == FunctionResult).
	ResultCode uint16

	// Register-Foreign-Device TTL (seconds).
	TTL uint16
}

// Parse parses a complete BVLC datagram. Declared length must match len(data).
func Parse(data []byte, limits bacnet.DecodeLimits) (Message, error) {
	limits = limits.Normalize()
	if len(data) < 4 {
		return Message{}, fmt.Errorf("%w: BVLC truncated", bacnet.ErrMalformed)
	}
	if len(data) > limits.MaxDatagramSize {
		return Message{}, fmt.Errorf("%w: datagram size", bacnet.ErrLimitExceeded)
	}
	if data[0] != TypeBACnetIP {
		return Message{}, fmt.Errorf("%w: BVLC type 0x%02x", bacnet.ErrMalformed, data[0])
	}
	fn := Function(data[1])
	declared := int(binary.BigEndian.Uint16(data[2:4]))
	if declared != len(data) {
		return Message{}, fmt.Errorf("%w: BVLC length %d != datagram %d", bacnet.ErrMalformed, declared, len(data))
	}
	body := data[4:]
	switch fn {
	case FunctionResult:
		if len(body) != 2 {
			return Message{}, fmt.Errorf("%w: BVLC-Result length", bacnet.ErrMalformed)
		}
		return Message{Function: fn, ResultCode: binary.BigEndian.Uint16(body)}, nil
	case FunctionOriginalUnicastNPDU, FunctionOriginalBroadcastNPDU, FunctionDistributeBroadcastToNetwork:
		return Message{Function: fn, Payload: body}, nil
	case FunctionForwardedNPDU:
		if len(body) < 6 {
			return Message{}, fmt.Errorf("%w: Forwarded-NPDU truncated", bacnet.ErrMalformed)
		}
		var origin [4]byte
		copy(origin[:], body[:4])
		port := binary.BigEndian.Uint16(body[4:6])
		return Message{
			Function:   fn,
			OriginIP:   origin,
			OriginPort: port,
			Payload:    body[6:],
		}, nil
	case FunctionRegisterForeignDevice:
		if len(body) != 2 {
			return Message{}, fmt.Errorf("%w: Register-Foreign-Device length", bacnet.ErrMalformed)
		}
		return Message{Function: fn, TTL: binary.BigEndian.Uint16(body)}, nil
	case FunctionWriteBroadcastDistributionTable,
		FunctionReadBroadcastDistributionTable,
		FunctionReadBroadcastDistributionTableAck,
		FunctionReadForeignDeviceTable,
		FunctionReadForeignDeviceTableAck,
		FunctionDeleteForeignDeviceTableEntry:
		return Message{Function: fn, Payload: body}, nil
	default:
		return Message{Function: fn, Payload: body}, nil
	}
}

// Append encodes a BVLC message. For ForwardedNPDU, OriginIP/OriginPort are used.
func Append(dst []byte, msg Message) ([]byte, error) {
	start := len(dst)
	dst = append(dst, TypeBACnetIP, byte(msg.Function), 0, 0) // length filled later
	switch msg.Function {
	case FunctionResult:
		dst = append(dst, byte(msg.ResultCode>>8), byte(msg.ResultCode))
	case FunctionRegisterForeignDevice:
		dst = append(dst, byte(msg.TTL>>8), byte(msg.TTL))
	case FunctionForwardedNPDU:
		dst = append(dst, msg.OriginIP[:]...)
		dst = append(dst, byte(msg.OriginPort>>8), byte(msg.OriginPort))
		dst = append(dst, msg.Payload...)
	default:
		dst = append(dst, msg.Payload...)
	}
	total := len(dst) - start
	if total > 0xFFFF {
		return dst, fmt.Errorf("%w: BVLC too large", bacnet.ErrMalformed)
	}
	binary.BigEndian.PutUint16(dst[start+2:start+4], uint16(total))
	return dst, nil
}

// OriginAddrPort returns the forwarded origin as AddrPort when present.
func (m Message) OriginAddrPort() (netip.AddrPort, bool) {
	if m.Function != FunctionForwardedNPDU {
		return netip.AddrPort{}, false
	}
	addr := netip.AddrFrom4(m.OriginIP)
	return netip.AddrPortFrom(addr, m.OriginPort), true
}
