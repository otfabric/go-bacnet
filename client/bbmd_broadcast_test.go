// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/internal/clock"
	"github.com/otfabric/go-bacnet/internal/diag"
	"github.com/otfabric/go-bacnet/internal/virtual"
	"github.com/otfabric/go-bacnet/npdu"
	"github.com/otfabric/go-bacnet/service"
)

// fdClient is a foreign-device client on a virtual link, its BBMD silent until
// the test accepts the registration.
func fdClient(t *testing.T, opts ...Option) (*Client, *virtual.Transport, bip.Endpoint, *clock.Manual) {
	t.Helper()
	clk := clock.NewManual(time.Unix(0, 0).UTC())
	local := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.5:47808"))
	bbmd := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.1:47808"))
	tr := virtual.New(local, clk, 16)
	c, err := New(append([]Option{
		WithTransport(AdaptVirtual(tr)),
		withClock(clk),
		WithForeignDevice(ForeignDeviceConfig{BBMD: bbmd, TTL: 60 * time.Second}),
		WithTransactionOptions(5*time.Second, 0, 0),
	}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, tr, bbmd, clk
}

// functions lists the BVLC functions of the frames the client sent.
func functions(tr *virtual.Transport) []bvlc.Function {
	var out []bvlc.Function
	for _, pkt := range tr.Outbox() {
		if msg, err := bvlc.Parse(pkt.Data, bacnet.DefaultDecodeLimits()); err == nil {
			out = append(out, msg.Function)
		}
	}
	return out
}

func count(fns []bvlc.Function, f bvlc.Function) int {
	n := 0
	for _, g := range fns {
		if g == f {
			n++
		}
	}
	return n
}

// waitRegisterFD returns once the client has sent its n-th
// Register-Foreign-Device. It waits for the transport's send event, not for
// an amount of time.
func waitRegisterFD(t *testing.T, tr *virtual.Transport, n int) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		sent := tr.Sent()
		if count(functions(tr), bvlc.FunctionRegisterForeignDevice) >= n {
			return
		}
		select {
		case <-sent:
		case <-deadline:
			t.Fatalf("no Register-Foreign-Device number %d", n)
		}
	}
}

// accept answers the client's Register-Foreign-Device with a successful
// BVLC-Result.
func accept(t *testing.T, tr *virtual.Transport, bbmd bip.Endpoint, clk clock.Clock) {
	t.Helper()
	waitRegisterFD(t, tr, 1)
	result, err := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionResult, ResultCode: 0})
	if err != nil {
		t.Fatal(err)
	}
	tr.Inject(virtual.InboundPacket{Data: result, ImmediatePeer: bbmd, ReceivedAt: clk.Now()})
}

// iAmFrame is an I-Am of device instance in a BVLC frame of function fn;
// origin is the Forwarded-NPDU's original source.
func iAmFrame(t *testing.T, fn bvlc.Function, instance uint32, origin bip.Endpoint) []byte {
	t.Helper()
	payload, err := service.EncodeIAm(service.IAm{
		Device:        bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: instance},
		MaxAPDULength: 1476,
		VendorID:      1,
	})
	if err != nil {
		t.Fatal(err)
	}
	n, err := npdu.Append(nil, npdu.NPDU{Version: npdu.Version1, APDU: apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{
		ServiceChoice: apdu.ServiceIAm,
		Payload:       payload,
	})})
	if err != nil {
		t.Fatal(err)
	}
	msg := bvlc.Message{Function: fn, Payload: n}
	if fn == bvlc.FunctionForwardedNPDU {
		msg.OriginIP = origin.Addr.Addr().As4()
		msg.OriginPort = origin.Addr.Port()
	}
	frame, err := bvlc.Append(nil, msg)
	if err != nil {
		t.Fatal(err)
	}
	return frame
}

func hasDevice(c *Client, instance uint32) bool {
	for _, d := range c.Devices() {
		if d.Instance == instance {
			return true
		}
	}
	return false
}

