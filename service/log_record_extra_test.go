// SPDX-License-Identifier: MIT

package service

import (
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
)

func TestLogRecordDatumChoicesRoundTrip(t *testing.T) {
	flags := bacnet.BitString{UnusedBits: 4, Bytes: []byte{0x00}}
	ts := DateTime{Date: bacnet.Date{Year: 126, Month: 3, Day: 4, Weekday: 2}, Time: bacnet.Time{Hour: 9}}
	cases := []LogRecord{
		{Timestamp: ts, DatumChoice: LogDatumLogStatus, Datum: bacnet.ApplicationValue{Kind: bacnet.ValueBitString, BitString: bacnet.BitString{UnusedBits: 5, Bytes: []byte{0x80}}}, StatusFlags: &flags},
		{Timestamp: ts, DatumChoice: LogDatumBoolean, Datum: bacnet.BoolValue(false)},
		{Timestamp: ts, DatumChoice: LogDatumEnumerated, Datum: bacnet.EnumValue(3), StatusFlags: &flags},
		{Timestamp: ts, DatumChoice: LogDatumUnsigned, Datum: bacnet.UnsignedValue(42), StatusFlags: &flags},
		{Timestamp: ts, DatumChoice: LogDatumSigned, Datum: bacnet.SignedValue(-7), StatusFlags: &flags},
		{Timestamp: ts, DatumChoice: LogDatumBitString, Datum: bacnet.ApplicationValue{Kind: bacnet.ValueBitString, BitString: bacnet.BitString{UnusedBits: 4, Bytes: []byte{0xf0}}}, StatusFlags: &flags},
		{Timestamp: ts, DatumChoice: LogDatumNull, Datum: bacnet.NullValue()},
		{Timestamp: ts, DatumChoice: LogDatumTimeChange, Datum: bacnet.RealValue(1.5), StatusFlags: &flags},
		{Timestamp: ts, DatumChoice: LogDatumAnyValue, Datum: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{{Value: bacnet.RealValue(9)}}}, StatusFlags: &flags},
	}
	for _, rec := range cases {
		els, err := EncodeLogRecord(rec)
		if err != nil {
			t.Fatalf("choice %d encode: %v", rec.DatumChoice, err)
		}
		got, err := DecodeLogRecords(els, 1)
		if err != nil {
			t.Fatalf("choice %d decode: %v", rec.DatumChoice, err)
		}
		if got[0].DatumChoice != rec.DatumChoice {
			t.Fatalf("choice %d got %d", rec.DatumChoice, got[0].DatumChoice)
		}
	}
}

func TestLogRecordEncodeDecodeErrors(t *testing.T) {
	if _, err := EncodeLogRecord(LogRecord{DatumChoice: LogDatumReal, Datum: bacnet.BoolValue(true)}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("kind mismatch: %v", err)
	}
	if _, err := EncodeLogRecord(LogRecord{DatumChoice: 99, Datum: bacnet.NullValue()}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("bad choice: %v", err)
	}
	if _, err := DecodeLogRecords(nil, -1); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("neg count: %v", err)
	}
	if _, err := DecodeLogRecords([]bacnet.Element{{Value: bacnet.RealValue(1)}}, 0); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("empty count with data: %v", err)
	}
	got, err := DecodeLogRecords(nil, 0)
	if err != nil || got != nil {
		t.Fatalf("empty ok: %v %#v", err, got)
	}
	if _, err := DecodeLogRecords([]bacnet.Element{{Context: true, TagNumber: 1}}, 1); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("truncated: %v", err)
	}
}

func TestContextLengthPrefixExtended(t *testing.T) {
	lvt, prefix := contextLengthPrefix(10)
	if lvt != 5 || len(prefix) != 1 || prefix[0] != 10 {
		t.Fatalf("lvt=%d prefix=%v", lvt, prefix)
	}
	lvt, prefix = contextLengthPrefix(300)
	if lvt != 5 || len(prefix) != 3 || prefix[0] != 254 {
		t.Fatalf("16-bit: lvt=%d prefix=%v", lvt, prefix)
	}
	lvt, prefix = contextLengthPrefix(70000)
	if lvt != 5 || len(prefix) != 5 || prefix[0] != 255 {
		t.Fatalf("32-bit: lvt=%d prefix=%v", lvt, prefix)
	}
}

