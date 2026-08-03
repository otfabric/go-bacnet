// SPDX-License-Identifier: MIT

package client

import (
	"fmt"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
)

// SuccessResponseKind is the successful confirmed-service response form.
// Error, Reject, and Abort remain universally valid terminal responses.
type SuccessResponseKind uint8

const (
	SuccessSimpleACK SuccessResponseKind = iota
	SuccessComplexACK
)

// OperationClass groups sensitive confirmed operations for opt-in.
type OperationClass uint8

const (
	OperationNormal OperationClass = iota
	OperationWrite
	OperationDeviceManagement
	OperationNetworkManagement
	OperationLifeSafety
)

// ConfirmedServicePolicy is the execution-safety policy for a confirmed service.
// Segmentation legality lives in ConfirmedServiceCapabilities.
type ConfirmedServicePolicy struct {
	Name                    string
	RetransmitSafe          bool
	SideEffecting           bool
	OutcomeUnknownAfterSend bool
	OperationClass          OperationClass
	SuccessResponse         SuccessResponseKind
}

// ConfirmedServiceCapabilities describes legal segmentation for a service.
// Actual segmentation still depends on peer MaxAPDU, peer support, and config.
type ConfirmedServiceCapabilities struct {
	RequestMaySegment  bool
	ResponseMaySegment bool
}

