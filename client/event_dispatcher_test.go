// SPDX-License-Identifier: MIT

package client

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet/internal/diag"
	"github.com/otfabric/go-bacnet/service"
)

func TestEventDispatcherOverflowDropNewest(t *testing.T) {
	var got atomic.Int64
	d := newEventDispatcher(EventDispatcherConfig{
		Workers: 1, BufferSize: 1, Overflow: EventOverflowDropNewest,
		Handler: func(EventNotificationDelivery) {
			time.Sleep(30 * time.Millisecond)
			got.Add(1)
		},
	}, diag.Discard{})
	defer d.close()
	d.publish(EventNotificationDelivery{Notification: service.EventNotification{ProcessIdentifier: 1}})
	d.publish(EventNotificationDelivery{Notification: service.EventNotification{ProcessIdentifier: 2}})
	d.publish(EventNotificationDelivery{Notification: service.EventNotification{ProcessIdentifier: 3}})
	time.Sleep(80 * time.Millisecond)
	if d.Dropped() == 0 {
		t.Fatal("expected overflow drops")
	}
	if got.Load() == 0 {
		t.Fatal("expected at least one delivery")
	}
}

func TestWithEventDispatcherRequiresHandler(t *testing.T) {
	_, err := New(WithEventDispatcher(EventDispatcherConfig{Workers: 1, BufferSize: 1}))
	if err == nil {
		t.Fatal("expected error")
	}
}
