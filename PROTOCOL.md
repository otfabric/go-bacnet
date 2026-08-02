# BACnet protocol primer

Short orientation for readers new to BACnet who want to use `go-bacnet`.
This is not a substitute for ANSI/ASHRAE 135; it explains the vocabulary and
stack shape this library assumes.

## Table of contents

- [What BACnet is](#what-bacnet-is)
- [Objects and properties](#objects-and-properties)
- [Stack layers on BACnet/IP](#stack-layers-on-bacnetip)
- [Confirmed vs unconfirmed](#confirmed-vs-unconfirmed)
- [Discovery and addressing](#discovery-and-addressing)
- [Segmentation (BACnet/IP)](#segmentation-horizon-1)
- [What to read next](#what-to-read-next)

## What BACnet is

BACnet (Building Automation and Control Network) is an open protocol for
building and industrial control systems: HVAC, lighting, meters, and similar
devices. Peers exchange typed **application values** about **objects** and
their **properties**, rather than raw register maps.

Typical supervisory work looks like:

1. Find devices on the network (**Who-Is** / **I-Am**).
2. Read or write object properties (**ReadProperty**, **WriteProperty**, …).
3. Optionally subscribe to change-of-value (**COV**) notifications.

This library is a **BACnet/IP supervisory client** over IPv4/UDP
(default port **47808** / `0xBAC0`). It does not implement a full BACnet
server/device product, MS/TP, BACnet/IPv6, or BACnet/SC.

## Objects and properties

Everything interesting on a BACnet device is an **object** with a type and
instance number (for example `analog-input, 3` or `device, 1001`). Each object
exposes **properties** such as `present-value`, `object-name`, or
`units`.

Reads and writes name:

- an object identifier (`ObjectIdentifier`);
- a property reference (`PropertyReference`);
- optionally an array index or write priority.

Application data uses BACnet **application tags** (null, boolean, unsigned,
real, enumerated, character string, object identifier, and so on). In this
repo those land in the root `bacnet.Value` helpers.

## Stack layers on BACnet/IP

On the wire, a UDP datagram is peeled from the outside in:

```text
UDP datagram
  └─ BVLC     BACnet Virtual Link Control (BACnet/IP framing)
       └─ NPDU     Network Protocol Data Unit (routing / hop control)
            └─ APDU     Application Protocol Data Unit (service PDU)
                 └─ service payload   (Who-Is, ReadProperty, …)
```

| Layer | Role | Package here |
|-------|------|----------------|
| UDP / endpoint | IPv4 host:port peer | `bip` |
| BVLC | Original-Unicast/Broadcast, Forwarded-NPDU, foreign-device register, … | `bvlc` |
| NPDU | Local vs routed (`DNET`/`DADR`), hop count, message type | `npdu` |
| APDU | Confirmed/unconfirmed request, ACK, error, reject, abort, segment ACK | `apdu` |
| Service | Encode/decode of service payloads | `service` |
| Client | Sockets, transactions, discovery, routing, COV | `client` |

Receive composition in the runtime is:

`UDP → bvlc.Parse → npdu.Parse → apdu.Parse → service.Decode…`

## Confirmed vs unconfirmed

- **Unconfirmed** services (for example Who-Is, I-Am, some COV notifications)
  are fire-and-forget. There is no invoke ID and no application ACK.
- **Confirmed** services (ReadProperty, WriteProperty, SubscribeCOV, …) carry
  an **invoke ID**. The client waits for a matching ComplexACK, SimpleACK,
  Error, Reject, or Abort (subject to timeout / segmentation).

`go-bacnet`’s transaction manager owns invoke IDs, timeouts, and optional
exact-APDU retransmission for confirmed requests.

## Discovery and addressing

**Who-Is** asks devices in a range (or all devices) to announce themselves.
**I-Am** replies with device instance, max APDU length, segmentation support,
and vendor ID. The client records observations so later reads can target a
device without hard-coding every capability.

Addresses in BACnet are not always “just an IP”:

- **Local station** — IP:port style peer on the same BACnet/IP network.
- **Remote station** — device reached via a BACnet router (`DNET` + `DADR`).
- **Broadcast / global broadcast** — used for discovery and some network
  messages.

BACnet/IP also uses:

- **BBMD** (BACnet Broadcast Management Device) — forwards broadcasts across
  IP subnets; This library receives **Forwarded-NPDU** and can act as a foreign
  device toward a BBMD.
- **Foreign device registration** — optional registration with a BBMD so a
  client on another subnet receives broadcasts.
- **Routers** — forward NPDUs between BACnet network numbers; this library
  maintains a router cache for routed confirmed traffic.

## Segmentation

Large ComplexACKs may arrive as **segments**. The client reassembles segmented
ComplexACK responses and exchanges SegmentACK as required (default receive
window 1). When a confirmed request exceeds the remote max APDU and peer
evidence shows the device accepts segmented requests (`segmented-both` or
`segmented-receive`), the client sends windowed segmented confirmed requests
(default proposed window 16; actual window follows SegmentACK) before waiting
for the service response.

## Framing policy (BACnet/IP)

APDU and NPDU parsers are **strict by default**: reserved header bits,
undefined MaxAPDU codes, MoreFollows without SegmentedMessage, zero or
out-of-range segment windows, global broadcast with a non-zero DADR, and
malformed router network lists are rejected as malformed rather than
normalized away. There is no separate compatibility profile in this library.

## What to read next

| Document | When |
|----------|------|
| [README.md](README.md) | Install and first client example |
| [API.md](API.md) | Ownership, discovery, routing, FD, RPM, COV contracts |
| [ERRORS.md](ERRORS.md) | Local vs remote errors, outcome-unknown writes |
| [docs/PACKAGE_DESIGN.md](docs/PACKAGE_DESIGN.md) | Import boundaries and layering |
| [docs/STANDARD_BASELINE.md](docs/STANDARD_BASELINE.md) | ASHRAE edition / revision baseline |
| [INTEROP.md](INTEROP.md) | Peer oracle evidence and `-tags=interop` |

For normative detail, use a licensed copy of ANSI/ASHRAE 135 and this repo’s
baseline notes — do not treat this primer as conformance language.
