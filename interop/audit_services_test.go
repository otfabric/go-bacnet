//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/service"
)

// Audit services. No pinned stack executes them, but two ship the Clause 21
// data types: bacnet-stack (bacaudit.c: BACnetAuditNotification and
// BACnetAuditLogRecord) and BACpypes3 (basetypes: AuditNotification,
// AuditLogQueryParameters, AuditLogRecordResult). The adapters write only
// the service frames around them, so what these tests establish is:
//
//   - this client decodes notifications and log records the two upstream
//     encoders produced, to the exact values the adapter set;
//   - BACpypes3's AuditLogQueryParameters decodes this client's query
//     parameters to the values sent;
//   - the frames (notifications [0], the request's four fields, the ACK's
//     three) are the adapter's, checked against Wireshark only.
//
// Neither adapter has an Audit Log object or reports real operations.

func ptrTo[T any](v T) *T { return &v }

// auditFull is the notification both adapters report with every field set.
// result differs: BACpypes3 encodes the Error sequence, bacnet-stack 1.6 the
// bare error code.
func auditFull(dev deviceFixture, result service.AuditResult) service.AuditNotification {
	device := deviceObject(dev)
	return service.AuditNotification{
		SourceTimestamp: &service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 7},
		TargetTimestamp: &service.TimeStamp{Choice: service.TimeStampDateTime, DateTime: service.DateTime{
			Date: bacnet.Date{Year: 126, Month: 10, Day: 10, Weekday: 6}, Time: bacnet.Time{Hour: 12, Minute: 30}}},
		SourceDevice:   service.AuditDevice(device),
		SourceObject:   &device,
		Operation:      1, // write
		SourceComment:  ptrTo("interop-source"),
		TargetComment:  ptrTo("interop-target"),
		InvokeID:       ptrTo(uint8(5)),
		SourceUserID:   ptrTo(uint16(1001)),
		SourceUserRole: ptrTo(uint8(3)),
		TargetDevice:   service.AuditDevice(device),
		TargetObject:   &bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
		TargetProperty: &bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
		TargetPriority: ptrTo(uint8(8)),
		TargetValue:    ptrTo(bacnet.RealValue(42.5)),
		CurrentValue:   ptrTo(bacnet.RealValue(21.5)),
		Result:         &result,
	}
}

var (
	// property / write-access-denied, as the BACpypes3 adapter sets it.
	bacpypes3AuditResult = service.AuditResult{Class: 2, Code: 40}
	// success (84) as a bare enumeration, as bacnet-stack 1.6 encodes it.
	bacnetStackAuditResult = service.AuditResult{Code: 84, Bare: true}
)

// auditMinimal is BACpypes3's second notification: required fields only,
// the target given as a BACnetAddress.
func auditMinimal(dev deviceFixture) service.AuditNotification {
	return service.AuditNotification{
		SourceDevice: service.AuditDevice(deviceObject(dev)),
		Operation:    15, // general
		TargetDevice: service.AuditRecipient{Address: &service.AuditAddress{Network: 7, MAC: []byte{10, 0, 0, 1, 0xba, 0xc0}}},
	}
}

