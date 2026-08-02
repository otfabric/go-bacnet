// SPDX-License-Identifier: MIT

package client

import (
	"testing"

	"github.com/otfabric/go-bacnet/service"
)

func TestTransitionOf(t *testing.T) {
	ack := true
	from := uint32(1)
	tr := TransitionOf(service.EventNotification{
		EventType:   5,
		ToState:     2,
		NotifyType:  0,
		FromState:   &from,
		AckRequired: &ack,
	})
	if tr.FromState != 1 || tr.ToState != 2 || !tr.AckRequired || tr.EventType != 5 {
		t.Fatalf("%+v", tr)
	}
}

func TestEventStreamPublishAndGap(t *testing.T) {
	s := &eventStream{
		capacity: 1,
		ch:       make(chan EventStreamEvent, 1),
	}
	s.publish(EventNotificationDelivery{Confirmed: true})
	s.publish(EventNotificationDelivery{Confirmed: false}) // should set gap
	ev := <-s.ch
	if !ev.Delivery.Confirmed {
		t.Fatalf("first %+v", ev)
	}
	s.mu.Lock()
	gap := s.gap
	s.mu.Unlock()
	if !gap {
		t.Fatal("expected gap after drop")
	}
}
