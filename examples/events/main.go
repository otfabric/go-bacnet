// SPDX-License-Identifier: MIT

// Command events listens for inbound EventNotification PDUs and prints typed
// NotificationParameters when recognized (for example change-of-state).
//
// Usage:
//
//	go run . -listen 0.0.0.0:47808
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/otfabric/go-bacnet/client"
)

func main() {
	listen := flag.String("listen", "0.0.0.0:47808", "local UDP bind address")
	flag.Parse()

	c, err := client.New(
		client.WithLocalAddr(*listen),
		client.WithEventNotificationHandler(func(ev client.EventNotificationDelivery) {
			n := ev.Notification
			fmt.Printf("event device=%s object=%s type=%d toState=%d confirmed=%v\n",
				n.InitiatingDevice, n.EventObject, n.EventType, n.ToState, ev.Confirmed)
			if n.Parameters != nil && n.Parameters.ChangeOfState != nil {
				cos := n.Parameters.ChangeOfState
				fmt.Printf("  change-of-state newState.choice=%d value=%v\n",
					cos.NewState.Choice, cos.NewState.Value)
			} else if n.NotificationParams != nil {
				fmt.Printf("  params (opaque) elements=%d\n", len(n.NotificationParams.Elements))
			}
		}),
	)
	if err != nil {
		log.Fatalf("New: %v", err)
	}
	defer func() { _ = c.Close() }()

	fmt.Printf("listening for EventNotification on %s\n", *listen)
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
}
