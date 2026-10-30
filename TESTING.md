# Testing

How each test tier runs and what it needs. `make help` lists every target.
Peer results are in [INTEROP.md](INTEROP.md), the evidence level of each
capability in [docs/CLIENT_SUPPORT.md](docs/CLIENT_SUPPORT.md).

## Tiers

| Tier | Command | Prerequisites | CI |
|---|---|---|---|
| Unit | `make test` | none | `ci.yml`, shared go-ci on Go 1.23–1.26 |
| Race | `make test-race` | none | `ci.yml` |
| Coverage gate | `make coverage` (`make coverage-check` is an alias) | a `bacnet-interop` checkout, for the fixture corpus (below) | `ci.yml`, job Library coverage gate |
| Import boundaries | `make imports` | none | part of the unit tier (`internal/imports`) |
| Chaos | `make chaos` | none | the `TestChaos` tests also run at default load in the unit tier |
| Fuzz | `make fuzz` (`FUZZTIME`, default 5s) | none | 10s per target in `ci.yml`; 5m per listed target in `fuzz-nightly.yml` |
| Benchmarks | `make bench` | none | not run in CI |
| Cross-compile | `make cross` | none | not run in CI |
| Interop | `make interop-pinned`, or `make interop` | Docker, a `bacnet-interop` checkout; `make interop-pinned` also needs `jq` | `interop.yml` |
| Dissector oracle | `make dissector` | `tshark` | `interop.yml` |

There is no end-to-end tier: the library is a client with no BACnet server of
its own, so nothing here runs a go-bacnet client against a go-bacnet device.
`client` tests run against scripted in-process devices on the in-memory
transport (`internal/virtual`), and `make chaos` adds faults
(`client/chaos_test.go`, described in
[CONTRIBUTING.md](CONTRIBUTING.md#concurrency-tests)). Independent devices are
reached through the interop tier.

## Prerequisites and their switches

| Variable | Effect |
|---|---|
| `BACNET_INTEROP_ROOT` | `bacnet-interop` checkout; default is a sibling directory |
| `BACNET_INTEROP_REQUIRED` | A missing image, fixture or Docker fails instead of skipping. Set by `make interop-required`, `make interop-pinned` and CI |
| `BACNET_INTEROP_SKIP` | Skips all peer tests |
| `BACNET_DISSECTOR_REQUIRED` | A missing `tshark` fails instead of skipping. Set by `make dissector` |
| `BACNET_CHAOS_SEED`, `BACNET_CHAOS_SCALE` | Pin the chaos seed and set its load |

[interop/doc.go](interop/doc.go) lists the image variables. Tests that need a
UDP socket, a broadcast-capable interface or an IPv4 loopback skip without one
(`client/*_extra_test.go`); no switch turns those skips into failures.

## Coverage policy

The gate is 90%, in `make coverage` (`COVERAGE_MIN`) and in `ci.yml`
(`coverage-min`). The gate's packages exclude:

| Excluded | Why |
|---|---|
| `examples/` | Example programs, not library code |
| `interop` | Runs only with Docker and the peers |
| `internal/fixtures` | Test loader. Its `Root()` depends on the environment; its corpus tests still run and cover the codecs and client |

`make coverage` runs the tests of every package except `examples/` and
`interop`. Without a `bacnet-interop` checkout the fixture corpus skips, so
the local number can fall below what CI measures.

## Fuzzing

Targets are discovered with `go test -list` in the packages of `FUZZ_PKGS`.
A crash found by `make fuzz` is written to
`<package>/testdata/fuzz/<Target>/`; the nightly job uploads corpus and
crashers as an artifact. To promote one: copy the file there if it came from
the artifact, fix the decoder, and commit the file, so that `go test` replays
it. A new target also goes in the matrix of `fuzz-nightly.yml`, which is
listed by hand.
