# Plan

## Current release

[go-bacnet v0.3.0](https://github.com/otfabric/go-bacnet/releases/tag/v0.3.0)
implements a broad BACnet/IP IPv4 supervisory client (including the closed
client-completeness backlog) and was tested against bacnet-stack, BACpypes3,
BACnet4J, and Worldiety
([bacnet-interop v0.9.0](https://github.com/otfabric/bacnet-interop/releases/tag/v0.9.0)).

Status: **container-interoperability validated at pin**; **field validation
pending** ([FIELD_VALIDATION.md](docs/FIELD_VALIDATION.md)).

## Client-completeness backlog (closed)

Former P1–P3 items are implemented or explicitly bounded in
[CLIENT_SUPPORT.md](docs/CLIENT_SUPPORT.md) and [API.md](API.md):

| Area | Status |
|---|---|
| Confirmed-service policy vs segmentation capabilities; OperationClass | Done |
| Outcome-unknown for side-effecting services; InvokeConfirmed explicit opts | Done |
| Typed BVLC BDT/FDT + interop where BBMD adapters are executable | Done |
| Network-message codecs; transaction-stable multi-route state | Done |
| MaxAPDU audit; RPM batched (read-only shrink); WPM batch outcomes | Done |
| File stream/record helpers; ReadRange pager | Done |
| List/priority helpers; optional `device` snapshot (`PropertyReader`) | Done |
| Schedule/Calendar/HostNPort constructed types; property decode helpers | Done |
| NotificationParameters practical CHOICEs (incl. life-safety/extended) | Done |
| Optional `WithEventDispatcher`; fault-injection transport (`internal/faulty`) | Done |
| Access/life-safety enums; reviewed ID name maps | Done |
| Audit / Auth / VT | Codec/unit — no peer servers at pin |
| Physical-device campaign | Out of scope for this backlog |

## v0.4.0 (unreleased)

Review release: client hardening (invoke-ID reservation, receive-loop
backoff, option validation, rejection of unserved requests), codec
corrections against the standard, five peer stacks and Wireshark, named
remote errors, `slog` logging, discovery and addressing helpers, peer
coverage for the helper APIs, a Wireshark dissector oracle, and the move of
the CLI to [otfabric/bacnetctl](https://github.com/otfabric/bacnetctl). See
[RELEASE.md](RELEASE.md).

The interop pin is at bacnet-interop v0.10.0. After this version is tagged,
bacnetctl drops its local `replace` and requires it.

## Current priorities

1. Field validation against physical BACnet/IP devices
   ([FIELD_VALIDATION.md](docs/FIELD_VALIDATION.md) →
   [COMPATIBILITY.md](docs/COMPATIBILITY.md)).
2. Longer soak / combined-fault evidence under load.
3. Optional packaging of bacnet-stack native router in bacnet-interop if
   bip-router topology evidence is insufficient.
4. COV-multiple end to end: `SubscribeCOVMultiple` is a managed subscription
   and delivers whole notifications, but no stack executes the subscription,
   so subscribe → change → notify has not been seen against one device.
5. VT: handle VT-Data sent to the client (a session API). No peer has a VT
   session to test it against.
6. AuthRequest: a typed codec once a stable stack implements the service
   (BACnet4J 7.0 is the first to have one). All five peers refuse it today.

## Future

- BACnet/IPv6
- BACnet/SC
- MS/TP transport
- Full BACnet server / device model
