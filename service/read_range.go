// SPDX-License-Identifier: MIT

package service

import (
	"fmt"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
)

// ReadRangeBy discriminates the optional ReadRange range CHOICE.
type ReadRangeBy uint8

const (
	ReadRangeAll ReadRangeBy = iota // omit range parameter
	ReadRangeByPosition
	ReadRangeBySequenceNumber
	ReadRangeByTime
)

// BACnetResultFlags bit positions (ASHRAE 135).
const (
	ResultFlagFirstItem = 0
	ResultFlagLastItem  = 1
	ResultFlagMoreItems = 2
)

// DateTime is a BACnetDateTime used by ReadRange byTime.
type DateTime struct {
	Date bacnet.Date
	Time bacnet.Time
}

// ReadRangeRequest is a ReadRange confirmed-request payload.
//
// Range may be omitted (By == ReadRangeAll). Count must be non-zero when a
// range form is selected (positive = forward, negative = backward).
type ReadRangeRequest struct {
	Object   bacnet.ObjectIdentifier
	Property bacnet.PropertyReference
	By       ReadRangeBy
	// ReferenceIndex is used by byPosition and bySequenceNumber.
	ReferenceIndex uint32
	// ReferenceTime is used by byTime.
	ReferenceTime DateTime
	Count         int32
}

// ReadRangeACK is a ReadRange ComplexACK payload.
//
// ItemData holds the flat application/context tag stream from itemData.
// When Property is Log_Buffer and the stream is well-formed BACnetLogRecord
// SEQUENCEs, LogRecords is also populated (len matches ItemCount). Prefer
// LogRecords for typed Trend Log access; ItemData remains for generic ranges.
// FirstSequence is set when present on the wire.
type ReadRangeACK struct {
	Object        bacnet.ObjectIdentifier
	Property      bacnet.PropertyReference
	ResultFlags   bacnet.BitString
	ItemCount     uint32
	ItemData      []bacnet.ApplicationValue
	LogRecords    []LogRecord
	FirstSequence *uint32
}

// FirstItem reports the FIRST_ITEM result flag.
func (a ReadRangeACK) FirstItem() bool { return bitStringBit(a.ResultFlags, ResultFlagFirstItem) }

// LastItem reports the LAST_ITEM result flag.
func (a ReadRangeACK) LastItem() bool { return bitStringBit(a.ResultFlags, ResultFlagLastItem) }

// MoreItems reports the MORE_ITEMS result flag.
func (a ReadRangeACK) MoreItems() bool { return bitStringBit(a.ResultFlags, ResultFlagMoreItems) }

func bitStringBit(bs bacnet.BitString, bit int) bool {
	if bit < 0 {
		return false
	}
	byteIdx := bit / 8
	bitIdx := 7 - (bit % 8)
	if byteIdx >= len(bs.Bytes) {
		return false
	}
	return bs.Bytes[byteIdx]&(1<<uint(bitIdx)) != 0
}

// EncodeResultFlags encodes the three BACnetResultFlags bits.
func EncodeResultFlags(first, last, more bool) bacnet.BitString {
	var b byte
	if first {
		b |= 0x80
	}
	if last {
		b |= 0x40
	}
	if more {
		b |= 0x20
	}
	return bacnet.BitString{UnusedBits: 5, Bytes: []byte{b}}
}

// EncodeReadRange encodes a ReadRange request payload.
func EncodeReadRange(req ReadRangeRequest) ([]byte, error) {
	var dst []byte
	var err error
	dst, err = bacnet.AppendContextObjectID(dst, 0, req.Object)
	if err != nil {
		return nil, err
	}
	dst, err = bacnet.AppendContextUnsigned(dst, 1, uint64(req.Property.Identifier))
	if err != nil {
		return nil, err
	}
	if req.Property.ArrayIndex != nil {
		dst, err = bacnet.AppendContextUnsigned(dst, 2, uint64(*req.Property.ArrayIndex))
		if err != nil {
			return nil, err
		}
	}
	switch req.By {
	case ReadRangeAll:
		return dst, nil
	case ReadRangeByPosition:
		if req.Count == 0 {
			return nil, fmt.Errorf("%w: ReadRange count must be non-zero", bacnet.ErrMalformed)
		}
		body := []bacnet.Element{
			{Value: bacnet.UnsignedValue(uint64(req.ReferenceIndex))},
			{Value: bacnet.SignedValue(int64(req.Count))},
		}
		return bacnet.AppendContextTagged(dst, 3, body)
	case ReadRangeBySequenceNumber:
		if req.Count == 0 {
			return nil, fmt.Errorf("%w: ReadRange count must be non-zero", bacnet.ErrMalformed)
		}
		body := []bacnet.Element{
			{Value: bacnet.UnsignedValue(uint64(req.ReferenceIndex))},
			{Value: bacnet.SignedValue(int64(req.Count))},
		}
		return bacnet.AppendContextTagged(dst, 6, body)
	case ReadRangeByTime:
		if req.Count == 0 {
			return nil, fmt.Errorf("%w: ReadRange count must be non-zero", bacnet.ErrMalformed)
		}
		body := []bacnet.Element{
			{Value: bacnet.ApplicationValue{Kind: bacnet.ValueDate, Date: req.ReferenceTime.Date}},
			{Value: bacnet.ApplicationValue{Kind: bacnet.ValueTime, Time: req.ReferenceTime.Time}},
			{Value: bacnet.SignedValue(int64(req.Count))},
		}
		return bacnet.AppendContextTagged(dst, 7, body)
	default:
		return nil, fmt.Errorf("%w: unknown ReadRange range form", bacnet.ErrMalformed)
	}
}

