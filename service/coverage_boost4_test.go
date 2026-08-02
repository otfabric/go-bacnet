// SPDX-License-Identifier: MIT

package service_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestCoverageBoostAuditTargetDeviceEncode(t *testing.T) {
	td := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 9}
	raw, err := service.EncodeAuditNotification(service.AuditNotification{
		SourceDevice: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		TargetDevice: &td,
		Operation:    3,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeAuditNotification(raw, bacnet.DefaultDecodeLimits())
	if err != nil || got.TargetDevice == nil || got.TargetDevice.Instance != 9 {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestCoverageBoostVTOpenEncode(t *testing.T) {
	raw, err := service.EncodeVTOpen(service.VTOpenRequest{VTClass: 2, LocalVTSessionIdentifier: 9})
	if err != nil || len(raw) < 2 {
		t.Fatal(err)
	}
}

func TestCoverageBoostAtomicWriteRecordEncode(t *testing.T) {
	raw, err := service.EncodeAtomicWriteFile(service.AtomicWriteFileRequest{
		File:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 2},
		Access: service.FileAccessRecord, StartPosition: 1, Records: [][]byte{[]byte("a"), []byte("b")},
	})
	if err != nil || len(raw) < 8 {
		t.Fatal(err)
	}
}

func TestCoverageBoostEnrollmentPriorityOnly(t *testing.T) {
	prio := [2]uint32{1, 2}
	raw, err := service.EncodeGetEnrollmentSummary(service.GetEnrollmentSummaryRequest{PriorityFilter: &prio})
	if err != nil || len(raw) < 4 {
		t.Fatal(err)
	}
}
