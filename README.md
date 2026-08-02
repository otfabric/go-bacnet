# go-bacnet

[![Go](https://img.shields.io/badge/Go-1.23%2B-00ADD8?style=flat&logo=go)](https://go.dev/)
[![Go Reference](https://pkg.go.dev/badge/github.com/otfabric/go-bacnet.svg)](https://pkg.go.dev/github.com/otfabric/go-bacnet)
[![License](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![CI](https://github.com/otfabric/go-bacnet/actions/workflows/ci.yml/badge.svg)](https://github.com/otfabric/go-bacnet/actions/workflows/ci.yml)
[![Codecov](https://codecov.io/gh/otfabric/go-bacnet/graph/badge.svg)](https://codecov.io/gh/otfabric/go-bacnet)
[![Release](https://img.shields.io/github/v/release/otfabric/go-bacnet?label=release)](https://github.com/otfabric/go-bacnet/releases)

`go-bacnet` is a pure-Go BACnet/IP client for discovery, supervisory control,
event handling, and device management. It implements BACnet/IP over IPv4 with
routed addressing, segmentation, COV, alarm/event, file, object-management, and
selected advanced application services.

New to BACnet? Start with [PROTOCOL.md](PROTOCOL.md).

**Status:** [v0.2.5](https://github.com/otfabric/go-bacnet/releases/tag/v0.2.5).
The API is usable and extensively tested against four independent open-source
BACnet stacks ([bacnet-interop v0.8.0](https://github.com/otfabric/bacnet-interop/releases/tag/v0.8.0)).
Hardware interoperability testing is still incomplete
([FIELD_VALIDATION.md](docs/FIELD_VALIDATION.md)).

### Capability groups

1. **Transport and network** — BACnet/IP IPv4/UDP, BVLC/NPDU/APDU, routing,
   Forwarded-NPDU, optional foreign-device registration, segmentation
2. **Discovery and object access** — Who-Is/I-Am, Who-Has/I-Have, RP/RPM/WP/WPM,
   ReadRange, list element mutation
3. **Events, COV, and alarms** — SubscribeCOV, notifications, EventNotification,
   AcknowledgeAlarm, GetEventInformation, GetAlarmSummary, GetEnrollmentSummary
4. **File and object management** — AtomicRead/WriteFile, CreateObject,
   DeleteObject
5. **Advanced services** — PrivateTransfer, TextMessage, time synchronization,
   WriteGroup, Who-Am-I/You-Are, LifeSafetyOperation, audit/VT codecs, opt-in
   DCC/ReinitializeDevice

Full matrix: [docs/CLIENT_SUPPORT.md](docs/CLIENT_SUPPORT.md).  
Scenario-by-peer results: [INTEROP.md](INTEROP.md).

**Out of scope:** convenience/supervisor package; MS/TP; BACnet/IPv6; BACnet/SC;
BBMD server; full BACnet server/device model; BTL certification.

### Install

```bash
go get github.com/otfabric/go-bacnet@latest
```

Requires **Go 1.23** or newer.

### Getting started

```go
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/client"
)

func main() {
	c, err := client.New(client.WithPort(bip.DefaultPort)) // 47808 / 0xBAC0
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	devices, err := c.Discover(ctx, client.DiscoveryOptions{})
	// Discover returns when ctx ends; results are still useful.
	_ = err
	for _, d := range devices {
		fmt.Printf("device %d at %s (%s)\n", d.Instance, d.Address, d.Origin)
	}

	if len(devices) == 0 {
		return
	}
	d := devices[0]
	target := client.Target{
		Address:  d.Address,
		Endpoint: d.ImmediatePeer,
		Origin:   d.Origin,
	}
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: d.Instance}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName}
	val, err := c.ReadProperty(context.Background(), target, obj, prop)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("object-name: %+v\n", val)
}
```

Always check the error from `client.New` **before** `defer c.Close()`.
BACnet/IP peers use `bip.Endpoint` (not a root type).

### Documentation

| Document | Contents |
|----------|----------|
| [PROTOCOL.md](PROTOCOL.md) | BACnet primer |
| [API.md](API.md) | Client behavioural contract |
| [ERRORS.md](ERRORS.md) | Sentinels, remote PDUs, outcome-unknown |
| [docs/CLIENT_SUPPORT.md](docs/CLIENT_SUPPORT.md) | Capability and evidence levels |
| [INTEROP.md](INTEROP.md) | Scenario-by-peer results |
| [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md) | Peer quirks and hardware results |
| [docs/CLIENT_PROFILE.md](docs/CLIENT_PROFILE.md) | Descriptive standards profile |
| [docs/STANDARD_BASELINE.md](docs/STANDARD_BASELINE.md) | Normative baseline |
| [docs/FIELD_VALIDATION.md](docs/FIELD_VALIDATION.md) | Physical-device checklist |
| [PLAN.md](PLAN.md) | Forward plan |
| [RELEASE.md](RELEASE.md) | Release history |
| [CONTRIBUTING.md](CONTRIBUTING.md) | Local checks and interop how-to |
| [SECURITY.md](SECURITY.md) | Trust model and reporting |
| [`examples/`](examples/) | Runnable samples |

### License

MIT — see [LICENSE](LICENSE).