// DecodeReadRange decodes a ReadRange request payload.
func DecodeReadRange(payload []byte, limits bacnet.DecodeLimits) (ReadRangeRequest, error) {
	elements, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return ReadRangeRequest{}, err
	}
	if n != len(payload) {
		return ReadRangeRequest{}, fmt.Errorf("%w: ReadRange trailing data", bacnet.ErrTrailingData)
	}
	var req ReadRangeRequest
	var haveObject, haveProperty, haveRange bool
	for _, el := range elements {
		switch {
		case el.TagNumber == 0 && !bacnet.IsContextConstructed(el):
			if haveObject {
				return ReadRangeRequest{}, fmt.Errorf("%w: duplicate objectIdentifier", bacnet.ErrMalformed)
			}
			req.Object, err = bacnet.ContextObjectID(el)
			haveObject = true
		case el.TagNumber == 1 && !bacnet.IsContextConstructed(el):
			if haveProperty {
				return ReadRangeRequest{}, fmt.Errorf("%w: duplicate propertyIdentifier", bacnet.ErrMalformed)
			}
			var u uint64
			u, err = bacnet.ContextUnsigned(el)
			req.Property.Identifier = bacnet.PropertyIdentifier(u)
			haveProperty = true
		case el.TagNumber == 2 && !bacnet.IsContextConstructed(el):
			var u uint64
			u, err = bacnet.ContextUnsigned(el)
			idx := uint32(u)
			req.Property.ArrayIndex = &idx
		case el.TagNumber == 3 && bacnet.IsContextConstructed(el):
			if haveRange {
				return ReadRangeRequest{}, fmt.Errorf("%w: duplicate range", bacnet.ErrMalformed)
			}
			req.By = ReadRangeByPosition
			req.ReferenceIndex, req.Count, err = decodeIndexCount(el.Value.Elements)
			haveRange = true
		case el.TagNumber == 6 && bacnet.IsContextConstructed(el):
			if haveRange {
				return ReadRangeRequest{}, fmt.Errorf("%w: duplicate range", bacnet.ErrMalformed)
			}
			req.By = ReadRangeBySequenceNumber
			req.ReferenceIndex, req.Count, err = decodeIndexCount(el.Value.Elements)
			haveRange = true
		case el.TagNumber == 7 && bacnet.IsContextConstructed(el):
			if haveRange {
				return ReadRangeRequest{}, fmt.Errorf("%w: duplicate range", bacnet.ErrMalformed)
			}
			req.By = ReadRangeByTime
			req.ReferenceTime, req.Count, err = decodeTimeCount(el.Value.Elements)
			haveRange = true
		default:
			return ReadRangeRequest{}, fmt.Errorf("%w: unexpected ReadRange tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
		if err != nil {
			return ReadRangeRequest{}, err
		}
	}
	if !haveObject || !haveProperty {
		return ReadRangeRequest{}, fmt.Errorf("%w: ReadRange missing required fields", bacnet.ErrMalformed)
	}
	if !haveRange {
		req.By = ReadRangeAll
	}
	return req, nil
}

func decodeIndexCount(els []bacnet.Element) (uint32, int32, error) {
	if len(els) != 2 {
		return 0, 0, fmt.Errorf("%w: ReadRange range expects index and count", bacnet.ErrMalformed)
	}
	if els[0].Context || els[0].Value.Kind != bacnet.ValueUnsigned {
		return 0, 0, fmt.Errorf("%w: ReadRange reference index must be Unsigned", bacnet.ErrMalformed)
	}
	if els[0].Value.Unsigned > 0xFFFFFFFF {
		return 0, 0, fmt.Errorf("%w: ReadRange reference index overflow", bacnet.ErrMalformed)
	}
	if els[1].Context || els[1].Value.Kind != bacnet.ValueSigned {
		return 0, 0, fmt.Errorf("%w: ReadRange count must be Signed", bacnet.ErrMalformed)
	}
	if els[1].Value.Signed < -0x7FFFFFFF || els[1].Value.Signed > 0x7FFFFFFF {
		return 0, 0, fmt.Errorf("%w: ReadRange count overflow", bacnet.ErrMalformed)
	}
	if els[1].Value.Signed == 0 {
		return 0, 0, fmt.Errorf("%w: ReadRange count must be non-zero", bacnet.ErrMalformed)
	}
	return uint32(els[0].Value.Unsigned), int32(els[1].Value.Signed), nil
}

func decodeTimeCount(els []bacnet.Element) (DateTime, int32, error) {
	if len(els) != 3 {
		return DateTime{}, 0, fmt.Errorf("%w: ReadRange byTime expects date, time, count", bacnet.ErrMalformed)
	}
	if els[0].Context || els[0].Value.Kind != bacnet.ValueDate {
		return DateTime{}, 0, fmt.Errorf("%w: ReadRange reference date", bacnet.ErrMalformed)
	}
	if els[1].Context || els[1].Value.Kind != bacnet.ValueTime {
		return DateTime{}, 0, fmt.Errorf("%w: ReadRange reference time", bacnet.ErrMalformed)
	}
	if els[2].Context || els[2].Value.Kind != bacnet.ValueSigned {
		return DateTime{}, 0, fmt.Errorf("%w: ReadRange count must be Signed", bacnet.ErrMalformed)
	}
	if els[2].Value.Signed == 0 {
		return DateTime{}, 0, fmt.Errorf("%w: ReadRange count must be non-zero", bacnet.ErrMalformed)
	}
	if els[2].Value.Signed < -0x7FFFFFFF || els[2].Value.Signed > 0x7FFFFFFF {
		return DateTime{}, 0, fmt.Errorf("%w: ReadRange count overflow", bacnet.ErrMalformed)
	}
	return DateTime{Date: els[0].Value.Date, Time: els[1].Value.Time}, int32(els[2].Value.Signed), nil
}

// EncodeReadRangeACK encodes a ReadRange ACK payload (tests/helpers).
func EncodeReadRangeACK(ack ReadRangeACK) ([]byte, error) {
	var dst []byte
	var err error
	dst, err = bacnet.AppendContextObjectID(dst, 0, ack.Object)
	if err != nil {
		return nil, err
	}
	dst, err = bacnet.AppendContextUnsigned(dst, 1, uint64(ack.Property.Identifier))
	if err != nil {
		return nil, err
	}
	if ack.Property.ArrayIndex != nil {
		dst, err = bacnet.AppendContextUnsigned(dst, 2, uint64(*ack.Property.ArrayIndex))
		if err != nil {
			return nil, err
		}
	}
	dst, err = bacnet.AppendContextBitString(dst, 3, ack.ResultFlags)
	if err != nil {
		return nil, err
	}
	dst, err = bacnet.AppendContextUnsigned(dst, 4, uint64(ack.ItemCount))
	if err != nil {
		return nil, err
	}
	var body []bacnet.Element
	if len(ack.LogRecords) > 0 {
		body = make([]bacnet.Element, 0, len(ack.LogRecords)*3)
		for _, rec := range ack.LogRecords {
			els, encErr := EncodeLogRecord(rec)
			if encErr != nil {
				return nil, encErr
			}
			body = append(body, els...)
		}
	} else {
		body = make([]bacnet.Element, 0, len(ack.ItemData))
		for _, item := range ack.ItemData {
			body = append(body, bacnet.Element{Value: item})
		}
	}
	dst, err = bacnet.AppendContextTagged(dst, 5, body)
	if err != nil {
		return nil, err
	}
	if ack.FirstSequence != nil {
		dst, err = bacnet.AppendContextUnsigned(dst, 6, uint64(*ack.FirstSequence))
	}
	return dst, err
}

// DecodeReadRangeACK decodes a ReadRange ComplexACK payload.
func DecodeReadRangeACK(payload []byte, limits bacnet.DecodeLimits) (ReadRangeACK, error) {
	elements, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return ReadRangeACK{}, err
	}
	if n != len(payload) {
		return ReadRangeACK{}, fmt.Errorf("%w: ReadRangeACK trailing data", bacnet.ErrTrailingData)
	}
	var ack ReadRangeACK
	var haveObject, haveProperty, haveFlags, haveCount, haveData bool
	var rawItemElements []bacnet.Element
	for _, el := range elements {
		switch {
		case el.TagNumber == 0 && !bacnet.IsContextConstructed(el):
			if haveObject {
				return ReadRangeACK{}, fmt.Errorf("%w: duplicate objectIdentifier", bacnet.ErrMalformed)
			}
			ack.Object, err = bacnet.ContextObjectID(el)
			haveObject = true
		case el.TagNumber == 1 && !bacnet.IsContextConstructed(el):
			if haveProperty {
				return ReadRangeACK{}, fmt.Errorf("%w: duplicate propertyIdentifier", bacnet.ErrMalformed)
			}
			var u uint64
			u, err = bacnet.ContextUnsigned(el)
			ack.Property.Identifier = bacnet.PropertyIdentifier(u)
			haveProperty = true
		case el.TagNumber == 2 && !bacnet.IsContextConstructed(el):
			var u uint64
			u, err = bacnet.ContextUnsigned(el)
			idx := uint32(u)
			ack.Property.ArrayIndex = &idx
		case el.TagNumber == 3 && !bacnet.IsContextConstructed(el):
			if haveFlags {
				return ReadRangeACK{}, fmt.Errorf("%w: duplicate resultFlags", bacnet.ErrMalformed)
			}
			ack.ResultFlags, err = bacnet.ContextBitString(el)
			haveFlags = true
		case el.TagNumber == 4 && !bacnet.IsContextConstructed(el):
			if haveCount {
				return ReadRangeACK{}, fmt.Errorf("%w: duplicate itemCount", bacnet.ErrMalformed)
			}
			var u uint64
			u, err = bacnet.ContextUnsigned(el)
			if u > 0xFFFFFFFF {
				return ReadRangeACK{}, fmt.Errorf("%w: itemCount overflow", bacnet.ErrMalformed)
			}
			ack.ItemCount = uint32(u)
			haveCount = true
		case el.TagNumber == 5 && bacnet.IsContextConstructed(el):
			if haveData {
				return ReadRangeACK{}, fmt.Errorf("%w: duplicate itemData", bacnet.ErrMalformed)
			}
			rawItemElements = el.Value.Elements
			ack.ItemData = make([]bacnet.ApplicationValue, 0, len(rawItemElements))
			for _, item := range rawItemElements {
				if item.Opening || item.Closing {
					return ReadRangeACK{}, fmt.Errorf("%w: unexpected open/close in itemData", bacnet.ErrMalformed)
				}
				if item.Context && !bacnet.IsContextConstructed(item) {
					// Context primitive item: surface as constructed/context value.
					ack.ItemData = append(ack.ItemData, bacnet.ApplicationValue{
						Kind: bacnet.ValueContext, Elements: []bacnet.Element{item},
					}.Clone())
					continue
				}
				if bacnet.IsContextConstructed(item) {
					ack.ItemData = append(ack.ItemData, bacnet.ApplicationValue{
						Kind: bacnet.ValueConstructed, Elements: item.Value.Elements,
					}.Clone())
					continue
				}
				ack.ItemData = append(ack.ItemData, item.Value.Clone())
			}
			haveData = true
		case el.TagNumber == 6 && !bacnet.IsContextConstructed(el):
			var u uint64
			u, err = bacnet.ContextUnsigned(el)
			if u > 0xFFFFFFFF {
				return ReadRangeACK{}, fmt.Errorf("%w: firstSequenceNumber overflow", bacnet.ErrMalformed)
			}
			seq := uint32(u)
			ack.FirstSequence = &seq
		default:
			return ReadRangeACK{}, fmt.Errorf("%w: unexpected ReadRangeACK tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
		if err != nil {
			return ReadRangeACK{}, err
		}
	}
	if !haveObject || !haveProperty || !haveFlags || !haveCount || !haveData {
		return ReadRangeACK{}, fmt.Errorf("%w: ReadRangeACK missing required fields", bacnet.ErrMalformed)
	}
	// itemData is SEQUENCE OF ABSTRACT-SYNTAX.&Type. Simple items are one
	// application tag each; complex SEQUENCEs such as BACnetLogRecord expand
	// to multiple tags with no outer delimiter. When Property is Log_Buffer,
	// attempt a typed split; otherwise trust the wire ItemCount when the flat
	// tag count differs (ItemData then holds the tag stream).
	if ack.ItemCount == 0 && len(ack.ItemData) != 0 {
		return ReadRangeACK{}, fmt.Errorf("%w: ReadRangeACK itemCount mismatch", bacnet.ErrMalformed)
	}
	if ack.ItemCount > 0 && len(ack.ItemData) == 0 {
		return ReadRangeACK{}, fmt.Errorf("%w: ReadRangeACK itemCount mismatch", bacnet.ErrMalformed)
	}
	if ack.Property.Identifier == bacnet.PropertyLogBuffer {
		if records, splitErr := DecodeLogRecords(rawItemElements, int(ack.ItemCount)); splitErr == nil {
			ack.LogRecords = records
		}
	}
	return ack, nil
}

const ServiceReadRange = apdu.ServiceReadRange
