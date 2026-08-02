// SPDX-License-Identifier: MIT

package service_test

import (
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestTimeStampWrapperAndDateTimeMalformed(t *testing.T) {
	// empty TimeStamp wrapper in EventNotification
	raw, err := bacnet.AppendContextUnsigned(nil, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextObjectID(raw, 1, bacnet.ObjectIdentifier{Type: 8, Instance: 1})
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextObjectID(raw, 2, bacnet.ObjectIdentifier{Type: 2, Instance: 1})
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, 0x3E, 0x3F)
	if _, err := service.DecodeEventNotification(raw, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("empty ts wrapper: %v", err)
	}
	araw, err := bacnet.AppendContextUnsigned(nil, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	araw, err = bacnet.AppendContextObjectID(araw, 1, bacnet.ObjectIdentifier{Type: 2, Instance: 1})
	if err != nil {
		t.Fatal(err)
	}
	araw, err = bacnet.AppendContextUnsigned(araw, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	araw = append(araw, 0x3E, 0x3F)
	if _, err := service.DecodeAcknowledgeAlarm(araw, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("ack empty ts: %v", err)
	}
	badDT, err := bacnet.AppendContextTagged(nil, 2, []bacnet.Element{
		{Value: bacnet.UnsignedValue(1)},
		{Value: bacnet.UnsignedValue(2)},
	})
	if err != nil {
		t.Fatal(err)
	}
	els, _, err := bacnet.ParseSequence(badDT, bacnet.DefaultDecodeLimits(), -1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeTimeStamp(els[0]); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("bad datetime: %v", err)
	}
}

func TestDecodeTimeStampDateTimeAndErrors(t *testing.T) {
	ts := service.TimeStamp{
		Choice: service.TimeStampDateTime,
		DateTime: service.DateTime{
			Date: bacnet.Date{Year: 126, Month: 1, Day: 2, Weekday: 3},
			Time: bacnet.Time{Hour: 4, Minute: 5, Second: 6, Hundredths: 7},
		},
	}
	raw, err := service.EncodeTimeStamp(ts)
	if err != nil {
		t.Fatal(err)
	}
	els, _, err := bacnet.ParseSequence(raw, bacnet.DefaultDecodeLimits(), -1)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeTimeStamp(els[0])
	if err != nil || got.Choice != service.TimeStampDateTime || got.DateTime.Time.Hour != 4 {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := service.DecodeTimeStamp(bacnet.Element{TagNumber: 9, Context: true}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("%v", err)
	}
	bad, err := bacnet.AppendContextTagged(nil, 0, []bacnet.Element{
		{Value: bacnet.ApplicationValue{Kind: bacnet.ValueTime}},
	})
	if err != nil {
		t.Fatal(err)
	}
	bels, _, err := bacnet.ParseSequence(bad, bacnet.DefaultDecodeLimits(), -1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeTimeStamp(bels[0]); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("%v", err)
	}
}

func TestEventNotificationFromStateAckRoundTrip(t *testing.T) {
	ack := true
	from := uint32(2)
	msg := bacnet.CharacterString{Value: "n"}
	note := service.EventNotification{
		ProcessIdentifier: 3,
		InitiatingDevice:  bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 9},
		EventObject:       bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeBinaryValue, Instance: 1},
		TimeStamp: service.TimeStamp{
			Choice:   service.TimeStampDateTime,
			DateTime: service.DateTime{Date: bacnet.Date{Year: 126, Month: 8, Day: 1, Weekday: 6}, Time: bacnet.Time{Hour: 10}},
		},
		NotificationClass: 2,
		Priority:          200,
		EventType:         1,
		MessageText:       &msg,
		NotifyType:        2,
		AckRequired:       &ack,
		FromState:         &from,
		ToState:           3,
	}
	raw, err := service.EncodeEventNotification(note)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeEventNotification(raw, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if got.FromState == nil || *got.FromState != 2 || got.AckRequired == nil || !*got.AckRequired || got.Priority != 200 {
		t.Fatalf("%+v", got)
	}
	if got.MessageText == nil || got.MessageText.Value != "n" || got.TimeStamp.DateTime.Date.Month != 8 {
		t.Fatalf("message/timestamp %+v", got)
	}
	if _, err := service.DecodeEventNotification(append(append([]byte{}, raw...), 0xFF), bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected error for garbage suffix")
	}

	// fromState 0 is a valid optional value and must still round-trip.
	fromZero := uint32(0)
	note.FromState = &fromZero
	raw, err = service.EncodeEventNotification(note)
	if err != nil {
		t.Fatal(err)
	}
	got, err = service.DecodeEventNotification(raw, bacnet.DefaultDecodeLimits())
	if err != nil || got.FromState == nil || *got.FromState != 0 {
		t.Fatalf("fromState 0: %+v %v", got, err)
	}
}

func TestEventNotificationPriorityOverflow(t *testing.T) {
	raw, err := bacnet.AppendContextUnsigned(nil, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextObjectID(raw, 1, bacnet.ObjectIdentifier{Type: 8, Instance: 1})
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextObjectID(raw, 2, bacnet.ObjectIdentifier{Type: 2, Instance: 1})
	if err != nil {
		t.Fatal(err)
	}
	ts, err := service.EncodeTimeStamp(service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 1})
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, 0x3E)
	raw = append(raw, ts...)
	raw = append(raw, 0x3F)
	raw, err = bacnet.AppendContextUnsigned(raw, 4, 1)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextUnsigned(raw, 5, 300)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeEventNotification(raw, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("%v", err)
	}
}

func TestAcknowledgeAlarmTimeStampVariants(t *testing.T) {
	req := service.AcknowledgeAlarmRequest{
		ProcessIdentifier:    2,
		EventObject:          bacnet.ObjectIdentifier{Type: 2, Instance: 3},
		EventStateAcked:      2,
		TimeStamp:            service.TimeStamp{Choice: service.TimeStampTime, Time: bacnet.Time{Hour: 1, Minute: 2, Second: 3, Hundredths: 4}},
		AcknowledgmentSource: bacnet.CharacterString{Value: "a"},
		TimeOfAcknowledgment: service.TimeStamp{Choice: service.TimeStampDateTime, DateTime: service.DateTime{
			Date: bacnet.Date{Year: 126, Month: 1, Day: 1, Weekday: 1},
			Time: bacnet.Time{Hour: 2},
		}},
	}
	raw, err := service.EncodeAcknowledgeAlarm(req)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeAcknowledgeAlarm(raw, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if got.TimeStamp.Choice != service.TimeStampTime || got.TimeOfAcknowledgment.Choice != service.TimeStampDateTime {
		t.Fatalf("%+v", got)
	}
	if _, err := service.DecodeAcknowledgeAlarm(append(append([]byte{}, raw...), 0xFF), bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected error for garbage suffix")
	}
}

func TestGetEventInformationEmptyAndErrors(t *testing.T) {
	req, err := service.DecodeGetEventInformation(nil, bacnet.DefaultDecodeLimits())
	if err != nil || req.LastReceived != nil {
		t.Fatalf("%+v %v", req, err)
	}
	id := bacnet.ObjectIdentifier{Type: 2, Instance: 1}
	raw, err := service.EncodeGetEventInformation(service.GetEventInformationRequest{LastReceived: &id})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeGetEventInformation(append(append([]byte{}, raw...), 0xFF), bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected error for garbage suffix")
	}
	empty := bacnet.ApplicationValue{Kind: bacnet.ValueConstructed}
	if _, err := service.EncodeGetEventInformationACK(service.GetEventInformationACK{
		Summaries: []service.EventSummary{{
			Object: id, EventTimeStamps: empty, EventPriorities: bacnet.ApplicationValue{Kind: bacnet.ValueUnsigned},
		}},
	}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("%v", err)
	}
	ackRaw, err := service.EncodeGetEventInformationACK(service.GetEventInformationACK{
		Summaries: []service.EventSummary{{
			Object: id, EventState: 0,
			AcknowledgedTransitions: bacnet.BitString{UnusedBits: 5, Bytes: []byte{0}},
			EventTimeStamps:         empty, NotifyType: 0,
			EventEnable: bacnet.BitString{UnusedBits: 5, Bytes: []byte{0}}, EventPriorities: empty,
		}},
		MoreEvents: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeGetEventInformationACK(append(append([]byte{}, ackRaw...), 0xFF), bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected error for garbage suffix")
	}
}

func TestDCCReinitDurationPasswordBranches(t *testing.T) {
	dur := uint16(60)
	raw, err := service.EncodeDeviceCommunicationControl(service.DeviceCommunicationControlRequest{
		TimeDuration:  &dur,
		EnableDisable: service.EnableDisableDisable,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeDeviceCommunicationControl(raw, bacnet.DefaultDecodeLimits())
	if err != nil || got.TimeDuration == nil || *got.TimeDuration != 60 {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := service.DecodeDeviceCommunicationControl(append(append([]byte{}, raw...), 0xFF), bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected error for garbage suffix")
	}
	big, err := bacnet.AppendContextUnsigned(nil, 0, 0x10000)
	if err != nil {
		t.Fatal(err)
	}
	big, err = bacnet.AppendContextUnsigned(big, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeDeviceCommunicationControl(big, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("%v", err)
	}
	bad, err := bacnet.AppendContextUnsigned(nil, 1, 5)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeDeviceCommunicationControl(bad, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("%v", err)
	}
	pw := bacnet.CharacterString{Value: "abc"}
	rraw, err := service.EncodeReinitializeDevice(service.ReinitializeDeviceRequest{
		State: service.ReinitializedStartBackup, Password: &pw,
	})
	if err != nil {
		t.Fatal(err)
	}
	rgot, err := service.DecodeReinitializeDevice(rraw, bacnet.DefaultDecodeLimits())
	if err != nil || rgot.Password == nil || rgot.State != service.ReinitializedStartBackup {
		t.Fatalf("%+v %v", rgot, err)
	}
	if _, err := service.DecodeReinitializeDevice(append(append([]byte{}, rraw...), 0xFF), bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected error for garbage suffix")
	}
	ostate, err := bacnet.AppendContextUnsigned(nil, 0, 8)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeReinitializeDevice(ostate, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("%v", err)
	}
}
