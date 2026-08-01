// SPDX-License-Identifier: MIT

package service_test

import (
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestH2EncodeRejectsInvalidObjectIDs(t *testing.T) {
	bad := bacnet.ObjectIdentifier{Type: 0x400, Instance: 0}
	if _, err := service.EncodeEventNotification(service.EventNotification{
		InitiatingDevice: bad,
		EventObject:      bacnet.ObjectIdentifier{Type: 2, Instance: 1},
		TimeStamp:        service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 1},
	}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("event: %v", err)
	}
	if _, err := service.EncodeEventNotification(service.EventNotification{
		ProcessIdentifier: 1,
		InitiatingDevice:  bacnet.ObjectIdentifier{Type: 8, Instance: 1},
		EventObject:       bad,
		TimeStamp:         service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 1},
	}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("event object: %v", err)
	}
	if _, err := service.EncodeAcknowledgeAlarm(service.AcknowledgeAlarmRequest{
		EventObject:          bacnet.ObjectIdentifier{Type: 2, Instance: 1},
		TimeStamp:            service.TimeStamp{Choice: 99},
		AcknowledgmentSource: bacnet.CharacterString{Value: "x"},
		TimeOfAcknowledgment: service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 1},
	}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("bad ts choice: %v", err)
	}
	if _, err := service.EncodeAcknowledgeAlarm(service.AcknowledgeAlarmRequest{
		EventObject:          bad,
		TimeStamp:            service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 1},
		AcknowledgmentSource: bacnet.CharacterString{Value: "x"},
		TimeOfAcknowledgment: service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 2},
	}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("ack: %v", err)
	}
	if _, err := service.EncodeGetEventInformation(service.GetEventInformationRequest{LastReceived: &bad}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("gei: %v", err)
	}
	empty := bacnet.ApplicationValue{Kind: bacnet.ValueConstructed}
	if _, err := service.EncodeGetEventInformationACK(service.GetEventInformationACK{
		Summaries: []service.EventSummary{{
			Object: bad, EventTimeStamps: empty, EventPriorities: empty,
			AcknowledgedTransitions: bacnet.BitString{UnusedBits: 5, Bytes: []byte{0}},
			EventEnable:             bacnet.BitString{UnusedBits: 5, Bytes: []byte{0}},
		}},
	}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("gei ack: %v", err)
	}
}

func TestH2DecodeUnexpectedTags(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	cases := []struct {
		name string
		fn   func([]byte) error
		raw  []byte
	}{
		{"event", func(b []byte) error { _, err := service.DecodeEventNotification(b, limits); return err }, mustCtx(t, 15, 1)},
		{"ack", func(b []byte) error { _, err := service.DecodeAcknowledgeAlarm(b, limits); return err }, mustCtx(t, 15, 1)},
		{"dcc", func(b []byte) error { _, err := service.DecodeDeviceCommunicationControl(b, limits); return err }, mustCtx(t, 15, 1)},
		{"reinit", func(b []byte) error { _, err := service.DecodeReinitializeDevice(b, limits); return err }, mustCtx(t, 15, 1)},
		{"gei-ack", func(b []byte) error { _, err := service.DecodeGetEventInformationACK(b, limits); return err }, mustCtx(t, 15, 1)},
	}
	for _, tc := range cases {
		if err := tc.fn(tc.raw); !errors.Is(err, bacnet.ErrMalformed) {
			t.Fatalf("%s: %v", tc.name, err)
		}
	}
}

func mustCtx(t *testing.T, tag uint8, v uint64) []byte {
	t.Helper()
	raw, err := bacnet.AppendContextUnsigned(nil, tag, v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestGetEventInformationACKWithTimestampsAndPriorities(t *testing.T) {
	ts, err := service.EncodeTimeStamp(service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 3})
	if err != nil {
		t.Fatal(err)
	}
	tsEls, _, err := bacnet.ParseSequence(ts, bacnet.DefaultDecodeLimits(), -1)
	if err != nil {
		t.Fatal(err)
	}
	prio := bacnet.Element{Context: true, TagNumber: 0, Value: bacnet.ApplicationValue{Kind: bacnet.ValueOctetString, OctetString: []byte{5}}}
	ack := service.GetEventInformationACK{
		Summaries: []service.EventSummary{{
			Object:                  bacnet.ObjectIdentifier{Type: 2, Instance: 1},
			EventState:              1,
			AcknowledgedTransitions: bacnet.BitString{UnusedBits: 5, Bytes: []byte{0xE0}},
			EventTimeStamps:         bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: tsEls},
			NotifyType:              0,
			EventEnable:             bacnet.BitString{UnusedBits: 5, Bytes: []byte{0xE0}},
			EventPriorities:         bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{prio}},
		}},
		MoreEvents: true,
	}
	raw, err := service.EncodeGetEventInformationACK(ack)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeGetEventInformationACK(raw, bacnet.DefaultDecodeLimits())
	if err != nil || !got.MoreEvents || len(got.Summaries) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	if len(got.Summaries[0].EventTimeStamps.Elements) != 1 || len(got.Summaries[0].EventPriorities.Elements) != 1 {
		t.Fatalf("timestamps/priorities %+v", got.Summaries[0])
	}
}

func TestDCCDuplicateEnableAndLongPasswordDecode(t *testing.T) {
	raw, err := bacnet.AppendContextUnsigned(nil, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextUnsigned(raw, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeDeviceCommunicationControl(raw, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("%v", err)
	}
	enc, err := bacnet.AppendContextUnsigned(nil, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	cs := bacnet.CharacterString{Value: string(make([]byte, 21))}
	body, err := bacnet.AppendContextCharacterString(nil, 2, cs)
	if err != nil {
		t.Fatal(err)
	}
	enc = append(enc, body...)
	if _, err := service.DecodeDeviceCommunicationControl(enc, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("long password: %v", err)
	}
	// ReinitializeDevice: state + oversized password (tag 1).
	renc, err := bacnet.AppendContextUnsigned(nil, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	p1, err := bacnet.AppendContextCharacterString(nil, 1, cs)
	if err != nil {
		t.Fatal(err)
	}
	renc = append(renc, p1...)
	if _, err := service.DecodeReinitializeDevice(renc, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("reinit long password: %v", err)
	}
	dup, err := bacnet.AppendContextUnsigned(nil, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	dup, err = bacnet.AppendContextUnsigned(dup, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeReinitializeDevice(dup, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("dup state: %v", err)
	}
}
