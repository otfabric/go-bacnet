// SPDX-License-Identifier: MIT

package service_test

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

// One BACnetLogMultipleRecord whose log-data list holds a single item with
// context tag n: timestamp [0], log-data [1] { log-data [1] { item } }.
func logDataListRecord(item string) []byte {
	return fromHex("0ea46f091b02b40e1d11120f" + "1e1e" + item + "1f1f")
}

// The log-data list of BACnetLogData numbers its alternatives boolean-value
// [0] to failure [7] and any-value [8]; each decodes to the LogDatumChoice of
// the same name and encodes back to the same octets.
func TestLogMultipleValueListTags(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	// constructed holds application values as a decoder returns them.
	constructed := func(vals ...bacnet.ApplicationValue) bacnet.ApplicationValue {
		var raw []byte
		for _, v := range vals {
			var err error
			if raw, err = bacnet.AppendApplicationValue(raw, v); err != nil {
				t.Fatal(err)
			}
		}
		els, _, err := bacnet.ParseSequence(raw, limits, -1)
		if err != nil {
			t.Fatal(err)
		}
		return bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: els}
	}
	text := bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: "ab"}}
	for _, tc := range []struct {
		name   string
		item   string
		choice service.LogDatumChoice
		value  bacnet.ApplicationValue
	}{
		{"boolean-value [0]", "0901", service.LogDatumBoolean, bacnet.BoolValue(true)},
		{"real-value [1]", "1c41ac0000", service.LogDatumReal, bacnet.RealValue(21.5)},
		{"enumerated-value [2]", "2903", service.LogDatumEnumerated, bacnet.EnumValue(3)},
		{"unsigned-value [3]", "3907", service.LogDatumUnsigned, bacnet.UnsignedValue(7)},
		{"integer-value [4]", "49f9", service.LogDatumSigned, bacnet.SignedValue(-7)},
		{"bitstring-value [5]", "5a04f0", service.LogDatumBitString, bacnet.ApplicationValue{Kind: bacnet.ValueBitString, BitString: bacnet.BitString{UnusedBits: 4, Bytes: []byte{0xF0}}}},
		{"null-value [6]", "68", service.LogDatumNull, bacnet.NullValue()},
		{"failure [7]", "7e9105912e7f", service.LogDatumFailure, constructed(bacnet.EnumValue(5), bacnet.EnumValue(46))},
		{"any-value [8] REAL", "8e4441ac00008f", service.LogDatumAnyValue, constructed(bacnet.RealValue(21.5))},
		{"any-value [8] CharacterString", "8e730061628f", service.LogDatumAnyValue, constructed(text)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire := logDataListRecord(tc.item)
			els, _, err := bacnet.ParseSequence(wire, limits, -1)
			if err != nil {
				t.Fatal(err)
			}
			recs, err := service.DecodeLogMultipleRecords(els, 1)
			if err != nil {
				t.Fatal(err)
			}
			if len(recs) != 1 || recs[0].Choice != service.LogDataValues || len(recs[0].Values) != 1 {
				t.Fatalf("records %+v", recs)
			}
			got := recs[0].Values[0]
			if got.Choice != tc.choice || !reflect.DeepEqual(got.Value, tc.value) {
				t.Fatalf("decoded choice %d value %+v, want choice %d value %+v", got.Choice, got.Value, tc.choice, tc.value)
			}
			out, err := service.EncodeLogMultipleRecord(recs[0])
			if err != nil {
				t.Fatalf("decoded record does not encode: %v", err)
			}
			var again []byte
			for _, el := range out {
				if again, err = bacnet.AppendTag(again, el); err != nil {
					t.Fatal(err)
				}
			}
			if !bytes.Equal(again, wire) {
				t.Fatalf("re-encoded %x, want %x", again, wire)
			}
		})
	}
}

// A list item with a context tag above any-value [8] is not a BACnetLogData
// alternative, and the BACnetLogRecord alternatives that the list does not
// have (log-status, time-change) are not encoded into it.
func TestLogMultipleValueListTagsRefused(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	for _, item := range []string{"9c41ac0000", "9e4441ac00009f", "ae4441ac0000af"} {
		els, _, err := bacnet.ParseSequence(logDataListRecord(item), limits, -1)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.DecodeLogMultipleRecords(els, 1); !errors.Is(err, bacnet.ErrMalformed) {
			t.Errorf("item %s: %v, want ErrMalformed", item, err)
		}
	}
	ts := service.DateTime{Date: bacnet.Date{Year: 126, Month: 11, Day: 8, Weekday: 7}}
	for _, c := range []service.LogDatumChoice{service.LogDatumLogStatus, service.LogDatumTimeChange, service.LogDatumAnyValue + 1} {
		rec := service.LogMultipleRecord{Timestamp: ts, Choice: service.LogDataValues, Values: []service.LogMultipleValue{{Choice: c, Value: bacnet.RealValue(1)}}}
		if _, err := service.EncodeLogMultipleRecord(rec); !errors.Is(err, bacnet.ErrMalformed) {
			t.Errorf("choice %d: %v, want ErrMalformed", c, err)
		}
	}
}

// A ReadRange-ACK of a Trend Log Multiple with failure and any-value items
// keeps them through encode and decode.
func TestReadRangeACKLogMultipleAnyValueRoundTrip(t *testing.T) {
	ts := service.DateTime{Date: bacnet.Date{Year: 126, Month: 11, Day: 8, Weekday: 7}, Time: bacnet.Time{Hour: 12}}
	limits := bacnet.DefaultDecodeLimits()
	constructed := func(vals ...bacnet.ApplicationValue) bacnet.ApplicationValue {
		var raw []byte
		for _, v := range vals {
			var err error
			if raw, err = bacnet.AppendApplicationValue(raw, v); err != nil {
				t.Fatal(err)
			}
		}
		els, _, err := bacnet.ParseSequence(raw, limits, -1)
		if err != nil {
			t.Fatal(err)
		}
		return bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: els}
	}
	recs := []service.LogMultipleRecord{{Timestamp: ts, Choice: service.LogDataValues, Values: []service.LogMultipleValue{
		{Choice: service.LogDatumFailure, Value: constructed(bacnet.EnumValue(2), bacnet.EnumValue(31))},
		{Choice: service.LogDatumAnyValue, Value: constructed(bacnet.RealValue(-1.25))},
		{Choice: service.LogDatumAnyValue, Value: constructed(bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: "open"}})},
	}}}
	ack := service.ReadRangeACK{
		Object:      bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeTrendLogMultiple, Instance: 2},
		Property:    bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer},
		ResultFlags: service.EncodeResultFlags(true, true, false), ItemCount: 1, LogMultipleRecords: recs,
	}
	p, err := service.EncodeReadRangeACK(ack)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeReadRangeACK(p, limits)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.LogMultipleRecords, recs) {
		t.Fatalf("got %+v\nwant %+v", got.LogMultipleRecords, recs)
	}
}
