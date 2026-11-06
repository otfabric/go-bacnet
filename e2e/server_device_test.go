// SPDX-License-Identifier: MIT

package e2e_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bbmd"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/server"
	"github.com/otfabric/go-bacnet/service"
)

// hookLog records what the server's hooks were called with.
type hookLog struct {
	mu    sync.Mutex
	calls []string
}

func (h *hookLog) add(s string) {
	h.mu.Lock()
	h.calls = append(h.calls, s)
	h.mu.Unlock()
}

func (h *hookLog) has(s string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range h.calls {
		if c == s {
			return true
		}
	}
	return false
}

// TestServerDeviceManagementAndHooks drives the device-management and
// hook-only services over UDP: DeviceCommunicationControl,
// ReinitializeDevice, time synchronization, private transfer, text
// messages and WriteGroup.
func TestServerDeviceManagementAndHooks(t *testing.T) {
	log := &hookLog{}
	s := startServer(t, server.WithPassword("secret"), server.WithHooks(server.Hooks{
		Reinitialize: func(_ context.Context, _ service.ReinitializedStateOfDevice) error {
			log.add("reinit")
			return nil
		},
		TimeSync: func(_ service.TimeSynchronization, utc bool) {
			if utc {
				log.add("utc-timesync")
			} else {
				log.add("timesync")
			}
		},
		PrivateTransfer: func(_ context.Context, p service.PrivateTransfer, confirmed bool) ([]bacnet.Element, error) {
			if confirmed {
				log.add("private-confirmed")
				return []bacnet.Element{{Value: bacnet.UnsignedValue(uint64(p.ServiceNumber) + 1)}}, nil
			}
			log.add("private-unconfirmed")
			return nil, nil
		},
		TextMessage: func(_ context.Context, m service.TextMessage, confirmed bool) error {
			if confirmed {
				log.add("text-confirmed:" + m.Message)
			} else {
				log.add("text-unconfirmed:" + m.Message)
			}
			return nil
		},
		WriteGroup: func(_ context.Context, _ service.WriteGroup) { log.add("writegroup") },
	}))
	c, err := client.New(
		client.WithLocalAddr("127.0.0.1:0"),
		client.WithTransactionOptions(300*time.Millisecond, 0, time.Second),
		client.WithDeviceManagementEnabled(client.DeviceManagementConfirm),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	ctx := ctxT(t)
	target := serverTarget(s)
	ep := s.Local()
	secret := bacnet.CharacterString{Value: "secret"}
	wrong := bacnet.CharacterString{Value: "wrong"}

	if err := c.ReinitializeDevice(ctx, target, service.ReinitializeDeviceRequest{State: service.ReinitializedWarmstart, Password: &wrong}); err == nil {
		t.Fatal("wrong password accepted")
	} else {
		remoteError(t, err, bacnet.ErrorClassSecurity, bacnet.ErrorCodePasswordFailure)
	}
	if err := c.ReinitializeDevice(ctx, target, service.ReinitializeDeviceRequest{State: service.ReinitializedWarmstart, Password: &secret}); err != nil {
		t.Fatal(err)
	}
	if !log.has("reinit") {
		t.Fatal("Reinitialize hook not called")
	}

	// DISABLE is refused (clause 16.1 deprecates it); DISABLE_INITIATION
	// and ENABLE are acknowledged.
	err = c.DeviceCommunicationControl(ctx, target, service.DeviceCommunicationControlRequest{EnableDisable: service.EnableDisableDisable, Password: &secret})
	remoteError(t, err, bacnet.ErrorClassServices, bacnet.ErrorCodeServiceRequestDenied)
	minutes := uint16(1)
	if err := c.DeviceCommunicationControl(ctx, target, service.DeviceCommunicationControlRequest{
		EnableDisable: service.EnableDisableDisableInitiation, TimeDuration: &minutes, Password: &secret,
	}); err != nil {
		t.Fatal(err)
	}
	if err := c.DeviceCommunicationControl(ctx, target, service.DeviceCommunicationControlRequest{EnableDisable: service.EnableDisableEnable, Password: &secret}); err != nil {
		t.Fatal(err)
	}

	ts := service.TimeSynchronization{Date: bacnet.Date{Year: 126, Month: 11, Day: 6, Weekday: 5}, Time: bacnet.Time{Hour: 12}}
	if err := c.TimeSynchronization(ctx, ep, false, ts); err != nil {
		t.Fatal(err)
	}
	if err := c.UTCTimeSynchronization(ctx, ep, false, ts); err != nil {
		t.Fatal(err)
	}
	pt := service.PrivateTransfer{VendorID: 999, ServiceNumber: 7, ServiceParameters: []bacnet.Element{{Value: bacnet.UnsignedValue(42)}}}
	if err := c.ConfirmedPrivateTransfer(ctx, target, pt); err != nil {
		t.Fatal(err)
	}
	if err := c.UnconfirmedPrivateTransfer(ctx, ep, false, pt); err != nil {
		t.Fatal(err)
	}
	msg := service.TextMessage{TextMessageSourceDevice: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 4000}, Message: "hello"}
	if err := c.ConfirmedTextMessage(ctx, target, msg); err != nil {
		t.Fatal(err)
	}
	if err := c.UnconfirmedTextMessage(ctx, ep, false, msg); err != nil {
		t.Fatal(err)
	}
	inhibit := false
	if err := c.WriteGroup(ctx, ep, false, service.WriteGroup{GroupNumber: 1, WritePriority: 8, InhibitDelay: &inhibit,
		ChangeList: []bacnet.Element{
			{Context: true, TagNumber: 0, Value: bacnet.UnsignedValue(1)},
			{Context: true, TagNumber: 2, Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{{Value: bacnet.RealValue(1)}}}},
		}}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"timesync", "utc-timesync", "private-confirmed", "private-unconfirmed", "text-confirmed:hello", "text-unconfirmed:hello", "writegroup"} {
		waitFor(t, func() bool { return log.has(want) }, want)
	}
}

