// SPDX-License-Identifier: MIT

package service_test

import (
	"encoding/hex"
	"os"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

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

func TestWhoAmIYouAreRoundTrip(t *testing.T) {
	who := service.WhoAmI{VendorID: 999, ModelName: "InteropModel", SerialNumber: "SN-001"}
	raw, err := service.EncodeWhoAmI(who)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeWhoAmI(raw, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if got != who {
		t.Fatalf("got %+v want %+v", got, who)
	}

	dev := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1234}
	you := service.YouAre{
		VendorID: 999, ModelName: "InteropModel", SerialNumber: "SN-001", Device: dev,
	}
	yraw, err := service.EncodeYouAre(you)
	if err != nil {
		t.Fatal(err)
	}
	ygot, err := service.DecodeYouAre(yraw, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if ygot.VendorID != you.VendorID || ygot.ModelName != you.ModelName ||
		ygot.SerialNumber != you.SerialNumber || ygot.Device != you.Device {
		t.Fatalf("got %+v want %+v", ygot, you)
	}
}

func TestEnrollmentSummaryACKRoundTrip(t *testing.T) {
	ack := service.GetEnrollmentSummaryACK{
		Entries: []service.EnrollmentSummaryEntry{
			{
				Object:            bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
				EventType:         5,
				EventState:        1,
				Priority:          100,
				NotificationClass: 1,
			},
		},
	}
	raw, err := service.EncodeGetEnrollmentSummaryACK(ack)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeGetEnrollmentSummaryACK(raw, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 1 || got.Entries[0].NotificationClass != 1 {
		t.Fatalf("%+v", got)
	}
}

func TestAlarmSummaryACKRoundTrip(t *testing.T) {
	ack := service.GetAlarmSummaryACK{
		Entries: []service.AlarmSummaryEntry{
			{
				Object:     bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogInput, Instance: 1},
				AlarmState: 1,
				AckedTransitions: bacnet.BitString{
					UnusedBits: 5,
					Bytes:      []byte{0xe0},
				},
			},
		},
	}
	raw, err := service.EncodeGetAlarmSummaryACK(ack)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeGetAlarmSummaryACK(raw, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 1 || got.Entries[0].Object.Instance != 1 {
		t.Fatalf("%+v", got)
	}
}

func TestCOVNotificationMultipleRoundTrip(t *testing.T) {
	n := service.COVNotificationMultiple{
		SubscriberProcessIdentifier: 7,
		InitiatingDevice:            bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1234},
		TimeRemaining:               60,
		Objects: []service.COVNotificationMultipleObject{
			{
				Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
				Values: []service.COVNotificationMultipleValue{
					{
						Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
						Value:    bacnet.RealValue(21.5),
					},
				},
			},
		},
	}
	raw, err := service.EncodeCOVNotificationMultiple(n)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeCOVNotificationMultiple(raw, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if got.SubscriberProcessIdentifier != 7 || len(got.Objects) != 1 {
		t.Fatalf("%+v", got)
	}
}

func TestOutOfRangeNotificationEncodeDecode(t *testing.T) {
	flags := bacnet.BitString{UnusedBits: 4, Bytes: []byte{0x90}}
	params := service.NotificationParameters{
		OutOfRange: &service.OutOfRangeParams{
			ExceedingValue: 90, StatusFlags: flags, Deadband: 0, ExceededLimit: 80,
		},
	}
	els, err := service.EncodeNotificationParameters(params)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeNotificationParameters(els)
	if err != nil {
		t.Fatal(err)
	}
	if got.OutOfRange == nil || got.OutOfRange.ExceedingValue != 90 {
		t.Fatalf("%+v", got)
	}
}

func TestChangeOfBitstringAndValueNotification(t *testing.T) {
	flags := bacnet.BitString{UnusedBits: 4, Bytes: []byte{0x10}}
	bits := bacnet.BitString{UnusedBits: 0, Bytes: []byte{0xff}}
	cases := []service.NotificationParameters{
		{ChangeOfBitstring: &service.ChangeOfBitstringParams{
			ReferencedBitstring: bits, StatusFlags: flags,
		}},
		{ChangeOfValue: &service.ChangeOfValueParams{
			NewValue: bacnet.ApplicationValue{
				Kind: bacnet.ValueConstructed,
				Elements: []bacnet.Element{{
					Value: bacnet.RealValue(3.5),
				}},
			},
			StatusFlags: flags,
		}},
		{ChangeOfState: &service.ChangeOfStateParams{
			NewState: service.PropertyStates{
				Choice: 0,
				Value:  bacnet.BoolValue(true),
			},
			StatusFlags: flags,
		}},
		{ChangeOfState: &service.ChangeOfStateParams{
			NewState: service.PropertyStates{
				Choice: 1,
				Value:  bacnet.EnumValue(2),
			},
			StatusFlags: flags,
		}},
		{ChangeOfState: &service.ChangeOfStateParams{
			NewState: service.PropertyStates{
				Choice: 2,
				Value:  bacnet.UnsignedValue(7),
			},
			StatusFlags: flags,
		}},
	}
	for i, params := range cases {
		els, err := service.EncodeNotificationParameters(params)
		if err != nil {
			t.Fatalf("%d encode: %v", i, err)
		}
		got, err := service.DecodeNotificationParameters(els)
		if err != nil {
			t.Fatalf("%d decode: %v", i, err)
		}
		switch {
		case params.ChangeOfBitstring != nil && got.ChangeOfBitstring == nil:
			t.Fatalf("%d bitstring", i)
		case params.ChangeOfValue != nil && got.ChangeOfValue == nil:
			t.Fatalf("%d value", i)
		case params.ChangeOfState != nil && got.ChangeOfState == nil:
			t.Fatalf("%d state", i)
		}
	}
}

func TestWriteGroupAndTimeSyncEncode(t *testing.T) {
	raw, err := service.EncodeWriteGroup(service.WriteGroup{
		GroupNumber:   1,
		WritePriority: 8,
		ChangeList:    []bacnet.Element{{Value: bacnet.RealValue(1.0)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 {
		t.Fatal("empty")
	}
	ts, err := service.EncodeTimeSynchronization(service.TimeSynchronization{
		Date: bacnet.Date{Year: 126, Month: 8, Day: 2, Weekday: 7},
		Time: bacnet.Time{Hour: 12, Minute: 0, Second: 0, Hundredths: 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) == 0 {
		t.Fatal("empty time sync")
	}
}

func TestIdentityDecodeMalformed(t *testing.T) {
	if _, err := service.DecodeWhoAmI([]byte{0xff}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected WhoAmI error")
	}
	if _, err := service.DecodeYouAre([]byte{0xff}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected YouAre error")
	}
}

func TestEnrollmentSummaryRequestFilters(t *testing.T) {
	es := uint32(1)
	et := uint32(5)
	pri := [2]uint32{1, 200}
	nc := uint32(1)
	raw, err := service.EncodeGetEnrollmentSummary(service.GetEnrollmentSummaryRequest{
		AcknowledgmentFilter:    service.EnrollmentFilterAll,
		EventStateFilter:        &es,
		EventTypeFilter:         &et,
		PriorityFilter:          &pri,
		NotificationClassFilter: &nc,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 10 {
		t.Fatalf("short payload %x", raw)
	}
}

func TestLifeSafetyAndVTRoundTrip(t *testing.T) {
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeLifeSafetyPoint, Instance: 1}
	req := service.LifeSafetyOperationRequest{
		RequestingProcessIdentifier: 1,
		RequestingSource:            "interop",
		Request:                     1,
		Object:                      &obj,
	}
	raw, err := service.EncodeLifeSafetyOperation(req)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeLifeSafetyOperation(raw, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if got.Object == nil || *got.Object != obj {
		t.Fatalf("%+v", got)
	}

	vraw, err := service.EncodeVTData(service.VTDataRequest{
		VTSessionIdentifier: 3,
		VTNewData:           []byte("vt"),
		VTDataFlag:          1,
	})
	if err != nil {
		t.Fatal(err)
	}
	vgot, err := service.DecodeVTData(vraw, bacnet.DefaultDecodeLimits())
	if err != nil || string(vgot.VTNewData) != "vt" {
		t.Fatalf("%+v %v", vgot, err)
	}
}

func TestSubscribeCOVPropertyMultipleOptionalFields(t *testing.T) {
	idx := uint32(2)
	inc := float32(0.5)
	life := uint32(120)
	raw, err := service.EncodeSubscribeCOVPropertyMultiple(service.SubscribeCOVPropertyMultipleRequest{
		SubscriberProcessIdentifier: 3,
		IssueConfirmedNotifications: true,
		LifetimeRemaining:           &life,
		Subscriptions: []service.COVMultipleSubscription{{
			Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
			Properties: []service.COVPropertyReference{{
				Property: bacnet.PropertyReference{
					Identifier: bacnet.PropertyPresentValue,
					ArrayIndex: &idx,
				},
				COVIncrement: &inc,
				Timestamped:  true,
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 20 {
		t.Fatalf("short %x", raw)
	}
	inhibit := true
	wraw, err := service.EncodeWriteGroup(service.WriteGroup{
		GroupNumber:   2,
		WritePriority: 8,
		ChangeList:    []bacnet.Element{{Value: bacnet.UnsignedValue(1)}},
		InhibitDelay:  &inhibit,
	})
	if err != nil || len(wraw) == 0 {
		t.Fatalf("%v %x", err, wraw)
	}
	fraw, err := service.EncodeAtomicWriteFile(service.AtomicWriteFileRequest{
		File:          bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 2},
		Access:        service.FileAccessRecord,
		StartPosition: 0,
		Records:       [][]byte{[]byte("record-0001")},
	})
	if err != nil || len(fraw) == 0 {
		t.Fatalf("%v %x", err, fraw)
	}
}

func TestAuthRequestEmptyAndRoundTrip(t *testing.T) {
	empty, err := service.EncodeAuthRequest(service.AuthRequest{})
	if err != nil || empty != nil {
		t.Fatalf("%v %v", empty, err)
	}
	raw, err := service.EncodeAuthRequest(service.AuthRequest{
		Parameters: []bacnet.Element{{Value: bacnet.UnsignedValue(9)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeAuthRequest(raw, bacnet.DefaultDecodeLimits())
	if err != nil || len(got.Parameters) == 0 {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := service.DecodeAuthRequest([]byte{0x0e, 0xff}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected trailing/malformed")
	}
}
