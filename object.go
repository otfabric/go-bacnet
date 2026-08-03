// SPDX-License-Identifier: MIT

package bacnet

import (
	"encoding/binary"
	"fmt"
)

// ObjectType is a BACnet object type (standard or proprietary).
type ObjectType uint16

// Common standard object types (ASHRAE 135).
const (
	ObjectTypeAnalogInput       ObjectType = 0
	ObjectTypeAnalogOutput      ObjectType = 1
	ObjectTypeAnalogValue       ObjectType = 2
	ObjectTypeBinaryInput       ObjectType = 3
	ObjectTypeBinaryOutput      ObjectType = 4
	ObjectTypeBinaryValue       ObjectType = 5
	ObjectTypeCalendar          ObjectType = 6
	ObjectTypeCommand           ObjectType = 7
	ObjectTypeDevice            ObjectType = 8
	ObjectTypeEventEnrollment   ObjectType = 9
	ObjectTypeFile              ObjectType = 10
	ObjectTypeGroup             ObjectType = 11
	ObjectTypeLifeSafetyPoint   ObjectType = 21
	ObjectTypeLifeSafetyZone    ObjectType = 22
	ObjectTypeMultiStateInput   ObjectType = 13
	ObjectTypeNotificationClass ObjectType = 15
	ObjectTypeMultiStateValue   ObjectType = 19
	ObjectTypeTrendLog          ObjectType = 20
	ObjectTypeSchedule          ObjectType = 17
	ObjectTypeMultiStateOutput  ObjectType = 14
	ObjectTypeAccumulator       ObjectType = 23
	ObjectTypePulseConverter    ObjectType = 24
	ObjectTypeTrendLogMultiple  ObjectType = 27
	ObjectTypeNetworkPort       ObjectType = 56
	ObjectTypeAuditLog          ObjectType = 61
	ObjectTypeAccessDoor        ObjectType = 30
	ObjectTypeAccessPoint       ObjectType = 33
)

// MaxObjectInstance is the largest valid object instance (22 bits).
const MaxObjectInstance = 0x3FFFFF

// ObjectIdentifier identifies a BACnet object.
type ObjectIdentifier struct {
	Type     ObjectType
	Instance uint32
}

// EncodeObjectIdentifier packs type and instance into a 4-octet value.
func EncodeObjectIdentifier(id ObjectIdentifier) (uint32, error) {
	if id.Type > 0x3FF {
		return 0, fmt.Errorf("%w: object type %d out of range", ErrMalformed, id.Type)
	}
	if id.Instance > MaxObjectInstance {
		return 0, fmt.Errorf("%w: object instance %d out of range", ErrMalformed, id.Instance)
	}
	return (uint32(id.Type) << 22) | id.Instance, nil
}

// DecodeObjectIdentifier unpacks a 4-octet object identifier.
func DecodeObjectIdentifier(v uint32) ObjectIdentifier {
	return ObjectIdentifier{
		Type:     ObjectType(v >> 22),
		Instance: v & MaxObjectInstance,
	}
}

// AppendObjectIdentifier appends the 4-octet encoding to dst.
func AppendObjectIdentifier(dst []byte, id ObjectIdentifier) ([]byte, error) {
	v, err := EncodeObjectIdentifier(id)
	if err != nil {
		return dst, err
	}
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], v)
	return append(dst, buf[:]...), nil
}

// String returns Type:Instance.
func (id ObjectIdentifier) String() string {
	return fmt.Sprintf("%d:%d", id.Type, id.Instance)
}
