// SPDX-License-Identifier: MIT

package client

import (
	"errors"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/internal/clock"
	"github.com/otfabric/go-bacnet/internal/diag"
	"github.com/otfabric/go-bacnet/internal/virtual"
)

func TestWithDiagnosticFunc(t *testing.T) {
	clk := clock.NewManual(time.Unix(0, 0).UTC())
	tr := virtual.New(bip.NewEndpoint(netip.MustParseAddrPort("127.0.0.1:47808")), clk, 8)

	var mu sync.Mutex
	var got []Diagnostic
	c, err := New(
		WithTransport(AdaptVirtual(tr)),
		withClock(clk),
		WithDiagnosticFunc(func(d Diagnostic) {
			mu.Lock()
			got = append(got, d)
			mu.Unlock()
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	// Malformed BVLC triggers a diagnostic.
	tr.Inject(virtual.InboundPacket{Data: []byte{0x01}, ImmediatePeer: bip.Endpoint{}, ReceivedAt: clk.Now()})
	time.Sleep(20 * time.Millisecond)

	mu.Lock()
	n := len(got)
	mu.Unlock()
	if n == 0 {
		t.Fatal("expected diagnostic callback")
	}
}

func TestWithDecodeLimitsApplied(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	limits.MaxAPDUSize = 206
	clk := clock.NewManual(time.Unix(0, 0).UTC())
	tr := virtual.New(bip.NewEndpoint(netip.MustParseAddrPort("127.0.0.1:47808")), clk, 8)
	c, err := New(
		WithTransport(AdaptVirtual(tr)),
		withClock(clk),
		WithDecodeLimits(limits),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if c.limits.MaxAPDUSize != 206 {
		t.Fatalf("limits.MaxAPDUSize=%d", c.limits.MaxAPDUSize)
	}
}

func TestNewUDPValidationFailures(t *testing.T) {
	_, err := New(WithLocalAddr("not-a-valid-host:udp"))
	if err == nil {
		t.Fatal("expected invalid local address error")
	}

	_, err = New(WithPort(-1))
	if err == nil {
		t.Fatal("expected invalid port error")
	}
}

func TestWithDiagnosticFuncNilUsesDiscard(t *testing.T) {
	clk := clock.NewManual(time.Unix(0, 0).UTC())
	tr := virtual.New(bip.NewEndpoint(netip.MustParseAddrPort("127.0.0.1:47808")), clk, 4)
	c, err := New(
		WithTransport(AdaptVirtual(tr)),
		withClock(clk),
		WithDiagnosticFunc(nil),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if _, ok := c.diag.(diag.Discard); !ok {
		t.Fatalf("diag type %T", c.diag)
	}
}

func TestWithPortAndLocalAddrDefaults(t *testing.T) {
	// Options alone do not dial until no transport is injected; verify option storage
	// indirectly by constructing with virtual transport plus port override.
	clk := clock.NewManual(time.Unix(0, 0).UTC())
	tr := virtual.New(bip.NewEndpoint(netip.MustParseAddrPort("127.0.0.1:47808")), clk, 4)
	c, err := New(
		WithTransport(AdaptVirtual(tr)),
		withClock(clk),
		WithPort(47809),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if c.cfg.port != 47809 {
		t.Fatalf("port=%d", c.cfg.port)
	}
}

func TestNewRejectsDecodeLimitsConflict(t *testing.T) {
	clk := clock.NewManual(time.Unix(0, 0).UTC())
	tr := virtual.New(bip.NewEndpoint(netip.MustParseAddrPort("127.0.0.1:47808")), clk, 4)
	limits := bacnet.DefaultDecodeLimits()
	limits.MaxAPDUSize = 206
	_, err := New(
		WithTransport(AdaptVirtual(tr)),
		withClock(clk),
		WithDecodeLimits(limits),
		WithAdvertisedMaxAPDU(480),
	)
	if err == nil || !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("got %v", err)
	}
}
