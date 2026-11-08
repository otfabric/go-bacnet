// SPDX-License-Identifier: MIT

package service_test

import (
	"encoding/hex"
	"errors"
	"reflect"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

// Recorded ReadRange answers (testdata/captures): the service payloads of
// trend-log-multiple frame 714 (a PolarSoft device's Trend Log Multiple,
// 13 records) and event-log frame 2 (an Event Log, 3 records). Both were
// refused before v0.7.0: every Log_Buffer was decoded as BACnetLogRecord.
var (
	recordedTLM = fromHex(
		"0c06c0000119833a05c0490d5e0ea46f091b02b40e1d10320f1e0a05401f0ea46f091b02b40e1d103c0f1e0a05001f0e" +
			"a46f091b02b40e1d11120f1e1e7e9105912e7f7e9105912e7f7e9105912e7f1f1f0ea46f091b02b40e1d13120f1e1e7e" +
			"9105912e7f7e9105912e7f7e9105912e7f1f1f0ea46f091b02b40e1d15120f1e1e7e9105912e7f7e9105912e7f7e9105" +
			"912e7f1f1f0ea46f091b02b40e1d17120f1e1e7e9105912e7f7e9105912e7f7e9105912e7f1f1f0ea46f091b02b40e1d" +
			"19120f1e1e7e9105912e7f7e9105912e7f7e9105912e7f1f1f0ea46f091b02b40e1d1b120f1e1e7e9105912e7f7e9105" +
			"912e7f7e9105912e7f1f1f0ea46f091b02b40e1d1d120f1e1e7e9105912e7f7e9105912e7f7e9105912e7f1f1f0ea46f" +
			"091b02b40e1d1f120f1e1e7e9105912e7f7e9105912e7f7e9105912e7f1f1f0ea46f091b02b40e1d21120f1e1e7e9105" +
			"912e7f7e9105912e7f7e9105912e7f1f1f0ea46f091b02b40e1d23120f1e1e7e9105912e7f7e9105912e7f7e9105912e" +
			"7f1f1f0ea46f091b02b40e1d25120f1e1e7e9105912e7f7e9105912e7f7e9105912e7f1f1f5f")
	recordedEventLog = fromHex(
		"0c0640000119833a05c049035e0ea46f090703b4120519400f1e0a05001f0ea46f090703b41205205f0f1e1e09641c04" +
			"000fa02c030000013e0c1205205f3f4901590169047d13004c4f3120466c6f6174696e67204c696d697489009901a900" +
			"b902ce4e0c428800001a04802c429000003c400000004fcf1f1f0ea46f090703b412052a5f0f1e1e09641c04000fa02c" +
			"030000013e2ea46f090703b412052a5f2f3f4901590369047d13004c4f3120466c6f6174696e67204c696d6974890099" +
			"01a902b900ce4e0c428cd6631a04002c429000003c400000004fcf1f1f5f")
)

func fromHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

func TestReadRangeACKTrendLogMultipleRecorded(t *testing.T) {
	ack, err := service.DecodeReadRangeACK(recordedTLM, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if ack.Object != (bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeTrendLogMultiple, Instance: 1}) || ack.ItemCount != 13 || len(ack.LogMultipleRecords) != 13 || len(ack.LogRecords) != 0 {
		t.Fatalf("%+v", ack)
	}
	first := ack.LogMultipleRecords[0]
	if first.Choice != service.LogDataLogStatus || first.LogStatus.Bytes[0] != 0x40 ||
		first.Timestamp.Date != (bacnet.Date{Year: 111, Month: 9, Day: 27, Weekday: 2}) {
		t.Fatalf("record 0: %+v", first)
	}
	rec := ack.LogMultipleRecords[2]
	if rec.Choice != service.LogDataValues || len(rec.Values) != 3 {
		t.Fatalf("record 2: %+v", rec)
	}
	for _, v := range rec.Values {
		// failure [7]: Error services / invalid-configuration-data.
		if v.Choice != service.LogDatumFailure || len(v.Value.Elements) != 2 || v.Value.Elements[0].Value.Enumerated != 5 || v.Value.Elements[1].Value.Enumerated != 46 {
			t.Fatalf("value %+v", v)
		}
	}
	again, err := service.EncodeReadRangeACK(ack)
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(again) != hex.EncodeToString(recordedTLM) {
		t.Fatalf("re-encoded\n%x\nrecorded\n%x", again, recordedTLM)
	}
}

func TestReadRangeACKEventLogRecorded(t *testing.T) {
	ack, err := service.DecodeReadRangeACK(recordedEventLog, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if ack.Object.Type != bacnet.ObjectTypeEventLog || ack.ItemCount != 3 || len(ack.EventLogRecords) != 3 {
		t.Fatalf("%+v", ack)
	}
	if ack.EventLogRecords[0].Choice != service.LogDataLogStatus {
		t.Fatalf("record 0: %+v", ack.EventLogRecords[0])
	}
	for _, rec := range ack.EventLogRecords[1:] {
		if rec.Choice != service.LogDataValues || rec.Notification == nil || rec.Notification.ProcessIdentifier != 100 {
			t.Fatalf("record %+v", rec)
		}
	}
	again, err := service.EncodeReadRangeACK(ack)
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(again) != hex.EncodeToString(recordedEventLog) {
		t.Fatalf("re-encoded\n%x\nrecorded\n%x", again, recordedEventLog)
	}
}

func TestLogMultipleRecordRoundTrip(t *testing.T) {
	ts := service.DateTime{Date: bacnet.Date{Year: 126, Month: 11, Day: 8, Weekday: 7}, Time: bacnet.Time{Hour: 12}}
	recs := []service.LogMultipleRecord{
		{Timestamp: ts, Choice: service.LogDataLogStatus, LogStatus: bacnet.BitString{UnusedBits: 5, Bytes: []byte{0x80}}},
		{Timestamp: ts, Choice: service.LogDataTimeChange, TimeChange: 1.5},
		{Timestamp: ts, Choice: service.LogDataValues, Values: []service.LogMultipleValue{
			{Choice: service.LogDatumBoolean, Value: bacnet.BoolValue(true)},
			{Choice: service.LogDatumReal, Value: bacnet.RealValue(21.5)},
			{Choice: service.LogDatumEnumerated, Value: bacnet.EnumValue(3)},
			{Choice: service.LogDatumUnsigned, Value: bacnet.UnsignedValue(7)},
			{Choice: service.LogDatumSigned, Value: bacnet.SignedValue(-7)},
			{Choice: service.LogDatumBitString, Value: bacnet.ApplicationValue{Kind: bacnet.ValueBitString, BitString: bacnet.BitString{UnusedBits: 4, Bytes: []byte{0xF0}}}},
			{Choice: service.LogDatumNull, Value: bacnet.NullValue()},
		}},
	}
	ack := service.ReadRangeACK{
		Object:      bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeTrendLogMultiple, Instance: 2},
		Property:    bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer},
		ResultFlags: service.EncodeResultFlags(true, true, false), ItemCount: 3, LogMultipleRecords: recs,
	}
	p, err := service.EncodeReadRangeACK(ack)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeReadRangeACK(p, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.LogMultipleRecords, recs) {
		t.Fatalf("got %+v\nwant %+v", got.LogMultipleRecords, recs)
	}
}

