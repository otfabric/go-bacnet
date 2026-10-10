// SPDX-License-Identifier: MIT

package service_test

import (
	"bytes"
	"encoding/hex"
	"errors"
	"reflect"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

// Peer-verified audit vectors. The bytes below were produced by the peers'
// own encoders for the notifications the bacnet-interop adapters report
// (BACNET_EMIT_AUDIT=1); the interop suite receives the same bytes live.
const (
	// BACpypes3 0.0.106/0.0.110 basetypes.AuditNotification, every field set.
	bacpypes3AuditFull = "0e19070f" + // source-timestamp [0] { sequence-number [1] 7 }
		"1e2ea47e0a0a06b40c1e00002f1f" + // target-timestamp [1] { date-time [2] }
		"2e0c020004d22f" + // source-device [2] { device [0] device:1234 }
		"3c020004d2" + // source-object [3] device:1234
		"4901" + // operation [4] write
		"5d0f00696e7465726f702d736f75726365" + // source-comment [5] "interop-source"
		"6d0f00696e7465726f702d746172676574" + // target-comment [6] "interop-target"
		"7905" + // invoke-id [7]
		"8a03e9" + // source-user-id [8] 1001
		"9903" + // source-user-role [9]
		"ae0c020004d2af" + // target-device [10] { device [0] device:1234 }
		"bc00800001" + // target-object [11] analog-value:1
		"ce0955cf" + // target-property [12] { property-identifier [0] present-value }
		"d908" + // target-priority [13]
		"ee44422a0000ef" + // target-value [14] { REAL 42.5 }
		"fe0f4441ac0000ff0f" + // current-value [15] { REAL 21.5 }
		"fe1091029128ff10" // result [16] { property, write-access-denied }
	// BACpypes3: required fields only, target given as a BACnetAddress.
	bacpypes3AuditMinimal = "2e0c020004d22f" + "490f" + "ae1e210765060a000001bac01faf"
	// bacnet-stack 1.6.0/1.6.1 bacnet_audit_log_notification_encode: the same
	// content; result [16] is a bare enumeration (success, 84).
	bacnetStackResult = "f91054"
)

var bacnetStackAuditFull = bacpypes3AuditFull[:len(bacpypes3AuditFull)-16] + bacnetStackResult

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func interopAuditFull() service.AuditNotification {
	dev := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1234}
	return service.AuditNotification{
		SourceTimestamp: &service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 7},
		TargetTimestamp: &service.TimeStamp{Choice: service.TimeStampDateTime, DateTime: service.DateTime{
			Date: bacnet.Date{Year: 126, Month: 10, Day: 10, Weekday: 6}, Time: bacnet.Time{Hour: 12, Minute: 30}}},
		SourceDevice:   service.AuditDevice(dev),
		SourceObject:   &dev,
		Operation:      1,
		SourceComment:  ptr("interop-source"),
		TargetComment:  ptr("interop-target"),
		InvokeID:       ptr(uint8(5)),
		SourceUserID:   ptr(uint16(1001)),
		SourceUserRole: ptr(uint8(3)),
		TargetDevice:   service.AuditDevice(dev),
		TargetObject:   &bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
		TargetProperty: &bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
		TargetPriority: ptr(uint8(8)),
		TargetValue:    ptr(bacnet.RealValue(42.5)),
		CurrentValue:   ptr(bacnet.RealValue(21.5)),
		Result:         &service.AuditResult{Class: 2, Code: 40},
	}
}

func interopAuditMinimal() service.AuditNotification {
	return service.AuditNotification{
		SourceDevice: service.AuditDevice(bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1234}),
		Operation:    15,
		TargetDevice: service.AuditRecipient{Address: &service.AuditAddress{Network: 7, MAC: []byte{10, 0, 0, 1, 0xba, 0xc0}}},
	}
}

// The request BACpypes3 sends: two notifications in one list, without a
// delimiter between them.
func TestAuditNotificationRequestMatchesBACpypes3(t *testing.T) {
	peer := mustHex(t, "0e"+bacpypes3AuditFull+bacpypes3AuditMinimal+"0f")
	want := []service.AuditNotification{interopAuditFull(), interopAuditMinimal()}
	got, err := service.DecodeAuditNotificationRequest(peer, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decoded\n %+v\nwant\n %+v", got, want)
	}
	raw, err := service.EncodeAuditNotificationRequest(want)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, peer) {
		t.Fatalf("encoded\n %x\nBACpypes3\n %x", raw, peer)
	}
}

