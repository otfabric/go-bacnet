// SPDX-License-Identifier: MIT

package service

import (
	"testing"

	"github.com/otfabric/go-bacnet"
)

func TestLogRecordRoundTripReal(t *testing.T) {
	flags := bacnet.BitString{UnusedBits: 4, Bytes: []byte{0x00}}
	rec := LogRecord{
		Timestamp: DateTime{
			Date: bacnet.Date{Year: 126, Month: 8, Day: 1, Weekday: 6},
			Time: bacnet.Time{Hour: 10, Minute: 11, Second: 12, Hundredths: 0},
		},
		DatumChoice: LogDatumReal,
		Datum:       bacnet.RealValue(21.5),
		StatusFlags: &flags,
	}
	els, err := EncodeLogRecord(rec)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeLogRecords(els, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].DatumChoice != LogDatumReal || got[0].Datum.Real != 21.5 {
		t.Fatalf("datum %+v", got[0])
	}
	if got[0].StatusFlags == nil || got[0].Timestamp.Time.Hour != 10 {
		t.Fatalf("record %+v", got[0])
	}
}

func TestReadRangeACKLogRecordSplit(t *testing.T) {
	flags := bacnet.BitString{UnusedBits: 4, Bytes: []byte{0x00}}
	records := []LogRecord{
		{
			Timestamp:   DateTime{Date: bacnet.Date{Year: 126, Month: 1, Day: 2, Weekday: 1}, Time: bacnet.Time{Hour: 1}},
			DatumChoice: LogDatumReal,
			Datum:       bacnet.RealValue(20),
			StatusFlags: &flags,
		},
		{
			Timestamp:   DateTime{Date: bacnet.Date{Year: 126, Month: 1, Day: 2, Weekday: 1}, Time: bacnet.Time{Hour: 2}},
			DatumChoice: LogDatumReal,
			Datum:       bacnet.RealValue(21),
			StatusFlags: &flags,
		},
	}
	ack := ReadRangeACK{
		Object:      bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeTrendLog, Instance: 1},
		Property:    bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer},
		ResultFlags: EncodeResultFlags(true, true, false),
		ItemCount:   2,
		LogRecords:  records,
	}
	raw, err := EncodeReadRangeACK(ack)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeReadRangeACK(raw, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.LogRecords) != 2 {
		t.Fatalf("LogRecords=%d ItemData=%d", len(got.LogRecords), len(got.ItemData))
	}
	if got.LogRecords[0].Datum.Real != 20 || got.LogRecords[1].Datum.Real != 21 {
		t.Fatalf("records %+v", got.LogRecords)
	}
	// Flat ItemData has more tags than ItemCount (timestamp/datum/flags each).
	if len(got.ItemData) <= 2 {
		t.Fatalf("expected flat tag stream longer than ItemCount, got %d", len(got.ItemData))
	}
}

func TestLogRecordOptionalStatusFlags(t *testing.T) {
	rec := LogRecord{
		Timestamp:   DateTime{Date: bacnet.Date{Year: 126, Month: 1, Day: 1, Weekday: 1}, Time: bacnet.Time{}},
		DatumChoice: LogDatumBoolean,
		Datum:       bacnet.BoolValue(true),
	}
	els, err := EncodeLogRecord(rec)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeLogRecords(els, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].StatusFlags != nil || !got[0].Datum.Boolean {
		t.Fatalf("%+v", got[0])
	}
}
