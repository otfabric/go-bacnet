// SPDX-License-Identifier: MIT

// Command bacnetctl is a small BACnet/IP utility for decode, discover, read and write.
package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/npdu"
)

// Set via -ldflags at build / release time:
//
//	-X main.version=${VERSION}
//	-X main.tag=${TAG}
//	-X main.commit=${COMMIT}
//	-X main.buildDate=${BUILD_DATE}
var (
	version   = "dev"
	tag       = "none"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "decode":
		err = runDecode(os.Args[2:])
	case "discover":
		err = runDiscover(os.Args[2:])
	case "read":
		err = runRead(os.Args[2:])
	case "write":
		err = runWrite(os.Args[2:])
	case "version":
		runVersion()
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "bacnetctl: %v\n", err)
		os.Exit(1)
	}
}

func runVersion() {
	fmt.Printf("bacnetctl %s\n", version)
	fmt.Printf("  tag:        %s\n", tag)
	fmt.Printf("  commit:     %s\n", commit)
	fmt.Printf("  built:      %s\n", buildDate)
}

func usage() {
	fmt.Fprintf(os.Stderr, `bacnetctl — BACnet/IP helper

Usage:
  bacnetctl decode  --hex <hex>
  bacnetctl discover [--timeout 3s] [--port 47808] [--local 0.0.0.0:47808]
  bacnetctl read    --addr host:port --object type:inst --property id [--timeout 3s]
  bacnetctl write   --addr host:port --object type:inst --property id --value <spec> [--priority N]
  bacnetctl version

Value specs: null | bool:true | unsigned:1 | signed:-1 | real:21.5 | enum:2
`)
}

func runDecode(args []string) error {
	fs := flag.NewFlagSet("decode", flag.ExitOnError)
	hexStr := fs.String("hex", "", "datagram as hex (spaces optional)")
	_ = fs.Parse(args)
	if *hexStr == "" {
		return fmt.Errorf("--hex is required")
	}
	raw, err := hex.DecodeString(strings.ReplaceAll(*hexStr, " ", ""))
	if err != nil {
		return fmt.Errorf("hex: %w", err)
	}
	limits := bacnet.DefaultDecodeLimits()
	msg, err := bvlc.Parse(raw, limits)
	if err != nil {
		return fmt.Errorf("bvlc: %w", err)
	}
	fmt.Printf("BVLC function=%v payload_len=%d\n", msg.Function, len(msg.Payload))
	if len(msg.Payload) == 0 {
		return nil
	}
	n, _, err := npdu.Parse(msg.Payload, limits)
	if err != nil {
		return fmt.Errorf("npdu: %w", err)
	}
	fmt.Printf("NPDU version=%d network_message=%v src=%s dst=%s apdu_len=%d\n",
		n.Version, n.NetworkMessage, n.Source, n.Destination, len(n.APDU))
	if n.NetworkMessage || len(n.APDU) == 0 {
		return nil
	}
	pdu, err := apdu.Parse(n.APDU, limits)
	if err != nil {
		return fmt.Errorf("apdu: %w", err)
	}
	fmt.Printf("APDU type=%v\n", pdu.Type)
	return nil
}

func runDiscover(args []string) error {
	fs := flag.NewFlagSet("discover", flag.ExitOnError)
	timeout := fs.Duration("timeout", 3*time.Second, "collection window")
	port := fs.Int("port", bip.DefaultPort, "BACnet/IP UDP port")
	local := fs.String("local", "", "optional bind address host:port")
	_ = fs.Parse(args)

	opts := []client.Option{client.WithPort(*port)}
	if *local != "" {
		opts = append(opts, client.WithLocalAddr(*local))
	}
	c, err := client.New(opts...)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	devices, err := c.Discover(ctx, client.DiscoveryOptions{})
	for _, d := range devices {
		fmt.Printf("instance=%d address=%s origin=%s peer=%s maxAPDU=%v seg=%v vendor=%v\n",
			d.Instance, d.Address, d.Origin, d.ImmediatePeer,
			d.Capabilities.MaxAPDULengthAccepted,
			d.Capabilities.Segmentation,
			d.Capabilities.VendorID,
		)
	}
	if err != nil && err != context.DeadlineExceeded && err != context.Canceled {
		return err
	}
	return nil
}

