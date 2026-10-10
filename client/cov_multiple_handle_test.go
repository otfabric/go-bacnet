// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/npdu"
	"github.com/otfabric/go-bacnet/service"
)

// sentCOVMultipleRequests returns the SubscribeCOVPropertyMultiple requests
// the client has sent so far.
func sentCOVMultipleRequests(t *testing.T, env *virtualPair) []service.SubscribeCOVPropertyMultipleRequest {
	t.Helper()
	limits := bacnet.DefaultDecodeLimits()
	var out []service.SubscribeCOVPropertyMultipleRequest
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
		if err != nil || pdu.ConfirmedRequest == nil || pdu.ConfirmedRequest.ServiceChoice != apdu.ServiceSubscribeCOVPropertyMultiple {
			continue
		}
		req, err := service.DecodeSubscribeCOVPropertyMultiple(pdu.ConfirmedRequest.Payload, limits)
		if err != nil {
			t.Fatalf("client sent an undecodable SubscribeCOVPropertyMultiple: %v", err)
		}
		out = append(out, req)
	}
	return out
}

func nextSubscriptionEvent(t *testing.T, sub Subscription, what string) SubscriptionEvent {
	t.Helper()
	select {
	case ev, ok := <-sub.Events():
		if !ok {
			t.Fatalf("%s: events closed", what)
		}
		return ev
	case <-time.After(2 * time.Second):
		t.Fatalf("%s: no event", what)
	}
	return SubscriptionEvent{}
}

func covMultipleNote(processID uint32) service.COVNotificationMultiple {
	return service.COVNotificationMultiple{
		SubscriberProcessIdentifier: processID,
		InitiatingDevice:            bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1234},
		TimeRemaining:               300,
		Timestamp: &service.DateTime{Date: bacnet.Date{Year: 126, Month: 10, Day: 10, Weekday: 6},
			Time: bacnet.Time{Hour: 12, Minute: 30}},
		Objects: []service.COVNotificationMultipleObject{
			{Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}, Values: []service.COVNotificationMultipleValue{{
				Property:     bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
				Value:        bacnet.RealValue(21.5),
				TimeOfChange: &bacnet.Time{Hour: 12, Minute: 30, Second: 5},
			}}},
			{Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeBinaryValue, Instance: 1}, Values: []service.COVNotificationMultipleValue{{
				Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
				Value:    bacnet.EnumValue(1),
			}}},
		},
	}
}

func injectCOVMultiple(t *testing.T, env *virtualPair, note service.COVNotificationMultiple, confirmed bool) {
	t.Helper()
	payload, err := service.EncodeCOVNotificationMultiple(note)
	if err != nil {
		t.Fatal(err)
	}
	raw := apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{
		ServiceChoice: apdu.ServiceUnconfirmedCOVNotificationMultiple, Payload: payload,
	})
	if confirmed {
		raw = apdu.AppendConfirmedRequest(nil, apdu.ConfirmedRequest{
			InvokeID: 200, ServiceChoice: apdu.ServiceConfirmedCOVNotificationMultiple, MaxAPDU: 5, Payload: payload,
		})
	}
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), raw)
}

// A SubscribeCOVMultiple subscription: the request carries the options, each
// COVNotificationMultiple arrives whole (timestamp and time of change
// included), and Close sends the cancellation form.
func TestSubscribeCOVMultipleLifecycle(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go serveSimpleACK(ctx, env.PeerTr, env.Local)

	increment := float32(0.5)
	specs := []service.COVMultipleSubscription{
		{Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}, Properties: []service.COVPropertyReference{
			{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}, COVIncrement: &increment, Timestamped: true},
		}},
		{Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeBinaryValue, Instance: 1}, Properties: []service.COVPropertyReference{
			{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}},
		}},
	}
	sub, err := env.Client.SubscribeCOVMultiple(ctx, env.Target, specs, COVMultipleOptions{
		COVOptions:           COVOptions{Lifetime: 300, IssueConfirmed: true},
		MaxNotificationDelay: 5,
	})
	if err != nil {
		t.Fatalf("SubscribeCOVMultiple: %v", err)
	}
	// The caller's slice is not retained.
	specs[0].Properties[0].Timestamped = false

	sent := sentCOVMultipleRequests(t, env)
	if len(sent) != 1 {
		t.Fatalf("%d requests sent, want 1", len(sent))
	}
	req := sent[0]
	if req.Cancellation || !req.IssueConfirmedNotifications || req.LifetimeRemaining == nil || *req.LifetimeRemaining != 300 ||
		req.MaxNotificationDelay == nil || *req.MaxNotificationDelay != 5 {
		t.Fatalf("request %+v, want confirmed, lifetime 300, delay 5", req)
	}
	if len(req.Subscriptions) != 2 || !req.Subscriptions[0].Properties[0].Timestamped ||
		*req.Subscriptions[0].Properties[0].COVIncrement != 0.5 {
		t.Fatalf("specifications %+v", req.Subscriptions)
	}
	processID := req.SubscriberProcessIdentifier

	if ev := nextSubscriptionEvent(t, sub, "active"); ev.State != SubscriptionActive || ev.Multiple != nil {
		t.Fatalf("first event %+v, want Active", ev)
	}

	// Another process identifier, and a single-object notification for this
	// one, are not this subscription's.
	injectCOVMultiple(t, env, covMultipleNote(processID+1), false)
	env.Client.subs.deliver(SubscriptionEvent{Notification: &service.COVNotification{ProcessIdentifier: processID}},
		processID, packetSource{origin: env.Peer, immediate: env.Peer})

	want := covMultipleNote(processID)
	for _, confirmed := range []bool{false, true} {
		injectCOVMultiple(t, env, want, confirmed)
		ev := nextSubscriptionEvent(t, sub, "notification")
		if ev.Notification != nil || ev.Multiple == nil {
			t.Fatalf("confirmed=%v: event %+v, want Multiple only", confirmed, ev)
		}
		if !reflect.DeepEqual(*ev.Multiple, want) {
			t.Fatalf("confirmed=%v: got\n %+v\nwant\n %+v", confirmed, *ev.Multiple, want)
		}
	}

	// Objects the subscription did not ask for are left out; a notification
	// with none of its objects is dropped. A device may still hold an earlier
	// client's subscription with the same process identifier.
	foreign := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 99}
	stale := covMultipleNote(processID)
	for i := range stale.Objects {
		stale.Objects[i].Object.Instance = 90 + uint32(i)
	}
	injectCOVMultiple(t, env, stale, false)
	mixed := covMultipleNote(processID)
	mixed.Objects = append(mixed.Objects, service.COVNotificationMultipleObject{Object: foreign})
	injectCOVMultiple(t, env, mixed, false)
	ev := nextSubscriptionEvent(t, sub, "notification with a foreign object")
	if ev.Multiple == nil || !reflect.DeepEqual(ev.Multiple.Objects, want.Objects) {
		t.Fatalf("got %+v, want the two subscribed objects only", ev.Multiple)
	}

	if err := sub.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	sent = sentCOVMultipleRequests(t, env)
	if len(sent) != 2 {
		t.Fatalf("%d requests after Close, want 2", len(sent))
	}
	c := sent[1]
	if !c.Cancellation || c.LifetimeRemaining != nil || c.MaxNotificationDelay != nil ||
		c.SubscriberProcessIdentifier != processID || len(c.Subscriptions) != 2 {
		t.Fatalf("cancellation %+v", c)
	}
	if ev := nextSubscriptionEvent(t, sub, "closed"); ev.State != SubscriptionClosed {
		t.Fatalf("last event %+v, want Closed", ev)
	}
}

