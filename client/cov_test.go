// SPDX-License-Identifier: MIT

package client

import (
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/internal/clock"
	"github.com/otfabric/go-bacnet/internal/diag"
	"github.com/otfabric/go-bacnet/service"
)

func TestCOVSendGapAndTerminalWithFullQueue(t *testing.T) {
	clk := clock.NewManual(time.Unix(0, 0).UTC())
	s := &subscription{
		c:        &Client{diag: diag.Discard{}, clock: clk},
		capacity: 1,
		events:   make(chan SubscriptionEvent, 2), // soft 1 + terminal
		done:     make(chan struct{}),
	}
	s.state.Store(uint32(SubscriptionActive))

	s.send(SubscriptionEvent{
		Notification: &service.COVNotification{},
		State:        SubscriptionActive,
	})
	// Overflow: gap sticky, no second event queued.
	s.send(SubscriptionEvent{
		Notification: &service.COVNotification{},
		State:        SubscriptionActive,
	})
	if SubscriptionState(s.state.Load()) != SubscriptionDegraded {
		t.Fatalf("state %v want Degraded", SubscriptionState(s.state.Load()))
	}
	if !s.gapPending {
		t.Fatal("expected sticky gap")
	}

	// Drain one, then deliver — Gap should be set.
	<-s.events
	s.send(SubscriptionEvent{
		Notification: &service.COVNotification{},
		State:        SubscriptionActive,
	})
	ev := <-s.events
	if !ev.Gap {
		t.Fatal("expected Gap on next delivered event")
	}
	if ev.Sequence == 0 {
		t.Fatal("expected non-zero sequence")
	}

	// Fill soft capacity again, then Closed must still admit.
	s.send(SubscriptionEvent{State: SubscriptionActive, Notification: &service.COVNotification{}})
	s.state.Store(uint32(SubscriptionClosed))
	s.publishClosed(nil)
	closed := false
	for ev := range s.events {
		if ev.State == SubscriptionClosed {
			closed = true
		}
	}
	if !closed {
		t.Fatal("terminal Closed not delivered")
	}
}

func TestCOVNeverOverwriteClosedWithDegraded(t *testing.T) {
	s := &subscription{
		c:        &Client{diag: diag.Discard{}},
		capacity: 1,
		events:   make(chan SubscriptionEvent, 2),
	}
	s.state.Store(uint32(SubscriptionClosed))
	s.send(SubscriptionEvent{State: SubscriptionDegraded, Err: bacnet.ErrDeliveryDropped})
	if SubscriptionState(s.state.Load()) != SubscriptionClosed {
		t.Fatal("Degraded overwrote Closed")
	}
	select {
	case <-s.events:
		t.Fatal("unexpected event after Closed")
	default:
	}
}

func TestCOVDeliverUsesSharedPathMatcher(t *testing.T) {
	remote := bacnet.RemoteStation(2, bacnet.MustMAC([]byte{10, 0, 0, 2, 0xBA, 0xC0}))
	hopA := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.1:47808"))
	hopB := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.2:47808"))
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}

	s := &subscription{
		c:        &Client{diag: diag.Discard{}},
		id:       7,
		target:   Target{Address: remote, Endpoint: hopA},
		object:   obj,
		capacity: 4,
		events:   make(chan SubscriptionEvent, 5),
	}
	s.state.Store(uint32(SubscriptionActive))
	m := newSubscriptionManager(diag.Discard{})
	m.register(s)

	note := &service.COVNotification{
		ProcessIdentifier: 7,
		MonitoredObject:   obj,
	}
	m.deliver(SubscriptionEvent{Notification: note, State: SubscriptionActive}, 7, packetSource{
		bacnetAddress: remote,
		immediate:     hopB,
	})
	select {
	case <-s.events:
		t.Fatal("accepted COV via wrong next hop")
	default:
	}

	m.deliver(SubscriptionEvent{Notification: note, State: SubscriptionActive}, 7, packetSource{
		bacnetAddress: remote,
		immediate:     hopA,
	})
	select {
	case <-s.events:
	default:
		t.Fatal("expected COV delivery for matching path")
	}
}

func TestCOVTryMarkDegradedNoOpWhenAlreadyDegraded(t *testing.T) {
	s := &subscription{
		c:        &Client{diag: diag.Discard{}},
		capacity: 1,
		events:   make(chan SubscriptionEvent, 2),
	}
	s.state.Store(uint32(SubscriptionDegraded))
	s.send(SubscriptionEvent{State: SubscriptionActive, Notification: &service.COVNotification{}})
	if SubscriptionState(s.state.Load()) != SubscriptionDegraded {
		t.Fatal("Degraded state should not change")
	}
}

func TestCOVSendAfterClosedFlag(t *testing.T) {
	s := &subscription{
		c:        &Client{diag: diag.Discard{}},
		capacity: 2,
		events:   make(chan SubscriptionEvent, 3),
	}
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.send(SubscriptionEvent{State: SubscriptionActive})
	select {
	case <-s.events:
		t.Fatal("unexpected event after closed flag")
	default:
	}
}

