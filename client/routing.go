// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"sync"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/internal/diag"
	"github.com/otfabric/go-bacnet/npdu"
)

// RouteSource identifies how a route was learned.
type RouteSource uint8

const (
	RouteSourceLearned RouteSource = iota
	RouteSourceManual
)

// RouteState is the usability of a learned next hop.
type RouteState uint8

const (
	RouteAvailable RouteState = iota
	RouteBusy
	RouteRejected
	RouteExpired
)

// Route is one next-hop toward a remote BACnet network.
type Route struct {
	Network      uint16
	NextHop      bip.Endpoint
	LearnedAt    time.Time
	LastSeen     time.Time
	BusyUntil    time.Time
	State        RouteState
	RejectReason uint8
	RejectedAt   time.Time
	Source       RouteSource
}

type routerCache struct {
	mu           sync.RWMutex
	byNet        map[uint16][]Route
	ttl          time.Duration
	maxPerNet    int
	maxGlobal    int
	busyDuration time.Duration
}

func newRouterCache() *routerCache {
	return &routerCache{
		byNet:        make(map[uint16][]Route),
		ttl:          10 * time.Minute,
		maxPerNet:    8,
		maxGlobal:    256,
		busyDuration: 30 * time.Second,
	}
}

func (r *routerCache) upsertLearned(network uint16, nextHop, _ bip.Endpoint, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	routes := r.byNet[network]
	for i := range routes {
		if routes[i].NextHop.Equal(nextHop) {
			routes[i].LastSeen = now
			routes[i].State = RouteAvailable
			routes[i].BusyUntil = time.Time{}
			routes[i].RejectReason = 0
			r.byNet[network] = routes
			return
		}
	}
	routes = append(routes, Route{
		Network:   network,
		NextHop:   nextHop,
		LearnedAt: now,
		LastSeen:  now,
		State:     RouteAvailable,
		Source:    RouteSourceLearned,
	})
	if len(routes) > r.maxPerNet {
		routes = routes[len(routes)-r.maxPerNet:]
	}
	r.byNet[network] = routes
	r.enforceGlobalLocked()
}

func (r *routerCache) enforceGlobalLocked() {
	total := 0
	for _, rs := range r.byNet {
		total += len(rs)
	}
	for total > r.maxGlobal {
		// Drop oldest learned route.
		var oldestNet uint16
		oldestIdx := -1
		var oldest time.Time
		first := true
		for net, rs := range r.byNet {
			for i, rt := range rs {
				if rt.Source == RouteSourceManual {
					continue
				}
				if first || rt.LastSeen.Before(oldest) {
					first = false
					oldest = rt.LastSeen
					oldestNet = net
					oldestIdx = i
				}
			}
		}
		if oldestIdx < 0 {
			return
		}
		rs := r.byNet[oldestNet]
		r.byNet[oldestNet] = append(rs[:oldestIdx], rs[oldestIdx+1:]...)
		if len(r.byNet[oldestNet]) == 0 {
			delete(r.byNet, oldestNet)
		}
		total--
	}
}

func (r *routerCache) markBusy(networks []uint16, nextHop bip.Endpoint, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	until := now.Add(r.busyDuration)
	for _, network := range networks {
		rs := r.byNet[network]
		for i := range rs {
			if rs[i].NextHop.Equal(nextHop) || !nextHop.IsValid() {
				rs[i].State = RouteBusy
				rs[i].BusyUntil = until
				rs[i].LastSeen = now
			}
		}
		r.byNet[network] = rs
	}
}

func (r *routerCache) markAvailable(networks []uint16, nextHop bip.Endpoint, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, network := range networks {
		rs := r.byNet[network]
		for i := range rs {
			if rs[i].NextHop.Equal(nextHop) || !nextHop.IsValid() {
				rs[i].State = RouteAvailable
				rs[i].BusyUntil = time.Time{}
				rs[i].LastSeen = now
			}
		}
		r.byNet[network] = rs
	}
}

func (r *routerCache) markRejected(network uint16, nextHop bip.Endpoint, reason uint8, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rs := r.byNet[network]
	for i := range rs {
		if !nextHop.IsValid() || rs[i].NextHop.Equal(nextHop) {
			rs[i].State = RouteRejected
			rs[i].RejectReason = reason
			rs[i].RejectedAt = now
			rs[i].LastSeen = now
		}
	}
	r.byNet[network] = rs
}

// selectNextHop returns a usable next hop fixed for the start of a transaction.
func (r *routerCache) selectNextHop(network uint16, now time.Time) (bip.Endpoint, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rs := r.byNet[network]
	changed := false
	for i := range rs {
		if r.ttl > 0 && now.Sub(rs[i].LastSeen) > r.ttl {
			rs[i].State = RouteExpired
			changed = true
			continue
		}
		if rs[i].State == RouteBusy && !rs[i].BusyUntil.IsZero() && !now.Before(rs[i].BusyUntil) {
			rs[i].State = RouteAvailable
			rs[i].BusyUntil = time.Time{}
			changed = true
		}
	}
	if changed {
		r.byNet[network] = rs
	}
	for _, rt := range rs {
		if rt.State == RouteAvailable && rt.NextHop.IsValid() {
			return rt.NextHop, true
		}
	}
	return bip.Endpoint{}, false
}

