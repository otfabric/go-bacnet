// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/npdu"
	"github.com/otfabric/go-bacnet/service"
)

// sentNPDU waits for the next frame the client sends after skip and returns its NPDU.
func sentNPDU(t *testing.T, env *virtualPair, skip int) npdu.NPDU {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if out := env.ClientTr.Outbox(); len(out) > skip {
			msg, err := bvlc.Parse(out[skip].Data, bacnet.DefaultDecodeLimits())
			if err != nil {
				t.Fatal(err)
			}
			if msg.Function != bvlc.FunctionOriginalUnicastNPDU {
				t.Fatalf("BVLC function %v, want Original-Unicast-NPDU", msg.Function)
			}
			n, _, err := npdu.Parse(msg.Payload, bacnet.DefaultDecodeLimits())
			if err != nil {
				t.Fatal(err)
			}
			return n
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("nothing sent")
	return npdu.NPDU{}
}

func TestInvokeUnconfirmedReachesARoutedDevice(t *testing.T) {
	env := newVirtualPair(t)
	ctx := context.Background()
	payload, err := service.EncodeTimeSynchronization(service.TimeSynchronization{
		Date: bacnet.Date{Year: 126, Month: 11, Day: 19, Weekday: 4}, Time: bacnet.Time{Hour: 8},
	})
	if err != nil {
		t.Fatal(err)
	}
	// A device on MS/TP network 5, station 12, behind the router at the peer endpoint.
	remote := bacnet.RemoteStation(5, bacnet.MustMAC([]byte{12}))
	before := len(env.ClientTr.Outbox())
	if err := env.Client.InvokeUnconfirmed(ctx, Target{Address: remote, Endpoint: env.Peer}, apdu.ServiceTimeSynchronization, payload); err != nil {
		t.Fatal(err)
	}
	n := sentNPDU(t, env, before)
	if !n.Destination.Equal(remote) || n.ExpectingReply {
		t.Fatalf("NPDU destination %v (expecting reply %v), want %v", n.Destination, n.ExpectingReply, remote)
	}
	pdu, err := apdu.Parse(n.APDU, bacnet.DefaultDecodeLimits())
	if err != nil || pdu.UnconfirmedRequest == nil || pdu.UnconfirmedRequest.ServiceChoice != apdu.ServiceTimeSynchronization {
		t.Fatalf("APDU %+v, %v", pdu, err)
	}
	if got, err := service.DecodeTimeSynchronization(pdu.UnconfirmedRequest.Payload, bacnet.DefaultDecodeLimits()); err != nil || got.Date.Day != 19 || got.Time.Hour != 8 {
		t.Fatalf("payload %+v, %v", got, err)
	}

	// A device on the local network: no DNET, as for a confirmed request.
	before = len(env.ClientTr.Outbox())
	if err := env.Client.InvokeUnconfirmed(ctx, Target{Endpoint: env.Peer}, apdu.ServiceUTCTimeSynchronization, payload); err != nil {
		t.Fatal(err)
	}
	if n := sentNPDU(t, env, before); n.Destination.Scope() == bacnet.AddressRemoteStation {
		t.Fatalf("a local device was addressed as remote: %v", n.Destination)
	}
}

func TestInvokeUnconfirmedRefusesATargetWithoutEndpoint(t *testing.T) {
	env := newVirtualPair(t)
	err := env.Client.InvokeUnconfirmed(context.Background(), Target{Endpoint: bip.Endpoint{}}, apdu.ServiceTimeSynchronization, nil)
	if !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("err = %v, want ErrMalformed", err)
	}
}
