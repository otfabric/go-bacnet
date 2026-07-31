// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/internal/clock"
	"github.com/otfabric/go-bacnet/internal/virtual"
	"github.com/otfabric/go-bacnet/service"
)

func TestConfirmedResponseWrongServiceIgnored(t *testing.T) {
	env := newVirtualPair(t, WithTransactionOptions(500*time.Millisecond, 0, 0))
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogInput, Instance: 20}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}

	errCh := make(chan error, 1)
	go func() {
		_, err := env.Client.ReadProperty(context.Background(), env.Target, obj, prop)
		errCh <- err
	}()

	invokeID, _ := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	payload, _ := service.EncodeReadPropertyACK(service.ReadPropertyACK{
		Object:   obj,
		Property: prop,
		Value:    bacnet.RealValue(1.0),
	})
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendComplexACK(nil, apdu.ComplexACK{
		InvokeID: invokeID, ServiceChoice: apdu.ServiceWriteProperty, Payload: payload,
	}))

	env.Clk.Advance(600 * time.Millisecond)
	if !errors.Is(<-errCh, bacnet.ErrTimeout) {
		t.Fatal("expected timeout after wrong service choice")
	}
}

func TestConfirmedResponseMalformedErrorPDU(t *testing.T) {
	env := newVirtualPair(t)
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogInput, Instance: 21}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}

	errCh := make(chan error, 1)
	go func() {
		_, err := env.Client.ReadProperty(context.Background(), env.Target, obj, prop)
		errCh <- err
	}()

	invokeID, _ := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendError(nil, apdu.ErrorPDU{
		InvokeID: invokeID, ServiceChoice: apdu.ServiceReadProperty, Payload: []byte{0x00},
	}))

	err := <-errCh
	if !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("got %v", err)
	}
}

func TestHandleConfirmedIndicationNil(t *testing.T) {
	env := newVirtualPair(t)
	env.Client.handleConfirmedIndication(nil, packetSource{})
}

func TestHandleUnconfirmedNil(t *testing.T) {
	env := newVirtualPair(t)
	env.Client.handleUnconfirmed(nil, packetSource{})
}

func TestNewTxManagerClampsMax(t *testing.T) {
	if m := newTxManager(0, time.Now); m.max != 255 {
		t.Fatalf("zero max: got %d", m.max)
	}
	if m := newTxManager(999, time.Now); m.max != 255 {
		t.Fatalf("large max: got %d", m.max)
	}
}

func TestTryAllocExhaustedAndQuarantine(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := func() time.Time { return now }

	m := newTxManager(1, clk)
	tx := &pendingTx{invokeID: 1, result: make(chan txResult, 1), timer: nopTimer{}}
	m.register(tx)
	m.mu.Lock()
	if id, ok := m.tryAllocLocked(); ok {
		t.Fatalf("unexpected alloc id=%d when at max", id)
	}
	m.mu.Unlock()

	m = newTxManager(4, clk)
	m.quarantine[2] = now.Add(time.Hour)
	m.mu.Lock()
	id, ok := m.tryAllocLocked()
	m.mu.Unlock()
	if !ok || id == 2 {
		t.Fatalf("alloc=%d ok=%v with quarantine on 2", id, ok)
	}
}

func TestTxPhaseMissing(t *testing.T) {
	m := newTxManager(4, time.Now)
	if _, ok := m.phase(42); ok {
		t.Fatal("expected missing phase")
	}
}

func TestDefaultRetransmitPolicyUnknownService(t *testing.T) {
	if DefaultRetransmitPolicy(0xFE) != RetransmitDisabled {
		t.Fatal("unknown service should disable retransmission")
	}
}

