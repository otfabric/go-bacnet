// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/service"
)

func TestGetAlarmSummaryComplexACK(t *testing.T) {
	env := newVirtualPair(t)
	ackPayload, err := service.EncodeGetAlarmSummaryACK(service.GetAlarmSummaryACK{
		Entries: []service.AlarmSummaryEntry{{
			Object:           bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
			AlarmState:       2,
			AckedTransitions: bacnet.BitString{UnusedBits: 5, Bytes: []byte{0x80}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	errCh := make(chan error, 1)
	var got service.GetAlarmSummaryACK
	go func() {
		var e error
		got, e = env.Client.GetAlarmSummary(context.Background(), env.Target)
		errCh <- e
	}()
	invokeID, choice := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	if choice != apdu.ServiceGetAlarmSummary {
		t.Fatalf("service=%d", choice)
	}
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendComplexACK(nil, apdu.ComplexACK{
		InvokeID: invokeID, ServiceChoice: apdu.ServiceGetAlarmSummary, Payload: ackPayload,
	}))
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 1 {
		t.Fatalf("%+v", got)
	}
}

func TestGetEnrollmentSummaryComplexACK(t *testing.T) {
	env := newVirtualPair(t)
	ackPayload, err := service.EncodeGetEnrollmentSummaryACK(service.GetEnrollmentSummaryACK{
		Entries: []service.EnrollmentSummaryEntry{{
			Object:            bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeEventEnrollment, Instance: 1},
			EventType:         5,
			EventState:        1,
			Priority:          50,
			NotificationClass: 1,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	errCh := make(chan error, 1)
	var got service.GetEnrollmentSummaryACK
	go func() {
		var e error
		got, e = env.Client.GetEnrollmentSummary(context.Background(), env.Target, service.GetEnrollmentSummaryRequest{})
		errCh <- e
	}()
	invokeID, choice := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	if choice != apdu.ServiceGetEnrollmentSummary {
		t.Fatalf("service=%d", choice)
	}
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendComplexACK(nil, apdu.ComplexACK{
		InvokeID: invokeID, ServiceChoice: apdu.ServiceGetEnrollmentSummary, Payload: ackPayload,
	}))
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 1 || got.Entries[0].Priority != 50 {
		t.Fatalf("%+v", got)
	}
}

func TestSubscribeCOVPropertyMultipleSimpleACK(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go serveSimpleACK(ctx, env.PeerTr, env.Local)
	life := uint32(60)
	err := env.Client.SubscribeCOVPropertyMultiple(ctx, env.Target, service.SubscribeCOVPropertyMultipleRequest{
		SubscriberProcessIdentifier: 3,
		IssueConfirmedNotifications: false,
		LifetimeRemaining:           &life,
		Subscriptions: []service.COVMultipleSubscription{{
			Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
			Properties: []service.COVPropertyReference{{
				Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOpenEventStreamAndDeliver(t *testing.T) {
	env := newVirtualPair(t)
	stream := env.Client.OpenEventStream(2)
	defer stream.Close()
	env.Client.deliverEventNotification(service.EventNotification{
		EventType: 5,
		ToState:   2,
	}, false, packetSource{})
	select {
	case ev := <-stream.Events():
		if ev.Delivery.Notification.EventType != 5 || ev.Delivery.Confirmed {
			t.Fatalf("%+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestDefaultRetransmitPolicySummariesAndCOVMultiple(t *testing.T) {
	if DefaultRetransmitPolicy(apdu.ServiceGetAlarmSummary) != RetransmitEnabled {
		t.Fatal("alarm summary")
	}
	if DefaultRetransmitPolicy(apdu.ServiceGetEnrollmentSummary) != RetransmitEnabled {
		t.Fatal("enrollment summary")
	}
	if DefaultRetransmitPolicy(apdu.ServiceSubscribeCOVPropertyMultiple) != RetransmitDisabled {
		t.Fatal("cov property multiple")
	}
}
