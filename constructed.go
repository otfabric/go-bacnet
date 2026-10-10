// SPDX-License-Identifier: MIT

package bacnet

import (
	"encoding/binary"
	"fmt"
	"math"
)

// ParseSequence parses a sequence of tags until data is exhausted or a closing
// tag matching closeTag is seen (if closeTag >= 0). Consumed length is returned.
// Nested opening/closing pairs become ValueConstructed elements (Opening=false).
func ParseSequence(data []byte, limits DecodeLimits, closeTag int) ([]Element, int, error) {
	limits = limits.Normalize()
	budget := limits.MaxElements
	return parseSequence(data, limits, closeTag, 0, &budget)
}

func parseSequence(data []byte, limits DecodeLimits, closeTag int, depth int, budget *int) ([]Element, int, error) {
	if depth > limits.MaxConstructedDepth {
		return nil, 0, fmt.Errorf("%w: constructed depth", ErrLimitExceeded)
	}
	var elements []Element
	off := 0
	for off < len(data) {
		if *budget <= 0 {
			return nil, 0, fmt.Errorf("%w: max elements", ErrLimitExceeded)
		}
		el, n, err := ParseTag(data[off:], limits)
		if err != nil {
			return nil, 0, err
		}
		if el.Closing {
			if closeTag >= 0 && int(el.TagNumber) == closeTag {
				return elements, off + n, nil
			}
			return nil, 0, fmt.Errorf("%w: unexpected closing tag %d", ErrMalformed, el.TagNumber)
		}
		if el.Opening {
			inner, m, err := parseSequence(data[off+n:], limits, int(el.TagNumber), depth+1, budget)
			if err != nil {
				return nil, 0, err
			}
			*budget--
			// Constructed context value: Opening=false so AppendTag emits
			// opening + body + closing via AppendContextTagged.
			elements = append(elements, Element{
				Context:   true,
				TagNumber: el.TagNumber,
				Value: ApplicationValue{
					Kind:     ValueConstructed,
					Elements: inner,
				},
			})
			off += n + m
			continue
		}
		*budget--
		elements = append(elements, el)
		off += n
	}
	if closeTag >= 0 {
		return nil, 0, fmt.Errorf("%w: missing closing tag %d", ErrMalformed, closeTag)
	}
	return elements, off, nil
}

// AppendContextTagged appends opening tag, body tags, and closing tag.
func AppendContextTagged(dst []byte, tagNumber uint8, body []Element) ([]byte, error) {
	if tagNumber == tagNumberReserved {
		return dst, fmt.Errorf("%w: tag number 255 is reserved", ErrMalformed)
	}
	dst = appendContextOpenClose(dst, tagNumber, true)
	for _, el := range body {
		var err error
		dst, err = AppendTag(dst, el)
		if err != nil {
			return dst, err
		}
	}
	return appendContextOpenClose(dst, tagNumber, false), nil
}

// AppendContextUnsigned appends a context-tagged unsigned integer.
func AppendContextUnsigned(dst []byte, tagNumber uint8, v uint64) ([]byte, error) {
	raw := encodeUnsigned(v)
	return appendContextPrimitive(dst, tagNumber, raw)
}

// AppendContextObjectID appends a context-tagged object identifier.
func AppendContextObjectID(dst []byte, tagNumber uint8, id ObjectIdentifier) ([]byte, error) {
	enc, err := EncodeObjectIdentifier(id)
	if err != nil {
		return dst, err
	}
	var buf [4]byte
	buf[0] = byte(enc >> 24)
	buf[1] = byte(enc >> 16)
	buf[2] = byte(enc >> 8)
	buf[3] = byte(enc)
	return appendContextPrimitive(dst, tagNumber, buf[:])
}

// AppendContextNull appends a context-tagged Null.
func AppendContextNull(dst []byte, tagNumber uint8) ([]byte, error) {
	return appendContextPrimitive(dst, tagNumber, nil)
}

// AppendContextSigned appends a context-tagged signed integer.
func AppendContextSigned(dst []byte, tagNumber uint8, v int64) ([]byte, error) {
	return appendContextPrimitive(dst, tagNumber, encodeSigned(v))
}

// AppendContextBitString appends a context-tagged bit string.
func AppendContextBitString(dst []byte, tagNumber uint8, v BitString) ([]byte, error) {
	if err := validateBitString(v.UnusedBits, len(v.Bytes)); err != nil {
		return dst, err
	}
	raw := append([]byte{v.UnusedBits}, v.Bytes...)
	return appendContextPrimitive(dst, tagNumber, raw)
}

// AppendContextCharacterString appends a context-tagged character string.
func AppendContextCharacterString(dst []byte, tagNumber uint8, v CharacterString) ([]byte, error) {
	raw := append([]byte{v.Encoding}, v.Value...)
	return appendContextPrimitive(dst, tagNumber, raw)
}

// AppendContextTime appends a context-tagged Time (4 octets).
func AppendContextTime(dst []byte, tagNumber uint8, t Time) ([]byte, error) {
	return appendContextPrimitive(dst, tagNumber, []byte{t.Hour, t.Minute, t.Second, t.Hundredths})
}

