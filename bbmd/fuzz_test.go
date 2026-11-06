// SPDX-License-Identifier: MIT

package bbmd

import (
	"context"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
)

// FuzzHandleBVLC feeds arbitrary BVLC messages from a peer, a registered
// foreign device and a local station: the BBMD must not panic and every
// frame it sends must parse.
func FuzzHandleBVLC(f *testing.F) {
	for _, m := range []bvlc.Message{
		{Function: bvlc.FunctionRegisterForeignDevice, TTL: 60},
		{Function: bvlc.FunctionReadBroadcastDistributionTable},
		{Function: bvlc.FunctionReadForeignDeviceTable},
		{Function: bvlc.FunctionDeleteForeignDeviceTableEntry, Payload: []byte{192, 168, 5, 5, 0xBA, 0xC0}},
		{Function: bvlc.FunctionWriteBroadcastDistributionTable},
		{Function: bvlc.FunctionDistributeBroadcastToNetwork, Payload: []byte{0x01, 0x00, 0x10, 0x08}},
		{Function: bvlc.FunctionOriginalBroadcastNPDU, Payload: []byte{0x01, 0x00, 0x10, 0x08}},
		{Function: bvlc.FunctionForwardedNPDU, OriginIP: [4]byte{10, 9, 9, 9}, OriginPort: 47808, Payload: []byte{0x01, 0x04, 0x00, 0x05, 0x01, 0x0C}},
	} {
		raw, err := bvlc.Append(nil, m)
		if err != nil {
			f.Fatal(err)
		}
		for from := uint8(0); from < 3; from++ {
			f.Add(raw, from)
		}
	}
	f.Fuzz(func(t *testing.T, data []byte, from uint8) {
		clk := &fakeClock{}
		b, err := New(Config{Self: self, BDT: []bvlc.BDTEntry{{Endpoint: self, Mask: ones}, {Endpoint: peerOne, Mask: ones}, {Endpoint: peerTwo, Mask: c24}}, MaxForeignDevices: 2, Now: clk.now})
		if err != nil {
			t.Fatal(err)
		}
		var rec recorder
		ctx := context.Background()
		reg, _ := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionRegisterForeignDevice, TTL: 60})
		m, _ := bvlc.Parse(reg, bacnet.DefaultDecodeLimits())
		b.HandleBVLC(ctx, m, fd, rec.send)
		msg, err := bvlc.Parse(data, bacnet.DefaultDecodeLimits())
		if err != nil {
			return
		}
		sender := []bip.Endpoint{peerOne, fd, local}[from%3]
		b.HandleBVLC(ctx, msg, sender, rec.send)
		b.LocalBroadcast(ctx, msg.Payload, rec.send)
		for _, p := range rec.take() {
			if _, err := bvlc.Parse(p.Data, bacnet.DefaultDecodeLimits()); err != nil {
				t.Fatalf("sent an unparsable frame % x: %v", p.Data, err)
			}
		}
	})
}
