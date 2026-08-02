# Plan

## Current release

[go-bacnet v0.2.5](https://github.com/otfabric/go-bacnet/releases/tag/v0.2.5)
implements a broad BACnet/IP IPv4 supervisory client and is tested against
bacnet-stack, BACpypes3, BACnet4J, and Worldiety
([bacnet-interop v0.8.0](https://github.com/otfabric/bacnet-interop/releases/tag/v0.8.0)).

Status: **container-interoperability validated**; **field validation pending**
([FIELD_VALIDATION.md](docs/FIELD_VALIDATION.md)).

## Current priorities

1. Client completeness backlog below (P1 → P3).
2. Validate against physical BACnet/IP devices and record results in
   [COMPATIBILITY.md](docs/COMPATIBILITY.md).
3. Improve higher-level client ergonomics without hiding BACnet semantics.

## Client completeness backlog

Prioritize client value over indiscriminate enum completion.

### Priority 1 — Core client correctness

- Network-layer responses needed for routing
- BVLC management for FD/BBMD (Read-BDT / Read-FDT / Delete-FDT exercise)
- MaxAPDU-aware request construction
- Segmentation correctness
- Safe retry classification
- Cancellation and transaction cleanup
- Error / Reject / Abort completeness
- Long-lived COV and event subscription behavior

### Priority 2 — Broad supervisory interoperability

- Standard object/property identifiers
- Common constructed types
- Schedule / calendar and network-port types
- Event / notification parameter variants
- Commandable value handling
- Property-list / object-list helpers

### Priority 3 — Rare or specialized

- VT; audit; legacy authentication
- Specialized life-safety / access-control structures
- Rarely implemented network messages
- Services with little executable peer support

## Future

- Additional network-layer services
- BACnet/IPv6
- BACnet/SC
- MS/TP transport
- Optional supervisory helpers
- Server functionality
