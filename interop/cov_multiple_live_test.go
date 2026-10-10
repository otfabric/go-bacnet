//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/service"
)

// COVNotificationMultiple from a live peer.
//
// BACnet4J parses SubscribeCOVPropertyMultiple with its own class and then
// refuses it (handle() throws NotImplementedException: Reject
// unrecognized-service), so no stack in the test bed executes the
// subscription. With BACNET_EMIT_COV_MULTIPLE=1 the BACnet4J adapter uses the
// request as a trigger: it reports what the stack's parser extracted, and
// sends the requester one UnconfirmedCOVNotificationMultiple and one
// ConfirmedCOVNotificationMultiple for the objects and properties of the
// request, built with BACnet4J's request classes and encoded by them. The
// subscription is the adapter's doing; the request parser, the notification
// encoder, the confirmed transaction and the receipt of the SimpleACK are the
// stack's.
//
// What the client delivers: every SubscriptionEvent that stems from a
// COVNotificationMultiple carries the whole notification in Multiple, with
// its timestamp and each value's time of change, compared here with what
// BACnet4J reports it sent.
//
// Because BACnet4J refuses the subscription, Client.SubscribeCOVMultiple
// returns no handle from this peer (its request life cycle is shown against
// BACpypes3). The notifications are therefore received through an ordinary
// SubscribeCOV on the same object, whose process identifier the multiple
// request reuses: such a subscription gets the entry for its object as
// Notification next to Multiple.
func TestBACnet4JCOVNotificationMultipleLive(t *testing.T) {
	image := bacnet4jImage()
	requireAdapterVersion(t, image, "0.10.0")
	dev := loadDeviceFixture(t)
	peer := startPeer(t, image, "bacnet4j", "BACNET_EMIT_COV_MULTIPLE=1")
	if peer.assertedByReexec {
		return
	}
	c := newClientOpts(t, client.WithTransactionOptions(2*time.Second, 0, time.Second))
	ctx, cancel := withTimeout(t, 30*time.Second)
	defer cancel()
	av, baseline := analogValueObject(dev)
	bv := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeBinaryValue, Instance: 1}

	// An ordinary subscription on AV-1: its events channel is where the
	// client delivers, and its first notification tells the process
	// identifier the client chose.
	sub, err := c.SubscribeCOV(ctx, peer.target, av, client.COVOptions{Lifetime: 60})
	if err != nil {
		t.Fatalf("SubscribeCOV: %v", err)
	}
	defer sub.Close()
	next := func(what string) client.SubscriptionEvent {
		t.Helper()
		deadline := time.After(8 * time.Second)
		for {
			select {
			case ev, ok := <-sub.Events():
				if !ok {
					t.Fatalf("%s: subscription closed", what)
				}
				if ev.Notification != nil {
					return ev
				}
			case <-deadline:
				t.Fatalf("%s: no notification", what)
				return client.SubscriptionEvent{}
			}
		}
	}
	initial := next("initial notification of the ordinary subscription").Notification
	if initial.TimeRemaining > 60 {
		t.Fatalf("initial notification time remaining %d, the subscription asked for 60", initial.TimeRemaining)
	}
	processID := initial.ProcessIdentifier

	lifetime, delay := uint32(300), uint32(5)
	increment := float32(0.5)
	req := service.SubscribeCOVPropertyMultipleRequest{
		SubscriberProcessIdentifier: processID,
		IssueConfirmedNotifications: true,
		LifetimeRemaining:           &lifetime,
		MaxNotificationDelay:        &delay,
		Subscriptions: []service.COVMultipleSubscription{
			{Object: av, Properties: []service.COVPropertyReference{
				{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}, COVIncrement: &increment, Timestamped: true},
			}},
			// No ordinary subscription exists for BV-1: it reaches the
			// caller in Multiple only.
			{Object: bv, Properties: []service.COVPropertyReference{
				{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}},
			}},
		},
	}
	mark := evidenceMark(peer)
	err = c.SubscribeCOVPropertyMultiple(ctx, peer.target, req)
	var rej *bacnet.RejectError
	if !errors.As(err, &rej) || rej.Reason != bacnet.RejectReasonUnrecognizedService {
		t.Fatalf("SubscribeCOVPropertyMultiple: got %v, want Reject unrecognized-service (parsed, not executed)", err)
	}

	// What BACnet4J's parser extracted from the request.
	type ref = map[string]any
	parsed := awaitOperationLine(t, peer, "subscribe-cov-property-multiple", mark, 5*time.Second)
	if parsed["result"] != "parsed" {
		t.Fatalf("operation result %v, want parsed", parsed["result"])
	}
	f := operationFields(t, parsed)
	wantField(t, f, "subscriber_process_identifier", processID)
	wantField(t, f, "issue_confirmed_notifications", true)
	wantField(t, f, "lifetime", 300)
	wantField(t, f, "max_notification_delay", 5)
	wantField(t, f, "cov_subscription_specifications", []ref{
		{"monitored_object_identifier": ref{"type": 2, "instance": av.Instance}, "cov_references": []ref{
			{"property_identifier": 85, "property_array_index": nil, "cov_increment": 0.5, "timestamped": true},
		}},
		{"monitored_object_identifier": ref{"type": 5, "instance": 1}, "cov_references": []ref{
			{"property_identifier": 85, "property_array_index": nil, "cov_increment": nil, "timestamped": false},
		}},
	})

	// What BACnet4J sent: the same content in both notifications.
	var sentTime bacnet.Time
	for _, op := range []string{"emit-unconfirmed-cov-notification-multiple", "emit-confirmed-cov-notification-multiple"} {
		line := awaitOperationLine(t, peer, op, mark, 8*time.Second)
		if line["result"] != "sent" {
			t.Fatalf("%s: result %v, want sent", op, line["result"])
		}
		s := operationFields(t, line)
		wantField(t, s, "subscriber_process_identifier", processID)
		wantField(t, s, "initiating_device_identifier", ref{"type": 8, "instance": dev.DeviceInstance})
		wantField(t, s, "time_remaining", 300)
		ts, ok := s["timestamp"].(map[string]any)
		if !ok {
			t.Fatalf("%s: no timestamp in %v", op, s)
		}
		// The timestamped reference carries the notification's time as
		// time-of-change; the other reference carries none.
		timeOfChange := ref{"hour": ts["hour"], "minute": ts["minute"], "second": ts["second"], "hundredths": ts["hundredths"]}
		part := func(key string) uint8 {
			v, ok := ts[key].(float64)
			if !ok {
				t.Fatalf("%s: timestamp %v has no %s", op, ts, key)
			}
			return uint8(v)
		}
		sentTime = bacnet.Time{Hour: part("hour"), Minute: part("minute"), Second: part("second"), Hundredths: part("hundredths")}
		wantField(t, s, "cov_notifications", []ref{
			{"monitored_object_identifier": ref{"type": 2, "instance": av.Instance}, "values": []ref{
				{"property_identifier": 85, "property_array_index": nil, "value": "21.5", "time_of_change": timeOfChange},
			}},
			{"monitored_object_identifier": ref{"type": 5, "instance": 1}, "values": []ref{
				{"property_identifier": 85, "property_array_index": nil, "value": "inactive", "time_of_change": nil},
			}},
		})
	}

	// What the client delivers, per COVNotificationMultiple: the entry for
	// AV-1 as a notification with the multiple request's lifetime and exactly
	// the one property BACnet4J put into it (an ordinary COV notification of
	// an Analog Value carries Present_Value and Status_Flags, and at most the
	// 60 seconds of the ordinary subscription), and the whole notification.
	for _, what := range []string{"first COVNotificationMultiple", "second COVNotificationMultiple"} {
		ev := next(what)
		n := ev.Notification
		if n.TimeRemaining != 300 || len(n.Values) != 1 {
			t.Fatalf("%s: got %+v, want time remaining 300 and one value (is it an ordinary COV notification?)", what, *n)
		}
		if n.ProcessIdentifier != processID || n.MonitoredObject != av ||
			n.InitiatingDevice != deviceObject(dev) {
			t.Fatalf("%s: process %d device %v object %v, want %d %v %v", what,
				n.ProcessIdentifier, n.InitiatingDevice, n.MonitoredObject, processID, deviceObject(dev), av)
		}
		v := n.Values[0]
		if v.Property.Identifier != bacnet.PropertyPresentValue || v.Property.ArrayIndex != nil {
			t.Fatalf("%s: property %+v, want present-value", what, v.Property)
		}
		if got, err := bacnet.AsReal(v.Value); err != nil || math.Abs(float64(got-baseline)) > 0.001 {
			t.Fatalf("%s: value %+v (%v), want REAL %v", what, v.Value, err, baseline)
		}

		m := ev.Multiple
		if m == nil {
			t.Fatalf("%s: event without the whole notification", what)
		}
		if m.SubscriberProcessIdentifier != processID || m.InitiatingDevice != deviceObject(dev) || m.TimeRemaining != 300 {
			t.Fatalf("%s: multiple %+v", what, *m)
		}
		if m.Timestamp == nil || m.Timestamp.Time != sentTime {
			t.Fatalf("%s: timestamp %+v, BACnet4J sent time %+v", what, m.Timestamp, sentTime)
		}
		if len(m.Objects) != 2 || m.Objects[0].Object != av || m.Objects[1].Object != bv ||
			len(m.Objects[0].Values) != 1 || len(m.Objects[1].Values) != 1 {
			t.Fatalf("%s: objects %+v, want AV-1 and BV-1 with one value each", what, m.Objects)
		}
		// Only the reference subscribed with Timestamped has a time of change.
		if toc := m.Objects[0].Values[0].TimeOfChange; toc == nil || *toc != sentTime {
			t.Fatalf("%s: AV-1 time of change %v, want %+v", what, toc, sentTime)
		}
		if toc := m.Objects[1].Values[0].TimeOfChange; toc != nil {
			t.Fatalf("%s: BV-1 time of change %+v, want none", what, *toc)
		}
		if got, err := bacnet.AsEnumerated(m.Objects[1].Values[0].Value); err != nil || got != 0 {
			t.Fatalf("%s: BV-1 value %+v (%v), want inactive", what, m.Objects[1].Values[0].Value, err)
		}
	}

	// BACnet4J's confirmed transaction ended with the client's SimpleACK.
	ack := awaitOperationLine(t, peer, "confirmed-cov-notification-multiple-ack", mark, 8*time.Second)
	if ack["result"] != "simple-ack" {
		t.Fatalf("BACnet4J's confirmed notification ended with %v, want simple-ack", ack["result"])
	}
}
