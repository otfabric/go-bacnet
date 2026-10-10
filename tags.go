// SPDX-License-Identifier: MIT

package bacnet

import (
	"encoding/binary"
	"fmt"
	"math"
)

// tagNumberReserved is the extended tag number reserved by ASHRAE 135
// (Clause 20.2.1.2); it is never valid on the wire.
const tagNumberReserved = 255

// ParseTag parses one BACnet tag from data and returns the element, bytes
// consumed, and error. Application primitive values may alias data.
func ParseTag(data []byte, limits DecodeLimits) (Element, int, error) {
	limits = limits.Normalize()
	if len(data) < 1 {
		return Element{}, 0, fmt.Errorf("%w: empty tag", ErrMalformed)
	}
	b0 := data[0]
	context := b0&0x08 != 0
	tagNumber := b0 >> 4
	off := 1

	extended := tagNumber == 0x0F
	if extended {
		if off >= len(data) {
			return Element{}, 0, fmt.Errorf("%w: truncated extended tag number", ErrMalformed)
		}
		tagNumber = data[off]
		off++
		// Tag numbers 0..14 use the initial octet; 255 is reserved.
		if tagNumber < 0x0F || tagNumber == tagNumberReserved {
			return Element{}, 0, fmt.Errorf("%w: invalid extended tag number %d", ErrMalformed, tagNumber)
		}
	}

	// Opening / closing tags (context, LVT = 6 or 7).
	lvt := b0 & 0x07
	if context && lvt == 6 {
		return Element{Context: true, TagNumber: tagNumber, Opening: true}, off, nil
	}
	if context && lvt == 7 {
		return Element{Context: true, TagNumber: tagNumber, Closing: true}, off, nil
	}

	// Application BOOLEAN encodes the value in LVT with zero value octets;
	// only 0 (FALSE) and 1 (TRUE) are defined (Clause 20.2.3).
	if !context && ApplicationTag(tagNumber) == TagBoolean {
		if lvt > 1 {
			return Element{}, 0, fmt.Errorf("%w: boolean length/value/type %d", ErrMalformed, lvt)
		}
		return Element{
			TagNumber: tagNumber,
			Value:     BoolValue(lvt == 1),
		}, off, nil
	}

	length, n, err := parseLength(data[off:], lvt)
	if err != nil {
		return Element{}, 0, err
	}
	off += n
	// Compare against the remaining length so a hostile 32-bit extended
	// length cannot overflow off+length where int is 32 bits wide.
	if length > len(data)-off {
		return Element{}, 0, fmt.Errorf("%w: truncated tag value", ErrMalformed)
	}
	valueBytes := data[off : off+length]
	off += length

	if context {
		// Context-tagged primitive: preserve raw octets. Context BOOLEAN is
		// encoded as a one-octet 0/1 value (not application-style LVT).
		if length > limits.MaxOctetStringLength {
			return Element{}, 0, fmt.Errorf("%w: context value length", ErrLimitExceeded)
		}
		return Element{
			Context:   true,
			TagNumber: tagNumber,
			Value: ApplicationValue{
				Kind:        ValueContext,
				OctetString: valueBytes,
			},
		}, off, nil
	}

	v, err := decodeApplicationValue(ApplicationTag(tagNumber), valueBytes, lvt, limits)
	if err != nil {
		return Element{}, 0, err
	}
	return Element{Context: false, TagNumber: tagNumber, Value: v}, off, nil
}

func parseLength(data []byte, lvt uint8) (int, int, error) {
	if lvt < 5 {
		return int(lvt), 0, nil
	}
	if lvt == 5 {
		if len(data) < 1 {
			return 0, 0, fmt.Errorf("%w: truncated extended length", ErrMalformed)
		}
		ext := data[0]
		if ext < 254 {
			return int(ext), 1, nil
		}
		if ext == 254 {
			if len(data) < 3 {
				return 0, 0, fmt.Errorf("%w: truncated 16-bit length", ErrMalformed)
			}
			u := binary.BigEndian.Uint16(data[1:3])
			return int(u), 3, nil
		}
		if len(data) < 5 {
			return 0, 0, fmt.Errorf("%w: truncated 32-bit length", ErrMalformed)
		}
		u := binary.BigEndian.Uint32(data[1:5])
		// Reject lengths that cannot be represented as a non-negative int
		// (relevant on 32-bit platforms where int is 32-bit).
		const maxSafe = int(^uint(0) >> 1)
		if uint64(u) > uint64(maxSafe) {
			return 0, 0, fmt.Errorf("%w: extended length", ErrLimitExceeded)
		}
		return int(u), 5, nil
	}
	return 0, 0, fmt.Errorf("%w: invalid length/value/type %d", ErrMalformed, lvt)
}

