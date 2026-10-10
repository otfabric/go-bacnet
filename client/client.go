// SPDX-License-Identifier: MIT

// Package client implements a BACnet/IP supervisory client runtime.
//
// One Client instance represents one local BACnet/IP data-link endpoint.
// The client composes bvlc → npdu → apdu → service layers.
package client

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/internal/clock"
	"github.com/otfabric/go-bacnet/internal/diag"
	"github.com/otfabric/go-bacnet/npdu"
)

// Client is a BACnet/IP supervisory client.
type Client struct {
	cfg    config
	tr     Transport
	reg    *Registry
	tx     *txManager
	clock  clock.Clock
	diag   diag.Sink
	limits bacnet.DecodeLimits

	mu      sync.Mutex
	closed  bool
	closeCh chan struct{}
	wg      sync.WaitGroup
	routers *routerCache
	fd      *fdState
	subs    *subscriptionManager
	seg     *segmentReceiver
	objReg  *objectRegistry

	eventMu         sync.Mutex
	eventHandler    EventNotificationHandler
	eventStream     *eventStream
	eventDispatcher *eventDispatcher

	auditMu     sync.Mutex
	auditStream *auditStream

	bvlcOps bvlcOps
}

// New validates the options, opens the BACnet/IP UDP socket (unless
// WithTransport supplies a transport) and starts the receive loop. With
// WithForeignDevice it also starts registering with the BBMD in the
// background; New does not wait for the result (see ForeignDeviceRegistered).
//
// An invalid option value fails with an error wrapping
// bacnet.ErrInvalidConfig; a socket that cannot be opened fails with the
// operating-system error. Close the returned Client to release the socket.
func New(opts ...Option) (*Client, error) {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	fail := func(err error) (*Client, error) {
		if cfg.transport != nil {
			_ = cfg.transport.Close()
		}
		return nil, err
	}
	if len(cfg.errs) > 0 {
		return fail(errors.Join(cfg.errs...))
	}
	cfg.limits = cfg.limits.Normalize()
	if cfg.clock == nil {
		cfg.clock = clock.Real{}
	}
	cfg.diag = newDiagSink(cfg.logger, cfg.diagFn)
	if err := validateAdvertisedMaxAPDU(cfg.advertisedMaxAPDU, cfg.limits.MaxAPDUSize); err != nil {
		return fail(fmt.Errorf("%w: %w", bacnet.ErrInvalidConfig, err))
	}
	if cfg.eventDispatcher != nil && cfg.eventDispatcher.Handler == nil {
		return fail(fmt.Errorf("%w: %w: EventDispatcher Handler required", bacnet.ErrInvalidConfig, bacnet.ErrMalformed))
	}
	var fd *fdState
	if cfg.fd != nil {
		var err error
		if fd, err = newFDState(*cfg.fd, cfg.clock, cfg.diag); err != nil {
			return fail(fmt.Errorf("%w: %w", bacnet.ErrInvalidConfig, err))
		}
	}

	var tr Transport
	if cfg.transport != nil {
		tr = cfg.transport
	} else {
		var err error
		tr, err = newUDPTransport(cfg)
		if err != nil {
			return nil, err
		}
	}

	c := &Client{
		cfg:          cfg,
		tr:           tr,
		reg:          newRegistry(cfg.diag, cfg.clock, cfg.registry),
		tx:           newTxManager(cfg.maxTransactions, cfg.clock.Now),
		clock:        cfg.clock,
		diag:         cfg.diag,
		limits:       cfg.limits,
		closeCh:      make(chan struct{}),
		routers:      newRouterCache(),
		subs:         newSubscriptionManager(cfg.diag),
		seg:          newSegmentReceiver(cfg.limits, cfg.diag, cfg.clock, cfg.segmentTimeout, cfg.segmentReceiveWindow),
		objReg:       newObjectRegistry(cfg.diag, cfg.clock, cfg.registry),
		eventHandler: cfg.eventHandler,
	}
	if cfg.eventDispatcher != nil {
		c.eventDispatcher = newEventDispatcher(*cfg.eventDispatcher, cfg.diag)
	}
	c.fd = fd
	c.wg.Add(1)
	go c.recvLoop()
	if c.fd != nil {
		c.wg.Add(1)
		go c.fdLoop()
	}
	return c, nil
}

