// SPDX-License-Identifier: MIT

package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/otfabric/go-bacnet"
)

func TestReadRangeByPositionRoundTrip(t *testing.T) {
	req := ReadRangeRequest{
		Object:         bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeTrendLog, Instance: 1},
		Property:       bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer},
		By:             ReadRangeByPosition,
		ReferenceIndex: 10,
		Count:          5,
	}
	enc, err := EncodeReadRange(req)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeReadRange(enc, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if got.By != ReadRangeByPosition || got.ReferenceIndex != 10 || got.Count != 5 {
		t.Fatalf("got %+v", got)
	}
}

func TestReadRangeByTimeAndSequenceRoundTrip(t *testing.T) {
	req := ReadRangeRequest{
		Object:         bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeTrendLog, Instance: 2},
		Property:       bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer},
		By:             ReadRangeBySequenceNumber,
		ReferenceIndex: 100,
		Count:          -3,
	}
	enc, err := EncodeReadRange(req)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeReadRange(enc, bacnet.DefaultDecodeLimits())
	if err != nil || got.Count != -3 || got.By != ReadRangeBySequenceNumber {
		t.Fatalf("seq: %+v err=%v", got, err)
	}

	req = ReadRangeRequest{
		Object:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeTrendLog, Instance: 2},
		Property: bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer},
		By:       ReadRangeByTime,
		ReferenceTime: DateTime{
			Date: bacnet.Date{Year: 126, Month: 8, Day: 1, Weekday: 6},
			Time: bacnet.Time{Hour: 12, Minute: 0, Second: 0, Hundredths: 0},
		},
		Count: 2,
	}
	enc, err = EncodeReadRange(req)
	if err != nil {
		t.Fatal(err)
	}
	got, err = DecodeReadRange(enc, bacnet.DefaultDecodeLimits())
	if err != nil || got.By != ReadRangeByTime || got.ReferenceTime.Date.Year != 126 {
		t.Fatalf("time: %+v err=%v", got, err)
	}
}

func TestReadRangeACKRoundTrip(t *testing.T) {
	seq := uint32(42)
	ack := ReadRangeACK{
		Object:      bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeTrendLog, Instance: 1},
		Property:    bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
		ResultFlags: EncodeResultFlags(true, false, true),
		ItemCount:   2,
		ItemData: []bacnet.ApplicationValue{
			bacnet.RealValue(1.5),
			bacnet.RealValue(2.5),
		},
		FirstSequence: &seq,
	}
	enc, err := EncodeReadRangeACK(ack)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeReadRangeACK(enc, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if !got.FirstItem() || got.LastItem() || !got.MoreItems() {
		t.Fatalf("flags first=%v last=%v more=%v", got.FirstItem(), got.LastItem(), got.MoreItems())
	}
	if got.ItemCount != 2 || len(got.ItemData) != 2 || got.FirstSequence == nil || *got.FirstSequence != 42 {
		t.Fatalf("got %+v", got)
	}
}

func TestReadRangeRejectsZeroCount(t *testing.T) {
	_, err := EncodeReadRange(ReadRangeRequest{
		Object:   bacnet.ObjectIdentifier{Type: 20, Instance: 1},
		Property: bacnet.PropertyReference{Identifier: 131},
		By:       ReadRangeByPosition,
		Count:    0,
	})
	if !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("got %v", err)
	}
	_, err = EncodeReadRange(ReadRangeRequest{
		Object:   bacnet.ObjectIdentifier{Type: 20, Instance: 1},
		Property: bacnet.PropertyReference{Identifier: 131},
		By:       ReadRangeBy(99),
		Count:    1,
	})
	if !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("unknown by: %v", err)
	}
}

func TestReadRangeAllOmitsRange(t *testing.T) {
	idx := uint32(3)
	req := ReadRangeRequest{
		Object:   bacnet.ObjectIdentifier{Type: 20, Instance: 1},
		Property: bacnet.PropertyReference{Identifier: 131, ArrayIndex: &idx},
		By:       ReadRangeAll,
	}
	enc, err := EncodeReadRange(req)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeReadRange(enc, bacnet.DefaultDecodeLimits())
	if err != nil || got.By != ReadRangeAll || got.Property.ArrayIndex == nil || *got.Property.ArrayIndex != 3 {
		t.Fatalf("got %+v err=%v", got, err)
	}
}

