// SPDX-License-Identifier: MIT

package npdu_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/npdu"
)

func TestParseRejectsGlobalBroadcastSNET(t *testing.T) {
	// Version 1, source present, SNET=0xFFFF, SLEN=1, SADR=05, Who-Is.
	in := []byte{0x01, 0x08, 0xFF, 0xFF, 0x01, 0x05, 0x10, 0x08}
	if _, _, err := npdu.Parse(in, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("SNET 0xFFFF: err=%v", err)
	}
	in[2], in[3] = 0x00, 0x07
	n, _, err := npdu.Parse(in, bacnet.DefaultDecodeLimits())
	if err != nil || n.Source.Network() != 7 {
		t.Fatalf("SNET 7: %+v err=%v", n, err)
	}
}

// The control octet is derived from the typed fields; stale Control bits
// (reserved, DNET-present, SNET-present, network-message) never reach the wire.
func TestAppendBuildsControlFromFields(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	who := []byte{0x10, 0x08}
	out, err := npdu.Append(nil, npdu.NPDU{Control: 0xFF &^ npdu.ControlExpectingReply, APDU: who})
	if err != nil {
		t.Fatal(err)
	}
	if want := []byte{0x01, 0x00, 0x10, 0x08}; !bytes.Equal(out, want) {
		t.Fatalf("stale control: got % X want % X", out, want)
	}
	if _, _, err := npdu.Parse(out, limits); err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	// Expecting-reply is honoured from either the field or the Control bit.
	for _, n := range []npdu.NPDU{
		{ExpectingReply: true, Priority: 2, APDU: who},
		{Control: npdu.ControlExpectingReply, Priority: 2, APDU: who},
	} {
		out, err = npdu.Append(nil, n)
		if err != nil || !bytes.Equal(out, []byte{0x01, 0x06, 0x10, 0x08}) {
			t.Fatalf("expecting reply: % X err=%v", out, err)
		}
	}
	// Destination and source bits follow the addresses.
	mac := bacnet.MustMAC([]byte{0x0A})
	out, err = npdu.Append(nil, npdu.NPDU{
		Control:     npdu.ControlReservedBits,
		Destination: bacnet.RemoteStation(5, mac),
		Source:      bacnet.RemoteStation(7, mac),
		APDU:        who,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x01, 0x28, 0x00, 0x05, 0x01, 0x0A, 0x00, 0x07, 0x01, 0x0A, 0xFF, 0x10, 0x08}
	if !bytes.Equal(out, want) {
		t.Fatalf("routed: got % X want % X", out, want)
	}
	got, _, err := npdu.Parse(out, limits)
	if err != nil || !got.Destination.Equal(bacnet.RemoteStation(5, mac)) || !got.Source.Equal(bacnet.RemoteStation(7, mac)) ||
		got.HopCount != 255 || !bytes.Equal(got.APDU, who) {
		t.Fatalf("routed re-parse: %+v err=%v", got, err)
	}
	// Network-message bit comes from NetworkMessage only.
	out, err = npdu.Append(nil, npdu.NPDU{NetworkMessage: true, NetMsgType: npdu.NetMsgWhoIsRouterToNetwork})
	if err != nil || !bytes.Equal(out, []byte{0x01, 0x80, 0x00}) {
		t.Fatalf("network message: % X err=%v", out, err)
	}
}

func TestAppendRejectsInvalidRemoteAddresses(t *testing.T) {
	mac := bacnet.MustMAC([]byte{0x01})
	bad := map[string]npdu.NPDU{
		"destination station net 0":      {Destination: bacnet.RemoteStation(0, mac)},
		"destination station net FFFF":   {Destination: bacnet.RemoteStation(0xFFFF, mac)},
		"destination station no MAC":     {Destination: bacnet.RemoteStation(5, bacnet.MAC{})},
		"destination broadcast net 0":    {Destination: bacnet.RemoteBroadcast(0)},
		"destination broadcast net FFFF": {Destination: bacnet.RemoteBroadcast(0xFFFF)},
		"source station net 0":           {Source: bacnet.RemoteStation(0, mac)},
		"source station net FFFF":        {Source: bacnet.RemoteStation(0xFFFF, mac)},
	}
	for name, n := range bad {
		n.APDU = []byte{0x10, 0x08}
		if out, err := npdu.Append(nil, n); !errors.Is(err, bacnet.ErrMalformed) {
			t.Fatalf("%s: out=% X err=%v", name, out, err)
		}
	}
	// Global and remote broadcasts stay encodable.
	out, err := npdu.Append(nil, npdu.NPDU{Destination: bacnet.GlobalBroadcast(), APDU: []byte{0x10, 0x08}})
	if err != nil || !bytes.Equal(out, []byte{0x01, 0x20, 0xFF, 0xFF, 0x00, 0xFF, 0x10, 0x08}) {
		t.Fatalf("global broadcast: % X err=%v", out, err)
	}
	out, err = npdu.Append(nil, npdu.NPDU{Destination: bacnet.RemoteBroadcast(9), APDU: []byte{0x10, 0x08}})
	if err != nil || !bytes.Equal(out, []byte{0x01, 0x20, 0x00, 0x09, 0x00, 0xFF, 0x10, 0x08}) {
		t.Fatalf("remote broadcast: % X err=%v", out, err)
	}
}

// Hop Count follows SNET/SLEN/SADR when both addresses are present (Clause
// 6.2.2), as in a routed I-Am answering a global Who-Is.
func TestParseHopCountAfterSourceAddress(t *testing.T) {
	// DNET=0xFFFF DLEN=0, SNET=2001 SLEN=1 SADR=0x0C, hop count 254, I-Am.
	in := []byte{0x01, 0x28, 0xFF, 0xFF, 0x00, 0x07, 0xD1, 0x01, 0x0C, 0xFE,
		0x10, 0x00, 0xC4, 0x02, 0x00, 0x00, 0x0C, 0x22, 0x01, 0xE0, 0x91, 0x00, 0x21, 0x05}
	n, _, err := npdu.Parse(in, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if n.Destination.Scope() != bacnet.AddressGlobalBroadcast ||
		!n.Source.Equal(bacnet.RemoteStation(2001, bacnet.MustMAC([]byte{0x0C}))) ||
		n.HopCount != 254 || !bytes.Equal(n.APDU, in[10:]) {
		t.Fatalf("%+v", n)
	}
	out, err := npdu.Append(nil, n)
	if err != nil || !bytes.Equal(out, in) {
		t.Fatalf("re-encode: % X err=%v", out, err)
	}
	// Truncated before the hop count.
	if _, _, err := npdu.Parse(in[:9], bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("truncated hop count: %v", err)
	}
}

// Network message type numbers of Clause 6.2.4. Router-Busy (X'04') and
// Router-Available (X'05') were swapped up to v0.3.0, so a router announcing
// congestion was taken as available again.
func TestNetworkMessageTypeNumbers(t *testing.T) {
	for name, tc := range map[string]struct{ got, want int }{
		"Who-Is-Router-To-Network":    {npdu.NetMsgWhoIsRouterToNetwork, 0x00},
		"I-Am-Router-To-Network":      {npdu.NetMsgIAmRouterToNetwork, 0x01},
		"I-Could-Be-Router":           {npdu.NetMsgICouldBeRouterToNetwork, 0x02},
		"Reject-Message-To-Network":   {npdu.NetMsgRejectMessageToNetwork, 0x03},
		"Router-Busy-To-Network":      {npdu.NetMsgRouterBusyToNetwork, 0x04},
		"Router-Available-To-Network": {npdu.NetMsgRouterAvailableToNetwork, 0x05},
		"Initialize-Routing-Table":    {npdu.NetMsgInitializeRoutingTable, 0x06},
		"What-Is-Network-Number":      {npdu.NetMsgWhatIsNetworkNumber, 0x12},
		"Network-Number-Is":           {npdu.NetMsgNetworkNumberIs, 0x13},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = 0x%02X, want 0x%02X", name, tc.got, tc.want)
		}
	}
}