func TestForeignDeviceDoesNotBroadcastLocallyBeforeRegistration(t *testing.T) {
	c, tr, bbmd, clk := fdClient(t)
	_, err := c.Discover(context.Background(), DiscoveryOptions{Window: time.Millisecond})
	if !errors.Is(err, bacnet.ErrNotRegistered) {
		t.Fatalf("Discover before registration: %v, want ErrNotRegistered", err)
	}
	for _, f := range functions(tr) {
		if f == bvlc.FunctionOriginalBroadcastNPDU || f == bvlc.FunctionDistributeBroadcastToNetwork {
			t.Fatalf("a broadcast left before registration: %v", f)
		}
	}

	accept(t, tr, bbmd, clk)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := c.WaitForeignDeviceRegistered(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.SendWhoIs(context.Background(), c.broadcastEndpoint(), true, DiscoveryOptions{}); err != nil {
		t.Fatal(err)
	}
	var distributed bool
	for _, pkt := range tr.Outbox() {
		msg, err := bvlc.Parse(pkt.Data, bacnet.DefaultDecodeLimits())
		if err != nil {
			continue
		}
		if msg.Function == bvlc.FunctionOriginalBroadcastNPDU {
			t.Fatal("a registered foreign device broadcast locally")
		}
		if msg.Function == bvlc.FunctionDistributeBroadcastToNetwork && pkt.Destination == bbmd {
			distributed = true
		}
	}
	if !distributed {
		t.Fatal("no Distribute-Broadcast-To-Network to the BBMD after registration")
	}
}

func TestWaitForeignDeviceRegisteredEnds(t *testing.T) {
	c, _, _, _ := fdClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := c.WaitForeignDeviceRegistered(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("no answer from the BBMD: %v, want the context's deadline", err)
	}

	done := make(chan error, 1)
	go func() { done <- c.WaitForeignDeviceRegistered(context.Background()) }()
	_ = c.Close()
	select {
	case err := <-done:
		if !errors.Is(err, bacnet.ErrClosed) {
			t.Fatalf("closed while waiting: %v, want ErrClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not end the wait")
	}

	env := newVirtualPair(t)
	if err := env.Client.WaitForeignDeviceRegistered(context.Background()); !errors.Is(err, bacnet.ErrInvalidConfig) {
		t.Fatalf("not a foreign device: %v, want ErrInvalidConfig", err)
	}
}

// A registration that lapses closes the way again: the next waiter blocks until
// the BBMD accepts a new one.
func TestForeignDeviceReadyFollowsTheRegistration(t *testing.T) {
	bbmd := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.1:47808"))
	f, err := newFDState(ForeignDeviceConfig{BBMD: bbmd, TTL: time.Minute}, clock.Real{}, diag.Discard{})
	if err != nil {
		t.Fatal(err)
	}
	isOpen := func() bool {
		select {
		case <-f.readyCh():
			return true
		default:
			return false
		}
	}
	if isOpen() {
		t.Fatal("ready before any registration")
	}
	f.setRegistered(true)
	f.setRegistered(true) // a renewal: no second close
	if !isOpen() || !f.registered() {
		t.Fatal("not ready after an accepted registration")
	}
	f.setRegistered(false)
	f.setRegistered(false)
	if isOpen() || f.registered() {
		t.Fatal("still ready after the registration lapsed")
	}
	f.setRegistered(true)
	if !isOpen() {
		t.Fatal("not ready after registering again")
	}
}

// A foreign device does not execute an Original-Broadcast-NPDU from its local
// subnet (J.2.12, interpretation IC 135-2024-8): an I-Am broadcast there is
// dropped, the same I-Am forwarded by its BBMD is taken, and an
// Original-Unicast-NPDU is processed as before. The packets are handled in
// order, so once the last one is seen the dropped one has been handled too.
func TestForeignDeviceIgnoresLocalOriginalBroadcast(t *testing.T) {
	var mu sync.Mutex
	var dropped []string
	c, tr, bbmd, clk := fdClient(t, WithDiagnosticFunc(func(d Diagnostic) {
		if strings.Contains(d.Message, "Original-Broadcast-NPDU ignored") {
			mu.Lock()
			dropped = append(dropped, d.Kind)
			mu.Unlock()
		}
	}))
	neighbour := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.9:47808"))
	remote := bip.NewEndpoint(netip.MustParseAddrPort("10.0.1.7:47808"))
	for _, p := range []virtual.InboundPacket{
		{Data: iAmFrame(t, bvlc.FunctionOriginalBroadcastNPDU, 42, bip.Endpoint{}), ImmediatePeer: neighbour},
		{Data: iAmFrame(t, bvlc.FunctionOriginalUnicastNPDU, 44, bip.Endpoint{}), ImmediatePeer: neighbour},
		{Data: iAmFrame(t, bvlc.FunctionForwardedNPDU, 43, remote), ImmediatePeer: bbmd},
	} {
		p.ReceivedAt = clk.Now()
		tr.Inject(p)
	}
	waitFor(t, "the I-Am forwarded by the BBMD", func() bool { return hasDevice(c, 43) })
	if hasDevice(c, 42) {
		t.Fatal("an I-Am in a local Original-Broadcast-NPDU was taken by a foreign device")
	}
	if !hasDevice(c, 44) {
		t.Fatal("an I-Am in an Original-Unicast-NPDU was dropped")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(dropped) != 1 || dropped[0] != string(diag.KindForeignDevice) {
		t.Fatalf("diagnostics of the dropped broadcast: %v", dropped)
	}
}

// A client that is not a foreign device processes a local
// Original-Broadcast-NPDU as before.
func TestClientTakesLocalOriginalBroadcast(t *testing.T) {
	env := newVirtualPair(t)
	env.ClientTr.Inject(virtual.InboundPacket{
		Data:          iAmFrame(t, bvlc.FunctionOriginalBroadcastNPDU, 42, bip.Endpoint{}),
		ImmediatePeer: env.Peer,
		ReceivedAt:    env.Clk.Now(),
	})
	waitFor(t, "the broadcast I-Am", func() bool { return hasDevice(env.Client, 42) })
}

// A renewal whose Result is lost leaves the client registered: the BBMD may
// have the renewal, and keeps the entry of the accepted registration for TTL
// plus a grace period (J.5.2.3). TestFDLostRenewalValidUntilExpiry follows
// the registration to its expiry.
func TestForeignDeviceLostRenewalResult(t *testing.T) {
	c, tr, bbmd, clk := fdClient(t)
	accept(t, tr, bbmd, clk)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.WaitForeignDeviceRegistered(ctx); err != nil {
		t.Fatal(err)
	}
	// Renewal at TTL/2 (30 s): step the clock until it is sent.
	for i := 0; count(functions(tr), bvlc.FunctionRegisterForeignDevice) < 2; i++ {
		if i > 120 {
			t.Fatal("no renewal")
		}
		sent := tr.Sent()
		clk.Advance(time.Second)
		select {
		case <-sent:
		case <-time.After(10 * time.Millisecond):
		}
	}
	// No Result: the renewal times out after the APDU timeout (5 s), and the
	// first retry follows 2 s later. The registration holds throughout.
	for i := 0; count(functions(tr), bvlc.FunctionRegisterForeignDevice) < 3; i++ {
		if i > 120 {
			t.Fatal("no retry after the lost renewal")
		}
		if !c.ForeignDeviceRegistered() {
			t.Fatal("a lost renewal Result ended the registration")
		}
		sent := tr.Sent()
		clk.Advance(time.Second)
		select {
		case <-sent:
		case <-time.After(10 * time.Millisecond):
		}
	}
	if err := c.SendWhoIs(context.Background(), c.broadcastEndpoint(), true, DiscoveryOptions{}); err != nil {
		t.Fatalf("broadcast after the lost renewal: %v", err)
	}
}
