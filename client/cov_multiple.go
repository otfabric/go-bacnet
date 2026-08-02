// SPDX-License-Identifier: MIT

package client

import (
	"context"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/service"
)

// SubscribeCOVPropertyMultiple performs a confirmed SubscribeCOVPropertyMultiple.
//
// Exact-APDU retransmission is disabled. After the request has been sent,
// timeout/cancel returns *bacnet.OutcomeUnknownError.
func (c *Client) SubscribeCOVPropertyMultiple(ctx context.Context, target Target, req service.SubscribeCOVPropertyMultipleRequest) error {
	payload, err := service.EncodeSubscribeCOVPropertyMultiple(req)
	if err != nil {
		return err
	}
	pdu, err := c.confirmedRequest(ctx, target, apdu.ServiceSubscribeCOVPropertyMultiple, payload, DefaultRetransmitPolicy(apdu.ServiceSubscribeCOVPropertyMultiple))
	if err != nil {
		return err
	}
	if pdu.Type != apdu.TypeSimpleACK {
		return bacnet.ErrProtocolViolation
	}
	return nil
}

func (c *Client) deliverCOVNotificationMultiple(note service.COVNotificationMultiple, _ bool, src packetSource) {
	// Synthesize per-object COVNotification values for existing Subscription streams.
	for _, obj := range note.Objects {
		vals := make([]service.PropertyValue, 0, len(obj.Values))
		for _, v := range obj.Values {
			vals = append(vals, service.PropertyValue{
				Property: v.Property,
				Value:    v.Value,
			})
		}
		synth := service.COVNotification{
			ProcessIdentifier: note.SubscriberProcessIdentifier,
			InitiatingDevice:  note.InitiatingDevice,
			MonitoredObject:   obj.Object,
			TimeRemaining:     note.TimeRemaining,
			Values:            vals,
		}
		c.subs.deliver(SubscriptionEvent{
			Notification: &synth,
			State:        SubscriptionActive,
		}, note.SubscriberProcessIdentifier, src)
	}
}
