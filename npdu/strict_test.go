// SPDX-License-Identifier: MIT

package npdu_test

import (
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/npdu"
)

func TestParseRejectsReservedAndInvalidAddressing(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	cases := []struct {
		name string
		raw  []byte
	}{
		{"reserved bit 0x40", []byte{0x01, 0x40, 0x10, 0x08}},
		{"reserved bit 0x10", []byte{0x01, 0x10, 0x10, 0x08}},
		{"global broadcast nonzero DLEN", []byte{0x01, 0x20, 0xff, 0xff, 0x01, 0xaa, 0xff, 0x10, 0x08}},
		{"remote broadcast DNET 0", []byte{0x01, 0x20, 0x00, 0x00, 0x00, 0xff, 0x10, 0x08}},
		{"remote station DNET 0", []byte{0x01, 0x20, 0x00, 0x00, 0x01, 0xaa, 0xff, 0x10, 0x08}},
		{"I-Am-Router odd length", []byte{0x01, 0x80, 0x01, 0x00}},
		{"I-Am-Router network 0", []byte{0x01, 0x80, 0x01, 0x00, 0x00}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := npdu.Parse(tc.raw, limits); !errors.Is(err, bacnet.ErrMalformed) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestDecodeNetworkList(t *testing.T) {
	nets, err := npdu.DecodeNetworkList([]byte{0x00, 0x02, 0x00, 0x03})
	if err != nil || len(nets) != 2 || nets[0] != 2 || nets[1] != 3 {
		t.Fatalf("nets=%v err=%v", nets, err)
	}
	if _, err := npdu.DecodeNetworkList([]byte{0x00}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("odd err=%v", err)
	}
	if _, err := npdu.DecodeNetworkList([]byte{0x00, 0x00}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("zero net err=%v", err)
	}
}

func TestGlobalBroadcastHopCountZeroAccepted(t *testing.T) {
	// Hop count 0 is legal at the final hop; do not reject it at Parse.
	raw := []byte{0x01, 0x20, 0xff, 0xff, 0x00, 0x00, 0x10, 0x08}
	n, _, err := npdu.Parse(raw, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if n.Destination.Scope() != bacnet.AddressGlobalBroadcast || n.HopCount != 0 {
		t.Fatalf("%#v hop=%d", n.Destination, n.HopCount)
	}
}

func FuzzParseNPDU(f *testing.F) {
	seeds := [][]byte{
		{0x01, 0x00, 0x10, 0x08},
		{0x01, 0x20, 0xff, 0xff, 0x00, 0xff, 0x10, 0x08},
		{0x01, 0x20, 0x00, 0x02, 0x06, 0x0a, 0x00, 0x00, 0x02, 0xba, 0xc0, 0xff, 0x10, 0x08},
		{0x01, 0x80, 0x01, 0x00, 0x02},
		{0x01, 0x84, 0x00},
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _, _ = npdu.Parse(data, bacnet.DefaultDecodeLimits())
	})
}