func TestLogRecordFailureAndTrailing(t *testing.T) {
	ts := DateTime{Date: bacnet.Date{Year: 126, Month: 1, Day: 1, Weekday: 1}, Time: bacnet.Time{}}
	rec := LogRecord{
		Timestamp:   ts,
		DatumChoice: LogDatumFailure,
		Datum: bacnet.ApplicationValue{
			Kind: bacnet.ValueConstructed,
			Elements: []bacnet.Element{
				{Value: bacnet.UnsignedValue(2)},
				{Value: bacnet.UnsignedValue(32)},
			},
		},
	}
	els, err := EncodeLogRecord(rec)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeLogRecords(els, 1)
	if err != nil || got[0].DatumChoice != LogDatumFailure {
		t.Fatalf("%v %+v", err, got)
	}
	// Extra trailing element after one record.
	extra := append(append([]bacnet.Element{}, els...), bacnet.Element{Value: bacnet.RealValue(1)})
	if _, err := DecodeLogRecords(extra, 1); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("trailing: %v", err)
	}
	if _, err := EncodeLogRecord(LogRecord{DatumChoice: LogDatumBoolean, Datum: bacnet.RealValue(1)}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("bool kind: %v", err)
	}
	if _, err := EncodeLogRecord(LogRecord{DatumChoice: LogDatumFailure, Datum: bacnet.RealValue(1)}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("failure kind: %v", err)
	}
}

func TestLogRecordParseOneElementConstructedAndErrors(t *testing.T) {
	raw, err := bacnet.AppendContextTagged(nil, 2, []bacnet.Element{{Value: bacnet.RealValue(3.25)}})
	if err != nil {
		t.Fatal(err)
	}
	els, err := parseOneElement(raw)
	if err != nil || len(els) != 1 || !bacnet.IsContextConstructed(els[0]) {
		t.Fatalf("constructed: %v %+v", err, els)
	}
	if _, err := parseOneElement(nil); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("empty: %v", err)
	}
	prim, err := bacnet.AppendContextBool(nil, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseOneElement(append(prim, 0x00)); !errors.Is(err, bacnet.ErrTrailingData) {
		t.Fatalf("trailing: %v", err)
	}
	if _, err := parseOneElement([]byte{0x2e}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("open only: %v", err)
	}
}

func TestLogRecordDecodeDatumValueForms(t *testing.T) {
	constructedReal := bacnet.Element{
		Context: true, TagNumber: 2,
		Value: bacnet.ApplicationValue{
			Kind:     bacnet.ValueConstructed,
			Elements: []bacnet.Element{{Value: bacnet.RealValue(8.5)}},
		},
	}
	v, err := decodeLogDatumValue(LogDatumReal, constructedReal)
	if err != nil || v.Real != 8.5 {
		t.Fatalf("constructed real: %v %+v", err, v)
	}
	if _, err := decodeLogDatumValue(LogDatumReal, bacnet.Element{
		Context: true, TagNumber: 2,
		Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed},
	}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("bad constructed real: %v", err)
	}
	if _, err := decodeLogDatumValue(LogDatumReal, bacnet.Element{
		Context: true, TagNumber: 2,
		Value: bacnet.ApplicationValue{Kind: bacnet.ValueContext, OctetString: []byte{1, 2}},
	}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("real length: %v", err)
	}
	bs := bacnet.BitString{UnusedBits: 4, Bytes: []byte{0xa0}}
	v, err = decodeLogDatumValue(LogDatumBitString, bacnet.Element{
		Context: true, TagNumber: 6,
		Value: bacnet.ApplicationValue{
			Kind:     bacnet.ValueConstructed,
			Elements: []bacnet.Element{{Value: bacnet.ApplicationValue{Kind: bacnet.ValueBitString, BitString: bs}}},
		},
	})
	if err != nil || v.Kind != bacnet.ValueBitString {
		t.Fatalf("constructed bs: %v %+v", err, v)
	}
	v, err = decodeLogDatumValue(LogDatumFailure, bacnet.Element{
		Context: true, TagNumber: 8,
		Value: bacnet.ApplicationValue{Kind: bacnet.ValueContext, OctetString: []byte{1}},
	})
	if err != nil || v.Kind != bacnet.ValueContext {
		t.Fatalf("failure prim: %v %+v", err, v)
	}
	if _, err := decodeLogDatumValue(99, bacnet.Element{Context: true}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("unknown choice: %v", err)
	}
	if _, err := decodeLogDatumValue(LogDatumBoolean, bacnet.Element{
		Context: true, TagNumber: 1,
		Value: bacnet.ApplicationValue{Kind: bacnet.ValueContext, OctetString: []byte{2}},
	}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("bad bool: %v", err)
	}
}

