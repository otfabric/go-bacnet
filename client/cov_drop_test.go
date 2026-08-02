// SPDX-License-Identifier: MIT

package client

import (
	"testing"

	"github.com/otfabric/go-bacnet/service"
)

func TestCOVEventDropMarksGap(t *testing.T) {
	env := newVirtualPair(t)
	s := &subscription{
		c:        env.Client,
		events:   make(chan SubscriptionEvent, 2),
		capacity: 1,
		id:       1,
	}
	s.send(SubscriptionEvent{Notification: &service.COVNotification{ProcessIdentifier: 1}, State: SubscriptionActive})
	s.send(SubscriptionEvent{Notification: &service.COVNotification{ProcessIdentifier: 1}, State: SubscriptionActive})
	if !s.gapPending {
		t.Fatal("expected gapPending after drop")
	}
	// Terminal on full channel hits default close path.
	s2 := &subscription{
		c:        env.Client,
		events:   make(chan SubscriptionEvent, 1),
		capacity: 10,
		id:       2,
	}
	s2.events <- SubscriptionEvent{State: SubscriptionActive} // fill
	s2.send(SubscriptionEvent{State: SubscriptionClosed})
	if !s2.closed {
		t.Fatal("expected closed")
	}
}
