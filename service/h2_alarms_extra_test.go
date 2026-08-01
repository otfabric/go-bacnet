// SPDX-License-Identifier: MIT

package service_test

import (
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestEventNotificationOptionalParamsRoundTrip(t *testing.T) {
	params := bacnet.ApplicationValue{
		Kind: bacnet.ValueConstructed,
		Elements: []bacnet.Element{
			{Context: true, TagNumber: 0, Value: bacnet.ApplicationValue{Kind: bacnet.ValueOctetString, OctetString: []byte{1}}},
		},
	}
	note := service.EventNotification{
		ProcessIdentifier:  2,
		InitiatingDevice:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		EventObject:        bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 2},
		TimeStamp:          service.TimeStamp{Choice: service.TimeStampTime, Time: bacnet.Time{Hour: 8, Minute: 0, Second: 0, Hundredths: 0}},
		NotificationClass:  9,
		Priority:           1,
		EventType:          2,
		NotifyType:         1,
		ToState:            0,
		NotificationParams: &params,
	}
	raw, err := service.EncodeEventNotification(note)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeEventNotification(raw, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if got.NotificationParams == nil || got.NotificationParams.Kind != bacnet.ValueConstructed {
		t.Fatalf("params %+v", got.NotificationParams)
	}
	if got.TimeStamp.Choice != service.TimeStampTime || got.TimeStamp.Time.Hour != 8 {
		t.Fatalf("ts %+v", got.TimeStamp)
	}
}

func TestEventNotificationMalformed(t *testing.T) {
	if _, err := service.DecodeEventNotification(nil, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("empty: %v", err)
	}
	// processIdentifier only
	raw, err := bacnet.AppendContextUnsigned(nil, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeEventNotification(raw, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("incomplete: %v", err)
	}
	if _, err := service.EncodeTimeStamp(service.TimeStamp{Choice: 99}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("bad choice: %v", err)
	}
	badParams := service.EventNotification{
		ProcessIdentifier:  1,
		InitiatingDevice:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		EventObject:        bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
		TimeStamp:          service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 1},
		NotificationClass:  1,
		Priority:           1,
		NotifyType:         0,
		ToState:            0,
		NotificationParams: &bacnet.ApplicationValue{Kind: bacnet.ValueReal, Real: 1},
	}
	if _, err := service.EncodeEventNotification(badParams); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("bad params: %v", err)
	}
}

