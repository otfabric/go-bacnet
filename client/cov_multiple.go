// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"fmt"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/service"
)

// COVMultipleOptions configures a SubscribeCOVMultiple subscription.
type COVMultipleOptions struct {
	COVOptions
	// MaxNotificationDelay is the longest time, in seconds, the device may
	// hold back a change to collect others into the same notification. Zero
	// leaves it to the device.
	MaxNotificationDelay uint32
}

// SubscribeCOVMultiple subscribes to changes of several properties of
// several objects with one SubscribeCOVPropertyMultiple request and returns
// once the device has accepted it.
//
// The Subscription behaves as the one SubscribeCOV returns: the client
// renews it at half the lifetime and Close cancels it on the device. Its
// notification events have Multiple set (and Notification nil): one event
// per COVNotificationMultiple, with the notification's timestamp and, for
// properties subscribed with Timestamped, each value's time of change.
// Objects the subscription did not ask for are left out, and a notification
// with none of its objects is dropped.
//
// specs is copied. A device that does not implement the service answers
// with a *bacnet.RejectError (unrecognized-service).
func (c *Client) SubscribeCOVMultiple(ctx context.Context, target Target, specs []service.COVMultipleSubscription, opts COVMultipleOptions) (Subscription, error) {
	if len(specs) == 0 {
		return nil, fmt.Errorf("%w: SubscribeCOVMultiple requires at least one object", bacnet.ErrInvalidConfig)
	}
	s := &subscription{multi: make([]service.COVMultipleSubscription, len(specs))}
	for i, spec := range specs {
		if len(spec.Properties) == 0 {
			return nil, fmt.Errorf("%w: SubscribeCOVMultiple: %v has no properties", bacnet.ErrInvalidConfig, spec.Object)
		}
		s.multi[i] = service.COVMultipleSubscription{
			Object:     spec.Object,
			Properties: append([]service.COVPropertyReference(nil), spec.Properties...),
		}
	}
	if opts.MaxNotificationDelay != 0 {
		delay := opts.MaxNotificationDelay
		s.maxDelay = &delay
	}
	return c.startSubscription(ctx, target, s, opts.COVOptions)
}

// SubscribeCOVPropertyMultiple sends one SubscribeCOVPropertyMultiple request
// as given. It is the raw service: nothing renews or cancels the
// subscription, and notifications for req.SubscriberProcessIdentifier are
// acknowledged and dropped unless a Subscription of this client has that
// identifier. Use SubscribeCOVMultiple for a managed subscription.
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
	c.subs.deliverMultiple(&note, src)
}
