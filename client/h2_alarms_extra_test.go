// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/service"
)

func TestReinitializeDeviceSimpleACK(t *testing.T) {
	env := newVirtualPair(t, WithDeviceManagementEnabled(DeviceManagementConfirm))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go serveSimpleACK(ctx, env.PeerTr, env.Local)
	if err := env.Client.ReinitializeDevice(ctx, env.Target, service.ReinitializeDeviceRequest{
		State: service.ReinitializedWarmstart,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestDeviceManagementWrongConfirm(t *testing.T) {
	env := newVirtualPair(t, WithDeviceManagementEnabled("nope"))
	if err := env.Client.DeviceCommunicationControl(context.Background(), env.Target, service.DeviceCommunicationControlRequest{
		EnableDisable: service.EnableDisableEnable,
	}); !errors.Is(err, bacnet.ErrDeviceManagementDisabled) {
		t.Fatalf("%v", err)
	}
}

func TestAcknowledgeAlarmProtocolViolation(t *testing.T) {
	env := newVirtualPair(t)
	errCh := make(chan error, 1)
	go func() {
		errCh <- env.Client.AcknowledgeAlarm(context.Background(), env.Target, service.AcknowledgeAlarmRequest{
			ProcessIdentifier:    1,
			EventObject:          bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
			EventStateAcked:      1,
			TimeStamp:            service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 1},
			AcknowledgmentSource: bacnet.CharacterString{Value: "ops"},
			TimeOfAcknowledgment: service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 2},
		})
	}()
	invokeID, _ := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendComplexACK(nil, apdu.ComplexACK{
		InvokeID: invokeID, ServiceChoice: apdu.ServiceAcknowledgeAlarm, Payload: nil,
	}))
	if err := <-errCh; !errors.Is(err, bacnet.ErrProtocolViolation) {
		t.Fatalf("%v", err)
	}
}

func TestGetEventInformationProtocolViolation(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go serveSimpleACK(ctx, env.PeerTr, env.Local)
	_, err := env.Client.GetEventInformation(ctx, env.Target, service.GetEventInformationRequest{})
	if !errors.Is(err, bacnet.ErrProtocolViolation) {
		t.Fatalf("%v", err)
	}
}

func TestSetEventNotificationHandlerNilClears(t *testing.T) {
	var calls int
	env := newVirtualPair(t, WithEventNotificationHandler(func(EventNotificationDelivery) { calls++ }))
	env.Client.SetEventNotificationHandler(nil)
	note := service.EventNotification{
		ProcessIdentifier: 1,
		InitiatingDevice:  bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		EventObject:       bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
		TimeStamp:         service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 1},
		NotificationClass: 1,
		Priority:          1,
		NotifyType:        0,
		ToState:           1,
	}
	payload, err := service.EncodeEventNotification(note)
	if err != nil {
		t.Fatal(err)
	}
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{
		ServiceChoice: apdu.ServiceUnconfirmedEventNotification,
		Payload:       payload,
	}))
	time.Sleep(50 * time.Millisecond)
	if calls != 0 {
		t.Fatalf("handler should be cleared, calls=%d", calls)
	}
}

func TestAcknowledgeAlarmOutcomeUnknown(t *testing.T) {
	env := newVirtualPair(t, WithTransactionOptions(80*time.Millisecond, 0, 0))
	errCh := make(chan error, 1)
	go func() {
		errCh <- env.Client.AcknowledgeAlarm(context.Background(), env.Target, service.AcknowledgeAlarmRequest{
			ProcessIdentifier:    1,
			EventObject:          bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
			EventStateAcked:      1,
			TimeStamp:            service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 1},
			AcknowledgmentSource: bacnet.CharacterString{Value: "ops"},
			TimeOfAcknowledgment: service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 2},
		})
	}()
	_, _ = waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	env.Clk.Advance(200 * time.Millisecond)
	err := <-errCh
	var unknown *bacnet.OutcomeUnknownError
	if !errors.As(err, &unknown) || unknown.Operation != "AcknowledgeAlarm" {
		t.Fatalf("%v", err)
	}
}

func TestDeviceCommunicationControlOutcomeUnknown(t *testing.T) {
	env := newVirtualPair(t, WithDeviceManagementEnabled(DeviceManagementConfirm), WithTransactionOptions(80*time.Millisecond, 0, 0))
	errCh := make(chan error, 1)
	go func() {
		errCh <- env.Client.DeviceCommunicationControl(context.Background(), env.Target, service.DeviceCommunicationControlRequest{
			EnableDisable: service.EnableDisableEnable,
		})
	}()
	_, _ = waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	env.Clk.Advance(200 * time.Millisecond)
	err := <-errCh
	var unknown *bacnet.OutcomeUnknownError
	if !errors.As(err, &unknown) || unknown.Operation != "DeviceCommunicationControl" {
		t.Fatalf("%v", err)
	}
}

func TestReinitializeDeviceOutcomeUnknown(t *testing.T) {
	env := newVirtualPair(t, WithDeviceManagementEnabled(DeviceManagementConfirm), WithTransactionOptions(80*time.Millisecond, 0, 0))
	errCh := make(chan error, 1)
	go func() {
		errCh <- env.Client.ReinitializeDevice(context.Background(), env.Target, service.ReinitializeDeviceRequest{
			State: service.ReinitializedColdstart,
		})
	}()
	_, _ = waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	env.Clk.Advance(200 * time.Millisecond)
	err := <-errCh
	var unknown *bacnet.OutcomeUnknownError
	if !errors.As(err, &unknown) || unknown.Operation != "ReinitializeDevice" {
		t.Fatalf("%v", err)
	}
}

func TestConfirmedEventNotificationMalformed(t *testing.T) {
	env := newVirtualPair(t)
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendConfirmedRequest(nil, apdu.ConfirmedRequest{
		InvokeID: 4, ServiceChoice: apdu.ServiceConfirmedEventNotification, MaxAPDU: 5, Payload: []byte{0x09},
	}))
	time.Sleep(30 * time.Millisecond) // must not panic
}
