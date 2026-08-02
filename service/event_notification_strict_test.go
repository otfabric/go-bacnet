// SPDX-License-Identifier: MIT

package service

import (
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
)

func TestEventNotificationStrictDuplicates(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	from := uint32(0)
	ackReq := true
	payload, err := EncodeEventNotification(EventNotification{
		ProcessIdentifier: 1,
		InitiatingDevice:  bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		EventObject:       bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
		TimeStamp:         TimeStamp{Choice: TimeStampSequence, Sequence: 1},
		NotificationClass: 1,
		Priority:          1,
		EventType:         0,
		NotifyType:        0,
		AckRequired:       &ackReq,
		FromState:         &from,
		ToState:           1,
	})
	if err != nil {
		t.Fatal(err)
	}
	dupPID, err := bacnet.AppendContextUnsigned(nil, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeEventNotification(append(payload, dupPID...), limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("duplicate processIdentifier: %v", err)
	}
}