func TestTxAbortAllWhenResultFull(t *testing.T) {
	m := newTxManager(4, time.Now)
	tx := &pendingTx{invokeID: 5, result: make(chan txResult, 1), timer: nopTimer{}}
	tx.result <- txResult{}
	m.register(tx)
	m.abortAll(bacnet.ErrClosed)
	select {
	case res := <-tx.result:
		if res.err != nil {
			t.Fatalf("full result channel should keep prior value, got %v", res.err)
		}
	default:
		t.Fatal("expected prior result to remain")
	}
}

func TestDiscoverUsesDefaultPort(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	local := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.1:47808"))
	tr := virtual.New(local, clk, 16)
	c, err := New(WithTransport(AdaptVirtual(tr)), withClock(clk), WithPort(0))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_, _ = c.Discover(ctx, DiscoveryOptions{})
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	<-done

	out := tr.Outbox()
	if len(out) == 0 {
		t.Fatal("expected Who-Is outbound")
	}
	msg, err := bvlc.Parse(out[0].Data, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if msg.Function != bvlc.FunctionOriginalBroadcastNPDU {
		t.Fatalf("function %v", msg.Function)
	}
}

func TestReadWriteRPMEncodeErrors(t *testing.T) {
	env := newVirtualPair(t)
	invalidObj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogInput, Instance: bacnet.MaxObjectInstance + 1}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}

	_, err := env.Client.ReadProperty(context.Background(), env.Target, invalidObj, prop)
	if !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("ReadProperty: %v", err)
	}
	err = env.Client.WriteProperty(context.Background(), env.Target, invalidObj, prop, bacnet.RealValue(1.0), nil)
	if !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("WriteProperty: %v", err)
	}
	_, err = env.Client.ReadPropertyMultiple(context.Background(), env.Target, []service.ReadAccessSpecification{{
		Object: invalidObj, Properties: []bacnet.PropertyReference{prop},
	}})
	if !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("ReadPropertyMultiple: %v", err)
	}
}

func TestVirtualClientLocalEndpoint(t *testing.T) {
	env := newVirtualPair(t)
	if got := env.Client.LocalEndpoint(); !got.Equal(env.Local) {
		t.Fatalf("LocalEndpoint=%v want %v", got, env.Local)
	}
}

func TestDeviceCapabilitiesMaxAPDUOrFallbackOnly(t *testing.T) {
	var caps DeviceCapabilities
	if caps.MaxAPDUOr(480) != 480 {
		t.Fatalf("got %d", caps.MaxAPDUOr(480))
	}
}

func TestResolveTargetExpiredRouter(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	local := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.2:47808"))
	router := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.1:47808"))
	tr := virtual.New(local, clk, 16)
	c, err := New(WithTransport(AdaptVirtual(tr)), withClock(clk))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	remoteMAC := bacnet.MustMAC([]byte{10, 0, 0, 5, 0xBA, 0xC0})
	remoteAddr := bacnet.RemoteStation(2, remoteMAC)
	c.routers.upsert(2, router, router, clk.Now())
	clk.Advance(11 * time.Minute)

	_, err = c.ResolveTarget(remoteAddr, bip.Endpoint{})
	if !errors.Is(err, bacnet.ErrUnsupported) {
		t.Fatalf("got %v", err)
	}
}

func TestWhoIsRouterClosedClient(t *testing.T) {
	env := newVirtualPair(t)
	if err := env.Client.Close(); err != nil {
		t.Fatal(err)
	}
	err := env.Client.WhoIsRouterToNetwork(context.Background(), nil)
	if !errors.Is(err, bacnet.ErrClosed) {
		t.Fatalf("got %v", err)
	}
}

func TestResolveTargetUnsupportedRemote(t *testing.T) {
	env := newVirtualPair(t)
	remote := bacnet.RemoteStation(99, bacnet.MustMAC([]byte{10, 0, 0, 9, 0xBA, 0xC0}))
	_, err := env.Client.ResolveTarget(remote, bip.Endpoint{})
	if !errors.Is(err, bacnet.ErrUnsupported) {
		t.Fatalf("got %v", err)
	}
}
