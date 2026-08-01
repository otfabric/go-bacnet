// SPDX-License-Identifier: MIT

package service

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/otfabric/go-bacnet"
)

// LogDatumChoice identifies the BACnetLogRecord logDatum CHOICE alternative.
type LogDatumChoice uint8

const (
	LogDatumLogStatus  LogDatumChoice = 0
	LogDatumBoolean    LogDatumChoice = 1
	LogDatumReal       LogDatumChoice = 2
	LogDatumEnumerated LogDatumChoice = 3
	LogDatumUnsigned   LogDatumChoice = 4
	LogDatumSigned     LogDatumChoice = 5
	LogDatumBitString  LogDatumChoice = 6
	LogDatumNull       LogDatumChoice = 7
	LogDatumFailure    LogDatumChoice = 8
	LogDatumTimeChange LogDatumChoice = 9
	LogDatumAnyValue   LogDatumChoice = 10
)

// LogRecord is a BACnetLogRecord (Trend Log Log_Buffer item).
//
// Wire form (ASHRAE 135):
//
//	timestamp [0] BACnetDateTime
//	logDatum  [1] CHOICE { … }
//	statusFlags [2] BACnetStatusFlags OPTIONAL
type LogRecord struct {
	Timestamp   DateTime
	DatumChoice LogDatumChoice
	// Datum holds the decoded logDatum value. For Failure/AnyValue the body is
	// retained as a constructed ApplicationValue; other choices use the matching
	// primitive Kind (Boolean, Real, Enumerated, Unsigned, Signed, BitString, Null).
	Datum       bacnet.ApplicationValue
	StatusFlags *bacnet.BitString
}

// EncodeLogRecord encodes one BACnetLogRecord as a flat SEQUENCE of context tags.
func EncodeLogRecord(rec LogRecord) ([]bacnet.Element, error) {
	ts, err := bacnet.AppendContextTagged(nil, 0, []bacnet.Element{
		{Value: bacnet.ApplicationValue{Kind: bacnet.ValueDate, Date: rec.Timestamp.Date}},
		{Value: bacnet.ApplicationValue{Kind: bacnet.ValueTime, Time: rec.Timestamp.Time}},
	})
	if err != nil {
		return nil, err
	}
	datumBody, err := encodeLogDatum(rec.DatumChoice, rec.Datum)
	if err != nil {
		return nil, err
	}
	datum, err := bacnet.AppendContextTagged(nil, 1, datumBody)
	if err != nil {
		return nil, err
	}
	raw := append(ts, datum...)
	if rec.StatusFlags != nil {
		raw, err = bacnet.AppendContextBitString(raw, 2, *rec.StatusFlags)
		if err != nil {
			return nil, err
		}
	}
	els, n, err := bacnet.ParseSequence(raw, bacnet.DefaultDecodeLimits(), -1)
	if err != nil {
		return nil, err
	}
	if n != len(raw) {
		return nil, fmt.Errorf("%w: LogRecord encode trailing", bacnet.ErrTrailingData)
	}
	return els, nil
}

func encodeLogDatum(choice LogDatumChoice, v bacnet.ApplicationValue) ([]bacnet.Element, error) {
	tag := uint8(choice)
	switch choice {
	case LogDatumLogStatus, LogDatumBitString:
		if v.Kind != bacnet.ValueBitString {
			return nil, fmt.Errorf("%w: LogDatum bit string kind", bacnet.ErrMalformed)
		}
		raw, err := bacnet.AppendContextBitString(nil, tag, v.BitString)
		if err != nil {
			return nil, err
		}
		return parseOneElement(raw)
	case LogDatumBoolean:
		if v.Kind != bacnet.ValueBoolean {
			return nil, fmt.Errorf("%w: LogDatum boolean kind", bacnet.ErrMalformed)
		}
		raw, err := bacnet.AppendContextBool(nil, tag, v.Boolean)
		if err != nil {
			return nil, err
		}
		return parseOneElement(raw)
	case LogDatumReal, LogDatumTimeChange:
		if v.Kind != bacnet.ValueReal {
			return nil, fmt.Errorf("%w: LogDatum real kind", bacnet.ErrMalformed)
		}
		var buf [4]byte
		binary.BigEndian.PutUint32(buf[:], math.Float32bits(v.Real))
		raw, err := appendContextOctets(nil, tag, buf[:])
		if err != nil {
			return nil, err
		}
		return parseOneElement(raw)
	case LogDatumEnumerated:
		if v.Kind != bacnet.ValueEnumerated {
			return nil, fmt.Errorf("%w: LogDatum enumerated kind", bacnet.ErrMalformed)
		}
		raw, err := bacnet.AppendContextUnsigned(nil, tag, uint64(v.Enumerated))
		if err != nil {
			return nil, err
		}
		return parseOneElement(raw)
	case LogDatumUnsigned:
		if v.Kind != bacnet.ValueUnsigned {
			return nil, fmt.Errorf("%w: LogDatum unsigned kind", bacnet.ErrMalformed)
		}
		raw, err := bacnet.AppendContextUnsigned(nil, tag, v.Unsigned)
		if err != nil {
			return nil, err
		}
		return parseOneElement(raw)
	case LogDatumSigned:
		if v.Kind != bacnet.ValueSigned {
			return nil, fmt.Errorf("%w: LogDatum signed kind", bacnet.ErrMalformed)
		}
		raw, err := bacnet.AppendContextSigned(nil, tag, v.Signed)
		if err != nil {
			return nil, err
		}
		return parseOneElement(raw)
	case LogDatumNull:
		raw, err := bacnet.AppendContextNull(nil, tag)
		if err != nil {
			return nil, err
		}
		return parseOneElement(raw)
	case LogDatumFailure, LogDatumAnyValue:
		if v.Kind != bacnet.ValueConstructed && v.Kind != bacnet.ValueContext {
			return nil, fmt.Errorf("%w: LogDatum any/failure must be constructed", bacnet.ErrMalformed)
		}
		return []bacnet.Element{{
			Context: true, TagNumber: tag,
			Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: v.Elements},
		}}, nil
	default:
		return nil, fmt.Errorf("%w: unknown LogDatum choice %d", bacnet.ErrMalformed, choice)
	}
}

