// SPDX-License-Identifier: MIT

package bacnet_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
)

func TestApplicationValueExtendedKinds(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	cases := []bacnet.ApplicationValue{
		bacnet.DoubleValue(3.1415926535),
		{Kind: bacnet.ValueOctetString, OctetString: []byte{0xDE, 0xAD, 0xBE, 0xEF}},
		{Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Encoding: 0, Value: "Interop"}},
		{Kind: bacnet.ValueBitString, BitString: bacnet.BitString{UnusedBits: 3, Bytes: []byte{0xF0}}},
		{Kind: bacnet.ValueDate, Date: bacnet.Date{Year: 126, Month: 8, Day: 1, Weekday: 6}},
		{Kind: bacnet.ValueTime, Time: bacnet.Time{Hour: 13, Minute: 30, Second: 0, Hundredths: 50}},
	}
	for _, want := range cases {
		enc, err := bacnet.AppendApplicationValue(nil, want)
		if err != nil {
			t.Fatalf("encode kind %d: %v", want.Kind, err)
		}
		got, n, err := bacnet.ParseApplicationValue(enc, limits)
		if err != nil || n != len(enc) {
			t.Fatalf("decode kind %d: %v n=%d", want.Kind, err, n)
		}
		re, err := bacnet.AppendApplicationValue(nil, got)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(enc, re) {
			t.Fatalf("kind %d reencode %x vs %x", want.Kind, enc, re)
		}
		if got.TagNumber() == bacnet.TagNull && want.Kind != bacnet.ValueNull {
			t.Fatalf("tag number for kind %d", want.Kind)
		}
	}
}

func TestContextBoolRoundTrip(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	for _, v := range []bool{true, false} {
		enc, err := bacnet.AppendContextBool(nil, 2, v)
		if err != nil {
			t.Fatal(err)
		}
		el, n, err := bacnet.ParseTag(enc, limits)
		if err != nil || n != len(enc) || !el.Context || el.TagNumber != 2 {
			t.Fatalf("parse %#v n=%d err=%v", el, n, err)
		}
		got, err := bacnet.ContextBool(el)
		if err != nil || got != v {
			t.Fatalf("ContextBool got %v err=%v want %v", got, err, v)
		}
	}
}

func TestContextObjectIDAndNull(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	oid := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 99}
	enc, err := bacnet.AppendContextObjectID(nil, 1, oid)
	if err != nil {
		t.Fatal(err)
	}
	el, _, err := bacnet.ParseTag(enc, limits)
	if err != nil {
		t.Fatal(err)
	}
	got, err := bacnet.ContextObjectID(el)
	if err != nil || got != oid {
		t.Fatalf("%v %v", got, err)
	}

	enc, err = bacnet.AppendContextNull(nil, 3)
	if err != nil {
		t.Fatal(err)
	}
	el, _, err = bacnet.ParseTag(enc, limits)
	if err != nil || el.TagNumber != 3 || !el.Context {
		t.Fatalf("%#v %v", el, err)
	}
}

func TestExtendedLengthOctetString(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	long := bytes.Repeat([]byte{0xAB}, 100)
	v := bacnet.ApplicationValue{Kind: bacnet.ValueOctetString, OctetString: long}
	enc, err := bacnet.AppendApplicationValue(nil, v)
	if err != nil {
		t.Fatal(err)
	}
	got, n, err := bacnet.ParseApplicationValue(enc, limits)
	if err != nil || n != len(enc) || !bytes.Equal(got.OctetString, long) {
		t.Fatalf("err=%v n=%d len=%d", err, n, len(got.OctetString))
	}
}

func TestParseTagMalformed(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	if _, _, err := bacnet.ParseTag(nil, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("empty: %v", err)
	}
	// truncated extended length
	if _, _, err := bacnet.ParseTag([]byte{0x65, 0xFE}, limits); err == nil {
		t.Fatal("expected truncated extended length error")
	}
}
