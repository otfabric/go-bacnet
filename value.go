// SPDX-License-Identifier: MIT

package bacnet

import (
	"fmt"
	"math"
	"unicode/utf16"
	"unicode/utf8"
)

// ApplicationTag numbers for BACnet application-tagged values.
type ApplicationTag uint8

const (
	TagNull ApplicationTag = iota
	TagBoolean
	TagUnsigned
	TagSigned
	TagReal
	TagDouble
	TagOctetString
	TagCharacterString
	TagBitString
	TagEnumerated
	TagDate
	TagTime
	TagObjectIdentifier
	TagReserved13
	TagReserved14
	TagReserved15
)

// Character string encodings (ASHRAE 135 Clause 20.2.9).
const (
	CharacterEncodingUTF8     uint8 = 0 // ISO 10646 (UTF-8), formerly ANSI X3.4
	CharacterEncodingDBCS     uint8 = 1 // IBM/Microsoft DBCS
	CharacterEncodingJIS      uint8 = 2 // JIS X 0208
	CharacterEncodingUCS4     uint8 = 3 // ISO 10646 (UCS-4), big-endian
	CharacterEncodingUCS2     uint8 = 4 // ISO 10646 (UCS-2), big-endian
	CharacterEncodingISO88591 uint8 = 5 // ISO 8859-1
)

// CharacterString holds a BACnet character string with encoding byte.
//
// Value holds the raw string octets exactly as they appear on the wire after
// the encoding octet; it is only valid Go text when Encoding is
// CharacterEncodingUTF8. Use Text to obtain a Go string for the other
// supported encodings.
type CharacterString struct {
	Encoding uint8
	Value    string
}

// Text converts the string octets to a Go (UTF-8) string.
//
// UTF-8 (0) is validated, UCS-4 (3) and UCS-2 (4) are decoded as big-endian
// code units, and ISO 8859-1 (5) is mapped octet by octet. DBCS (1), JIS X
// 0208 (2) and unknown encodings return an error wrapping ErrUnsupported;
// octets that are not valid for the declared encoding return an error
// wrapping ErrMalformed.
func (c CharacterString) Text() (string, error) {
	switch c.Encoding {
	case CharacterEncodingUTF8:
		if !utf8.ValidString(c.Value) {
			return "", fmt.Errorf("%w: character string is not valid UTF-8", ErrMalformed)
		}
		return c.Value, nil
	case CharacterEncodingUCS4:
		if len(c.Value)%4 != 0 {
			return "", fmt.Errorf("%w: UCS-4 character string length %d", ErrMalformed, len(c.Value))
		}
		out := make([]rune, 0, len(c.Value)/4)
		for i := 0; i < len(c.Value); i += 4 {
			u := uint32(c.Value[i])<<24 | uint32(c.Value[i+1])<<16 | uint32(c.Value[i+2])<<8 | uint32(c.Value[i+3])
			if u > utf8.MaxRune || (u >= 0xD800 && u <= 0xDFFF) {
				return "", fmt.Errorf("%w: UCS-4 code point %#x", ErrMalformed, u)
			}
			out = append(out, rune(u))
		}
		return string(out), nil
	case CharacterEncodingUCS2:
		if len(c.Value)%2 != 0 {
			return "", fmt.Errorf("%w: UCS-2 character string length %d", ErrMalformed, len(c.Value))
		}
		units := make([]uint16, 0, len(c.Value)/2)
		for i := 0; i < len(c.Value); i += 2 {
			units = append(units, uint16(c.Value[i])<<8|uint16(c.Value[i+1]))
		}
		// Surrogate pairs are tolerated (peers commonly send UTF-16).
		return string(utf16.Decode(units)), nil
	case CharacterEncodingISO88591:
		out := make([]rune, len(c.Value))
		for i := 0; i < len(c.Value); i++ {
			out[i] = rune(c.Value[i])
		}
		return string(out), nil
	default:
		return "", fmt.Errorf("%w: character string encoding %d", ErrUnsupported, c.Encoding)
	}
}

// BitString holds a BACnet bit string.
type BitString struct {
	UnusedBits uint8
	Bytes      []byte
}

// Date is a BACnet date (year offset from 1900, month, day, weekday).
type Date struct {
	Year    uint8 // years since 1900; 0xFF = unspecified
	Month   uint8
	Day     uint8
	Weekday uint8
}

// Time is a BACnet time.
type Time struct {
	Hour       uint8
	Minute     uint8
	Second     uint8
	Hundredths uint8
}

// ValueKind discriminates ApplicationValue contents.
type ValueKind uint8

const (
	ValueNull ValueKind = iota
	ValueBoolean
	ValueUnsigned
	ValueSigned
	ValueReal
	ValueDouble
	ValueOctetString
	ValueCharacterString
	ValueBitString
	ValueEnumerated
	ValueDate
	ValueTime
	ValueObjectID
	ValueConstructed
	ValueContext // context-tagged primitive; OctetString holds the raw content octets
)

// Element is a generic BACnet tag element (application or context).
type Element struct {
	Context   bool
	TagNumber uint8
	Opening   bool
	Closing   bool
	Value     ApplicationValue
}