// appendContextOctets encodes a context primitive with raw value octets via a
// constructed detour using AppendContextTagged of a single octet-string-shaped
// element is not available; use Unsigned/Bool helpers for typed cases. For Real
// we lean on ParseTag round-trip through AppendContextTagged of application Real
// when possible — prefer encoding as context constructed containing app Real for
// maximum decoder compatibility.
func appendContextOctets(dst []byte, tagNumber uint8, octets []byte) ([]byte, error) {
	// Encode as context-constructed containing an application Real/Octet when
	// length is 4: many peers accept either form; bacnet4j uses context primitive.
	// Build a context primitive manually: tag | 0x08 | LVT + length + octets.
	lvt, lenPrefix := contextLengthPrefix(len(octets))
	b0 := (tagNumber << 4) | 0x08 | lvt
	dst = append(dst, b0)
	dst = append(dst, lenPrefix...)
	dst = append(dst, octets...)
	return dst, nil
}

func contextLengthPrefix(n int) (lvt byte, prefix []byte) {
	switch {
	case n < 5:
		return byte(n), nil
	case n < 254:
		return 5, []byte{byte(n)}
	case n < 65535:
		return 5, []byte{254, byte(n >> 8), byte(n)}
	default:
		return 5, []byte{255, byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}
	}
}

func parseOneElement(raw []byte) ([]bacnet.Element, error) {
	el, n, err := bacnet.ParseTag(raw, bacnet.DefaultDecodeLimits())
	if err != nil {
		return nil, err
	}
	// AppendContextTagged encodes open/close; ParseTag returns only the opening
	// tag, so reassemble via ParseSequence before checking for trailing data.
	if el.Opening {
		els, nn, err := bacnet.ParseSequence(raw, bacnet.DefaultDecodeLimits(), -1)
		if err != nil {
			return nil, err
		}
		if nn != len(raw) || len(els) != 1 {
			return nil, fmt.Errorf("%w: LogDatum element shape", bacnet.ErrMalformed)
		}
		return els, nil
	}
	if n != len(raw) {
		return nil, fmt.Errorf("%w: LogDatum trailing", bacnet.ErrTrailingData)
	}
	return []bacnet.Element{el}, nil
}

// DecodeLogRecords splits a flat itemData element stream into count LogRecords.
func DecodeLogRecords(elements []bacnet.Element, count int) ([]LogRecord, error) {
	if count < 0 {
		return nil, fmt.Errorf("%w: LogRecord count", bacnet.ErrMalformed)
	}
	if count == 0 {
		if len(elements) != 0 {
			return nil, fmt.Errorf("%w: LogRecord empty count with data", bacnet.ErrMalformed)
		}
		return nil, nil
	}
	out := make([]LogRecord, 0, count)
	off := 0
	for i := 0; i < count; i++ {
		rec, n, err := decodeLogRecord(elements[off:])
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
		off += n
	}
	if off != len(elements) {
		return nil, fmt.Errorf("%w: LogRecord trailing itemData", bacnet.ErrMalformed)
	}
	return out, nil
}

