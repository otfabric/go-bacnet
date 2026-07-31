// SPDX-License-Identifier: MIT

// Package npdu implements BACnet Network Layer Protocol Data Units.
//
// npdu is a sibling wire codec: it may import github.com/otfabric/go-bacnet
// but must not import bvlc or apdu. APDU payload may alias the input buffer.
package npdu

import (
	"fmt"

	"github.com/otfabric/go-bacnet"
)

const Version1 = 1

// Control bits.
const (
	ControlNetworkMessage  = 0x80
	ControlDestinationSpec = 0x20
	ControlSourceSpec      = 0x08
	ControlExpectingReply  = 0x04
	ControlPriorityMask    = 0x03
)

// NetworkMessage types used in Horizon 1.
const (
	NetMsgWhoIsRouterToNetwork     = 0x00
	NetMsgIAmRouterToNetwork       = 0x01
	NetMsgICouldBeRouterToNetwork  = 0x02
	NetMsgRejectMessageToNetwork   = 0x03
	NetMsgRouterAvailableToNetwork = 0x04
	NetMsgRouterBusyToNetwork      = 0x05
	NetMsgRouterNetworkNumberIs    = 0x13
)

// NPDU is a parsed network PDU. APDU aliases input when present.
type NPDU struct {
	Version        uint8
	Control        uint8
	Destination    bacnet.Address
	Source         bacnet.Address
	HopCount       uint8
	NetworkMessage bool
	NetMsgType     uint8
	NetMsgData     []byte
	APDU           []byte
	ExpectingReply bool
	Priority       uint8
}

// Parse parses an NPDU. APDU/NetMsgData may alias data.
func Parse(data []byte, limits bacnet.DecodeLimits) (NPDU, int, error) {
	limits = limits.Normalize()
	if len(data) > limits.MaxDatagramSize {
		return NPDU{}, 0, fmt.Errorf("%w: NPDU size", bacnet.ErrLimitExceeded)
	}
	if len(data) < 2 {
		return NPDU{}, 0, fmt.Errorf("%w: NPDU truncated", bacnet.ErrMalformed)
	}
	n := NPDU{
		Version: data[0],
		Control: data[1],
	}
	if n.Version != Version1 {
		return NPDU{}, 0, fmt.Errorf("%w: NPDU version %d", bacnet.ErrUnsupported, n.Version)
	}
	n.NetworkMessage = n.Control&ControlNetworkMessage != 0
	n.ExpectingReply = n.Control&ControlExpectingReply != 0
	n.Priority = n.Control & ControlPriorityMask
	off := 2

	hasDst := n.Control&ControlDestinationSpec != 0
	hasSrc := n.Control&ControlSourceSpec != 0

	if hasDst {
		if off+2 > len(data) {
			return NPDU{}, 0, fmt.Errorf("%w: DNET truncated", bacnet.ErrMalformed)
		}
		dnet := uint16(data[off])<<8 | uint16(data[off+1])
		off += 2
		if off >= len(data) {
			return NPDU{}, 0, fmt.Errorf("%w: DLEN truncated", bacnet.ErrMalformed)
		}
		dlen := int(data[off])
		off++
		if dlen > bacnet.MaxMACLength {
			return NPDU{}, 0, fmt.Errorf("%w: DLEN %d", bacnet.ErrMalformed, dlen)
		}
		if off+dlen > len(data) {
			return NPDU{}, 0, fmt.Errorf("%w: DADR truncated", bacnet.ErrMalformed)
		}
		mac, err := bacnet.NewMAC(data[off : off+dlen])
		if err != nil {
			return NPDU{}, 0, err
		}
		off += dlen
		switch {
		case dnet == 0xFFFF:
			n.Destination = bacnet.GlobalBroadcast()
		case dlen == 0:
			n.Destination = bacnet.RemoteBroadcast(dnet)
		default:
			n.Destination = bacnet.RemoteStation(dnet, mac)
		}
		if off >= len(data) {
			return NPDU{}, 0, fmt.Errorf("%w: hop count truncated", bacnet.ErrMalformed)
		}
		n.HopCount = data[off]
		off++
	}

	if hasSrc {
		if off+2 > len(data) {
			return NPDU{}, 0, fmt.Errorf("%w: SNET truncated", bacnet.ErrMalformed)
		}
		snet := uint16(data[off])<<8 | uint16(data[off+1])
		off += 2
		if off >= len(data) {
			return NPDU{}, 0, fmt.Errorf("%w: SLEN truncated", bacnet.ErrMalformed)
		}
		slen := int(data[off])
		off++
		if slen == 0 || slen > bacnet.MaxMACLength {
			return NPDU{}, 0, fmt.Errorf("%w: SLEN %d", bacnet.ErrMalformed, slen)
		}
		if off+slen > len(data) {
			return NPDU{}, 0, fmt.Errorf("%w: SADR truncated", bacnet.ErrMalformed)
		}
		mac, err := bacnet.NewMAC(data[off : off+slen])
		if err != nil {
			return NPDU{}, 0, err
		}
		off += slen
		if snet == 0 {
			n.Source = bacnet.LocalStation(mac)
		} else {
			n.Source = bacnet.RemoteStation(snet, mac)
		}
	}

	if n.NetworkMessage {
		if off >= len(data) {
			return NPDU{}, 0, fmt.Errorf("%w: network message type truncated", bacnet.ErrMalformed)
		}
		n.NetMsgType = data[off]
		off++
		n.NetMsgData = data[off:]
		return n, len(data), nil
	}
	n.APDU = data[off:]
	return n, len(data), nil
}

