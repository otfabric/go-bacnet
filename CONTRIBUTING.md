# Contributing

Thanks for contributing to `go-bacnet`.

## Requirements

- Go **1.23** or newer
- Always run toolchain commands with module isolation when a parent `go.work`
  may be present:

```bash
export GOWORK=off
```

The Makefile exports `GOWORK=off` for you.

## Local checks

```bash
make check
```

`check` runs format, tidy, vet, staticcheck, **golangci-lint** (same gate as
CI, including `errcheck`), vulncheck, unit tests, race tests, coverage, and
the import-boundary test. Install the CI tools once:

```bash
go install honnef.co/go/tools/cmd/staticcheck@latest
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
go install golang.org/x/vuln/cmd/govulncheck@latest
```

Useful individual targets: `make test`, `make test-race`, `make vet`,
`make lint`, `make lint-ci`, `make fmt`, `make tidy`, `make vuln`,
`make coverage`, `make fuzz`, `make chaos`, `make imports`,
`make interop-pinned`, `make dissector`. `make help` lists them all.

### Concurrency tests

The client is one socket shared by every caller, so its handlers are tested
under concurrency, always with the race detector (`make test-race`, also in
CI):

- Tests built on `newVirtualPair` run in parallel with each other. Each has
  its own client, transports and clock; a test must not change package-level
  state (the service policy tables are read by every request without a lock).
- `client/chaos_test.go` drives one client from many goroutines against
  in-process devices that lose, delay, duplicate, misaddress, truncate and
  segment their replies and flood unsolicited frames. It asserts invariants
  rather than scripts: no call hangs, a successful read returns the value of
  the object it asked for, failures have a known error category, nothing is
  left behind when the traffic stops, subscription streams are in sequence
  and end with Closed. `make chaos` runs it at five times the load, three
  times over; `BACNET_CHAOS_SEED=<n>` pins a seed and `BACNET_CHAOS_SCALE`
  sets the load. Scheduling is not reproducible from the seed: a failure
  that does not recur at once is still a finding.

When a handler gains state, add the invariant to `chaosIdle` or to the
outcome checks instead of a new scripted test.

The command-line client is a separate repository,
[otfabric/bacnetctl](https://github.com/otfabric/bacnetctl). A change to an
exported API here should be built against it before release.

## Releases

[RELEASE.md](RELEASE.md) is **release notes only**: title, then newest
`## vX.Y.Z` section first. The shared release workflow parses from the top of
that file — do not put versioning policy or how-to text above the latest tag.

To cut a release:

1. Add/update the `## vX.Y.Z` section at the top of `RELEASE.md` (after the title)
2. Merge to `main`
3. Run **Actions → Release → Run workflow** with the bump (`patch` / `minor` /
   `major`). First tag: use **`minor`** for `v0.1.0`
4. The workflow tags the module and publishes the GitHub release

Do not push tags or create GitHub releases locally unless that is explicitly
requested.

## Import boundaries

Package dependency direction is documented in
[ARCHITECTURE.md](ARCHITECTURE.md) and enforced by
`internal/imports`. Do not introduce cycles or cross-import sibling codecs.

Root `bacnet` stays transport-neutral: no `bip` / wire / client imports.

## Style

- SPDX header on every first-party `.go` file: `// SPDX-License-Identifier: MIT`
- Prefer sentinel / typed errors with `%w`; see [ERRORS.md](ERRORS.md)
- Keep diffs focused; update tests and docs when behaviour changes
- Do not claim hardware interoperability without evidence in
  [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md) / [docs/FIELD_VALIDATION.md](docs/FIELD_VALIDATION.md)

## Interop tests

Pinned peer images and fixtures come from
[`bacnet-interop`](https://github.com/otfabric/bacnet-interop)
(`interop/bacnet-interop-pin.json`).

```bash
# Docker, jq and a bacnet-interop checkout next to this repository
# (or BACNET_INTEROP_ROOT=/path/to/bacnet-interop):
make interop-pinned                              # pulls the pinned images, fails on anything missing
make interop-pinned INTEROP_RUN='TestReadRange'  # a subset
make interop                                     # locally built :local images; skips what is missing
make dissector                                   # tshark only, no Docker
```

A peer test starts one container per (sub)test, so each must leave the peer
as it found it only for its own sake. Record what a peer does differently in
the test itself and in [INTEROP.md](INTEROP.md), and assert it, rather than
skipping the peer silently. `interop/doc.go` lists the environment variables.

Scenario results: [INTEROP.md](INTEROP.md). Peer notes:
[bacnet-interop PEER_SUPPORT.md](https://github.com/otfabric/bacnet-interop/blob/main/PEER_SUPPORT.md).

## Pull requests

1. Discuss non-trivial API changes first
2. Add or update tests
3. Run `make check` (or at least `GOWORK=off go test ./...` and `go vet ./...`)
4. Open a PR with a clear summary and test notes
