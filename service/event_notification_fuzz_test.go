// SPDX-License-Identifier: MIT

package service_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func FuzzDecodeEventNotification(f *testing.F) {
	limits := bacnet.DefaultDecodeLimits()
	f.Add([]byte(nil))
	if p, err := service.EncodeEventNotification(service.EventNotification{
		ProcessIdentifier: 1,
		InitiatingDevice:  bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		EventObject:       bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
		TimeStamp:         service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 1},
		NotificationClass: 1,
		Priority:          1,
		EventType:         0,
		NotifyType:        0,
		ToState:           1,
	}); err == nil {
		f.Add(p)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = service.DecodeEventNotification(data, limits)
	})
}

func FuzzDecodeNotificationParameters(f *testing.F) {
	f.Add([]byte(nil))
	params := service.NotificationParameters{
		OutOfRange: &service.OutOfRangeParams{
			ExceedingValue: 1,
			StatusFlags:    bacnet.BitString{UnusedBits: 4, Bytes: []byte{0}},
			Deadband:       0.1,
			ExceededLimit:  0.5,
		},
	}
	if els, err := service.EncodeNotificationParameters(params); err == nil {
		if raw, err := bacnet.AppendContextTagged(nil, 12, els); err == nil {
			f.Add(raw)
		}
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		els, _, err := bacnet.ParseSequence(data, bacnet.DefaultDecodeLimits(), -1)
		if err != nil {
			return
		}
		_, _ = service.DecodeNotificationParameters(els)
	})
}

func FuzzDecodeGetEventInformationACK(f *testing.F) {
	limits := bacnet.DefaultDecodeLimits()
	f.Add([]byte(nil))
	if p, err := service.EncodeGetEventInformationACK(service.GetEventInformationACK{
		MoreEvents: false,
	}); err == nil {
		f.Add(p)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = service.DecodeGetEventInformationACK(data, limits)
	})
}

func FuzzDecodeAcknowledgeAlarm(f *testing.F) {
	limits := bacnet.DefaultDecodeLimits()
	f.Add([]byte(nil))
	if p, err := service.EncodeAcknowledgeAlarm(service.AcknowledgeAlarmRequest{
		ProcessIdentifier:    1,
		EventObject:          bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
		EventStateAcked:      1,
		TimeStamp:            service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 1},
		AcknowledgmentSource: bacnet.CharacterString{Value: "ops"},
		TimeOfAcknowledgment: service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 2},
	}); err == nil {
		f.Add(p)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = service.DecodeAcknowledgeAlarm(data, limits)
	})
}
