// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
)

func TestConfirmedServicePolicyCompleteness(t *testing.T) {
	required := []uint8{
		apdu.ServiceAcknowledgeAlarm,
		apdu.ServiceGetAlarmSummary,
		apdu.ServiceGetEnrollmentSummary,
		apdu.ServiceSubscribeCOV,
		apdu.ServiceAtomicReadFile,
		apdu.ServiceAtomicWriteFile,
		apdu.ServiceAddListElement,
		apdu.ServiceRemoveListElement,
		apdu.ServiceCreateObject,
		apdu.ServiceDeleteObject,
		apdu.ServiceReadProperty,
		apdu.ServiceReadPropertyMultiple,
		apdu.ServiceWriteProperty,
		apdu.ServiceWritePropertyMultiple,
		apdu.ServiceDeviceCommunicationControl,
		apdu.ServiceConfirmedPrivateTransfer,
		apdu.ServiceConfirmedTextMessage,
		apdu.ServiceReinitializeDevice,
		apdu.ServiceVTOpen,
		apdu.ServiceVTClose,
		apdu.ServiceVTData,
		apdu.ServiceReadRange,
		apdu.ServiceLifeSafetyOperation,
		apdu.ServiceSubscribeCOVProperty,
		apdu.ServiceGetEventInformation,
		apdu.ServiceSubscribeCOVPropertyMultiple,
		apdu.ServiceAuditLogQuery,
		apdu.ServiceAuthRequest,
	}
	for _, choice := range required {
		p, ok := ConfirmedServicePolicyFor(choice)
		if !ok {
			t.Fatalf("missing policy for service %d", choice)
		}
		if p.Name == "" {
			t.Fatalf("empty policy name for service %d", choice)
		}
		caps, ok := ConfirmedServiceCapabilitiesFor(choice)
		if !ok {
			t.Fatalf("missing capabilities for service %d", choice)
		}
		_ = caps
		if DefaultRetransmitPolicy(choice) == RetransmitEnabled && !p.RetransmitSafe {
			t.Fatalf("retransmit mismatch for %s", p.Name)
		}
		if DefaultRetransmitPolicy(choice) == RetransmitDisabled && p.RetransmitSafe {
			t.Fatalf("retransmit mismatch for %s", p.Name)
		}
	}
	got := TypedConfirmedServices()
	if len(got) != len(required) {
		sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
		t.Fatalf("registry size=%d want %d: %v", len(got), len(required), got)
	}
}

func TestWrapOutcomeUnknownUsesPolicy(t *testing.T) {
	cause := errors.New("cause")
	if err := wrapOutcomeUnknown(apdu.ServiceReadProperty, cause); err != cause {
		t.Fatalf("read must not wrap: %v", err)
	}
	var unknown *bacnet.OutcomeUnknownError
	err := wrapOutcomeUnknown(apdu.ServiceCreateObject, cause)
	if !errors.As(err, &unknown) || unknown.Operation != "CreateObject" {
		t.Fatalf("create must wrap: %#v", err)
	}
	err = wrapOutcomeUnknown(apdu.ServiceAtomicWriteFile, cause)
	if !errors.As(err, &unknown) || unknown.Operation != "AtomicWriteFile" {
		t.Fatalf("atomic write file must wrap: %#v", err)
	}
}

func TestRequireServicePolicyAndOperationClass(t *testing.T) {
	if _, err := requireServicePolicy(0xff); err == nil {
		t.Fatal("expected missing policy")
	}
	env := newVirtualPair(t)
	if err := env.Client.checkOperationClass(apdu.ServiceDeviceCommunicationControl); !errors.Is(err, bacnet.ErrDeviceManagementDisabled) {
		t.Fatalf("got %v", err)
	}
	confirmedPolicies[0xfe] = ConfirmedServicePolicy{Name: "tmp-net", OperationClass: OperationNetworkManagement}
	confirmedPolicies[0xfd] = ConfirmedServicePolicy{Name: "tmp-other", OperationClass: OperationClass(99)}
	defer func() {
		delete(confirmedPolicies, 0xfe)
		delete(confirmedPolicies, 0xfd)
	}()
	if err := env.Client.checkOperationClass(0xfe); !errors.Is(err, ErrNetworkManagementDisabled) {
		t.Fatalf("network: %v", err)
	}
	if err := env.Client.checkOperationClass(0xfd); err == nil {
		t.Fatal("expected unknown class disabled")
	}
}

func TestMaybeUnknownForceDefaultName(t *testing.T) {
	opts := confirmedOpts{forceOutcomeUnknown: true}
	err := opts.maybeUnknown(apdu.ServiceReadProperty, errors.New("x"), true)
	var unknown *bacnet.OutcomeUnknownError
	if !errors.As(err, &unknown) || unknown.Operation != "InvokeConfirmed" {
		t.Fatalf("%#v", err)
	}
	if opts.maybeUnknown(apdu.ServiceReadProperty, errors.New("x"), false).Error() != "x" {
		t.Fatal("unsent must not wrap")
	}
	opts.outcomeUnknownName = "Custom"
	err = opts.maybeUnknown(apdu.ServiceReadProperty, errors.New("x"), true)
	if !errors.As(err, &unknown) || unknown.Operation != "Custom" {
		t.Fatalf("%#v", err)
	}
}

func TestInvokeConfirmedOptions(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	_, err := env.Client.InvokeConfirmed(ctx, env.Target, apdu.ServiceReadProperty, nil, ConfirmedInvokeOptions{})
	if err == nil {
		t.Fatal("expected error")
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel2()
	_, err = env.Client.InvokeConfirmed(ctx2, env.Target, apdu.ServiceReadProperty, nil, ConfirmedInvokeOptions{
		Retransmit:    RetransmitEnabled,
		SideEffecting: true,
	})
	if err == nil {
		t.Fatal("expected error with retransmit")
	}
	if err := env.Client.checkOperationClass(0xff); err == nil {
		t.Fatal("missing policy via checkOperationClass")
	}
}
