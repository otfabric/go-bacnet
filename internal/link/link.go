// SPDX-License-Identifier: MIT

// Package link holds the BACnet/IP (Annex J) to network layer (clause 6)
// steps that the client and the server compose the same way: the origin of
// a Forwarded-NPDU, the six-octet B/IP MAC of an endpoint and back, and the
// NPDU and BVLC framing of an outbound APDU.
//
// Which BVLC functions are accepted, from whom, and what a network-layer
// message or a malformed APDU leads to differ by role and stay in the
// client and the server. Decoding itself is bvlc.Parse, npdu.Parse and
// apdu.Parse.
package link

import (
	"encoding/binary"
	"net/netip"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/npdu"
)

// MACLen is the length of a B/IP MAC address: an IPv4 address followed by
// a UDP port, both in network byte order (Annex J).
const MACLen = 6

// Origin is the B/IP node that originated msg: the original source of a
// Forwarded-NPDU (Annex J.2), else immediate, the node that sent the
// datagram.
func Origin(msg bvlc.Message, immediate bip.Endpoint) bip.Endpoint {
	if ap, ok := msg.OriginAddrPort(); ok {
		return bip.NewEndpoint(ap)
	}
	return immediate
}

// MAC is the B/IP MAC address of ep. It reports false for an endpoint
// that is not a valid IPv4 address and port.
func MAC(ep bip.Endpoint) (bacnet.MAC, bool) {
	if !ep.IsValid() {
		return bacnet.MAC{}, false
	}
	ip := ep.Addr.Addr().As4()
	mac := binary.BigEndian.AppendUint16(ip[:], ep.Addr.Port())
	return bacnet.MustMAC(mac), true
}

// Endpoint is the endpoint of a B/IP MAC address. It reports false when
// mac is not six octets long.
func Endpoint(mac []byte) (bip.Endpoint, bool) {
	if len(mac) != MACLen {
		return bip.Endpoint{}, false
	}
	ip := netip.AddrFrom4([4]byte{mac[0], mac[1], mac[2], mac[3]})
	return bip.NewEndpoint(netip.AddrPortFrom(ip, binary.BigEndian.Uint16(mac[4:]))), true
}

// APDU is the NPDU that carries apdu to dest. The destination specifier
// (DNET, DADR) is present only for a remote station, a remote broadcast and
// the global broadcast; a local station or local broadcast is addressed by
// the data link alone (clause 6.2).
func APDU(dest bacnet.Address, expectingReply bool, hopCount uint8, apdu []byte) npdu.NPDU {
	n := npdu.NPDU{Version: npdu.Version1, ExpectingReply: expectingReply, HopCount: hopCount, APDU: apdu}
	switch dest.Scope() {
	case bacnet.AddressRemoteStation, bacnet.AddressRemoteBroadcast, bacnet.AddressGlobalBroadcast:
		n.Destination = dest
	case bacnet.AddressLocalStation, bacnet.AddressLocalBroadcast:
	}
	return n
}

// Frame encodes n and wraps it in a BVLC message of function fn. It
// returns the encoded NPDU as well, for a BBMD that forwards it.
func Frame(fn bvlc.Function, n npdu.NPDU) (raw, frame []byte, err error) {
	raw, err = npdu.Append(nil, n)
	if err != nil {
		return nil, nil, err
	}
	frame, err = bvlc.Append(nil, bvlc.Message{Function: fn, Payload: raw})
	if err != nil {
		return nil, nil, err
	}
	return raw, frame, nil
}
