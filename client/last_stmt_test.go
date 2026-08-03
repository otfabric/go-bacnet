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

func TestOperationClassEnablement(t *testing.T) {
	env := newVirtualPair(t)
	if !env.Client.operationClassEnabled(OperationNormal) || !env.Client.operationClassEnabled(OperationWrite) {
		t.Fatal("normal/write")
	}
	if !env.Client.operationClassEnabled(OperationLifeSafety) {
		t.Fatal("life safety")
	}
	if env.Client.operationClassEnabled(OperationDeviceManagement) {
		t.Fatal("device mgmt should be disabled")
	}
	if env.Client.operationClassEnabled(OperationNetworkManagement) {
		t.Fatal("network mgmt should be disabled")
	}
	if env.Client.operationClassEnabled(OperationClass(99)) {
		t.Fatal("unknown class")
	}
	env2 := newVirtualPair(t, WithDeviceManagementEnabled(DeviceManagementConfirm), WithNetworkManagementEnabled(NetworkManagementConfirm))
	if !env2.Client.operationClassEnabled(OperationDeviceManagement) || !env2.Client.operationClassEnabled(OperationNetworkManagement) {
		t.Fatal("opt-in classes")
	}
}

func TestWriteFileStreamEmptyAndUnsolicitedBVLC(t *testing.T) {
	env := newVirtualPair(t)
	outcomes, err := env.Client.WriteFileStream(context.Background(), env.Target,
		bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 1}, 0, nil, FileWriteOptions{})
	if err != nil || len(outcomes) != 0 {
		t.Fatalf("%v %#v", err, outcomes)
	}

	clk := clock.NewManual(time.Now())
	local := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.1:47808"))
	peer := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.2:47808"))
	tr := virtual.New(local, clk, 8)
	c, err := New(WithTransport(AdaptVirtual(tr)), withClock(clk))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	frame, _ := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionReadBroadcastDistributionTableAck, Payload: nil})
	tr.Inject(virtual.InboundPacket{Data: frame, ImmediatePeer: peer, ReceivedAt: clk.Now()})
	time.Sleep(20 * time.Millisecond)
	_ = c.handleBVLCManagement(bvlc.Message{Function: bvlc.FunctionDistributeBroadcastToNetwork}, peer)
}

func TestBusyExpirySelectsRoute(t *testing.T) {
	r := newRouterCache()
	r.busyDuration = time.Second
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ep := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.1:47808"))
	r.upsertLearned(3, ep, bip.Endpoint{}, now)
	r.markBusy([]uint16{3}, ep, now)
	if _, ok := r.selectNextHop(3, now); ok {
		t.Fatal("busy")
	}
	if hop, ok := r.selectNextHop(3, now.Add(2*time.Second)); !ok || !hop.Equal(ep) {
		t.Fatalf("expected available after busy expiry, got %v ok=%v", hop, ok)
	}
	// Duplicate learn + I-Am refresh path.
	r.upsertLearned(3, ep, bip.Endpoint{}, now.Add(3*time.Second))
}
