// SPDX-License-Identifier: MIT

package bbmd

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
)

// Every BVLC function from a non-IPv4 or zero immediate peer is ignored:
// nothing is sent, nothing registered and nothing delivered.
func TestHandleBVLCNonIPv4PeerIgnored(t *testing.T) {
	b, r, _ := newTest(t)
	ctx := context.Background()
	del, _ := bvlc.EncodeDeleteFDTEntry(nil, fd)
	origin := ep("10.1.0.30:47808")
	msgs := []bvlc.Message{
		{Function: bvlc.FunctionRegisterForeignDevice, TTL: 60},
		{Function: bvlc.FunctionOriginalBroadcastNPDU, Payload: whoIs},
		{Function: bvlc.FunctionDistributeBroadcastToNetwork, Payload: whoIs},
		{Function: bvlc.FunctionForwardedNPDU, OriginIP: origin.Addr.Addr().As4(), OriginPort: origin.Addr.Port(), Payload: whoIs},
		{Function: bvlc.FunctionReadBroadcastDistributionTable},
		{Function: bvlc.FunctionWriteBroadcastDistributionTable},
		{Function: bvlc.FunctionReadForeignDeviceTable},
		{Function: bvlc.FunctionDeleteForeignDeviceTableEntry, Payload: del},
		{Function: bvlc.FunctionOriginalUnicastNPDU, Payload: whoIs},
	}
	for _, from := range []bip.Endpoint{
		ep("[2001:db8::1]:47808"),
		ep("[::ffff:10.0.0.9]:47808"),
		{},
	} {
		for _, m := range msgs {
			if deliver, _ := b.HandleBVLC(ctx, m, from, r.send); deliver {
				t.Errorf("%v from %q delivered", m.Function, from.String())
			}
		}
	}
	if pk := r.take(); len(pk) != 0 {
		t.Fatalf("sent %d packets: %+v", len(pk), pk)
	}
	if len(b.FDT()) != 0 {
		t.Fatalf("FDT: %+v", b.FDT())
	}
	if _, err := forwarded(ep("[2001:db8::1]:47808"), whoIs); err == nil {
		t.Fatal("forwarded accepted a non-IPv4 origin")
	}
}

// A Forwarded-NPDU from a node that is not a BDT peer is delivered locally
// only: not rebroadcast, not sent to the foreign devices.
func TestForwardedFromNonPeerDeliveredLocallyOnly(t *testing.T) {
	b, r, _ := newTest(t)
	ctx := context.Background()
	b.HandleBVLC(ctx, bvlc.Message{Function: bvlc.FunctionRegisterForeignDevice, TTL: 60}, fd, r.send)
	r.take()
	origin := ep("192.0.2.7:47808")
	fw := bvlc.Message{Function: bvlc.FunctionForwardedNPDU, OriginIP: origin.Addr.Addr().As4(), OriginPort: origin.Addr.Port(), Payload: whoIs}
	for _, from := range []bip.Endpoint{ep("198.51.100.9:47808"), fd, local} {
		deliver, o := b.HandleBVLC(ctx, fw, from, r.send)
		if !deliver || !o.Equal(origin) {
			t.Fatalf("from %s: deliver=%v origin=%s", from, deliver, o)
		}
		if pk := r.take(); len(pk) != 0 {
			t.Fatalf("from %s: redistributed %+v", from, dests(pk))
		}
	}
	// A confirmed request is never accepted as a broadcast.
	confirmed := []byte{0x01, 0x04, 0x00, 0x05, 0x01, 0x0C}
	fw.Payload = confirmed
	if deliver, _ := b.HandleBVLC(ctx, fw, peerOne, r.send); deliver || len(r.take()) != 0 {
		t.Fatal("confirmed Forwarded-NPDU accepted")
	}
}

// A BDT peer cannot register as a foreign device, and a foreign device
// entry that names a BDT peer or the immediate sender receives nothing.
func TestPeerNotForeignDevice(t *testing.T) {
	b, r, clk := newTest(t)
	ctx := context.Background()
	for _, from := range []bip.Endpoint{peerOne, peerTwo} {
		b.HandleBVLC(ctx, bvlc.Message{Function: bvlc.FunctionRegisterForeignDevice, TTL: 60}, from, r.send)
		if code := resultCode(t, r.take(), from); code != bvlc.ResultRegisterForeignDeviceNAK {
			t.Fatalf("register %s: %#x", from, code)
		}
	}
	if code := b.register(self, 60); code != bvlc.ResultRegisterForeignDeviceNAK {
		t.Fatalf("register self: %#x", code)
	}
	if len(b.FDT()) != 0 {
		t.Fatalf("FDT: %+v", b.FDT())
	}
	// Entries placed directly, as if left from an earlier configuration.
	b.mu.Lock()
	b.fdt[peerOne] = foreign{ttl: 60, expires: clk.t.Add(time.Hour)}
	b.fdt[fd] = foreign{ttl: 60, expires: clk.t.Add(time.Hour)}
	b.mu.Unlock()
	remote := ep("10.1.0.30:47808")
	fw := bvlc.Message{Function: bvlc.FunctionForwardedNPDU, OriginIP: remote.Addr.Addr().As4(), OriginPort: remote.Addr.Port(), Payload: whoIs}
	b.HandleBVLC(ctx, fw, peerOne, r.send)
	d := dests(r.take())
	if _, ok := d[peerOne.String()]; ok || len(d) != 2 {
		t.Fatalf("one-hop forward: %v", d)
	}
	b.HandleBVLC(ctx, bvlc.Message{Function: bvlc.FunctionDistributeBroadcastToNetwork, Payload: whoIs}, fd, r.send)
	d = dests(r.take())
	if _, ok := d[fd.String()]; ok {
		t.Fatalf("DBTN back to the sender: %v", d)
	}
}

