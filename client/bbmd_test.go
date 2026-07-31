// SPDX-License-Identifier: MIT

package client

import (
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/internal/clock"
	"github.com/otfabric/go-bacnet/internal/diag"
	"github.com/otfabric/go-bacnet/internal/virtual"
)

func TestFDRejectsInvalidBBMD(t *testing.T) {
	_, err := newFDState(ForeignDeviceConfig{BBMD: bip.Endpoint{}, TTL: time.Minute}, clock.Real{}, diag.Discard{})
	if err == nil {
		t.Fatal("expected invalid BBMD error")
	}
}

func TestFDRejectsHugeTTL(t *testing.T) {
	bbmd := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.1:47808"))
	_, err := newFDState(ForeignDeviceConfig{
		BBMD: bbmd,
		TTL:  time.Duration(1<<32) * time.Second,
	}, clock.Real{}, diag.Discard{})
	if err == nil {
		t.Fatal("expected TTL overflow error")
	}
}

func TestFDRejectsSubSecondAndOneSecondTTL(t *testing.T) {
	bbmd := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.1:47808"))
	_, err := newFDState(ForeignDeviceConfig{BBMD: bbmd, TTL: 1500 * time.Millisecond}, clock.Real{}, diag.Discard{})
	if err == nil {
		t.Fatal("expected non-whole-second TTL rejection")
	}
	_, err = newFDState(ForeignDeviceConfig{BBMD: bbmd, TTL: time.Second}, clock.Real{}, diag.Discard{})
	if err == nil {
		t.Fatal("expected TTL < 2s rejection")
	}
	f, err := newFDState(ForeignDeviceConfig{BBMD: bbmd, TTL: 2 * time.Second}, clock.Real{}, diag.Discard{})
	if err != nil {
		t.Fatal(err)
	}
	if f.ttl != 2*time.Second || f.ttlSec != 2 {
		t.Fatalf("ttl=%v sec=%d", f.ttl, f.ttlSec)
	}
}

func TestFDResultCorrelation(t *testing.T) {
	bbmd := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.1:47808"))
	other := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.9:47808"))
	f, err := newFDState(ForeignDeviceConfig{BBMD: bbmd, TTL: time.Minute}, clock.NewManual(time.Unix(0, 0).UTC()), diag.Discard{})
	if err != nil {
		t.Fatal(err)
	}
	a := f.beginAttempt()

	f.handleResult(0, other)
	select {
	case <-a.ch:
		t.Fatal("accepted result from wrong peer")
	default:
	}

	f.handleResult(0, bbmd)
	select {
	case code := <-a.ch:
		if code != 0 {
			t.Fatalf("code %d", code)
		}
	default:
		t.Fatal("expected correlated result")
	}

	f.clearAttempt(a)
	f.handleResult(1, bbmd)
}

func TestBVLCResultError(t *testing.T) {
	err := &BVLCResultError{Code: 0x0010}
	if err.Error() != "bacnet: BVLC-Result code=16" {
		t.Fatalf("Error()=%q", err.Error())
	}
	if !errors.Is(err, bacnet.ErrProtocolViolation) {
		t.Fatal("should unwrap to protocol violation")
	}
}

func TestFDDefaultTTLWhenZero(t *testing.T) {
	bbmd := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.1:47808"))
	f, err := newFDState(ForeignDeviceConfig{BBMD: bbmd, TTL: 0}, clock.Real{}, diag.Discard{})
	if err != nil {
		t.Fatal(err)
	}
	if f.ttl != 60*time.Second || f.ttlSec != 60 {
		t.Fatalf("ttl=%v sec=%d", f.ttl, f.ttlSec)
	}
}

func TestForeignDeviceRegisteredNilAndNoFD(t *testing.T) {
	var c *Client
	if c.ForeignDeviceRegistered() {
		t.Fatal("nil client should not be registered")
	}
	env := newVirtualPair(t)
	if env.Client.ForeignDeviceRegistered() {
		t.Fatal("non-FD client should not be registered")
	}
}

func TestForeignDeviceRegisteredVirtual(t *testing.T) {
	clk := clock.NewManual(time.Unix(0, 0).UTC())
	local := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.5:47808"))
	bbmd := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.1:47808"))
	tr := virtual.New(local, clk, 16)

	c, err := New(
		WithTransport(AdaptVirtual(tr)),
		withClock(clk),
		WithForeignDevice(ForeignDeviceConfig{BBMD: bbmd, TTL: 60 * time.Second}),
		WithTransactionOptions(5*time.Second, 0, 0),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	if c.ForeignDeviceRegistered() {
		t.Fatal("should not be registered before BVLC-Result")
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		for _, pkt := range tr.Outbox() {
			msg, err := bvlc.Parse(pkt.Data, bacnet.DefaultDecodeLimits())
			if err != nil {
				continue
			}
			if msg.Function == bvlc.FunctionRegisterForeignDevice {
				result, err := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionResult, ResultCode: 0})
				if err != nil {
					t.Fatal(err)
				}
				tr.Inject(virtual.InboundPacket{Data: result, ImmediatePeer: bbmd, ReceivedAt: clk.Now()})
				goto injected
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for Register-Foreign-Device")
injected:
	time.Sleep(30 * time.Millisecond)
	if !c.ForeignDeviceRegistered() {
		t.Fatal("expected registered after successful BVLC-Result")
	}
}
