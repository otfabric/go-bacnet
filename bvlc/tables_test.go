// SPDX-License-Identifier: MIT

package bvlc_test

import (
	"net/netip"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
)

func TestBDTRoundTripAndMask(t *testing.T) {
	ep := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.1:47808"))
	mask := bvlc.IPv4Mask{255, 255, 255, 0}
	entry, err := bvlc.NewBDTEntry(ep, mask)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := bvlc.EncodeBDTEntries(nil, []bvlc.BDTEntry{entry, entry})
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 20 {
		t.Fatalf("len=%d", len(raw))
	}
	got, err := bvlc.DecodeBDTEntries(raw, bacnet.DefaultDecodeLimits())
	if err != nil || len(got) != 2 {
		t.Fatalf("decode: %v %#v", err, got)
	}
	if !got[0].Endpoint.Equal(ep) || got[0].Mask != mask {
		t.Fatalf("got %#v", got[0])
	}
	_, err = bvlc.NewBDTEntry(ep, bvlc.IPv4Mask{255, 0, 255, 0})
	if err == nil {
		t.Fatal("expected non-contiguous mask error")
	}
	if _, err := bvlc.DecodeBDTEntries([]byte{0x01}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected length error")
	}
}

func TestFDTRoundTripAndDelete(t *testing.T) {
	ep := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.5:47808"))
	raw, err := bvlc.EncodeFDTEntries(nil, []bvlc.FDTEntry{{Address: ep, TTL: 60, Remaining: 45}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := bvlc.DecodeFDTEntries(raw, bacnet.DefaultDecodeLimits())
	if err != nil || len(got) != 1 || got[0].TTL != 60 || got[0].Remaining != 45 {
		t.Fatalf("got %#v err=%v", got, err)
	}
	del, err := bvlc.EncodeDeleteFDTEntry(nil, ep)
	if err != nil || len(del) != 6 {
		t.Fatalf("delete encode: %v %x", err, del)
	}
	back, err := bvlc.DecodeDeleteFDTEntry(del)
	if err != nil || !back.Equal(ep) {
		t.Fatalf("delete decode: %v %#v", err, back)
	}
}

func TestBDTFDTErrorPaths(t *testing.T) {
	mask := bvlc.IPv4Mask{255, 255, 255, 0}
	if !mask.Addr().Is4() {
		t.Fatal(mask.Addr())
	}
	if _, err := bvlc.NewBDTEntry(bip.Endpoint{}, mask); err == nil {
		t.Fatal("expected invalid BDT endpoint")
	}
	ep := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.1:47808"))
	if _, err := bvlc.EncodeBDTEntries(nil, []bvlc.BDTEntry{{Endpoint: bip.Endpoint{}, Mask: mask}}); err == nil {
		t.Fatal("expected encode invalid endpoint")
	}
	if _, err := bvlc.EncodeBDTEntries(nil, []bvlc.BDTEntry{{Endpoint: ep, Mask: bvlc.IPv4Mask{255, 0, 255, 0}}}); err == nil {
		t.Fatal("expected non-contiguous mask")
	}
	limits := bacnet.DefaultDecodeLimits()
	limits.MaxElements = 1
	raw, err := bvlc.EncodeBDTEntries(nil, []bvlc.BDTEntry{{Endpoint: ep, Mask: mask}, {Endpoint: ep, Mask: mask}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bvlc.DecodeBDTEntries(raw, limits); err == nil {
		t.Fatal("expected BDT MaxElements")
	}
	if _, err := bvlc.EncodeFDTEntries(nil, []bvlc.FDTEntry{{Address: bip.Endpoint{}}}); err == nil {
		t.Fatal("expected FDT encode invalid")
	}
	if _, err := bvlc.DecodeFDTEntries([]byte{1, 2, 3}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected FDT length")
	}
	fraw, err := bvlc.EncodeFDTEntries(nil, []bvlc.FDTEntry{
		{Address: ep, TTL: 1, Remaining: 1},
		{Address: ep, TTL: 2, Remaining: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bvlc.DecodeFDTEntries(fraw, limits); err == nil {
		t.Fatal("expected FDT MaxElements")
	}
	if _, err := bvlc.EncodeDeleteFDTEntry(nil, bip.Endpoint{}); err == nil {
		t.Fatal("expected delete encode invalid")
	}
	if _, err := bvlc.DecodeDeleteFDTEntry([]byte{1, 2, 3}); err == nil {
		t.Fatal("expected delete length")
	}
}
