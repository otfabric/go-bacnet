// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"fmt"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/internal/clock"
	"github.com/otfabric/go-bacnet/internal/diag"
	"github.com/otfabric/go-bacnet/internal/link"
	"github.com/otfabric/go-bacnet/npdu"
)

// BBMD is a BACnet/IP broadcast management device that shares the server's
// endpoint (package bbmd implements it).
type BBMD interface {
	// HandleBVLC processes one inbound BVLC message that is not a plain
	// Original-Unicast-NPDU. It returns deliver true when the NPDU it carries
	// must also be processed by the local device, with the address of the
	// node that originated it.
	HandleBVLC(ctx context.Context, msg bvlc.Message, from bip.Endpoint, send func(context.Context, bip.OutboundPacket) error) (deliver bool, origin bip.Endpoint)
	// LocalBroadcast is called when the local device sends a local
	// broadcast, so that the BBMD forwards it to its peers and foreign
	// devices (Annex J.4.5).
	LocalBroadcast(ctx context.Context, npdu []byte, send func(context.Context, bip.OutboundPacket) error)
}

// WithBBMD makes the server also act as a BBMD.
func WithBBMD(b BBMD) Option { return func(c *config) { c.bbmd = b } }

// peer identifies where a request came from and where the reply goes.
type peer struct {
	// immediate is the B/IP node that sent the datagram (a router or BBMD
	// for remote and forwarded traffic).
	immediate bip.Endpoint
	// origin is the B/IP node that originated it (the Forwarded-NPDU
	// origin, else immediate).
	origin bip.Endpoint
	// source is the NPDU source when it carried SNET/SADR (a remote
	// station); zero for a local station.
	source bacnet.Address
	// broadcast reports that the request arrived as a broadcast.
	broadcast bool
}

// key is the transaction key of a confirmed request (clause 5.4: the
// invoke ID is unique per requesting device).
func (p peer) key(invokeID uint8) string {
	return fmt.Sprintf("%s|%s|%d", p.origin, p.source, invokeID)
}

func (s *Server) handlePacket(pkt bip.InboundPacket) {
	msg, err := bvlc.Parse(pkt.Data, s.limits)
	if err != nil {
		s.report(diag.KindMalformed, err.Error(), map[string]any{"from": pkt.ImmediatePeer.String()})
		return
	}
	p := peer{immediate: pkt.ImmediatePeer, origin: pkt.ImmediatePeer}
	switch msg.Function {
	case bvlc.FunctionOriginalUnicastNPDU:
	case bvlc.FunctionOriginalBroadcastNPDU, bvlc.FunctionForwardedNPDU, bvlc.FunctionDistributeBroadcastToNetwork:
		p.broadcast = true
		if s.cfg.bbmd != nil {
			deliver, origin := s.cfg.bbmd.HandleBVLC(s.ctx, msg, pkt.ImmediatePeer, s.sendPacket)
			if !deliver {
				return
			}
			p.origin = origin
			break
		}
		switch msg.Function {
		case bvlc.FunctionForwardedNPDU:
			p.origin = link.Origin(msg, pkt.ImmediatePeer)
		case bvlc.FunctionDistributeBroadcastToNetwork:
			s.bvlcResult(pkt.ImmediatePeer, bvlc.ResultDistributeBroadcastToNetworkNAK)
			return
		default:
		}
	case bvlc.FunctionRegisterForeignDevice, bvlc.FunctionReadBroadcastDistributionTable,
		bvlc.FunctionWriteBroadcastDistributionTable, bvlc.FunctionReadForeignDeviceTable,
		bvlc.FunctionDeleteForeignDeviceTableEntry:
		if s.cfg.bbmd != nil {
			s.cfg.bbmd.HandleBVLC(s.ctx, msg, pkt.ImmediatePeer, s.sendPacket)
			return
		}
		s.bvlcResult(pkt.ImmediatePeer, nakFor(msg.Function))
		return
	default:
		// BVLC-Result and table ACKs are answers to requests a server does
		// not send.
		return
	}
	if !p.origin.IsValid() || len(msg.Payload) == 0 {
		return
	}
	n, _, err := npdu.Parse(msg.Payload, s.limits)
	if err != nil {
		s.report(diag.KindMalformed, err.Error(), map[string]any{"from": p.origin.String()})
		return
	}
	s.learnRoutes(n, p.origin)
	if n.NetworkMessage {
		// Not a router: network-layer messages are not served; an
		// I-Am-Router-To-Network only fills the router table.
		return
	}
	// A DNET other than the global broadcast is traffic for another network.
	if sc := n.Destination.Scope(); sc == bacnet.AddressRemoteStation || sc == bacnet.AddressRemoteBroadcast {
		return
	}
	if n.Destination.Scope() == bacnet.AddressGlobalBroadcast {
		p.broadcast = true
	}
	if sc := n.Source.Scope(); sc == bacnet.AddressRemoteStation {
		p.source = n.Source
	}
	if len(n.APDU) == 0 {
		return
	}
	pdu, err := apdu.Parse(n.APDU, s.limits)
	if err != nil {
		s.report(diag.KindMalformed, err.Error(), map[string]any{"from": p.origin.String()})
		// A confirmed request whose header parsed is answered with a Reject
		// so the requester does not retry into silence.
		if len(n.APDU) >= 4 && n.APDU[0]&0xF0 == byte(apdu.TypeConfirmedRequest) && n.APDU[0]&0x08 == 0 {
			s.sendReject(p, n.APDU[2], bacnet.RejectReasonInvalidTag)
		}
		return
	}
	s.dispatch(pdu, p)
}

