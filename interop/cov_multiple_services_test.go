//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"bytes"
	"encoding/hex"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/service"
)

func covMultipleRequest() service.SubscribeCOVPropertyMultipleRequest {
	lifetime, delay, index := uint32(300), uint32(5), uint32(8)
	increment := float32(0.5)
	return service.SubscribeCOVPropertyMultipleRequest{
		SubscriberProcessIdentifier: 77,
		IssueConfirmedNotifications: true,
		LifetimeRemaining:           &lifetime,
		MaxNotificationDelay:        &delay,
		Subscriptions: []service.COVMultipleSubscription{
			{Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}, Properties: []service.COVPropertyReference{
				{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}, COVIncrement: &increment, Timestamped: true},
				{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyStatusFlags}},
			}},
			{Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeBinaryValue, Instance: 1}, Properties: []service.COVPropertyReference{
				{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPriorityArray, ArrayIndex: &index}},
			}},
		},
	}
}

// SubscribeCOVPropertyMultiple against BACpypes3. BACpypes3 has no class for
// the request; the adapter declares the frame (the four scalar parameters
// and the list tag) and decodes list-of-cov-subscription-specifications with
// the upstream type basetypes.COVMultipleSubscriptionList. The evidence is
// therefore upstream for the specification list, and adapter-declared for the
// frame around it. The adapter answers SimpleACK and subscribes nothing.
func TestBACpypes3SubscribeCOVPropertyMultipleDecodeEvidence(t *testing.T) {
	image := getEnv("BACPYPES3_IMAGE", defaultBACpypes3Image)
	requireAdapterVersion(t, image, "0.10.0")
	peer := startPeer(t, image, "bacpypes3")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 15*time.Second)
	defer cancel()

	mark := evidenceMark(peer)
	if err := c.SubscribeCOVPropertyMultiple(ctx, peer.target, covMultipleRequest()); err != nil {
		t.Fatalf("SubscribeCOVPropertyMultiple: %v", err)
	}
	f := operationFields(t, awaitOperationLine(t, peer, "subscribe-cov-property-multiple", mark, 5*time.Second))
	wantField(t, f, "subscriber_process_identifier", 77)
	wantField(t, f, "issue_confirmed_notifications", true)
	wantField(t, f, "lifetime", 300)
	wantField(t, f, "max_notification_delay", 5)
	type ref = map[string]any
	wantField(t, f, "cov_subscription_specifications", []ref{
		{"monitored_object_identifier": ref{"type": 2, "instance": 1}, "cov_references": []ref{
			{"property_identifier": 85, "property_array_index": nil, "cov_increment": 0.5, "timestamped": true},
			{"property_identifier": 111, "property_array_index": nil, "cov_increment": nil, "timestamped": false},
		}},
		{"monitored_object_identifier": ref{"type": 5, "instance": 1}, "cov_references": []ref{
			{"property_identifier": 87, "property_array_index": 8, "cov_increment": nil, "timestamped": false},
		}},
	})

	// Without lifetime and max-notification-delay the two optional fields
	// are reported absent, not shifted.
	req := covMultipleRequest()
	req.LifetimeRemaining, req.MaxNotificationDelay = nil, nil
	req.Subscriptions = req.Subscriptions[1:]
	mark = evidenceMark(peer)
	if err := c.SubscribeCOVPropertyMultiple(ctx, peer.target, req); err != nil {
		t.Fatalf("SubscribeCOVPropertyMultiple without lifetime: %v", err)
	}
	f = operationFields(t, awaitOperationLine(t, peer, "subscribe-cov-property-multiple", mark, 5*time.Second))
	wantField(t, f, "lifetime", nil)
	wantField(t, f, "max_notification_delay", nil)
	wantField(t, f, "cov_subscription_specifications", []ref{
		{"monitored_object_identifier": ref{"type": 5, "instance": 1}, "cov_references": []ref{
			{"property_identifier": 87, "property_array_index": 8, "cov_increment": nil, "timestamped": false},
		}},
	})
}