func decodeApplicationValue(tag ApplicationTag, data []byte, lvt uint8, limits DecodeLimits) (ApplicationValue, error) {
	switch tag {
	case TagNull:
		if len(data) != 0 {
			return ApplicationValue{}, fmt.Errorf("%w: null with value", ErrMalformed)
		}
		return NullValue(), nil
	case TagBoolean:
		// ParseTag handles application BOOLEAN before reading a length; this
		// arm only guards direct callers.
		if len(data) != 0 || lvt > 1 {
			return ApplicationValue{}, fmt.Errorf("%w: boolean length", ErrMalformed)
		}
		return BoolValue(lvt == 1), nil
	case TagUnsigned:
		u, err := decodeUnsigned(data)
		if err != nil {
			return ApplicationValue{}, err
		}
		return UnsignedValue(u), nil
	case TagSigned:
		s, err := decodeSigned(data)
		if err != nil {
			return ApplicationValue{}, err
		}
		return SignedValue(s), nil
	case TagReal:
		if len(data) != 4 {
			return ApplicationValue{}, fmt.Errorf("%w: real length", ErrMalformed)
		}
		bits := binary.BigEndian.Uint32(data)
		return RealValue(math.Float32frombits(bits)), nil
	case TagDouble:
		if len(data) != 8 {
			return ApplicationValue{}, fmt.Errorf("%w: double length", ErrMalformed)
		}
		bits := binary.BigEndian.Uint64(data)
		return DoubleValue(math.Float64frombits(bits)), nil
	case TagOctetString:
		if len(data) > limits.MaxOctetStringLength {
			return ApplicationValue{}, fmt.Errorf("%w: octet string", ErrLimitExceeded)
		}
		return ApplicationValue{Kind: ValueOctetString, OctetString: data}, nil
	case TagCharacterString:
		if len(data) < 1 {
			return ApplicationValue{}, fmt.Errorf("%w: character string truncated", ErrMalformed)
		}
		if len(data)-1 > limits.MaxCharacterLength {
			return ApplicationValue{}, fmt.Errorf("%w: character string", ErrLimitExceeded)
		}
		return ApplicationValue{
			Kind: ValueCharacterString,
			Character: CharacterString{
				Encoding: data[0],
				Value:    string(data[1:]),
			},
		}, nil
	case TagBitString:
		if len(data) < 1 {
			return ApplicationValue{}, fmt.Errorf("%w: bit string truncated", ErrMalformed)
		}
		unused := data[0]
		if err := validateBitString(unused, len(data)-1); err != nil {
			return ApplicationValue{}, err
		}
		bits := (len(data) - 1) * 8
		if bits > limits.MaxBitStringBits {
			return ApplicationValue{}, fmt.Errorf("%w: bit string", ErrLimitExceeded)
		}
		return ApplicationValue{
			Kind: ValueBitString,
			BitString: BitString{
				UnusedBits: unused,
				Bytes:      data[1:],
			},
		}, nil
	case TagEnumerated:
		u, err := decodeUnsigned(data)
		if err != nil {
			return ApplicationValue{}, err
		}
		if u > math.MaxUint32 {
			return ApplicationValue{}, fmt.Errorf("%w: enumerated overflow", ErrMalformed)
		}
		return EnumValue(uint32(u)), nil
	case TagDate:
		if len(data) != 4 {
			return ApplicationValue{}, fmt.Errorf("%w: date length", ErrMalformed)
		}
		return ApplicationValue{Kind: ValueDate, Date: Date{data[0], data[1], data[2], data[3]}}, nil
	case TagTime:
		if len(data) != 4 {
			return ApplicationValue{}, fmt.Errorf("%w: time length", ErrMalformed)
		}
		return ApplicationValue{Kind: ValueTime, Time: Time{data[0], data[1], data[2], data[3]}}, nil
	case TagObjectIdentifier:
		if len(data) != 4 {
			return ApplicationValue{}, fmt.Errorf("%w: object identifier length", ErrMalformed)
		}
		v := binary.BigEndian.Uint32(data)
		return ObjectIDValue(DecodeObjectIdentifier(v)), nil
	default:
		// Unknown/reserved application tags: preserve as octet string.
		if len(data) > limits.MaxOctetStringLength {
			return ApplicationValue{}, fmt.Errorf("%w: unknown tag value", ErrLimitExceeded)
		}
		return ApplicationValue{Kind: ValueOctetString, OctetString: data}, nil
	}
}

