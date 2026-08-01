//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/service"
)

// BACnet Error class/code and Reject/Abort reasons used by peer assertions.
const (
	errorClassProperty        uint16 = 2
	errorCodeUnknownProperty  uint16 = 32
	rejectUnrecognizedService uint8  = 9
	// Proprietary / unused confirmed service choice — peers should Reject.
	unrecognizedConfirmedService uint8 = 99
	// Non-existent standard property identifier for Error probes.
	unknownPropertyID bacnet.PropertyIdentifier = 4194302
)

func deviceObject(dev deviceFixture) bacnet.ObjectIdentifier {
	return bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: dev.DeviceInstance}
}

func analogValueObject(dev deviceFixture) (bacnet.ObjectIdentifier, float32) {
	inst := uint32(1)
	var pv float32 = 21.5
	for _, o := range dev.Objects {
		if o.Type == "analog-value" {
			inst = o.Instance
			if f, ok := o.PresentValue.(float64); ok {
				pv = float32(f)
			}
			break
		}
	}
	return bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: inst}, pv
}

func trendLogObject(dev deviceFixture) (bacnet.ObjectIdentifier, bool) {
	for _, o := range dev.Objects {
		if o.Type == "trend-log" {
			return bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeTrendLog, Instance: o.Instance}, true
		}
	}
	return bacnet.ObjectIdentifier{}, false
}

func assertErrorUnknownProperty(t *testing.T, err error) {
	t.Helper()
	var er *bacnet.ErrorResponse
	if !errors.As(err, &er) {
		t.Fatalf("want *ErrorResponse, got %T: %v", err, err)
	}
	if er.Class != errorClassProperty || er.Code != errorCodeUnknownProperty {
		t.Fatalf("Error class/code=%d/%d, want property/unknown-property (%d/%d)",
			er.Class, er.Code, errorClassProperty, errorCodeUnknownProperty)
	}
	if er.Service != 0 && er.Service != apdu.ServiceReadProperty {
		t.Fatalf("Error service=%d, want ReadProperty (%d)", er.Service, apdu.ServiceReadProperty)
	}
}

func assertRejectUnrecognized(t *testing.T, err error) {
	t.Helper()
	var rr *bacnet.RejectError
	if !errors.As(err, &rr) {
		t.Fatalf("want *RejectError, got %T: %v", err, err)
	}
	if rr.Reason != rejectUnrecognizedService {
		t.Fatalf("Reject reason=%d, want unrecognized-service (%d)", rr.Reason, rejectUnrecognizedService)
	}
}

func waitCOVNotification(t *testing.T, sub client.Subscription, timeout time.Duration) *service.COVNotification {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for COV notification")
		case ev, ok := <-sub.Events():
			if !ok {
				t.Fatal("subscription closed before COV notification")
			}
			if ev.Notification != nil {
				return ev.Notification
			}
			if ev.Err != nil && ev.State != client.SubscriptionActive {
				t.Fatalf("subscription event error before notify: state=%v err=%v", ev.State, ev.Err)
			}
		}
	}
}

func withTimeout(t *testing.T, d time.Duration) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), d)
}
