# go-bacnet — Plan

Consumer-side plan for evidence integrity and the path from provisional
production-candidate toward an unambiguous Go BACnet leadership claim.
Adapter images, fixtures, and peer smoke live in
[`bacnet-interop`](https://github.com/otfabric/bacnet-interop) (`PLAN.md`).

**Status:** **production-candidate** —
**[`v0.2.2`](https://github.com/otfabric/go-bacnet/releases/tag/v0.2.2)** released;
**`v0.2.3`** pending CI/tag (see [RELEASE.md](RELEASE.md)). Pin:
[`bacnet-interop` v0.6.0](https://github.com/otfabric/bacnet-interop/releases/tag/v0.6.0).

**Pin:** [`interop/bacnet-interop-pin.json`](interop/bacnet-interop-pin.json) →
`v0.6.0` (File / Create-Delete / NC list adapters; bacnet-stack **1.6.0**;
BACpypes3 **0.0.106**; Worldiety digest).

**Next cycle (evidence expansion — no new services):**

1. **`v0.2.3`** — cut when shared CI + pinned interop `v0.6.0` are green
2. B3 remainder (COV-multiple / second-peer summaries); B7d–g; B5/B6
3. Then [real-device gate](docs/REAL_DEVICE_GATE.md) toward **production-usable**

Honest positioning (see [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md)):

> OT Fabric currently has the highest-quality Go BACnet supervisory-client
> foundation. Worldiety currently has the broadest modern pure-Go BACnet stack.

Do **not** merge codebases or depend on competitors. Study them; turn them into
executable peers.

Status labels: `done` · `partial` · `open`.

### Horizon 2 checklist

| # | Item | Status | Evidence | Follow-ups |
|---|---|---|---|---|
| 1 | WritePropertyMultiple | `done` | codecs + client + three-peer live + fixtures; [30722438676](https://github.com/otfabric/go-bacnet/actions/runs/30722438676) | — |
| 2 | Segmented confirmed-request send | `done` | windowed send (proposed 16) + receive window 1; BACpypes3 live segmented WPM; same run | BACnet4J rejects segmented confirmed receive (documented / blocked upstream) |
| 3 | ReadRange | `done` | codecs + client + fixtures + live byPosition (stack/4J TrendLog); typed LogRecord split | — |
| 4 | Who-Has / I-Have | `done` | codecs + client + three-peer live + fixtures; same run | — |
| 5 | EventNotification / alarms | `done` | receive + AckAlarm + GetEventInformation; typed NotificationParameters for common CHOICEs; live emit BACpypes3/4J | remaining uncommon event-parameter CHOICEs |
| 6 | DeviceCommunicationControl / ReinitializeDevice | `done` | opt-in client; live DCC enable (stack) + Reinit warmstart (3 peers); same run | — |

---

## Competitive landscape (summary)

| Rank | Project | Strongest aspect | Main limitation |
|---:|---|---|---|
| 1 | **otfabric/go-bacnet** | Quality, strictness, transactions, segmentation, pinned multi-peer interop, H2 client breadth, docs | Focused client; no server/BBMD-server; no real-device gate yet |
| 2 | worldiety/bacnet | Broadest modern pure-Go surface (server, BBMD, router, WPM, ReadRange, …) | Prototype; weaker independent interop/strictness evidence; no client-side segmented ComplexACK receive |
| 3 | NubeDev/bacnet | Historical field-device / routed MS/TP claims | Legacy, GPL-2.0, inactive since ~2023 |
| 4+ | ulbios, maxzerker, gobacnet, … | Historical codec or minimal experiments | Narrow or unsuitable as foundations |

**Category winners today**

| Category | Winner |
|---|---|
| Supervisory-client runtime quality / strictness / interop evidence / docs | OT Fabric |
| Application-service / server / BBMD / router breadth | Worldiety |
| Historical hardware name-list | NubeDev (informal; not a gate) |

Use NubeDev’s vendor families as **inspiration for the real-device matrix**, not
as evidence. Worldiety is the **fourth bacnet-interop Go peer** (pinned in `v0.5.0`).

---

## What is already strong (baseline)

Do not re-litigate these unless a regression appears:

| Area | Notes |
|---|---|
| Transactions | Atomic timeout / segmented ownership / winner-only completion |
| Segmented ComplexACK receive | Transaction-owned; BACpypes3 + BACnet4J peer evidence |
| RP / RPM / WP | Three-peer live evidence |
| Discovery / Error / Reject / Abort | Covered where peers support the path |
| COV | Subscribe / notify / cancel; renew on BACpypes3; sticky gap contract |
| Routing (client) | DNET/DADR + SNET/SADR via `bip-router` topology aid |
| FD / BBMD (client) | BACpypes3 + BACnet4J peer-as-BBMD |
| Strict framing | Reserved APDU/NPDU bits, MaxAPDU, windows, router lists → `ErrMalformed` |
| Evidence architecture | Digest-pinned adapters, fixture provenance, hermetic coverage, PR + nightly fuzz |
| Adapters | `bacnet-interop` `v0.6.0` (incl. Worldiety; File/lifecycle/NC list) |

Routing evidence phrase (until a second independent router exists):

> Routed client addressing and response handling validated through the OT Fabric
> interop router with independent endpoint stacks behind it.

Not: “proven interoperable with independent BACnet routers.”

---

## Ownership vs bacnet-interop

`bacnet-interop` must **never** check out, build, or test against `go-bacnet`.
Consumer assertions and peer interop jobs live only in this repository.

Trade-off:

- `go-bacnet` detects compatibility with bacnet-interop (pinned release + latest
  main) after changes merge here;
- bacnet-interop PRs are not validated against this consumer before merge.

A reusable/manual workflow owned by `go-bacnet` that accepts a bacnet-interop
ref is the preferred way to smoke a bacnet-interop release candidate.

---

## Batch 1 — CI and evidence integrity

| # | Item | Status | Notes |
|---|---|---|---|
| 1 | **P0** — `reexecInNetwork` honors `BACNET_INTEROP_ROOT` + require fixture files under mount | done | `interop/root.go` + unit tests |
| 2 | Print / artifact both repo SHAs (+ peer ready metadata) in required interop job | done | workflow summary + artifacts |
| 3 | Pin file for bacnet-interop ref/digests; required pinned lane | done | `interop/bacnet-interop-pin.json` → `v0.6.0` |
| 4 | Latest-main compatibility lane (detect upcoming breakage) | done | `peer-interop-main` job |
| 5 | bacnet-interop CI: consumer job against go-bacnet main | **n/a** | Dependency inversion rejected |
| 6 | bacnet-interop CI: build + require bip-router smoke | done | in bacnet-interop |

---

## Batch 2 — Runtime contract closure

| # | Item | Status |
|---|---|---|
| 1 | Sticky COV gap / sequence semantics; never overwrite Closed with Degraded; admit terminal event | done |
| 2 | Share transaction path matcher with routed COV (remote address + next hop) | done |
| 3 | `OutcomeUnknownError` for SubscribeCOV / SubscribeCOVProperty / cancel / renew (not only WP) | done |
| 4 | Bound + diagnose confirmed-COV ACK sends (no unbounded `context.Background`) | done |
| 5 | Normalize FD TTL (whole seconds, ≥2s) + document delayed BVLC-Result ambiguity | done |
| 6 | Validate advertised MaxAPDU vs parser limit at `New` | done |

---

## Batch 3 — Wire / API / fixture closure

| # | Item | Status |
|---|---|---|
| 1 | Harden Who-Is, I-Am, ReadProperty **request** decoders | done |
| 2 | Complete executable fixture operations beyond RP/BVLC/tag/Error | done |
| 3 | Enforce `deterministic_reencode_equal` and expected error category/**layer** | done |
| 4 | Remove exported options that take `internal` types; delete inert `WithLogger` | done |
| 5 | `InvokeConfirmed` documented experimental | done |
| 6 | Map concrete tests into `docs/TRACEABILITY.md` | done |

---

## Batch 4 — First tagged baseline (v0.1.0)

| # | Item | Status |
|---|---|---|
| 1 | Full required interop green + race/fuzz on new cases | done | completed via 4A / H2 CI |
| 2 | Label tree **production-candidate** (docs / RELEASE; humans own tags) | done | provisional until 4D |
| 3 | Real-device gate (≥2 independent devices) → **production-usable** | open |

---

## Batch 4A — Make production-candidate evidence reproducible

| # | Item | Status |
|---|---|---|
| 1 | CI runs `make coverage` and fails below `COVERAGE_MIN` | done |
| 2 | Hermetic fixtures: checkout pinned bacnet-interop + `BACNET_INTEROP_REQUIRED=1` | done |
| 3 | PR fuzz: all fuzz targets, short fixed duration | done (`FUZZTIME=15s`) |
| 4 | Nightly/longer fuzz with retained crash corpus | done (`fuzz-nightly.yml`, 5m/target) |
| 5 | Pinned interop required; latest-main clearly classified | done |
| 6 | Remove committed `cover*.out` profiles; gitignore | done |

---

## Batch 4B — Runtime boundedness and panic closure

| # | Item | Status |
|---|---|---|
| 1 | Bounded/expiring device observation registry | done |
| 2 | IPv4 validation for B/IP endpoints / custom transports (no `As4` panic) | done |
| 3 | Decide + test inbound DBTN (ignore; not a BBMD server) | done |
| 4 | Clarify synchronous diagnostic callback contract | done |
| 5 | Document + test confirmed COV ACK-before-admission | done |
| 6 | Hostile I-Am flood / registry eviction tests | done |

---

## Batch 4C — Strict wire corpus

| # | Item | Status |
|---|---|---|
| 1 | Fix expected-error layer helper for multi-stage parsers | done |
| 2 | Malformed APDU reserved bits / MaxAPDU / segment flag fixtures | done |
| 3 | Malformed NPDU reserved bits / DNET-DLEN / router message fixtures | done |
| 4 | Service-level negative fixtures | done |

---

## Batch 4D — Finish the quality claim

Do **not** chase Worldiety feature breadth before `v0.2.0` is tagged.

| # | Item | Status |
|---|---|---|
| 1 | Required CI green (shared go-ci + library coverage gate) | done | [30705297706](https://github.com/otfabric/go-bacnet/actions/runs/30705297706) |
| 2 | Race detector green | done | `-race` in shared Test matrix (same run) |
| 3 | PR fuzz green | done | same run |
| 4 | Nightly fuzz green (manual/scheduled retention) | done | [30707778922](https://github.com/otfabric/go-bacnet/actions/runs/30707778922) |
| 5 | Pinned interop green **repeatedly** (≥2 runs) | done | `v0.4.0` [30720046370](https://github.com/otfabric/go-bacnet/actions/runs/30720046370); `v0.4.1` [30722438676](https://github.com/otfabric/go-bacnet/actions/runs/30722438676) |
| 6 | Latest bacnet-interop main compatibility green | done | [30722438676](https://github.com/otfabric/go-bacnet/actions/runs/30722438676) |
| 7 | Registry-bound + IPv4/DBTN cases on the released tree | done | on `6256fd2` + follow-up docs commit |
| 8 | Evidence artifact links in `RELEASE.md` for `v0.1.1` | done | see RELEASE.md |
| 9 | Descriptive conventional commits on main (no `wip` noise) | partial | going forward; prior `wip` left intact |
| 10 | [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md) seeded | done |

### Release prep (`v0.2.0`)

| # | Item | Status | Notes |
|---|---|---|---|
| 1 | H2 APIs + interop assertions on `main` | done | see checklist above |
| 2 | Pin → bacnet-interop `v0.5.0` | done | digests in `interop/bacnet-interop-pin.json` (incl. Worldiety; BACpypes3 0.0.106) |
| 3 | CI green on `main` | done | [30722438815](https://github.com/otfabric/go-bacnet/actions/runs/30722438815) |
| 4 | Interop green (pinned + main compat) | done | [30722438676](https://github.com/otfabric/go-bacnet/actions/runs/30722438676) |
| 5 | Docs / PLAN / RELEASE / INTEROP synced | done | |
| 6 | **Human:** cut `v0.2.0` | done | tag `v0.2.0` @ `3bc6288` |
| 7 | Real-device gate → **production-usable** | open | after `v0.2.2` |

### Batch — `v0.2.1` correctness / strictness

| # | Item | Status | Notes |
|---|---|---|---|
| 1 | Remove zero-sentinel service matching | `done` | unconditional SimpleACK/ComplexACK/Error + segment checks |
| 2 | Fix first-segment overhead 7→6 | `done` | boundary tests for MaxAPDU 50/128/206/480 |
| 3 | Validate SegmentACK `Server` direction | `done` | client ACK ignored on outbound send path |
| 4 | Bound SegmentACK / Abort send contexts | `done` | `clock.ContextWithTimeout` + control kind in diagnostics |
| 5 | Wrap ambiguous post-send failures (Close, transport) | `done` | `abortAll` + await/send paths |
| 6 | EventNotification handler: docs + panic recover | `done` | stream API deferred |
| 7 | Correct service-choice-on-every-segment docs | `done` | API / COMPATIBILITY / codec comments |
| 8 | Exact-tag release evidence artifact | `ready` | pre-tag green on `614c4d9` ([CI](https://github.com/otfabric/go-bacnet/actions/runs/30725263974), [interop](https://github.com/otfabric/go-bacnet/actions/runs/30725263801)); attach on tag SHA at cut |
| 9 | Decoder strictness parity + negative fixtures | `done` | duplicates/overflow/ResultFlags/Log_Buffer/known CHOICE; 5 new interop negatives |
| 10 | Service fuzz targets (WPM/ReadRange/Who-Has/events/DCC/Reinit) | `done` | functional `*_fuzz_test.go` + anchored `make fuzz` + nightly |
| 11 | `bacnet-interop` v0.4.2 hygiene | `done` | tag [`v0.4.2`](https://github.com/otfabric/bacnet-interop/releases/tag/v0.4.2); superseded by `v0.5.0` pin |
| 12 | **Human:** cut `v0.2.1` | `done` | [`v0.2.1`](https://github.com/otfabric/go-bacnet/releases/tag/v0.2.1) |
| 13 | Pin → `bacnet-interop` v0.5.0 (+ Worldiety) | `done` | digests in `interop/bacnet-interop-pin.json`; CI pulls Worldiety |
| 14 | **Human:** cut `v0.2.2` | `done` | [`v0.2.2`](https://github.com/otfabric/go-bacnet/releases/tag/v0.2.2) @ `a277aea` |

Promotion rules:

| Label | Requirement |
|---|---|
| **alpha** | Pre-hardening |
| **production-candidate** | Batches 4A–4D + Horizon 2 client breadth closed; reproducible oracle evidence |
| **production-usable** | [Real-device gate](docs/REAL_DEVICE_GATE.md) complete |

Container oracles are necessary but **not** sufficient for production-usable.

---

## Horizon 2 — Supervisory-client breadth (after 4D)

**Closed** for the scoped deliverables below (in-tree + peer evidence; pin
`v0.5.0`). Preserve ownership, bounds, outcome-unknown, and peer evidence
discipline in follow-ups. Do **not** inherit global process state, GPL lineage,
or permissive unbounded decoding from competitors.

| Priority | Item | Status | Notes |
|---|---|---|---|
| 1 | **WritePropertyMultiple** | `done` | Codecs + client + three-peer live + fixtures |
| 2 | **Client-side segmented confirmed-request send** | `done` | Windowed send (proposed 16) + receive window 1; BACpypes3 live segmented WPM; BACnet4J receive gap documented |
| 3 | **ReadRange** | `done` | Codecs + client + fixtures + live byPosition (stack/4J); typed `LogRecords` on Log_Buffer |
| 4 | **Who-Has / I-Have** | `done` | Codecs + client + three-peer live + fixtures |
| 5 | **EventNotification / alarms** | `done` | Receive + AckAlarm + GetEventInformation; typed NotificationParameters for common CHOICEs; live emit BACpypes3/4J |
| 6 | **DeviceCommunicationControl / ReinitializeDevice** | `done` | Opt-in APIs; live DCC (stack) + Reinit warmstart (3 peers) |

### Horizon 2 follow-ups

| Item | Status | Notes |
|---|---|---|
| Segmented confirmed-request windows > 1 | `done` | Send proposed window 16; receive actual window 1 (peer-safe); `WithSegmentWindow` sets both; unit test send window=4 |
| BACnet4J segmented confirmed-request receive | `blocked` | Upstream peer rejects (reason 9); keep skip + COMPATIBILITY row — not in `v0.2.0` scope |
| Typed ReadRange LogRecord split | `done` | `DecodeLogRecords` / `ReadRangeACK.LogRecords` when property is Log_Buffer |
| Richer EventNotification parameter typing | `done` | change-of-state / bitstring / value / out-of-range; opaque fallback retained |
| Runnable `examples/` for H2 APIs | `done` | `examples/{read-range,write-multiple,who-has,events}` |

Update the checklist at the top of this file whenever an item moves between
`open` / `partial` / `done`.

---

## Horizon 3 — Convenience API (separate package)

Keep the precise `client` API. Add a higher-level package (e.g. `supervisor`)
for commissioning ergonomics:

- device-ID resolution and capability cache;
- routed targeting and pacing;
- batching / object-list enumeration;
- typed property helpers;

without hiding raw protocol errors or moving convenience into wire packages.

---

## Horizon 4 — Competitors as executable peers

Add bacnet-interop adapters (assertions stay in go-bacnet):

1. **worldiety/bacnet** (primary modern Go peer)
2. **NubeDev/bacnet** (behavioral / routed MS/TP comparison source)
3. optionally UlBios (historical codec)

Once OT Fabric has a server, test both directions:

```text
OT Fabric client → worldiety server
worldiety client → OT Fabric server
```

---

## Horizon 5 — Server and infrastructure

After the client is release-grade:

1. Minimal Device object / server runtime
2. RP / RPM / WP / WPM server services
3. COV producer
4. Segmented confirmed-request receive + ComplexACK send
5. Duplicate confirmed-request suppression
6. BBMD server (BDT/FDT)
7. Routing / forwarding service
8. Broader network-layer message coverage

Worldiety is the main **breadth** reference; keep OT Fabric ownership and
boundedness rules.

---

## Horizon 6 — Additional data links

Long-term, still out of Horizon 1:

- BACnet/IPv6
- native MS/TP
- BACnet/SC

Keep BVLC/BACnet-IP concepts out of application-service layers so APDU/service
code stays reusable.

---

## Documentation follow-ups

| Document | Purpose | Status |
|---|---|---|
| [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md) | Peer/device, version, topology, scenarios, artifacts, deviations | done (seeded; keep current with pins) |
| `docs/CAPABILITY_MATRIX.md` + `capabilities.yaml` | Machine-readable capability surface | done (keep current) |
| [INTEROP.md](INTEROP.md) | Peer scenario matrix vs live tests | done (H2 rows + `device-baseline-v2`) |
| Field-deviation catalogue (in COMPATIBILITY or ERRORS) | App vs context Error tags, string charsets, MaxAPDU segment sizing, Docker broadcast, delayed BVLC-Result | partial |
| Runnable examples under `examples/` | H2: ReadRange, WPM, Who-Has, events shipped; remaining: discovery/RP/COV/FD as needed | partial |
| `MIGRATING_FROM_NUBEDEV.md` / `MIGRATING_FROM_GOBACNET.md` | Adoption accelerators | open |

---

## Out of scope for the current promotion path

- Claiming “best overall Go BACnet stack” before Horizon 3 / server breadth closes material gaps
- Claiming hardware or BTL interoperability without the real-device gate
- Merging with or depending on worldiety / NubeDev
- Moving consumer assertions into bacnet-interop
- Cross-repo API homogenization for its own sake
