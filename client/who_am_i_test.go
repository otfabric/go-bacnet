// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/internal/virtual"
	"github.com/otfabric/go-bacnet/npdu"
	"github.com/otfabric/go-bacnet/service"
)

func whoAmIRequest(t *testing.T) []byte {
	t.Helper()
	payload, err := service.EncodeWhoAmI(service.WhoAmI{VendorID: 999, ModelName: "VAV-2", SerialNumber: "SN-0042"})
	if err != nil {
		t.Fatal(err)
	}
	return apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{ServiceChoice: apdu.ServiceWhoAmI, Payload: payload})
}

func waitWhoAmI(t *testing.T, got <-chan WhoAmIDelivery) WhoAmIDelivery {
	t.Helper()
	select {
	case d := <-got:
		return d
	case <-time.After(time.Second):
		t.Fatal("no Who-Am-I delivered")
	}
	return WhoAmIDelivery{}
}

func TestWhoAmIReachesTheHandler(t *testing.T) {
	got := make(chan WhoAmIDelivery, 4)
	env := newVirtualPair(t, WithWhoAmIHandler(func(d WhoAmIDelivery) { got <- d }))

	// Broadcast on the local network: the sender's B/IP address.
	injectBroadcastNPDU(t, env.ClientTr, env.Peer, time.Now(), whoAmIRequest(t))
	d := waitWhoAmI(t, got)
	if d.Request.VendorID != 999 || d.Request.ModelName != "VAV-2" || d.Request.SerialNumber != "SN-0042" {
		t.Fatalf("request %+v", d.Request)
	}
	if !d.Address.Equal(env.Target.Address) || d.Immediate != env.Peer {
		t.Fatalf("from %v via %v, want %v via %v", d.Address, d.Immediate, env.Target.Address, env.Peer)
	}

	// From an MS/TP station behind a router: its network and station address, the router as the immediate peer.
	station := bacnet.RemoteStation(5, bacnet.MustMAC([]byte{12}))
	injectUnicastNPDUWithSource(t, env.ClientTr, env.Peer, time.Now(), npdu.NPDU{Version: npdu.Version1, Source: station, APDU: whoAmIRequest(t)})
	d = waitWhoAmI(t, got)
	if !d.Address.Equal(station) || d.Immediate != env.Peer {
		t.Fatalf("routed Who-Am-I from %v via %v", d.Address, d.Immediate)
	}

	// A You-Are answers it at that address.
	before := len(env.ClientTr.Outbox())
	payload, err := service.EncodeYouAre(service.YouAre{VendorID: 999, ModelName: "VAV-2", SerialNumber: "SN-0042",
		Device: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 2042}})
	if err != nil {
		t.Fatal(err)
	}
	if tgt := d.Target(); tgt.Endpoint != env.Peer || !tgt.Address.Equal(station) {
		t.Fatalf("Target of a routed Who-Am-I %+v, want %v through the router %v", tgt, station, env.Peer)
	}
	if err := env.Client.InvokeUnconfirmed(context.Background(), d.Target(), apdu.ServiceYouAre, payload); err != nil {
		t.Fatal(err)
	}
	if n := sentNPDU(t, env, before); !n.Destination.Equal(station) {
		t.Fatalf("You-Are sent to %v, want %v", n.Destination, station)
	}
}