// runAuditNotifications triggers the adapter's audit notifications
// (BACNET_EMIT_AUDIT=1: one Unconfirmed and one ConfirmedAuditNotification
// after the first ReadProperty) and checks what the audit stream delivers.
func runAuditNotifications(t *testing.T, p interopPeer, want func(deviceFixture) []service.AuditNotification) {
	requireAdapterVersion(t, p.image, "0.10.0")
	peer := startPeer(t, p.image, p.name, "BACNET_EMIT_AUDIT=1")
	if peer.assertedByReexec {
		return
	}
	dev := loadDeviceFixture(t)
	c := newClient(t)
	stream := c.OpenAuditStream(16)
	defer stream.Close()
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()

	mark := evidenceMark(peer)
	if _, err := c.ReadProperty(ctx, peer.target, deviceObject(dev),
		bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName}); err != nil {
		t.Fatalf("ReadProperty (trigger): %v", err)
	}

	notes := want(dev)
	var unconfirmed, confirmed []client.AuditStreamEvent
	deadline := time.After(10 * time.Second)
	for len(unconfirmed) < len(notes) || len(confirmed) < len(notes) {
		select {
		case ev := <-stream.Events():
			if ev.Gap {
				t.Fatalf("audit stream reported a gap: %+v", ev)
			}
			if ev.Confirmed {
				confirmed = append(confirmed, ev)
			} else {
				unconfirmed = append(unconfirmed, ev)
			}
		case <-deadline:
			t.Fatalf("got %d unconfirmed and %d confirmed audit notifications, want %d of each; evidence=%q",
				len(unconfirmed), len(confirmed), len(notes), evidenceLines(peer))
		}
	}
	for name, got := range map[string][]client.AuditStreamEvent{"unconfirmed": unconfirmed, "confirmed": confirmed} {
		if len(got) != len(notes) {
			t.Fatalf("%s: %d notifications, want %d", name, len(got), len(notes))
		}
		for i := range notes {
			if !reflect.DeepEqual(got[i].Notification, notes[i]) {
				t.Errorf("%s notification %d decoded\n %+v\nwant\n %+v", name, i, got[i].Notification, notes[i])
			}
		}
	}
	// The peer's transaction completed on this client's SimpleACK.
	ack := awaitOperationLine(t, peer, "confirmed-audit-notification-ack", mark, 10*time.Second)
	if ack["result"] != "simple-ack" {
		t.Errorf("peer saw %v for its ConfirmedAuditNotification, want simple-ack", ack["result"])
	}
}

// BACpypes3 sends two notifications in one request; they carry no delimiter
// and are delivered as two events.
func TestBACpypes3AuditNotifications(t *testing.T) {
	runAuditNotifications(t, bacpypes3Peer(), func(dev deviceFixture) []service.AuditNotification {
		return []service.AuditNotification{auditFull(dev, bacpypes3AuditResult), auditMinimal(dev)}
	})
}

func TestBacnetStackAuditNotifications(t *testing.T) {
	runAuditNotifications(t, stackPeer(), func(dev deviceFixture) []service.AuditNotification {
		return []service.AuditNotification{auditFull(dev, bacnetStackAuditResult)}
	})
}

func auditLogObject() bacnet.ObjectIdentifier {
	return bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAuditLog, Instance: 1}
}

