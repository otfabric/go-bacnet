// SPDX-License-Identifier: MIT

package service_test

import (
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestRequestDecodersRoundTrip(t *testing.T) {
	if err := service.DecodeGetAlarmSummary(nil, bacnet.DefaultDecodeLimits()); err != nil {
		t.Fatal(err)
	}
	if err := service.DecodeGetAlarmSummary([]byte{0x09}, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrTrailingData) && err == nil {
		t.Fatal("expected trailing")
	}

	es := uint32(1)
	et := uint32(5)
	pri := [2]uint32{1, 200}
	nc := uint32(1)
	enrollRaw, err := service.EncodeGetEnrollmentSummary(service.GetEnrollmentSummaryRequest{
		AcknowledgmentFilter:    service.EnrollmentFilterAll,
		EventStateFilter:        &es,
		EventTypeFilter:         &et,
		PriorityFilter:          &pri,
		NotificationClassFilter: &nc,
	})
	if err != nil {
		t.Fatal(err)
	}
	enroll, err := service.DecodeGetEnrollmentSummary(enrollRaw, bacnet.DefaultDecodeLimits())
	if err != nil || enroll.EventStateFilter == nil || *enroll.EventStateFilter != 1 ||
		enroll.EventTypeFilter == nil || enroll.PriorityFilter == nil || enroll.NotificationClassFilter == nil {
		t.Fatalf("%+v %v", enroll, err)
	}

	arfRaw, err := service.EncodeAtomicReadFile(service.AtomicReadFileRequest{
		File:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 1},
		Access: service.FileAccessStream, StartPosition: 0, Count: 16,
	})
	if err != nil {
		t.Fatal(err)
	}
	arf, err := service.DecodeAtomicReadFile(arfRaw, bacnet.DefaultDecodeLimits())
	if err != nil || arf.Count != 16 || arf.Access != service.FileAccessStream {
		t.Fatalf("%+v %v", arf, err)
	}
	arrRaw, err := service.EncodeAtomicReadFile(service.AtomicReadFileRequest{
		File:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 2},
		Access: service.FileAccessRecord, StartPosition: 1, Count: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	arr, err := service.DecodeAtomicReadFile(arrRaw, bacnet.DefaultDecodeLimits())
	if err != nil || arr.Access != service.FileAccessRecord || arr.StartPosition != 1 {
		t.Fatalf("%+v %v", arr, err)
	}

	awfRaw, err := service.EncodeAtomicWriteFile(service.AtomicWriteFileRequest{
		File:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 1},
		Access: service.FileAccessStream, Data: []byte("hello"),
	})
	if err != nil {
		t.Fatal(err)
	}
	awf, err := service.DecodeAtomicWriteFile(awfRaw, bacnet.DefaultDecodeLimits())
	if err != nil || string(awf.Data) != "hello" {
		t.Fatalf("%+v %v", awf, err)
	}
	awrRaw, err := service.EncodeAtomicWriteFile(service.AtomicWriteFileRequest{
		File:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 2},
		Access: service.FileAccessRecord, Records: [][]byte{[]byte("r1"), []byte("r2")},
	})
	if err != nil {
		t.Fatal(err)
	}
	awr, err := service.DecodeAtomicWriteFile(awrRaw, bacnet.DefaultDecodeLimits())
	if err != nil || len(awr.Records) != 2 {
		t.Fatalf("%+v %v", awr, err)
	}

	id := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 101}
	ot := bacnet.ObjectTypeAnalogValue
	coID, err := service.EncodeCreateObject(service.CreateObjectRequest{ObjectIdentifier: &id})
	if err != nil {
		t.Fatal(err)
	}
	co, err := service.DecodeCreateObject(coID, bacnet.DefaultDecodeLimits())
	if err != nil || co.ObjectIdentifier == nil || co.ObjectIdentifier.Instance != 101 {
		t.Fatalf("%+v %v", co, err)
	}
	coType, err := service.EncodeCreateObject(service.CreateObjectRequest{ObjectType: &ot})
	if err != nil {
		t.Fatal(err)
	}
	cot, err := service.DecodeCreateObject(coType, bacnet.DefaultDecodeLimits())
	if err != nil || cot.ObjectType == nil || *cot.ObjectType != ot {
		t.Fatalf("%+v %v", cot, err)
	}
	if _, err := service.DecodeCreateObject(nil, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("empty create")
	}

	idx := uint32(1)
	inc := float32(0.5)
	life := uint32(60)
	subRaw, err := service.EncodeSubscribeCOVPropertyMultiple(service.SubscribeCOVPropertyMultipleRequest{
		SubscriberProcessIdentifier: 1,
		IssueConfirmedNotifications: true,
		LifetimeRemaining:           &life,
		Subscriptions: []service.COVMultipleSubscription{{
			Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
			Properties: []service.COVPropertyReference{{
				Property:     bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue, ArrayIndex: &idx},
				COVIncrement: &inc,
				Timestamped:  true,
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := service.DecodeSubscribeCOVPropertyMultiple(subRaw, bacnet.DefaultDecodeLimits())
	if err != nil || len(sub.Subscriptions) != 1 ||
		len(sub.Subscriptions[0].Properties) == 0 ||
		sub.Subscriptions[0].Properties[0].Property.ArrayIndex == nil {
		t.Fatalf("%+v %v", sub, err)
	}

	alRaw, err := service.EncodeAuditLogQuery(service.AuditLogQueryRequest{
		AuditLog: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAuditLog, Instance: 1},
		Query:    []bacnet.Element{{Value: bacnet.UnsignedValue(1)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	al, err := service.DecodeAuditLogQuery(alRaw, bacnet.DefaultDecodeLimits())
	if err != nil || al.AuditLog.Instance != 1 || len(al.Query) != 1 {
		t.Fatalf("%+v %v", al, err)
	}

	vtRaw, err := service.EncodeVTOpen(service.VTOpenRequest{VTClass: 1, LocalVTSessionIdentifier: 3})
	if err != nil {
		t.Fatal(err)
	}
	vt, err := service.DecodeVTOpen(vtRaw, bacnet.DefaultDecodeLimits())
	if err != nil || vt.VTClass != 1 || vt.LocalVTSessionIdentifier != 3 {
		t.Fatalf("%+v %v", vt, err)
	}
	if _, err := service.DecodeVTOpen([]byte{0x91}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("truncated vt")
	}
	if _, err := service.DecodeAtomicReadFile(nil, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("empty arf")
	}
	if _, err := service.DecodeAtomicWriteFile([]byte{0xff}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("bad awf")
	}
	if _, err := service.DecodeSubscribeCOVPropertyMultiple([]byte{0xff}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("bad sub")
	}
	if _, err := service.DecodeGetEnrollmentSummary([]byte{0xff}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("bad enroll")
	}
	if _, err := service.DecodeAuditLogQuery([]byte{0xff}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("bad audit")
	}

	// Trailing / missing-field / wrong-tag negatives for decoder error paths.
	negatives := []struct {
		name string
		fn   func([]byte) error
		raw  []byte
	}{
		{"enroll-trailing", func(b []byte) error {
			_, err := service.DecodeGetEnrollmentSummary(b, bacnet.DefaultDecodeLimits())
			return err
		}, append(append([]byte{}, enrollRaw...), 0x00)},
		{"enroll-empty", func(b []byte) error {
			_, err := service.DecodeGetEnrollmentSummary(b, bacnet.DefaultDecodeLimits())
			return err
		}, nil},
		{"arf-trailing", func(b []byte) error {
			_, err := service.DecodeAtomicReadFile(b, bacnet.DefaultDecodeLimits())
			return err
		}, append(append([]byte{}, arfRaw...), 0x00)},
		{"arf-bad-tag", func(b []byte) error {
			_, err := service.DecodeAtomicReadFile(b, bacnet.DefaultDecodeLimits())
			return err
		}, []byte{0x3c, 0x02, 0x80, 0x00, 0x01}},
		{"awf-trailing", func(b []byte) error {
			_, err := service.DecodeAtomicWriteFile(b, bacnet.DefaultDecodeLimits())
			return err
		}, append(append([]byte{}, awfRaw...), 0x00)},
		{"awf-empty", func(b []byte) error {
			_, err := service.DecodeAtomicWriteFile(b, bacnet.DefaultDecodeLimits())
			return err
		}, nil},
		{"create-trailing", func(b []byte) error {
			_, err := service.DecodeCreateObject(b, bacnet.DefaultDecodeLimits())
			return err
		}, append(append([]byte{}, coID...), 0x00)},
		{"create-bad-tag", func(b []byte) error {
			_, err := service.DecodeCreateObject(b, bacnet.DefaultDecodeLimits())
			return err
		}, []byte{0x3c, 0x00, 0x80, 0x00, 0x01}},
		{"sub-trailing", func(b []byte) error {
			_, err := service.DecodeSubscribeCOVPropertyMultiple(b, bacnet.DefaultDecodeLimits())
			return err
		}, append(append([]byte{}, subRaw...), 0x00)},
		{"sub-empty", func(b []byte) error {
			_, err := service.DecodeSubscribeCOVPropertyMultiple(b, bacnet.DefaultDecodeLimits())
			return err
		}, nil},
		{"audit-trailing", func(b []byte) error {
			_, err := service.DecodeAuditLogQuery(b, bacnet.DefaultDecodeLimits())
			return err
		}, append(append([]byte{}, alRaw...), 0x00)},
		{"audit-empty", func(b []byte) error {
			_, err := service.DecodeAuditLogQuery(b, bacnet.DefaultDecodeLimits())
			return err
		}, nil},
		{"audit-bad-tag", func(b []byte) error {
			_, err := service.DecodeAuditLogQuery(b, bacnet.DefaultDecodeLimits())
			return err
		}, []byte{0x1c, 0x0f, 0x40, 0x00, 0x01}},
		{"vt-empty", func(b []byte) error { _, err := service.DecodeVTOpen(b, bacnet.DefaultDecodeLimits()); return err }, nil},
		{"vt-trailing", func(b []byte) error { _, err := service.DecodeVTOpen(b, bacnet.DefaultDecodeLimits()); return err }, append(append([]byte{}, vtRaw...), 0x00)},
		{"vt-bad-enum", func(b []byte) error { _, err := service.DecodeVTOpen(b, bacnet.DefaultDecodeLimits()); return err }, []byte{0x21, 0x01, 0x21, 0x03}},
		{"stream-params", func(b []byte) error {
			_, err := service.DecodeAtomicReadFile(b, bacnet.DefaultDecodeLimits())
			return err
		}, []byte{0x0c, 0x02, 0x80, 0x00, 0x01, 0x1e, 0x31, 0x00, 0x1f}},
		{"write-stream-bad", func(b []byte) error {
			_, err := service.DecodeAtomicWriteFile(b, bacnet.DefaultDecodeLimits())
			return err
		}, []byte{0x0c, 0x02, 0x80, 0x00, 0x01, 0x1e, 0x31, 0x00, 0x1f}},
		{"write-record-bad", func(b []byte) error {
			_, err := service.DecodeAtomicWriteFile(b, bacnet.DefaultDecodeLimits())
			return err
		}, []byte{0x0c, 0x02, 0x80, 0x00, 0x02, 0x2e, 0x21, 0x00, 0x2f}},
		{"sub-missing-refs", func(b []byte) error {
			_, err := service.DecodeSubscribeCOVPropertyMultiple(b, bacnet.DefaultDecodeLimits())
			return err
		}, []byte{0x09, 0x01, 0x19, 0x00, 0x3e, 0x0c, 0x00, 0x80, 0x00, 0x01, 0x3f}},
	}
	for _, tc := range negatives {
		if err := tc.fn(tc.raw); err == nil {
			t.Fatalf("%s: expected error", tc.name)
		}
	}

	// Additional parse / incomplete-structure edges.
	edges := [][]byte{
		{0xff},                   // AtomicReadFile parse
		{0x09, 0x01},             // SubscribeCOVPropertyMultiple incomplete
		{0x09, 0x01, 0x19, 0x01}, // missing subscriptions constructed
		{0x0c, 0x02, 0x80, 0x00, 0x01, 0x1e, 0x21, 0x00, 0x21, 0x10, 0x1f}, // stream start not signed
		{0x0c, 0x02, 0x80, 0x00, 0x01, 0x1e, 0x31, 0x00, 0x31, 0x01, 0x1f}, // stream data not octet
		{0x0c, 0x0f, 0x40, 0x00, 0x01, 0x1e, 0x21, 0x01, 0x1f},             // audit query ok path already; add bad second
	}
	for i, raw := range edges {
		if _, err := service.DecodeAtomicReadFile(raw, bacnet.DefaultDecodeLimits()); err == nil && i == 0 {
			t.Fatalf("edge %d arf", i)
		}
		if _, err := service.DecodeSubscribeCOVPropertyMultiple(raw, bacnet.DefaultDecodeLimits()); err == nil && (i == 1 || i == 2) {
			t.Fatalf("edge %d sub", i)
		}
		if _, err := service.DecodeAtomicWriteFile(raw, bacnet.DefaultDecodeLimits()); err == nil && (i == 3 || i == 4) {
			t.Fatalf("edge %d awf", i)
		}
	}
	if _, err := service.DecodeAtomicReadFile([]byte{0x0c, 0x02, 0x80, 0x00, 0x01, 0x1e, 0x21, 0x00, 0x21, 0x10, 0x1f}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("arf non-signed start")
	}
	if _, err := service.DecodeAtomicWriteFile([]byte{0x0c, 0x02, 0x80, 0x00, 0x01, 0x1e, 0x31, 0x00, 0x21, 0x01, 0x1f}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("awf non-octet data")
	}
	if _, err := service.DecodeVTOpen([]byte{0x10, 0x21, 0x03}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("vt null class")
	}
	if _, err := service.DecodeGetEnrollmentSummary([]byte{0x09, 0x00, 0x4e, 0x10, 0x10, 0x4f}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("enroll bad priority elements")
	}
}
