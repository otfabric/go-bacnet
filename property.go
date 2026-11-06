// SPDX-License-Identifier: MIT

package bacnet

// PropertyIdentifier is a BACnet property identifier (standard or proprietary).
type PropertyIdentifier uint32

// Special property identifiers of ReadPropertyMultiple: a device answers each
// with every property of the object in that conformance class.
const (
	PropertyAll      PropertyIdentifier = 8
	PropertyOptional PropertyIdentifier = 80
	PropertyRequired PropertyIdentifier = 105
)

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

// Further standard property identifiers (ASHRAE 135 clause 21
// BACnetPropertyIdentifier; numbers cross-checked against bacnet-stack
// bacenum.h at the bacnet-interop pin).
const (
	PropertyAckedTransitions             PropertyIdentifier = 0
	PropertyAckRequired                  PropertyIdentifier = 1
	PropertyActiveText                   PropertyIdentifier = 4
	PropertyAlarmValue                   PropertyIdentifier = 6
	PropertyAlarmValues                  PropertyIdentifier = 7
	PropertyFaultValues                  PropertyIdentifier = 39
	PropertyAPDUSegmentTimeout           PropertyIdentifier = 10
	PropertyAPDUTimeout                  PropertyIdentifier = 11
	PropertyApplicationSoftwareVersion   PropertyIdentifier = 12
	PropertyArchive                      PropertyIdentifier = 13
	PropertyNotificationClass            PropertyIdentifier = 17
	PropertyCOVIncrement                 PropertyIdentifier = 22
	PropertyDaylightSavingsStatus        PropertyIdentifier = 24
	PropertyDeadband                     PropertyIdentifier = 25
	PropertyDeviceAddressBinding         PropertyIdentifier = 30
	PropertyEventEnable                  PropertyIdentifier = 35
	PropertyFileAccessMethod             PropertyIdentifier = 41
	PropertyFileSize                     PropertyIdentifier = 42
	PropertyFileType                     PropertyIdentifier = 43
	PropertyFirmwareRevision             PropertyIdentifier = 44
	PropertyHighLimit                    PropertyIdentifier = 45
	PropertyInactiveText                 PropertyIdentifier = 46
	PropertyLimitEnable                  PropertyIdentifier = 52
	PropertyLocalDate                    PropertyIdentifier = 56
	PropertyLocalTime                    PropertyIdentifier = 57
	PropertyLocation                     PropertyIdentifier = 58
	PropertyLowLimit                     PropertyIdentifier = 59
	PropertyModificationDate             PropertyIdentifier = 71
	PropertyNotifyType                   PropertyIdentifier = 72
	PropertyNumberOfAPDURetries          PropertyIdentifier = 73
	PropertyNumberOfStates               PropertyIdentifier = 74
	PropertyOutOfService                 PropertyIdentifier = 81
	PropertyPolarity                     PropertyIdentifier = 84
	PropertyPriority                     PropertyIdentifier = 86
	PropertyProtocolObjectTypesSupported PropertyIdentifier = 96
	PropertyProtocolServicesSupported    PropertyIdentifier = 97
	PropertyReadOnly                     PropertyIdentifier = 99
	PropertyStateText                    PropertyIdentifier = 110
	PropertyTimeDelay                    PropertyIdentifier = 113
	PropertyUTCOffset                    PropertyIdentifier = 119
	PropertyBufferSize                   PropertyIdentifier = 126
	PropertyEventTimeStamps              PropertyIdentifier = 130
	PropertyEnable                       PropertyIdentifier = 133
	PropertyLogInterval                  PropertyIdentifier = 134
	PropertyRecordCount                  PropertyIdentifier = 141
	PropertyStopWhenFull                 PropertyIdentifier = 144
	PropertyTotalRecordCount             PropertyIdentifier = 145
	PropertyActiveCOVSubscriptions       PropertyIdentifier = 152
	PropertyDatabaseRevision             PropertyIdentifier = 155
	PropertyMaxSegmentsAccepted          PropertyIdentifier = 167
	PropertyLoggingType                  PropertyIdentifier = 197
	PropertyEventDetectionEnable         PropertyIdentifier = 353
	PropertySerialNumber                 PropertyIdentifier = 372
	PropertyCurrentCommandPriority       PropertyIdentifier = 431
)

// Property identifiers of the Calendar, Schedule, value and Structured View
// objects (ASHRAE 135 clause 21 BACnetPropertyIdentifier; numbers
// cross-checked against bacnet-stack bacenum.h at 7a53f0a7).
const (
	PropertyEffectivePeriod                PropertyIdentifier = 32
	PropertyListOfObjectPropertyReferences PropertyIdentifier = 54
	PropertyMaxPresValue                   PropertyIdentifier = 65
	PropertyMinPresValue                   PropertyIdentifier = 69
	PropertyPriorityForWriting             PropertyIdentifier = 88
	PropertyNodeSubtype                    PropertyIdentifier = 207
	PropertyNodeType                       PropertyIdentifier = 208
	PropertySubordinateAnnotations         PropertyIdentifier = 210
	PropertySubordinateList                PropertyIdentifier = 211
)