// validateBitString checks the unused-bits octet of a BIT STRING against the
// number of data octets: 0..7 unused bits, and 0 when there are no data octets
// (Clause 20.2.10).
func validateBitString(unused uint8, octets int) error {
	if unused > 7 {
		return fmt.Errorf("%w: bit string unused bits %d", ErrMalformed, unused)
	}
	if octets == 0 && unused != 0 {
		return fmt.Errorf("%w: empty bit string with %d unused bits", ErrMalformed, unused)
	}
	return nil
}

func decodeUnsigned(data []byte) (uint64, error) {
	if len(data) == 0 || len(data) > 8 {
		return 0, fmt.Errorf("%w: unsigned length %d", ErrMalformed, len(data))
	}
	var u uint64
	for _, b := range data {
		u = (u << 8) | uint64(b)
	}
	return u, nil
}

func decodeSigned(data []byte) (int64, error) {
	if len(data) == 0 || len(data) > 8 {
		return 0, fmt.Errorf("%w: signed length %d", ErrMalformed, len(data))
	}
	var u uint64
	for _, b := range data {
		u = (u << 8) | uint64(b)
	}
	// Sign-extend.
	shift := uint((8 - len(data)) * 8)
	return int64(u<<shift) >> shift, nil
}

// AppendTag appends a deterministic encoding of el to dst.
//
// A context element whose Value is a typed primitive (for example
// Element{Context: true, TagNumber: 1, Value: RealValue(1)}) is encoded as a
// context-tagged primitive carrying that value's content octets; a context
// BOOLEAN is one octet. A ValueContext value contributes its raw OctetString.
// A value that cannot be encoded returns an error; nothing is dropped
// silently.
func AppendTag(dst []byte, el Element) ([]byte, error) {
	if (el.Opening || el.Closing || el.Context) && el.TagNumber == tagNumberReserved {
		return dst, fmt.Errorf("%w: tag number 255 is reserved", ErrMalformed)
	}
	if el.Opening {
		return appendContextOpenClose(dst, el.TagNumber, true), nil
	}
	if el.Closing {
		return appendContextOpenClose(dst, el.TagNumber, false), nil
	}
	if el.Context && el.Value.Kind == ValueConstructed {
		return AppendContextTagged(dst, el.TagNumber, el.Value.Elements)
	}
	if el.Context {
		raw, err := contextContentOctets(el.Value)
		if err != nil {
			return dst, err
		}
		return appendContextPrimitive(dst, el.TagNumber, raw)
	}
	return AppendApplicationValue(dst, el.Value)
}

// contextContentOctets returns the content octets of a primitive value as
// they appear under a context tag (Clause 20.2.1.3.1): the application
// encoding without its tag, with BOOLEAN as a single 0/1 octet.
func contextContentOctets(v ApplicationValue) ([]byte, error) {
	switch v.Kind {
	case ValueContext:
		if len(v.Elements) != 0 {
			return nil, fmt.Errorf("%w: context primitive with nested elements", ErrUnsupported)
		}
		return v.OctetString, nil
	case ValueNull:
		// The zero Kind doubles as "raw octets" for elements built without a
		// Kind; a true Null has no content octets.
		return v.OctetString, nil
	case ValueBoolean:
		if v.Boolean {
			return []byte{1}, nil
		}
		return []byte{0}, nil
	case ValueUnsigned:
		return encodeUnsigned(v.Unsigned), nil
	case ValueSigned:
		return encodeSigned(v.Signed), nil
	case ValueReal:
		var buf [4]byte
		binary.BigEndian.PutUint32(buf[:], math.Float32bits(v.Real))
		return buf[:], nil
	case ValueDouble:
		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], math.Float64bits(v.Double))
		return buf[:], nil
	case ValueOctetString:
		return v.OctetString, nil
	case ValueCharacterString:
		return append([]byte{v.Character.Encoding}, v.Character.Value...), nil
	case ValueBitString:
		if err := validateBitString(v.BitString.UnusedBits, len(v.BitString.Bytes)); err != nil {
			return nil, err
		}
		return append([]byte{v.BitString.UnusedBits}, v.BitString.Bytes...), nil
	case ValueEnumerated:
		return encodeUnsigned(uint64(v.Enumerated)), nil
	case ValueDate:
		return []byte{v.Date.Year, v.Date.Month, v.Date.Day, v.Date.Weekday}, nil
	case ValueTime:
		return []byte{v.Time.Hour, v.Time.Minute, v.Time.Second, v.Time.Hundredths}, nil
	case ValueObjectID:
		enc, err := EncodeObjectIdentifier(v.ObjectID)
		if err != nil {
			return nil, err
		}
		var buf [4]byte
		binary.BigEndian.PutUint32(buf[:], enc)
		return buf[:], nil
	default:
		return nil, fmt.Errorf("%w: cannot encode value kind %d under a context tag", ErrUnsupported, v.Kind)
	}
}

