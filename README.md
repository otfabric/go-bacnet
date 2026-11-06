# go-bacnet

[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8?style=flat&logo=go)](https://go.dev/)
[![Go Reference](https://pkg.go.dev/badge/github.com/otfabric/go-bacnet.svg)](https://pkg.go.dev/github.com/otfabric/go-bacnet)
[![License](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![CI](https://github.com/otfabric/go-bacnet/actions/workflows/ci.yml/badge.svg)](https://github.com/otfabric/go-bacnet/actions/workflows/ci.yml)
[![Codecov](https://codecov.io/gh/otfabric/go-bacnet/graph/badge.svg)](https://codecov.io/gh/otfabric/go-bacnet)
[![Release](https://img.shields.io/github/v/release/otfabric/go-bacnet?label=release)](https://github.com/otfabric/go-bacnet/releases)

`go-bacnet` is a pure-Go BACnet/IP (Annex J, IPv4) library with a client and
a server. The client discovers devices, reads and writes properties,
subscribes to change-of-value and alarm notifications, transfers files and
manages objects, including over routed networks, foreign-device registration
and segmentation. The server (`server`, `server/object`, `bbmd`) serves a
BACnet device of your own, with its objects, on the same codecs, and can act
as a BBMD. It has no dependencies outside the Go standard library.

New to BACnet? Start with [PROTOCOL.md](PROTOCOL.md).

## Status

A client and, since v0.6.0, a server. The client is tested on every change against five independent
open-source BACnet stacks running in containers (bacnet-stack, BACpypes3,
BACnet4J, Worldiety, System.IO.BACnet; [INTEROP.md](INTEROP.md)) and its
requests are checked against Wireshark's dissector. It has **not** been
validated against physical controllers yet; the checklist for that is
[docs/FIELD_VALIDATION.md](docs/FIELD_VALIDATION.md). The server is
tested against the go-bacnet client and against three independent clients,
the bacnet-stack command-line tools, BACnet4J and BACpypes3 in bacnet-interop
client-mode containers (discovery, property access, COV, ReadRange, files,
lists, object creation, alarms, device management, segmentation and the
BBMD), and its replies are checked against Wireshark's dissector
([docs/SERVER_SUPPORT.md](docs/SERVER_SUPPORT.md)). The API is v0 and can
still change between minor versions; [RELEASE.md](RELEASE.md) lists every
change.

## Scope

| | |
|---|---|
| Transport and network | BACnet/IP over IPv4/UDP, BVLC, routed addressing (DNET/DADR), Forwarded-NPDU, foreign-device registration with one BBMD, BDT/FDT management, segmented requests and responses |
| Discovery | Who-Is / I-Am, Who-Has / I-Have, Who-Is-Router-To-Network |
| Object access | ReadProperty, ReadPropertyMultiple, WriteProperty, WritePropertyMultiple, ReadRange, Add/RemoveListElement, CreateObject, DeleteObject, AtomicRead/WriteFile |
| Notifications | SubscribeCOV, SubscribeCOVProperty, SubscribeCOVPropertyMultiple, COV and EventNotification reception, AcknowledgeAlarm, GetEventInformation, GetAlarmSummary, GetEnrollmentSummary |
| Other services | time synchronization, text message, private transfer, WriteGroup, LifeSafetyOperation, and (opt-in) DeviceCommunicationControl and ReinitializeDevice |

The complete matrix, with how well each capability is validated, is
[docs/CLIENT_SUPPORT.md](docs/CLIENT_SUPPORT.md).

The server (`server`, `server/object`, optional `bbmd`) serves a Device with
analog, binary and multi-state points, Notification Class, Trend Log, File,
Calendar, Schedule, CharacterString / Integer / Positive Integer Value and
Structured View objects: the data-sharing, list, file, COV, alarm and event, and
device-management services above, segmentation in both directions, and a
BBMD. Its matrix is [docs/SERVER_SUPPORT.md](docs/SERVER_SUPPORT.md); a
runnable example is [server/example_test.go](server/example_test.go).

**Not in scope:** routing, MS/TP, BACnet/IPv6, BACnet/SC, BTL certification. BACnet/IP has no
authentication or encryption and this library adds none
([SECURITY.md](SECURITY.md)).

A command-line client built on this library lives in
[otfabric/bacnetctl](https://github.com/otfabric/bacnetctl).

## Install

```bash
go get github.com/otfabric/go-bacnet@latest
```

Requires **Go 1.26** or newer.

## Getting started

```go
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/client"
)

func main() {
	c, err := client.New() // binds UDP port 47808
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()

	ctx := context.Background()
	devices, err := c.Discover(ctx, client.DiscoveryOptions{Window: 3 * time.Second})
	if err != nil {
		log.Fatal(err)
	}
	name := bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName}
	for _, d := range devices {
		val, err := c.ReadProperty(ctx, d.Target(), d.DeviceID(), name)
		if err != nil {
			fmt.Printf("device %d at %s: %v\n", d.Instance, d.Address, err)
			continue
		}
		fmt.Printf("device %d at %s: %s\n", d.Instance, d.Address, val.Character.Value)
	}
}
```

A device whose address you already know needs no discovery:

```go
c, err := client.New(client.WithLocalAddr("0.0.0.0:0")) // any free local port
// ...
target := client.Target{Endpoint: bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.20:47808"))}
av1 := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
pv, err := c.ReadProperty(ctx, target, av1, bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue})
```

### Serving a device

```go
dev, err := object.NewDevice(object.DeviceConfig{Instance: 1234, Name: "dev-1234", VendorID: 999})
// ...
av, err := object.NewAnalogValue(object.PointConfig{Instance: 1, Name: "setpoint", Units: 62, Value: 21, Commandable: true})
// ...
if err := dev.Add(av); err != nil {
	log.Fatal(err)
}
s, err := server.New(dev) // binds UDP port 47808
if err != nil {
	log.Fatal(err)
}
defer s.Close()
_ = s.IAm()
// The application sets the values it owns; COV subscribers are notified.
_ = av.SetPresentValue(bacnet.RealValue(22))
```

These and further examples (writing with a priority, COV subscriptions,
logging, a complete server) are compiled as part of the test suite: see
[client/example_test.go](client/example_test.go),
[server/example_test.go](server/example_test.go) and [`examples/`](examples/)
([`examples/server`](examples/server) runs a device until interrupted).

## Things to know before you use it

- **One client, one socket.** A `Client` owns one UDP socket and is safe for
  concurrent use; requests are multiplexed on it by invoke ID. Create one per
  local interface and keep it; `Close` releases the socket and fails pending
  requests with `bacnet.ErrClosed`.
- **Port 47808.** Devices answer Who-Is with a *broadcast* to port 47808, so
  discovery needs the client bound there (the default), and only one process
  on a host can be. Unicast requests work from any port
  (`WithLocalAddr("0.0.0.0:0")`).
- **Broadcasts stop at routers.** A Who-Is goes to the broadcast address of
  every local subnet. To discover devices on another IP subnet, register as a
  foreign device with a BBMD there (`WithForeignDevice`) or send a unicast
  Who-Is (`SendWhoIs`). `WithInterface` restricts broadcasts to one interface.
- **Timeouts.** A request without an answer fails with `bacnet.ErrTimeout`
  after the APDU timeout (3 s) times the attempts (reads are retransmitted
  3 times; writes never). A context deadline or cancellation ends it earlier
  and is preserved for `errors.Is`. See `WithTransactionOptions`.
- **Writes can be ambiguous.** A write is sent once. If no answer arrives the
  device may or may not have applied it: the call returns
  `*bacnet.OutcomeUnknownError`, never a plain timeout. Read the value back
  before repeating a write that is not idempotent.
- **Errors are typed.** A device's Error, Reject and Abort PDUs come back as
  `*bacnet.ErrorResponse`, `*bacnet.RejectError` and `*bacnet.AbortError`
  with standard names; ReadPropertyMultiple reports per-property errors inside
  its results. [ERRORS.md](ERRORS.md) has the whole taxonomy.
- **Dangerous services are opt-in.** DeviceCommunicationControl,
  ReinitializeDevice and Write-BDT fail locally unless enabled with
  `WithDeviceManagementEnabled` / `WithNetworkManagementEnabled`.
- **Silent by default.** No logging unless you pass `WithLogger`
  ([OBSERVABILITY.md](OBSERVABILITY.md)).
- **The server answers what its objects hold.** `server.New` takes a
  `*object.Device`; objects added to it are served at once. Writes from
  clients go through `Hooks.BeforeWrite`, which can refuse them with a BACnet
  error. The application updates input values with `SetPresentValue`. State
  is kept in memory only: persisting it across restarts is the
  application's ([LIMITATIONS.md](LIMITATIONS.md),
  [docs/SERVER_SUPPORT.md](docs/SERVER_SUPPORT.md)).
- **Character strings.** `CharacterString.Value` holds the octets as received.
  Use `Text()` to get a Go string from devices that send UCS-2 or ISO 8859-1.

## Documentation

| Document | Contents |
|----------|----------|
| [PROTOCOL.md](PROTOCOL.md) | BACnet primer |
| [API.md](API.md) | Client and server behavioural contract: lifecycle, ownership, timeouts, result models |
| [ERRORS.md](ERRORS.md) | Sentinels, remote PDUs, outcome-unknown |
| [OBSERVABILITY.md](OBSERVABILITY.md) | Logging and diagnostics |
| [ARCHITECTURE.md](ARCHITECTURE.md) | Packages, dependency rules, client and server runtime |
| [docs/CLIENT_SUPPORT.md](docs/CLIENT_SUPPORT.md) | Every client capability and its evidence level |
| [docs/SERVER_SUPPORT.md](docs/SERVER_SUPPORT.md) | Every server capability and its evidence level |
| [docs/SERVER_PLAN.md](docs/SERVER_PLAN.md) | Server scope, phases and recorded deviations |
| [INTEROP.md](INTEROP.md) | Scenario-by-peer results and how to run them |
| [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md) | Peer quirks and hardware results |
| [docs/CLIENT_PROFILE.md](docs/CLIENT_PROFILE.md) | Descriptive standards profile |
| [docs/STANDARD_BASELINE.md](docs/STANDARD_BASELINE.md) | Normative baseline |
| [docs/FIELD_VALIDATION.md](docs/FIELD_VALIDATION.md) | Physical-device checklist |
| [PLAN.md](PLAN.md) | Forward plan |
| [RELEASE.md](RELEASE.md) | Release history |
| [CONTRIBUTING.md](CONTRIBUTING.md) | Local checks and interop how-to |
| [TESTING.md](TESTING.md) | Test tiers, prerequisites, coverage and fuzz procedure |
| [LIMITATIONS.md](LIMITATIONS.md) | What is not implemented or not verified |
| [SECURITY.md](SECURITY.md) | Trust model and reporting |

## License

MIT — see [LICENSE](LICENSE).
