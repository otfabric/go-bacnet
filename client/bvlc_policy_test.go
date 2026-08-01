// SPDX-License-Identifier: MIT

package client

import (
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/internal/diag"
	"github.com/otfabric/go-bacnet/internal/virtual"
	"github.com/otfabric/go-bacnet/npdu"
	"github.com/otfabric/go-bacnet/service"
)

func TestInboundIPv6PeerDoesNotPanic(t *testing.T) {
	var mu sync.Mutex
	var kinds []string
	env := newVirtualPair(t, WithDiagnosticFunc(func(d Diagnostic) {
		mu.Lock()
		kinds = append(kinds, d.Kind)
		mu.Unlock()
	}))

	// NPDU without SADR forces bipMACAddress on ImmediatePeer.
	nraw, err := npdu.Append(nil, npdu.NPDU{
		Version: npdu.Version1,
		APDU: apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{
			ServiceChoice: apdu.ServiceWhoIs,
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	frame, err := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionOriginalUnicastNPDU, Payload: nraw})
	if err != nil {
		t.Fatal(err)
	}
	v6 := bip.NewEndpoint(netip.MustParseAddrPort("[2001:db8::1]:47808"))
	if v6.IsValid() {
		t.Fatal("IPv6 endpoint must not be IsValid in Horizon 1")
	}
	env.ClientTr.Inject(virtual.InboundPacket{
		Data:          frame,
		ImmediatePeer: v6,
		ReceivedAt:    env.Clk.Now(),
	})
	time.Sleep(20 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, k := range kinds {
		if k == string(diag.KindBVLC) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected BVLC diagnostic, got %#v", kinds)
	}
	if len(env.Client.Devices()) != 0 {
		t.Fatal("IPv6 peer must not create observations")
	}
}

func TestInboundDBTNIgnored(t *testing.T) {
	var mu sync.Mutex
	var kinds []string
	env := newVirtualPair(t, WithDiagnosticFunc(func(d Diagnostic) {
		mu.Lock()
		kinds = append(kinds, d.Kind)
		mu.Unlock()
	}))

	payload, err := service.EncodeIAm(service.IAm{
		Device:        bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 42},
		MaxAPDULength: 480,
		Segmentation:  0,
		VendorID:      1,
	})
	if err != nil {
		t.Fatal(err)
	}
	apduBytes := apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{
		ServiceChoice: apdu.ServiceIAm,
		Payload:       payload,
	})
	nraw, err := npdu.Append(nil, npdu.NPDU{Version: npdu.Version1, APDU: apduBytes})
	if err != nil {
		t.Fatal(err)
	}
	frame, err := bvlc.Append(nil, bvlc.Message{
		Function: bvlc.FunctionDistributeBroadcastToNetwork,
		Payload:  nraw,
	})
	if err != nil {
		t.Fatal(err)
	}
	env.ClientTr.Inject(virtual.InboundPacket{
		Data:          frame,
		ImmediatePeer: env.Peer,
		ReceivedAt:    env.Clk.Now(),
	})
	time.Sleep(20 * time.Millisecond)

	if len(env.Client.Devices()) != 0 {
		t.Fatalf("DBTN must not populate registry: %#v", env.Client.Devices())
	}
	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, k := range kinds {
		if k == string(diag.KindBVLC) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected BVLC diagnostic for inbound DBTN, got %#v", kinds)
	}
}
