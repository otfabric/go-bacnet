// SPDX-License-Identifier: MIT

package bacnet

import "strconv"

// BACnet Error Class values (ASHRAE 135 BACnetErrorClass).
const (
	ErrorClassDevice        uint16 = 0
	ErrorClassObject        uint16 = 1
	ErrorClassProperty      uint16 = 2
	ErrorClassResources     uint16 = 3
	ErrorClassSecurity      uint16 = 4
	ErrorClassServices      uint16 = 5
	ErrorClassVT            uint16 = 6
	ErrorClassCommunication uint16 = 7
)

// Commonly tested BACnet Error Code values (ASHRAE 135 BACnetErrorCode), in
// numeric order. ErrorCodeName knows the full standard set.
const (
	ErrorCodeOther                             uint16 = 0
	ErrorCodeConfigurationInProgress           uint16 = 2
	ErrorCodeDeviceBusy                        uint16 = 3
	ErrorCodeDynamicCreationNotSupported       uint16 = 4
	ErrorCodeFileAccessDenied                  uint16 = 5
	ErrorCodeInconsistentParameters            uint16 = 7
	ErrorCodeInconsistentSelectionCriterion    uint16 = 8
	ErrorCodeInvalidDataType                   uint16 = 9
	ErrorCodeInvalidFileAccessMethod           uint16 = 10
	ErrorCodeInvalidFileStartPosition          uint16 = 11
	ErrorCodeInvalidParameterDataType          uint16 = 13
	ErrorCodeInvalidTimeStamp                  uint16 = 14
	ErrorCodeMissingRequiredParameter          uint16 = 16
	ErrorCodeNoObjectsOfSpecifiedType          uint16 = 17
	ErrorCodeNoSpaceForObject                  uint16 = 18
	ErrorCodeNoSpaceToAddListElement           uint16 = 19
	ErrorCodeNoSpaceToWriteProperty            uint16 = 20
	ErrorCodePropertyIsNotAList                uint16 = 22
	ErrorCodeObjectDeletionNotPermitted        uint16 = 23
	ErrorCodeObjectIdentifierAlreadyExists     uint16 = 24
	ErrorCodeOperationalProblem                uint16 = 25
	ErrorCodePasswordFailure                   uint16 = 26
	ErrorCodeReadAccessDenied                  uint16 = 27
	ErrorCodeServiceRequestDenied              uint16 = 29
	ErrorCodeTimeout                           uint16 = 30
	ErrorCodeUnknownObject                     uint16 = 31
	ErrorCodeUnknownProperty                   uint16 = 32
	ErrorCodeUnsupportedObjectType             uint16 = 36
	ErrorCodeValueOutOfRange                   uint16 = 37
	ErrorCodeWriteAccessDenied                 uint16 = 40
	ErrorCodeCharacterSetNotSupported          uint16 = 41
	ErrorCodeInvalidArrayIndex                 uint16 = 42
	ErrorCodeCOVSubscriptionFailed             uint16 = 43
	ErrorCodeNotCOVProperty                    uint16 = 44
	ErrorCodeOptionalFunctionalityNotSupported uint16 = 45
	ErrorCodeInvalidConfigurationData          uint16 = 46
	ErrorCodeDatatypeNotSupported              uint16 = 47
	ErrorCodeDuplicateName                     uint16 = 48
	ErrorCodeDuplicateObjectID                 uint16 = 49
	ErrorCodePropertyIsNotAnArray              uint16 = 50
	ErrorCodeAbortSegmentationNotSupported     uint16 = 54
	ErrorCodeInvalidTag                        uint16 = 57
	ErrorCodeNetworkDown                       uint16 = 58
	ErrorCodeRejectUnrecognizedService         uint16 = 67
	ErrorCodeUnknownDevice                     uint16 = 70
	ErrorCodeUnknownRoute                      uint16 = 71
	ErrorCodeValueNotInitialized               uint16 = 72
	ErrorCodeInvalidEventState                 uint16 = 73
	ErrorCodeNoAlarmConfigured                 uint16 = 74
	ErrorCodeLogBufferFull                     uint16 = 75
	ErrorCodeLoggedValuePurged                 uint16 = 76
	ErrorCodeNoPropertySpecified               uint16 = 77
	ErrorCodeNotConfiguredForTriggeredLogging  uint16 = 78
	ErrorCodeUnknownSubscription               uint16 = 79
	ErrorCodeParameterOutOfRange               uint16 = 80
	ErrorCodeListElementNotFound               uint16 = 81
	ErrorCodeBusy                              uint16 = 82
	ErrorCodeCommunicationDisabled             uint16 = 83
	ErrorCodeSuccess                           uint16 = 84
	ErrorCodeAccessDenied                      uint16 = 85
	ErrorCodeUnknownFileSize                   uint16 = 122
	ErrorCodeFileFull                          uint16 = 128
	ErrorCodeInternalError                     uint16 = 131
	ErrorCodeNotConfigured                     uint16 = 132
	ErrorCodeOutOfMemory                       uint16 = 133
	ErrorCodeValueTooLong                      uint16 = 134
)

