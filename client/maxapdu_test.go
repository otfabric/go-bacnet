// SPDX-License-Identifier: MIT

package client

import (
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/internal/clock"
	"github.com/otfabric/go-bacnet/internal/virtual"
)

func TestValidateAdvertisedMaxAPDU(t *testing.T) {
	if err := validateAdvertisedMaxAPDU(0, 1476); err != nil {
		t.Fatal(err)
	}
	if err := validateAdvertisedMaxAPDU(50, 1476); err != nil {
		t.Fatal(err)
	}
	if err := validateAdvertisedMaxAPDU(49, 1476); err == nil {
		t.Fatal("expected reject below 50")
	}
	if err := validateAdvertisedMaxAPDU(480, 206); err == nil {
		t.Fatal("expected reject advertised > parser")
	}
}

func TestNewRejectsInvalidAdvertisedMaxAPDU(t *testing.T) {
	clk := clock.NewManual(time.Unix(0, 0).UTC())
	tr := virtual.New(bip.NewEndpoint(netip.MustParseAddrPort("127.0.0.1:47808")), clk, 4)
	_, err := New(
		WithTransport(AdaptVirtual(tr)),
		withClock(clk),
		WithAdvertisedMaxAPDU(49),
	)
	if err == nil || !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("got %v", err)
	}
}
