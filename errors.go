// SPDX-License-Identifier: MIT

package bacnet

import (
	"errors"
	"fmt"
)

// Local/runtime sentinels.
var (
	ErrClosed  = errors.New("bacnet: client closed")
	ErrTimeout = errors.New("bacnet: request timeout")
	// Deprecated: ErrTransactionCapacity is never returned. A request that
	// finds all invoke IDs in use waits for one until its context ends.
	ErrTransactionCapacity = errors.New("bacnet: transaction capacity exhausted")
	ErrMalformed           = errors.New("bacnet: malformed encoding")
	ErrUnsupported         = errors.New("bacnet: unsupported")
	// Deprecated: ErrResponseSourceMismatch is never returned. A response
	// from an unexpected source is dropped (diagnostic "wrong_source") and
	// the request runs into its timeout.
	ErrResponseSourceMismatch = errors.New("bacnet: response source mismatch")
	ErrProtocolViolation      = errors.New("bacnet: protocol violation")
	ErrAPDUTooLarge           = errors.New("bacnet: APDU exceeds remote capability")
	ErrTrailingData           = errors.New("bacnet: unexpected trailing data")
	ErrLimitExceeded          = errors.New("bacnet: decode limit exceeded")
	// Deprecated: ErrDeliveryDropped is never returned. Dropped notifications
	// are signalled by SubscriptionEvent.Gap and EventStreamEvent.Gap.
	ErrDeliveryDropped = errors.New("bacnet: delivery dropped")
	// ErrDeviceManagementDisabled is returned when DeviceCommunicationControl
	// or ReinitializeDevice is called without WithDeviceManagementEnabled.
	ErrDeviceManagementDisabled = errors.New("bacnet: device management API disabled")
	ErrBusy                     = errors.New("bacnet: operation already in progress")
	// ErrInvalidConfig is returned by client.New when an option carries a
	// value the client cannot run with.
	ErrInvalidConfig = errors.New("bacnet: invalid configuration")
)

// APDUTooLargeError reports a request that would exceed the remote max APDU
// without permitted segmentation.
type APDUTooLargeError struct {
	EncodedSize           int
	RemoteMax             int
	SegmentationSupported bool
}

func (e *APDUTooLargeError) Error() string {
	return fmt.Sprintf("%v: encoded=%d remote_max=%d segmentation_supported=%v",
		ErrAPDUTooLarge, e.EncodedSize, e.RemoteMax, e.SegmentationSupported)
}

func (e *APDUTooLargeError) Unwrap() error { return ErrAPDUTooLarge }

// ErrorResponse is a remote BACnet Error PDU.
type ErrorResponse struct {
	InvokeID uint8
	Service  uint8
	Class    uint16
	Code     uint16
}

func (e *ErrorResponse) Error() string {
	return fmt.Sprintf("bacnet: error invoke=%d service=%d class=%s(%d) code=%s(%d)",
		e.InvokeID, e.Service, ErrorClassName(e.Class), e.Class, ErrorCodeName(e.Code), e.Code)
}

// RejectError is a remote BACnet Reject PDU.
type RejectError struct {
	InvokeID uint8
	Reason   uint8
}

func (e *RejectError) Error() string {
	return fmt.Sprintf("bacnet: reject invoke=%d reason=%s(%d)", e.InvokeID, RejectReasonName(e.Reason), e.Reason)
}

// AbortError is a remote BACnet Abort PDU.
type AbortError struct {
	InvokeID uint8
	Server   bool
	Reason   uint8
}

func (e *AbortError) Error() string {
	return fmt.Sprintf("bacnet: abort invoke=%d server=%v reason=%s(%d)",
		e.InvokeID, e.Server, AbortReasonName(e.Reason), e.Reason)
}

// OutcomeUnknownError indicates a request may have been executed remotely but
// no definitive response was observed (typical after a write send without ACK).
type OutcomeUnknownError struct {
	Operation string
	Cause     error
}

func (e *OutcomeUnknownError) Error() string {
	if e.Cause == nil {
		return fmt.Sprintf("bacnet: outcome unknown for %s", e.Operation)
	}
	return fmt.Sprintf("bacnet: outcome unknown for %s: %v", e.Operation, e.Cause)
}

func (e *OutcomeUnknownError) Unwrap() error { return e.Cause }
