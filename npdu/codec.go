// SPDX-License-Identifier: MIT

// Package npdu implements BACnet Network Layer Protocol Data Units.
//
// Strict BACnet/IP uses a framing decoder: reserved control bits, global
// broadcast with non-zero DADR, and odd-length router network lists are
// rejected as ErrMalformed.
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
	// ControlReservedBits must be zero on the wire (ASHRAE 135).
	ControlReservedBits = 0x40 | 0x10
)

// Network message types (ASHRAE 135).
const (
	NetMsgWhoIsRouterToNetwork          = 0x00
	NetMsgIAmRouterToNetwork            = 0x01
	NetMsgICouldBeRouterToNetwork       = 0x02
	NetMsgRejectMessageToNetwork        = 0x03
	NetMsgRouterAvailableToNetwork      = 0x04
	NetMsgRouterBusyToNetwork           = 0x05
	NetMsgInitializeRoutingTable        = 0x06
	NetMsgInitializeRoutingTableAck     = 0x07
	NetMsgEstablishConnectionToNetwork  = 0x08
	NetMsgDisconnectConnectionToNetwork = 0x09
	NetMsgChallengeRequest              = 0x0A
	NetMsgSecurityPayload               = 0x0B
	NetMsgSecurityResponse              = 0x0C
	NetMsgRequestKeyUpdate              = 0x0D
	NetMsgUpdateKeySet                  = 0x0E
	NetMsgUpdateDistributionKey         = 0x0F
	NetMsgRequestMasterKey              = 0x10
	NetMsgSetMasterKey                  = 0x11
	NetMsgWhatIsNetworkNumber           = 0x12
	NetMsgNetworkNumberIs               = 0x13
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
	if n.Control&ControlReservedBits != 0 {
		return NPDU{}, 0, fmt.Errorf("%w: NPDU reserved control bits set", bacnet.ErrMalformed)
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
			if dlen != 0 {
				return NPDU{}, 0, fmt.Errorf("%w: global broadcast with non-zero DLEN", bacnet.ErrMalformed)
			}
			n.Destination = bacnet.GlobalBroadcast()
		case dlen == 0:
			if dnet == 0 {
				return NPDU{}, 0, fmt.Errorf("%w: remote broadcast DNET 0", bacnet.ErrMalformed)
			}
			n.Destination = bacnet.RemoteBroadcast(dnet)
		default:
			if dnet == 0 {
				return NPDU{}, 0, fmt.Errorf("%w: remote station DNET 0", bacnet.ErrMalformed)
			}
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
		if err := validateNetworkMessageData(n.NetMsgType, n.NetMsgData); err != nil {
			return NPDU{}, 0, err
		}
		return n, len(data), nil
	}
	n.APDU = data[off:]
	return n, len(data), nil
}

func validateNetworkMessageData(msgType uint8, data []byte) error {
	switch msgType {
	case NetMsgIAmRouterToNetwork, NetMsgRouterAvailableToNetwork, NetMsgRouterBusyToNetwork:
		if _, err := DecodeNetworkList(data); err != nil {
			return err
		}
	case NetMsgRejectMessageToNetwork:
		if _, _, err := DecodeRejectMessageToNetwork(data); err != nil {
			return err
		}
	case NetMsgICouldBeRouterToNetwork:
		if _, _, err := DecodeICouldBeRouterToNetwork(data); err != nil {
			return err
		}
	case NetMsgNetworkNumberIs:
		if len(data) < 2 {
			return fmt.Errorf("%w: Network-Number-Is truncated", bacnet.ErrMalformed)
		}
	}
	return nil
}

// DecodeNetworkList decodes a sequence of 16-bit BACnet network numbers.
// Length must be even; network number 0 is rejected.
func DecodeNetworkList(data []byte) ([]uint16, error) {
	if len(data)%2 != 0 {
		return nil, fmt.Errorf("%w: odd network list length", bacnet.ErrMalformed)
	}
	out := make([]uint16, 0, len(data)/2)
	for i := 0; i+1 < len(data); i += 2 {
		netn := uint16(data[i])<<8 | uint16(data[i+1])
		if netn == 0 {
			return nil, fmt.Errorf("%w: network number 0 in list", bacnet.ErrMalformed)
		}
		out = append(out, netn)
	}
	return out, nil
}

// DecodeRejectMessageToNetwork returns reason and DNET from Reject-Message-To-Network.
func DecodeRejectMessageToNetwork(data []byte) (reason uint8, network uint16, err error) {
	if len(data) < 3 {
		return 0, 0, fmt.Errorf("%w: Reject-Message-To-Network truncated", bacnet.ErrMalformed)
	}
	if len(data) > 3 {
		return 0, 0, fmt.Errorf("%w: Reject-Message-To-Network trailing", bacnet.ErrTrailingData)
	}
	return data[0], uint16(data[1])<<8 | uint16(data[2]), nil
}

// DecodeICouldBeRouterToNetwork returns DNET and performance index.
func DecodeICouldBeRouterToNetwork(data []byte) (network uint16, perf uint8, err error) {
	if len(data) < 3 {
		return 0, 0, fmt.Errorf("%w: I-Could-Be-Router-To-Network truncated", bacnet.ErrMalformed)
	}
	if len(data) > 3 {
		return 0, 0, fmt.Errorf("%w: I-Could-Be-Router-To-Network trailing", bacnet.ErrTrailingData)
	}
	return uint16(data[0])<<8 | uint16(data[1]), data[2], nil
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
