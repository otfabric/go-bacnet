// SPDX-License-Identifier: MIT

package service_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestEventNotificationRoundTrip(t *testing.T) {
	ackReq := true
	from := uint32(0)
	msg := bacnet.CharacterString{Value: "hi"}
	note := service.EventNotification{
		ProcessIdentifier: 7,
		InitiatingDevice:  bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 100},
		EventObject:       bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
		TimeStamp:         service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 42},
		NotificationClass: 1,
		Priority:          3,
		EventType:         0, // change-of-state
		MessageText:       &msg,
		NotifyType:        0, // alarm
		AckRequired:       &ackReq,
		FromState:         &from,
		ToState:           1,
	}
	raw, err := service.EncodeEventNotification(note)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeEventNotification(raw, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if got.ProcessIdentifier != 7 || got.Priority != 3 || got.ToState != 1 {
		t.Fatalf("%+v", got)
	}
	if got.TimeStamp.Choice != service.TimeStampSequence || got.TimeStamp.Sequence != 42 {
		t.Fatalf("ts=%+v", got.TimeStamp)
	}
	if got.MessageText == nil || got.MessageText.Value != "hi" || got.AckRequired == nil || !*got.AckRequired {
		t.Fatalf("optional fields %+v", got)
	}
	re, err := service.EncodeEventNotification(got)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, re) {
		t.Fatalf("reencode mismatch\n%x\n%x", raw, re)
	}
}

func TestTimeStampChoices(t *testing.T) {
	cases := []service.TimeStamp{
		{Choice: service.TimeStampTime, Time: bacnet.Time{Hour: 12, Minute: 30, Second: 0, Hundredths: 0}},
		{Choice: service.TimeStampSequence, Sequence: 9},
		{Choice: service.TimeStampDateTime, DateTime: service.DateTime{
			Date: bacnet.Date{Year: 126, Month: 8, Day: 1, Weekday: 6},
			Time: bacnet.Time{Hour: 9, Minute: 0, Second: 0, Hundredths: 0},
		}},
	}
	for _, ts := range cases {
		raw, err := service.EncodeTimeStamp(ts)
		if err != nil {
			t.Fatal(err)
		}
		els, n, err := bacnet.ParseSequence(raw, bacnet.DefaultDecodeLimits(), -1)
		if err != nil || n != len(raw) || len(els) != 1 {
			t.Fatalf("parse: n=%d els=%d err=%v", n, len(els), err)
		}
		got, err := service.DecodeTimeStamp(els[0])
		if err != nil {
			t.Fatal(err)
		}
		if got.Choice != ts.Choice {
			t.Fatalf("choice %v != %v", got.Choice, ts.Choice)
		}
	}
}

func TestAcknowledgeAlarmRoundTrip(t *testing.T) {
	req := service.AcknowledgeAlarmRequest{
		ProcessIdentifier:    1,
		EventObject:          bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
		EventStateAcked:      1,
		TimeStamp:            service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 10},
		AcknowledgmentSource: bacnet.CharacterString{Value: "ops"},
		TimeOfAcknowledgment: service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 11},
	}
	raw, err := service.EncodeAcknowledgeAlarm(req)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeAcknowledgeAlarm(raw, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if got.ProcessIdentifier != 1 || got.AcknowledgmentSource.Value != "ops" {
		t.Fatalf("%+v", got)
	}
}

func TestGetEventInformationRoundTrip(t *testing.T) {
	empty := bacnet.ApplicationValue{Kind: bacnet.ValueConstructed}
	ack := service.GetEventInformationACK{
		Summaries: []service.EventSummary{{
			Object:                  bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
			EventState:              1,
			AcknowledgedTransitions: bacnet.BitString{UnusedBits: 5, Bytes: []byte{0xE0}},
			EventTimeStamps:         empty,
			NotifyType:              0,
			EventEnable:             bacnet.BitString{UnusedBits: 5, Bytes: []byte{0xE0}},
			EventPriorities:         empty,
		}},
		MoreEvents: false,
	}
	raw, err := service.EncodeGetEventInformationACK(ack)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeGetEventInformationACK(raw, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Summaries) != 1 || got.MoreEvents || got.Summaries[0].EventState != 1 {
		t.Fatalf("%+v", got)
	}
	reqRaw, err := service.EncodeGetEventInformation(service.GetEventInformationRequest{})
	if err != nil || len(reqRaw) != 0 {
		t.Fatalf("empty req: %v %x", err, reqRaw)
	}
	id := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 2}
	reqRaw, err = service.EncodeGetEventInformation(service.GetEventInformationRequest{LastReceived: &id})
	if err != nil {
		t.Fatal(err)
	}
	req, err := service.DecodeGetEventInformation(reqRaw, bacnet.DefaultDecodeLimits())
	if err != nil || req.LastReceived == nil || *req.LastReceived != id {
		t.Fatalf("%+v %v", req, err)
	}
}

func TestDeviceCommunicationControlRoundTrip(t *testing.T) {
	dur := uint16(5)
	pw := bacnet.CharacterString{Value: "secret"}
	req := service.DeviceCommunicationControlRequest{
		TimeDuration:  &dur,
		EnableDisable: service.EnableDisableDisable,
		Password:      &pw,
	}
	raw, err := service.EncodeDeviceCommunicationControl(req)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeDeviceCommunicationControl(raw, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if got.EnableDisable != service.EnableDisableDisable || got.TimeDuration == nil || *got.TimeDuration != 5 {
		t.Fatalf("%+v", got)
	}
	if _, err := service.EncodeDeviceCommunicationControl(service.DeviceCommunicationControlRequest{EnableDisable: 9}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("want malformed, got %v", err)
	}
}

func TestReinitializeDeviceRoundTrip(t *testing.T) {
	pw := bacnet.CharacterString{Value: "pw"}
	req := service.ReinitializeDeviceRequest{
		State:    service.ReinitializedWarmstart,
		Password: &pw,
	}
	raw, err := service.EncodeReinitializeDevice(req)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeReinitializeDevice(raw, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if got.State != service.ReinitializedWarmstart || got.Password == nil || got.Password.Value != "pw" {
		t.Fatalf("%+v", got)
	}
}