func TestEventLogRecordRoundTrip(t *testing.T) {
	ts := service.DateTime{Date: bacnet.Date{Year: 126, Month: 11, Day: 8, Weekday: 7}}
	note := service.EventNotification{
		ProcessIdentifier: 1, InitiatingDevice: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		EventObject:       bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogInput, Instance: 1},
		TimeStamp:         service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 4},
		NotificationClass: 1, Priority: 100, EventType: 5, NotifyType: 0, ToState: 0,
	}
	recs := []service.EventLogRecord{
		{Timestamp: ts, Choice: service.LogDataLogStatus, LogStatus: bacnet.BitString{UnusedBits: 5, Bytes: []byte{0x40}}},
		{Timestamp: ts, Choice: service.LogDataTimeChange, TimeChange: -2},
		{Timestamp: ts, Choice: service.LogDataValues, Notification: &note},
	}
	ack := service.ReadRangeACK{
		Object:      bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeEventLog, Instance: 1},
		Property:    bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer},
		ResultFlags: service.EncodeResultFlags(true, true, false), ItemCount: 3, EventLogRecords: recs,
	}
	p, err := service.EncodeReadRangeACK(ack)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeReadRangeACK(p, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.EventLogRecords) != 3 || got.EventLogRecords[1].TimeChange != -2 || got.EventLogRecords[2].Notification.Priority != 100 {
		t.Fatalf("%+v", got.EventLogRecords)
	}
}

