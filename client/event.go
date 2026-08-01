// SPDX-License-Identifier: MIT

package client

import (
	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
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
// The handler runs synchronously on the receive path and must return promptly.
type EventNotificationHandler func(EventNotificationDelivery)

// SetEventNotificationHandler installs or clears the inbound EventNotification
// handler. It may be called concurrently with reception.
func (c *Client) SetEventNotificationHandler(h EventNotificationHandler) {
	c.eventMu.Lock()
	c.eventHandler = h
	c.eventMu.Unlock()
}

func (c *Client) deliverEventNotification(note service.EventNotification, confirmed bool, src packetSource) {
	c.eventMu.Lock()
	h := c.eventHandler
	c.eventMu.Unlock()
	if h == nil {
		return
	}
	h(EventNotificationDelivery{
		Notification: note,
		Confirmed:    confirmed,
		Address:      src.bacnetAddress,
		Origin:       src.origin,
		Immediate:    src.immediate,
	})
}
