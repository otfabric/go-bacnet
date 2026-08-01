# go-bacnet — Plan

Consumer-side plan for evidence integrity and the path from provisional
production-candidate toward an unambiguous Go BACnet leadership claim.
Adapter images, fixtures, and peer smoke live in
[`bacnet-interop`](https://github.com/otfabric/bacnet-interop) (`PLAN.md`).

**Status:** **production-candidate** (release-grade evidence closed in Batch
**4D**; cut tag `v0.1.1` via Release workflow). Pin:
[`bacnet-interop` v0.2.1](https://github.com/otfabric/bacnet-interop/releases/tag/v0.2.1).
**Next:** Horizon-2 supervisory breadth (WPM + segmented confirmed-request
send), then real-device gate → **production-usable**
([docs/REAL_DEVICE_GATE.md](docs/REAL_DEVICE_GATE.md)).

Honest positioning (see [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md)):

> OT Fabric currently has the highest-quality Go BACnet supervisory-client
> foundation. Worldiety currently has the broadest modern pure-Go BACnet stack.

Do **not** merge codebases or depend on competitors. Study them; turn them into
executable peers.

Status labels: `done` · `partial` · `open`.

---

## Competitive landscape (summary)

| Rank | Project | Strongest aspect | Main limitation |
|---:|---|---|---|
| 1 | **otfabric/go-bacnet** | Quality, strictness, transactions, segmented ComplexACK receive, pinned multi-peer interop, docs | Focused client; no server/BBMD-server/WPM/ReadRange |
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
as evidence. Prefer Worldiety as the **fourth bacnet-interop Go peer** once an
adapter exists.

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
| Adapters | `bacnet-interop` `v0.2.1` |

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
| 3 | Pin file for bacnet-interop ref/digests; required pinned lane | done | `interop/bacnet-interop-pin.json` → `v0.2.1` |
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
| 1 | Full required interop green + race/fuzz on new cases | partial | completed via 4A |
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

Do **not** chase Worldiety feature breadth before the next tag is cut.

| # | Item | Status |
|---|---|---|
| 1 | Required CI green (shared go-ci + library coverage gate) | done | [30705297706](https://github.com/otfabric/go-bacnet/actions/runs/30705297706) |
| 2 | Race detector green | done | `-race` in shared Test matrix (same run) |
| 3 | PR fuzz green | done | same run |
| 4 | Nightly fuzz green (manual/scheduled retention) | done | [30707778922](https://github.com/otfabric/go-bacnet/actions/runs/30707778922) |
| 5 | Pinned `v0.2.1` interop green **repeatedly** (≥2 runs) | done | [30705297526](https://github.com/otfabric/go-bacnet/actions/runs/30705297526), [30707779897](https://github.com/otfabric/go-bacnet/actions/runs/30707779897) |
| 6 | Latest bacnet-interop main compatibility green | done | both interop runs |
| 7 | Registry-bound + IPv4/DBTN cases on the released tree | done | on `6256fd2` + follow-up docs commit |
| 8 | Evidence artifact links in `RELEASE.md` for `v0.1.1` | done | see RELEASE.md |
| 9 | Descriptive conventional commits on main (no `wip` noise) | partial | going forward; prior `wip` left intact |
| 10 | [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md) seeded | done |

**Human next step:** Actions → Release → bump **`patch`** to cut `v0.1.1`.

Promotion rules:

| Label | Requirement |
|---|---|
| **alpha** | Pre-hardening |
| **production-candidate** | Batches 4A–4D closed; reproducible oracle evidence |
| **production-usable** | [Real-device gate](docs/REAL_DEVICE_GATE.md) complete |

Container oracles are necessary but **not** sufficient for production-usable.

---

## Horizon 2 — Supervisory-client breadth (after 4D)

Implement in this order. Preserve ownership, bounds, outcome-unknown, and
peer evidence discipline. Do **not** inherit global process state, GPL lineage,
or permissive unbounded decoding from competitors.

| Priority | Item | Notes |
|---|---|---|
| 1 | **WritePropertyMultiple** | Per-property outcomes; ambiguous write semantics; MaxAPDU preflight; mixed-success + malformed fixtures; three-peer interop |
| 2 | **Client-side segmented confirmed-request send** | Pair with WPM; transmit windows, SegmentACK/NAK, timeout ownership, MaxSegments, cancel/Abort, path correlation |
| 3 | **ReadRange** | Trends / historical data |
| 4 | **Who-Has / I-Have** | Bounded commissioning discovery |
| 5 | EventNotification / alarm-oriented services | COV is not a substitute |
| 6 | DeviceCommunicationControl / ReinitializeDevice | Explicit opt-in; safety-sensitive; outcome-unknown |

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
| [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md) | Peer/device, version, topology, scenarios, artifacts, deviations | partial (seeded) |
| `docs/CAPABILITY_MATRIX.md` + `capabilities.yaml` | Machine-readable capability surface | done (keep current) |
| Field-deviation catalogue (in COMPATIBILITY or ERRORS) | App vs context Error tags, string charsets, MaxAPDU segment sizing, Docker broadcast, delayed BVLC-Result | partial |
| Runnable examples under `examples/` | Discovery, RP, routed RP, RPM partial, WP/null, COV, FD, diagnostics, capture decode, shutdown | open |
| `MIGRATING_FROM_NUBEDEV.md` / `MIGRATING_FROM_GOBACNET.md` | Adoption accelerators | open |

---

## Out of scope for the current promotion path

- Claiming “best overall Go BACnet stack” before Horizons 2–3 close material gaps
- Claiming hardware or BTL interoperability without the real-device gate
- Merging with or depending on worldiety / NubeDev
- Moving consumer assertions into bacnet-interop
- Cross-repo API homogenization for its own sake