// AuditLogQuery against BACpypes3: the query parameters are decoded by the
// upstream AuditLogQueryParameters type, the records of the ACK encoded by
// the upstream AuditLogRecordResult type.
func TestBACpypes3AuditLogQuery(t *testing.T) {
	image := getEnv("BACPYPES3_IMAGE", defaultBACpypes3Image)
	requireAdapterVersion(t, image, "0.10.0")
	peer := startPeer(t, image, "bacpypes3")
	if peer.assertedByReexec {
		return
	}
	dev := loadDeviceFixture(t)
	c := newClient(t)
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()
	type obj = map[string]any

	// By target, every parameter set.
	mark := evidenceMark(peer)
	ack, err := c.AuditLogQuery(ctx, peer.target, service.AuditLogQueryRequest{
		AuditLog: auditLogObject(),
		ByTarget: &service.AuditQueryByTarget{
			TargetDevice:     deviceObject(dev),
			TargetObject:     &bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
			TargetProperty:   ptrTo(bacnet.PropertyPresentValue),
			TargetArrayIndex: ptrTo(uint32(2)),
			TargetPriority:   ptrTo(uint8(8)),
			// BACnetAuditOperationFlags: read, write.
			Operations:    &bacnet.BitString{UnusedBits: 6, Bytes: []byte{0xC0}},
			SuccessFilter: service.AuditSuccessSuccessesOnly,
		},
		StartAtSequenceNumber: ptrTo(uint64(500)),
		RequestedCount:        10,
	})
	if err != nil {
		t.Fatalf("AuditLogQuery by target: %v", err)
	}
	f := operationFields(t, awaitOperationLine(t, peer, "audit-log-query", mark, 5*time.Second))
	wantField(t, f, "audit_log", obj{"type": 61, "instance": 1})
	wantField(t, f, "start_at_sequence_number", 500)
	wantField(t, f, "requested_count", 10)
	wantField(t, f, "by_target", obj{
		"target_device_identifier":   obj{"type": 8, "instance": dev.DeviceInstance},
		"target_object_identifier":   obj{"type": 2, "instance": 1},
		"target_property_identifier": 85,
		"target_array_index":         2,
		"target_priority":            8,
		"operations":                 []int{0, 1}, // bits set: read, write
		"successful_actions_only":    true,
	})
	if _, ok := f["by_source"]; ok {
		t.Errorf("peer decoded by-source parameters from a by-target query: %v", f)
	}

	// The adapter answers two records numbered from start-at-sequence-number.
	stamp := service.DateTime{Date: bacnet.Date{Year: 126, Month: 10, Day: 10, Weekday: 6}, Time: bacnet.Time{Hour: 12, Minute: 30}}
	note := auditFull(dev, bacpypes3AuditResult)
	want := service.AuditLogQueryACK{
		AuditLog: auditLogObject(),
		Records: []service.AuditLogRecordResult{
			// BACnetLogStatus {buffer-purged}.
			{SequenceNumber: 500, Timestamp: stamp, LogStatus: &bacnet.BitString{UnusedBits: 5, Bytes: []byte{0x40}}},
			{SequenceNumber: 501, Timestamp: stamp, Notification: &note},
		},
		NoMoreItems: true,
	}
	if !reflect.DeepEqual(ack, want) {
		t.Fatalf("AuditLogQuery-ACK decoded\n %+v\nwant\n %+v", ack, want)
	}

	// By source, only the required parameters, no start-at-sequence-number.
	mark = evidenceMark(peer)
	ack, err = c.AuditLogQuery(ctx, peer.target, service.AuditLogQueryRequest{
		AuditLog: auditLogObject(),
		BySource: &service.AuditQueryBySource{
			SourceDevice: deviceObject(dev),
			SourceObject: &bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeBinaryValue, Instance: 1},
		},
		RequestedCount: 2,
	})
	if err != nil {
		t.Fatalf("AuditLogQuery by source: %v", err)
	}
	f = operationFields(t, awaitOperationLine(t, peer, "audit-log-query", mark, 5*time.Second))
	wantField(t, f, "start_at_sequence_number", nil)
	wantField(t, f, "requested_count", 2)
	wantField(t, f, "by_source", obj{
		"source_device_identifier": obj{"type": 8, "instance": dev.DeviceInstance},
		"source_object_identifier": obj{"type": 5, "instance": 1},
		"operations":               nil,
		"successful_actions_only":  false,
	})
	if _, ok := f["by_target"]; ok {
		t.Errorf("peer decoded by-target parameters from a by-source query: %v", f)
	}
	if len(ack.Records) != 2 || ack.Records[0].SequenceNumber != 100 || ack.Records[1].Notification == nil {
		t.Fatalf("AuditLogQuery-ACK (by source) = %+v", ack)
	}
}

