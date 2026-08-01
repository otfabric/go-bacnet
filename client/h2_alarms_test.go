// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/npdu"
	"github.com/otfabric/go-bacnet/service"
)

func TestAcknowledgeAlarmSimpleACK(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go serveSimpleACK(ctx, env.PeerTr, env.Local)

	err := env.Client.AcknowledgeAlarm(ctx, env.Target, service.AcknowledgeAlarmRequest{
		ProcessIdentifier:    1,
		EventObject:          bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
		EventStateAcked:      1,
		TimeStamp:            service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 1},
		AcknowledgmentSource: bacnet.CharacterString{Value: "ops"},
		TimeOfAcknowledgment: service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestGetEventInformationComplexACK(t *testing.T) {
	env := newVirtualPair(t)
	empty := bacnet.ApplicationValue{Kind: bacnet.ValueConstructed}
	ackPayload, err := service.EncodeGetEventInformationACK(service.GetEventInformationACK{
		Summaries: []service.EventSummary{{
			Object:                  bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
			EventState:              0,
			AcknowledgedTransitions: bacnet.BitString{UnusedBits: 5, Bytes: []byte{0x00}},
			EventTimeStamps:         empty,
			NotifyType:              0,
			EventEnable:             bacnet.BitString{UnusedBits: 5, Bytes: []byte{0xE0}},
			EventPriorities:         empty,
		}},
		MoreEvents: false,
	})
	if err != nil {
		t.Fatal(err)
	}

	errCh := make(chan error, 1)
	var got service.GetEventInformationACK
	go func() {
		var e error
		got, e = env.Client.GetEventInformation(context.Background(), env.Target, service.GetEventInformationRequest{})
		errCh <- e
	}()

	invokeID, choice := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	if choice != apdu.ServiceGetEventInformation {
		t.Fatalf("service=%d", choice)
	}
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendComplexACK(nil, apdu.ComplexACK{
		InvokeID:      invokeID,
		ServiceChoice: apdu.ServiceGetEventInformation,
		Payload:       ackPayload,
	}))
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if len(got.Summaries) != 1 || got.MoreEvents {
		t.Fatalf("%+v", got)
	}
}

func TestDeviceManagementOptIn(t *testing.T) {
	env := newVirtualPair(t)
	err := env.Client.DeviceCommunicationControl(context.Background(), env.Target, service.DeviceCommunicationControlRequest{
		EnableDisable: service.EnableDisableEnable,
	})
	if !errors.Is(err, bacnet.ErrDeviceManagementDisabled) {
		t.Fatalf("want disabled, got %v", err)
	}
	err = env.Client.ReinitializeDevice(context.Background(), env.Target, service.ReinitializeDeviceRequest{
		State: service.ReinitializedWarmstart,
	})
	if !errors.Is(err, bacnet.ErrDeviceManagementDisabled) {
		t.Fatalf("want disabled, got %v", err)
	}

	env2 := newVirtualPair(t, WithDeviceManagementEnabled(DeviceManagementConfirm))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go serveSimpleACK(ctx, env2.PeerTr, env2.Local)
	if err := env2.Client.DeviceCommunicationControl(ctx, env2.Target, service.DeviceCommunicationControlRequest{
		EnableDisable: service.EnableDisableEnable,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestEventNotificationConfirmedAndUnconfirmed(t *testing.T) {
	var got atomic.Int32
	env := newVirtualPair(t, WithEventNotificationHandler(func(d EventNotificationDelivery) {
		if d.Notification.ProcessIdentifier != 9 {
			t.Errorf("pid=%d", d.Notification.ProcessIdentifier)
		}
		if d.Confirmed {
			got.Add(1)
		} else {
			got.Add(10)
		}
	}))

	note := service.EventNotification{
		ProcessIdentifier: 9,
		InitiatingDevice:  bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 100},
		EventObject:       bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
		TimeStamp:         service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 1},
		NotificationClass: 1,
		Priority:          2,
		EventType:         0,
		NotifyType:        0,
		ToState:           1,
	}
	payload, err := service.EncodeEventNotification(note)
	if err != nil {
		t.Fatal(err)
	}

	// Confirmed: expect SimpleACK + handler.
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendConfirmedRequest(nil, apdu.ConfirmedRequest{
		InvokeID:      3,
		ServiceChoice: apdu.ServiceConfirmedEventNotification,
		MaxAPDU:       5,
		Payload:       payload,
	}))
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && got.Load()&1 == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if got.Load()&1 == 0 {
		t.Fatal("confirmed handler not called")
	}
	limits := bacnet.DefaultDecodeLimits()
	acked := false
	for _, pkt := range env.ClientTr.Outbox() {
		msg, err := bvlc.Parse(pkt.Data, limits)
		if err != nil {
			continue
		}
		n, _, err := npdu.Parse(msg.Payload, limits)
		if err != nil || len(n.APDU) == 0 {
			continue
		}
		pdu, err := apdu.Parse(n.APDU, limits)
		if err == nil && pdu.SimpleACK != nil && pdu.SimpleACK.InvokeID == 3 {
			acked = true
			break
		}
	}
	if !acked {
		t.Fatal("expected SimpleACK for confirmed EventNotification")
	}

	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{
		ServiceChoice: apdu.ServiceUnconfirmedEventNotification,
		Payload:       payload,
	}))
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) && got.Load() < 11 {
		time.Sleep(5 * time.Millisecond)
	}
	if got.Load() < 11 {
		t.Fatalf("unconfirmed handler not called: %d", got.Load())
	}
}

func TestDefaultRetransmitPolicyAlarmsAndDeviceMgmt(t *testing.T) {
	if DefaultRetransmitPolicy(apdu.ServiceAcknowledgeAlarm) != RetransmitDisabled {
		t.Fatal("AcknowledgeAlarm")
	}
	if DefaultRetransmitPolicy(apdu.ServiceGetEventInformation) != RetransmitEnabled {
		t.Fatal("GetEventInformation")
	}
	if DefaultRetransmitPolicy(apdu.ServiceDeviceCommunicationControl) != RetransmitDisabled {
		t.Fatal("DCC")
	}
	if DefaultRetransmitPolicy(apdu.ServiceReinitializeDevice) != RetransmitDisabled {
		t.Fatal("Reinit")
	}
}