func nakFor(fn bvlc.Function) uint16 {
	switch fn {
	case bvlc.FunctionWriteBroadcastDistributionTable:
		return bvlc.ResultWriteBroadcastDistributionTableNAK
	case bvlc.FunctionReadBroadcastDistributionTable:
		return bvlc.ResultReadBroadcastDistributionTableNAK
	case bvlc.FunctionRegisterForeignDevice:
		return bvlc.ResultRegisterForeignDeviceNAK
	case bvlc.FunctionReadForeignDeviceTable:
		return bvlc.ResultReadForeignDeviceTableNAK
	case bvlc.FunctionDeleteForeignDeviceTableEntry:
		return bvlc.ResultDeleteForeignDeviceTableEntryNAK
	default:
		return bvlc.ResultDistributeBroadcastToNetworkNAK
	}
}

func (s *Server) bvlcResult(to bip.Endpoint, code uint16) {
	frame, err := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionResult, ResultCode: code})
	if err != nil {
		return
	}
	ctx, cancel := clock.ContextWithTimeout(s.ctx, s.clock, s.cfg.apduTimeout)
	defer cancel()
	_ = s.sendPacket(ctx, bip.OutboundPacket{Data: frame, Destination: to})
}

func (s *Server) sendPacket(ctx context.Context, pkt bip.OutboundPacket) error {
	err := s.tr.Send(ctx, pkt)
	if err != nil && !s.closed.Load() {
		s.report(diag.KindTransport, "send failed", map[string]any{"error": err.Error(), "to": pkt.Destination.String()})
	}
	return err
}

// reply sends an APDU back to the requester of p.
func (s *Server) reply(p peer, apduBytes []byte) error {
	ctx, cancel := clock.ContextWithTimeout(s.ctx, s.clock, s.cfg.apduTimeout)
	defer cancel()
	return s.sendAPDU(ctx, p.immediate, false, p.source, false, apduBytes)
}

// sendAPDU sends an APDU to a B/IP node, or to a remote station through it
// when dest is a remote address.
func (s *Server) sendAPDU(ctx context.Context, to bip.Endpoint, broadcast bool, dest bacnet.Address, expectingReply bool, apduBytes []byte) error {
	fn := bvlc.FunctionOriginalUnicastNPDU
	if broadcast {
		fn = bvlc.FunctionOriginalBroadcastNPDU
	}
	raw, frame, err := link.Frame(fn, link.APDU(dest, expectingReply, 255, apduBytes))
	if err != nil {
		return err
	}
	if broadcast && s.cfg.bbmd != nil {
		s.cfg.bbmd.LocalBroadcast(ctx, raw, s.sendPacket)
	}
	return s.sendPacket(ctx, bip.OutboundPacket{Data: frame, Destination: to, Broadcast: broadcast})
}

// broadcastAPDU sends an unconfirmed APDU as a local broadcast, or as a
// broadcast on the remote network of p when the request came from one.
func (s *Server) broadcastAPDU(p *peer, apduBytes []byte) error {
	ctx, cancel := clock.ContextWithTimeout(s.ctx, s.clock, s.cfg.apduTimeout)
	defer cancel()
	if p != nil && p.source.Scope() == bacnet.AddressRemoteStation {
		return s.sendAPDU(ctx, p.immediate, false, bacnet.RemoteBroadcast(p.source.Network()), false, apduBytes)
	}
	return s.sendAPDU(ctx, bip.Endpoint{}, true, bacnet.Address{}, false, apduBytes)
}

func (s *Server) sendReject(p peer, invokeID, reason uint8) {
	_ = s.reply(p, apdu.AppendReject(nil, apdu.RejectPDU{InvokeID: invokeID, Reason: reason}))
}

func (s *Server) sendAbort(p peer, invokeID, reason uint8) {
	_ = s.reply(p, apdu.AppendAbort(nil, apdu.AbortPDU{Server: true, InvokeID: invokeID, Reason: reason}))
}

// endpointFromMAC turns a B/IP MAC (6 octets) back into an endpoint.
func endpointFromMAC(mac []byte) (bip.Endpoint, bool) { return link.Endpoint(mac) }
