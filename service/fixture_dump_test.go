// SPDX-License-Identifier: MIT

package service_test

import (
	"encoding/hex"
	"os"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

// TestDumpCodecFixtureHex is an opt-in helper for regenerating codec hex vectors.
// Run with FIXTURE_DUMP=1.
func TestDumpCodecFixtureHex(t *testing.T) {
	if os.Getenv("FIXTURE_DUMP") == "" {
		t.Skip("set FIXTURE_DUMP=1 to emit codec hex vectors")
	}
	dump := func(name string, encode func() ([]byte, error)) {
		t.Helper()
		b, err := encode()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		t.Logf("%s %s", name, hex.EncodeToString(b))
	}
	dump("get-alarm-summary-req", service.EncodeGetAlarmSummary)
	dump("get-alarm-summary-ack", func() ([]byte, error) {
		return service.EncodeGetAlarmSummaryACK(service.GetAlarmSummaryACK{Entries: []service.AlarmSummaryEntry{{
			Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogInput, Instance: 1}, AlarmState: 1,
			AckedTransitions: bacnet.BitString{UnusedBits: 5, Bytes: []byte{0xe0}},
		}}})
	})
	es := uint32(1)
	dump("get-enrollment-summary-req", func() ([]byte, error) {
		return service.EncodeGetEnrollmentSummary(service.GetEnrollmentSummaryRequest{
			AcknowledgmentFilter: service.EnrollmentFilterAll, EventStateFilter: &es,
		})
	})
	dump("get-enrollment-summary-ack", func() ([]byte, error) {
		return service.EncodeGetEnrollmentSummaryACK(service.GetEnrollmentSummaryACK{Entries: []service.EnrollmentSummaryEntry{{
			Object:    bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
			EventType: 5, EventState: 1, Priority: 100, NotificationClass: 1,
		}}})
	})
	life := uint32(60)
	dump("subscribe-cov-property-multiple", func() ([]byte, error) {
		return service.EncodeSubscribeCOVPropertyMultiple(service.SubscribeCOVPropertyMultipleRequest{
			SubscriberProcessIdentifier: 1, IssueConfirmedNotifications: false, LifetimeRemaining: &life,
			Subscriptions: []service.COVMultipleSubscription{{
				Object:     bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
				Properties: []service.COVPropertyReference{{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}}},
			}},
		})
	})
	dump("cov-notification-multiple", func() ([]byte, error) {
		return service.EncodeCOVNotificationMultiple(service.COVNotificationMultiple{
			SubscriberProcessIdentifier: 1,
			InitiatingDevice:            bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1234},
			TimeRemaining:               60,
			Objects: []service.COVNotificationMultipleObject{{
				Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
				Values: []service.COVNotificationMultipleValue{{
					Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}, Value: bacnet.RealValue(21.5),
				}},
			}},
		})
	})
	dump("atomic-read-file-stream", func() ([]byte, error) {
		return service.EncodeAtomicReadFile(service.AtomicReadFileRequest{
			File: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 1}, Access: service.FileAccessStream, Count: 16,
		})
	})
	dump("atomic-read-file-ack-stream", func() ([]byte, error) {
		return service.EncodeAtomicReadFileACK(service.AtomicReadFileACK{
			EndOfFile: true, Access: service.FileAccessStream, Data: []byte("hello"),
		})
	})
	dump("atomic-write-file-stream", func() ([]byte, error) {
		return service.EncodeAtomicWriteFile(service.AtomicWriteFileRequest{
			File: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 1}, Access: service.FileAccessStream, Data: []byte("hello"),
		})
	})
	dump("atomic-write-file-ack", func() ([]byte, error) {
		return service.EncodeAtomicWriteFileACK(service.AtomicWriteFileACK{Access: service.FileAccessStream, StartPosition: 5})
	})
	dump("atomic-read-file-record", func() ([]byte, error) {
		return service.EncodeAtomicReadFile(service.AtomicReadFileRequest{
			File: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 2}, Access: service.FileAccessRecord, Count: 2,
		})
	})
	listReq := service.ListElementRequest{
		Object:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1234},
		Property: bacnet.PropertyReference{Identifier: 76},
		Elements: []bacnet.Element{{Value: bacnet.ObjectIDValue(bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1})}},
	}
	dump("add-list-element", func() ([]byte, error) { return service.EncodeListElementRequest(listReq) })
	id := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 101}
	dump("create-object", func() ([]byte, error) {
		return service.EncodeCreateObject(service.CreateObjectRequest{ObjectIdentifier: &id})
	})
	dump("create-object-ack", func() ([]byte, error) {
		return service.EncodeCreateObjectACK(service.CreateObjectACK{Object: id})
	})
	dump("delete-object", func() ([]byte, error) {
		return service.EncodeDeleteObject(service.DeleteObjectRequest{Object: id})
	})
	dump("confirmed-private-transfer", func() ([]byte, error) {
		return service.EncodePrivateTransfer(service.PrivateTransfer{VendorID: 999, ServiceNumber: 1})
	})
	dump("unconfirmed-text-message", func() ([]byte, error) {
		return service.EncodeTextMessage(service.TextMessage{
			TextMessageSourceDevice: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1234}, Message: "hello",
		})
	})
	dump("time-synchronization", func() ([]byte, error) {
		return service.EncodeTimeSynchronization(service.TimeSynchronization{
			Date: bacnet.Date{Year: 126, Month: 8, Day: 2, Weekday: 7}, Time: bacnet.Time{Hour: 12},
		})
	})
	dump("write-group", func() ([]byte, error) {
		return service.EncodeWriteGroup(service.WriteGroup{
			GroupNumber: 1, WritePriority: 8, ChangeList: []bacnet.Element{{Value: bacnet.UnsignedValue(1)}},
		})
	})
	dump("audit-log-query", func() ([]byte, error) {
		return service.EncodeAuditLogQuery(service.AuditLogQueryRequest{
			AuditLog: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAuditLog, Instance: 1},
		})
	})
	dump("who-am-i", func() ([]byte, error) {
		return service.EncodeWhoAmI(service.WhoAmI{VendorID: 999, ModelName: "m", SerialNumber: "s"})
	})
	dump("you-are", func() ([]byte, error) {
		return service.EncodeYouAre(service.YouAre{
			VendorID: 999, ModelName: "m", SerialNumber: "s",
			Device: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1234},
		})
	})
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeLifeSafetyPoint, Instance: 1}
	dump("life-safety-operation", func() ([]byte, error) {
		return service.EncodeLifeSafetyOperation(service.LifeSafetyOperationRequest{
			RequestingProcessIdentifier: 1, RequestingSource: "ops", Request: 1, Object: &obj,
		})
	})
	dump("vt-open", func() ([]byte, error) {
		return service.EncodeVTOpen(service.VTOpenRequest{VTClass: 1, LocalVTSessionIdentifier: 3})
	})
}