func TestLogRecordDecodeMalformedFields(t *testing.T) {
	if _, _, err := decodeLogRecord(nil); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("nil: %v", err)
	}
	badTS := []bacnet.Element{
		{Context: true, TagNumber: 0, Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed}},
		{Context: true, TagNumber: 1},
	}
	if _, _, err := decodeLogRecord(badTS); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("bad ts contents: %v", err)
	}
	badKinds := []bacnet.Element{
		{Context: true, TagNumber: 0, Value: bacnet.ApplicationValue{
			Kind: bacnet.ValueConstructed,
			Elements: []bacnet.Element{
				{Value: bacnet.UnsignedValue(1)},
				{Value: bacnet.UnsignedValue(2)},
			},
		}},
		{Context: true, TagNumber: 1, Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed}},
	}
	if _, _, err := decodeLogRecord(badKinds); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("bad ts kinds: %v", err)
	}
	okTS := bacnet.Element{
		Context: true, TagNumber: 0,
		Value: bacnet.ApplicationValue{
			Kind: bacnet.ValueConstructed,
			Elements: []bacnet.Element{
				{Value: bacnet.ApplicationValue{Kind: bacnet.ValueDate}},
				{Value: bacnet.ApplicationValue{Kind: bacnet.ValueTime}},
			},
		},
	}
	if _, _, err := decodeLogRecord([]bacnet.Element{
		okTS,
		{Context: true, TagNumber: 3, Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed}},
	}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("bad datum tag: %v", err)
	}
	if _, _, err := decodeLogRecord([]bacnet.Element{
		okTS,
		{Context: true, TagNumber: 1, Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed}},
	}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("empty choice: %v", err)
	}
	if _, _, err := decodeLogRecord([]bacnet.Element{
		okTS,
		{Context: true, TagNumber: 1, Value: bacnet.ApplicationValue{
			Kind:     bacnet.ValueConstructed,
			Elements: []bacnet.Element{{Value: bacnet.RealValue(1)}},
		}},
	}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("non-context choice: %v", err)
	}
}

func TestLogRecordEncodeDatumKindMismatches(t *testing.T) {
	bad := []struct {
		choice LogDatumChoice
		val    bacnet.ApplicationValue
	}{
		{LogDatumLogStatus, bacnet.RealValue(1)},
		{LogDatumEnumerated, bacnet.RealValue(1)},
		{LogDatumUnsigned, bacnet.RealValue(1)},
		{LogDatumSigned, bacnet.RealValue(1)},
		{LogDatumTimeChange, bacnet.BoolValue(true)},
	}
	for _, tc := range bad {
		if _, err := encodeLogDatum(tc.choice, tc.val); !errors.Is(err, bacnet.ErrMalformed) {
			t.Fatalf("choice %d: %v", tc.choice, err)
		}
	}
}
