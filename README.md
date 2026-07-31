# go-bacnet

[![Go](https://img.shields.io/badge/Go-1.23%2B-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

`go-bacnet` is a pure-Go BACnet/IP supervisory client and protocol foundation for
OT Fabric. Horizon 1 targets ANSI/ASHRAE 135-2024 (Protocol Revision 31 baseline)
over IPv4/UDP on port **47808** (`0xBAC0`).

**Status:** early v0.x **production-candidate**. Releases are not described as
**production-usable** until the [real-device gate](docs/REAL_DEVICE_GATE.md) is
met (≥2 independent BACnet/IP devices). Public Address, Value, transaction and
subscription APIs may still evolve.

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
- Who-Is / I-Am discovery
- ReadProperty, ReadPropertyMultiple, WriteProperty (priority + NULL relinquish)
- Confirmed-request transactions; segmented ComplexACK receive
- Routed BACnet networks; BBMD Forwarded-NPDU receive
- Optional foreign-device registration
- COV subscribe / notify / renew / cancel
- `bacnetctl` for decode, discover, read and write

**Out of scope (Horizon 1):**

- Native MS/TP, BACnet/IPv6, BACnet/SC
- Full BBMD server / multi-BBMD failover
- Full BACnet server / device object model
- Alarms, schedules, trends, WritePropertyMultiple
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
| [API.md](API.md) | Address/MAC/Value, lifecycle, retries, RPM, COV, ownership |
| [ERRORS.md](ERRORS.md) | Sentinels, remote PDUs, outcome-unknown |
| [SECURITY.md](SECURITY.md) | Trust model and vulnerability reporting |
| [INTEROP.md](INTEROP.md) | Interop ownership and `-tags=interop` |
| [RELEASE.md](RELEASE.md) | Versioning policy and history |
| [CONTRIBUTING.md](CONTRIBUTING.md) | Local checks and import boundaries |
| [docs/PACKAGE_DESIGN.md](docs/PACKAGE_DESIGN.md) | Package dependency rules |
| [docs/STANDARD_BASELINE.md](docs/STANDARD_BASELINE.md) | Normative baseline |
| [docs/CAPABILITY_MATRIX.md](docs/CAPABILITY_MATRIX.md) | Horizon 1 capabilities |
| [docs/REAL_DEVICE_GATE.md](docs/REAL_DEVICE_GATE.md) | Production-usable evidence bar |

### License

MIT — see [LICENSE](LICENSE).