// Close stops the client and releases its socket. It is idempotent and safe
// to call concurrently with any other method.
//
// Requests still waiting for a response return bacnet.ErrClosed, wrapped in
// *bacnet.OutcomeUnknownError when the request was a write that had already
// been sent. COV subscriptions end with a Closed event and are not cancelled
// on the devices (they lapse with their lifetime); close each Subscription
// first to cancel it. A foreign-device registration is left to expire with
// its TTL. Close waits for the client's goroutines, so it must not be called
// from an EventNotification handler or a diagnostic callback.
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	close(c.closeCh)
	c.mu.Unlock()

	c.tx.abortAll(bacnet.ErrClosed)
	c.seg.abortAll()
	c.subs.closeAll()
	if c.fd != nil {
		c.fd.stop()
	}
	if c.eventDispatcher != nil {
		c.eventDispatcher.close()
	}
	err := c.tr.Close()
	c.wg.Wait()
	return err
}

// Devices returns the device observation registry snapshot.
func (c *Client) Devices() []DeviceObservation {
	return c.reg.Observations()
}

// LocalEndpoint returns the local B/IP endpoint.
func (c *Client) LocalEndpoint() bip.Endpoint {
	return c.tr.Local()
}

func (c *Client) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// Receive-error backoff bounds. A transport that keeps failing (interface
// down, socket error) must not turn the receive loop into a busy loop.
const (
	recvBackoffMin = 5 * time.Millisecond
	recvBackoffMax = 500 * time.Millisecond
)

func (c *Client) recvLoop() {
	defer c.wg.Done()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-c.closeCh:
			cancel()
		case <-ctx.Done():
		}
	}()
	backoff := recvBackoffMin
	for {
		pkt, err := c.tr.Recv(ctx)
		if err != nil {
			if c.isClosed() {
				return
			}
			c.diag.Report(diag.Event{
				Kind:    diag.KindTransport,
				Message: "receive failed",
				Fields:  map[string]any{"error": err.Error(), "retry_in": backoff.String()},
			})
			select {
			case <-c.closeCh:
				return
			case <-time.After(backoff):
			}
			if backoff *= 2; backoff > recvBackoffMax {
				backoff = recvBackoffMax
			}
			continue
		}
		backoff = recvBackoffMin
		c.handlePacket(pkt)
	}
}

type packetSource struct {
	bacnetAddress bacnet.Address
	origin        bip.Endpoint
	immediate     bip.Endpoint
}

func (c *Client) handlePacket(pkt InboundPacket) {
	msg, err := bvlc.Parse(pkt.Data, c.limits)
	if err != nil {
		c.diag.Report(diag.Event{Kind: diag.KindMalformed, Message: err.Error()})
		return
	}
	origin := pkt.ImmediatePeer
	payload := msg.Payload
	switch msg.Function {
	case bvlc.FunctionOriginalUnicastNPDU, bvlc.FunctionOriginalBroadcastNPDU:
		// origin == immediate
	case bvlc.FunctionDistributeBroadcastToNetwork:
		// DBTN is a BBMD-directed request. client is not a BBMD server.
		c.diag.Report(diag.Event{
			Kind:    diag.KindBVLC,
			Message: "inbound Distribute-Broadcast-To-Network ignored (client is not a BBMD)",
			Fields:  map[string]any{"from": pkt.ImmediatePeer.String()},
		})
		return
	case bvlc.FunctionForwardedNPDU:
		// When foreign-device mode is active, only the configured BBMD may forward.
		if c.fd != nil && !pkt.ImmediatePeer.Equal(c.fd.bbmd) {
			c.diag.Report(diag.Event{
				Kind:    diag.KindForeignDevice,
				Message: "Forwarded-NPDU from non-BBMD peer rejected",
				Fields:  map[string]any{"from": pkt.ImmediatePeer.String()},
			})
			return
		}
		if ap, ok := msg.OriginAddrPort(); ok {
			origin = bip.NewEndpoint(ap)
		}
	case bvlc.FunctionResult,
		bvlc.FunctionReadBroadcastDistributionTableAck,
		bvlc.FunctionReadForeignDeviceTableAck:
		_ = c.handleBVLCManagement(msg, pkt.ImmediatePeer)
		return
	case bvlc.FunctionRegisterForeignDevice:
		return
	default:
		// Unsupported BVLC functions never reach the NPDU layer.
		return
	}
	if !origin.IsValid() {
		c.diag.Report(diag.Event{
			Kind:    diag.KindBVLC,
			Message: "inbound NPDU discarded: non-IPv4 or invalid endpoint",
			Fields: map[string]any{
				"origin":    origin.String(),
				"immediate": pkt.ImmediatePeer.String(),
			},
		})
		return
	}
	if len(payload) == 0 {
		return
	}
	n, _, err := npdu.Parse(payload, c.limits)
	if err != nil {
		c.diag.Report(diag.Event{Kind: diag.KindMalformed, Message: err.Error()})
		return
	}
	src := packetSource{
		bacnetAddress: n.Source,
		origin:        origin,
		immediate:     pkt.ImmediatePeer,
	}
	if n.Source.MAC().IsZero() && n.Source.Scope() == 0 {
		// No SADR: derive local station from origin IP MAC (BIP 6-octet).
		addr, ok := bipMACAddress(origin)
		if !ok {
			c.diag.Report(diag.Event{
				Kind:    diag.KindBVLC,
				Message: "inbound NPDU discarded: cannot derive IPv4 MAC from endpoint",
				Fields:  map[string]any{"origin": origin.String()},
			})
			return
		}
		src.bacnetAddress = addr
	}
	if n.NetworkMessage {
		c.handleNetworkMessage(n, src)
		return
	}
	if len(n.APDU) == 0 {
		return
	}
	pdu, err := apdu.Parse(n.APDU, c.limits)
	if err != nil {
		c.diag.Report(diag.Event{Kind: diag.KindMalformed, Message: err.Error()})
		return
	}
	c.dispatchAPDU(pdu, src, n)
}