func TestCOVTerminalClosedWithFullChannel(t *testing.T) {
	s := &subscription{
		c:        &Client{diag: diag.Discard{}},
		capacity: 1,
		events:   make(chan SubscriptionEvent, 2),
	}
	s.state.Store(uint32(SubscriptionActive))
	s.events <- SubscriptionEvent{State: SubscriptionActive}
	s.events <- SubscriptionEvent{State: SubscriptionActive}
	s.publishClosed(nil)
	if !s.closed {
		t.Fatal("expected closed flag set when terminal cannot enqueue")
	}
	if _, ok := <-s.events; !ok {
		t.Fatal("expected first buffered event")
	}
	if _, ok := <-s.events; !ok {
		t.Fatal("expected second buffered event")
	}
	if _, ok := <-s.events; ok {
		t.Fatal("expected channel closed after draining buffered events")
	}
}

func TestSubscriptionManagerNextIDWrap(t *testing.T) {
	m := newSubscriptionManager(diag.Discard{})
	m.next = 0xFFFFFFFF
	if id := m.nextID(); id != 0xFFFFFFFF {
		t.Fatalf("first id=%d", id)
	}
	if id := m.nextID(); id != 1 {
		t.Fatalf("wrapped id=%d", id)
	}
}

func TestCOVDeliverUnknownProcessID(t *testing.T) {
	m := newSubscriptionManager(diag.Discard{})
	note := &service.COVNotification{ProcessIdentifier: 99, MonitoredObject: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}}
	m.deliver(SubscriptionEvent{Notification: note, State: SubscriptionActive}, 99, packetSource{})
}

func TestCOVDeliverObjectMismatch(t *testing.T) {
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	s := &subscription{
		c:        &Client{diag: diag.Discard{}},
		id:       3,
		object:   obj,
		capacity: 2,
		events:   make(chan SubscriptionEvent, 3),
	}
	s.state.Store(uint32(SubscriptionActive))
	m := newSubscriptionManager(diag.Discard{})
	m.register(s)

	wrongObj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 2}
	note := &service.COVNotification{ProcessIdentifier: 3, MonitoredObject: wrongObj}
	m.deliver(SubscriptionEvent{Notification: note, State: SubscriptionActive}, 3, packetSource{
		bacnetAddress: bacnet.LocalStation(bacnet.MustMAC([]byte{10, 0, 0, 2, 0xBA, 0xC0})),
		immediate:     bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.2:47808")),
	})
	select {
	case <-s.events:
		t.Fatal("object mismatch should not deliver")
	default:
	}
}

func TestWrapOutcomeUnknownSideEffects(t *testing.T) {
	cause := bacnet.ErrTimeout
	err := wrapOutcomeUnknown(apdu.ServiceWriteProperty, cause)
	var unknown *bacnet.OutcomeUnknownError
	if !errors.As(err, &unknown) || unknown.Operation != "WriteProperty" {
		t.Fatalf("WriteProperty wrap: %#v", err)
	}
	err = wrapOutcomeUnknown(apdu.ServiceWritePropertyMultiple, cause)
	if !errors.As(err, &unknown) || unknown.Operation != "WritePropertyMultiple" {
		t.Fatalf("WritePropertyMultiple wrap: %#v", err)
	}
	err = wrapOutcomeUnknown(apdu.ServiceSubscribeCOV, cause)
	if !errors.As(err, &unknown) || unknown.Operation != "SubscribeCOV" {
		t.Fatalf("SubscribeCOV wrap: %#v", err)
	}
	err = wrapOutcomeUnknown(apdu.ServiceSubscribeCOVProperty, cause)
	if !errors.As(err, &unknown) || unknown.Operation != "SubscribeCOVProperty" {
		t.Fatalf("SubscribeCOVProperty wrap: %#v", err)
	}
	err = wrapOutcomeUnknown(apdu.ServiceAcknowledgeAlarm, cause)
	if !errors.As(err, &unknown) || unknown.Operation != "AcknowledgeAlarm" {
		t.Fatalf("AcknowledgeAlarm wrap: %#v", err)
	}
	err = wrapOutcomeUnknown(apdu.ServiceDeviceCommunicationControl, cause)
	if !errors.As(err, &unknown) || unknown.Operation != "DeviceCommunicationControl" {
		t.Fatalf("DCC wrap: %#v", err)
	}
	err = wrapOutcomeUnknown(apdu.ServiceReinitializeDevice, cause)
	if !errors.As(err, &unknown) || unknown.Operation != "ReinitializeDevice" {
		t.Fatalf("Reinit wrap: %#v", err)
	}
	plain := wrapOutcomeUnknown(apdu.ServiceReadProperty, cause)
	if !errors.Is(plain, cause) {
		t.Fatalf("ReadProperty should stay plain: %#v", plain)
	}
	unknown = nil
	if errors.As(plain, &unknown) {
		t.Fatalf("ReadProperty should not wrap: %#v", plain)
	}
}
