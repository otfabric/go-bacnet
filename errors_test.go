// SPDX-License-Identifier: MIT

package bacnet_test

import (
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
)

func TestTypedErrorsUnwrap(t *testing.T) {
	apduErr := &bacnet.APDUTooLargeError{EncodedSize: 100, RemoteMax: 50, SegmentationSupported: false}
	if !errors.Is(apduErr, bacnet.ErrAPDUTooLarge) || apduErr.Error() == "" {
		t.Fatalf("%v", apduErr)
	}

	er := &bacnet.ErrorResponse{InvokeID: 1, Service: 12, Class: 2, Code: 0}
	if er.Error() == "" {
		t.Fatal("empty ErrorResponse")
	}
	rj := &bacnet.RejectError{InvokeID: 2, Reason: 1}
	if rj.Error() == "" {
		t.Fatal("empty RejectError")
	}
	ab := &bacnet.AbortError{InvokeID: 3, Server: true, Reason: 4}
	if ab.Error() == "" {
		t.Fatal("empty AbortError")
	}

	ou := &bacnet.OutcomeUnknownError{Operation: "WriteProperty"}
	if ou.Error() == "" || ou.Unwrap() != nil {
		t.Fatalf("%v unwrap=%v", ou, ou.Unwrap())
	}
	cause := errors.New("timeout")
	ou2 := &bacnet.OutcomeUnknownError{Operation: "WriteProperty", Cause: cause}
	if !errors.Is(ou2, cause) || ou2.Error() == "" {
		t.Fatalf("%v", ou2)
	}
}
