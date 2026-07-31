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

type routerEntry struct {
	network     uint16
	nextHop     bip.Endpoint
	learnedFrom bip.Endpoint
	lastSeen    time.Time
}

type routerCache struct {
	mu    sync.RWMutex
	byNet map[uint16]routerEntry
	ttl   time.Duration
}

func newRouterCache() *routerCache {
	return &routerCache{
		byNet: make(map[uint16]routerEntry),
		ttl:   10 * time.Minute,
	}
}

func (r *routerCache) upsert(network uint16, nextHop, learnedFrom bip.Endpoint, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byNet[network] = routerEntry{
		network:     network,
		nextHop:     nextHop,
		learnedFrom: learnedFrom,
		lastSeen:    now,
	}
}

func (r *routerCache) nextHop(network uint16, now time.Time) (bip.Endpoint, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.byNet[network]
	if !ok {
		return bip.Endpoint{}, false
	}
	if r.ttl > 0 && now.Sub(e.lastSeen) > r.ttl {
		return bip.Endpoint{}, false
	}
	return e.nextHop, true
}

func (c *Client) handleNetworkMessage(n npdu.NPDU, src packetSource) {
	switch n.NetMsgType {
	case npdu.NetMsgIAmRouterToNetwork:
		off := 0
		for off+2 <= len(n.NetMsgData) {
			netn := uint16(n.NetMsgData[off])<<8 | uint16(n.NetMsgData[off+1])
			off += 2
			c.routers.upsert(netn, src.immediate, src.origin, c.clock.Now())
		}
	case npdu.NetMsgWhoIsRouterToNetwork:
		// Client does not answer as router.
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
// When broadcast is false, dest receives Original-Unicast-NPDU — useful when
// the router IP is known (for example a dual-homed docker topology hop).
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
func (c *Client) ResolveTarget(addr bacnet.Address, direct bip.Endpoint) (Target, error) {
	t := Target{Address: addr, Endpoint: direct}
	if addr.Scope() == bacnet.AddressRemoteStation || addr.Scope() == bacnet.AddressRemoteBroadcast {
		if hop, ok := c.routers.nextHop(addr.Network(), c.clock.Now()); ok {
			t.Endpoint = hop
		} else if !direct.IsValid() {
			return Target{}, bacnet.ErrUnsupported
		}
	}
	return t, nil
}