func TestAcknowledgeAlarmMalformed(t *testing.T) {
	if _, err := service.DecodeAcknowledgeAlarm(nil, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("%v", err)
	}
	raw, err := bacnet.AppendContextUnsigned(nil, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeAcknowledgeAlarm(raw, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("%v", err)
	}
}

func TestGetEventInformationMalformed(t *testing.T) {
	if _, err := service.DecodeGetEventInformation([]byte{0x19, 0x01}, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("%v", err)
	}
	if _, err := service.DecodeGetEventInformationACK(nil, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("%v", err)
	}
	if _, err := service.EncodeGetEventInformationACK(service.GetEventInformationACK{
		Summaries: []service.EventSummary{{
			Object:          bacnet.ObjectIdentifier{Type: 2, Instance: 1},
			EventTimeStamps: bacnet.ApplicationValue{Kind: bacnet.ValueReal},
			EventPriorities: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed},
		}},
	}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("%v", err)
	}
	// two summaries
	empty := bacnet.ApplicationValue{Kind: bacnet.ValueConstructed}
	ack := service.GetEventInformationACK{
		Summaries: []service.EventSummary{
			{
				Object: bacnet.ObjectIdentifier{Type: 2, Instance: 1}, EventState: 0,
				AcknowledgedTransitions: bacnet.BitString{UnusedBits: 5, Bytes: []byte{0}},
				EventTimeStamps:         empty, NotifyType: 0,
				EventEnable: bacnet.BitString{UnusedBits: 5, Bytes: []byte{0}}, EventPriorities: empty,
			},
			{
				Object: bacnet.ObjectIdentifier{Type: 2, Instance: 2}, EventState: 1,
				AcknowledgedTransitions: bacnet.BitString{UnusedBits: 5, Bytes: []byte{0}},
				EventTimeStamps:         empty, NotifyType: 0,
				EventEnable: bacnet.BitString{UnusedBits: 5, Bytes: []byte{0}}, EventPriorities: empty,
			},
		},
		MoreEvents: true,
	}
	raw, err := service.EncodeGetEventInformationACK(ack)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeGetEventInformationACK(raw, bacnet.DefaultDecodeLimits())
	if err != nil || len(got.Summaries) != 2 || !got.MoreEvents {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestDeviceManagementMalformed(t *testing.T) {
	long := bacnet.CharacterString{Value: string(make([]byte, 21))}
	if _, err := service.EncodeDeviceCommunicationControl(service.DeviceCommunicationControlRequest{
		EnableDisable: service.EnableDisableEnable,
		Password:      &long,
	}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("%v", err)
	}
	if _, err := service.EncodeReinitializeDevice(service.ReinitializeDeviceRequest{
		State:    99,
		Password: &long,
	}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("%v", err)
	}
	if _, err := service.DecodeDeviceCommunicationControl(nil, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("%v", err)
	}
	if _, err := service.DecodeReinitializeDevice(nil, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("%v", err)
	}
	// enable only + password
	pw := bacnet.CharacterString{Value: "x"}
	raw, err := service.EncodeDeviceCommunicationControl(service.DeviceCommunicationControlRequest{
		EnableDisable: service.EnableDisableDisableInitiation,
		Password:      &pw,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeDeviceCommunicationControl(raw, bacnet.DefaultDecodeLimits())
	if err != nil || got.EnableDisable != service.EnableDisableDisableInitiation || got.Password == nil {
		t.Fatalf("%+v %v", got, err)
	}
	raw, err = service.EncodeReinitializeDevice(service.ReinitializeDeviceRequest{State: service.ReinitializedActivateChanges})
	if err != nil {
		t.Fatal(err)
	}
	gotR, err := service.DecodeReinitializeDevice(raw, bacnet.DefaultDecodeLimits())
	if err != nil || gotR.State != service.ReinitializedActivateChanges {
		t.Fatalf("%+v %v", gotR, err)
	}
}

func TestEventNotificationDecodeFieldErrors(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	dev := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}

	base := func(t *testing.T) []byte {
		t.Helper()
		raw, err := bacnet.AppendContextUnsigned(nil, 0, 1)
		if err != nil {
			t.Fatal(err)
		}
		raw, err = bacnet.AppendContextObjectID(raw, 1, dev)
		if err != nil {
			t.Fatal(err)
		}
		raw, err = bacnet.AppendContextObjectID(raw, 2, obj)
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
		return raw
	}

	cases := []struct {
		name  string
		build func(t *testing.T) []byte
	}{
		{
			name: "processIdentifier overflow",
			build: func(t *testing.T) []byte {
				raw, err := bacnet.AppendContextUnsigned(nil, 0, 1<<32)
				if err != nil {
					t.Fatal(err)
				}
				return raw
			},
		},
		{
			name: "processIdentifier kind",
			build: func(t *testing.T) []byte {
				raw, err := bacnet.AppendContextBool(nil, 0, true)
				if err != nil {
					t.Fatal(err)
				}
				return raw
			},
		},
		{
			name: "notificationClass overflow",
			build: func(t *testing.T) []byte {
				raw := base(t)
				raw, err := bacnet.AppendContextUnsigned(raw, 4, 1<<32)
				if err != nil {
					t.Fatal(err)
				}
				return raw
			},
		},
		{
			name: "notificationClass empty",
			build: func(t *testing.T) []byte {
				return append(base(t), 0x48) // context tag 4, length 0
			},
		},
		{
			name: "priority empty",
			build: func(t *testing.T) []byte {
				raw := base(t)
				raw, err := bacnet.AppendContextUnsigned(raw, 4, 1)
				if err != nil {
					t.Fatal(err)
				}
				return append(raw, 0x58) // context tag 5, length 0
			},
		},
		{
			name: "eventType empty",
			build: func(t *testing.T) []byte {
				raw := base(t)
				raw, err := bacnet.AppendContextUnsigned(raw, 4, 1)
				if err != nil {
					t.Fatal(err)
				}
				raw, err = bacnet.AppendContextUnsigned(raw, 5, 1)
				if err != nil {
					t.Fatal(err)
				}
				return append(raw, 0x68) // context tag 6, length 0
			},
		},
		{
			name: "messageText kind",
			build: func(t *testing.T) []byte {
				raw := base(t)
				for _, tag := range []uint8{4, 5, 6} {
					var err error
					raw, err = bacnet.AppendContextUnsigned(raw, tag, 1)
					if err != nil {
						t.Fatal(err)
					}
				}
				raw, err := bacnet.AppendContextUnsigned(raw, 7, 1)
				if err != nil {
					t.Fatal(err)
				}
				return raw
			},
		},
		{
			name: "notifyType empty",
			build: func(t *testing.T) []byte {
				raw := base(t)
				for _, tag := range []uint8{4, 5, 6} {
					var err error
					raw, err = bacnet.AppendContextUnsigned(raw, tag, 1)
					if err != nil {
						t.Fatal(err)
					}
				}
				return append(raw, 0x88) // context tag 8, length 0
			},
		},
		{
			name: "ackRequired bad value",
			build: func(t *testing.T) []byte {
				raw := base(t)
				for _, tag := range []uint8{4, 5, 6, 8} {
					var err error
					raw, err = bacnet.AppendContextUnsigned(raw, tag, 1)
					if err != nil {
						t.Fatal(err)
					}
				}
				raw, err := bacnet.AppendContextUnsigned(raw, 9, 2) // not a boolean 0/1
				if err != nil {
					t.Fatal(err)
				}
				return raw
			},
		},
		{
			name: "fromState empty",
			build: func(t *testing.T) []byte {
				raw := base(t)
				for _, tag := range []uint8{4, 5, 6, 8} {
					var err error
					raw, err = bacnet.AppendContextUnsigned(raw, tag, 1)
					if err != nil {
						t.Fatal(err)
					}
				}
				return append(raw, 0xA8) // context tag 10, length 0
			},
		},
		{
			name: "toState empty",
			build: func(t *testing.T) []byte {
				raw := base(t)
				for _, tag := range []uint8{4, 5, 6, 8} {
					var err error
					raw, err = bacnet.AppendContextUnsigned(raw, tag, 1)
					if err != nil {
						t.Fatal(err)
					}
				}
				return append(raw, 0xB8) // context tag 11, length 0
			},
		},
		{
			name: "unexpected tag",
			build: func(t *testing.T) []byte {
				return []byte{0xD9, 0x01}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := service.DecodeEventNotification(tc.build(t), limits)
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}

	// TimeStamp sequence overflow and ContextTime/ContextUnsigned failures.
	ovSeq, err := bacnet.AppendContextUnsigned(nil, 1, 1<<32)
	if err != nil {
		t.Fatal(err)
	}
	els, _, err := bacnet.ParseSequence(ovSeq, limits, -1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeTimeStamp(els[0]); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("seq overflow: %v", err)
	}
	badTime := bacnet.Element{Context: true, TagNumber: 0, Value: bacnet.UnsignedValue(1)}
	if _, err := service.DecodeTimeStamp(badTime); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("bad time: %v", err)
	}
	badSeq := bacnet.Element{Context: true, TagNumber: 1, Value: bacnet.BoolValue(true)}
	if _, err := service.DecodeTimeStamp(badSeq); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("bad seq: %v", err)
	}
}

func TestAcknowledgeAlarmDecodeFieldErrors(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	obj := bacnet.ObjectIdentifier{Type: 2, Instance: 1}
	req := service.AcknowledgeAlarmRequest{
		ProcessIdentifier:    1,
		EventObject:          obj,
		EventStateAcked:      1,
		TimeStamp:            service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 1},
		AcknowledgmentSource: bacnet.CharacterString{Value: "op"},
		TimeOfAcknowledgment: service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 2},
	}
	raw, err := service.EncodeAcknowledgeAlarm(req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeAcknowledgeAlarm(append(append([]byte(nil), raw...), 0x69, 0x01), limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("extra tag: %v", err)
	}
	if _, err := service.DecodeAcknowledgeAlarm([]byte{0x09, 0x01}, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("unexpected: %v", err)
	}
	// truncated PID context (wrong kind)
	if _, err := service.DecodeAcknowledgeAlarm([]byte{0x08}, limits); err == nil {
		t.Fatal("expected malformed")
	}
	// missing fields after PID/object/state
	partial, err := bacnet.AppendContextUnsigned(nil, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	partial, err = bacnet.AppendContextObjectID(partial, 1, obj)
	if err != nil {
		t.Fatal(err)
	}
	partial, err = bacnet.AppendContextUnsigned(partial, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeAcknowledgeAlarm(partial, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("missing: %v", err)
	}
	// bad state kind
	badState, err := bacnet.AppendContextUnsigned(nil, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	badState, err = bacnet.AppendContextObjectID(badState, 1, obj)
	if err != nil {
		t.Fatal(err)
	}
	badState, err = bacnet.AppendContextBool(badState, 2, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeAcknowledgeAlarm(badState, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("state kind: %v", err)
	}
	// timeOfAcknowledgment empty wrapper after a valid prefix up through source
	araw, err := bacnet.AppendContextUnsigned(nil, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	araw, err = bacnet.AppendContextObjectID(araw, 1, obj)
	if err != nil {
		t.Fatal(err)
	}
	araw, err = bacnet.AppendContextUnsigned(araw, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	ts, err := service.EncodeTimeStamp(service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 1})
	if err != nil {
		t.Fatal(err)
	}
	araw = append(araw, 0x3E)
	araw = append(araw, ts...)
	araw = append(araw, 0x3F)
	araw, err = bacnet.AppendContextCharacterString(araw, 4, bacnet.CharacterString{Value: "x"})
	if err != nil {
		t.Fatal(err)
	}
	araw = append(araw, 0x5E, 0x5F)
	if _, err := service.DecodeAcknowledgeAlarm(araw, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("empty ack ts: %v", err)
	}
}

func TestGetEventInformationACKDecodeFieldErrors(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	// trailing after valid LastReceived
	id := bacnet.ObjectIdentifier{Type: 2, Instance: 1}
	raw, err := service.EncodeGetEventInformation(service.GetEventInformationRequest{LastReceived: &id})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeGetEventInformation(append(append([]byte(nil), raw...), 0x00), limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("gei extra: %v", err)
	}
	// incomplete summary (object only)
	sum, err := bacnet.AppendContextObjectID(nil, 0, id)
	if err != nil {
		t.Fatal(err)
	}
	ack := append([]byte{0x0E}, sum...)
	ack = append(ack, 0x0F)
	ack, err = bacnet.AppendContextBool(ack, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeGetEventInformationACK(ack, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("incomplete summary: %v", err)
	}
	// summary with bad bitstring / unsigned kinds
	sum, err = bacnet.AppendContextObjectID(nil, 0, id)
	if err != nil {
		t.Fatal(err)
	}
	sum, err = bacnet.AppendContextUnsigned(sum, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	sum, err = bacnet.AppendContextUnsigned(sum, 2, 1) // should be bitstring
	if err != nil {
		t.Fatal(err)
	}
	ack = append([]byte{0x0E}, sum...)
	ack = append(ack, 0x0F)
	ack, err = bacnet.AppendContextBool(ack, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeGetEventInformationACK(ack, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("bad ack transitions: %v", err)
	}
	// missing moreEvents
	empty := bacnet.ApplicationValue{Kind: bacnet.ValueConstructed}
	good, err := service.EncodeGetEventInformationACK(service.GetEventInformationACK{
		Summaries: []service.EventSummary{{
			Object: id, EventState: 0,
			AcknowledgedTransitions: bacnet.BitString{UnusedBits: 5, Bytes: []byte{0}},
			EventTimeStamps:         empty, NotifyType: 0,
			EventEnable: bacnet.BitString{UnusedBits: 5, Bytes: []byte{0}}, EventPriorities: empty,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// strip moreEvents bool (last context bool)
	if len(good) < 2 {
		t.Fatal("short ack")
	}
	if _, err := service.DecodeGetEventInformationACK(good[:len(good)-2], limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("missing moreEvents: %v", err)
	}
	// unexpected summary tag
	sum, err = bacnet.AppendContextObjectID(nil, 0, id)
	if err != nil {
		t.Fatal(err)
	}
	sum, err = bacnet.AppendContextUnsigned(sum, 9, 1)
	if err != nil {
		t.Fatal(err)
	}
	ack = append([]byte{0x0E}, sum...)
	ack = append(ack, 0x0F)
	ack, err = bacnet.AppendContextBool(ack, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeGetEventInformationACK(ack, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("unexpected summary tag: %v", err)
	}
}