// ContextTime extracts a Time from a context primitive element.
func ContextTime(el Element) (Time, error) {
	if !el.Context || el.Opening || el.Closing || el.Value.Kind == ValueConstructed {
		return Time{}, fmt.Errorf("%w: not context primitive", ErrMalformed)
	}
	if len(el.Value.OctetString) != 4 {
		return Time{}, fmt.Errorf("%w: context time length", ErrMalformed)
	}
	return Time{
		Hour:       el.Value.OctetString[0],
		Minute:     el.Value.OctetString[1],
		Second:     el.Value.OctetString[2],
		Hundredths: el.Value.OctetString[3],
	}, nil
}

// AppendContextReal appends a context-tagged REAL (4 content octets).
func AppendContextReal(dst []byte, tagNumber uint8, v float32) ([]byte, error) {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], math.Float32bits(v))
	return appendContextPrimitive(dst, tagNumber, buf[:])
}

// ContextReal extracts a REAL from a context primitive element.
func ContextReal(el Element) (float32, error) {
	if !el.Context || el.Opening || el.Closing || el.Value.Kind == ValueConstructed {
		return 0, fmt.Errorf("%w: not context primitive", ErrMalformed)
	}
	if len(el.Value.OctetString) != 4 {
		return 0, fmt.Errorf("%w: context real length", ErrMalformed)
	}
	return math.Float32frombits(binary.BigEndian.Uint32(el.Value.OctetString)), nil
}

// ContextDate extracts a Date from a context primitive element.
func ContextDate(el Element) (Date, error) {
	if !el.Context || el.Opening || el.Closing || el.Value.Kind == ValueConstructed {
		return Date{}, fmt.Errorf("%w: not context primitive", ErrMalformed)
	}
	b := el.Value.OctetString
	if len(b) != 4 {
		return Date{}, fmt.Errorf("%w: context date length", ErrMalformed)
	}
	return Date{Year: b[0], Month: b[1], Day: b[2], Weekday: b[3]}, nil
}

// ContextUnsigned32 extracts an unsigned from a context primitive element and
// rejects values that do not fit in 32 bits, so identifiers, indexes and
// counts are never truncated silently.
func ContextUnsigned32(el Element) (uint32, error) {
	u, err := ContextUnsigned(el)
	if err != nil {
		return 0, err
	}
	if u > math.MaxUint32 {
		return 0, fmt.Errorf("%w: context unsigned %d exceeds 32 bits", ErrMalformed, u)
	}
	return uint32(u), nil
}

// ContextUnsigned extracts an unsigned from a context primitive element.
func ContextUnsigned(el Element) (uint64, error) {
	if !el.Context || el.Opening || el.Closing || el.Value.Kind == ValueConstructed {
		return 0, fmt.Errorf("%w: not context primitive", ErrMalformed)
	}
	return decodeUnsigned(el.Value.OctetString)
}

// ContextSigned extracts a signed integer from a context primitive element.
func ContextSigned(el Element) (int64, error) {
	if !el.Context || el.Opening || el.Closing || el.Value.Kind == ValueConstructed {
		return 0, fmt.Errorf("%w: not context primitive", ErrMalformed)
	}
	return decodeSigned(el.Value.OctetString)
}

// ContextBitString extracts a bit string from a context primitive element.
func ContextBitString(el Element) (BitString, error) {
	if !el.Context || el.Opening || el.Closing || el.Value.Kind == ValueConstructed {
		return BitString{}, fmt.Errorf("%w: not context primitive", ErrMalformed)
	}
	if len(el.Value.OctetString) < 1 {
		return BitString{}, fmt.Errorf("%w: empty context bit string", ErrMalformed)
	}
	if err := validateBitString(el.Value.OctetString[0], len(el.Value.OctetString)-1); err != nil {
		return BitString{}, err
	}
	return BitString{
		UnusedBits: el.Value.OctetString[0],
		Bytes:      append([]byte(nil), el.Value.OctetString[1:]...),
	}, nil
}

// ContextCharacterString extracts a character string from a context primitive.
func ContextCharacterString(el Element) (CharacterString, error) {
	if !el.Context || el.Opening || el.Closing || el.Value.Kind == ValueConstructed {
		return CharacterString{}, fmt.Errorf("%w: not context primitive", ErrMalformed)
	}
	if len(el.Value.OctetString) < 1 {
		return CharacterString{}, fmt.Errorf("%w: empty context character string", ErrMalformed)
	}
	return CharacterString{
		Encoding: el.Value.OctetString[0],
		Value:    string(el.Value.OctetString[1:]),
	}, nil
}

// ContextObjectID extracts an object identifier from a context primitive.
func ContextObjectID(el Element) (ObjectIdentifier, error) {
	if !el.Context || el.Opening || el.Closing || el.Value.Kind == ValueConstructed {
		return ObjectIdentifier{}, fmt.Errorf("%w: not context primitive", ErrMalformed)
	}
	if len(el.Value.OctetString) != 4 {
		return ObjectIdentifier{}, fmt.Errorf("%w: object id length", ErrMalformed)
	}
	v := uint32(el.Value.OctetString[0])<<24 | uint32(el.Value.OctetString[1])<<16 |
		uint32(el.Value.OctetString[2])<<8 | uint32(el.Value.OctetString[3])
	return DecodeObjectIdentifier(v), nil
}
