// SPDX-License-Identifier: MIT

package client

import (
	"fmt"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/internal/diag"
	"github.com/otfabric/go-bacnet/service"
)

// EventNotificationDelivery is an inbound EventNotification plus source metadata.
type EventNotificationDelivery struct {
	Notification service.EventNotification
	Confirmed    bool
	Address      bacnet.Address
	Origin       bip.Endpoint
	Immediate    bip.Endpoint
}

// EventNotificationHandler receives decoded EventNotifications.
//
// Confirmed notifications are SimpleACK'd before the handler is invoked.
// The handler runs synchronously on the receive path: it must not block and
// must not call Client methods that wait for a response (ReadProperty,
// confirmed requests, Discover, etc.), or the receive loop can deadlock.
// Prefer copying the delivery and handling work on another goroutine.
// Handler panics are recovered so a faulty callback cannot stop reception.
type EventNotificationHandler func(EventNotificationDelivery)

// SetEventNotificationHandler installs or clears the inbound EventNotification
// handler. It may be called concurrently with reception.
func (c *Client) SetEventNotificationHandler(h EventNotificationHandler) {
	c.eventMu.Lock()
	c.eventHandler = h
	c.eventMu.Unlock()
}

func (c *Client) deliverEventNotification(note service.EventNotification, confirmed bool, src packetSource) {
	d := EventNotificationDelivery{
		Notification: note,
		Confirmed:    confirmed,
		Address:      src.bacnetAddress,
		Origin:       src.origin,
		Immediate:    src.immediate,
	}
	c.eventMu.Lock()
	h := c.eventHandler
	stream := c.eventStream
	c.eventMu.Unlock()
	if stream != nil {
		stream.publish(d)
	}
	if h == nil {
		return
	}
	defer func() {
		if rec := recover(); rec != nil {
			c.diag.Report(diag.Event{
				Kind:    diag.KindUnexpectedAPDU,
				Message: "EventNotification handler panic",
				Fields:  map[string]any{"panic": fmt.Sprint(rec)},
			})
		}
	}()
	h(d)
}
