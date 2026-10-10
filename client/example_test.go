// SPDX-License-Identifier: MIT

package client_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/netip"
	"os"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/client"
)

// Discover the devices on the local network and read the name of each.
func Example() {
	c, err := client.New()
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	ctx := context.Background()
	devices, err := c.Discover(ctx, client.DiscoveryOptions{Window: 3 * time.Second})
	if err != nil {
		log.Fatal(err)
	}
	name := bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName}
	for _, d := range devices {
		val, err := c.ReadProperty(ctx, d.Target(), d.DeviceID(), name)
		if err != nil {
			fmt.Printf("device %d at %s: %v\n", d.Instance, d.Address, err)
			continue
		}
		fmt.Printf("device %d at %s: %s\n", d.Instance, d.Address, val.Character.Value)
	}
}

// Read from a device whose address is known, without discovery, from an
// ephemeral local port.
func ExampleClient_ReadProperty() {
	c, err := client.New(client.WithLocalAddr("0.0.0.0:0"))
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	target := client.Target{Endpoint: bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.20:47808"))}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// The wildcard instance asks the device for its own identifier.
	id, err := c.ReadProperty(ctx, target, bacnet.AnyDevice(),
		bacnet.PropertyReference{Identifier: bacnet.PropertyObjectIdentifier})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("device instance:", id.ObjectID.Instance)

	av1 := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	pv, err := c.ReadProperty(ctx, target, av1, bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue})
	var remote *bacnet.ErrorResponse
	switch {
	case errors.As(err, &remote):
		// The device answered with an Error PDU, e.g. object/unknown-object.
		fmt.Println("device refused:", bacnet.ErrorClassName(remote.Class), bacnet.ErrorCodeName(remote.Code))
	case errors.Is(err, bacnet.ErrTimeout):
		fmt.Println("no answer")
	case err != nil:
		log.Fatal(err)
	default:
		fmt.Println("present-value:", pv.Real)
	}
}

// Command an output at priority 8 and release it again. A write that gets no
// answer may still have been applied.
func ExampleClient_WritePriority() {
	c, err := client.New(client.WithLocalAddr("0.0.0.0:0"))
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	target := client.Target{Endpoint: bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.20:47808"))}
	ao1 := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogOutput, Instance: 1}
	ctx := context.Background()

	err = c.WritePriority(ctx, target, ao1, 8, bacnet.RealValue(21.5))
	var unknown *bacnet.OutcomeUnknownError
	if errors.As(err, &unknown) {
		// Sent, not acknowledged: read the value back before writing again.
		log.Fatalf("write may or may not have been applied: %v", err)
	}
	if err != nil {
		log.Fatal(err)
	}
	if err := c.RelinquishPriority(ctx, target, ao1, 8); err != nil {
		log.Fatal(err)
	}
}

// Subscribe to change-of-value notifications and print them until the
// context ends.
func ExampleClient_SubscribeCOV() {
	c, err := client.New(client.WithLocalAddr("0.0.0.0:0"))
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	target := client.Target{Endpoint: bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.20:47808"))}
	av1 := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	sub, err := c.SubscribeCOV(ctx, target, av1, client.COVOptions{Lifetime: 300})
	if err != nil {
		log.Fatal(err)
	}
	// Close cancels the subscription on the device.
	defer func() { _ = sub.Close() }()

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-sub.Events():
			if !ok {
				return
			}
			if ev.Gap {
				fmt.Println("notifications were dropped: the consumer is too slow")
			}
			if ev.Notification == nil {
				continue // a state change: Active, Renewing, Degraded, Closed
			}
			for _, v := range ev.Notification.Values {
				fmt.Printf("%v = %+v\n", v.Property.Identifier, v.Value)
			}
		}
	}
}

// Log what the client drops and notices. Without WithLogger it is silent.
func ExampleWithLogger() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	c, err := client.New(client.WithLogger(logger.With("component", "bacnet")))
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = c.Close() }()
}
