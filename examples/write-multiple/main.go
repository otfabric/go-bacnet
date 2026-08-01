// SPDX-License-Identifier: MIT

// Command write-multiple demonstrates Client.WritePropertyMultiple.
// Large payloads may segment when the peer MaxAPDU is small and Segmentation
// evidence shows the peer can receive confirmed-request segments.
//
// Usage:
//
//	go run . -addr 192.168.1.10:47808 -instance 1001 -object 0:1 -value 22.5
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/service"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:47808", "peer BACnet/IP host:port")
	instance := flag.Uint("instance", 1001, "remote device instance")
	object := flag.String("object", "2:1", "object type:instance (default analog-value:1)")
	value := flag.Float64("value", 22.5, "present-value to write")
	flag.Parse()

	target, err := parseTarget(*addr)
	if err != nil {
		log.Fatal(err)
	}
	obj, err := parseObject(*object)
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	c, err := client.New()
	if err != nil {
		log.Fatalf("New: %v", err)
	}
	defer func() { _ = c.Close() }()

	low, high := uint32(*instance), uint32(*instance)
	_ = c.SendWhoIs(ctx, target.Endpoint, false, client.DiscoveryOptions{
		LowLimit: &low, HighLimit: &high,
	})
	time.Sleep(300 * time.Millisecond)

	prio := uint8(8)
	err = c.WritePropertyMultiple(ctx, target, []service.WriteAccessSpecification{{
		Object: obj,
		Properties: []service.WritePropertyValue{{
			Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
			Value:    bacnet.RealValue(float32(*value)),
			Priority: &prio,
		}},
	}})
	if err != nil {
		log.Fatalf("WritePropertyMultiple: %v", err)
	}
	fmt.Printf("wrote present-value=%.2f to %s\n", *value, obj)
}

func parseTarget(addr string) (client.Target, error) {
	ap, err := netip.ParseAddrPort(addr)
	if err != nil {
		return client.Target{}, fmt.Errorf("addr: %w", err)
	}
	if !ap.Addr().Is4() {
		return client.Target{}, fmt.Errorf("addr: Horizon 1 requires IPv4")
	}
	ip := ap.Addr().As4()
	ep := bip.NewEndpoint(ap)
	mac := []byte{ip[0], ip[1], ip[2], ip[3], byte(ap.Port() >> 8), byte(ap.Port())}
	return client.Target{
		Address:  bacnet.LocalStation(bacnet.MustMAC(mac)),
		Endpoint: ep,
	}, nil
}

func parseObject(s string) (bacnet.ObjectIdentifier, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return bacnet.ObjectIdentifier{}, fmt.Errorf("object must be type:instance")
	}
	t, err := strconv.ParseUint(parts[0], 10, 16)
	if err != nil {
		return bacnet.ObjectIdentifier{}, err
	}
	inst, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil {
		return bacnet.ObjectIdentifier{}, err
	}
	return bacnet.ObjectIdentifier{Type: bacnet.ObjectType(t), Instance: uint32(inst)}, nil
}