// bacnet-stack encodes result [16] as the bare error code; everything else
// is byte-identical to BACpypes3.
func TestAuditNotificationRequestFromBacnetStack(t *testing.T) {
	got, err := service.DecodeAuditNotificationRequest(mustHex(t, "0e"+bacnetStackAuditFull+"0f"), bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	want := interopAuditFull()
	want.Result = &service.AuditResult{Code: 84, Bare: true}
	if len(got) != 1 || !reflect.DeepEqual(got[0], want) {
		t.Fatalf("decoded\n %+v\nwant\n %+v", got, want)
	}
}

// The layout this library encoded up to v0.3.0 (source-device [0] as a bare
// object identifier, no notifications [0] list) is not an audit notification.
func TestAuditNotificationRejectsProvisionalLayout(t *testing.T) {
	old := mustHex(t, "0c020004d2"+"4901")
	if _, err := service.DecodeAuditNotificationRequest(old, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("got %v, want ErrMalformed", err)
	}
}

func TestAuditNotificationMalformed(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	for name, in := range map[string]string{
		"empty list":             "0e0f",
		"no target-device":       "0e" + "2e0c020004d22f" + "4901" + "0f",
		"no operation":           "0e" + "2e0c020004d22f" + "ae0c020004d2af" + "0f",
		"recipient choice [2]":   "0e" + "2e2c020004d22f" + "4901" + "ae0c020004d2af" + "0f",
		"unknown field [17]":     "0e" + "2e0c020004d22f" + "4901" + "ae0c020004d2af" + "f91101" + "0f",
		"second list after [0]":  "0e" + bacpypes3AuditMinimal + "0f" + "1901",
		"priority out of range":  "0e" + "2e0c020004d22f" + "4901" + "ae0c020004d2af" + "da0100" + "0f",
		"primitive source [2]":   "0e" + "2c020004d2" + "4901" + "ae0c020004d2af" + "0f",
		"application-tagged":     "0e" + "2e0c020004d22f" + "4901" + "ae0c020004d2af" + "2101" + "0f",
		"timestamp two choices":  "0e" + "0e19071908" + "0f" + "2e0c020004d22f" + "4901" + "ae0c020004d2af" + "0f",
		"result one enumeration": "0e" + "2e0c020004d22f" + "4901" + "ae0c020004d2af" + "fe109102ff10" + "0f",
	} {
		if _, err := service.DecodeAuditNotificationRequest(mustHex(t, in), limits); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := service.EncodeAuditNotificationRequest(nil); err == nil {
		t.Error("request without notification encoded")
	}
	both := interopAuditMinimal()
	both.SourceDevice.Address = &service.AuditAddress{}
	if _, err := service.EncodeAuditNotificationRequest([]service.AuditNotification{both}); err == nil {
		t.Error("recipient with device and address encoded")
	}
	if _, err := service.EncodeAuditNotificationRequest([]service.AuditNotification{{Operation: 1}}); err == nil {
		t.Error("notification without recipients encoded")
	}
}

// A constructed target-value survives a round trip as its elements.
func TestAuditNotificationConstructedValue(t *testing.T) {
	n := interopAuditMinimal()
	n.TargetValue = &bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{
		{Value: bacnet.UnsignedValue(1)}, {Value: bacnet.UnsignedValue(2)}}}
	n.TargetProperty = &bacnet.PropertyReference{Identifier: bacnet.PropertyPriorityArray, ArrayIndex: ptr(uint32(8))}
	raw, err := service.EncodeAuditNotificationRequest([]service.AuditNotification{n})
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeAuditNotificationRequest(raw, bacnet.DefaultDecodeLimits())
	if err != nil || len(got) != 1 {
		t.Fatalf("%v %+v", err, got)
	}
	tv := got[0].TargetValue
	if tv == nil || tv.Kind != bacnet.ValueConstructed || len(tv.Elements) != 2 ||
		tv.Elements[0].Value.Unsigned != 1 || tv.Elements[1].Value.Unsigned != 2 {
		t.Fatalf("target-value = %+v", tv)
	}
	if p := got[0].TargetProperty; p == nil || p.Identifier != bacnet.PropertyPriorityArray || p.ArrayIndex == nil || *p.ArrayIndex != 8 {
		t.Fatalf("target-property = %+v", p)
	}
	again, err := service.EncodeAuditNotificationRequest(got)
	if err != nil || !bytes.Equal(again, raw) {
		t.Fatalf("re-encoded %x, want %x (%v)", again, raw, err)
	}
}

// AuditLogQuery by target. BACpypes3's AuditLogQueryParameters decodes these
// bytes to these values (the interop suite checks it live) and Wireshark 4.6
// labels them field by field.
func TestAuditLogQueryByTargetBytes(t *testing.T) {
	req := service.AuditLogQueryRequest{
		AuditLog: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAuditLog, Instance: 1},
		ByTarget: &service.AuditQueryByTarget{
			TargetDevice:     bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1234},
			TargetObject:     &bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
			TargetProperty:   ptr(bacnet.PropertyPresentValue),
			TargetArrayIndex: ptr(uint32(2)),
			TargetPriority:   ptr(uint8(8)),
			Operations:       &bacnet.BitString{UnusedBits: 6, Bytes: []byte{0x40}},
			SuccessFilter:    service.AuditSuccessSuccessesOnly,
		},
		StartAtSequenceNumber: ptr(uint64(100)),
		RequestedCount:        10,
	}
	want := mustHex(t, "0c0f400001"+ // audit-log [0] audit-log:1
		"1e"+"0e"+ // query-parameters [1] { by-target [0] {
		"0c020004d2"+ // target-device-identifier [0]
		"2c00800001"+ // target-object-identifier [2]
		"3955"+ // target-property-identifier [3]
		"4902"+ // target-array-index [4]
		"5908"+ // target-priority [5]
		"6a0640"+ // operations [6] (write)
		"7901"+ // successful-actions-only [7]
		"0f"+"1f"+
		"2964"+ // start-at-sequence-number [2]
		"390a") // requested-count [3]
	raw, err := service.EncodeAuditLogQuery(req)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, want) {
		t.Fatalf("encoded\n %x\nwant\n %x", raw, want)
	}
	got, err := service.DecodeAuditLogQuery(raw, bacnet.DefaultDecodeLimits())
	if err != nil || !reflect.DeepEqual(got, req) {
		t.Fatalf("%v\n got %+v\nwant %+v", err, got, req)
	}
}

