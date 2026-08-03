// SPDX-License-Identifier: MIT

package bacnet

import "fmt"

// Identifier name maps are hand-reviewed against ASHRAE 135-2020 Clause 21
// for the constants exported by this module. Unknown numeric values format as
// "ObjectType(n)" / "PropertyIdentifier(n)" so proprietary IDs remain usable.

func (t ObjectType) String() string {
	if s, ok := objectTypeNames[t]; ok {
		return s
	}
	return fmt.Sprintf("ObjectType(%d)", uint16(t))
}

func (p PropertyIdentifier) String() string {
	if s, ok := propertyIdentifierNames[p]; ok {
		return s
	}
	return fmt.Sprintf("PropertyIdentifier(%d)", uint32(p))
}

var objectTypeNames = map[ObjectType]string{
	ObjectTypeAnalogInput:       "analog-input",
	ObjectTypeAnalogOutput:      "analog-output",
	ObjectTypeAnalogValue:       "analog-value",
	ObjectTypeBinaryInput:       "binary-input",
	ObjectTypeBinaryOutput:      "binary-output",
	ObjectTypeBinaryValue:       "binary-value",
	ObjectTypeCalendar:          "calendar",
	ObjectTypeCommand:           "command",
	ObjectTypeDevice:            "device",
	ObjectTypeEventEnrollment:   "event-enrollment",
	ObjectTypeFile:              "file",
	ObjectTypeGroup:             "group",
	ObjectTypeMultiStateInput:   "multi-state-input",
	ObjectTypeMultiStateOutput:  "multi-state-output",
	ObjectTypeNotificationClass: "notification-class",
	ObjectTypeSchedule:          "schedule",
	ObjectTypeMultiStateValue:   "multi-state-value",
	ObjectTypeTrendLog:          "trend-log",
	ObjectTypeLifeSafetyPoint:   "life-safety-point",
	ObjectTypeLifeSafetyZone:    "life-safety-zone",
	ObjectTypeAccumulator:       "accumulator",
	ObjectTypePulseConverter:    "pulse-converter",
	ObjectTypeTrendLogMultiple:  "trend-log-multiple",
	ObjectTypeAccessDoor:        "access-door",
	ObjectTypeAccessPoint:       "access-point",
	ObjectTypeNetworkPort:       "network-port",
	ObjectTypeAuditLog:          "audit-log",
}

var propertyIdentifierNames = map[PropertyIdentifier]string{
	PropertyObjectIdentifier:  "object-identifier",
	PropertyObjectName:        "object-name",
	PropertyObjectType:        "object-type",
	PropertyPresentValue:      "present-value",
	PropertyPriorityArray:     "priority-array",
	PropertyStatusFlags:       "status-flags",
	PropertySystemStatus:      "system-status",
	PropertyVendorName:        "vendor-name",
	PropertyVendorIdentifier:  "vendor-identifier",
	PropertyModelName:         "model-name",
	PropertyProtocolVersion:   "protocol-version",
	PropertyProtocolRevision:  "protocol-revision",
	PropertyMaxAPDULength:     "max-apdu-length-accepted",
	PropertySegmentation:      "segmentation-supported",
	PropertyDescription:       "description",
	PropertyUnits:             "units",
	PropertyRelinquishDefault: "relinquish-default",
	PropertyLogBuffer:         "log-buffer",
	PropertyObjectList:        "object-list",
	PropertyPropertyList:      "property-list",
	PropertyReliability:       "reliability",
	PropertyEventState:        "event-state",
	PropertyWeeklySchedule:    "weekly-schedule",
	PropertyExceptionSchedule: "exception-schedule",
	PropertyScheduleDefault:   "schedule-default",
	PropertyDateList:          "date-list",
	PropertyRecipientList:     "recipient-list",
	PropertyNetworkType:       "network-type",
	PropertyIPAddress:         "ip-address",
	PropertyIPSubnetMask:      "ip-subnet-mask",
	PropertyBACnetIPUDPPort:   "bacnet-ip-udp-port",
}
