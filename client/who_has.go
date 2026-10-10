// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"time"

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
	// Window is how long DiscoverObjects collects I-Have replies; see
	// DiscoveryOptions.Window.
	Window time.Duration
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

// DiscoverObjects broadcasts a Who-Has and collects the I-Have replies seen
// since the call started. The collection window, the returned error and the
// broadcast path follow Discover. Objects() returns everything the registry
// still retains.
func (c *Client) DiscoverObjects(ctx context.Context, opts WhoHasOptions) ([]ObjectObservation, error) {
	if c.isClosed() {
		return nil, bacnet.ErrClosed
	}
	since := c.clock.Now()
	if err := c.SendWhoHas(ctx, c.broadcastEndpoint(), true, opts); err != nil {
		return nil, err
	}
	err := c.collect(ctx, opts.Window)
	return c.objReg.ObservationsSince(since), err
}

// Objects returns the I-Have object observation registry snapshot.
func (c *Client) Objects() []ObjectObservation {
	return c.objReg.Observations()
}
