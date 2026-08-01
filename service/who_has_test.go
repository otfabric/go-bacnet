// SPDX-License-Identifier: MIT

package service

import (
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
)

func TestWhoHasByNameRoundTrip(t *testing.T) {
	low, high := uint32(1), uint32(100)
	name := bacnet.CharacterString{Encoding: 0, Value: "AHU-1"}
	enc, err := EncodeWhoHas(WhoHas{LowLimit: &low, HighLimit: &high, Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeWhoHas(enc, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if got.Name == nil || got.Name.Value != "AHU-1" || got.Object != nil {
		t.Fatalf("got %+v", got)
	}
	if got.LowLimit == nil || *got.LowLimit != 1 || got.HighLimit == nil || *got.HighLimit != 100 {
		t.Fatalf("limits %+v", got)
	}
}

func TestWhoHasByObjectRoundTrip(t *testing.T) {
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 7}
	enc, err := EncodeWhoHas(WhoHas{Object: &obj})
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeWhoHas(enc, bacnet.DefaultDecodeLimits())
	if err != nil || got.Object == nil || *got.Object != obj || got.Name != nil {
		t.Fatalf("got %+v err=%v", got, err)
	}
}

func TestWhoHasRejectsInvalidChoice(t *testing.T) {
	if _, err := EncodeWhoHas(WhoHas{}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("empty: %v", err)
	}
	obj := bacnet.ObjectIdentifier{Type: 1, Instance: 1}
	name := bacnet.CharacterString{Value: "x"}
	if _, err := EncodeWhoHas(WhoHas{Object: &obj, Name: &name}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("both: %v", err)
	}
}

func TestIHaveRoundTrip(t *testing.T) {
	msg := IHave{
		Device: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 99},
		Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 3},
		Name:   bacnet.CharacterString{Encoding: 0, Value: "ZoneTemp"},
	}
	enc, err := EncodeIHave(msg)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeIHave(enc, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if got.Device != msg.Device || got.Object != msg.Object || got.Name.Value != "ZoneTemp" {
		t.Fatalf("got %+v", got)
	}
	apduBytes, err := EncodeIHaveAPDU(msg)
	if err != nil || len(apduBytes) < 3 {
		t.Fatalf("APDU: %v %v", apduBytes, err)
	}
	whoAPDU, err := EncodeWhoHasAPDU(WhoHas{Object: &msg.Object})
	if err != nil || len(whoAPDU) < 3 {
		t.Fatalf("WhoHas APDU: %v %v", whoAPDU, err)
	}
}

func TestWhoHasDecodeMalformed(t *testing.T) {
	if _, err := DecodeWhoHas([]byte{0xFF}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected error")
	}
	if _, err := DecodeIHave([]byte{0xC4}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected truncated I-Have error")
	}
	low := uint32(5)
	name := bacnet.CharacterString{Value: "x"}
	if _, err := EncodeWhoHas(WhoHas{LowLimit: &low, Name: &name}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("one limit: %v", err)
	}
	high := uint32(1)
	low2 := uint32(5)
	if _, err := EncodeWhoHas(WhoHas{LowLimit: &low2, HighLimit: &high, Name: &name}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("low>high: %v", err)
	}
	// Decode with only low limit.
	partial, err := bacnet.AppendContextUnsigned(nil, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeWhoHas(partial, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("partial limits: %v", err)
	}
	// Non-device in I-Have device field.
	raw, err := bacnet.AppendApplicationValue(nil, bacnet.ObjectIDValue(bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}))
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendApplicationValue(raw, bacnet.ObjectIDValue(bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 2}))
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendApplicationValue(raw, bacnet.ApplicationValue{
		Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: "x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeIHave(raw, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("non-device: %v", err)
	}
}

func TestWhoHasIHaveDecodeErrorPaths(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	name := bacnet.CharacterString{Value: "AV-1"}

	// ContextUnsigned fail / overflows on limits.
	if _, err := DecodeWhoHas([]byte{0x08}, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("low empty: %v", err)
	}
	ovLow, err := bacnet.AppendContextUnsigned(nil, 0, 1<<32)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeWhoHas(ovLow, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("low overflow: %v", err)
	}
	dupHigh, err := bacnet.AppendContextUnsigned(nil, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	dupHigh, err = bacnet.AppendContextUnsigned(dupHigh, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	dupHigh, err = bacnet.AppendContextUnsigned(dupHigh, 1, 3)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeWhoHas(dupHigh, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("dup high: %v", err)
	}
	badHigh, err := bacnet.AppendContextUnsigned(nil, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	badHigh = append(badHigh, 0x18) // context tag 1, length 0
	if _, err := DecodeWhoHas(badHigh, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("high empty: %v", err)
	}
	ovHigh, err := bacnet.AppendContextUnsigned(nil, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	ovHigh, err = bacnet.AppendContextUnsigned(ovHigh, 1, 1<<32)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeWhoHas(ovHigh, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("high overflow: %v", err)
	}

	// ObjectIdentifier / CharacterString decode failures and unexpected tags.
	badObj := []byte{0x2A, 0x00, 0x01} // truncated context object id tag 2
	if _, err := DecodeWhoHas(badObj, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("bad object: %v", err)
	}
	badName := []byte{0x3D, 0x01} // truncated context character string tag 3
	if _, err := DecodeWhoHas(badName, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("bad name: %v", err)
	}
	if _, err := DecodeWhoHas([]byte{0x49, 0x01}, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("unexpected tag: %v", err)
	}

	// name then object is also a duplicate choice.
	both, err := bacnet.AppendContextCharacterString(nil, 3, name)
	if err != nil {
		t.Fatal(err)
	}
	both, err = bacnet.AppendContextObjectID(both, 2, obj)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeWhoHas(both, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("name+object: %v", err)
	}

	// I-Have mid-stream parse / type failures.
	dev := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
	ih, err := EncodeIHave(IHave{Device: dev, Object: obj, Name: name})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeIHave(ih[:len(ih)-1], limits); err == nil {
		t.Fatal("expected truncated name")
	}
	// device application value that is not an object identifier
	notObj, err := bacnet.AppendApplicationValue(nil, bacnet.UnsignedValue(1))
	if err != nil {
		t.Fatal(err)
	}
	notObj, err = bacnet.AppendApplicationValue(notObj, bacnet.ObjectIDValue(obj))
	if err != nil {
		t.Fatal(err)
	}
	notObj, err = bacnet.AppendApplicationValue(notObj, bacnet.ApplicationValue{
		Kind: bacnet.ValueCharacterString, Character: name,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeIHave(notObj, limits); err == nil {
		t.Fatal("device not object")
	}
	// object field not object id
	notObj2, err := bacnet.AppendApplicationValue(nil, bacnet.ObjectIDValue(dev))
	if err != nil {
		t.Fatal(err)
	}
	notObj2, err = bacnet.AppendApplicationValue(notObj2, bacnet.UnsignedValue(2))
	if err != nil {
		t.Fatal(err)
	}
	notObj2, err = bacnet.AppendApplicationValue(notObj2, bacnet.ApplicationValue{
		Kind: bacnet.ValueCharacterString, Character: name,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeIHave(notObj2, limits); err == nil {
		t.Fatal("object not object")
	}
	// name not character string
	badCS, err := bacnet.AppendApplicationValue(nil, bacnet.ObjectIDValue(dev))
	if err != nil {
		t.Fatal(err)
	}
	badCS, err = bacnet.AppendApplicationValue(badCS, bacnet.ObjectIDValue(obj))
	if err != nil {
		t.Fatal(err)
	}
	badCS, err = bacnet.AppendApplicationValue(badCS, bacnet.UnsignedValue(3))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeIHave(badCS, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("name not string: %v", err)
	}
}
