// SPDX-License-Identifier: MIT

package bacnet_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bvlc"
)

func TestIPv4MaskAddrAndAsDateTimeErrors(t *testing.T) {
	m := bvlc.IPv4Mask{255, 255, 255, 0}
	if !m.Addr().Is4() {
		t.Fatal(m.Addr())
	}
	if _, err := bacnet.AsDate(bacnet.BoolValue(true)); err == nil {
		t.Fatal("expected")
	}
	if _, err := bacnet.AsTime(bacnet.BoolValue(true)); err == nil {
		t.Fatal("expected")
	}
	if _, err := bacnet.DecodeDateRange(bacnet.ApplicationValue{Kind: bacnet.ValueConstructed}); err == nil {
		t.Fatal("expected")
	}
	if _, err := bacnet.DecodeDateTime(bacnet.ApplicationValue{Kind: bacnet.ValueConstructed}); err == nil {
		t.Fatal("expected")
	}
	if bacnet.PropertyIdentifier(99999).String() == "object-list" {
		t.Fatal("unknown")
	}
	h := bacnet.HostNPort{Name: "bbmd.example", Port: 47808}
	got, err := bacnet.DecodeHostNPort(bacnet.EncodeHostNPort(h))
	if err != nil || got.Name != "bbmd.example" {
		t.Fatal(got, err)
	}
}
