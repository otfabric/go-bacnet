# Testing

How each test tier runs and what it needs. `make help` lists every target.
Peer results are in [INTEROP.md](INTEROP.md), the evidence level of each
capability in [docs/CLIENT_SUPPORT.md](docs/CLIENT_SUPPORT.md).

## Tiers

| Tier | Command | Prerequisites | CI |
|---|---|---|---|
| Unit | `make test` | none | `ci.yml`, shared go-ci on Go 1.26–1.27 |
| Race | `make test-race` | none | `ci.yml` |
| Coverage gate | `make coverage` (`make coverage-check` is an alias) | a `bacnet-interop` checkout, for the fixture corpus (below) | `ci.yml`, job Library coverage gate |
| Import boundaries | `make imports` | none | part of the unit tier (`internal/imports`) |
| End to end | `make e2e` | none | part of the unit tier, on Go 1.26–1.27 |
| Chaos | `make chaos` | none | the `TestChaos` tests also run at default load in the unit tier |
| Fuzz | `make fuzz` (`FUZZTIME`, default 5s) | none | 10s per target in `ci.yml`; 5m per listed target in `fuzz-nightly.yml` |
| Benchmarks | `make bench` | none | not run in CI |
| Cross-compile | `make cross` | none | not run in CI |
| Interop | `make interop-pinned`, or `make interop` | Docker, a `bacnet-interop` checkout; `make interop-pinned` also needs `jq` | `interop.yml` |
| Dissector oracle | `make dissector` | `tshark` | `interop.yml` |
| Recorded traffic, golden | part of `make test` | none | part of the unit tier |
| Recorded traffic, all originals | `make captures-extended` | network (kargs.net), 246 MB | not run in CI |
| Recorded traffic, regenerate | `make captures-generate`, `make captures-report` | `tshark`, the originals | not run in CI |

`e2e/` runs the client on a real UDP socket against a device on another,
over loopback. In most tests that device is built in the test from the public codecs (`bvlc`, `npdu`, `apdu`, `service`),
and covers discovery, reads, writes, device errors, timeouts, COV and
concurrent requests. `e2e/hostile_test.go` replaces that device with a hostile
one: malformed frames ahead of a valid answer, out-of-range values in ACKs and
I-Ams, I-Am and COV floods, unsolicited junk and endless or broken
segmentation; the client must refuse or ignore each, within its timeout and
with bounded memory. `client` tests run against scripted in-process devices on
the in-memory transport (`internal/virtual`), and `make chaos` adds faults
(`client/chaos_test.go` and `client/chaos_segack_test.go` for the segmented
request path, described in
[CONTRIBUTING.md](CONTRIBUTING.md#concurrency-tests)). `FuzzClientReceive`
feeds raw datagrams to the client while a request is pending. Independent
devices are reached through the interop tier.

The server has the same tiers. `e2e/server*_test.go` runs the client against
the go-bacnet server over loopback UDP: every served service, one object of each
built-in type read with ReadPropertyMultiple ALL, and the BBMD (foreign
device, tables). With `BACNET_SERVER_FRAMES_DIR` set those tests record
every datagram the server sends, which `make dissector` passes to tshark;
`server/*_internal_test.go` sends encoded requests to a server on the
in-memory transport for the paths a client does not produce (segmentation
faults, repeats, a full worker queue, hooks, malformed frames).
`TestChaosServer` (`make chaos`) runs concurrent requesters with
retransmissions and junk while objects change. `FuzzServerReceive` feeds two
raw datagrams to a serving device, and `FuzzHandleBVLC` arbitrary BVLC
messages to a BBMD. `TestBACnetStackClientAgainstServer` (`make interop`)
runs a bacnet-interop image in client mode against the server in a Go
container on the image's network; it skips when the image has no client
mode, as the pinned release does today.

`internal/capture` reads recorded captures of physical devices
([testdata/captures](testdata/captures/README.md)). Their golden excerpts
run in the unit tier, offline: `internal/capture` compares every datagram
with Wireshark's decode and encodes it again, `client`'s
`TestCaptureReplay*` runs the client against the recorded answers (and with
segments lost, duplicated or swapped), `server`'s `TestCaptureServer*`
compares the server's answers with the devices', and the fuzz targets are
seeded with the recorded octets. `make captures-extended` downloads the 217
originals (not redistributed) and re-derives
[manifest.json](testdata/captures/manifest.json) from them.

`internal/leak` fails `client`, `server`, `bbmd`, `internal/clock` and `e2e` when a test leaves a
goroutine behind; it compares goroutine stacks, so the failure names it.

## Prerequisites and their switches

| Variable | Effect |
|---|---|
| `BACNET_INTEROP_ROOT` | `bacnet-interop` checkout; default is a sibling directory |
| `BACNET_INTEROP_REQUIRED` | A missing image, fixture or Docker fails instead of skipping. Set by `make interop-required`, `make interop-pinned` and CI |
| `BACNET_INTEROP_SKIP` | Skips all peer tests |
| `BACNET_DISSECTOR_REQUIRED` | A missing `tshark` fails instead of skipping. Set by `make dissector` |
| `BACNET_SERVER_FRAMES_DIR` | The server e2e tests write every datagram the server sends to this directory, one hex file per test. Set by the server dissector test |
| `BACNET_CHAOS_SEED`, `BACNET_CHAOS_SCALE` | Pin the chaos seed and set its load |

[interop/doc.go](interop/doc.go) lists the image variables. Tests that need a
UDP socket, a broadcast-capable interface or an IPv4 loopback skip without one
(`client/*_extra_test.go`); no switch turns those skips into failures.

## Coverage policy

The gate is 90%, in `make coverage` (`COVERAGE_MIN`) and in `ci.yml`
(`coverage-min`), and 85% per package in `make coverage-pkg-check`
(`COVER_PKG_MIN`), run by the same CI job. The gate's packages exclude:

| Excluded | Why |
|---|---|
| `examples/` | Example programs, not library code |
| `interop` | Runs only with Docker and the peers |
| `e2e`, `internal/leak` | Tests and a test helper |
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
