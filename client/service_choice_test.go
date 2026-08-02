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

func ackAlarmRequest() service.AcknowledgeAlarmRequest {
	return service.AcknowledgeAlarmRequest{
		ProcessIdentifier:    1,
		EventObject:          bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
		EventStateAcked:      1,
		TimeStamp:            service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 1},
		AcknowledgmentSource: bacnet.CharacterString{Value: "ops"},
		TimeOfAcknowledgment: service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 2},
	}
}

func TestWritePropertyRejectsSimpleACKServiceZero(t *testing.T) {
	env := newVirtualPair(t, WithTransactionOptions(400*time.Millisecond, 0, 0))
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}

	errCh := make(chan error, 1)
	go func() {
		errCh <- env.Client.WriteProperty(context.Background(), env.Target, obj, prop, bacnet.RealValue(1), nil)
	}()

	invokeID, _ := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendSimpleACK(nil, apdu.SimpleACK{
		InvokeID: invokeID, ServiceChoice: apdu.ServiceAcknowledgeAlarm, // 0
	}))

	env.Clk.Advance(500 * time.Millisecond)
	err := <-errCh
	var unknown *bacnet.OutcomeUnknownError
	if !errors.As(err, &unknown) || !errors.Is(err, bacnet.ErrTimeout) {
		t.Fatalf("got %v, want OutcomeUnknown timeout after wrong service 0", err)
	}
}

func TestAcknowledgeAlarmRejectsWrongSimpleACKService(t *testing.T) {
	env := newVirtualPair(t, WithTransactionOptions(400*time.Millisecond, 0, 0))
	errCh := make(chan error, 1)
	go func() {
		errCh <- env.Client.AcknowledgeAlarm(context.Background(), env.Target, ackAlarmRequest())
	}()

	invokeID, _ := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendSimpleACK(nil, apdu.SimpleACK{
		InvokeID: invokeID, ServiceChoice: apdu.ServiceWriteProperty, // 15
	}))

	env.Clk.Advance(500 * time.Millisecond)
	err := <-errCh
	var unknown *bacnet.OutcomeUnknownError
	if !errors.As(err, &unknown) || !errors.Is(err, bacnet.ErrTimeout) {
		t.Fatalf("got %v, want OutcomeUnknown timeout after wrong service", err)
	}
}

func TestAcknowledgeAlarmRejectsComplexACK(t *testing.T) {
	env := newVirtualPair(t, WithTransactionOptions(400*time.Millisecond, 0, 0))
	errCh := make(chan error, 1)
	go func() {
		errCh <- env.Client.AcknowledgeAlarm(context.Background(), env.Target, ackAlarmRequest())
	}()

	invokeID, _ := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendComplexACK(nil, apdu.ComplexACK{
		InvokeID: invokeID, ServiceChoice: apdu.ServiceReadPropertyMultiple, Payload: []byte{0x21, 0x01},
	}))

	env.Clk.Advance(500 * time.Millisecond)
	err := <-errCh
	var unknown *bacnet.OutcomeUnknownError
	if !errors.As(err, &unknown) || !errors.Is(err, bacnet.ErrTimeout) {
		t.Fatalf("got %v, want OutcomeUnknown timeout after ComplexACK for AckAlarm", err)
	}
}

func TestSegmentedComplexACKLaterServiceZeroRejected(t *testing.T) {
	env := newVirtualPair(t, WithTransactionOptions(300*time.Millisecond, 0, time.Second))
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 9}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}

	errCh := make(chan error, 1)
	go func() {
		_, err := env.Client.ReadProperty(context.Background(), env.Target, obj, prop)
		errCh <- err
	}()

	invokeID, _ := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	injectSegmentedComplexACK(t, env, invokeID, 0, true, apdu.ServiceReadProperty, []byte("part-a"))
	time.Sleep(10 * time.Millisecond)
	injectSegmentedComplexACK(t, env, invokeID, 1, false, apdu.ServiceAcknowledgeAlarm, []byte("part-b"))
	time.Sleep(20 * time.Millisecond)

	if !errors.Is(<-errCh, bacnet.ErrProtocolViolation) {
		t.Fatal("expected protocol violation for service 0 on later segment")
	}
}

func TestSegmentedComplexACKFirstServiceZeroRejected(t *testing.T) {
	env := newVirtualPair(t, WithTransactionOptions(300*time.Millisecond, 0, time.Second))
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 10}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}

	errCh := make(chan error, 1)
	go func() {
		_, err := env.Client.ReadProperty(context.Background(), env.Target, obj, prop)
		errCh <- err
	}()

	invokeID, _ := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	injectSegmentedComplexACK(t, env, invokeID, 0, true, apdu.ServiceAcknowledgeAlarm, []byte("part-a"))
	time.Sleep(20 * time.Millisecond)

	if !errors.Is(<-errCh, bacnet.ErrProtocolViolation) {
		t.Fatal("expected protocol violation for service 0 on first segment of RP")
	}
}

func TestEventNotificationHandlerPanicRecovered(t *testing.T) {
	env := newVirtualPair(t)
	env.Client.SetEventNotificationHandler(func(EventNotificationDelivery) {
		panic("handler boom")
	})
	env.Client.deliverEventNotification(service.EventNotification{
		ProcessIdentifier: 1,
		InitiatingDevice:  bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		EventObject:       bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
		TimeStamp:         service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 1},
	}, false, packetSource{
		bacnetAddress: env.Target.Address,
		immediate:     env.Peer,
	})
}