// AppendContextBool appends a context-tagged BOOLEAN as one value octet (0 or 1).
func AppendContextBool(dst []byte, tagNumber uint8, v bool) ([]byte, error) {
	var b byte
	if v {
		b = 1
	}
	return appendContextPrimitive(dst, tagNumber, []byte{b})
}

// ContextBool extracts a context-tagged BOOLEAN (exactly one octet 0 or 1).
func ContextBool(el Element) (bool, error) {
	if !el.Context || el.Opening || el.Closing || el.Value.Kind == ValueConstructed {
		return false, fmt.Errorf("%w: not context boolean", ErrMalformed)
	}
	if len(el.Value.OctetString) != 1 {
		return false, fmt.Errorf("%w: context boolean length", ErrMalformed)
	}
	switch el.Value.OctetString[0] {
	case 0:
		return false, nil
	case 1:
		return true, nil
	default:
		return false, fmt.Errorf("%w: context boolean value", ErrMalformed)
	}
}

// IsContextConstructed reports a context-tagged constructed (open/close) value.
func IsContextConstructed(el Element) bool {
	return el.Context && !el.Opening && !el.Closing && el.Value.Kind == ValueConstructed
}

func appendContextOpenClose(dst []byte, tagNumber uint8, opening bool) []byte {
	lvt := byte(7)
	if opening {
		lvt = 6
	}
	if tagNumber < 15 {
		return append(dst, (tagNumber<<4)|0x08|lvt)
	}
	dst = append(dst, 0xF0|0x08|lvt)
	return append(dst, tagNumber)
}

func appendContextPrimitive(dst []byte, tagNumber uint8, value []byte) ([]byte, error) {
	if tagNumber == tagNumberReserved {
		return dst, fmt.Errorf("%w: tag number 255 is reserved", ErrMalformed)
	}
	hdr, err := encodeTagHeader(true, tagNumber, len(value))
	if err != nil {
		return dst, err
	}
	dst = append(dst, hdr...)
	return append(dst, value...), nil
}

