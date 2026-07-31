// SPDX-License-Identifier: MIT

package npdu_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/npdu"
)

func TestLocalAPDURoundTrip(t *testing.T) {
	apdu := []byte{0x10, 0x08}
	raw, err := npdu.Append(nil, npdu.NPDU{Version: 1, APDU: apdu})
	if err != nil {
		t.Fatal(err)
	}
	n, consumed, err := npdu.Parse(raw, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if consumed != len(raw) || string(n.APDU) != string(apdu) {
		t.Fatalf("got %#v consumed=%d", n, consumed)
	}
}

func TestRemoteDestination(t *testing.T) {
	mac := bacnet.MustMAC([]byte{1, 2, 3})
	raw, err := npdu.Append(nil, npdu.NPDU{
		Version:        1,
		Destination:    bacnet.RemoteStation(7, mac),
		HopCount:       255,
		APDU:           []byte{0x10, 0x08},
		ExpectingReply: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	n, _, err := npdu.Parse(raw, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if n.Destination.Network() != 7 || n.Destination.MAC() != mac {
		t.Fatalf("dest %#v", n.Destination)
	}
	if !n.ExpectingReply {
		t.Fatal("expecting reply")
	}
}