func bipMACAddress(ep bip.Endpoint) (bacnet.Address, bool) {
	if !ep.IsValid() {
		return bacnet.Address{}, false
	}
	ip := ep.Addr.Addr().As4()
	port := ep.Addr.Port()
	mac := []byte{ip[0], ip[1], ip[2], ip[3], byte(port >> 8), byte(port)}
	return bacnet.LocalStation(bacnet.MustMAC(mac)), true
}

func (c *Client) dispatchAPDU(pdu apdu.PDU, src packetSource, n npdu.NPDU) {
	switch pdu.Type {
	case apdu.TypeUnconfirmedRequest:
		c.handleUnconfirmed(pdu.UnconfirmedRequest, src)
	case apdu.TypeSimpleACK, apdu.TypeComplexACK, apdu.TypeError, apdu.TypeReject, apdu.TypeAbort:
		c.handleConfirmedResponse(pdu, src)
	case apdu.TypeSegmentACK:
		c.seg.handleSegmentACK(pdu.SegmentACK, src)
	case apdu.TypeConfirmedRequest:
		// Client may receive confirmed COV notifications.
		c.handleConfirmedIndication(pdu.ConfirmedRequest, src, n.Destination)
	default:
		c.diag.Report(diag.Event{Kind: diag.KindUnexpectedAPDU, Message: fmt.Sprintf("type %v", pdu.Type)})
	}
}

func (c *Client) sendNPDU(ctx context.Context, dest bip.Endpoint, broadcast bool, n npdu.NPDU) error {
	raw, err := npdu.Append(nil, n)
	if err != nil {
		return err
	}
	fn := bvlc.FunctionOriginalUnicastNPDU
	if broadcast {
		fn = bvlc.FunctionOriginalBroadcastNPDU
		if c.fd != nil && c.fd.registered() {
			fn = bvlc.FunctionDistributeBroadcastToNetwork
			dest = c.fd.bbmd
			broadcast = false
		}
	}
	frame, err := bvlc.Append(nil, bvlc.Message{Function: fn, Payload: raw})
	if err != nil {
		return err
	}
	return c.send(ctx, OutboundPacket{Data: frame, Destination: dest, Broadcast: broadcast})
}

// send hands one datagram to the transport. Once the client is closed the
// transport's own failure ("use of closed network connection") says nothing
// a caller can act on: it is reported as bacnet.ErrClosed.
func (c *Client) send(ctx context.Context, pkt OutboundPacket) error {
	err := c.tr.Send(ctx, pkt)
	if err != nil && ctx.Err() == nil && c.isClosed() {
		return fmt.Errorf("%w: %v", bacnet.ErrClosed, err)
	}
	return err
}

func (c *Client) sendAPDU(ctx context.Context, dest bip.Endpoint, broadcast bool, destAddr bacnet.Address, expectingReply bool, apduBytes []byte) error {
	n := npdu.NPDU{
		Version:        npdu.Version1,
		Destination:    destAddr,
		ExpectingReply: expectingReply,
		HopCount:       c.cfg.hopCount,
		APDU:           apduBytes,
	}
	// Local unicast/broadcast: no DNET.
	if destAddr.Scope() == bacnet.AddressLocalStation || destAddr.Scope() == bacnet.AddressLocalBroadcast || destAddr.Scope() == 0 {
		n.Destination = bacnet.Address{}
	}
	return c.sendNPDU(ctx, dest, broadcast, n)
}
