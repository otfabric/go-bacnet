// SPDX-License-Identifier: MIT

package link_test

import (
	"bytes"
	"net/netip"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/internal/link"
	"github.com/otfabric/go-bacnet/npdu"
)

func ep(s string) bip.Endpoint { return bip.NewEndpoint(netip.MustParseAddrPort(s)) }

func TestOrigin(t *testing.T) {
	immediate := ep("192.0.2.1:47808")
	fwd := bvlc.Message{Function: bvlc.FunctionForwardedNPDU, OriginIP: [4]byte{198, 51, 100, 7}, OriginPort: 47809}
	if got := link.Origin(fwd, immediate); !got.Equal(ep("198.51.100.7:47809")) {
		t.Fatalf("Forwarded-NPDU origin %v", got)
	}
	for _, fn := range []bvlc.Function{bvlc.FunctionOriginalUnicastNPDU, bvlc.FunctionOriginalBroadcastNPDU, bvlc.FunctionDistributeBroadcastToNetwork} {
		// OriginIP is ignored for every function but Forwarded-NPDU.
		msg := bvlc.Message{Function: fn, OriginIP: [4]byte{198, 51, 100, 7}, OriginPort: 1}
		if got := link.Origin(msg, immediate); !got.Equal(immediate) {
			t.Fatalf("function %v origin %v, want the sender", fn, got)
		}
	}
}

func TestMACRoundTrip(t *testing.T) {
	e := ep("192.168.1.20:47808")
	mac, ok := link.MAC(e)
	if !ok {
		t.Fatal("MAC of a valid endpoint failed")
	}
	want := []byte{192, 168, 1, 20, 0xBA, 0xC0}
	if !bytes.Equal(mac.Bytes(), want) || mac.Len() != link.MACLen {
		t.Fatalf("MAC % x, want % x", mac.Bytes(), want)
	}
	back, ok := link.Endpoint(mac.Bytes())
	if !ok || !back.Equal(e) {
		t.Fatalf("Endpoint(% x) = %v %v", mac.Bytes(), back, ok)
	}
}

func TestMACInvalid(t *testing.T) {
	for _, e := range []bip.Endpoint{{}, ep("[2001:db8::1]:47808")} {
		if mac, ok := link.MAC(e); ok || !mac.IsZero() {
			t.Fatalf("MAC(%v) = % x %v, want none", e, mac.Bytes(), ok)
		}
	}
}

func TestEndpointInvalid(t *testing.T) {
	for _, mac := range [][]byte{nil, {1, 2, 3, 4, 5}, {1, 2, 3, 4, 5, 6, 7}} {
		if got, ok := link.Endpoint(mac); ok || got.IsValid() {
			t.Fatalf("Endpoint(% x) = %v %v, want none", mac, got, ok)
		}
	}
}

func TestAPDUDestination(t *testing.T) {
	mac := bacnet.MustMAC([]byte{10})
	apduBytes := []byte{0x10, 0x08}
	cases := []struct {
		dest     bacnet.Address
		withDNET bool
	}{
		{bacnet.Address{}, false},
		{bacnet.LocalStation(mac), false},
		{bacnet.LocalBroadcast(), false},
		{bacnet.RemoteStation(5, mac), true},
		{bacnet.RemoteBroadcast(5), true},
		{bacnet.GlobalBroadcast(), true},
	}
	for _, c := range cases {
		n := link.APDU(c.dest, true, 200, apduBytes)
		if n.Version != npdu.Version1 || !n.ExpectingReply || n.HopCount != 200 || !bytes.Equal(n.APDU, apduBytes) {
			t.Fatalf("%v: %+v", c.dest, n)
		}
		if c.withDNET {
			if n.Destination != c.dest {
				t.Fatalf("%v: destination %v", c.dest, n.Destination)
			}
		} else if n.Destination != (bacnet.Address{}) {
			t.Fatalf("%v: local destination carried %v", c.dest, n.Destination)
		}
	}
}

func TestFrameRoundTrip(t *testing.T) {
	apduBytes := []byte{0x10, 0x08}
	for _, dest := range []bacnet.Address{bacnet.LocalBroadcast(), bacnet.RemoteStation(7, bacnet.MustMAC([]byte{3}))} {
		raw, frame, err := link.Frame(bvlc.FunctionOriginalBroadcastNPDU, link.APDU(dest, false, 255, apduBytes))
		if err != nil {
			t.Fatal(err)
		}
		msg, err := bvlc.Parse(frame, bacnet.DefaultDecodeLimits())
		if err != nil || msg.Function != bvlc.FunctionOriginalBroadcastNPDU || !bytes.Equal(msg.Payload, raw) {
			t.Fatalf("BVLC %+v %v", msg, err)
		}
		n, _, err := npdu.Parse(raw, bacnet.DefaultDecodeLimits())
		if err != nil || !bytes.Equal(n.APDU, apduBytes) {
			t.Fatalf("NPDU %+v %v", n, err)
		}
		if dest.Scope() == bacnet.AddressRemoteStation && (n.Destination.Network() != 7 || n.HopCount != 255) {
			t.Fatalf("remote destination %v hop %d", n.Destination, n.HopCount)
		}
	}
}

func TestFrameErrors(t *testing.T) {
	// An NPDU the codec refuses: a remote station without a MAC.
	if _, _, err := link.Frame(bvlc.FunctionOriginalUnicastNPDU, link.APDU(bacnet.RemoteStation(5, bacnet.MAC{}), false, 255, nil)); err == nil {
		t.Fatal("remote station without MAC accepted")
	}
	// A frame larger than a BVLC length can express.
	big := link.APDU(bacnet.Address{}, false, 255, make([]byte, 70000))
	if _, _, err := link.Frame(bvlc.FunctionOriginalUnicastNPDU, big); err == nil {
		t.Fatal("oversized frame accepted")
	}
}
