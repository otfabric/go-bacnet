// SPDX-License-Identifier: MIT

package bacnet

// PropertyIdentifier is a BACnet property identifier (standard or proprietary).
type PropertyIdentifier uint32

// Common property identifiers.
const (
	PropertyObjectIdentifier  PropertyIdentifier = 75
	PropertyObjectName        PropertyIdentifier = 77
	PropertyObjectType        PropertyIdentifier = 79
	PropertyPresentValue      PropertyIdentifier = 85
	PropertyPriorityArray     PropertyIdentifier = 87
	PropertyStatusFlags       PropertyIdentifier = 111
	PropertySystemStatus      PropertyIdentifier = 112
	PropertyVendorIdentifier  PropertyIdentifier = 120
	PropertyVendorName        PropertyIdentifier = 121
	PropertyModelName         PropertyIdentifier = 70
	PropertyProtocolVersion   PropertyIdentifier = 98
	PropertyProtocolRevision  PropertyIdentifier = 139
	PropertyMaxAPDULength     PropertyIdentifier = 62
	PropertySegmentation      PropertyIdentifier = 107
	PropertyDescription       PropertyIdentifier = 28
	PropertyUnits             PropertyIdentifier = 117
	PropertyRelinquishDefault PropertyIdentifier = 104
	PropertyLogBuffer         PropertyIdentifier = 131
	PropertyObjectList        PropertyIdentifier = 76
	PropertyPropertyList      PropertyIdentifier = 371
	PropertyReliability       PropertyIdentifier = 103
	PropertyEventState        PropertyIdentifier = 36
	PropertyWeeklySchedule    PropertyIdentifier = 123
	PropertyExceptionSchedule PropertyIdentifier = 38
	PropertyScheduleDefault   PropertyIdentifier = 174
	PropertyDateList          PropertyIdentifier = 23
	PropertyRecipientList     PropertyIdentifier = 102
	PropertyNetworkType       PropertyIdentifier = 427
	PropertyIPAddress         PropertyIdentifier = 400
	PropertyIPSubnetMask      PropertyIdentifier = 411
	PropertyBACnetIPUDPPort   PropertyIdentifier = 412
)

// ArrayAll selects all array elements.
const ArrayAll uint32 = 0xFFFFFFFF

// PropertyReference identifies a property, optionally with an array index.
type PropertyReference struct {
	Identifier PropertyIdentifier
	ArrayIndex *uint32 // nil means the entire property; ArrayAll means all elements
}

// Equal reports whether p and o identify the same property reference.
func (p PropertyReference) Equal(o PropertyReference) bool {
	if p.Identifier != o.Identifier {
		return false
	}
	if p.ArrayIndex == nil && o.ArrayIndex == nil {
		return true
	}
	if p.ArrayIndex == nil || o.ArrayIndex == nil {
		return false
	}
	return *p.ArrayIndex == *o.ArrayIndex
}