// AuditLogQuery against bacnet-stack, which has no decoder for the request:
// the adapter reads only the audit-log identifier and says so
// ("request_decoder":"none"). What is evidenced is the ACK: three records -
// log-status, audit-notification, time-change - encoded by
// bacnet_audit_log_record_encode().
func TestBacnetStackAuditLogQueryACK(t *testing.T) {
	image := getEnv("BACNET_STACK_IMAGE", defaultStackImage)
	requireAdapterVersion(t, image, "0.10.0")
	peer := startPeer(t, image, "bacnet-stack")
	if peer.assertedByReexec {
		return
	}
	dev := loadDeviceFixture(t)
	c := newClient(t)
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()

	mark := evidenceMark(peer)
	ack, err := c.AuditLogQuery(ctx, peer.target, service.AuditLogQueryRequest{
		AuditLog:       auditLogObject(),
		ByTarget:       &service.AuditQueryByTarget{TargetDevice: deviceObject(dev)},
		RequestedCount: 10,
	})
	if err != nil {
		t.Fatalf("AuditLogQuery: %v", err)
	}
	ev := awaitOperationLine(t, peer, "audit-log-query", mark, 5*time.Second)
	if ev["request_decoder"] != "none" || ev["audit_log"] != "61:1" {
		t.Errorf("operation line = %v", ev)
	}
	stamp := func(sec uint8) service.DateTime {
		return service.DateTime{Date: bacnet.Date{Year: 126, Month: 10, Day: 10, Weekday: 6}, Time: bacnet.Time{Hour: 12, Minute: 30, Second: sec}}
	}
	note := auditFull(dev, bacnetStackAuditResult)
	want := service.AuditLogQueryACK{
		AuditLog: auditLogObject(),
		Records: []service.AuditLogRecordResult{
			{SequenceNumber: 100, Timestamp: stamp(0), LogStatus: &bacnet.BitString{UnusedBits: 5, Bytes: []byte{0x40}}},
			{SequenceNumber: 101, Timestamp: stamp(1), Notification: &note},
			{SequenceNumber: 102, Timestamp: stamp(2), TimeChange: ptrTo(float32(1.5))},
		},
		NoMoreItems: true,
	}
	if !reflect.DeepEqual(ack, want) {
		t.Fatalf("AuditLogQuery-ACK decoded\n %+v\nwant\n %+v", ack, want)
	}
}

// AuthRequest: no pinned or newest stable release of any peer stack has a
// codec for it (BACpypes3 has only the removed Authenticate service, choice
// 24; bacnet-stack knows the service's name and nothing else). So no peer can
// verify the request's parameters, and all there is to observe is how each
// refuses service choice 34:
//
//   - bacnet-stack, BACnet4J and System.IO.BACnet: Reject unrecognized-service;
//   - the Worldiety adapter: Reject with reason other;
//   - BACpypes3 0.0.110: no answer at all, so the request ends as
//     outcome-unknown after the APDU timeout.
//
// Pinned so that a peer that starts to serve AuthRequest is noticed.
func TestAuthRequestUnrecognizedService(t *testing.T) {
	want := map[string]uint8{
		"bacnet-stack": bacnet.RejectReasonUnrecognizedService,
		"bacnet4j":     bacnet.RejectReasonUnrecognizedService,
		sysioAdapter:   bacnet.RejectReasonUnrecognizedService,
		"worldiety":    bacnet.RejectReasonOther,
	}
	for _, p := range append(allPeers(), sysioPeer()) {
		forPeers(t, []interopPeer{p}, func(t *testing.T, _ deviceFixture, peer *peerHandle) {
			authRequestRefused(t, peer, want, p.name)
		})
	}
}

func authRequestRefused(t *testing.T, peer *peerHandle, want map[string]uint8, name string) {
	c := newClientOpts(t, client.WithTransactionOptions(time.Second, 0, time.Second))
	ctx, cancel := withTimeout(t, 10*time.Second)
	defer cancel()
	err := c.AuthRequest(ctx, peer.target, service.AuthRequest{
		Parameters: []bacnet.Element{{Context: true, TagNumber: 0, Value: bacnet.UnsignedValue(1)}},
	})
	reason, answers := want[name]
	if !answers {
		var unknown *bacnet.OutcomeUnknownError
		if !errors.As(err, &unknown) || !errors.Is(err, bacnet.ErrTimeout) {
			t.Fatalf("AuthRequest: got %v, want no answer (outcome unknown after a timeout)", err)
		}
		return
	}
	var rej *bacnet.RejectError
	if !errors.As(err, &rej) || rej.Reason != reason {
		t.Fatalf("AuthRequest: got %v, want Reject %s", err, bacnet.RejectReasonName(reason))
	}
}
