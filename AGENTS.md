# AGENTS.md — go-bacnet

## What this is

Pure Go BACnet/IP client library (ANSI/ASHRAE 135-2024 baseline, Annex J over IPv4). Client only: no server or device object model. No dependencies outside the standard library. Profile: **protocol-library**. The API is v0. Not BTL listed; not yet validated against physical devices.

## Organisation standards

This repository follows **otfabric/engineering policy 1.1**, profile **protocol-library** (`.otfabric/policy.yaml`, mode `enforce`): API design, errors, logging, documentation and testing guidance live in otfabric/engineering `standards/`, not here. Where this file and the organisation standards disagree, this file wins here — say so in your report.

## Where the truth lives

| Question | Source |
|---|---|
| Wire behaviour | ANSI/ASHRAE 135-2024 (docs/STANDARD_BASELINE.md) |
| What is supported and how well it is verified | docs/CLIENT_SUPPORT.md, LIMITATIONS.md |
| Peer results and quirks | INTEROP.md, docs/COMPATIBILITY.md |
| Behavioural contract | API.md, ERRORS.md |
| Physical device evidence | docs/FIELD_VALIDATION.md |
| Release history | RELEASE.md (`## vX.Y.Z` … `---`) |

Repository-local truth order, when these disagree: current code and tests, then `go.mod`, then this file, then README.md, API.md and the other documents. Verify current behaviour and correct stale documentation in the same change.

## Hard rules

1. Never lower `COVERAGE_MIN` (90), add lint suppressions, or skip or delete tests to make CI pass.
2. Never invent protocol semantics; cite the clause or the recorded peer behaviour.
3. Never claim interoperability, conformance or BTL status beyond the evidence in docs/CLIENT_SUPPORT.md and INTEROP.md.
4. No dependencies outside the standard library.
5. Do not change package-level state in tests: the service policy tables are read by every request without a lock.
6. Package boundaries are enforced by `make imports`.

## Commands

| When you change… | Run |
|---|---|
| anything | `make check` |
| client handlers or state | `make chaos`, `make test-race` |
| a decoder | `make fuzz` |
| a client request | `make dissector` (tshark) |
| interop-relevant behaviour | `make interop-pinned` (Docker, jq) |

## Definition of Done

`make check` green; tests for new behaviour including error paths; docs/CLIENT_SUPPORT.md, API.md and ERRORS.md updated; RELEASE.md section for user-visible changes; failing or skipped steps reported.

## Git and releases

Branches and PRs. Do not commit, push, tag or release unless the maintainer asks in the session. Lower layers are bumped and released first.