// ApplicationValue is a generic decoded BACnet application value.
//
// OctetString and BitString.Bytes may alias parse input unless Clone is used.
// Runtime storage beyond packet processing must call Clone.
type ApplicationValue struct {
	Kind ValueKind

	Boolean     bool
	Unsigned    uint64
	Signed      int64
	Real        float32
	Double      float64
	OctetString []byte
	Character   CharacterString
	BitString   BitString
	Enumerated  uint32
	Date        Date
	Time        Time
	ObjectID    ObjectIdentifier

	// Constructed / context nesting.
	Elements []Element
}

// TagNumber returns the application tag for primitive kinds.
func (v ApplicationValue) TagNumber() ApplicationTag {
	switch v.Kind {
	case ValueNull:
		return TagNull
	case ValueBoolean:
		return TagBoolean
	case ValueUnsigned:
		return TagUnsigned
	case ValueSigned:
		return TagSigned
	case ValueReal:
		return TagReal
	case ValueDouble:
		return TagDouble
	case ValueOctetString:
		return TagOctetString
	case ValueCharacterString:
		return TagCharacterString
	case ValueBitString:
		return TagBitString
	case ValueEnumerated:
		return TagEnumerated
	case ValueDate:
		return TagDate
	case ValueTime:
		return TagTime
	case ValueObjectID:
		return TagObjectIdentifier
	default:
		return TagNull
	}
}

// Clone returns a deep copy that owns all byte slices.
func (v ApplicationValue) Clone() ApplicationValue {
	out := v
	if v.OctetString != nil {
		out.OctetString = append([]byte(nil), v.OctetString...)
	}
	if v.BitString.Bytes != nil {
		out.BitString.Bytes = append([]byte(nil), v.BitString.Bytes...)
	}
	if v.Elements != nil {
		out.Elements = make([]Element, len(v.Elements))
		for i, e := range v.Elements {
			out.Elements[i] = Element{
				Context:   e.Context,
				TagNumber: e.TagNumber,
				Opening:   e.Opening,
				Closing:   e.Closing,
				Value:     e.Value.Clone(),
			}
		}
	}
	return out
}

// NullValue returns a Null application value.
func NullValue() ApplicationValue { return ApplicationValue{Kind: ValueNull} }

// BoolValue returns a Boolean application value.
func BoolValue(b bool) ApplicationValue {
	return ApplicationValue{Kind: ValueBoolean, Boolean: b}
}

// UnsignedValue returns an Unsigned application value.
func UnsignedValue(u uint64) ApplicationValue {
	return ApplicationValue{Kind: ValueUnsigned, Unsigned: u}
}

// SignedValue returns a Signed application value.
func SignedValue(s int64) ApplicationValue {
	return ApplicationValue{Kind: ValueSigned, Signed: s}
}

// RealValue returns a Real application value.
func RealValue(f float32) ApplicationValue {
	return ApplicationValue{Kind: ValueReal, Real: f}
}

// DoubleValue returns a Double application value.
func DoubleValue(f float64) ApplicationValue {
	return ApplicationValue{Kind: ValueDouble, Double: f}
}

// EnumValue returns an Enumerated application value.
func EnumValue(e uint32) ApplicationValue {
	return ApplicationValue{Kind: ValueEnumerated, Enumerated: e}
}

// ObjectIDValue returns an ObjectIdentifier application value.
func ObjectIDValue(id ObjectIdentifier) ApplicationValue {
	return ApplicationValue{Kind: ValueObjectID, ObjectID: id}
}

// AsReal converts a Real or Double value to float32.
func AsReal(v ApplicationValue) (float32, error) {
	switch v.Kind {
	case ValueReal:
		return v.Real, nil
	case ValueDouble:
		if v.Double > math.MaxFloat32 || v.Double < -math.MaxFloat32 {
			return 0, fmt.Errorf("%w: double out of float32 range", ErrUnsupported)
		}
		return float32(v.Double), nil
	default:
		return 0, fmt.Errorf("%w: value kind %d is not real", ErrUnsupported, v.Kind)
	}
}

// AsUnsigned converts an Unsigned or Enumerated value to uint64.
func AsUnsigned(v ApplicationValue) (uint64, error) {
	switch v.Kind {
	case ValueUnsigned:
		return v.Unsigned, nil
	case ValueEnumerated:
		return uint64(v.Enumerated), nil
	default:
		return 0, fmt.Errorf("%w: value kind %d is not unsigned", ErrUnsupported, v.Kind)
	}
}

// AsEnumerated converts an Enumerated value to uint32.
func AsEnumerated(v ApplicationValue) (uint32, error) {
	if v.Kind != ValueEnumerated {
		return 0, fmt.Errorf("%w: value kind %d is not enumerated", ErrUnsupported, v.Kind)
	}
	return v.Enumerated, nil
}

// AsBool converts a Boolean value.
func AsBool(v ApplicationValue) (bool, error) {
	if v.Kind != ValueBoolean {
		return false, fmt.Errorf("%w: value kind %d is not boolean", ErrUnsupported, v.Kind)
	}
	return v.Boolean, nil
}

// AsObjectID converts an ObjectIdentifier value.
func AsObjectID(v ApplicationValue) (ObjectIdentifier, error) {
	if v.Kind != ValueObjectID {
		return ObjectIdentifier{}, fmt.Errorf("%w: value kind %d is not object identifier", ErrUnsupported, v.Kind)
	}
	return v.ObjectID, nil
}
