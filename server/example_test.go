// SPDX-License-Identifier: MIT

package server_test

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/server"
	"github.com/otfabric/go-bacnet/server/object"
)

// A device with one commandable Analog Value, served on loopback and read
// and written by the go-bacnet client. A real deployment binds
// "0.0.0.0:47808" (the default) instead.
func Example() {
	dev, err := object.NewDevice(object.DeviceConfig{Instance: 1234, Name: "dev-1234", VendorID: 999})
	if err != nil {
		log.Fatal(err)
	}
	av, err := object.NewAnalogValue(object.PointConfig{Instance: 1, Name: "setpoint", Units: 62, Value: 21, Commandable: true})
	if err != nil {
		log.Fatal(err)
	}
	if err := dev.Add(av); err != nil {
		log.Fatal(err)
	}
	s, err := server.New(dev, server.WithLocalAddr("127.0.0.1:0"))
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	c, err := client.New(client.WithLocalAddr("127.0.0.1:0"))
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	target := client.Target{Endpoint: s.Local()}
	pv := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}

	// The application changes the value; COV subscribers would be notified.
	if err := av.SetPresentValue(bacnet.RealValue(22)); err != nil {
		log.Fatal(err)
	}
	v, err := c.ReadProperty(ctx, target, av.Identifier(), pv)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("read:", v.Real)

	// A client commands it at priority 8.
	prio := uint8(8)
	if err := c.WriteProperty(ctx, target, av.Identifier(), pv, bacnet.RealValue(19.5), &prio); err != nil {
		log.Fatal(err)
	}
	v, err = c.ReadProperty(ctx, target, av.Identifier(), pv)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("after write:", v.Real)
	// Output:
	// read: 22
	// after write: 19.5
}
