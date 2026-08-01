// SPDX-License-Identifier: MIT

package bacnet

import (
	"errors"
	"fmt"
)

// Local/runtime sentinels.
var (
	ErrClosed                 = errors.New("bacnet: client closed")
	ErrTimeout                = errors.New("bacnet: request timeout")
	ErrTransactionCapacity    = errors.New("bacnet: transaction capacity exhausted")
	ErrMalformed              = errors.New("bacnet: malformed encoding")
	ErrUnsupported            = errors.New("bacnet: unsupported")
	ErrResponseSourceMismatch = errors.New("bacnet: response source mismatch")
	ErrProtocolViolation      = errors.New("bacnet: protocol violation")
	ErrAPDUTooLarge           = errors.New("bacnet: APDU exceeds remote capability")
	ErrTrailingData           = errors.New("bacnet: unexpected trailing data")
	ErrLimitExceeded          = errors.New("bacnet: decode limit exceeded")
	ErrDeliveryDropped        = errors.New("bacnet: delivery dropped")
	// ErrDeviceManagementDisabled is returned when DeviceCommunicationControl
	// or ReinitializeDevice is called without WithDeviceManagementEnabled.
	ErrDeviceManagementDisabled = errors.New("bacnet: device management API disabled")
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
	return fmt.Sprintf("bacnet: error invoke=%d service=%d class=%d code=%d",
		e.InvokeID, e.Service, e.Class, e.Code)
}

// RejectError is a remote BACnet Reject PDU.
type RejectError struct {
	InvokeID uint8
	Reason   uint8
}

func (e *RejectError) Error() string {
	return fmt.Sprintf("bacnet: reject invoke=%d reason=%d", e.InvokeID, e.Reason)
}

// AbortError is a remote BACnet Abort PDU.
type AbortError struct {
	InvokeID uint8
	Server   bool
	Reason   uint8
}

func (e *AbortError) Error() string {
	return fmt.Sprintf("bacnet: abort invoke=%d server=%v reason=%d",
		e.InvokeID, e.Server, e.Reason)
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
