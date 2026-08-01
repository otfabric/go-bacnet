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

func TestClientH2EncodeErrors(t *testing.T) {
	env := newVirtualPair(t, WithDeviceManagementEnabled(DeviceManagementConfirm))
	bad := bacnet.ObjectIdentifier{Type: 0x400, Instance: 0}
	if err := env.Client.AcknowledgeAlarm(context.Background(), env.Target, service.AcknowledgeAlarmRequest{
		EventObject:          bad,
		TimeStamp:            service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 1},
		AcknowledgmentSource: bacnet.CharacterString{Value: "x"},
		TimeOfAcknowledgment: service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 2},
	}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("ack: %v", err)
	}
	if _, err := env.Client.GetEventInformation(context.Background(), env.Target, service.GetEventInformationRequest{LastReceived: &bad}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("gei: %v", err)
	}
	long := bacnet.CharacterString{Value: string(make([]byte, 21))}
	if err := env.Client.DeviceCommunicationControl(context.Background(), env.Target, service.DeviceCommunicationControlRequest{
		EnableDisable: service.EnableDisableEnable,
		Password:      &long,
	}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("dcc: %v", err)
	}
	if err := env.Client.ReinitializeDevice(context.Background(), env.Target, service.ReinitializeDeviceRequest{
		State:    service.ReinitializedWarmstart,
		Password: &long,
	}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("reinit: %v", err)
	}
}

func TestDeviceManagementProtocolViolation(t *testing.T) {
	env := newVirtualPair(t, WithDeviceManagementEnabled(DeviceManagementConfirm))
	errCh := make(chan error, 1)
	go func() {
		errCh <- env.Client.DeviceCommunicationControl(context.Background(), env.Target, service.DeviceCommunicationControlRequest{
			EnableDisable: service.EnableDisableEnable,
		})
	}()
	invokeID, _ := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendComplexACK(nil, apdu.ComplexACK{
		InvokeID: invokeID, ServiceChoice: apdu.ServiceDeviceCommunicationControl,
	}))
	if err := <-errCh; !errors.Is(err, bacnet.ErrProtocolViolation) {
		t.Fatalf("%v", err)
	}

	since := len(env.ClientTr.Outbox())
	errCh2 := make(chan error, 1)
	go func() {
		errCh2 <- env.Client.ReinitializeDevice(context.Background(), env.Target, service.ReinitializeDeviceRequest{
			State: service.ReinitializedWarmstart,
		})
	}()
	invokeID, _ = waitConfirmedInvokeIDSince(t, env.ClientTr, since, time.Second)
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendComplexACK(nil, apdu.ComplexACK{
		InvokeID: invokeID, ServiceChoice: apdu.ServiceReinitializeDevice,
	}))
	if err := <-errCh2; !errors.Is(err, bacnet.ErrProtocolViolation) {
		t.Fatalf("%v", err)
	}
}

func TestUnconfirmedEventNotificationMalformed(t *testing.T) {
	env := newVirtualPair(t, WithEventNotificationHandler(func(EventNotificationDelivery) {
		t.Error("handler should not run")
	}))
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{
		ServiceChoice: apdu.ServiceUnconfirmedEventNotification,
		Payload:       []byte{0x09},
	}))
	time.Sleep(30 * time.Millisecond)
}