func TestAuditLogQueryBySourceBytes(t *testing.T) {
	req := service.AuditLogQueryRequest{
		AuditLog: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAuditLog, Instance: 1},
		BySource: &service.AuditQueryBySource{
			SourceDevice:  bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1234},
			SourceAddress: &service.AuditAddress{Network: 7, MAC: []byte{10, 0, 0, 1, 0xba, 0xc0}},
			SourceObject:  &bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1234},
		},
		RequestedCount: 5,
	}
	want := mustHex(t, "0c0f400001"+"1e"+"1e"+ // query-parameters [1] { by-source [1] {
		"0c020004d2"+ // source-device-identifier [0]
		"1e210765060a000001bac01f"+ // source-device-address [1] { 7, MAC }
		"2c020004d2"+ // source-object-identifier [2]
		"4900"+ // successful-actions-only [4] (all)
		"1f"+"1f"+
		"3905") // requested-count [3]
	raw, err := service.EncodeAuditLogQuery(req)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, want) {
		t.Fatalf("encoded\n %x\nwant\n %x", raw, want)
	}
	got, err := service.DecodeAuditLogQuery(raw, bacnet.DefaultDecodeLimits())
	if err != nil || !reflect.DeepEqual(got, req) {
		t.Fatalf("%v\n got %+v\nwant %+v", err, got, req)
	}
}