// relay delivers the packets one BBMD sends to the other BBMD and counts
// the hops of one forwarded broadcast.
func relay(t *testing.T, nodes map[bip.Endpoint]*BBMD, recs map[bip.Endpoint]*recorder, start bip.Endpoint, limit int) int {
	t.Helper()
	ctx := context.Background()
	lim := bacnet.DefaultDecodeLimits()
	hops := 0
	queue := []bip.Endpoint{start}
	for len(queue) > 0 && hops < limit {
		from := queue[0]
		queue = queue[1:]
		for _, p := range recs[from].take() {
			to, ok := nodes[p.Destination]
			if !ok {
				continue
			}
			m, err := bvlc.Parse(p.Data, lim)
			if err != nil {
				t.Fatal(err)
			}
			if m.Function != bvlc.FunctionForwardedNPDU {
				continue
			}
			hops++
			to.HandleBVLC(ctx, m, from, recs[p.Destination].send)
			queue = append(queue, p.Destination)
		}
	}
	return hops
}

// Two BBMDs registered as each other's foreign device: a broadcast is
// forwarded once and stops there.
func TestTwoBBMDsAsForeignDevicesStopAfterOneHop(t *testing.T) {
	aEP, bEP := ep("10.0.0.1:47808"), ep("10.9.0.1:47808")
	clk := &fakeClock{t: time.Unix(1000, 0)}
	a, _ := New(Config{Self: aEP, Now: clk.now})
	bb, _ := New(Config{Self: bEP, Now: clk.now})
	ra, rb := &recorder{}, &recorder{}
	ctx := context.Background()
	reg := bvlc.Message{Function: bvlc.FunctionRegisterForeignDevice, TTL: 0xFFFF}
	a.HandleBVLC(ctx, reg, bEP, ra.send)
	bb.HandleBVLC(ctx, reg, aEP, rb.send)
	ra.take()
	rb.take()
	nodes := map[bip.Endpoint]*BBMD{aEP: a, bEP: bb}
	recs := map[bip.Endpoint]*recorder{aEP: ra, bEP: rb}

	// A Forwarded-NPDU from an unrelated node: delivered at A only.
	origin := ep("192.0.2.7:47808")
	fw := bvlc.Message{Function: bvlc.FunctionForwardedNPDU, OriginIP: origin.Addr.Addr().As4(), OriginPort: origin.Addr.Port(), Payload: whoIs}
	if deliver, _ := a.HandleBVLC(ctx, fw, ep("198.51.100.9:47808"), ra.send); !deliver {
		t.Fatal("not delivered")
	}
	if hops := relay(t, nodes, recs, aEP, 100); hops != 0 {
		t.Fatalf("forwarded %d hops", hops)
	}
	// A local broadcast at A reaches B (its foreign device) once.
	a.HandleBVLC(ctx, bvlc.Message{Function: bvlc.FunctionOriginalBroadcastNPDU, Payload: whoIs}, ep("10.0.0.20:47808"), ra.send)
	if hops := relay(t, nodes, recs, aEP, 100); hops != 1 {
		t.Fatalf("forwarded %d hops, want 1", hops)
	}
}

// Two BBMDs in each other's BDT, with a stale foreign device entry naming
// the other: a broadcast does not circulate.
func TestTwoPeerBBMDsDoNotLoop(t *testing.T) {
	aEP, bEP := ep("10.0.0.1:47808"), ep("10.9.0.1:47808")
	clk := &fakeClock{t: time.Unix(1000, 0)}
	bdt := []bvlc.BDTEntry{{Endpoint: aEP, Mask: ones}, {Endpoint: bEP, Mask: ones}}
	a, _ := New(Config{Self: aEP, BDT: bdt, Now: clk.now})
	bb, _ := New(Config{Self: bEP, BDT: bdt, Now: clk.now})
	ra, rb := &recorder{}, &recorder{}
	ctx := context.Background()
	reg := bvlc.Message{Function: bvlc.FunctionRegisterForeignDevice, TTL: 0xFFFF}
	a.HandleBVLC(ctx, reg, bEP, ra.send)
	if code := resultCode(t, ra.take(), bEP); code != bvlc.ResultRegisterForeignDeviceNAK {
		t.Fatalf("peer registered: %#x", code)
	}
	for _, x := range []struct {
		b     *BBMD
		other bip.Endpoint
	}{{a, bEP}, {bb, aEP}} {
		x.b.mu.Lock()
		x.b.fdt[x.other] = foreign{ttl: 0xFFFF, expires: clk.t.Add(time.Hour)}
		x.b.mu.Unlock()
	}
	nodes := map[bip.Endpoint]*BBMD{aEP: a, bEP: bb}
	recs := map[bip.Endpoint]*recorder{aEP: ra, bEP: rb}
	a.HandleBVLC(ctx, bvlc.Message{Function: bvlc.FunctionOriginalBroadcastNPDU, Payload: whoIs}, ep("10.0.0.20:47808"), ra.send)
	if hops := relay(t, nodes, recs, aEP, 100); hops != 1 {
		t.Fatalf("forwarded %d hops, want 1", hops)
	}
	origin := netip.MustParseAddrPort("192.0.2.7:47808")
	fw := bvlc.Message{Function: bvlc.FunctionForwardedNPDU, OriginIP: origin.Addr().As4(), OriginPort: origin.Port(), Payload: whoIs}
	a.HandleBVLC(ctx, fw, bEP, ra.send)
	if hops := relay(t, nodes, recs, aEP, 100); hops != 0 {
		t.Fatalf("forwarded %d hops, want 0", hops)
	}
}
