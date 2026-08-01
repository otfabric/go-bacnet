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
	"flag"
	"fmt"
	"log"
	"net/netip"
	"os"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/service"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:47808", "peer BACnet/IP host:port")
	instance := flag.Uint("instance", 1001, "remote device instance")
	object := flag.Uint("object", 1, "Trend Log object instance")
	count := flag.Int("count", 4, "ReadRange byPosition count")
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

	low, high := uint32(*instance), uint32(*instance)
	if err := c.SendWhoIs(ctx, target.Endpoint, false, client.DiscoveryOptions{
		LowLimit: &low, HighLimit: &high,
	}); err != nil {
		log.Fatalf("SendWhoIs: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	req := service.ReadRangeRequest{
		Object:         bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeTrendLog, Instance: uint32(*object)},
		Property:       bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer},
		By:             service.ReadRangeByPosition,
		ReferenceIndex: 1,
		Count:          int32(*count),
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
