// SPDX-License-Identifier: MIT

package bacnet_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
)

func TestExtendedLengthEncodings(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	// 16-bit extended length path (length >= 254).
	long := bytes.Repeat([]byte{0x11}, 300)
	v := bacnet.ApplicationValue{Kind: bacnet.ValueOctetString, OctetString: long}
	enc, err := bacnet.AppendApplicationValue(nil, v)
	if err != nil {
		t.Fatal(err)
	}
	got, n, err := bacnet.ParseApplicationValue(enc, limits)
	if err != nil || n != len(enc) || !bytes.Equal(got.OctetString, long) {
		t.Fatalf("16-bit ext: err=%v n=%d len=%d", err, n, len(got.OctetString))
	}

	// Extended tag number (>=15) via context tag.
	enc, err = bacnet.AppendContextUnsigned(nil, 20, 7)
	if err != nil {
		t.Fatal(err)
	}
	el, n, err := bacnet.ParseTag(enc, limits)
	if err != nil || n != len(enc) || el.TagNumber != 20 {
		t.Fatalf("ext tag: %#v n=%d err=%v", el, n, err)
	}
}

func TestDecodeApplicationValueErrorPaths(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	cases := []struct {
		name string
		raw  []byte
		want error
	}{
		{"null with value", []byte{0x01, 0x00}, bacnet.ErrMalformed},
		{"real short", []byte{0x44, 0x00, 0x00}, bacnet.ErrMalformed},
		{"double short", []byte{0x55, 0x00, 0x00, 0x00, 0x00}, bacnet.ErrMalformed},
		{"char truncated", []byte{0x70}, bacnet.ErrMalformed},
		{"bit truncated", []byte{0x80}, bacnet.ErrMalformed},
		{"date short", []byte{0xA2, 0x01, 0x02}, bacnet.ErrMalformed},
		{"time short", []byte{0xB2, 0x01, 0x02}, bacnet.ErrMalformed},
		{"object id short", []byte{0xC2, 0x00, 0x00}, bacnet.ErrMalformed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := bacnet.ParseApplicationValue(tc.raw, limits)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want %v", err, tc.want)
			}
		})
	}

	tiny := bacnet.DecodeLimits{MaxOctetStringLength: 2, MaxCharacterLength: 2}
	tiny = tiny.Normalize()
	enc, err := bacnet.AppendApplicationValue(nil, bacnet.ApplicationValue{
		Kind: bacnet.ValueOctetString, OctetString: []byte{1, 2, 3},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := bacnet.ParseApplicationValue(enc, tiny); !errors.Is(err, bacnet.ErrLimitExceeded) {
		t.Fatalf("octet limit: %v", err)
	}
	enc, err = bacnet.AppendApplicationValue(nil, bacnet.ApplicationValue{
		Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: "abcd"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := bacnet.ParseApplicationValue(enc, tiny); !errors.Is(err, bacnet.ErrLimitExceeded) {
		t.Fatalf("char limit: %v", err)
	}
}

func TestContextBoolErrorsAndConstructedEncode(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	el := bacnet.Element{Context: true, TagNumber: 2, Value: bacnet.UnsignedValue(1)}
	if _, err := bacnet.ContextBool(el); err == nil {
		t.Fatal("expected ContextBool kind error")
	}
	el = bacnet.Element{Context: false, TagNumber: 1, Value: bacnet.BoolValue(true)}
	if _, err := bacnet.ContextBool(el); err == nil {
		t.Fatal("expected non-context error")
	}

	constructed := bacnet.ApplicationValue{
		Kind: bacnet.ValueConstructed,
		Elements: []bacnet.Element{
			{Value: bacnet.UnsignedValue(1)},
			{Value: bacnet.BoolValue(true)},
		},
	}
	enc, err := bacnet.AppendApplicationValue(nil, constructed)
	if err != nil {
		t.Fatal(err)
	}
	if len(enc) == 0 {
		t.Fatal("empty constructed encode")
	}

	if _, err := bacnet.AppendApplicationValue(nil, bacnet.ApplicationValue{Kind: bacnet.ValueKind(200)}); !errors.Is(err, bacnet.ErrUnsupported) {
		t.Fatalf("unsupported kind: %v", err)
	}

	// ParseApplicationValue rejects context tags.
	ctx, err := bacnet.AppendContextUnsigned(nil, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := bacnet.ParseApplicationValue(ctx, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("context as app: %v", err)
	}
}

func TestSignedAndBooleanLVTForms(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	for _, s := range []int64{-128, -1, 0, 127, 128, 32767, -32768} {
		enc, err := bacnet.AppendApplicationValue(nil, bacnet.SignedValue(s))
		if err != nil {
			t.Fatalf("encode %d: %v", s, err)
		}
		got, _, err := bacnet.ParseApplicationValue(enc, limits)
		if err != nil || got.Signed != s {
			t.Fatalf("signed %d got %#v err=%v", s, got, err)
		}
	}
	// Boolean LVT-encoded true/false (no value octet).
	for _, raw := range [][]byte{{0x11}, {0x10}} {
		got, n, err := bacnet.ParseApplicationValue(raw, limits)
		if err != nil || n != 1 || got.Kind != bacnet.ValueBoolean {
			t.Fatalf("bool LVT %x -> %#v n=%d err=%v", raw, got, n, err)
		}
	}
}

func TestExtendedLengthTruncation(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	// lvt=5, ext length truncated after 254 marker
	if _, _, err := bacnet.ParseTag([]byte{0x65, 0xFE, 0x01}, limits); err == nil {
		t.Fatal("expected truncated 16-bit length")
	}
	if _, _, err := bacnet.ParseTag([]byte{0x65, 0xFF, 0x00, 0x00}, limits); err == nil {
		t.Fatal("expected truncated 32-bit length")
	}
}

func TestParseTagExtendedAndLengthErrors(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	cases := []struct {
		name string
		raw  []byte
	}{
		{"extended tag truncated", []byte{0xF8}},
		{"invalid LVT", []byte{0x26, 0x01}},
		{"decodeUnsigned LVT0", []byte{0x21}},
		{"decodeSigned LVT0", []byte{0x31}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := bacnet.ParseTag(tc.raw, limits); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestContextValueLengthLimit(t *testing.T) {
	tiny := bacnet.DecodeLimits{MaxOctetStringLength: 2}.Normalize()
	if _, _, err := bacnet.ParseTag([]byte{0x1B, 0x01, 0x02, 0x03}, tiny); !errors.Is(err, bacnet.ErrLimitExceeded) {
		t.Fatalf("context length limit: %v", err)
	}
}

func TestApplicationValueLimitsAndUnknownTag(t *testing.T) {
	limits := bacnet.DecodeLimits{
		MaxBitStringBits:     8,
		MaxOctetStringLength: 65535,
	}.Normalize()

	bitEnc, err := bacnet.AppendApplicationValue(nil, bacnet.ApplicationValue{
		Kind: bacnet.ValueBitString,
		BitString: bacnet.BitString{
			UnusedBits: 0,
			Bytes:      []byte{0x00, 0x00},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := bacnet.ParseApplicationValue(bitEnc, limits); !errors.Is(err, bacnet.ErrLimitExceeded) {
		t.Fatalf("bit string limit: %v", err)
	}

	enumEnc := []byte{0x95, 0x05, 0x01, 0x00, 0x00, 0x00, 0x00}
	if _, _, err := bacnet.ParseApplicationValue(enumEnc, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("enumerated overflow: %v", err)
	}

	unknown := []byte{0xE1, 0xAA}
	got, _, err := bacnet.ParseApplicationValue(unknown, limits)
	if err != nil || got.Kind != bacnet.ValueOctetString || got.OctetString[0] != 0xAA {
		t.Fatalf("unknown app tag: %#v err=%v", got, err)
	}
}

func TestAppendTagOpeningClosingAndContextBoolBad(t *testing.T) {
	open, err := bacnet.AppendTag(nil, bacnet.Element{Context: true, TagNumber: 3, Opening: true})
	if err != nil || len(open) == 0 {
		t.Fatalf("opening: %v %x", err, open)
	}
	closeTag, err := bacnet.AppendTag(nil, bacnet.Element{Context: true, TagNumber: 3, Closing: true})
	if err != nil || len(closeTag) == 0 {
		t.Fatalf("closing: %v %x", err, closeTag)
	}

	limits := bacnet.DefaultDecodeLimits()
	badBool := []byte{0x29, 0x02}
	el, _, err := bacnet.ParseTag(badBool, limits)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bacnet.ContextBool(el); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("context bool bad value: %v", err)
	}
}

func TestAppendApplicationValueInvalidObjectIDAndConstructedChild(t *testing.T) {
	badOID := bacnet.ObjectIdentifier{Type: bacnet.ObjectType(0x400), Instance: 1}
	if _, err := bacnet.AppendApplicationValue(nil, bacnet.ObjectIDValue(badOID)); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("invalid object id: %v", err)
	}
	constructed := bacnet.ApplicationValue{
		Kind: bacnet.ValueConstructed,
		Elements: []bacnet.Element{
			{Value: bacnet.ApplicationValue{Kind: bacnet.ValueKind(200)}},
		},
	}
	if _, err := bacnet.AppendApplicationValue(nil, constructed); !errors.Is(err, bacnet.ErrUnsupported) {
		t.Fatalf("constructed child kind 200: %v", err)
	}
}

func TestLargeOctetString32BitLengthEncode(t *testing.T) {
	limits := bacnet.DecodeLimits{MaxOctetStringLength: 100000}.Normalize()
	long := bytes.Repeat([]byte{0xCD}, 70000)
	v := bacnet.ApplicationValue{Kind: bacnet.ValueOctetString, OctetString: long}
	enc, err := bacnet.AppendApplicationValue(nil, v)
	if err != nil {
		t.Fatal(err)
	}
	got, n, err := bacnet.ParseApplicationValue(enc, limits)
	if err != nil || n != len(enc) || len(got.OctetString) != len(long) {
		t.Fatalf("32-bit length roundtrip: err=%v n=%d len=%d", err, n, len(got.OctetString))
	}
}
