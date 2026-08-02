// SPDX-License-Identifier: MIT

package service

import (
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
)

func TestReadRangeEncodeInvalidObject(t *testing.T) {
	_, err := EncodeReadRange(ReadRangeRequest{
		Object:   bacnet.ObjectIdentifier{Type: 0xFFFF, Instance: 1},
		Property: bacnet.PropertyReference{Identifier: 131},
		By:       ReadRangeAll,
	})
	if !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("got %v", err)
	}
	_, err = EncodeReadRangeACK(ReadRangeACK{
		Object:      bacnet.ObjectIdentifier{Type: 0xFFFF, Instance: 1},
		Property:    bacnet.PropertyReference{Identifier: 131},
		ResultFlags: EncodeResultFlags(true, true, false),
		ItemCount:   0,
	})
	if !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("ack: %v", err)
	}
	if _, err := EncodeReadRange(ReadRangeRequest{
		Object:   bacnet.ObjectIdentifier{Type: 20, Instance: 1},
		Property: bacnet.PropertyReference{Identifier: 131},
		By:       ReadRangeBy(99),
		Count:    1,
	}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("unknown form: %v", err)
	}
}

func TestReadRangeDecodeMissingAndUnexpected(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	obj := bacnet.ObjectIdentifier{Type: 20, Instance: 1}
	// missing property
	raw, err := bacnet.AppendContextObjectID(nil, 0, obj)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeReadRange(raw, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("missing property: %v", err)
	}
	// unexpected tag
	if _, err := DecodeReadRange([]byte{0x49, 0x01}, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("unexpected: %v", err)
	}
	// duplicate propertyIdentifier
	raw, err = bacnet.AppendContextObjectID(nil, 0, obj)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextUnsigned(raw, 1, 131)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextUnsigned(raw, 1, 132)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeReadRange(raw, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("dup property: %v", err)
	}
	// duplicate bySequence range after byPosition
	raw, err = EncodeReadRange(ReadRangeRequest{
		Object: obj, Property: bacnet.PropertyReference{Identifier: 131},
		By: ReadRangeByPosition, ReferenceIndex: 1, Count: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextTagged(raw, 6, []bacnet.Element{
		{Value: bacnet.UnsignedValue(1)}, {Value: bacnet.SignedValue(1)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeReadRange(raw, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("dup range: %v", err)
	}
	// byTime incomplete (date only) and unsigned count
	raw, err = bacnet.AppendContextObjectID(nil, 0, obj)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextUnsigned(raw, 1, 131)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextTagged(raw, 7, []bacnet.Element{
		{Value: bacnet.ApplicationValue{Kind: bacnet.ValueDate, Date: bacnet.Date{Year: 126, Month: 1, Day: 1, Weekday: 1}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeReadRange(raw, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("byTime short: %v", err)
	}
}

func TestReadRangeDecodeDuplicatesAndTrailing(t *testing.T) {
	base, err := EncodeReadRange(ReadRangeRequest{
		Object:   bacnet.ObjectIdentifier{Type: 20, Instance: 1},
		Property: bacnet.PropertyReference{Identifier: 131},
		By:       ReadRangeByPosition, ReferenceIndex: 1, Count: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	dup, err := bacnet.AppendContextObjectID(base, 0, bacnet.ObjectIdentifier{Type: 20, Instance: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeReadRange(dup, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("dup object: %v", err)
	}
	// Unexpected application tag after a valid prefix.
	if _, err := DecodeReadRange(append(append([]byte(nil), base...), 0x00), bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("extra null: %v", err)
	}

	ack, err := EncodeReadRangeACK(ReadRangeACK{
		Object: bacnet.ObjectIdentifier{Type: 20, Instance: 1}, Property: bacnet.PropertyReference{Identifier: 131},
		ResultFlags: EncodeResultFlags(true, true, false), ItemCount: 1,
		ItemData: []bacnet.ApplicationValue{bacnet.UnsignedValue(1)},
	})
	if err != nil {
		t.Fatal(err)
	}
	dupACK, err := bacnet.AppendContextObjectID(ack, 0, bacnet.ObjectIdentifier{Type: 20, Instance: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeReadRangeACK(dupACK, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("dup ack object: %v", err)
	}
	// Duplicate resultFlags.
	flags, err := bacnet.AppendContextBitString(ack, 3, EncodeResultFlags(false, false, true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeReadRangeACK(flags, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("dup flags: %v", err)
	}
}

func TestReadRangeDecodeIndexCountOverflows(t *testing.T) {
	obj, err := bacnet.AppendContextObjectID(nil, 0, bacnet.ObjectIdentifier{Type: 20, Instance: 1})
	if err != nil {
		t.Fatal(err)
	}
	obj, err = bacnet.AppendContextUnsigned(obj, 1, 131)
	if err != nil {
		t.Fatal(err)
	}
	cases := [][]bacnet.Element{
		{{Value: bacnet.UnsignedValue(1)}},                                          // missing count
		{{Value: bacnet.UnsignedValue(1)}, {Value: bacnet.UnsignedValue(2)}},        // count not signed
		{{Value: bacnet.UnsignedValue(^uint64(0))}, {Value: bacnet.SignedValue(1)}}, // index overflow
	}
	for i, body := range cases {
		raw, err := bacnet.AppendContextTagged(obj, 3, body)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeReadRange(raw, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
			t.Fatalf("case %d: %v", i, err)
		}
	}
	timeCases := [][]bacnet.Element{
		{{Value: bacnet.ApplicationValue{Kind: bacnet.ValueDate}}, {Value: bacnet.SignedValue(1)}},
		{
			{Value: bacnet.ApplicationValue{Kind: bacnet.ValueDate}},
			{Value: bacnet.ApplicationValue{Kind: bacnet.ValueTime}},
			{Value: bacnet.UnsignedValue(1)},
		},
		{
			{Value: bacnet.ApplicationValue{Kind: bacnet.ValueDate}},
			{Value: bacnet.ApplicationValue{Kind: bacnet.ValueTime}},
			{Value: bacnet.SignedValue(0)},
		},
		// referenceTime date/time must be application Date/Time, not other kinds.
		{
			{Value: bacnet.UnsignedValue(1)},
			{Value: bacnet.ApplicationValue{Kind: bacnet.ValueTime}},
			{Value: bacnet.SignedValue(1)},
		},
		{
			{Value: bacnet.ApplicationValue{Kind: bacnet.ValueDate, Date: bacnet.Date{Year: 126, Month: 1, Day: 1, Weekday: 1}}},
			{Value: bacnet.UnsignedValue(1)},
			{Value: bacnet.SignedValue(1)},
		},
	}
	for i, body := range timeCases {
		raw, err := bacnet.AppendContextTagged(obj, 7, body)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeReadRange(raw, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
			t.Fatalf("time case %d: %v", i, err)
		}
	}
}

func TestWhoHasIHaveEncodeDecodeErrors(t *testing.T) {
	bad := bacnet.ObjectIdentifier{Type: 0x400, Instance: 0}
	if _, err := EncodeWhoHas(WhoHas{Object: &bad}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("who-has object: %v", err)
	}
	_, err := EncodeIHave(IHave{
		Device: bacnet.ObjectIdentifier{Type: 0xFFFF, Instance: 1},
		Object: bacnet.ObjectIdentifier{Type: 1, Instance: 1},
		Name:   bacnet.CharacterString{Value: "x"},
	})
	if !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("bad device: %v", err)
	}
	if _, err := EncodeIHave(IHave{
		Device: bad,
		Object: bacnet.ObjectIdentifier{Type: 2, Instance: 1},
		Name:   bacnet.CharacterString{Value: "x"},
	}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("i-have device: %v", err)
	}
	_, err = EncodeWhoHasAPDU(WhoHas{})
	if !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("who apdu: %v", err)
	}
	_, err = EncodeIHaveAPDU(IHave{
		Device: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		Object: bacnet.ObjectIdentifier{Type: 0xFFFF, Instance: 1},
		Name:   bacnet.CharacterString{Value: "x"},
	})
	if !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("ihave apdu: %v", err)
	}

	good, err := EncodeWhoHas(WhoHas{Object: &bacnet.ObjectIdentifier{Type: 1, Instance: 1}})
	if err != nil {
		t.Fatal(err)
	}
	// duplicate object choice
	dup, err := bacnet.AppendContextCharacterString(good, 3, bacnet.CharacterString{Value: "n"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeWhoHas(dup, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("dup choice: %v", err)
	}
	// duplicate low limit
	lowDup, err := bacnet.AppendContextUnsigned(nil, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	lowDup, err = bacnet.AppendContextUnsigned(lowDup, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeWhoHas(lowDup, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("dup low: %v", err)
	}

	ih, err := EncodeIHave(IHave{
		Device: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		Object: bacnet.ObjectIdentifier{Type: 2, Instance: 3},
		Name:   bacnet.CharacterString{Value: "n"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeIHave(append(append([]byte(nil), ih...), 0x00), bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrTrailingData) {
		t.Fatalf("trailing i-have: %v", err)
	}
}

func TestReadRangeMoreEncodeBranches(t *testing.T) {
	for _, by := range []ReadRangeBy{ReadRangeBySequenceNumber, ReadRangeByTime} {
		_, err := EncodeReadRange(ReadRangeRequest{
			Object:   bacnet.ObjectIdentifier{Type: 20, Instance: 1},
			Property: bacnet.PropertyReference{Identifier: 131},
			By:       by,
			Count:    0,
		})
		if !errors.Is(err, bacnet.ErrMalformed) {
			t.Fatalf("by=%d: %v", by, err)
		}
	}
	if bitStringBit(bacnet.BitString{Bytes: []byte{0x80}}, 20) {
		t.Fatal("out-of-range bit")
	}
	if bitStringBit(bacnet.BitString{Bytes: []byte{0x80}}, -1) {
		t.Fatal("negative bit")
	}
	idx := uint32(2)
	seq := uint32(9)
	enc, err := EncodeReadRangeACK(ReadRangeACK{
		Object:        bacnet.ObjectIdentifier{Type: 20, Instance: 1},
		Property:      bacnet.PropertyReference{Identifier: 131, ArrayIndex: &idx},
		ResultFlags:   EncodeResultFlags(true, true, false),
		ItemCount:     0,
		FirstSequence: &seq,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeReadRangeACK(enc, bacnet.DefaultDecodeLimits())
	if err != nil || got.FirstSequence == nil || *got.FirstSequence != 9 || got.Property.ArrayIndex == nil {
		t.Fatalf("got %+v err=%v", got, err)
	}
	// low>high Who-Has on decode
	raw, err := bacnet.AppendContextUnsigned(nil, 0, 9)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextUnsigned(raw, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextCharacterString(raw, 3, bacnet.CharacterString{Value: "n"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeWhoHas(raw, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("low>high decode: %v", err)
	}
	widx := uint32(4)
	_, err = EncodeWritePropertyMultiple([]WriteAccessSpecification{{
		Object: bacnet.ObjectIdentifier{Type: 1, Instance: 1},
		Properties: []WritePropertyValue{{
			Property: bacnet.PropertyReference{Identifier: 85, ArrayIndex: &widx},
			Value:    bacnet.RealValue(1),
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	prio := uint8(3)
	_, err = EncodeWritePropertyMultiple([]WriteAccessSpecification{{
		Object: bacnet.ObjectIdentifier{Type: 1, Instance: 1},
		Properties: []WritePropertyValue{{
			Property: bacnet.PropertyReference{Identifier: 85, ArrayIndex: &widx},
			Value:    bacnet.RealValue(1),
			Priority: &prio,
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	aidx := uint32(5)
	werr, err := EncodeWritePropertyMultipleError(WritePropertyMultipleError{
		Class: 1, Code: 2,
		FirstFailed:   bacnet.ObjectIdentifier{Type: 1, Instance: 1},
		FirstProperty: bacnet.PropertyReference{Identifier: 85, ArrayIndex: &aidx},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeWritePropertyMultipleError(werr, bacnet.DefaultDecodeLimits()); err != nil {
		t.Fatal(err)
	}

	// itemCount overflow via oversized context unsigned (5+ octets).
	overflow := []byte{
		0x0C, 0x00, 0x50, 0x00, 0x01, // context object id type 20 instance 1 roughly — rebuild properly below
	}
	_ = overflow
	var big []byte
	big, err = bacnet.AppendContextObjectID(nil, 0, bacnet.ObjectIdentifier{Type: 20, Instance: 1})
	if err != nil {
		t.Fatal(err)
	}
	big, err = bacnet.AppendContextUnsigned(big, 1, 131)
	if err != nil {
		t.Fatal(err)
	}
	big, err = bacnet.AppendContextBitString(big, 3, EncodeResultFlags(true, true, false))
	if err != nil {
		t.Fatal(err)
	}
	big, err = bacnet.AppendContextUnsigned(big, 4, uint64(0x1FFFFFFFF))
	if err != nil {
		t.Fatal(err)
	}
	big, err = bacnet.AppendContextTagged(big, 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeReadRangeACK(big, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("itemCount overflow: %v", err)
	}

	// Context-primitive item in itemData (tag 2 inside [5]).
	var prim []byte
	prim, err = bacnet.AppendContextObjectID(nil, 0, bacnet.ObjectIdentifier{Type: 20, Instance: 1})
	if err != nil {
		t.Fatal(err)
	}
	prim, err = bacnet.AppendContextUnsigned(prim, 1, uint64(bacnet.PropertyPresentValue))
	if err != nil {
		t.Fatal(err)
	}
	prim, err = bacnet.AppendContextBitString(prim, 3, EncodeResultFlags(true, true, false))
	if err != nil {
		t.Fatal(err)
	}
	prim, err = bacnet.AppendContextUnsigned(prim, 4, 1)
	if err != nil {
		t.Fatal(err)
	}
	prim = append(prim, 0x5E) // opening context 5
	prim, err = bacnet.AppendContextUnsigned(prim, 2, 7)
	if err != nil {
		t.Fatal(err)
	}
	prim = append(prim, 0x5F) // closing context 5
	gotPrim, err := DecodeReadRangeACK(prim, bacnet.DefaultDecodeLimits())
	if err != nil || len(gotPrim.ItemData) != 1 {
		t.Fatalf("primitive item: %+v err=%v", gotPrim, err)
	}
}

func TestWritePropertyMultipleMoreMalformed(t *testing.T) {
	_, err := EncodeWritePropertyMultiple([]WriteAccessSpecification{{
		Object: bacnet.ObjectIdentifier{Type: 0xFFFF, Instance: 1},
		Properties: []WritePropertyValue{{
			Property: bacnet.PropertyReference{Identifier: 85},
			Value:    bacnet.RealValue(1),
		}},
	}})
	if !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("bad object: %v", err)
	}
	enc, err := EncodeWritePropertyMultiple([]WriteAccessSpecification{{
		Object: bacnet.ObjectIdentifier{Type: 1, Instance: 1},
		Properties: []WritePropertyValue{{
			Property: bacnet.PropertyReference{Identifier: 85},
			Value:    bacnet.RealValue(1),
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	// listOfProperties without preceding object
	onlyList := []byte{0x1E, 0x1F}
	if _, err := DecodeWritePropertyMultiple(onlyList, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("orphan list: %v", err)
	}
	_ = enc
}