func TestWhoAmIMalformedOrAPanickingHandler(t *testing.T) {
	got := make(chan WhoAmIDelivery, 4)
	calls := 0
	var diagMu sync.Mutex
	kinds := map[string]int{}
	env := newVirtualPair(t, WithDiagnosticFunc(func(d Diagnostic) {
		diagMu.Lock()
		defer diagMu.Unlock()
		if d.Kind == "malformed" || d.Message == "Who-Am-I handler panic" {
			kinds[d.Kind+"/"+d.Message]++
		}
	}), WithWhoAmIHandler(func(d WhoAmIDelivery) {
		calls++ // the receive loop calls the handler from one goroutine
		if calls == 1 {
			panic("handler fault")
		}
		got <- d
	}))
	bad := apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{ServiceChoice: apdu.ServiceWhoAmI, Payload: []byte{0x21}})
	injectBroadcastNPDU(t, env.ClientTr, env.Peer, time.Now(), bad)              // malformed: reported, not delivered
	injectBroadcastNPDU(t, env.ClientTr, env.Peer, time.Now(), whoAmIRequest(t)) // the handler panics: recovered
	injectBroadcastNPDU(t, env.ClientTr, env.Peer, time.Now(), whoAmIRequest(t))
	if d := waitWhoAmI(t, got); d.Request.SerialNumber != "SN-0042" {
		t.Fatalf("after a malformed request and a panic: %+v", d)
	}
	diagMu.Lock()
	malformed, panics := 0, kinds["unexpected_apdu/Who-Am-I handler panic"]
	for k, n := range kinds {
		if strings.HasPrefix(k, "malformed/") {
			malformed += n
		}
	}
	diagMu.Unlock()
	if malformed != 1 || panics != 1 {
		t.Fatalf("diagnostics %v, want one malformed and one handler panic (unexpected_apdu)", kinds)
	}
	select {
	case d := <-got:
		t.Fatalf("one more delivery than requests handled: %+v", d)
	case <-time.After(50 * time.Millisecond):
	}
	// Without a handler a Who-Am-I is ignored.
	env.Client.SetWhoAmIHandler(nil)
	injectBroadcastNPDU(t, env.ClientTr, env.Peer, time.Now(), whoAmIRequest(t))
	select {
	case d := <-got:
		t.Fatalf("delivered with no handler: %+v", d)
	case <-time.After(50 * time.Millisecond):
	}
}

// A Who-Am-I a BBMD forwarded is answered at the sender's own B/IP address,
// not at the BBMD (the sender is a local station on the BBMD's subnet).
func TestWhoAmIForwardedByABBMD(t *testing.T) {
	got := make(chan WhoAmIDelivery, 1)
	env := newVirtualPair(t, WithWhoAmIHandler(func(d WhoAmIDelivery) { got <- d }))
	bbmd := env.Peer
	sender := bip.NewEndpoint(netip.MustParseAddrPort("10.0.7.21:47808"))
	n, err := npdu.Append(nil, npdu.NPDU{Version: npdu.Version1, APDU: whoAmIRequest(t)})
	if err != nil {
		t.Fatal(err)
	}
	frame, err := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionForwardedNPDU,
		OriginIP: sender.Addr.Addr().As4(), OriginPort: sender.Addr.Port(), Payload: n})
	if err != nil {
		t.Fatal(err)
	}
	env.ClientTr.Inject(virtual.InboundPacket{Data: frame, ImmediatePeer: bbmd, ReceivedAt: env.Clk.Now()})
	d := waitWhoAmI(t, got)
	if d.Origin != sender || d.Immediate != bbmd {
		t.Fatalf("origin %v via %v, want %v via %v", d.Origin, d.Immediate, sender, bbmd)
	}
	tgt := d.Target()
	if tgt.Endpoint != sender || tgt.Address.Scope() != bacnet.AddressLocalStation {
		t.Fatalf("Target %+v, want the sender %v as a local station", tgt, sender)
	}
	before := len(env.ClientTr.Outbox())
	payload, err := service.EncodeYouAre(service.YouAre{VendorID: 999, ModelName: "VAV-2", SerialNumber: "SN-0042",
		Device: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 2042}})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Client.InvokeUnconfirmed(context.Background(), tgt, apdu.ServiceYouAre, payload); err != nil {
		t.Fatal(err)
	}
	sentNPDU(t, env, before)
	if out := env.ClientTr.Outbox()[before]; out.Destination != sender {
		t.Fatalf("You-Are sent to %v, want %v (not the BBMD)", out.Destination, sender)
	}
}

// SetWhoAmIHandler may run while requests arrive (go test -race).
func TestSetWhoAmIHandlerDuringReception(t *testing.T) {
	var n atomic.Int64
	env := newVirtualPair(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 200 {
			if i%2 == 0 {
				env.Client.SetWhoAmIHandler(func(WhoAmIDelivery) { n.Add(1) })
			} else {
				env.Client.SetWhoAmIHandler(nil)
			}
		}
	}()
	for range 200 {
		injectBroadcastNPDU(t, env.ClientTr, env.Peer, time.Now(), whoAmIRequest(t))
	}
	<-done
	// Requests still queued from the loop may reach this handler too; it
	// must not block the receive path.
	got := make(chan WhoAmIDelivery, 1)
	env.Client.SetWhoAmIHandler(func(d WhoAmIDelivery) {
		select {
		case got <- d:
		default:
		}
	})
	injectBroadcastNPDU(t, env.ClientTr, env.Peer, time.Now(), whoAmIRequest(t))
	waitWhoAmI(t, got)
}
