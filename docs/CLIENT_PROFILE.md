# Client profile

Descriptive BACnet client implementation profile for `go-bacnet`.

> This is a descriptive implementation profile, not a BTL-certified PICS or
> formal conformance claim.

Capability detail and validation levels: [CLIENT_SUPPORT.md](CLIENT_SUPPORT.md).  
Peer results: [INTEROP.md](../INTEROP.md).  
Standards edition: [STANDARD_BASELINE.md](STANDARD_BASELINE.md).

## Protocol baseline

| Item | Value |
|---|---|
| Standard | ANSI/ASHRAE 135-2024 |
| Protocol Revision baseline | 31 |
| Conformance mapping reference | ANSI/ASHRAE 135.1-2023 (descriptive) |
| Role | A-side supervisory / commissioning client |
| Remote adaptation | Adapts to remote I-Am / Device object capabilities; remotes need not implement PR31 |

## Data link

| Option | Support |
|---|---|
| BACnet/IP Annex J over IPv4/UDP | Supported (default port 47808 / `0xBAC0`) |
| Foreign-device registration | Supported (optional) |
| BBMD Forwarded-NPDU receive | Supported |
| BBMD server | Not supported |
| BACnet/IPv6 | Not supported |
| BACnet/SC | Not supported |
| MS/TP | Not supported |

## Segmentation

| Capability | Support |
|---|---|
| Segmented ComplexACK receive | Supported (receive window 1) |
| Segmented confirmed-request transmit | Supported (proposed window 16 when peer allows) |
| Segmented confirmed-request receive (as server) | Not applicable (client library) |

## Character sets

| Encoding | Support |
|---|---|
| UTF-8 CharacterString | Supported |
| Other CharacterString encodings | Accepted on decode where tags allow; prefer UTF-8 on encode |

## Service roles (summary)

| Role | Scope |
|---|---|
| Initiate | Discovery, property access, COV subscribe, alarm/event acknowledge and queries, file, object lifecycle, list mutation, messaging, time sync, identity, life-safety, VT, audit codecs, DCC/Reinitialize (opt-in) |
| Execute / receive | I-Am, I-Have, COV notifications, EventNotifications, I-Am-Router, Forwarded-NPDU, network messages needed for client routing |
| Server / device object model | Not provided |

For per-service implementation and evidence, see [CLIENT_SUPPORT.md](CLIENT_SUPPORT.md).

## Device-management assumptions

- Controllers and devices are remote peers; this library does not expose a BACnet device object.
- Destructive services (DCC, ReinitializeDevice, writes) require explicit caller opt-in and safe operational practice.
- Vendor extensions and newer revision constructs are preserved as raw ASN.1 where typed models are incomplete.

## Explicitly unsupported

- BACnet server / BBMD server product roles
- Native MS/TP, BACnet/IPv6, BACnet/SC
- BTL listing or formal PICS submission
