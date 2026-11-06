// SPDX-License-Identifier: MIT

package bbmd

import (
	"context"
	"errors"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
)

func ep(s string) bip.Endpoint { return bip.NewEndpoint(netip.MustParseAddrPort(s)) }

var (
	self    = ep("10.0.0.1:47808")
	peerOne = ep("10.1.0.1:47808") // one-hop peer: all-ones mask
	peerTwo = ep("10.2.0.1:47808") // two-hop peer: /24 mask
	fd      = ep("192.168.5.5:47808")
	local   = ep("10.0.0.20:47808")
	ones    = bvlc.IPv4Mask{255, 255, 255, 255}
	c24     = bvlc.IPv4Mask{255, 255, 255, 0}
)

type recorder struct {
	mu   sync.Mutex
	pkts []bip.OutboundPacket
	fail error
}

func (r *recorder) send(_ context.Context, p bip.OutboundPacket) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pkts = append(r.pkts, p)
	return r.fail
}

func (r *recorder) take() []bip.OutboundPacket {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.pkts
	r.pkts = nil
	return out
}

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

func newTest(t *testing.T) (*BBMD, *recorder, *fakeClock) {
	t.Helper()
	clk := &fakeClock{t: time.Unix(1000, 0)}
	b, err := New(Config{Self: self, BDT: []bvlc.BDTEntry{{Endpoint: self, Mask: ones}, {Endpoint: peerOne, Mask: ones}, {Endpoint: peerTwo, Mask: c24}}, MaxForeignDevices: 2, Now: clk.now})
	if err != nil {
		t.Fatal(err)
	}
	return b, &recorder{}, clk
}

func dests(pkts []bip.OutboundPacket) map[string]bvlc.Message {
	out := map[string]bvlc.Message{}
	for _, p := range pkts {
		m, err := bvlc.Parse(p.Data, bacnet.DefaultDecodeLimits())
		if err != nil {
			panic(err)
		}
		key := p.Destination.String()
		if p.Broadcast {
			key = "broadcast"
		}
		out[key] = m
	}
	return out
}

// An unconfirmed Who-Is NPDU.
var whoIs = []byte{0x01, 0x00, 0x10, 0x08}

func resultCode(t *testing.T, pkts []bip.OutboundPacket, to bip.Endpoint) uint16 {
	t.Helper()
	if len(pkts) != 1 || !pkts[0].Destination.Equal(to) {
		t.Fatalf("want one result to %s, got %+v", to, pkts)
	}
	m, err := bvlc.Parse(pkts[0].Data, bacnet.DefaultDecodeLimits())
	if err != nil || m.Function != bvlc.FunctionResult {
		t.Fatalf("not a result: %v %+v", err, m)
	}
	return m.ResultCode
}

