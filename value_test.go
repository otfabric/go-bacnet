// SPDX-License-Identifier: MIT

package bacnet_test

import (
	"errors"
	"math"
	"testing"

	"github.com/otfabric/go-bacnet"
)

func TestValueConstructorsAndAsHelpers(t *testing.T) {
	if bacnet.NullValue().TagNumber() != bacnet.TagNull {
		t.Fatal("null tag")
	}
	b, err := bacnet.AsBool(bacnet.BoolValue(true))
	if err != nil || !b {
		t.Fatalf("bool %v %v", b, err)
	}
	u, err := bacnet.AsUnsigned(bacnet.UnsignedValue(9))
	if err != nil || u != 9 {
		t.Fatalf("unsigned %v %v", u, err)
	}
	u, err = bacnet.AsUnsigned(bacnet.EnumValue(3))
	if err != nil || u != 3 {
		t.Fatalf("enum as unsigned %v %v", u, err)
	}
	e, err := bacnet.AsEnumerated(bacnet.EnumValue(4))
	if err != nil || e != 4 {
		t.Fatalf("enumerated %v %v", e, err)
	}
	f, err := bacnet.AsReal(bacnet.RealValue(1.5))
	if err != nil || f != 1.5 {
		t.Fatalf("real %v %v", f, err)
	}
	f, err = bacnet.AsReal(bacnet.DoubleValue(2.25))
	if err != nil || f != 2.25 {
		t.Fatalf("double as real %v %v", f, err)
	}
	if _, err := bacnet.AsReal(bacnet.DoubleValue(math.MaxFloat64)); !errors.Is(err, bacnet.ErrUnsupported) {
		t.Fatalf("double overflow err=%v", err)
	}
	oid := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
	got, err := bacnet.AsObjectID(bacnet.ObjectIDValue(oid))
	if err != nil || got != oid {
		t.Fatalf("object id %#v %v", got, err)
	}
	if bacnet.SignedValue(-3).TagNumber() != bacnet.TagSigned {
		t.Fatal("signed tag")
	}
	if bacnet.DoubleValue(1).TagNumber() != bacnet.TagDouble {
		t.Fatal("double tag")
	}
}

func TestTagNumberAllKinds(t *testing.T) {
	cases := []struct {
		val  bacnet.ApplicationValue
		want bacnet.ApplicationTag
	}{
		{bacnet.BoolValue(true), bacnet.TagBoolean},
		{bacnet.UnsignedValue(1), bacnet.TagUnsigned},
		{bacnet.RealValue(1.0), bacnet.TagReal},
		{bacnet.ApplicationValue{Kind: bacnet.ValueOctetString, OctetString: []byte{1}}, bacnet.TagOctetString},
		{bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: "x"}}, bacnet.TagCharacterString},
		{bacnet.ApplicationValue{Kind: bacnet.ValueBitString, BitString: bacnet.BitString{Bytes: []byte{0}}}, bacnet.TagBitString},
		{bacnet.EnumValue(1), bacnet.TagEnumerated},
		{bacnet.ApplicationValue{Kind: bacnet.ValueDate}, bacnet.TagDate},
		{bacnet.ApplicationValue{Kind: bacnet.ValueTime}, bacnet.TagTime},
		{bacnet.ObjectIDValue(bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}), bacnet.TagObjectIdentifier},
		{bacnet.ApplicationValue{Kind: bacnet.ValueKind(200)}, bacnet.TagNull},
	}
	for _, tc := range cases {
		if got := tc.val.TagNumber(); got != tc.want {
			t.Fatalf("kind %d tag got %d want %d", tc.val.Kind, got, tc.want)
		}
	}
}

func TestAsHelpersWrongKind(t *testing.T) {
	v := bacnet.NullValue()
	if _, err := bacnet.AsBool(v); !errors.Is(err, bacnet.ErrUnsupported) {
		t.Fatal(err)
	}
	if _, err := bacnet.AsUnsigned(v); !errors.Is(err, bacnet.ErrUnsupported) {
		t.Fatal(err)
	}
	if _, err := bacnet.AsEnumerated(v); !errors.Is(err, bacnet.ErrUnsupported) {
		t.Fatal(err)
	}
	if _, err := bacnet.AsReal(v); !errors.Is(err, bacnet.ErrUnsupported) {
		t.Fatal(err)
	}
	if _, err := bacnet.AsObjectID(v); !errors.Is(err, bacnet.ErrUnsupported) {
		t.Fatal(err)
	}
}

func TestApplicationValueClone(t *testing.T) {
	src := bacnet.ApplicationValue{
		Kind:        bacnet.ValueOctetString,
		OctetString: []byte{1, 2},
		BitString:   bacnet.BitString{UnusedBits: 1, Bytes: []byte{0x80}},
		Elements: []bacnet.Element{{
			Context: true, TagNumber: 1,
			Value: bacnet.UnsignedValue(7),
		}},
	}
	cl := src.Clone()
	src.OctetString[0] = 9
	src.BitString.Bytes[0] = 0
	src.Elements[0].Value.Unsigned = 0
	if cl.OctetString[0] != 1 || cl.BitString.Bytes[0] != 0x80 || cl.Elements[0].Value.Unsigned != 7 {
		t.Fatalf("clone not independent %#v", cl)
	}
}