// AppendApplicationValue appends a deterministic application-tagged encoding.
func AppendApplicationValue(dst []byte, v ApplicationValue) ([]byte, error) {
	switch v.Kind {
	case ValueNull:
		return append(dst, byte(TagNull)<<4), nil
	case ValueBoolean:
		if v.Boolean {
			return append(dst, (byte(TagBoolean)<<4)|1), nil
		}
		return append(dst, byte(TagBoolean)<<4), nil
	case ValueUnsigned:
		raw := encodeUnsigned(v.Unsigned)
		return appendAppPrimitive(dst, TagUnsigned, raw)
	case ValueSigned:
		raw := encodeSigned(v.Signed)
		return appendAppPrimitive(dst, TagSigned, raw)
	case ValueReal:
		var buf [4]byte
		binary.BigEndian.PutUint32(buf[:], math.Float32bits(v.Real))
		return appendAppPrimitive(dst, TagReal, buf[:])
	case ValueDouble:
		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], math.Float64bits(v.Double))
		return appendAppPrimitive(dst, TagDouble, buf[:])
	case ValueOctetString:
		return appendAppPrimitive(dst, TagOctetString, v.OctetString)
	case ValueCharacterString:
		raw := append([]byte{v.Character.Encoding}, v.Character.Value...)
		return appendAppPrimitive(dst, TagCharacterString, raw)
	case ValueBitString:
		if err := validateBitString(v.BitString.UnusedBits, len(v.BitString.Bytes)); err != nil {
			return dst, err
		}
		raw := append([]byte{v.BitString.UnusedBits}, v.BitString.Bytes...)
		return appendAppPrimitive(dst, TagBitString, raw)
	case ValueEnumerated:
		raw := encodeUnsigned(uint64(v.Enumerated))
		return appendAppPrimitive(dst, TagEnumerated, raw)
	case ValueDate:
		return appendAppPrimitive(dst, TagDate, []byte{v.Date.Year, v.Date.Month, v.Date.Day, v.Date.Weekday})
	case ValueTime:
		return appendAppPrimitive(dst, TagTime, []byte{v.Time.Hour, v.Time.Minute, v.Time.Second, v.Time.Hundredths})
	case ValueObjectID:
		enc, err := EncodeObjectIdentifier(v.ObjectID)
		if err != nil {
			return dst, err
		}
		var buf [4]byte
		binary.BigEndian.PutUint32(buf[:], enc)
		return appendAppPrimitive(dst, TagObjectIdentifier, buf[:])
	case ValueConstructed:
		for _, e := range v.Elements {
			var err error
			dst, err = AppendTag(dst, e)
			if err != nil {
				return dst, err
			}
		}
		return dst, nil
	default:
		return dst, fmt.Errorf("%w: cannot encode value kind %d", ErrUnsupported, v.Kind)
	}
}

func appendAppPrimitive(dst []byte, tag ApplicationTag, value []byte) ([]byte, error) {
	hdr, err := encodeTagHeader(false, uint8(tag), len(value))
	if err != nil {
		return dst, err
	}
	dst = append(dst, hdr...)
	return append(dst, value...), nil
}

func encodeTagHeader(context bool, tagNumber uint8, length int) ([]byte, error) {
	if uint64(length) > math.MaxUint32 {
		return nil, fmt.Errorf("%w: tag value length %d", ErrLimitExceeded, length)
	}
	var b0 byte
	extTag := false
	if tagNumber < 15 {
		b0 = tagNumber << 4
	} else {
		b0 = 0xF0
		extTag = true
	}
	if context {
		b0 |= 0x08
	}
	var lengthBytes []byte
	switch {
	case length < 5:
		b0 |= byte(length)
	case length < 254:
		b0 |= 5
		lengthBytes = []byte{byte(length)}
	case length <= 0xFFFF:
		b0 |= 5
		lengthBytes = []byte{254, byte(length >> 8), byte(length)}
	default:
		b0 |= 5
		lengthBytes = []byte{255, byte(length >> 24), byte(length >> 16), byte(length >> 8), byte(length)}
	}
	out := []byte{b0}
	if extTag {
		out = append(out, tagNumber)
	}
	out = append(out, lengthBytes...)
	return out, nil
}

func encodeUnsigned(u uint64) []byte {
	if u == 0 {
		return []byte{0}
	}
	var tmp [8]byte
	n := 0
	for i := 7; i >= 0; i-- {
		shift := uint(i * 8)
		b := byte(u >> shift)
		if n > 0 || b != 0 {
			tmp[n] = b
			n++
		}
	}
	return tmp[:n]
}

func encodeSigned(v int64) []byte {
	// Minimal two's-complement encoding (1..8 octets).
	u := uint64(v)
	width := 1
	for width < 8 {
		shift := uint((8 - width) * 8)
		s := int64(u<<shift) >> shift
		if s == v {
			break
		}
		width++
	}
	out := make([]byte, width)
	for i := 0; i < width; i++ {
		out[i] = byte(u >> uint((width-1-i)*8))
	}
	return out
}

// ParseApplicationValue parses a single application-tagged value.
func ParseApplicationValue(data []byte, limits DecodeLimits) (ApplicationValue, int, error) {
	el, n, err := ParseTag(data, limits)
	if err != nil {
		return ApplicationValue{}, 0, err
	}
	if el.Context || el.Opening || el.Closing {
		return ApplicationValue{}, 0, fmt.Errorf("%w: expected application tag", ErrMalformed)
	}
	return el.Value, n, nil
}
