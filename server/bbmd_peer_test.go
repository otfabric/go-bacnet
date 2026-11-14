// SPDX-License-Identifier: MIT

package server_test

import (
	"context"
	"fmt"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bbmd"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/internal/virtual"
	"github.com/otfabric/go-bacnet/server"
	"github.com/otfabric/go-bacnet/server/object"
)

// whoIsOutOfRange is an NPDU with a Who-Is for instances 1..2: the served
// device (100) does not answer it, so any output is BVLL handling.
var whoIsOutOfRange = []byte{0x01, 0x00, 0x10, 0x08, 0x09, 0x01, 0x19, 0x02}

func bbmdServer(t *testing.T, inst uint32, self bip.Endpoint, tr bip.Transport, cfg bbmd.Config) (*server.Server, *bbmd.BBMD, func() []server.Diagnostic) {
	t.Helper()
	dev, err := object.NewDevice(object.DeviceConfig{Instance: inst, Name: fmt.Sprint("dev", inst), VendorID: 999})
	if err != nil {
		t.Fatal(err)
	}
	cfg.Self = self
	b, err := bbmd.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var diags []server.Diagnostic
	s, err := server.New(dev, server.WithTransport(tr), server.WithBBMD(b), server.WithDiagnosticFunc(func(d server.Diagnostic) {
		mu.Lock()
		diags = append(diags, d)
		mu.Unlock()
	}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, b, func() []server.Diagnostic {
		mu.Lock()
		defer mu.Unlock()
		return append([]server.Diagnostic(nil), diags...)
	}
}

// Every BVLC function from a non-IPv4 or zero immediate peer is discarded
// before the BBMD sees it: no output, no registration.
func TestServerDiscardsNonIPv4ImmediatePeer(t *testing.T) {
	self := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.1:47808"))
	tr := virtual.New(self, nil, 256)
	_, b, diags := bbmdServer(t, 100, self, tr, bbmd.Config{})
	del, _ := bvlc.EncodeDeleteFDTEntry(nil, self)
	origin := netip.MustParseAddrPort("10.1.0.30:47808")
	msgs := []bvlc.Message{
		{Function: bvlc.FunctionRegisterForeignDevice, TTL: 60},
		{Function: bvlc.FunctionOriginalUnicastNPDU, Payload: whoIsOutOfRange},
		{Function: bvlc.FunctionOriginalBroadcastNPDU, Payload: whoIsOutOfRange},
		{Function: bvlc.FunctionDistributeBroadcastToNetwork, Payload: whoIsOutOfRange},
		{Function: bvlc.FunctionForwardedNPDU, OriginIP: origin.Addr().As4(), OriginPort: origin.Port(), Payload: whoIsOutOfRange},
		{Function: bvlc.FunctionReadBroadcastDistributionTable},
		{Function: bvlc.FunctionWriteBroadcastDistributionTable},
		{Function: bvlc.FunctionReadForeignDeviceTable},
		{Function: bvlc.FunctionDeleteForeignDeviceTableEntry, Payload: del},
		{Function: bvlc.FunctionResult, ResultCode: 0},
	}
	peers := []bip.Endpoint{
		bip.NewEndpoint(netip.MustParseAddrPort("[2001:db8::1]:47808")),
		bip.NewEndpoint(netip.MustParseAddrPort("[::ffff:10.0.0.2]:47808")),
		{},
	}
	for _, from := range peers {
		for _, m := range msgs {
			frame, err := bvlc.Append(nil, m)
			if err != nil {
				t.Fatal(err)
			}
			tr.Inject(bip.InboundPacket{Data: frame, ImmediatePeer: from, ReceivedAt: time.Now()})
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	for len(diags()) < len(peers)*len(msgs) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	if out := tr.Outbox(); len(out) != 0 {
		t.Fatalf("sent %d packets", len(out))
	}
	if fdt := b.FDT(); len(fdt) != 0 {
		t.Fatalf("FDT %+v", fdt)
	}
	n := 0
	for _, d := range diags() {
		if d.Kind == "bvlc" {
			n++
		}
	}
	if n != len(peers)*len(msgs) {
		t.Fatalf("%d discards reported, want %d", n, len(peers)*len(msgs))
	}
}

// subnetOnly keeps local broadcasts on the sender's own subnet.
type subnetOnly struct{ *virtual.Transport }

func (s subnetOnly) Send(ctx context.Context, p bip.OutboundPacket) error {
	if p.Broadcast {
		return nil
	}
	return s.Transport.Send(ctx, p)
}

// Two servers whose BBMDs are registered as each other's foreign device:
// a broadcast is forwarded once, and a Forwarded-NPDU from an unrelated
// node is not forwarded at all.
func TestTwoServerBBMDsForwardOnce(t *testing.T) {
	aEP := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.1:47808"))
	bEP := bip.NewEndpoint(netip.MustParseAddrPort("10.9.0.1:47808"))
	ta, tb := virtual.New(aEP, nil, 256), virtual.New(bEP, nil, 256)
	virtual.Link(ta, tb)
	_, ba, _ := bbmdServer(t, 1, aEP, subnetOnly{ta}, bbmd.Config{})
	_, bb, _ := bbmdServer(t, 2, bEP, subnetOnly{tb}, bbmd.Config{})
	reg, _ := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionRegisterForeignDevice, TTL: 0xFFFF})
	ta.Inject(bip.InboundPacket{Data: reg, ImmediatePeer: bEP})
	tb.Inject(bip.InboundPacket{Data: reg, ImmediatePeer: aEP})
	deadline := time.Now().Add(3 * time.Second)
	for (len(ba.FDT()) == 0 || len(bb.FDT()) == 0) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	forwarded := func(origin netip.AddrPort) int {
		n := 0
		for _, tr := range []*virtual.Transport{ta, tb} {
			for _, p := range tr.Outbox() {
				m, err := bvlc.Parse(p.Data, bacnet.DefaultDecodeLimits())
				if err != nil || m.Function != bvlc.FunctionForwardedNPDU {
					continue
				}
				if o, _ := m.OriginAddrPort(); o == origin {
					n++
				}
			}
		}
		return n
	}
	other := netip.MustParseAddrPort("192.0.2.7:47808")
	fw, _ := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionForwardedNPDU, OriginIP: other.Addr().As4(), OriginPort: other.Port(), Payload: whoIsOutOfRange})
	ta.Inject(bip.InboundPacket{Data: fw, ImmediatePeer: bip.NewEndpoint(netip.MustParseAddrPort("198.51.100.9:47808"))})
	local := netip.MustParseAddrPort("10.0.0.20:47808")
	ob, _ := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionOriginalBroadcastNPDU, Payload: whoIsOutOfRange})
	ta.Inject(bip.InboundPacket{Data: ob, ImmediatePeer: bip.NewEndpoint(local)})
	deadline = time.Now().Add(3 * time.Second)
	for forwarded(local) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	if n := forwarded(local); n != 1 {
		t.Fatalf("broadcast forwarded %d times, want 1", n)
	}
	if n := forwarded(other); n != 0 {
		t.Fatalf("Forwarded-NPDU from an unrelated node forwarded %d times", n)
	}
}