// TestServerCOVProperty subscribes to one property with
// SubscribeCOVProperty and to two with SubscribeCOVPropertyMultiple and
// expects the initial notifications.
func TestServerCOVProperty(t *testing.T) {
	s := startServer(t)
	c := newClient(t)
	ctx := ctxT(t)
	target := serverTarget(s)
	incr := float32(0.5)
	sub, err := c.SubscribeCOVProperty(ctx, target, av1, presentValue, client.COVOptions{Lifetime: 60, IssueConfirmed: true}, &incr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sub.Close() }()
	// The first events report the subscription state; the initial
	// notification follows.
	for got := false; !got; {
		select {
		case ev := <-sub.Events():
			if ev.Err != nil {
				t.Fatal(ev.Err)
			}
			if ev.Notification == nil {
				continue
			}
			if len(ev.Notification.Values) == 0 || ev.Notification.Values[0].Value.Real != 21.5 {
				t.Fatalf("initial notification %+v", ev.Notification)
			}
			got = true
		case <-ctx.Done():
			t.Fatal("no initial COV-property notification")
		}
	}
	lifetime := uint32(60)
	if err := c.SubscribeCOVPropertyMultiple(ctx, target, service.SubscribeCOVPropertyMultipleRequest{
		SubscriberProcessIdentifier: 9, LifetimeRemaining: &lifetime,
		Subscriptions: []service.COVMultipleSubscription{
			{Object: av1, Properties: []service.COVPropertyReference{{Property: presentValue}}},
			{Object: bv1, Properties: []service.COVPropertyReference{{Property: presentValue}, {Property: bacnet.PropertyReference{Identifier: bacnet.PropertyStatusFlags}}}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	v, err := c.ReadProperty(ctx, target, dev, bacnet.PropertyReference{Identifier: bacnet.PropertyActiveCOVSubscriptions})
	if err != nil || len(v.Elements) == 0 {
		t.Fatalf("Active_COV_Subscriptions: %v %+v", err, v)
	}
}

// TestServerBBMDTables reads and writes the BBMD tables of a server that
// is a BBMD: Read-BDT, Read-FDT with the registered client, a refused
// Write-BDT and Delete-Foreign-Device-Table-Entry.
func TestServerBBMDTables(t *testing.T) {
	tr, err := bip.ListenUDP(bip.UDPConfig{LocalAddr: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	self := tr.Local()
	b, err := bbmd.New(bbmd.Config{Self: self, BDT: []bvlc.BDTEntry{{Endpoint: self, Mask: bvlc.IPv4Mask{255, 255, 255, 255}}}})
	if err != nil {
		t.Fatal(err)
	}
	s := startServer(t, server.WithTransport(recordFrames(t, tr)), server.WithBBMD(b))
	c, err := client.New(
		client.WithLocalAddr("127.0.0.1:0"),
		client.WithTransactionOptions(300*time.Millisecond, 0, time.Second),
		client.WithForeignDevice(client.ForeignDeviceConfig{BBMD: s.Local(), TTL: time.Minute}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	waitFor(t, func() bool { return len(b.FDT()) == 1 }, "foreign device registration")
	ctx := ctxT(t)
	bdt, err := c.ReadBroadcastDistributionTable(ctx, s.Local())
	if err != nil || len(bdt) != 1 || !bdt[0].Endpoint.Equal(self) {
		t.Fatalf("Read-BDT: %v %+v", err, bdt)
	}
	fdt, err := c.ReadForeignDeviceTable(ctx, s.Local())
	if err != nil || len(fdt) != 1 || !fdt[0].Address.Equal(c.LocalEndpoint()) {
		t.Fatalf("Read-FDT: %v %+v", err, fdt)
	}
	// The BBMD's BDT is configuration, not writable over the network: NAK.
	if err := c.WriteBroadcastDistributionTable(ctx, s.Local(), bdt); err == nil {
		t.Fatal("Write-BDT accepted")
	}
	if err := c.DeleteForeignDeviceTableEntry(ctx, s.Local(), c.LocalEndpoint()); err != nil {
		t.Fatal(err)
	}
	if n := len(b.FDT()); n != 0 {
		t.Fatalf("FDT has %d entries after delete", n)
	}
}
