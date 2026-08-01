// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"net/netip"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/service"
)

// WhoHasOptions configures a Who-Has request.
//
// Exactly one of Object or Name must be set. Optional LowLimit/HighLimit bound
// the device-instance search window. Address follows the same NPDU destination
// rules as DiscoveryOptions.
type WhoHasOptions struct {
	LowLimit  *uint32
	HighLimit *uint32
	Object    *bacnet.ObjectIdentifier
	Name      *bacnet.CharacterString
	Address   bacnet.Address
}

// SendWhoHas transmits a Who-Has request.
//
// Broadcast / unicast / FD DBTN behaviour matches SendWhoIs.
func (c *Client) SendWhoHas(ctx context.Context, dest bip.Endpoint, broadcast bool, opts WhoHasOptions) error {
	if c.isClosed() {
		return bacnet.ErrClosed
	}
	payload, err := service.EncodeWhoHas(service.WhoHas{
		LowLimit:  opts.LowLimit,
		HighLimit: opts.HighLimit,
		Object:    opts.Object,
		Name:      opts.Name,
	})
	if err != nil {
		return err
	}
	apduBytes := apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{
		ServiceChoice: apdu.ServiceWhoHas,
		Payload:       payload,
	})
	destAddr := bacnet.Address{}
	switch opts.Address.Scope() {
	case bacnet.AddressRemoteStation, bacnet.AddressRemoteBroadcast, bacnet.AddressGlobalBroadcast:
		destAddr = opts.Address
	default:
		if broadcast {
			destAddr = bacnet.GlobalBroadcast()
		}
	}
	return c.sendAPDU(ctx, dest, broadcast, destAddr, false, apduBytes)
}

// DiscoverObjects sends a global-broadcast Who-Has and collects I-Have
// observations until ctx is done. The returned slice contains only observations
// whose LastSeen is at or after the start of this discovery window.
// Objects() still returns the full retained object-observation snapshot.
func (c *Client) DiscoverObjects(ctx context.Context, opts WhoHasOptions) ([]ObjectObservation, error) {
	if c.isClosed() {
		return nil, bacnet.ErrClosed
	}
	since := c.clock.Now()
	port := c.cfg.port
	if port == 0 {
		port = bip.DefaultPort
	}
	bcast := bip.NewEndpoint(netip.AddrPortFrom(netip.MustParseAddr("255.255.255.255"), uint16(port)))
	if err := c.SendWhoHas(ctx, bcast, true, opts); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return c.objReg.ObservationsSince(since), ctx.Err()
	case <-c.closeCh:
		return c.objReg.ObservationsSince(since), bacnet.ErrClosed
	}
}

// Objects returns the I-Have object observation registry snapshot.
func (c *Client) Objects() []ObjectObservation {
	return c.objReg.Observations()
}
