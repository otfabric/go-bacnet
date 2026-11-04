// SPDX-License-Identifier: MIT

// Command read-range demonstrates Client.ReadRange against a Trend Log
// Log_Buffer, including typed BACnetLogRecord decode when the peer returns
// well-formed records.
//
// Usage:
//
//	go run . -addr 192.168.1.10:47808 -instance 1001 -object 1 -count 10
package main

import (
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"net/netip"
	"os"
	"strconv"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/service"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:47808", "peer BACnet/IP host:port")
	instance := uint32Flag("instance", 1001, "remote device instance")
	object := uint32Flag("object", 1, "Trend Log object instance")
	count := int32Flag("count", 4, "ReadRange byPosition count")
	flag.Parse()

	target, err := parseTarget(*addr)
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

	low, high := *instance, *instance
	if err := c.SendWhoIs(ctx, target.Endpoint, false, client.DiscoveryOptions{
		LowLimit: &low, HighLimit: &high,
	}); err != nil {
		log.Fatalf("SendWhoIs: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	req := service.ReadRangeRequest{
		Object:         bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeTrendLog, Instance: *object},
		Property:       bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer},
		By:             service.ReadRangeByPosition,
		ReferenceIndex: 1,
		Count:          *count,
	}
	ack, err := c.ReadRange(ctx, target, req)
	if err != nil {
		log.Fatalf("ReadRange: %v", err)
	}

	fmt.Printf("itemCount=%d first=%v last=%v more=%v\n",
		ack.ItemCount, ack.FirstItem(), ack.LastItem(), ack.MoreItems())
	if len(ack.LogRecords) > 0 {
		for i, rec := range ack.LogRecords {
			fmt.Printf("  [%d] %02d:%02d:%02d choice=%d kind=%v\n",
				i, rec.Timestamp.Time.Hour, rec.Timestamp.Time.Minute, rec.Timestamp.Time.Second,
				rec.DatumChoice, rec.Datum.Kind)
		}
		return
	}
	fmt.Fprintf(os.Stderr, "typed LogRecords unavailable; flat ItemData tags=%d\n", len(ack.ItemData))
}

func parseTarget(addr string) (client.Target, error) {
	ap, err := netip.ParseAddrPort(addr)
	if err != nil {
		return client.Target{}, fmt.Errorf("addr: %w", err)
	}
	if !ap.Addr().Is4() {
		return client.Target{}, fmt.Errorf("addr: BACnet/IP requires IPv4")
	}
	ip := ap.Addr().As4()
	ep := bip.NewEndpoint(ap)
	mac := binary.BigEndian.AppendUint16([]byte{ip[0], ip[1], ip[2], ip[3]}, ap.Port())
	return client.Target{
		Address:  bacnet.LocalStation(bacnet.MustMAC(mac)),
		Endpoint: ep,
	}, nil
}

// uint32Flag defines a decimal flag that must fit 32 bits.
func uint32Flag(name string, def uint32, usage string) *uint32 {
	v := def
	flag.Func(name, fmt.Sprintf("%s (default %d)", usage, def), func(s string) error {
		u, err := strconv.ParseUint(s, 10, 32)
		if err != nil {
			return err
		}
		v = uint32(u)
		return nil
	})
	return &v
}

// int32Flag defines a decimal flag that must fit 32 signed bits.
func int32Flag(name string, def int32, usage string) *int32 {
	v := def
	flag.Func(name, fmt.Sprintf("%s (default %d)", usage, def), func(s string) error {
		n, err := strconv.ParseInt(s, 10, 32)
		if err != nil {
			return err
		}
		v = int32(n)
		return nil
	})
	return &v
}
