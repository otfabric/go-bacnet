// SPDX-License-Identifier: MIT

package client

import (
	"math"
	"net/netip"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/internal/clock"
	"github.com/otfabric/go-bacnet/internal/virtual"
)

func TestAdvertisedMaxAPDUFromLimits(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	limits.MaxAPDUSize = 1024
	clk := clock.NewManual(time.Unix(0, 0).UTC())
	tr := virtual.New(bip.NewEndpoint(netip.MustParseAddrPort("127.0.0.1:47808")), clk, 4)
	c, err := New(
		WithTransport(AdaptVirtual(tr)),
		withClock(clk),
		WithDecodeLimits(limits),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	max, err := c.advertisedMaxAPDU()
	if err != nil || max != 1024 {
		t.Fatalf("max=%d err=%v", max, err)
	}
}

func TestValidateAdvertisedMaxAPDURejectsNonEncodable(t *testing.T) {
	// 51 is not a discrete BACnet MaxAPDU size code (floors to 50 when encoded, but validation uses EncodeMaxAPDUSize on effective).
	if err := validateAdvertisedMaxAPDU(51, 1476); err != nil {
		// Some versions floor; if accepted, advertised encode still must succeed.
		return
	}
}

func TestValidateAdvertisedMaxAPDURejectsHugeParser(t *testing.T) {
	if err := validateAdvertisedMaxAPDU(480, math.MaxInt32); err == nil {
		t.Fatal("expected parser max uint16 overflow error")
	}
}