// The managed subscription (Client.SubscribeCOVMultiple) against BACpypes3,
// whose adapter answers SimpleACK and reports each request it decoded. That
// shows the three requests of a subscription's life on the wire: the
// subscription, its renewal at half the lifetime with the same process
// identifier, and on Close the cancellation, which carries neither
// issue-confirmed-notifications nor lifetime. No notification follows:
// BACpypes3 subscribes nothing (TestBACnet4JCOVNotificationMultipleLive has
// the notifications).
func TestBACpypes3SubscribeCOVMultipleLifecycle(t *testing.T) {
	image := getEnv("BACPYPES3_IMAGE", defaultBACpypes3Image)
	requireAdapterVersion(t, image, "0.10.0")
	peer := startPeer(t, image, "bacpypes3")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()

	type ref = map[string]any
	specs := covMultipleRequest().Subscriptions
	wantSpecs := []ref{
		{"monitored_object_identifier": ref{"type": 2, "instance": 1}, "cov_references": []ref{
			{"property_identifier": 85, "property_array_index": nil, "cov_increment": 0.5, "timestamped": true},
			{"property_identifier": 111, "property_array_index": nil, "cov_increment": nil, "timestamped": false},
		}},
		{"monitored_object_identifier": ref{"type": 5, "instance": 1}, "cov_references": []ref{
			{"property_identifier": 87, "property_array_index": 8, "cov_increment": nil, "timestamped": false},
		}},
	}

	mark := evidenceMark(peer)
	sub, err := c.SubscribeCOVMultiple(ctx, peer.target, specs, client.COVMultipleOptions{
		COVOptions:           client.COVOptions{Lifetime: 4, IssueConfirmed: true},
		MaxNotificationDelay: 2,
	})
	if err != nil {
		t.Fatalf("SubscribeCOVMultiple: %v", err)
	}
	f := operationFields(t, awaitOperationLine(t, peer, "subscribe-cov-property-multiple", mark, 5*time.Second))
	processID := f["subscriber_process_identifier"]
	wantField(t, f, "issue_confirmed_notifications", true)
	wantField(t, f, "lifetime", 4)
	wantField(t, f, "max_notification_delay", 2)
	wantField(t, f, "cov_subscription_specifications", wantSpecs)

	// Renewal after half the lifetime.
	mark = evidenceMark(peer)
	states := map[client.SubscriptionState]bool{}
	deadline := time.After(8 * time.Second)
	for !states[client.SubscriptionRenewing] || !states[client.SubscriptionActive] {
		select {
		case ev := <-sub.Events():
			if ev.Err != nil {
				t.Fatalf("subscription event %+v", ev)
			}
			if states[client.SubscriptionRenewing] || ev.State == client.SubscriptionRenewing {
				states[ev.State] = true
			}
		case <-deadline:
			t.Fatalf("no renewal within 8 s of a 4 s lifetime; saw %v", states)
		}
	}
	f = operationFields(t, awaitOperationLine(t, peer, "subscribe-cov-property-multiple", mark, 5*time.Second))
	wantField(t, f, "subscriber_process_identifier", processID)
	wantField(t, f, "lifetime", 4)
	wantField(t, f, "cov_subscription_specifications", wantSpecs)

	mark = evidenceMark(peer)
	if err := sub.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	f = operationFields(t, awaitOperationLine(t, peer, "subscribe-cov-property-multiple", mark, 5*time.Second))
	wantField(t, f, "subscriber_process_identifier", processID)
	wantField(t, f, "issue_confirmed_notifications", nil)
	wantField(t, f, "lifetime", nil)
	wantField(t, f, "max_notification_delay", nil)
	wantField(t, f, "cov_subscription_specifications", wantSpecs)
}

// SubscribeCOVPropertyMultiple against BACnet4J 6.1.1, which parses the
// request with its own class (SubscribeCOVPropertyMultipleRequest) but does
// not execute it: handle() checks that issue-confirmed-notifications and
// lifetime are both present or both absent and then throws
// NotImplementedException. The reply therefore tells what the parser saw:
//
//   - both present, or both absent (the cancellation): Reject
//     unrecognized-service (parsed, consistent, not implemented);
//   - lifetime absent: an Error PDU (services/inconsistent-parameters, raised
//     from the decoded values of [1] and [2]). BACnet4J sends it with
//     error-choice 127 instead of the request's service choice 30. The
//     client reports a wrong_service diagnostic and still takes it as the
//     answer: invoke ID and source identify the transaction, and dropping it
//     would turn the refusal into a timeout.
//
// The stock adapter suffices: it needs no handler for this.
func TestBACnet4JSubscribeCOVPropertyMultipleParsed(t *testing.T) {
	peer := startPeer(t, bacnet4jImage(), "bacnet4j")
	if peer.assertedByReexec {
		return
	}
	var mu sync.Mutex
	wrongService := 0
	c := newClientOpts(t,
		client.WithTransactionOptions(time.Second, 0, time.Second),
		client.WithDiagnosticFunc(func(d client.Diagnostic) {
			if d.Kind == "wrong_service" {
				mu.Lock()
				wrongService++
				mu.Unlock()
			}
		}))
	ctx, cancel := withTimeout(t, 15*time.Second)
	defer cancel()

	err := c.SubscribeCOVPropertyMultiple(ctx, peer.target, covMultipleRequest())
	var rej *bacnet.RejectError
	if !errors.As(err, &rej) || rej.Reason != bacnet.RejectReasonUnrecognizedService {
		t.Fatalf("with lifetime: got %v, want Reject unrecognized-service (parsed, handle not implemented)", err)
	}
	// The cancellation form passes the same consistency check.
	cancellation := covMultipleRequest()
	cancellation.Cancellation = true
	err = c.SubscribeCOVPropertyMultiple(ctx, peer.target, cancellation)
	if !errors.As(err, &rej) || rej.Reason != bacnet.RejectReasonUnrecognizedService {
		t.Fatalf("cancellation: got %v, want Reject unrecognized-service (parsed as consistent)", err)
	}
	// And the managed subscription is refused cleanly: no handle.
	if sub, err := c.SubscribeCOVMultiple(ctx, peer.target, covMultipleRequest().Subscriptions, client.COVMultipleOptions{}); !errors.As(err, &rej) || sub != nil {
		t.Fatalf("SubscribeCOVMultiple: got %v, %v, want Reject unrecognized-service", sub, err)
	}
	mu.Lock()
	before := wrongService
	mu.Unlock()
	if before != 0 {
		t.Fatalf("%d wrong_service diagnostics before the second request", before)
	}

	req := covMultipleRequest()
	req.LifetimeRemaining = nil
	err = c.SubscribeCOVPropertyMultiple(ctx, peer.target, req)
	var remote *bacnet.ErrorResponse
	if !errors.As(err, &remote) {
		t.Fatalf("without lifetime: got %T %v, want BACnet4J's Error", err, err)
	}
	if remote.Service != 127 {
		t.Fatalf("error-choice %d, BACnet4J 6.1.1 sends 127", remote.Service)
	}
	var unknown *bacnet.OutcomeUnknownError
	if errors.As(err, &unknown) {
		t.Fatalf("a definite refusal reported as outcome-unknown: %v", err)
	}
	t.Logf("BACnet4J refuses the request without lifetime with %s/%s",
		bacnet.ErrorClassName(remote.Class), bacnet.ErrorCodeName(remote.Code))
	mu.Lock()
	after := wrongService
	mu.Unlock()
	if after == 0 {
		t.Fatal("without lifetime: no wrong_service diagnostic for the Error with a foreign error-choice")
	}
}

