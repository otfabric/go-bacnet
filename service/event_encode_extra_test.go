// SPDX-License-Identifier: MIT

package service_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestEncodeEventNotificationOptionalFields(t *testing.T) {
	msg := bacnet.CharacterString{Value: "hi"}
	ack := true
	from := uint32(0)
	flags := bacnet.BitString{UnusedBits: 4, Bytes: []byte{0x00}}
	note := service.EventNotification{
		ProcessIdentifier: 1,
		InitiatingDevice:  bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		EventObject:       bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
		TimeStamp:         service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 2},
		NotificationClass: 3,
		Priority:          4,
		EventType:         5,
		MessageText:       &msg,
		NotifyType:        0,
		AckRequired:       &ack,
		FromState:         &from,
		ToState:           1,
		Parameters: &service.NotificationParameters{
			ChangeOfLifeSafety: &service.ChangeOfLifeSafetyParams{
				NewState: 1, NewMode: 2, StatusFlags: flags, OperationExpected: 3,
			},
		},
	}
	raw, err := service.EncodeEventNotification(note)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeEventNotification(raw, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if got.MessageText == nil || got.MessageText.Value != "hi" || got.Parameters == nil || got.Parameters.ChangeOfLifeSafety == nil {
		t.Fatalf("%#v", got)
	}

	opaque := note
	opaque.Parameters = nil
	opaque.NotificationParams = &bacnet.ApplicationValue{
		Kind: bacnet.ValueConstructed,
		Elements: []bacnet.Element{{
			Context: true, TagNumber: uint8(service.NotificationExtended),
			Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{{Value: bacnet.UnsignedValue(1)}}},
		}},
	}
	if _, err := service.EncodeEventNotification(opaque); err != nil {
		t.Fatal(err)
	}
	bad := opaque
	bad.NotificationParams = &bacnet.ApplicationValue{Kind: bacnet.ValueUnsigned, Unsigned: 1}
	if _, err := service.EncodeEventNotification(bad); err == nil {
		t.Fatal("expected malformed opaque params")
	}
}
