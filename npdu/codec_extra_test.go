// SPDX-License-Identifier: MIT

package npdu_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/npdu"
)

func TestSourceAndNetworkMessage(t *testing.T) {
	srcMAC := bacnet.MustMAC([]byte{10, 0, 0, 1, 0xBA, 0xC0})
	dstMAC := bacnet.MustMAC([]byte{10, 0, 0, 2, 0xBA, 0xC0})
	raw, err := npdu.Append(nil, npdu.NPDU{
		Version:     npdu.Version1,
		Destination: bacnet.RemoteStation(2, dstMAC),
		Source:      bacnet.RemoteStation(1, srcMAC),
		HopCount:    200,
		APDU:        []byte{0x10, 0x08},
	})
	if err != nil {
		t.Fatal(err)
	}
	n, _, err := npdu.Parse(raw, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if n.Source.Network() != 1 || n.Source.MAC() != srcMAC {
		t.Fatalf("source %#v", n.Source)
	}
	if n.Destination.Network() != 2 || n.HopCount != 200 {
		t.Fatalf("dest/hop %#v %d", n.Destination, n.HopCount)
	}
}

func TestRemoteAndGlobalBroadcast(t *testing.T) {
	raw, err := npdu.Append(nil, npdu.NPDU{
		Version: npdu.Version1, Destination: bacnet.RemoteBroadcast(5), HopCount: 255, APDU: []byte{0x10, 0x08},
	})
	if err != nil {
		t.Fatal(err)
	}
	n, _, err := npdu.Parse(raw, bacnet.DefaultDecodeLimits())
	if err != nil || n.Destination.Scope() != bacnet.AddressRemoteBroadcast || n.Destination.Network() != 5 {
		t.Fatalf("%v %#v", err, n.Destination)
	}

	raw, err = npdu.Append(nil, npdu.NPDU{
		Version: npdu.Version1, Destination: bacnet.GlobalBroadcast(), HopCount: 255, APDU: []byte{0x10, 0x08},
	})
	if err != nil {
		t.Fatal(err)
	}
	n, _, err = npdu.Parse(raw, bacnet.DefaultDecodeLimits())
	if err != nil || n.Destination.Scope() != bacnet.AddressGlobalBroadcast {
		t.Fatalf("%v %#v", err, n.Destination)
	}
}

func TestNetworkMessageWhoIsRouter(t *testing.T) {
	raw, err := npdu.Append(nil, npdu.NPDU{
		Version:        npdu.Version1,
		NetworkMessage: true,
		NetMsgType:     npdu.NetMsgWhoIsRouterToNetwork,
		NetMsgData:     []byte{0x00, 0x02},
	})
	if err != nil {
		t.Fatal(err)
	}
	n, _, err := npdu.Parse(raw, bacnet.DefaultDecodeLimits())
	if err != nil || !n.NetworkMessage || n.NetMsgType != npdu.NetMsgWhoIsRouterToNetwork {
		t.Fatalf("%v %#v", err, n)
	}
	if !bytes.Equal(n.NetMsgData, []byte{0x00, 0x02}) {
		t.Fatalf("data %x", n.NetMsgData)
	}
}

func TestNPDUMalformed(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	if _, _, err := npdu.Parse([]byte{0x01}, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("short: %v", err)
	}
	if _, _, err := npdu.Parse([]byte{0x02, 0x00}, limits); !errors.Is(err, bacnet.ErrUnsupported) {
		t.Fatalf("version: %v", err)
	}
	tiny := bacnet.DecodeLimits{MaxDatagramSize: 2}
	if _, _, err := npdu.Parse([]byte{0x01, 0x00, 0x10}, tiny); !errors.Is(err, bacnet.ErrLimitExceeded) {
		t.Fatalf("limit: %v", err)
	}
}