func runRead(args []string) error {
	fs := flag.NewFlagSet("read", flag.ExitOnError)
	addr := fs.String("addr", "", "device host:port")
	object := fs.String("object", "", "type:instance")
	property := fs.String("property", "", "property identifier (number)")
	timeout := fs.Duration("timeout", 3*time.Second, "request timeout")
	port := fs.Int("port", bip.DefaultPort, "local UDP port")
	_ = fs.Parse(args)
	if *addr == "" || *object == "" || *property == "" {
		return fmt.Errorf("--addr, --object and --property are required")
	}
	target, err := parseTarget(*addr)
	if err != nil {
		return err
	}
	obj, err := parseObject(*object)
	if err != nil {
		return err
	}
	prop, err := parseProperty(*property)
	if err != nil {
		return err
	}

	c, err := client.New(client.WithPort(*port))
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	val, err := c.ReadProperty(ctx, target, obj, prop)
	if err != nil {
		return err
	}
	fmt.Printf("%+v\n", val)
	return nil
}

func runWrite(args []string) error {
	fs := flag.NewFlagSet("write", flag.ExitOnError)
	addr := fs.String("addr", "", "device host:port")
	object := fs.String("object", "", "type:instance")
	property := fs.String("property", "", "property identifier (number)")
	value := fs.String("value", "", "value spec (null|bool:|unsigned:|signed:|real:|enum:)")
	priority := fs.Int("priority", 0, "optional write priority 1-16 (0 = omit)")
	timeout := fs.Duration("timeout", 3*time.Second, "request timeout")
	port := fs.Int("port", bip.DefaultPort, "local UDP port")
	_ = fs.Parse(args)
	if *addr == "" || *object == "" || *property == "" || *value == "" {
		return fmt.Errorf("--addr, --object, --property and --value are required")
	}
	target, err := parseTarget(*addr)
	if err != nil {
		return err
	}
	obj, err := parseObject(*object)
	if err != nil {
		return err
	}
	prop, err := parseProperty(*property)
	if err != nil {
		return err
	}
	val, err := parseValue(*value)
	if err != nil {
		return err
	}
	var pri *uint8
	if *priority > 0 {
		if *priority > 16 {
			return fmt.Errorf("priority must be 1-16")
		}
		p := uint8(*priority)
		pri = &p
	}

	c, err := client.New(client.WithPort(*port))
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	return c.WriteProperty(ctx, target, obj, prop, val, pri)
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
		return bacnet.ObjectIdentifier{}, fmt.Errorf("object type: %w", err)
	}
	inst, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil {
		return bacnet.ObjectIdentifier{}, fmt.Errorf("object instance: %w", err)
	}
	return bacnet.ObjectIdentifier{Type: bacnet.ObjectType(t), Instance: uint32(inst)}, nil
}

func parseProperty(s string) (bacnet.PropertyReference, error) {
	id, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return bacnet.PropertyReference{}, fmt.Errorf("property: %w", err)
	}
	return bacnet.PropertyReference{Identifier: bacnet.PropertyIdentifier(id)}, nil
}

func parseValue(s string) (bacnet.ApplicationValue, error) {
	if s == "null" {
		return bacnet.NullValue(), nil
	}
	kind, rest, ok := strings.Cut(s, ":")
	if !ok {
		return bacnet.ApplicationValue{}, fmt.Errorf("value spec must be null or kind:payload")
	}
	switch kind {
	case "bool":
		b, err := strconv.ParseBool(rest)
		if err != nil {
			return bacnet.ApplicationValue{}, err
		}
		return bacnet.BoolValue(b), nil
	case "unsigned":
		u, err := strconv.ParseUint(rest, 10, 64)
		if err != nil {
			return bacnet.ApplicationValue{}, err
		}
		return bacnet.UnsignedValue(u), nil
	case "signed":
		v, err := strconv.ParseInt(rest, 10, 64)
		if err != nil {
			return bacnet.ApplicationValue{}, err
		}
		return bacnet.SignedValue(v), nil
	case "real":
		f, err := strconv.ParseFloat(rest, 32)
		if err != nil {
			return bacnet.ApplicationValue{}, err
		}
		return bacnet.RealValue(float32(f)), nil
	case "enum":
		u, err := strconv.ParseUint(rest, 10, 32)
		if err != nil {
			return bacnet.ApplicationValue{}, err
		}
		return bacnet.EnumValue(uint32(u)), nil
	default:
		return bacnet.ApplicationValue{}, fmt.Errorf("unsupported value kind %q", kind)
	}
}