// BACnet Reject reasons (ASHRAE 135 BACnetRejectReason).
const (
	RejectReasonOther                    uint8 = 0
	RejectReasonBufferOverflow           uint8 = 1
	RejectReasonInconsistentParameters   uint8 = 2
	RejectReasonInvalidParameterDataType uint8 = 3
	RejectReasonInvalidTag               uint8 = 4
	RejectReasonMissingRequiredParameter uint8 = 5
	RejectReasonParameterOutOfRange      uint8 = 6
	RejectReasonTooManyArguments         uint8 = 7
	RejectReasonUndefinedEnumeration     uint8 = 8
	RejectReasonUnrecognizedService      uint8 = 9
)

// BACnet Abort reasons (ASHRAE 135 BACnetAbortReason).
const (
	AbortReasonOther                         uint8 = 0
	AbortReasonBufferOverflow                uint8 = 1
	AbortReasonInvalidAPDUInThisState        uint8 = 2
	AbortReasonPreemptedByHigherPriorityTask uint8 = 3
	AbortReasonSegmentationNotSupported      uint8 = 4
	AbortReasonSecurityError                 uint8 = 5
	AbortReasonInsufficientSecurity          uint8 = 6
	AbortReasonWindowSizeOutOfRange          uint8 = 7
	AbortReasonApplicationExceededReplyTime  uint8 = 8
	AbortReasonOutOfResources                uint8 = 9
	AbortReasonTSMTimeout                    uint8 = 10
	AbortReasonAPDUTooLong                   uint8 = 11
)

var errorClassNames = [...]string{
	"device", "object", "property", "resources", "security", "services", "vt", "communication",
}

var rejectReasonNames = [...]string{
	"other", "buffer-overflow", "inconsistent-parameters", "invalid-parameter-data-type",
	"invalid-tag", "missing-required-parameter", "parameter-out-of-range",
	"too-many-arguments", "undefined-enumeration", "unrecognized-service",
}

var abortReasonNames = [...]string{
	"other", "buffer-overflow", "invalid-apdu-in-this-state",
	"preempted-by-higher-priority-task", "segmentation-not-supported", "security-error",
	"insufficient-security", "window-size-out-of-range", "application-exceeded-reply-time",
	"out-of-resources", "tsm-timeout", "apdu-too-long",
}