func TestRegistrationAndTables(t *testing.T) {
	b, r, clk := newTest(t)
	ctx := context.Background()
	if deliver, _ := b.HandleBVLC(ctx, bvlc.Message{Function: bvlc.FunctionRegisterForeignDevice, TTL: 60}, fd, r.send); deliver {
		t.Fatal("registration delivered")
	}
	if code := resultCode(t, r.take(), fd); code != bvlc.ResultSuccessfulCompletion {
		t.Fatalf("register: %#x", code)
	}
	fdt := b.FDT()
	if len(fdt) != 1 || fdt[0].TTL != 60 || fdt[0].Remaining != 90 {
		t.Fatalf("FDT: %+v", fdt)
	}
	// The table is full after a second device; re-registering is fine.
	other := ep("192.168.5.6:47808")
	b.HandleBVLC(ctx, bvlc.Message{Function: bvlc.FunctionRegisterForeignDevice, TTL: 10}, other, r.send)
	b.HandleBVLC(ctx, bvlc.Message{Function: bvlc.FunctionRegisterForeignDevice, TTL: 10}, ep("192.168.5.7:47808"), r.send)
	b.HandleBVLC(ctx, bvlc.Message{Function: bvlc.FunctionRegisterForeignDevice, TTL: 120}, fd, r.send)
	pk := r.take()
	if len(pk) != 3 {
		t.Fatal(pk)
	}
	if code := resultCode(t, pk[1:2], ep("192.168.5.7:47808")); code != bvlc.ResultRegisterForeignDeviceNAK {
		t.Fatalf("full: %#x", code)
	}
	// Read the tables.
	b.HandleBVLC(ctx, bvlc.Message{Function: bvlc.FunctionReadBroadcastDistributionTable}, fd, r.send)
	b.HandleBVLC(ctx, bvlc.Message{Function: bvlc.FunctionReadForeignDeviceTable}, fd, r.send)
	pk = r.take()
	m, _ := bvlc.Parse(pk[0].Data, bacnet.DefaultDecodeLimits())
	bdt, err := bvlc.DecodeBDTEntries(m.Payload, bacnet.DefaultDecodeLimits())
	if err != nil || len(bdt) != 3 || m.Function != bvlc.FunctionReadBroadcastDistributionTableAck {
		t.Fatalf("BDT ack: %v %+v", err, bdt)
	}
	m, _ = bvlc.Parse(pk[1].Data, bacnet.DefaultDecodeLimits())
	fdts, err := bvlc.DecodeFDTEntries(m.Payload, bacnet.DefaultDecodeLimits())
	if err != nil || len(fdts) != 2 {
		t.Fatalf("FDT ack: %v %+v", err, fdts)
	}
	// Write-BDT is refused (J.4.4.2).
	b.HandleBVLC(ctx, bvlc.Message{Function: bvlc.FunctionWriteBroadcastDistributionTable}, fd, r.send)
	if code := resultCode(t, r.take(), fd); code != bvlc.ResultWriteBroadcastDistributionTableNAK {
		t.Fatalf("write BDT: %#x", code)
	}
	// Delete an entry, then a missing one, then a malformed request.
	del, _ := bvlc.EncodeDeleteFDTEntry(nil, other)
	b.HandleBVLC(ctx, bvlc.Message{Function: bvlc.FunctionDeleteForeignDeviceTableEntry, Payload: del}, fd, r.send)
	b.HandleBVLC(ctx, bvlc.Message{Function: bvlc.FunctionDeleteForeignDeviceTableEntry, Payload: del}, fd, r.send)
	b.HandleBVLC(ctx, bvlc.Message{Function: bvlc.FunctionDeleteForeignDeviceTableEntry, Payload: []byte{1}}, fd, r.send)
	pk = r.take()
	if resultCode(t, pk[0:1], fd) != bvlc.ResultSuccessfulCompletion || resultCode(t, pk[1:2], fd) != bvlc.ResultDeleteForeignDeviceTableEntryNAK || resultCode(t, pk[2:3], fd) != bvlc.ResultDeleteForeignDeviceTableEntryNAK {
		t.Fatal("delete results")
	}
	// Entries expire after TTL plus the grace period.
	clk.t = clk.t.Add(149 * time.Second)
	if len(b.FDT()) != 1 {
		t.Fatal("expired early")
	}
	clk.t = clk.t.Add(2 * time.Second)
	if len(b.FDT()) != 0 {
		t.Fatal("not expired")
	}
	if len(b.BDT()) != 3 {
		t.Fatal("BDT")
	}
}