var confirmedPolicies = map[uint8]ConfirmedServicePolicy{
	apdu.ServiceAcknowledgeAlarm: {
		Name: "AcknowledgeAlarm", RetransmitSafe: false, SideEffecting: true,
		OutcomeUnknownAfterSend: true, OperationClass: OperationWrite, SuccessResponse: SuccessSimpleACK,
	},
	apdu.ServiceGetAlarmSummary: {
		Name: "GetAlarmSummary", RetransmitSafe: true, SideEffecting: false,
		OperationClass: OperationNormal, SuccessResponse: SuccessComplexACK,
	},
	apdu.ServiceGetEnrollmentSummary: {
		Name: "GetEnrollmentSummary", RetransmitSafe: true, SideEffecting: false,
		OperationClass: OperationNormal, SuccessResponse: SuccessComplexACK,
	},
	apdu.ServiceSubscribeCOV: {
		Name: "SubscribeCOV", RetransmitSafe: false, SideEffecting: true,
		OutcomeUnknownAfterSend: true, OperationClass: OperationWrite, SuccessResponse: SuccessSimpleACK,
	},
	apdu.ServiceAtomicReadFile: {
		Name: "AtomicReadFile", RetransmitSafe: true, SideEffecting: false,
		OperationClass: OperationNormal, SuccessResponse: SuccessComplexACK,
	},
	apdu.ServiceAtomicWriteFile: {
		Name: "AtomicWriteFile", RetransmitSafe: false, SideEffecting: true,
		OutcomeUnknownAfterSend: true, OperationClass: OperationWrite, SuccessResponse: SuccessComplexACK,
	},
	apdu.ServiceAddListElement: {
		Name: "AddListElement", RetransmitSafe: false, SideEffecting: true,
		OutcomeUnknownAfterSend: true, OperationClass: OperationWrite, SuccessResponse: SuccessSimpleACK,
	},
	apdu.ServiceRemoveListElement: {
		Name: "RemoveListElement", RetransmitSafe: false, SideEffecting: true,
		OutcomeUnknownAfterSend: true, OperationClass: OperationWrite, SuccessResponse: SuccessSimpleACK,
	},
	apdu.ServiceCreateObject: {
		Name: "CreateObject", RetransmitSafe: false, SideEffecting: true,
		OutcomeUnknownAfterSend: true, OperationClass: OperationWrite, SuccessResponse: SuccessComplexACK,
	},
	apdu.ServiceDeleteObject: {
		Name: "DeleteObject", RetransmitSafe: false, SideEffecting: true,
		OutcomeUnknownAfterSend: true, OperationClass: OperationWrite, SuccessResponse: SuccessSimpleACK,
	},
	apdu.ServiceReadProperty: {
		Name: "ReadProperty", RetransmitSafe: true, SideEffecting: false,
		OperationClass: OperationNormal, SuccessResponse: SuccessComplexACK,
	},
	apdu.ServiceReadPropertyMultiple: {
		Name: "ReadPropertyMultiple", RetransmitSafe: true, SideEffecting: false,
		OperationClass: OperationNormal, SuccessResponse: SuccessComplexACK,
	},
	apdu.ServiceWriteProperty: {
		Name: "WriteProperty", RetransmitSafe: false, SideEffecting: true,
		OutcomeUnknownAfterSend: true, OperationClass: OperationWrite, SuccessResponse: SuccessSimpleACK,
	},
	apdu.ServiceWritePropertyMultiple: {
		Name: "WritePropertyMultiple", RetransmitSafe: false, SideEffecting: true,
		OutcomeUnknownAfterSend: true, OperationClass: OperationWrite, SuccessResponse: SuccessSimpleACK,
	},
	apdu.ServiceDeviceCommunicationControl: {
		Name: "DeviceCommunicationControl", RetransmitSafe: false, SideEffecting: true,
		OutcomeUnknownAfterSend: true, OperationClass: OperationDeviceManagement, SuccessResponse: SuccessSimpleACK,
	},
	apdu.ServiceConfirmedPrivateTransfer: {
		Name: "ConfirmedPrivateTransfer", RetransmitSafe: false, SideEffecting: true,
		OutcomeUnknownAfterSend: true, OperationClass: OperationWrite, SuccessResponse: SuccessComplexACK,
	},
	apdu.ServiceConfirmedTextMessage: {
		Name: "ConfirmedTextMessage", RetransmitSafe: false, SideEffecting: true,
		OutcomeUnknownAfterSend: true, OperationClass: OperationWrite, SuccessResponse: SuccessSimpleACK,
	},
	apdu.ServiceReinitializeDevice: {
		Name: "ReinitializeDevice", RetransmitSafe: false, SideEffecting: true,
		OutcomeUnknownAfterSend: true, OperationClass: OperationDeviceManagement, SuccessResponse: SuccessSimpleACK,
	},
	apdu.ServiceVTOpen: {
		Name: "VTOpen", RetransmitSafe: false, SideEffecting: true,
		OutcomeUnknownAfterSend: true, OperationClass: OperationWrite, SuccessResponse: SuccessComplexACK,
	},
	apdu.ServiceVTClose: {
		Name: "VTClose", RetransmitSafe: false, SideEffecting: true,
		OutcomeUnknownAfterSend: true, OperationClass: OperationWrite, SuccessResponse: SuccessSimpleACK,
	},
	apdu.ServiceVTData: {
		Name: "VTData", RetransmitSafe: false, SideEffecting: true,
		OutcomeUnknownAfterSend: true, OperationClass: OperationWrite, SuccessResponse: SuccessSimpleACK,
	},
	apdu.ServiceReadRange: {
		Name: "ReadRange", RetransmitSafe: true, SideEffecting: false,
		OperationClass: OperationNormal, SuccessResponse: SuccessComplexACK,
	},
	apdu.ServiceLifeSafetyOperation: {
		Name: "LifeSafetyOperation", RetransmitSafe: false, SideEffecting: true,
		OutcomeUnknownAfterSend: true, OperationClass: OperationLifeSafety, SuccessResponse: SuccessSimpleACK,
	},
	apdu.ServiceSubscribeCOVProperty: {
		Name: "SubscribeCOVProperty", RetransmitSafe: false, SideEffecting: true,
		OutcomeUnknownAfterSend: true, OperationClass: OperationWrite, SuccessResponse: SuccessSimpleACK,
	},
	apdu.ServiceGetEventInformation: {
		Name: "GetEventInformation", RetransmitSafe: true, SideEffecting: false,
		OperationClass: OperationNormal, SuccessResponse: SuccessComplexACK,
	},
	apdu.ServiceSubscribeCOVPropertyMultiple: {
		Name: "SubscribeCOVPropertyMultiple", RetransmitSafe: false, SideEffecting: true,
		OutcomeUnknownAfterSend: true, OperationClass: OperationWrite, SuccessResponse: SuccessSimpleACK,
	},
	apdu.ServiceAuditLogQuery: {
		Name: "AuditLogQuery", RetransmitSafe: true, SideEffecting: false,
		OperationClass: OperationNormal, SuccessResponse: SuccessComplexACK,
	},
	apdu.ServiceAuthRequest: {
		Name: "AuthRequest", RetransmitSafe: false, SideEffecting: true,
		OutcomeUnknownAfterSend: true, OperationClass: OperationWrite, SuccessResponse: SuccessComplexACK,
	},
}

