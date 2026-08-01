# go-bacnet Releases

## v0.1.1

**Date:** 2026-08-01  
**Previous release:** [v0.1.0](https://github.com/otfabric/go-bacnet/releases/tag/v0.1.0)

### Summary

Hardening and evidence release for release-grade **production-candidate**
status. Bounded device-observation retention, IPv4-only B/IP endpoint
validation, strict APDU/NPDU framing, hermetic CI coverage, PR and nightly
fuzz, and pin to [`bacnet-interop` v0.2.1](https://github.com/otfabric/bacnet-interop/releases/tag/v0.2.1)
(28 fixtures including service-layer negatives). Requires **Go 1.23+**.

Still not **production-usable** until the
[real-device gate](docs/REAL_DEVICE_GATE.md) is complete.

### Added

- `WithRegistryOptions` / bounded observation registry (TTL, global cap,
  per-instance path cap) with eviction diagnostics
- Window-scoped `Discover` results (`Devices()` remains the full snapshot)
- Strict APDU/NPDU framing rejections (reserved bits, undefined MaxAPDU,
  MoreFollows without SEG, invalid windows, global-broadcast DADR, router
  network lists)
- `npdu.DecodeNetworkList`
- CI **library coverage gate** (`make coverage`, hermetic pinned fixtures)
- CI **PR fuzz** and scheduled **nightly fuzz** (`fuzz-nightly.yml`)
- APDU/NPDU fuzz targets
- [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md) — positioning and evidence matrix

### Changed

- Horizon 1 `bip.Endpoint.IsValid` requires IPv4
- Inbound Distribute-Broadcast-To-Network ignored (client is not a BBMD)
- Diagnostic callbacks documented as synchronous
- Confirmed COV ACK-before-admission documented and tested
- Fixture expected-error helper supports multi-stage (APDU → service) failures
- Pin file → bacnet-interop `v0.2.1` digests
- PLAN / README: provisional PC until this tag; competitive positioning noted

### Evidence (Batch 4D)

| Gate | Result | Link |
|---|---|---|
| Shared CI (lint, Go 1.23–1.26, **race**) | green on `6256fd2` | [CI run](https://github.com/otfabric/go-bacnet/actions/runs/30705297706) |
| Library coverage gate (≥90%) | green | same run, job *Library coverage gate* |
| PR fuzz (15s/target) | green | same run, job *PR fuzz* |
| Pinned interop `v0.2.1` (run 1) | green | [30705297526](https://github.com/otfabric/go-bacnet/actions/runs/30705297526) |
| Pinned interop `v0.2.1` (run 2) | green | [30707779897](https://github.com/otfabric/go-bacnet/actions/runs/30707779897) |
| bacnet-interop main compat | green | both interop runs |
| Nightly fuzz (5m/target, manual) | green | [30707778922](https://github.com/otfabric/go-bacnet/actions/runs/30707778922) |
| Local `make test-race` + `make coverage` | 90.2% ≥ 90% | operator workstation, 2026-08-01 |

### Notes

- No public API break intended; additive options and stricter malformed
  rejection only.
- Segmented confirmed-request *send*, WPM, server/BBMD-server remain out of
  scope (see [PLAN.md](PLAN.md) Horizon 2+).

---

## v0.1.0

**Date:** 2026-08-01  
**Previous release:** none (first tagged release)

### Summary

First tagged release of a pure-Go BACnet/IP supervisory client and protocol
foundation. Provides BVLC/NPDU/APDU/service codecs, a UDP client runtime for
discovery and confirmed services, COV subscriptions, routed addressing, optional
foreign-device registration, and the `bacnetctl` CLI. Requires **Go 1.23+**.

Interoperability has been exercised against bacnet-stack, BACpypes3, and
BACnet4J peers (plus a bip-router topology aid) via digest-pinned images from
[`bacnet-interop`](https://github.com/otfabric/bacnet-interop) `v0.2.0`. This
is software-oracle evidence, not a claim of vendor or BTL certification.

### Added

#### Codecs and leaf types

- **BVLC** — Original-Unicast / Original-Broadcast / Forwarded-NPDU, BVLC-Result,
  Register-Foreign-Device encode/decode with length limits
- **NPDU** — local and routed (`DNET`/`DADR`, `SNET`/`SADR`) framing
- **APDU** — confirmed/unconfirmed request, SimpleACK, ComplexACK (including
  segmented receive), Error, Reject, Abort, SegmentACK
- **Service payloads** — Who-Is / I-Am, ReadProperty / ReadPropertyACK,
  ReadPropertyMultiple ACK (including per-property errors), WriteProperty
  (priority and NULL relinquish), SubscribeCOV / SubscribeCOVProperty,
  COV notification
- **Root types** — `Address` / `MAC`, object and property identifiers,
  application `Value` helpers, BACnet error class/code, decode limits

#### Client runtime

- **UDP BACnet/IP transport** on default port **47808** (`0xBAC0`), with
  injectable transport for tests
- **Who-Is / I-Am discovery** and a device observation registry (max APDU,
  segmentation, vendor)
- **ReadProperty**, **ReadPropertyMultiple**, **WriteProperty**
- **Confirmed-request transactions** — invoke IDs, timeouts, optional
  exact-APDU retransmission; abort / reject / error handling
- **Segmented ComplexACK reassembly** and SegmentACK exchange (segmented
  confirmed *request* send is not included)
- **Routed targets** — router cache, Who-Is-Router-To-Network, DNET/DADR path
  matching on responses
- **BBMD Forwarded-NPDU receive** and optional **foreign-device registration**
  (TTL renew / BVLC-Result correlation)
- **COV** — SubscribeCOV and SubscribeCOVProperty; confirmed and unconfirmed
  notifications; renew / cancel; subscription event channel with gap signalling
- **`bacnetctl`** — decode, discover, read, write, and version helpers

#### Documentation and evidence

- [PROTOCOL.md](PROTOCOL.md) — short BACnet primer mapped to this library
- [API.md](API.md), [ERRORS.md](ERRORS.md), [INTEROP.md](INTEROP.md),
  [SECURITY.md](SECURITY.md)
- Fixture corpus consumer for [`bacnet-interop`](https://github.com/otfabric/bacnet-interop)
  goldens (`BACNET_INTEROP_ROOT`)
- `-tags=interop` peer scenarios against pinned adapter digests
  (`interop/bacnet-interop-pin.json`)

### Not in this release

- Native MS/TP, BACnet/IPv6, BACnet/SC
- Full BBMD server / BDT management / multi-BBMD failover
- Full BACnet server or device object model
- Alarms, schedules, trends, WritePropertyMultiple
- Segmented confirmed-request *send*
- BTL certification or vendor hardware claims

### Notes

- Public APIs may still evolve in later v0.x tags; treat this as the first
  usable tagged baseline, not a stability freeze.
- Library unit coverage gate is **90%** (CLI and Docker interop packages
  excluded from the percentage).

---
