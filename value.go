// SPDX-License-Identifier: MIT

package bacnet

import (
	"fmt"
	"math"
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

// CharacterString holds a BACnet character string with encoding byte.
type CharacterString struct {
	Encoding uint8
	Value    string
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
	ValueContext // context-tagged primitive or opening; Elements hold nested data
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