var confirmedCapabilities = map[uint8]ConfirmedServiceCapabilities{
	apdu.ServiceAcknowledgeAlarm:             {RequestMaySegment: false, ResponseMaySegment: false},
	apdu.ServiceGetAlarmSummary:              {RequestMaySegment: false, ResponseMaySegment: true},
	apdu.ServiceGetEnrollmentSummary:         {RequestMaySegment: false, ResponseMaySegment: true},
	apdu.ServiceSubscribeCOV:                 {RequestMaySegment: false, ResponseMaySegment: false},
	apdu.ServiceAtomicReadFile:               {RequestMaySegment: true, ResponseMaySegment: true},
	apdu.ServiceAtomicWriteFile:              {RequestMaySegment: true, ResponseMaySegment: false},
	apdu.ServiceAddListElement:               {RequestMaySegment: true, ResponseMaySegment: false},
	apdu.ServiceRemoveListElement:            {RequestMaySegment: true, ResponseMaySegment: false},
	apdu.ServiceCreateObject:                 {RequestMaySegment: true, ResponseMaySegment: false},
	apdu.ServiceDeleteObject:                 {RequestMaySegment: false, ResponseMaySegment: false},
	apdu.ServiceReadProperty:                 {RequestMaySegment: false, ResponseMaySegment: true},
	apdu.ServiceReadPropertyMultiple:         {RequestMaySegment: true, ResponseMaySegment: true},
	apdu.ServiceWriteProperty:                {RequestMaySegment: true, ResponseMaySegment: false},
	apdu.ServiceWritePropertyMultiple:        {RequestMaySegment: true, ResponseMaySegment: false},
	apdu.ServiceDeviceCommunicationControl:   {RequestMaySegment: false, ResponseMaySegment: false},
	apdu.ServiceConfirmedPrivateTransfer:     {RequestMaySegment: true, ResponseMaySegment: true},
	apdu.ServiceConfirmedTextMessage:         {RequestMaySegment: true, ResponseMaySegment: false},
	apdu.ServiceReinitializeDevice:           {RequestMaySegment: false, ResponseMaySegment: false},
	apdu.ServiceVTOpen:                       {RequestMaySegment: false, ResponseMaySegment: false},
	apdu.ServiceVTClose:                      {RequestMaySegment: false, ResponseMaySegment: false},
	apdu.ServiceVTData:                       {RequestMaySegment: true, ResponseMaySegment: false},
	apdu.ServiceReadRange:                    {RequestMaySegment: false, ResponseMaySegment: true},
	apdu.ServiceLifeSafetyOperation:          {RequestMaySegment: false, ResponseMaySegment: false},
	apdu.ServiceSubscribeCOVProperty:         {RequestMaySegment: false, ResponseMaySegment: false},
	apdu.ServiceGetEventInformation:          {RequestMaySegment: false, ResponseMaySegment: true},
	apdu.ServiceSubscribeCOVPropertyMultiple: {RequestMaySegment: true, ResponseMaySegment: false},
	apdu.ServiceAuditLogQuery:                {RequestMaySegment: false, ResponseMaySegment: true},
	apdu.ServiceAuthRequest:                  {RequestMaySegment: true, ResponseMaySegment: true},
}

// ConfirmedServicePolicyFor returns the execution policy for a service choice.
func ConfirmedServicePolicyFor(serviceChoice uint8) (ConfirmedServicePolicy, bool) {
	p, ok := confirmedPolicies[serviceChoice]
	return p, ok
}

// ConfirmedServiceCapabilitiesFor returns segmentation legality for a service.
func ConfirmedServiceCapabilitiesFor(serviceChoice uint8) (ConfirmedServiceCapabilities, bool) {
	c, ok := confirmedCapabilities[serviceChoice]
	return c, ok
}

// ErrMissingServicePolicy is returned when a typed helper lacks a registry entry.
var ErrMissingServicePolicy = fmt.Errorf("%w: missing confirmed service policy", bacnet.ErrUnsupported)

func requireServicePolicy(serviceChoice uint8) (ConfirmedServicePolicy, error) {
	p, ok := ConfirmedServicePolicyFor(serviceChoice)
	if !ok {
		return ConfirmedServicePolicy{}, fmt.Errorf("%w: service %d", ErrMissingServicePolicy, serviceChoice)
	}
	return p, nil
}

// TypedConfirmedServices lists every confirmed service choice used by typed client helpers.
func TypedConfirmedServices() []uint8 {
	out := make([]uint8, 0, len(confirmedPolicies))
	for choice := range confirmedPolicies {
		out = append(out, choice)
	}
	return out
}

func (c *Client) operationClassEnabled(class OperationClass) bool {
	switch class {
	case OperationNormal, OperationWrite:
		return true
	case OperationDeviceManagement:
		return c != nil && c.cfg.deviceManagementEnabled
	case OperationNetworkManagement:
		return c != nil && c.cfg.networkManagementEnabled
	case OperationLifeSafety:
		// LifeSafety remains enabled by default for supervisory clients; opt-out
		// is not modeled. Dedicated opt-in can be added later without breaking
		// the OperationClass taxonomy.
		return true
	default:
		return false
	}
}

func (c *Client) checkOperationClass(serviceChoice uint8) error {
	p, err := requireServicePolicy(serviceChoice)
	if err != nil {
		return err
	}
	if !c.operationClassEnabled(p.OperationClass) {
		switch p.OperationClass {
		case OperationDeviceManagement:
			return bacnet.ErrDeviceManagementDisabled
		case OperationNetworkManagement:
			return ErrNetworkManagementDisabled
		default:
			return fmt.Errorf("%w: operation class %d disabled", bacnet.ErrUnsupported, p.OperationClass)
		}
	}
	return nil
}

// ErrNetworkManagementDisabled is returned when Write-BDT (and similar) run
// without WithNetworkManagementEnabled.
var ErrNetworkManagementDisabled = fmt.Errorf("%w: network management disabled", bacnet.ErrUnsupported)