// Append encodes an NPDU. For application messages, set APDU; for network
// messages set NetworkMessage, NetMsgType and NetMsgData.
func Append(dst []byte, n NPDU) ([]byte, error) {
	if n.Version == 0 {
		n.Version = Version1
	}
	control := n.Control
	if n.NetworkMessage {
		control |= ControlNetworkMessage
	} else {
		control &^= ControlNetworkMessage
	}
	if n.ExpectingReply {
		control |= ControlExpectingReply
	}
	control = (control &^ ControlPriorityMask) | (n.Priority & ControlPriorityMask)

	hasDst := false
	hasSrc := false
	switch n.Destination.Scope() {
	case bacnet.AddressRemoteStation, bacnet.AddressRemoteBroadcast, bacnet.AddressGlobalBroadcast:
		hasDst = true
		control |= ControlDestinationSpec
	default:
		control &^= ControlDestinationSpec
	}
	switch n.Source.Scope() {
	case bacnet.AddressLocalStation, bacnet.AddressRemoteStation:
		if !n.Source.MAC().IsZero() {
			hasSrc = true
			control |= ControlSourceSpec
		}
	default:
		control &^= ControlSourceSpec
	}

	dst = append(dst, n.Version, control)

	if hasDst {
		switch n.Destination.Scope() {
		case bacnet.AddressGlobalBroadcast:
			dst = append(dst, 0xFF, 0xFF, 0) // DNET=0xFFFF, DLEN=0
		case bacnet.AddressRemoteBroadcast:
			dnet := n.Destination.Network()
			dst = append(dst, byte(dnet>>8), byte(dnet), 0)
		case bacnet.AddressRemoteStation:
			dnet := n.Destination.Network()
			mac := n.Destination.MAC().Bytes()
			dst = append(dst, byte(dnet>>8), byte(dnet), byte(len(mac)))
			dst = append(dst, mac...)
		case bacnet.AddressLocalStation, bacnet.AddressLocalBroadcast:
			// Local destinations do not encode DNET/DADR; hasDst should be false.
		default:
			return dst, fmt.Errorf("%w: unsupported destination scope %d", bacnet.ErrUnsupported, n.Destination.Scope())
		}
		hop := n.HopCount
		if hop == 0 {
			hop = 255
		}
		dst = append(dst, hop)
	}

	if hasSrc {
		mac := n.Source.MAC().Bytes()
		snet := n.Source.Network()
		if n.Source.Scope() == bacnet.AddressLocalStation {
			snet = 0
		}
		dst = append(dst, byte(snet>>8), byte(snet), byte(len(mac)))
		dst = append(dst, mac...)
	}

	if n.NetworkMessage {
		dst = append(dst, n.NetMsgType)
		dst = append(dst, n.NetMsgData...)
		return dst, nil
	}
	return append(dst, n.APDU...), nil
}
