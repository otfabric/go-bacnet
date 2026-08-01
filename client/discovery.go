// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"net/netip"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/internal/diag"
	"github.com/otfabric/go-bacnet/service"
)

// DiscoveryOptions configures a Who-Is collection.
type DiscoveryOptions struct {
	LowLimit  *uint32
	HighLimit *uint32
	// Address overrides the NPDU destination. Zero means GlobalBroadcast when
	// broadcast is true, or local (no DNET) when broadcast is false.
	// Use RemoteBroadcast(dnet) with broadcast=false and dest=router to probe
	// a remote BACnet network via a known next hop.
	Address bacnet.Address
}

// SendWhoIs transmits a Who-Is request.
//
// When broadcast is true, the request uses Original-Broadcast-NPDU (or
// Distribute-Broadcast-To-Network when registered as a foreign device).
// When broadcast is false, dest is addressed with Original-Unicast-NPDU —
// useful for Docker port-mapped peers where global broadcast cannot reach
// the container, or for routed remote-broadcast Who-Is via a router hop.
func (c *Client) SendWhoIs(ctx context.Context, dest bip.Endpoint, broadcast bool, opts DiscoveryOptions) error {
	if c.isClosed() {
		return bacnet.ErrClosed
	}
	payload, err := service.EncodeWhoIs(service.WhoIs{LowLimit: opts.LowLimit, HighLimit: opts.HighLimit})
	if err != nil {
		return err
	}
	apduBytes := apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{
		ServiceChoice: apdu.ServiceWhoIs,
		Payload:       payload,
	})
	// Zero Address (LocalStation) means "unset": use GlobalBroadcast for
	// broadcast Who-Is, or no DNET for directed/unicast Who-Is. Explicit
	// RemoteBroadcast/RemoteStation/GlobalBroadcast overrides the NPDU DNET.
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

// Discover sends a global-broadcast Who-Is and collects I-Am observations
// until ctx is done. The returned slice contains only observations whose
// LastSeen is at or after the start of this discovery window. Devices()
// still returns the full retained registry snapshot.
//
// The returned error is typically context.Canceled or context.DeadlineExceeded
// after a successful collection window.
//
// For directed discovery (e.g. Docker published UDP ports), call SendWhoIs
// with broadcast=false and poll Devices() / registry observations.
func (c *Client) Discover(ctx context.Context, opts DiscoveryOptions) ([]DeviceObservation, error) {
	if c.isClosed() {
		return nil, bacnet.ErrClosed
	}
	since := c.clock.Now()
	port := c.cfg.port
	if port == 0 {
		port = bip.DefaultPort
	}
	bcast := bip.NewEndpoint(netip.AddrPortFrom(netip.MustParseAddr("255.255.255.255"), uint16(port)))
	if err := c.SendWhoIs(ctx, bcast, true, opts); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return c.reg.ObservationsSince(since), ctx.Err()
	case <-c.closeCh:
		return c.reg.ObservationsSince(since), bacnet.ErrClosed
	}
}

func (c *Client) handleUnconfirmed(req *apdu.UnconfirmedRequest, src packetSource) {
	if req == nil {
		return
	}
	switch req.ServiceChoice {
	case apdu.ServiceIAm:
		iam, err := service.DecodeIAm(req.Payload, c.limits)
		if err != nil {
			c.diag.Report(diag.Event{Kind: diag.KindMalformed, Message: err.Error()})
			return
		}
		caps := DeviceCapabilities{}
		caps.SetIAmFields(iam.MaxAPDULength, iam.Segmentation, iam.VendorID)
		obs := DeviceObservation{
			Instance:      iam.Device.Instance,
			Address:       src.bacnetAddress,
			Origin:        src.origin,
			ImmediatePeer: src.immediate,
			LastSeen:      c.clock.Now(),
			Capabilities:  caps,
		}
		if obs.Address.MAC().IsZero() {
			if addr, ok := bipMACAddress(src.origin); ok {
				obs.Address = addr
			}
		}
		c.reg.Upsert(obs)
	case apdu.ServiceIHave:
		ih, err := service.DecodeIHave(req.Payload, c.limits)
		if err != nil {
			c.diag.Report(diag.Event{Kind: diag.KindMalformed, Message: err.Error()})
			return
		}
		obs := ObjectObservation{
			DeviceInstance: ih.Device.Instance,
			Object:         ih.Object,
			Name:           ih.Name,
			Address:        src.bacnetAddress,
			Origin:         src.origin,
			ImmediatePeer:  src.immediate,
			LastSeen:       c.clock.Now(),
		}
		if obs.Address.MAC().IsZero() {
			if addr, ok := bipMACAddress(src.origin); ok {
				obs.Address = addr
			}
		}
		c.objReg.Upsert(obs)
	case apdu.ServiceUnconfirmedCOV:
		note, err := service.DecodeCOVNotification(req.Payload, c.limits)
		if err != nil {
			c.diag.Report(diag.Event{Kind: diag.KindMalformed, Message: err.Error()})
			return
		}
		c.subs.deliver(SubscriptionEvent{
			Notification: &note,
			State:        SubscriptionActive,
		}, note.ProcessIdentifier, src)
	case apdu.ServiceUnconfirmedEventNotification:
		note, err := service.DecodeEventNotification(req.Payload, c.limits)
		if err != nil {
			c.diag.Report(diag.Event{Kind: diag.KindMalformed, Message: err.Error()})
			return
		}
		c.deliverEventNotification(note, false, src)
	}
}