// A COVNotificationMultiple for the process identifier of a single-object
// subscription: the entry for its object arrives as an ordinary notification
// with the whole notification next to it.
func TestCOVNotificationMultipleToSingleObjectSubscription(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go serveSimpleACK(ctx, env.PeerTr, env.Local)

	av := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	sub, err := env.Client.SubscribeCOV(ctx, env.Target, av, COVOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sub.Close() }()
	nextSubscriptionEvent(t, sub, "active")
	note := covMultipleNote(sub.(*subscription).id)
	injectCOVMultiple(t, env, note, false)

	ev := nextSubscriptionEvent(t, sub, "notification")
	if ev.Notification == nil || ev.Notification.MonitoredObject != av || len(ev.Notification.Values) != 1 ||
		ev.Notification.TimeRemaining != 300 {
		t.Fatalf("notification %+v", ev.Notification)
	}
	if ev.Multiple == nil || ev.Multiple.Timestamp == nil || ev.Multiple.Objects[0].Values[0].TimeOfChange == nil {
		t.Fatalf("whole notification missing: %+v", ev.Multiple)
	}
	select {
	case ev := <-sub.Events():
		t.Fatalf("event for an object the subscription does not monitor: %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestSubscribeCOVMultipleRefused(t *testing.T) {
	env := newVirtualPair(t)
	av := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	for _, specs := range [][]service.COVMultipleSubscription{nil, {{Object: av}}} {
		if _, err := env.Client.SubscribeCOVMultiple(context.Background(), env.Target, specs, COVMultipleOptions{}); !errors.Is(err, bacnet.ErrInvalidConfig) {
			t.Fatalf("specs %+v: got %v, want ErrInvalidConfig", specs, err)
		}
	}

	errCh := make(chan error, 1)
	go func() {
		_, err := env.Client.SubscribeCOVMultiple(context.Background(), env.Target, []service.COVMultipleSubscription{{
			Object:     av,
			Properties: []service.COVPropertyReference{{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}}},
		}}, COVMultipleOptions{})
		errCh <- err
	}()
	invokeID, _ := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendReject(nil, apdu.RejectPDU{
		InvokeID: invokeID, Reason: bacnet.RejectReasonUnrecognizedService,
	}))
	var rej *bacnet.RejectError
	if err := <-errCh; !errors.As(err, &rej) {
		t.Fatalf("got %v, want the device's Reject", err)
	}
	env.Client.subs.mu.Lock()
	left := len(env.Client.subs.byID)
	env.Client.subs.mu.Unlock()
	if left != 0 {
		t.Fatalf("%d subscriptions left registered after a refused request", left)
	}
}

func TestVTDataACK(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	count := uint32(2)
	go serveComplexACK(ctx, env.PeerTr, env.Local, func(uint8) ([]byte, error) {
		return service.EncodeVTDataACK(service.VTDataACK{AcceptedOctetCount: &count})
	})
	ack, err := env.Client.VTData(ctx, env.Target, service.VTDataRequest{VTSessionIdentifier: 1, VTNewData: []byte("ls\r"), VTDataFlag: 1})
	if err != nil {
		t.Fatal(err)
	}
	if ack.AllNewDataAccepted || ack.AcceptedOctetCount == nil || *ack.AcceptedOctetCount != 2 {
		t.Fatalf("ack %+v, want not all accepted, 2 octets", ack)
	}
}
