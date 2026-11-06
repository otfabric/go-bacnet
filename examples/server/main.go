// SPDX-License-Identifier: MIT

// Command server serves a small BACnet/IP device: a commandable Analog
// Value setpoint, an Analog Input whose value drifts, and a Binary Value.
// It prints every write a client makes, announces itself with I-Am and runs
// until interrupted.
//
// Usage:
//
//	go run . -addr 0.0.0.0:47808 -instance 1234
//
// Then, from another host or port, for example with bacnetctl or the
// go-bacnet client: read analog-value,1 present-value, or write it at a
// priority.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/server"
	"github.com/otfabric/go-bacnet/server/object"
)

func main() {
	addr := flag.String("addr", "0.0.0.0:47808", "local UDP bind address")
	instance := flag.Uint("instance", 1234, "device instance (0..4194302)")
	name := flag.String("name", "go-bacnet-example", "device Object_Name")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ready := func(ep bip.Endpoint) { log.Printf("serving device %d on %s", *instance, ep) }
	if err := serve(ctx, *addr, uint32(*instance), *name, ready, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

// serve runs the device until ctx ends. ready is called with the bound
// endpoint once the server is up; every accepted write is printed to out.
func serve(ctx context.Context, addr string, instance uint32, name string, ready func(bip.Endpoint), out io.Writer) error {
	if instance >= bacnet.MaxObjectInstance {
		return fmt.Errorf("instance %d out of range", instance)
	}
	dev, err := object.NewDevice(object.DeviceConfig{
		Instance: instance, Name: name, VendorID: 999, VendorName: "example",
		ModelName: "go-bacnet example server", FirmwareRevision: "1", ApplicationSoftwareVersion: "1",
	})
	if err != nil {
		return err
	}
	setpoint, err := object.NewAnalogValue(object.PointConfig{Instance: 1, Name: "setpoint", Units: 62, Value: 21, Commandable: true})
	if err != nil {
		return err
	}
	temp, err := object.NewAnalogInput(object.PointConfig{Instance: 1, Name: "temperature", Units: 62, Value: 20, COVIncrement: 0.5})
	if err != nil {
		return err
	}
	fan, err := object.NewBinaryValue(object.PointConfig{Instance: 1, Name: "fan", ActiveText: "on", InactiveText: "off", Commandable: true})
	if err != nil {
		return err
	}
	for _, o := range []object.Object{setpoint, temp, fan} {
		if err := dev.Add(o); err != nil {
			return err
		}
	}

	s, err := server.New(dev, server.WithLocalAddr(addr), server.WithHooks(server.Hooks{
		// BeforeWrite may refuse a write with a BACnet error; this one
		// accepts every write and reports it.
		BeforeWrite: func(_ context.Context, obj bacnet.ObjectIdentifier, w object.Write) error {
			_, _ = fmt.Fprintf(out, "write %s %s priority=%d\n", obj, w.Property.Identifier, w.Priority)
			return nil
		},
	}))
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	if err := s.IAm(); err != nil {
		log.Printf("I-Am: %v", err)
	}
	ready(s.Local())

	// The application owns the values of its inputs; COV subscribers are
	// notified when the change exceeds COV_Increment.
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	v := float32(20)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			v += 0.25
			if v > 24 {
				v = 20
			}
			if err := temp.SetPresentValue(bacnet.RealValue(v)); err != nil {
				return err
			}
		}
	}
}