func (r *routerCache) routes(network uint16) []Route {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rs := r.byNet[network]
	out := make([]Route, len(rs))
	copy(out, rs)
	return out
}

func (c *Client) handleNetworkMessage(n npdu.NPDU, src packetSource) {
	now := c.clock.Now()
	switch n.NetMsgType {
	case npdu.NetMsgIAmRouterToNetwork:
		nets, err := npdu.DecodeNetworkList(n.NetMsgData)
		if err != nil {
			c.diag.Report(diag.Event{Kind: diag.KindMalformed, Message: err.Error()})
			return
		}
		for _, netn := range nets {
			c.routers.upsertLearned(netn, src.immediate, src.origin, now)
		}
		c.diag.Report(diag.Event{
			Kind:    diag.KindRouter,
			Message: "I-Am-Router-To-Network",
			Fields:  map[string]any{"networks": nets, "next_hop": src.immediate.String()},
		})
	case npdu.NetMsgWhoIsRouterToNetwork:
		// Client does not answer as router.
	case npdu.NetMsgRouterBusyToNetwork:
		nets, err := npdu.DecodeNetworkList(n.NetMsgData)
		if err != nil {
			c.diag.Report(diag.Event{Kind: diag.KindMalformed, Message: err.Error()})
			return
		}
		c.routers.markBusy(nets, src.immediate, now)
		c.diag.Report(diag.Event{Kind: diag.KindRouter, Message: "Router-Busy-To-Network", Fields: map[string]any{"networks": nets}})
	case npdu.NetMsgRouterAvailableToNetwork:
		nets, err := npdu.DecodeNetworkList(n.NetMsgData)
		if err != nil {
			c.diag.Report(diag.Event{Kind: diag.KindMalformed, Message: err.Error()})
			return
		}
		c.routers.markAvailable(nets, src.immediate, now)
		c.diag.Report(diag.Event{Kind: diag.KindRouter, Message: "Router-Available-To-Network", Fields: map[string]any{"networks": nets}})
	case npdu.NetMsgRejectMessageToNetwork:
		reason, network, err := npdu.DecodeRejectMessageToNetwork(n.NetMsgData)
		if err != nil {
			c.diag.Report(diag.Event{Kind: diag.KindMalformed, Message: err.Error()})
			return
		}
		c.routers.markRejected(network, src.immediate, reason, now)
		c.diag.Report(diag.Event{
			Kind:    diag.KindRouter,
			Message: "Reject-Message-To-Network",
			Fields:  map[string]any{"network": network, "reason": reason, "from": src.immediate.String()},
		})
	case npdu.NetMsgICouldBeRouterToNetwork:
		network, _, err := npdu.DecodeICouldBeRouterToNetwork(n.NetMsgData)
		if err != nil {
			c.diag.Report(diag.Event{Kind: diag.KindMalformed, Message: err.Error()})
			return
		}
		c.diag.Report(diag.Event{
			Kind:    diag.KindRouter,
			Message: "I-Could-Be-Router-To-Network",
			Fields:  map[string]any{"network": network, "from": src.immediate.String()},
		})
	default:
		c.diag.Report(diag.Event{Kind: diag.KindRouter, Message: "unhandled network message", Fields: map[string]any{"type": n.NetMsgType}})
	}
}

// WhoIsRouterToNetwork sends a network-layer Who-Is-Router-To-Network as a
// local broadcast. network nil means query all.
func (c *Client) WhoIsRouterToNetwork(ctx context.Context, network *uint16) error {
	return c.WhoIsRouterToNetworkAt(ctx, bip.Endpoint{}, true, network)
}

// WhoIsRouterToNetworkAt sends Who-Is-Router-To-Network to dest.
func (c *Client) WhoIsRouterToNetworkAt(ctx context.Context, dest bip.Endpoint, broadcast bool, network *uint16) error {
	if c.isClosed() {
		return bacnet.ErrClosed
	}
	var data []byte
	if network != nil {
		data = []byte{byte(*network >> 8), byte(*network)}
	}
	n := npdu.NPDU{
		Version:        npdu.Version1,
		NetworkMessage: true,
		NetMsgType:     npdu.NetMsgWhoIsRouterToNetwork,
		NetMsgData:     data,
		HopCount:       c.cfg.hopCount,
	}
	return c.sendNPDU(ctx, dest, broadcast, n)
}

// ResolveTarget fills Endpoint from the router cache when Address is remote.
// Selection is fixed at call time for the subsequent transaction; route updates
// do not change an in-flight transaction's immediate peer.
func (c *Client) ResolveTarget(addr bacnet.Address, direct bip.Endpoint) (Target, error) {
	t := Target{Address: addr, Endpoint: direct}
	if addr.Scope() == bacnet.AddressRemoteStation || addr.Scope() == bacnet.AddressRemoteBroadcast {
		if hop, ok := c.routers.selectNextHop(addr.Network(), c.clock.Now()); ok {
			t.Endpoint = hop
		} else if !direct.IsValid() {
			return Target{}, bacnet.ErrUnsupported
		}
	}
	return t, nil
}

// Routes returns a copy of cached routes for network.
func (c *Client) Routes(network uint16) []Route {
	if c == nil || c.routers == nil {
		return nil
	}
	return c.routers.routes(network)
}