// COVNotificationMultiple: no adapter can send one yet (only BACnet4J has
// the encoder, see bacnet-interop PEER_SUPPORT.md). Until one does, this pins
// the bytes BACnet4J 6.1.1's ConfirmedCovNotificationMultipleRequest.write()
// produced, outside the test bed, for the notification below: this library
// decodes them to the same values and encodes the same bytes. BACnet4J's
// parser likewise accepted this library's bytes and re-encoded them
// identically. No peer is started.
func TestCOVNotificationMultipleBACnet4JVector(t *testing.T) {
	const bacnet4j = "094d" + // subscriber-process-identifier [0] 77
		"1c020004d2" + // initiating-device-identifier [1] device:1234
		"2a012c" + // time-remaining [2] 300
		"3ea47e0a0a06b40c1e00003f" + // timestamp [3] 2026-10-10 12:30:00.00
		"4e" + // list-of-cov-notifications [4]
		"0c00800001" + "1e" + // analog-value:1, list-of-values [1]
		"0955" + "2e4441ac00002f" + "3c0c1e0500" + // present-value 21.5, time-of-change [3] 12:30:05.00
		"096f" + "2e8204402f" + // status-flags {fault}
		"1f" +
		"0c01400001" + "1e" + "0955" + "2e91012f" + "1f" + // binary-value:1 present-value active
		"4f"
	peer, err := hex.DecodeString(bacnet4j)
	if err != nil {
		t.Fatal(err)
	}
	want := service.COVNotificationMultiple{
		SubscriberProcessIdentifier: 77,
		InitiatingDevice:            bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1234},
		TimeRemaining:               300,
		Timestamp: &service.DateTime{Date: bacnet.Date{Year: 126, Month: 10, Day: 10, Weekday: 6},
			Time: bacnet.Time{Hour: 12, Minute: 30}},
		Objects: []service.COVNotificationMultipleObject{
			{Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}, Values: []service.COVNotificationMultipleValue{
				{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}, Value: bacnet.RealValue(21.5),
					TimeOfChange: &bacnet.Time{Hour: 12, Minute: 30, Second: 5}},
				{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyStatusFlags},
					Value: bacnet.ApplicationValue{Kind: bacnet.ValueBitString, BitString: bacnet.BitString{UnusedBits: 4, Bytes: []byte{0x40}}}},
			}},
			{Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeBinaryValue, Instance: 1}, Values: []service.COVNotificationMultipleValue{
				{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}, Value: bacnet.EnumValue(1)},
			}},
		},
	}
	got, err := service.DecodeCOVNotificationMultiple(peer, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decoded\n %+v\nwant\n %+v", got, want)
	}
	raw, err := service.EncodeCOVNotificationMultiple(want)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, peer) {
		t.Fatalf("encoded\n %x\nBACnet4J\n %x", raw, peer)
	}

	// The request, as BACnet4J's SubscribeCOVPropertyMultipleRequest parsed
	// and re-encoded it byte for byte.
	const request = "094d19012a012c39054e0c008000011e0e09550f1c3f00000029010e096f0f29001f0c014000011e0e095719080f29001f4f"
	sub, err := service.EncodeSubscribeCOVPropertyMultiple(covMultipleRequest())
	if err != nil || hex.EncodeToString(sub) != request {
		t.Fatalf("SubscribeCOVPropertyMultiple encoded %x (%v), want %s", sub, err, request)
	}
}