func TestLogMultipleRecordErrors(t *testing.T) {
	ts := service.DateTime{}
	bad := []service.LogMultipleRecord{
		{Timestamp: ts, Choice: 9},
		{Timestamp: ts, Choice: service.LogDataValues, Values: []service.LogMultipleValue{{Choice: service.LogDatumTimeChange, Value: bacnet.RealValue(1)}}},
		{Timestamp: ts, Choice: service.LogDataValues, Values: []service.LogMultipleValue{{Choice: service.LogDatumLogStatus}}},
		{Timestamp: ts, Choice: service.LogDataValues, Values: []service.LogMultipleValue{{Choice: service.LogDatumReal, Value: bacnet.BoolValue(true)}}},
	}
	for i, r := range bad {
		if _, err := service.EncodeLogMultipleRecord(r); !errors.Is(err, bacnet.ErrMalformed) {
			t.Errorf("%d: %v", i, err)
		}
	}
	for i, r := range []service.EventLogRecord{{Choice: 9}, {Choice: service.LogDataValues}} {
		if _, err := service.EncodeEventLogRecord(r); !errors.Is(err, bacnet.ErrMalformed) {
			t.Errorf("event %d: %v", i, err)
		}
	}
	if _, err := service.EncodeEventLogRecord(service.EventLogRecord{Choice: service.LogDataValues, Notification: &service.EventNotification{TimeStamp: service.TimeStamp{Choice: 9}}}); err == nil {
		t.Error("bad notification encoded")
	}
}

func TestDecodeLogRecordsMalformed(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	parse := func(h string) []bacnet.Element {
		t.Helper()
		els, _, err := bacnet.ParseSequence(fromHex(h), limits, -1)
		if err != nil {
			t.Fatal(err)
		}
		return els
	}
	ts := "0ea46f091b02b40e1d10320f"
	cases := map[string]string{
		"odd elements":           ts,
		"timestamp not [0]":      "1ea46f091b02b40e1d10321f" + "1e0a05401f",
		"timestamp one element":  "0ea46f091b020f" + "1e0a05401f",
		"timestamp kinds":        "0e2101b40e1d10320f" + "1e0a05401f",
		"datum not [1]":          ts + "2e0a05402f",
		"datum empty":            ts + "1e1f",
		"datum application":      ts + "1e21011f",
		"unknown choice":         ts + "1e3a05401f",
		"status not bit string":  ts + "1e09011f",
		"time change not real":   ts + "1e29011f",
		"values not constructed": ts + "1e19011f",
		"value application":      ts + "1e1e21011f1f",
		"value tag 9":            ts + "1e1e99011f1f",
		"value bad boolean":      ts + "1e1e0a01011f1f",
	}
	for name, h := range cases {
		els := parse(h)
		if _, err := service.DecodeLogMultipleRecords(els, len(els)/2); !errors.Is(err, bacnet.ErrMalformed) {
			t.Errorf("multiple %s: %v", name, err)
		}
	}
	if _, err := service.DecodeLogMultipleRecords(parse(ts+"1e0a05401f"), 2); !errors.Is(err, bacnet.ErrMalformed) {
		t.Error("count larger than itemData accepted")
	}
	if recs, err := service.DecodeLogMultipleRecords(nil, 0); err != nil || recs != nil {
		t.Errorf("empty: %v %v", recs, err)
	}
	for name, h := range map[string]string{
		"unknown choice":         ts + "1e3a05401f",
		"status":                 ts + "1e09011f",
		"time change":            ts + "1e29011f",
		"notification primitive": ts + "1e19011f",
		"notification malformed": ts + "1e1e09011f1f",
		"timestamp":              "0e0f" + "1e0a05401f",
	} {
		els := parse(h)
		if _, err := service.DecodeEventLogRecords(els, len(els)/2, limits); !errors.Is(err, bacnet.ErrMalformed) {
			t.Errorf("event %s: %v", name, err)
		}
	}
	if _, err := service.DecodeEventLogRecords(nil, -1, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Error("negative count accepted")
	}
}

func FuzzDecodeLogMultipleRecords(f *testing.F) {
	f.Add(recordedTLM)
	f.Add(recordedEventLog)
	f.Fuzz(func(t *testing.T, data []byte) {
		limits := bacnet.DefaultDecodeLimits()
		els, _, err := bacnet.ParseSequence(data, limits, -1)
		if err != nil {
			return
		}
		for _, n := range []int{0, 1, len(els) / 2} {
			_, _ = service.DecodeLogMultipleRecords(els, n)
			_, _ = service.DecodeEventLogRecords(els, n, limits)
		}
		ack, err := service.DecodeReadRangeACK(data, limits)
		if err != nil || len(ack.LogMultipleRecords)+len(ack.EventLogRecords) == 0 {
			return // the generic itemData path has no encode guarantee
		}
		if _, err := service.EncodeReadRangeACK(ack); err != nil {
			t.Fatalf("decoded ACK does not encode: %v", err)
		}
	})
}
