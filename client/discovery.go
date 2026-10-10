// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"net/netip"
	"time"

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
	// Window is how long Discover collects I-Am replies. When positive,
	// Discover returns the devices with a nil error once it has elapsed.
	// Zero collects until ctx ends and returns ctx.Err(). SendWhoIs ignores
	// it.
	Window time.Duration
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

// Discover broadcasts a Who-Is and collects the I-Am replies. The returned
// slice holds the devices seen since the call started, in no particular
// order; Devices() returns everything the registry still retains.
//
// With opts.Window set, Discover returns after that time with a nil error.
// Without it, it collects until ctx ends and returns the devices together
// with ctx.Err(). In both cases the devices gathered so far are returned
// when ctx is cancelled or the client is closed (bacnet.ErrClosed).
//
// The Who-Is is a global broadcast sent to the local IPv4 broadcast address
// (see WithInterface), or through the BBMD when registered as a foreign
// device. For peers a broadcast cannot reach, call SendWhoIs with their
// address and read Devices().
func (c *Client) Discover(ctx context.Context, opts DiscoveryOptions) ([]DeviceObservation, error) {
	if c.isClosed() {
		return nil, bacnet.ErrClosed
	}
	since := c.clock.Now()
	if err := c.SendWhoIs(ctx, c.broadcastEndpoint(), true, opts); err != nil {
		return nil, err
	}
	err := c.collect(ctx, opts.Window)
	return c.reg.ObservationsSince(since), err
}

// broadcastEndpoint is the nominal destination of a local broadcast; the UDP
// transport substitutes the interface broadcast address when one is set.
func (c *Client) broadcastEndpoint() bip.Endpoint {
	port := c.cfg.port
	if port == 0 {
		port = bip.DefaultPort
	}
	return bip.NewEndpoint(netip.AddrPortFrom(netip.AddrFrom4([4]byte{255, 255, 255, 255}), uint16(port)))
}

// collect waits for the collection window of a discovery: window when
// positive (nil), else the end of ctx (ctx.Err()).
func (c *Client) collect(ctx context.Context, window time.Duration) error {
	var elapsed <-chan time.Time
	if window > 0 {
		timer := c.clock.NewTimer(window)
		defer timer.Stop()
		elapsed = timer.C()
	}
	select {
	case <-elapsed:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closeCh:
		return bacnet.ErrClosed
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
	case apdu.ServiceUnconfirmedCOVNotificationMultiple:
		note, err := service.DecodeCOVNotificationMultiple(req.Payload, c.limits)
		if err != nil {
			c.diag.Report(diag.Event{Kind: diag.KindMalformed, Message: err.Error()})
			return
		}
		c.deliverCOVNotificationMultiple(note, false, src)
	case apdu.ServiceUnconfirmedEventNotification:
		note, err := service.DecodeEventNotification(req.Payload, c.limits)
		if err != nil {
			c.diag.Report(diag.Event{Kind: diag.KindMalformed, Message: err.Error()})
			return
		}
		c.deliverEventNotification(note, false, src)
	case apdu.ServiceUnconfirmedAuditNotification:
		notes, err := service.DecodeAuditNotificationRequest(req.Payload, c.limits)
		if err != nil {
			c.diag.Report(diag.Event{Kind: diag.KindMalformed, Message: err.Error()})
			return
		}
		for _, note := range notes {
			c.deliverAuditNotification(note, false)
		}
	}
}
