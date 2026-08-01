# go-bacnet

[![Go](https://img.shields.io/badge/Go-1.23%2B-00ADD8?style=flat&logo=go)](https://go.dev/)
[![Go Reference](https://pkg.go.dev/badge/github.com/otfabric/go-bacnet.svg)](https://pkg.go.dev/github.com/otfabric/go-bacnet)
[![License](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![CI](https://github.com/otfabric/go-bacnet/actions/workflows/ci.yml/badge.svg)](https://github.com/otfabric/go-bacnet/actions/workflows/ci.yml)
[![Codecov](https://codecov.io/gh/otfabric/go-bacnet/graph/badge.svg)](https://codecov.io/gh/otfabric/go-bacnet)
[![Release](https://img.shields.io/github/v/release/otfabric/go-bacnet?label=release)](https://github.com/otfabric/go-bacnet/releases)

`go-bacnet` is a pure-Go BACnet/IP supervisory client and protocol foundation for
OT Fabric. Horizon 1 targets ANSI/ASHRAE 135-2024 (Protocol Revision 31 baseline)
over IPv4/UDP on port **47808** (`0xBAC0`).

New to BACnet? Start with [PROTOCOL.md](PROTOCOL.md) for a short primer on
objects, BVLC/NPDU/APDU layering, discovery, and how those map to this library.

**Status:** early v0.x **production-candidate** — ready to tag as **`v0.2.0`**
(WritePropertyMultiple, ReadRange, Who-Has, EventNotification, opt-in device
management, segmented confirmed-request send; green CI + pinned interop on
[`bacnet-interop` v0.4.1](https://github.com/otfabric/bacnet-interop/releases/tag/v0.4.1);
see [RELEASE.md](RELEASE.md)). Not **production-usable** until the
[real-device gate](docs/REAL_DEVICE_GATE.md) (≥2 independent BACnet/IP devices).

| Label | Meaning |
|-------|---------|
| **alpha** | Pre-hardening / incomplete evidence |
| **production-candidate** | Current — wire/runtime + supervisory client services + reproducible pinned multi-peer evidence |
| **production-usable** | [Real-device gate](docs/REAL_DEVICE_GATE.md) met (≥2 independent BACnet/IP devices) |

Public Address, Value, transaction and subscription APIs may still evolve.

### Table of contents

- [Scope](#scope)
- [Install](#install)
- [Getting started](#getting-started)
- [Documentation](#documentation)
- [License](#license)

### Scope

**In scope (Horizon 1):**

- BACnet/IP over IPv4/UDP (default port 47808 / `0xBAC0`)
- BVLC, NPDU and APDU codecs
- Who-Is / I-Am and Who-Has / I-Have discovery
- ReadProperty, ReadPropertyMultiple, WriteProperty (priority + NULL relinquish)
- WritePropertyMultiple (first-failed Error model; outcome-unknown after send)
- ReadRange (by position / sequence / time)
- Confirmed-request transactions; segmented ComplexACK receive
- Segmented confirmed-request send (windowed; when peer Segmentation is both/receive)
- Routed BACnet networks; BBMD Forwarded-NPDU receive
- Optional foreign-device registration
- COV subscribe / notify / renew / cancel
- EventNotification receive (typed common NotificationParameters); AcknowledgeAlarm; GetEventInformation
- ReadRange with typed Trend Log `LogRecords` when applicable
- DeviceCommunicationControl / ReinitializeDevice (explicit opt-in)
- `bacnetctl` for decode, discover, read and write
- Runnable H2 examples under `examples/`

**Out of scope (Horizon 3+ / later):**

- Convenience / supervisor package (Horizon 3)
- Native MS/TP, BACnet/IPv6, BACnet/SC
- Full BBMD server / multi-BBMD failover
- Full BACnet server / device object model
- Schedules; uncommon event-parameter CHOICEs; GetAlarmSummary
- BTL certification itself

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
| [PROTOCOL.md](PROTOCOL.md) | BACnet primer for newcomers (objects, layers, discovery) |
| [API.md](API.md) | Address/MAC/Value, lifecycle, discovery, routing, FD, RPM, COV |
| [ERRORS.md](ERRORS.md) | Sentinels, remote PDUs, outcome-unknown |
| [SECURITY.md](SECURITY.md) | Trust model and vulnerability reporting |
| [INTEROP.md](INTEROP.md) | Peer/topology scenarios and `-tags=interop` |
| [RELEASE.md](RELEASE.md) | Versioning policy and history |
| [CONTRIBUTING.md](CONTRIBUTING.md) | Local checks and import boundaries |
| [PLAN.md](PLAN.md) | Evidence batches and forward plan |
| [`examples/`](examples/) | Runnable samples (ReadRange, WPM, Who-Has, events) |
| [docs/PACKAGE_DESIGN.md](docs/PACKAGE_DESIGN.md) | Package dependency rules |
| [docs/STANDARD_BASELINE.md](docs/STANDARD_BASELINE.md) | Normative baseline |
| [docs/CAPABILITY_MATRIX.md](docs/CAPABILITY_MATRIX.md) | Client capability matrix |
| [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md) | Peer/device evidence matrix and positioning |
| [docs/REAL_DEVICE_GATE.md](docs/REAL_DEVICE_GATE.md) | Production-usable evidence bar |

### License

MIT — see [LICENSE](LICENSE).
