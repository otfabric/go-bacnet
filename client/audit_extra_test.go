// SPDX-License-Identifier: MIT

package client

import (
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestAuditStreamGapReplaceAndClosed(t *testing.T) {
	env := newVirtualPair(t)
	s1 := env.Client.OpenAuditStream(0) // default capacity
	env.Client.deliverAuditNotification(service.AuditNotification{
		SourceDevice: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}, Operation: 1,
	}, false)
	env.Client.deliverAuditNotification(service.AuditNotification{
		SourceDevice: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}, Operation: 2,
	}, false)
	env.Client.deliverAuditNotification(service.AuditNotification{
		SourceDevice: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}, Operation: 3,
	}, false)
	s2 := env.Client.OpenAuditStream(2)
	s1.Close()
	env.Client.deliverAuditNotification(service.AuditNotification{
		SourceDevice: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}, Operation: 4,
	}, true)
	select {
	case ev := <-s2.Events():
		if ev.Notification.Operation != 4 {
			t.Fatalf("%+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
	s2.Close()
	env.Client.deliverAuditNotification(service.AuditNotification{
		SourceDevice: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}, Operation: 5,
	}, false)
}

func TestCreateObjectWithObjectIdentifier(t *testing.T) {
	env := newVirtualPair(t)
	id := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 55}
	idx := uint32(0)
	raw, err := service.EncodeCreateObject(service.CreateObjectRequest{
		ObjectIdentifier: &id,
		InitialValues: []service.PropertyValue{{
			Property: bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName, ArrayIndex: &idx},
			Value:    bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: "x"}},
		}},
	})
	if err != nil || len(raw) == 0 {
		t.Fatal(err)
	}
	_ = env
}
