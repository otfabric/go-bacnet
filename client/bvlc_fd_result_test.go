// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/internal/clock"
	"github.com/otfabric/go-bacnet/internal/virtual"
)

// waitOutbound waits until the client has sent a BVLC message of function fn.
func waitOutbound(t *testing.T, tr *virtual.Transport, fn bvlc.Function) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, pkt := range tr.Outbox() {
			msg, err := bvlc.Parse(pkt.Data, bacnet.DefaultDecodeLimits())
			if err == nil && msg.Function == fn {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for BVLC function 0x%02x", uint8(fn))
}

// A foreign device that reads its BBMD's BDT while its registration is
// pending receives two answers from the same BBMD: the BVLC-Result of the
// registration and the Read-BDT-Ack. The successful-completion Result
// (Annex J.2.1.1) answers the registration; a read is answered by its ACK
// (Annex J.2.4). Before the fix the Result was handed to the pending read,
// which returned no entries, and the registration never saw it.
func TestForeignDeviceResultNotTakenByPendingRead(t *testing.T) {
	for _, code := range []uint16{bvlc.ResultSuccessfulCompletion, bvlc.ResultRegisterForeignDeviceNAK} {
		clk := clock.NewManual(time.Unix(0, 0).UTC())
		local := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.5:47808"))
		bbmd := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.1:47808"))
		tr := virtual.New(local, clk, 16)
		c, err := New(
			WithTransport(AdaptVirtual(tr)),
			withClock(clk),
			WithForeignDevice(ForeignDeviceConfig{BBMD: bbmd, TTL: 60 * time.Second}),
			WithTransactionOptions(5*time.Second, 0, 0),
		)
		if err != nil {
			t.Fatal(err)
		}
		waitOutbound(t, tr, bvlc.FunctionRegisterForeignDevice)

		type readResult struct {
			entries []bvlc.BDTEntry
			err     error
		}
		done := make(chan readResult, 1)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			e, err := c.ReadBroadcastDistributionTable(ctx, bbmd)
			done <- readResult{e, err}
		}()
		waitOutbound(t, tr, bvlc.FunctionReadBroadcastDistributionTable)

		inject := func(m bvlc.Message) {
			frame, err := bvlc.Append(nil, m)
			if err != nil {
				t.Fatal(err)
			}
			tr.Inject(virtual.InboundPacket{Data: frame, ImmediatePeer: bbmd, ReceivedAt: clk.Now()})
		}
		inject(bvlc.Message{Function: bvlc.FunctionResult, ResultCode: code})
		want := []bvlc.BDTEntry{{Endpoint: bbmd, Mask: bvlc.IPv4Mask{255, 255, 255, 255}}}
		payload, err := bvlc.EncodeBDTEntries(nil, want)
		if err != nil {
			t.Fatal(err)
		}
		inject(bvlc.Message{Function: bvlc.FunctionReadBroadcastDistributionTableAck, Payload: payload})

		res := <-done
		if res.err != nil || len(res.entries) != 1 || !res.entries[0].Endpoint.Equal(bbmd) {
			t.Fatalf("code 0x%04x: Read-BDT = %v, %v; want the one ACK entry", code, res.entries, res.err)
		}
		wantReg := code == bvlc.ResultSuccessfulCompletion
		for end := time.Now().Add(time.Second); wantReg && !c.ForeignDeviceRegistered() && time.Now().Before(end); {
			time.Sleep(5 * time.Millisecond)
		}
		if got := c.ForeignDeviceRegistered(); got != wantReg {
			t.Fatalf("code 0x%04x: registered = %v, want %v", code, got, wantReg)
		}
		_ = c.Close()
	}
}