func decodeLogRecord(els []bacnet.Element) (LogRecord, int, error) {
	if len(els) < 2 {
		return LogRecord{}, 0, fmt.Errorf("%w: LogRecord truncated", bacnet.ErrMalformed)
	}
	tsEl := els[0]
	if tsEl.TagNumber != 0 || !bacnet.IsContextConstructed(tsEl) {
		return LogRecord{}, 0, fmt.Errorf("%w: LogRecord timestamp", bacnet.ErrMalformed)
	}
	if len(tsEl.Value.Elements) != 2 {
		return LogRecord{}, 0, fmt.Errorf("%w: LogRecord timestamp contents", bacnet.ErrMalformed)
	}
	d, t := tsEl.Value.Elements[0], tsEl.Value.Elements[1]
	if d.Context || d.Value.Kind != bacnet.ValueDate || t.Context || t.Value.Kind != bacnet.ValueTime {
		return LogRecord{}, 0, fmt.Errorf("%w: LogRecord timestamp kinds", bacnet.ErrMalformed)
	}
	rec := LogRecord{Timestamp: DateTime{Date: d.Value.Date, Time: t.Value.Time}}

	datumEl := els[1]
	if datumEl.TagNumber != 1 || !bacnet.IsContextConstructed(datumEl) {
		return LogRecord{}, 0, fmt.Errorf("%w: LogRecord logDatum", bacnet.ErrMalformed)
	}
	if len(datumEl.Value.Elements) != 1 {
		return LogRecord{}, 0, fmt.Errorf("%w: LogRecord logDatum choice", bacnet.ErrMalformed)
	}
	choiceEl := datumEl.Value.Elements[0]
	if !choiceEl.Context {
		return LogRecord{}, 0, fmt.Errorf("%w: LogRecord logDatum choice tag", bacnet.ErrMalformed)
	}
	rec.DatumChoice = LogDatumChoice(choiceEl.TagNumber)
	datum, err := decodeLogDatumValue(rec.DatumChoice, choiceEl)
	if err != nil {
		return LogRecord{}, 0, err
	}
	rec.Datum = datum

	n := 2
	if len(els) > 2 && els[2].Context && els[2].TagNumber == 2 && !bacnet.IsContextConstructed(els[2]) {
		bs, err := bacnet.ContextBitString(els[2])
		if err != nil {
			return LogRecord{}, 0, err
		}
		rec.StatusFlags = &bs
		n = 3
	}
	return rec, n, nil
}

func decodeLogDatumValue(choice LogDatumChoice, el bacnet.Element) (bacnet.ApplicationValue, error) {
	switch choice {
	case LogDatumLogStatus, LogDatumBitString:
		bs, err := bacnet.ContextBitString(el)
		if err != nil {
			// Constructed form with nested application bit string.
			if bacnet.IsContextConstructed(el) && len(el.Value.Elements) == 1 &&
				!el.Value.Elements[0].Context && el.Value.Elements[0].Value.Kind == bacnet.ValueBitString {
				return el.Value.Elements[0].Value.Clone(), nil
			}
			return bacnet.ApplicationValue{}, err
		}
		return bacnet.ApplicationValue{Kind: bacnet.ValueBitString, BitString: bs}, nil
	case LogDatumBoolean:
		b, err := bacnet.ContextBool(el)
		if err != nil {
			return bacnet.ApplicationValue{}, err
		}
		return bacnet.BoolValue(b), nil
	case LogDatumReal, LogDatumTimeChange:
		if !el.Context || bacnet.IsContextConstructed(el) {
			if bacnet.IsContextConstructed(el) && len(el.Value.Elements) == 1 &&
				!el.Value.Elements[0].Context && el.Value.Elements[0].Value.Kind == bacnet.ValueReal {
				return el.Value.Elements[0].Value.Clone(), nil
			}
			return bacnet.ApplicationValue{}, fmt.Errorf("%w: LogDatum real", bacnet.ErrMalformed)
		}
		if len(el.Value.OctetString) != 4 {
			return bacnet.ApplicationValue{}, fmt.Errorf("%w: LogDatum real length", bacnet.ErrMalformed)
		}
		bits := binary.BigEndian.Uint32(el.Value.OctetString)
		return bacnet.RealValue(math.Float32frombits(bits)), nil
	case LogDatumEnumerated:
		u, err := bacnet.ContextUnsigned(el)
		if err != nil {
			return bacnet.ApplicationValue{}, err
		}
		return bacnet.EnumValue(uint32(u)), nil
	case LogDatumUnsigned:
		u, err := bacnet.ContextUnsigned(el)
		if err != nil {
			return bacnet.ApplicationValue{}, err
		}
		return bacnet.UnsignedValue(u), nil
	case LogDatumSigned:
		s, err := bacnet.ContextSigned(el)
		if err != nil {
			return bacnet.ApplicationValue{}, err
		}
		return bacnet.SignedValue(s), nil
	case LogDatumNull:
		return bacnet.NullValue(), nil
	case LogDatumFailure, LogDatumAnyValue:
		if bacnet.IsContextConstructed(el) {
			return bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: el.Value.Elements}.Clone(), nil
		}
		return bacnet.ApplicationValue{Kind: bacnet.ValueContext, Elements: []bacnet.Element{el}}.Clone(), nil
	default:
		return bacnet.ApplicationValue{}, fmt.Errorf("%w: unknown LogDatum choice %d", bacnet.ErrMalformed, choice)
	}
}