func TestForwarding(t *testing.T) {
	b, r, _ := newTest(t)
	ctx := context.Background()
	b.HandleBVLC(ctx, bvlc.Message{Function: bvlc.FunctionRegisterForeignDevice, TTL: 60}, fd, r.send)
	r.take()

	// A local Original-Broadcast goes to the peers (two-hop as a directed
	// broadcast) and the foreign device, and to the local device.
	deliver, origin := b.HandleBVLC(ctx, bvlc.Message{Function: bvlc.FunctionOriginalBroadcastNPDU, Payload: whoIs}, local, r.send)
	if !deliver || !origin.Equal(local) {
		t.Fatal("original broadcast not delivered")
	}
	d := dests(r.take())
	for _, k := range []string{peerOne.String(), "10.2.0.255:47808", fd.String()} {
		m, ok := d[k]
		if !ok || m.Function != bvlc.FunctionForwardedNPDU {
			t.Fatalf("missing forward to %s: %+v", k, d)
		}
		if o, _ := m.OriginAddrPort(); o != local.Addr {
			t.Fatalf("origin %v", o)
		}
	}
	if len(d) != 3 {
		t.Fatalf("forwards: %v", d)
	}
	// A confirmed request broadcast is discarded.
	confirmed := []byte{0x01, 0x04, 0x00, 0x05, 0x01, 0x0C}
	if deliver, _ := b.HandleBVLC(ctx, bvlc.Message{Function: bvlc.FunctionOriginalBroadcastNPDU, Payload: confirmed}, local, r.send); deliver || len(r.take()) != 0 {
		t.Fatal("confirmed broadcast forwarded")
	}

	// A Forwarded-NPDU from a one-hop peer is rebroadcast locally and sent
	// to the foreign device; from a two-hop peer only to the foreign device.
	remote := ep("10.1.0.30:47808")
	fw := bvlc.Message{Function: bvlc.FunctionForwardedNPDU, OriginIP: remote.Addr.Addr().As4(), OriginPort: remote.Addr.Port(), Payload: whoIs}
	deliver, origin = b.HandleBVLC(ctx, fw, peerOne, r.send)
	if !deliver || !origin.Equal(remote) {
		t.Fatal("forwarded not delivered")
	}
	d = dests(r.take())
	if _, ok := d["broadcast"]; !ok || len(d) != 2 {
		t.Fatalf("one-hop forward: %v", d)
	}
	b.HandleBVLC(ctx, fw, peerTwo, r.send)
	if d := dests(r.take()); len(d) != 1 {
		t.Fatalf("two-hop forward: %v", d)
	}
	// Our own forward looped back is dropped.
	mine := fw
	mine.OriginIP, mine.OriginPort = self.Addr.Addr().As4(), self.Addr.Port()
	if deliver, _ := b.HandleBVLC(ctx, mine, peerOne, r.send); deliver {
		t.Fatal("own forward delivered")
	}
	if deliver, _ := b.HandleBVLC(ctx, bvlc.Message{Function: bvlc.FunctionOriginalBroadcastNPDU, Payload: whoIs}, self, r.send); deliver {
		t.Fatal("own broadcast delivered")
	}

	// Distribute-Broadcast-To-Network from the foreign device: local
	// broadcast and the peers, not back to the sender.
	deliver, origin = b.HandleBVLC(ctx, bvlc.Message{Function: bvlc.FunctionDistributeBroadcastToNetwork, Payload: whoIs}, fd, r.send)
	if !deliver || !origin.Equal(fd) {
		t.Fatal("DBTN not delivered")
	}
	d = dests(r.take())
	if _, ok := d["broadcast"]; !ok || len(d) != 3 {
		t.Fatalf("DBTN: %v", d)
	}
	if _, back := d[fd.String()]; back {
		t.Fatal("DBTN sent back to the sender")
	}
	// From an unregistered node: NAK.
	if deliver, _ := b.HandleBVLC(ctx, bvlc.Message{Function: bvlc.FunctionDistributeBroadcastToNetwork, Payload: whoIs}, local, r.send); deliver {
		t.Fatal("unregistered DBTN delivered")
	}
	if code := resultCode(t, r.take(), local); code != bvlc.ResultDistributeBroadcastToNetworkNAK {
		t.Fatalf("DBTN NAK: %#x", code)
	}

	// The local device's own broadcasts reach the peers and the FD.
	b.LocalBroadcast(ctx, whoIs, r.send)
	if d := dests(r.take()); len(d) != 3 {
		t.Fatalf("local broadcast: %v", d)
	}
	r.fail = errors.New("send failed")
	if err := b.distribute(ctx, self, whoIs, true, r.send); err == nil {
		t.Fatal("send errors not joined")
	}
}

func TestConfig(t *testing.T) {
	for _, cfg := range []Config{
		{},
		{Self: ep("0.0.0.0:47808")},
		{Self: self, BDT: []bvlc.BDTEntry{{Endpoint: peerOne, Mask: bvlc.IPv4Mask{255, 0, 255, 0}}}},
		{Self: self, MaxForeignDevices: -1},
	} {
		if _, err := New(cfg); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("accepted %+v: %v", cfg, err)
		}
	}
	b, err := New(Config{Self: self, RejectRegistrations: true})
	if err != nil {
		t.Fatal(err)
	}
	r := &recorder{}
	b.HandleBVLC(context.Background(), bvlc.Message{Function: bvlc.FunctionRegisterForeignDevice, TTL: 1}, fd, r.send)
	if code := resultCode(t, r.take(), fd); code != bvlc.ResultRegisterForeignDeviceNAK {
		t.Fatalf("rejecting BBMD: %#x", code)
	}
	if b.FDT() == nil || len(b.FDT()) != 0 {
		t.Fatal("FDT")
	}
}

func TestConfirmedRequestDetection(t *testing.T) {
	cases := []struct {
		npdu []byte
		want bool
	}{
		{nil, false},
		{[]byte{0x01, 0x80, 0x00}, false},      // network message
		{[]byte{0x01, 0x04, 0x00, 0x05}, true}, // confirmed
		{[]byte{0x01, 0x20, 0xFF, 0xFF, 0x00, 0xFF, 0x10, 0x08}, false},                  // global broadcast Who-Is
		{[]byte{0x01, 0x28, 0x00, 0x05, 0x00, 0x00, 0x07, 0x01, 0x09, 0xFF, 0x00}, true}, // DNET+SNET confirmed
		{[]byte{0x01, 0x20, 0x00}, false},                                                // truncated DNET
		{[]byte{0x01, 0x08, 0x00}, false},                                                // truncated SNET
	}
	for i, c := range cases {
		if got := confirmedRequest(c.npdu); got != c.want {
			t.Errorf("case %d: %v", i, got)
		}
	}
}