func TestAuditLogQueryMalformed(t *testing.T) {
	log := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAuditLog, Instance: 1}
	dev := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
	if _, err := service.EncodeAuditLogQuery(service.AuditLogQueryRequest{AuditLog: log}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Errorf("query without parameters: %v", err)
	}
	if _, err := service.EncodeAuditLogQuery(service.AuditLogQueryRequest{AuditLog: log,
		ByTarget: &service.AuditQueryByTarget{TargetDevice: dev}, BySource: &service.AuditQueryBySource{SourceDevice: dev}}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Errorf("query by target and by source: %v", err)
	}
	limits := bacnet.DefaultDecodeLimits()
	for name, in := range map[string]string{
		// The request this library sent up to v0.3.0: the audit log alone.
		"provisional layout":     "0c0f400001",
		"no requested-count":     "0c0f400001" + "1e0e0c0200000179000f1f",
		"no success filter":      "0c0f400001" + "1e0e0c020000010f1f" + "390a",
		"choice [2]":             "0c0f400001" + "1e2e0c0200000179002f1f" + "390a",
		"by-source priority [5]": "0c0f400001" + "1e1e0c0200000159081f1f" + "390a",
		"requested-count 65536":  "0c0f400001" + "1e0e0c0200000179000f1f" + "3b010000",
	} {
		if _, err := service.DecodeAuditLogQuery(mustHex(t, in), limits); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// AuditLogQuery-ACK as bacnet-stack's bacnet_audit_log_record_encode writes
// its three record kinds (frame written by the bacnet-interop adapter).
func TestAuditLogQueryACKFromBacnetStack(t *testing.T) {
	peer := mustHex(t, "0c0f400001"+"1e"+
		"0964"+"1e"+"0ea47e0a0a06b40c1e00000f"+"1e"+"0a0540"+"1f"+"1f"+
		"0965"+"1e"+"0ea47e0a0a06b40c1e01000f"+"1e"+"1e"+bacnetStackAuditFull+"1f"+"1f"+"1f"+
		"0966"+"1e"+"0ea47e0a0a06b40c1e02000f"+"1e"+"2c3fc00000"+"1f"+"1f"+
		"1f"+"2901")
	got, err := service.DecodeAuditLogQueryACK(peer, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	note := interopAuditFull()
	note.Result = &service.AuditResult{Code: 84, Bare: true}
	stamp := func(sec uint8) service.DateTime {
		return service.DateTime{Date: bacnet.Date{Year: 126, Month: 10, Day: 10, Weekday: 6}, Time: bacnet.Time{Hour: 12, Minute: 30, Second: sec}}
	}
	want := service.AuditLogQueryACK{
		AuditLog: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAuditLog, Instance: 1},
		Records: []service.AuditLogRecordResult{
			{SequenceNumber: 100, Timestamp: stamp(0), LogStatus: &bacnet.BitString{UnusedBits: 5, Bytes: []byte{0x40}}},
			{SequenceNumber: 101, Timestamp: stamp(1), Notification: &note},
			{SequenceNumber: 102, Timestamp: stamp(2), TimeChange: ptr(float32(1.5))},
		},
		NoMoreItems: true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decoded\n %+v\nwant\n %+v", got, want)
	}
}

// AuditLogQuery-ACK with records BACpypes3's AuditLogRecordResult encodes;
// this library's encoder produces the same bytes.
func TestAuditLogQueryACKMatchesBACpypes3(t *testing.T) {
	peer := mustHex(t, "0c0f400001"+"1e"+
		"0964"+"1e"+"0ea47e0a0a06b40c1e00000f"+"1e"+"0a0540"+"1f"+"1f"+
		"0965"+"1e"+"0ea47e0a0a06b40c1e00000f"+"1e"+"1e"+bacpypes3AuditFull+"1f"+"1f"+"1f"+
		"1f"+"2901")
	note := interopAuditFull()
	stamp := service.DateTime{Date: bacnet.Date{Year: 126, Month: 10, Day: 10, Weekday: 6}, Time: bacnet.Time{Hour: 12, Minute: 30}}
	want := service.AuditLogQueryACK{
		AuditLog: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAuditLog, Instance: 1},
		Records: []service.AuditLogRecordResult{
			{SequenceNumber: 100, Timestamp: stamp, LogStatus: &bacnet.BitString{UnusedBits: 5, Bytes: []byte{0x40}}},
			{SequenceNumber: 101, Timestamp: stamp, Notification: &note},
		},
		NoMoreItems: true,
	}
	got, err := service.DecodeAuditLogQueryACK(peer, bacnet.DefaultDecodeLimits())
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("%v\ndecoded\n %+v\nwant\n %+v", err, got, want)
	}
	raw, err := service.EncodeAuditLogQueryACK(want)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, peer) {
		t.Fatalf("encoded\n %x\nBACpypes3\n %x", raw, peer)
	}
}

func TestAuditLogQueryACKMalformed(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	for name, in := range map[string]string{
		"empty":                "",
		"opaque unsigned":      "2101",
		"no no-more-items":     "0c0f400001" + "1e1f",
		"record without datum": "0c0f400001" + "1e" + "0964" + "1e" + "0ea47e0a0a06b40c1e00000f" + "1f" + "1f" + "2901",
		"datum choice [3]":     "0c0f400001" + "1e" + "0964" + "1e" + "0ea47e0a0a06b40c1e00000f" + "1e3901" + "1f" + "1f" + "1f" + "2901",
		"sequence alone":       "0c0f400001" + "1e" + "0964" + "1f" + "2901",
	} {
		if _, err := service.DecodeAuditLogQueryACK(mustHex(t, in), limits); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := service.EncodeAuditLogQueryACK(service.AuditLogQueryACK{Records: []service.AuditLogRecordResult{{}}}); err == nil {
		t.Error("record without log-datum encoded")
	}
}
