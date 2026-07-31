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
)

func TestWritePropertyRejectsComplexACK(t *testing.T) {
	env := newVirtualPair(t)
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogOutput, Instance: 2}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}

	errCh := make(chan error, 1)
	go func() {
		errCh <- env.Client.WriteProperty(context.Background(), env.Target, obj, prop, bacnet.RealValue(1.0), nil)
	}()

	invokeID, _ := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendComplexACK(nil, apdu.ComplexACK{
		InvokeID: invokeID, ServiceChoice: apdu.ServiceWriteProperty, Payload: []byte{0x01},
	}))

	if !errors.Is(<-errCh, bacnet.ErrProtocolViolation) {
		t.Fatal("expected protocol violation")
	}
}

func TestForeignDeviceRegistrationRejectedVirtual(t *testing.T) {
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
	defer func() { _ = c.Close() }()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		for _, pkt := range tr.Outbox() {
			msg, err := bvlc.Parse(pkt.Data, bacnet.DefaultDecodeLimits())
			if err != nil {
				continue
			}
			if msg.Function == bvlc.FunctionRegisterForeignDevice {
				result, err := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionResult, ResultCode: 0x0010})
				if err != nil {
					t.Fatal(err)
				}
				tr.Inject(virtual.InboundPacket{Data: result, ImmediatePeer: bbmd, ReceivedAt: clk.Now()})
				goto injected
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for Register-Foreign-Device")
injected:
	clk.Advance(6 * time.Second)
	if c.ForeignDeviceRegistered() {
		t.Fatal("expected rejected registration to stay unregistered")
	}
}

func TestForeignDeviceRegistrationTimeoutVirtual(t *testing.T) {
	clk := clock.NewManual(time.Unix(0, 0).UTC())
	local := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.5:47808"))
	bbmd := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.1:47808"))
	tr := virtual.New(local, clk, 16)
	c, err := New(
		WithTransport(AdaptVirtual(tr)),
		withClock(clk),
		WithForeignDevice(ForeignDeviceConfig{BBMD: bbmd, TTL: 60 * time.Second}),
		WithTransactionOptions(200*time.Millisecond, 0, 0),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	deadline := time.Now().Add(time.Second)
	sawRegister := false
	for time.Now().Before(deadline) {
		for _, pkt := range tr.Outbox() {
			msg, err := bvlc.Parse(pkt.Data, bacnet.DefaultDecodeLimits())
			if err != nil {
				continue
			}
			if msg.Function == bvlc.FunctionRegisterForeignDevice {
				sawRegister = true
				goto registerSent
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
registerSent:
	if !sawRegister {
		t.Fatal("expected Register-Foreign-Device")
	}
	clk.Advance(300 * time.Millisecond)
	time.Sleep(30 * time.Millisecond)
	if c.ForeignDeviceRegistered() {
		t.Fatal("expected timeout to leave client unregistered")
	}
}

func TestForeignDeviceBroadcastFrame(t *testing.T) {
	clk := clock.NewManual(time.Unix(0, 0).UTC())
	local := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.5:47808"))
	bbmd := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.1:47808"))
	tr := virtual.New(local, clk, 16)
	c, err := New(
		WithTransport(AdaptVirtual(tr)),
		withClock(clk),
		WithForeignDevice(ForeignDeviceConfig{BBMD: bbmd, TTL: 60 * time.Second}),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		for _, pkt := range tr.Outbox() {
			msg, err := bvlc.Parse(pkt.Data, bacnet.DefaultDecodeLimits())
			if err != nil {
				continue
			}
			if msg.Function == bvlc.FunctionRegisterForeignDevice {
				result, err := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionResult, ResultCode: 0})
				if err != nil {
					t.Fatal(err)
				}
				tr.Inject(virtual.InboundPacket{Data: result, ImmediatePeer: bbmd, ReceivedAt: clk.Now()})
				goto registered
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for Register-Foreign-Device")
registered:
	time.Sleep(30 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := c.SendWhoIs(ctx, bbmd, true, DiscoveryOptions{}); err != nil {
		t.Fatal(err)
	}
	out := tr.Outbox()
	if len(out) == 0 {
		t.Fatal("expected outbound")
	}
	msg, err := bvlc.Parse(out[len(out)-1].Data, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if msg.Function != bvlc.FunctionDistributeBroadcastToNetwork {
		t.Fatalf("function %v", msg.Function)
	}
}