func TestReadRangeMalformed(t *testing.T) {
	if _, err := DecodeReadRange([]byte{0xFF}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected decode error")
	}
	if _, err := DecodeReadRangeACK([]byte{0xFF}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected ACK decode error")
	}
	idx := uint32(1)
	req := ReadRangeRequest{
		Object:   bacnet.ObjectIdentifier{Type: 20, Instance: 1},
		Property: bacnet.PropertyReference{Identifier: 131, ArrayIndex: &idx},
		By:       ReadRangeByPosition,
		Count:    1,
	}
	enc, err := EncodeReadRange(req)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeReadRange(enc, bacnet.DefaultDecodeLimits())
	if err != nil || got.Property.ArrayIndex == nil || *got.Property.ArrayIndex != 1 {
		t.Fatalf("array index: %+v err=%v", got, err)
	}
	ack := ReadRangeACK{
		Object:      req.Object,
		Property:    bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
		ResultFlags: EncodeResultFlags(false, true, false),
		ItemCount:   1,
		ItemData:    []bacnet.ApplicationValue{bacnet.UnsignedValue(9)},
	}
	aenc, err := EncodeReadRangeACK(ack)
	if err != nil {
		t.Fatal(err)
	}
	agot, err := DecodeReadRangeACK(aenc, bacnet.DefaultDecodeLimits())
	if err != nil || !agot.LastItem() || agot.ItemData[0].Unsigned != 9 {
		t.Fatalf("ack %+v err=%v", agot, err)
	}
	// Empty itemData with non-zero count is still malformed.
	mismatch := ReadRangeACK{
		Object: req.Object, Property: req.Property,
		ResultFlags: EncodeResultFlags(true, true, false),
		ItemCount:   2,
		ItemData:    nil,
	}
	menc, err := EncodeReadRangeACK(mismatch)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeReadRangeACK(menc, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("empty data with count: %v", err)
	}
	// Non-Log_Buffer properties may report ItemCount that does not match the
	// flat tag stream; ItemCount from the wire is trusted.
	complexMismatch := ReadRangeACK{
		Object:      req.Object,
		Property:    bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
		ResultFlags: EncodeResultFlags(true, true, false),
		ItemCount:   2,
		ItemData:    []bacnet.ApplicationValue{bacnet.UnsignedValue(1)},
	}
	cmenc, err := EncodeReadRangeACK(complexMismatch)
	if err != nil {
		t.Fatal(err)
	}
	gotComplex, err := DecodeReadRangeACK(cmenc, bacnet.DefaultDecodeLimits())
	if err != nil || gotComplex.ItemCount != 2 || len(gotComplex.ItemData) != 1 {
		t.Fatalf("complex itemCount trust: %+v err=%v", gotComplex, err)
	}
}

