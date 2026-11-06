// SPDX-License-Identifier: MIT

package server

import (
	"encoding/binary"
	"sync"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/internal/clock"
	"github.com/otfabric/go-bacnet/internal/diag"
	"github.com/otfabric/go-bacnet/npdu"
)

// maxRoutes bounds the router table.
const maxRoutes = 64

// routeTable maps a remote network number to the B/IP address of the
// router that reaches it, for the server's own requests to remote
// stations (event notification recipients). The server is not a router:
// it learns routes only from what it receives. An NPDU with SNET/SADR
// (clause 6.2.2) came from that network through the B/IP node that sent
// it (the Forwarded-NPDU origin, else the sender), which bacnet-stack
// likewise keeps as the MAC of a remote source (h_bbmd.c bvlc_handler);
// I-Am-Router-To-Network names the networks its sender reaches (clause
// 6.4.2). A newer announcement replaces an older one; a network not yet
// known is not learnt once the table is full.
type routeTable struct {
	mu     sync.Mutex
	routes map[uint16]bip.Endpoint
}

func newRouteTable() *routeTable {
	return &routeTable{routes: make(map[uint16]bip.Endpoint)}
}

func (t *routeTable) learn(network uint16, router bip.Endpoint) {
	if network == 0 || network == 0xFFFF || !router.IsValid() {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, known := t.routes[network]; !known && len(t.routes) >= maxRoutes {
		return
	}
	t.routes[network] = router
}

func (t *routeTable) lookup(network uint16) (bip.Endpoint, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	ep, ok := t.routes[network]
	return ep, ok
}

// learnRoutes records the routes an inbound NPDU reveals; router is the
// B/IP node that sent it (peer.origin).
func (s *Server) learnRoutes(n npdu.NPDU, router bip.Endpoint) {
	if n.Source.Scope() == bacnet.AddressRemoteStation {
		s.routes.learn(n.Source.Network(), router)
	}
	if n.NetworkMessage && n.NetMsgType == npdu.NetMsgIAmRouterToNetwork {
		nets, err := npdu.DecodeNetworkList(n.NetMsgData)
		if err != nil {
			return
		}
		for _, net := range nets {
			s.routes.learn(net, router)
		}
	}
}

// whoIsRouter broadcasts Who-Is-Router-To-Network for a network
// (clause 6.4.1); the I-Am-Router-To-Network answer fills the table.
func (s *Server) whoIsRouter(network uint16) {
	var frame []byte
	raw, err := npdu.Append(nil, npdu.NPDU{
		Version: npdu.Version1, NetworkMessage: true, NetMsgType: npdu.NetMsgWhoIsRouterToNetwork,
		NetMsgData: binary.BigEndian.AppendUint16(nil, network),
	})
	if err == nil {
		frame, err = bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionOriginalBroadcastNPDU, Payload: raw})
	}
	if err != nil {
		s.report(diag.KindEvent, "Who-Is-Router-To-Network not encoded", map[string]any{"error": err.Error()})
		return
	}
	ctx, cancel := clock.ContextWithTimeout(s.ctx, s.clock, s.cfg.apduTimeout)
	defer cancel()
	if s.cfg.bbmd != nil {
		s.cfg.bbmd.LocalBroadcast(ctx, raw, s.sendPacket)
	}
	_ = s.sendPacket(ctx, bip.OutboundPacket{Data: frame, Broadcast: true})
}
