# go-bacnet — Plan (Horizon 1 → production-candidate)

Consumer-side plan for closing alpha hardening and evidence integrity.
Adapter images, fixtures, and peer smoke live in
[`bacnet-interop`](https://github.com/otfabric/bacnet-interop) (`PLAN.md`).

**Status:** **production-candidate**. Batches 1–4 closed. **production-usable**
still requires the [real-device gate](docs/REAL_DEVICE_GATE.md) (≥2 independent
devices).

Architecture is frozen; no redesign. Remaining work for production-usable is
hardware evidence only.

Status labels: `done` · `partial` · `open`.

---

## What is already strong (baseline)

Do not re-litigate these unless a regression appears:

| Area | Notes |
|---|---|
| Transactions | Atomic timeout / segmented ownership / winner-only completion |
| Segmented ComplexACK | Transaction-owned state machine; BACpypes3 + BACnet4J peer evidence |
| RP / RPM / WP | Three-peer live evidence |
| Discovery / Error / Reject / Abort | Covered where peers support the path |
| COV happy path | Subscribe / notify / cancel on all three peers; renew on BACpypes3 |
| Routing (client) | DNET/DADR + SNET/SADR via `bip-router` topology aid |
| FD / BBMD | BACpypes3 + BACnet4J peer-as-BBMD |
| Adapters | Fixture-driven, digest-pinned bases, ready-after-bind, `v0.2.0` published |

Routing evidence phrase (until a second independent router exists):

> Routed client addressing and response handling validated through the OT Fabric
> interop router with independent endpoint stacks behind it.

Not: “proven interoperable with independent BACnet routers.”

---

## Batch 1 — CI and evidence integrity

| # | Item | Status | Notes |
|---|---|---|---|
| 1 | **P0** — `reexecInNetwork` honors `BACNET_INTEROP_ROOT` + require fixture files under mount | done | `interop/root.go` + unit tests |
| 2 | Print / artifact both repo SHAs (+ peer ready metadata) in required interop job | done | workflow summary + artifacts |
| 3 | Pin file for bacnet-interop ref/digests; required pinned lane | done | `interop/bacnet-interop-pin.json` |
| 4 | Latest-main compatibility lane (detect upcoming breakage) | done | `peer-interop-main` job |
| 5 | bacnet-interop CI: consumer job against go-bacnet main | done | bacnet-interop Phase 6 |
| 6 | bacnet-interop CI: build + require bip-router smoke | done | `make build` + `BIP_ROUTER_REQUIRED=1` |

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
| 1 | Harden Who-Is, I-Am, ReadProperty **request** decoders (overflow, duplicates, required fields, enums) | done |
| 2 | Complete executable fixture operations beyond RP/BVLC/tag/Error | done |
| 3 | Enforce `deterministic_reencode_equal` and expected error category/**layer** | done |
| 4 | Remove or redesign exported options that take `internal` types; delete inert `WithLogger` | done |
| 5 | Decide long-term status of `InvokeConfirmed` (documented raw / experimental / internal) | done (experimental) |
| 6 | Map concrete tests into `docs/TRACEABILITY.md` | done |

---

## Batch 4 — Promotion

| # | Item | Status |
|---|---|---|
| 1 | Full required interop green repeatedly + race/fuzz on new cases | done (local: race, fuzz, interop ×2) |
| 2 | Label tree **production-candidate** (docs / RELEASE; humans own tags) | done |
| 3 | Real-device gate (≥2 independent devices) → **production-usable** | open |

Promotion rules (match `INTEROP.md` / `docs/REAL_DEVICE_GATE.md`):

| Label | Requirement |
|---|---|
| **alpha** | Pre-hardening |
| **production-candidate** | Batches 1–4 closed; P0 wire/runtime + reproducible oracle evidence |
| **production-usable** | Real-device gate checklist complete |

Container oracles are necessary but **not** sufficient for production-usable.

---

## Out of scope for this promotion path

- BACnet/SC, MS/TP, IPv6, full server/device model
- BBMD product server / multi-BBMD failover
- Claiming hardware or BTL interoperability without real-device gate evidence
- Cross-repo API homogenization for its own sake
