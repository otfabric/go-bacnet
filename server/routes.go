// SPDX-License-Identifier: MIT

package server

import (
	"encoding/binary"
	"sync"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/internal/clock"
	"github.com/otfabric/go-bacnet/internal/diag"
	"github.com/otfabric/go-bacnet/npdu"
)

// maxRoutes bounds the router table, and the networks a router is being
// asked for at once.
const maxRoutes = 64

// routeRecheck is the shortest time between two Who-Is-Router-To-Network
// that the server sends for one network because another router than the
// one that answered it shows up.
const routeRecheck = 5 * time.Minute

// routeTable maps a remote network number to the B/IP address of the
// router that reaches it, for the server's own requests to remote
// stations (event notification recipients). The server is not a router:
// it learns routes only from what it receives. An NPDU with SNET/SADR
// (clause 6.2.2) came from that network through the B/IP node that sent
// it (the Forwarded-NPDU origin, else the sender), which bacnet-stack
// likewise keeps as the MAC of a remote source (h_bbmd.c bvlc_handler);
// I-Am-Router-To-Network names the networks its sender reaches (clause
// 6.4.2).
//
// A route is one of two kinds. An answered route is the I-Am-Router-To-
// Network that followed the server's own Who-Is-Router-To-Network within
// the answer window; anything else is an observed route. A newer
// observation replaces an observed route. It does not replace an answered
// one: the server then asks again (at most once per routeRecheck and
// network), and the answer decides. When the table is full the route used
// or heard from longest ago makes room, observed routes first.
type routeTable struct {
	mu     sync.Mutex
	routes map[uint16]route
	asked  map[uint16]time.Time // Who-Is-Router-To-Network sent, and when
}

type route struct {
	router   bip.Endpoint
	seen     time.Time
	answered bool
}

func newRouteTable() *routeTable {
	return &routeTable{routes: make(map[uint16]route), asked: make(map[uint16]time.Time)}
}

// learnt is what learn did with a route.
type learnt struct {
	// evicted is set with the network whose route made room.
	evicted bool
	network uint16
	// recheck: the route differs from the answered one, which is kept.
	recheck bool
}

// learn records that router reaches network, heard at now. announced says
// that the router said so itself (I-Am-Router-To-Network), which makes it
// the answer to a Who-Is-Router-To-Network sent within window.
func (t *routeTable) learn(network uint16, router bip.Endpoint, now time.Time, announced bool, window time.Duration) learnt {
	if network == 0 || network == 0xFFFF || !router.IsValid() {
		return learnt{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	at, asked := t.asked[network]
	answer := announced && asked && now.Sub(at) < window
	cur, known := t.routes[network]
	switch {
	case answer:
		cur = route{router: router, answered: true}
	case known && cur.router == router:
	case known && cur.answered:
		return learnt{recheck: true}
	default:
		cur = route{router: router}
	}
	var out learnt
	if !known && len(t.routes) >= maxRoutes {
		out = learnt{evicted: true, network: t.evictLocked()}
	}
	cur.seen = now
	t.routes[network] = cur
	return out
}

// evictLocked removes the route heard from longest ago, an observed one
// when there is one, and returns its network. The caller holds t.mu; the
// table is not empty.
func (t *routeTable) evictLocked() uint16 {
	var oldest uint16
	var best route
	first := true
	for n, r := range t.routes {
		older := r.seen.Before(best.seen) || (r.seen.Equal(best.seen) && n < oldest)
		if first || (best.answered && !r.answered) || (best.answered == r.answered && older) {
			oldest, best, first = n, r, false
		}
	}
	delete(t.routes, oldest)
	return oldest
}

func (t *routeTable) lookup(network uint16) (bip.Endpoint, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	r, ok := t.routes[network]
	return r.router, ok
}

// ask reports whether a Who-Is-Router-To-Network for network is due at
// now, and records it: at most one per network and every, and at most
// maxRoutes networks are asked for at once.
func (t *routeTable) ask(network uint16, now time.Time, every time.Duration) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	at, pending := t.asked[network]
	if pending && now.Sub(at) < every {
		return false
	}
	if !pending && len(t.asked) >= maxRoutes {
		for n, at := range t.asked {
			if now.Sub(at) >= routeRecheck {
				delete(t.asked, n)
			}
		}
		if len(t.asked) >= maxRoutes {
			return false
		}
	}
	t.asked[network] = now
	return true
}

// learnRoutes records the routes an inbound NPDU reveals; router is the
// B/IP node that sent it (peer.origin).
func (s *Server) learnRoutes(n npdu.NPDU, router bip.Endpoint) {
	now := s.clock.Now()
	record := func(network uint16, announced bool) {
		l := s.routes.learn(network, router, now, announced, s.answerWindow())
		if l.evicted {
			s.report(diag.KindRegistryEviction, "route dropped for a newer one: table full", map[string]any{"network": l.network, "limit": maxRoutes})
		}
		if l.recheck && s.routes.ask(network, now, routeRecheck) {
			s.whoIsRouter(network)
		}
	}
	if n.Source.Scope() == bacnet.AddressRemoteStation {
		record(n.Source.Network(), false)
	}
	if n.NetworkMessage && n.NetMsgType == npdu.NetMsgIAmRouterToNetwork {
		nets, err := npdu.DecodeNetworkList(n.NetMsgData)
		if err != nil {
			return
		}
		for _, net := range nets {
			record(net, true)
		}
	}
}

// whoIsRouter broadcasts Who-Is-Router-To-Network for a network
// (clause 6.4.1); the I-Am-Router-To-Network answer fills the table. The
// caller asks routeTable.ask first, which bounds how often it is sent.
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
