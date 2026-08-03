// SPDX-License-Identifier: MIT

package npdu_test

import (
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/npdu"
)

func TestValidateNetworkMessageViaParse(t *testing.T) {
	// Valid I-Could-Be-Router
	raw, err := npdu.Append(nil, npdu.NPDU{
		Version: npdu.Version1, NetworkMessage: true,
		NetMsgType: npdu.NetMsgICouldBeRouterToNetwork, NetMsgData: []byte{0x00, 0x01, 0x02},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := npdu.Parse(raw, bacnet.DefaultDecodeLimits()); err != nil {
		t.Fatal(err)
	}
	// Invalid I-Could-Be
	raw, err = npdu.Append(nil, npdu.NPDU{
		Version: npdu.Version1, NetworkMessage: true,
		NetMsgType: npdu.NetMsgICouldBeRouterToNetwork, NetMsgData: []byte{0x00},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := npdu.Parse(raw, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("got %v", err)
	}
	// Reject trailing
	raw, err = npdu.Append(nil, npdu.NPDU{
		Version: npdu.Version1, NetworkMessage: true,
		NetMsgType: npdu.NetMsgRejectMessageToNetwork, NetMsgData: []byte{0x01, 0x00, 0x02, 0xff},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := npdu.Parse(raw, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected reject trailing")
	}
	// Busy malformed list
	raw, err = npdu.Append(nil, npdu.NPDU{
		Version: npdu.Version1, NetworkMessage: true,
		NetMsgType: npdu.NetMsgRouterBusyToNetwork, NetMsgData: []byte{0x00},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := npdu.Parse(raw, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected busy malformed")
	}
	// Network-Number-Is truncated
	raw, err = npdu.Append(nil, npdu.NPDU{
		Version: npdu.Version1, NetworkMessage: true,
		NetMsgType: npdu.NetMsgNetworkNumberIs, NetMsgData: []byte{0x00},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := npdu.Parse(raw, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("got %v", err)
	}
	// Network list zero
	if _, err := npdu.DecodeNetworkList([]byte{0x00, 0x00}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatal(err)
	}
	if _, _, err := npdu.DecodeRejectMessageToNetwork([]byte{0x01, 0x00, 0x02, 0x00}); err == nil {
		t.Fatal("trailing")
	}
	if _, _, err := npdu.DecodeICouldBeRouterToNetwork([]byte{0x00, 0x01, 0x02, 0x03}); err == nil {
		t.Fatal("trailing")
	}
}
