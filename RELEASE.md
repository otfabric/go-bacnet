# go-bacnet Releases

## v0.2.2

**Date:** 2026-08-02  
**Previous release:** [v0.2.1](https://github.com/otfabric/go-bacnet/releases/tag/v0.2.1)  
**Tag:** [`v0.2.2`](https://github.com/otfabric/go-bacnet/releases/tag/v0.2.2) @ `a277aea`

### Summary

Service-breadth minor: Worldiety as a fourth digest-pinned peer oracle, plus
codecs/APIs for alarm/enrollment/COV-multiple, file, list/lifecycle, messaging,
audit/identity, and life-safety/VT. Pin moves to
[`bacnet-interop` v0.5.0](https://github.com/otfabric/bacnet-interop/releases/tag/v0.5.0).
Requires **Go 1.23+**.

Still **production-candidate** until the
[real-device gate](docs/REAL_DEVICE_GATE.md) is complete.

### Added

- Worldiety as a fourth digest-pinned peer oracle (`interop/worldiety_test.go`):
  Who-Is/Who-Has, RP/RPM/WP/WPM, ReadRange (unsegmented)
- GetAlarmSummary / GetEnrollmentSummary / SubscribeCOVPropertyMultiple
- Confirmed/Unconfirmed COVNotificationMultiple receive (+ encode helper)
- OpenEventStream / TransitionOf; OpenAuditStream
- AtomicReadFile / AtomicWriteFile (+ FileChunkBounds)
- AddListElement / RemoveListElement / CreateObject / DeleteObject
- Confirmed/Unconfirmed PrivateTransfer and TextMessage; TimeSynchronization /
  UTCTimeSynchronization; WriteGroup
- AuditLogQuery / AuthRequest / Who-Am-I / You-Are; audit notification receive
- LifeSafetyOperation; VT-Open / VT-Close / VT-Data
- Additional NotificationParameters typed CHOICEs (command-failure, floating-limit,
  buffer-ready, unsigned-range)
- Request decoders for the new confirmed/unconfirmed services
- Codec goldens for the new services via `bacnet-interop/fixtures/codec`

### Changed

- CI pinned interop lane pulls and runs Worldiety digests from `v0.5.0`
- Interop pin file includes `worldiety` image digest
- Portable loopback interface selection for coverage-stable `WithInterface` tests
  (Linux `lo` / Darwin `lo0`)

### Evidence

Green on tag SHA `a277aea`:

| Gate | Result | Link |
|---|---|---|
| Shared CI (lint, Go matrix, race, coverage, PR fuzz) | green | [30757758929](https://github.com/otfabric/go-bacnet/actions/runs/30757758929) |
| Pinned interop `v0.5.0` (+ Worldiety) | green | [30757758742](https://github.com/otfabric/go-bacnet/actions/runs/30757758742) (job *Pinned release peers*) |
| bacnet-interop main compat | green | same run (job *bacnet-interop main compat*) |
| Pin | [`bacnet-interop` v0.5.0](https://github.com/otfabric/bacnet-interop/releases/tag/v0.5.0) | `interop/bacnet-interop-pin.json` |
| GitHub Release | published | [v0.2.2](https://github.com/otfabric/go-bacnet/releases/tag/v0.2.2) |

### Notes / known skips

- Worldiety segmented WPM/RPM skipped (peer segment service-choice — B6)
- BACnet4J AtomicReadFile live path skipped (Error services/10 — B8)
- BACnet4J CreateObject / DeleteObject live path skipped (config — B9)
- Broader live multi-peer coverage for event/COV-multiple, file, list/lifecycle,
  messaging, audit, life-safety/VT, and topology/bbmd v2 modes remains partial
  (see `bacnet-interop/BLOCKERS.md`)
- Library coverage gate held at ≥90%

---

## v0.2.1

**Date:** 2026-08-02  
**Previous release:** [v0.2.0](https://github.com/otfabric/go-bacnet/releases/tag/v0.2.0)

### Summary

Correctness and decoder-strictness patch on the `v0.2.0` supervisory-client
surface: service-choice correlation, segmented confirmed-request overhead and
SegmentACK direction, bounded segmentation control sends, broader
outcome-unknown wrapping, EventNotification receive-path safety, and fail-closed
decoders for WPM / ReadRange / events / DCC / ReinitializeDevice. Pin moves to
[`bacnet-interop` v0.4.2](https://github.com/otfabric/bacnet-interop/releases/tag/v0.4.2).
Requires **Go 1.23+**.

Still **production-candidate** until the
[real-device gate](docs/REAL_DEVICE_GATE.md) is complete.

### Fixed

- Service-choice correlation no longer treats choice `0` (AcknowledgeAlarm) as
  “unknown”; SimpleACK / ComplexACK / Error and every ComplexACK segment compare
  unconditionally
- Segmented confirmed-request first-segment overhead corrected (`6`, not `7`)
- Outbound segmented-request SegmentACK delivery requires `Server=true`
- Segmentation control sends (SegmentACK / Abort) use a bounded timeout
- Side-effecting services wrap Close and ambiguous transport failures after
  transmission has been attempted as `*OutcomeUnknownError`
- EventNotification handler: receive-path deadlock warning; panics recovered
- Documentation: segmented PDUs carry service choice on every segment (not an
  ASHRAE “omit after segment 0” peer quirk)
- Decoder strictness: WPM / ReadRange / EventNotification / DCC /
  ReinitializeDevice reject duplicate fields and unsigned overflows; ResultFlags
  shape checked; Log_Buffer requires successful LogRecord split; known
  NotificationParameters CHOICEs fail closed on malformed contents (unknown /
  unimplemented CHOICEs stay opaque)

### Added

- Fuzz targets for WPM, ReadRange(+ACK), LogRecords, Who-Has/I-Have,
  EventNotification, NotificationParameters, GetEventInformationACK,
  AcknowledgeAlarm, DCC, ReinitializeDevice
- Negative codec fixtures in `bacnet-interop` for WPM/ReadRange/DCC/Reinit
  duplicates and overflow

### Changed

- Pin → [`bacnet-interop` v0.4.2](https://github.com/otfabric/bacnet-interop/releases/tag/v0.4.2)
  (`interop/bacnet-interop-pin.json`)
- `make fuzz` discovers `Fuzz*` targets via `go test -list` and runs each with an
  anchored `-fuzz='^Name$'` so substring matches cannot skip or double-run

### Evidence

Pre-tag green on `main` @ `614c4d9` (re-verify / attach on the exact tag SHA when
cutting):

| Gate | Result | Link |
|---|---|---|
| Shared CI (lint, Go matrix, race, coverage, PR fuzz) | green on `614c4d9` | [30725263974](https://github.com/otfabric/go-bacnet/actions/runs/30725263974) |
| Pinned interop `v0.4.2` | green | [30725263801](https://github.com/otfabric/go-bacnet/actions/runs/30725263801) (job *Pinned release peers*) |
| bacnet-interop main compat | green | same run (job *bacnet-interop main compat*) |
| Pin | [`bacnet-interop` v0.4.2](https://github.com/otfabric/bacnet-interop/releases/tag/v0.4.2) | `interop/bacnet-interop-pin.json` |

### Notes

- No public API break intended; behaviour and decoder strictness only.
- BACnet4J still rejects segmented confirmed-request *receive* (reject reason 9);
  see [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md).

---

## v0.2.0

**Date:** 2026-08-02  
**Previous release:** [v0.1.1](https://github.com/otfabric/go-bacnet/releases/tag/v0.1.1)

### Summary

Supervisory-client breadth release: WritePropertyMultiple, ReadRange (including
typed Trend Log records), Who-Has / I-Have, EventNotification receive with typed
common NotificationParameters, AcknowledgeAlarm, GetEventInformation, opt-in
DeviceCommunicationControl / ReinitializeDevice, and windowed segmented
confirmed-request send. Pin moves to
[`bacnet-interop` v0.4.1](https://github.com/otfabric/bacnet-interop/releases/tag/v0.4.1)
(BACpypes3 **0.0.106**). Requires **Go 1.23+**.

Still **production-candidate** until the
[real-device gate](docs/REAL_DEVICE_GATE.md) is complete.

### Added

- **WritePropertyMultiple** service codecs + `Client.WritePropertyMultiple`
  (SimpleACK success; `*service.WritePropertyMultipleError` for first-failed
  write; exact-APDU retransmit disabled; outcome-unknown after send)
- **Segmented confirmed-request send** (proposed window 16, `WithSegmentWindow`):
  SegmentACK/NAK handling with windowed in-flight send, segment timeout → client
  Abort, peer Segmentation evidence gate, MaxSegments preflight; enables large
  WPM when the peer can receive segments. Segmented ComplexACK receive defaults
  to actual window 1 so peers that wait for SegmentACK before the next segment
  (BACpypes3 / BACnet4J) do not stall
- Confirmed-request / ComplexACK codecs: segmented PDUs carry service choice on
  every segment (constant across reassembly; BACpypes3/BACnet4J agree)
- **ReadRange** codecs + `Client.ReadRange` (byPosition / bySequenceNumber /
  byTime / all; resultFlags helpers; retransmit enabled)
- Typed **BACnetLogRecord** split: `service.LogRecord` /
  `ReadRangeACK.LogRecords` when property is `Log_Buffer`
- **Who-Has / I-Have** codecs + `SendWhoHas` / `DiscoverObjects` / `Objects()`
  with a bounded object-observation registry (shared retention options)
- **EventNotification** receive (Confirmed/Unconfirmed) + handler API;
  typed **NotificationParameters** for change-of-state / bitstring / value /
  out-of-range (opaque `NotificationParams` retained); **AcknowledgeAlarm**;
  **GetEventInformation**
- **DeviceCommunicationControl** / **ReinitializeDevice** (opt-in via
  `WithDeviceManagementEnabled`; outcome-unknown after send)
- Runnable examples: `examples/read-range`, `examples/write-multiple`,
  `examples/who-has`, `examples/events`
- Context helpers: `AppendContextSigned`, `AppendContextBitString`,
  `AppendContextCharacterString`, `AppendContextTime` (+ matching `Context*`)
- Constants: `ObjectTypeTrendLog`, `PropertyLogBuffer`
- Interop tests: three-peer WPM + Who-Has; BACpypes3 segmented WPM send;
  bacnet-stack/BACnet4J ReadRange; BACpypes3/BACnet4J EventNotification receive;
  three-peer ReinitializeDevice warmstart; bacnet-stack DCC enable;
  BACpypes3/BACnet4J segmented RPM ComplexACK receive
- Hermetic codec fixtures for the new services (via bacnet-interop)

### Fixed

- Routed interop harness: bind `bip-router` by static address→network (not
  Docker `eth0`/`eth1` order), which was dropping DNET forwards when iface
  order swapped after `create` + `network connect`

### Changed

- Pin → [`bacnet-interop` v0.4.1](https://github.com/otfabric/bacnet-interop/releases/tag/v0.4.1)
  (BACpypes3 **0.0.106**; bacnet-stack 1.6.0; BACnet4J 6.1.0; digest-pinned GHCR images)
- `DecodeReadRangeACK` populates `LogRecords` for Log_Buffer; otherwise trusts
  wire `itemCount` when complex item tag streams do not 1:1 match flat items

### Evidence

| Gate | Result | Link |
|---|---|---|
| Shared CI (lint, Go matrix, race, coverage, PR fuzz) | green on `8bee3e6` | [30722438815](https://github.com/otfabric/go-bacnet/actions/runs/30722438815) |
| Pinned interop `v0.4.1` | green | [30722438676](https://github.com/otfabric/go-bacnet/actions/runs/30722438676) (job *Pinned release peers*) |
| bacnet-interop main compat | green | same run (job *bacnet-interop main compat*) |
| Pin | [`bacnet-interop` v0.4.1](https://github.com/otfabric/bacnet-interop/releases/tag/v0.4.1) | `interop/bacnet-interop-pin.json` |

### Notes

- No public API break intended; additive services and options only.
- BACnet4J rejects segmented confirmed-request *receive* (reject reason 9);
  segmented WPM send evidence uses BACpypes3. See
  [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md).

---

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
  scope for this tag (added in later releases).

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
