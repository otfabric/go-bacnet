// SPDX-License-Identifier: MIT

// Command who-has demonstrates SendWhoHas / DiscoverObjects and the object
// observation registry.
//
// Usage:
//
//	go run . -name "AV-1"
//	go run . -object 2:1
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/client"
)

func main() {
	name := flag.String("name", "", "object name for Who-Has (mutually exclusive with -object)")
	object := flag.String("object", "", "object type:instance for Who-Has")
	timeout := flag.Duration("timeout", 3*time.Second, "collection window")
	flag.Parse()

	if (*name == "") == (*object == "") {
		log.Fatal("provide exactly one of -name or -object")
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	c, err := client.New(client.WithPort(bip.DefaultPort))
	if err != nil {
		log.Fatalf("New: %v", err)
	}
	defer func() { _ = c.Close() }()

	opts := client.WhoHasOptions{}
	if *name != "" {
		cs := bacnet.CharacterString{Encoding: 0, Value: *name}
		opts.Name = &cs
	} else {
		obj, err := parseObject(*object)
		if err != nil {
			log.Fatal(err)
		}
		opts.Object = &obj
	}

	objects, err := c.DiscoverObjects(ctx, opts)
	// DiscoverObjects returns when ctx ends; results are still useful.
	_ = err
	if len(objects) == 0 {
		fmt.Println("no I-Have responses")
		return
	}
	for _, o := range objects {
		fmt.Printf("device %d object %s name=%q at %s\n",
			o.DeviceInstance, o.Object, o.Name.Value, o.ImmediatePeer)
	}
	fmt.Printf("registry Objects()=%d\n", len(c.Objects()))
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