func TestReadRangeDecodeRangeErrors(t *testing.T) {
	obj, err := bacnet.AppendContextObjectID(nil, 0, bacnet.ObjectIdentifier{Type: 20, Instance: 1})
	if err != nil {
		t.Fatal(err)
	}
	obj, err = bacnet.AppendContextUnsigned(obj, 1, 131)
	if err != nil {
		t.Fatal(err)
	}
	// byPosition with signed reference (invalid).
	badPos, err := bacnet.AppendContextTagged(obj, 3, []bacnet.Element{
		{Value: bacnet.SignedValue(1)},
		{Value: bacnet.SignedValue(2)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeReadRange(badPos, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("bad position: %v", err)
	}
	// byTime with missing count.
	badTime, err := bacnet.AppendContextTagged(obj, 7, []bacnet.Element{
		{Value: bacnet.ApplicationValue{Kind: bacnet.ValueDate}},
		{Value: bacnet.ApplicationValue{Kind: bacnet.ValueTime}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeReadRange(badTime, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("bad time: %v", err)
	}
	// bySequence with zero count.
	badSeq, err := bacnet.AppendContextTagged(obj, 6, []bacnet.Element{
		{Value: bacnet.UnsignedValue(1)},
		{Value: bacnet.SignedValue(0)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeReadRange(badSeq, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("zero count: %v", err)
	}
}

func TestReadRangeACKConstructedItems(t *testing.T) {
	// Log-style items are typically one context-constructed value each.
	var enc []byte
	var err error
	enc, err = bacnet.AppendContextObjectID(enc, 0, bacnet.ObjectIdentifier{Type: 20, Instance: 1})
	if err != nil {
		t.Fatal(err)
	}
	enc, err = bacnet.AppendContextUnsigned(enc, 1, uint64(bacnet.PropertyPresentValue))
	if err != nil {
		t.Fatal(err)
	}
	enc, err = bacnet.AppendContextBitString(enc, 3, EncodeResultFlags(true, true, false))
	if err != nil {
		t.Fatal(err)
	}
	enc, err = bacnet.AppendContextUnsigned(enc, 4, 1)
	if err != nil {
		t.Fatal(err)
	}
	enc, err = bacnet.AppendContextTagged(enc, 5, []bacnet.Element{{
		Context:   true,
		TagNumber: 0,
		Value: bacnet.ApplicationValue{
			Kind:     bacnet.ValueConstructed,
			Elements: []bacnet.Element{{Value: bacnet.RealValue(1.0)}},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeReadRangeACK(enc, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if got.ItemData[0].Kind != bacnet.ValueConstructed || len(got.ItemData[0].Elements) != 1 {
		t.Fatalf("constructed item %+v", got.ItemData[0])
	}
	if bitStringBit(bacnet.BitString{}, 0) {
		t.Fatal("empty bitstring should be false")
	}
}
func TestReadRangeStrictDuplicates(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()

	rrObj, err := bacnet.AppendContextObjectID(nil, 0, bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeTrendLog, Instance: 1})
	if err != nil {
		t.Fatal(err)
	}
	rrProp, err := bacnet.AppendContextUnsigned(nil, 1, uint64(bacnet.PropertyLogBuffer))
	if err != nil {
		t.Fatal(err)
	}
	rrI1, err := bacnet.AppendContextUnsigned(nil, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	rrI2, err := bacnet.AppendContextUnsigned(nil, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	rrRange, err := bacnet.AppendContextTagged(nil, 3, []bacnet.Element{
		{Value: bacnet.UnsignedValue(1)}, {Value: bacnet.SignedValue(2)},
	})
	if err != nil {
		t.Fatal(err)
	}
	dupIdx := append(append([]byte(nil), rrObj...), rrProp...)
	dupIdx = append(dupIdx, rrI1...)
	dupIdx = append(dupIdx, rrI2...)
	dupIdx = append(dupIdx, rrRange...)
	_, err = DecodeReadRange(dupIdx, limits)
	if !errors.Is(err, bacnet.ErrMalformed) || !strings.Contains(err.Error(), "duplicate arrayIndex") {
		t.Fatalf("duplicate arrayIndex: %v", err)
	}

	ack, err := EncodeReadRangeACK(ReadRangeACK{
		Object:      bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeTrendLog, Instance: 1},
		Property:    bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
		ResultFlags: EncodeResultFlags(true, true, false),
		ItemCount:   1,
		ItemData:    []bacnet.ApplicationValue{bacnet.RealValue(1)},
	})
	if err != nil {
		t.Fatal(err)
	}
	seq, err := bacnet.AppendContextUnsigned(nil, 6, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeReadRangeACK(append(append([]byte(nil), ack...), seq...), limits); err != nil {
		t.Fatalf("single firstSequence: %v", err)
	}
	if _, err := DecodeReadRangeACK(append(append(append([]byte(nil), ack...), seq...), seq...), limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("duplicate firstSequence: %v", err)
	}

	badFlags := []byte{
		0x0C, 0x05, 0x00, 0x00, 0x01,
		0x19, 0x55,
		0x3A, 0x03, 0x40, // unusedBits=3 — not BACnetResultFlags SIZE(3)
		0x41, 0x01,
		0x5E, 0x44, 0x3f, 0x80, 0x00, 0x00, 0x5F,
	}
	if _, err := DecodeReadRangeACK(badFlags, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("invalid resultFlags: %v", err)
	}

	badLog, err := EncodeReadRangeACK(ReadRangeACK{
		Object:      bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeTrendLog, Instance: 1},
		Property:    bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer},
		ResultFlags: EncodeResultFlags(true, true, false),
		ItemCount:   1,
		ItemData:    []bacnet.ApplicationValue{bacnet.RealValue(1)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeReadRangeACK(badLog, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("malformed LogRecord split: %v", err)
	}
}