// errorCodeNames is indexed by BACnetErrorCode. Empty entries are values the
// standard leaves unassigned (or removed).
var errorCodeNames = [...]string{
	0:   "other",
	2:   "configuration-in-progress",
	3:   "device-busy",
	4:   "dynamic-creation-not-supported",
	5:   "file-access-denied",
	7:   "inconsistent-parameters",
	8:   "inconsistent-selection-criterion",
	9:   "invalid-data-type",
	10:  "invalid-file-access-method",
	11:  "invalid-file-start-position",
	13:  "invalid-parameter-data-type",
	14:  "invalid-time-stamp",
	16:  "missing-required-parameter",
	17:  "no-objects-of-specified-type",
	18:  "no-space-for-object",
	19:  "no-space-to-add-list-element",
	20:  "no-space-to-write-property",
	21:  "no-vt-sessions-available",
	22:  "property-is-not-a-list",
	23:  "object-deletion-not-permitted",
	24:  "object-identifier-already-exists",
	25:  "operational-problem",
	26:  "password-failure",
	27:  "read-access-denied",
	29:  "service-request-denied",
	30:  "timeout",
	31:  "unknown-object",
	32:  "unknown-property",
	34:  "unknown-vt-class",
	35:  "unknown-vt-session",
	36:  "unsupported-object-type",
	37:  "value-out-of-range",
	38:  "vt-session-already-closed",
	39:  "vt-session-termination-failure",
	40:  "write-access-denied",
	41:  "character-set-not-supported",
	42:  "invalid-array-index",
	43:  "cov-subscription-failed",
	44:  "not-cov-property",
	45:  "optional-functionality-not-supported",
	46:  "invalid-configuration-data",
	47:  "datatype-not-supported",
	48:  "duplicate-name",
	49:  "duplicate-object-id",
	50:  "property-is-not-an-array",
	51:  "abort-buffer-overflow",
	52:  "abort-invalid-apdu-in-this-state",
	53:  "abort-preempted-by-higher-priority-task",
	54:  "abort-segmentation-not-supported",
	55:  "abort-proprietary",
	56:  "abort-other",
	57:  "invalid-tag",
	58:  "network-down",
	59:  "reject-buffer-overflow",
	60:  "reject-inconsistent-parameters",
	61:  "reject-invalid-parameter-data-type",
	62:  "reject-invalid-tag",
	63:  "reject-missing-required-parameter",
	64:  "reject-parameter-out-of-range",
	65:  "reject-too-many-arguments",
	66:  "reject-undefined-enumeration",
	67:  "reject-unrecognized-service",
	68:  "reject-proprietary",
	69:  "reject-other",
	70:  "unknown-device",
	71:  "unknown-route",
	72:  "value-not-initialized",
	73:  "invalid-event-state",
	74:  "no-alarm-configured",
	75:  "log-buffer-full",
	76:  "logged-value-purged",
	77:  "no-property-specified",
	78:  "not-configured-for-triggered-logging",
	79:  "unknown-subscription",
	80:  "parameter-out-of-range",
	81:  "list-element-not-found",
	82:  "busy",
	83:  "communication-disabled",
	84:  "success",
	85:  "access-denied",
	86:  "bad-destination-address",
	87:  "bad-destination-device-id",
	88:  "bad-signature",
	89:  "bad-source-address",
	90:  "bad-timestamp",
	91:  "cannot-use-key",
	92:  "cannot-verify-message-id",
	93:  "correct-key-revision",
	94:  "destination-device-id-required",
	95:  "duplicate-message",
	96:  "encryption-not-configured",
	97:  "encryption-required",
	98:  "incorrect-key",
	99:  "invalid-key-data",
	100: "key-update-in-progress",
	101: "malformed-message",
	102: "not-key-server",
	103: "security-not-configured",
	104: "source-security-required",
	105: "too-many-keys",
	106: "unknown-authentication-type",
	107: "unknown-key",
	108: "unknown-key-revision",
	109: "unknown-source-message",
	110: "not-router-to-dnet",
	111: "router-busy",
	112: "unknown-network-message",
	113: "message-too-long",
	114: "security-error",
	115: "addressing-error",
	116: "write-bdt-failed",
	117: "read-bdt-failed",
	118: "register-foreign-device-failed",
	119: "read-fdt-failed",
	120: "delete-fdt-entry-failed",
	121: "distribute-broadcast-failed",
	122: "unknown-file-size",
	123: "abort-apdu-too-long",
	124: "abort-application-exceeded-reply-time",
	125: "abort-out-of-resources",
	126: "abort-tsm-timeout",
	127: "abort-window-size-out-of-range",
	128: "file-full",
	129: "inconsistent-configuration",
	130: "inconsistent-object-type",
	131: "internal-error",
	132: "not-configured",
	133: "out-of-memory",
	134: "value-too-long",
	135: "abort-insufficient-security",
	136: "abort-security-error",
}

// ErrorClassName returns the standard name of a BACnet error class, or
// "class-N" for a value the standard does not define (classes ≥ 64 are
// vendor-proprietary).
func ErrorClassName(class uint16) string {
	if int(class) < len(errorClassNames) {
		return errorClassNames[class]
	}
	return "class-" + strconv.Itoa(int(class))
}

// ErrorCodeName returns the standard name of a BACnet error code, or "code-N"
// for a value this library has no name for (codes ≥ 256 are
// vendor-proprietary).
func ErrorCodeName(code uint16) string {
	if int(code) < len(errorCodeNames) && errorCodeNames[code] != "" {
		return errorCodeNames[code]
	}
	return "code-" + strconv.Itoa(int(code))
}

// RejectReasonName returns the standard name of a BACnet reject reason, or
// "reason-N" (reasons ≥ 64 are vendor-proprietary).
func RejectReasonName(reason uint8) string {
	if int(reason) < len(rejectReasonNames) {
		return rejectReasonNames[reason]
	}
	return "reason-" + strconv.Itoa(int(reason))
}

// AbortReasonName returns the standard name of a BACnet abort reason, or
// "reason-N" (reasons ≥ 64 are vendor-proprietary).
func AbortReasonName(reason uint8) string {
	if int(reason) < len(abortReasonNames) {
		return abortReasonNames[reason]
	}
	return "reason-" + strconv.Itoa(int(reason))
}
