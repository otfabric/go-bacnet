// SPDX-License-Identifier: MIT

package client

import (
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

// TestAlarmServicesSimpleACK checks that a SimpleACK to a service whose
// answer is a ComplexACK (clauses 13.10-13.12) is a protocol violation.
func TestAlarmServicesSimpleACK(t *testing.T) {
	env := newVirtualPair(t)
	ctx := t.Context()
	go serveSimpleACK(ctx, env.PeerTr, env.Local)
	if _, err := env.Client.GetAlarmSummary(ctx, env.Target); !errors.Is(err, bacnet.ErrProtocolViolation) {
		t.Fatalf("GetAlarmSummary: %v", err)
	}
	if _, err := env.Client.GetEnrollmentSummary(ctx, env.Target, service.GetEnrollmentSummaryRequest{}); !errors.Is(err, bacnet.ErrProtocolViolation) {
		t.Fatalf("GetEnrollmentSummary: %v", err)
	}
	if _, err := env.Client.GetEventInformation(ctx, env.Target, service.GetEventInformationRequest{}); !errors.Is(err, bacnet.ErrProtocolViolation) {
		t.Fatalf("GetEventInformation: %v", err)
	}
}

// TestRequestEncodeErrorsNotSent checks that a request that cannot be
// encoded fails before anything is sent.
func TestRequestEncodeErrorsNotSent(t *testing.T) {
	env := newVirtualPair(t)
	ctx := t.Context()
	badObject := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: bacnet.MaxObjectInstance + 1}
	badParams := []bacnet.Element{{Value: bacnet.ApplicationValue{Kind: bacnet.ValueKind(200)}}}
	errs := map[string]error{}
	_, errs["GetEventInformation"] = env.Client.GetEventInformation(ctx, env.Target, service.GetEventInformationRequest{LastReceived: &badObject})
	_, errs["GetEnrollmentSummary"] = env.Client.GetEnrollmentSummary(ctx, env.Target, service.GetEnrollmentSummaryRequest{PriorityFilter: &[2]uint32{0, 256}})
	errs["ConfirmedPrivateTransfer"] = env.Client.ConfirmedPrivateTransfer(ctx, env.Target, service.PrivateTransfer{VendorID: 1, ServiceParameters: badParams})
	errs["UnconfirmedPrivateTransfer"] = env.Client.UnconfirmedPrivateTransfer(ctx, env.Peer, false, service.PrivateTransfer{VendorID: 1, ServiceParameters: badParams})
	errs["ConfirmedTextMessage"] = env.Client.ConfirmedTextMessage(ctx, env.Target, service.TextMessage{TextMessageSourceDevice: badObject, Message: "x"})
	errs["UnconfirmedTextMessage"] = env.Client.UnconfirmedTextMessage(ctx, env.Peer, true, service.TextMessage{TextMessageSourceDevice: badObject, Message: "x"})
	for name, err := range errs {
		if err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if n := len(env.ClientTr.Outbox()); n != 0 {
		t.Fatalf("%d packets sent", n)
	}
}
