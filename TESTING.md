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
| Fuzz | `make fuzz` (`FUZZTIME`, default 5s); `make fuzz-list` prints the targets | none | 10s per target in `ci.yml`; 5m per discovered target in `fuzz-nightly.yml` |
| Benchmarks | `make bench` | none | not run in CI |
| Cross-compile | `make cross` | none | not run in CI |
| 32-bit | `GOARCH=386 go test -count=1 ./...` | a host that executes `386` binaries (linux/amd64) | `fuzz-nightly.yml`, job `test-386` |
| Interop | `make interop-pinned`, or `make interop` | Docker, a `bacnet-interop` checkout; `make interop-pinned` also needs `jq` | `interop.yml` |
| Dissector oracle | `make dissector` | `tshark` | `interop.yml` |
| Recorded traffic, golden | part of `make test` | none | part of the unit tier |
| Recorded traffic, the two captures over 50 MB | `make captures-extended` | network (kargs.net), 161 MB | not run in CI |
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
([testdata/captures](testdata/captures/README.md)). They run in the unit
tier, offline: `TestCorpus`, `TestCorpusReplay` and
`TestCorpusServerParity` over every committed capture, and on the golden
excerpts `internal/capture` compares every datagram
with Wireshark's decode and encodes it again, `client`'s
`TestCaptureReplay*` runs the client against the recorded answers (and with
segments lost, duplicated or swapped), `server`'s `TestCaptureServer*`
compares the server's answers with the devices', and the fuzz targets are
seeded with the recorded octets. `make captures-extended` downloads the two
captures over 50 MB and runs the corpus suites over them too.

`internal/leak` fails `client`, `server`, `bbmd`, `internal/clock` and `e2e` when a test leaves a
goroutine behind; it compares goroutine stacks, so the failure names it.

The codecs and the capture reader convert 32-bit wire and file lengths to
`int`, so the suite also runs where `int` has 32 bits: the `test-386` job of
`fuzz-nightly.yml` runs `GOARCH=386 go test -count=1 ./...` every night, on
linux/amd64, which executes `386` binaries. The race detector does not exist
for `386`, so that run is without it. `GOOS=linux GOARCH=386 go vet ./...`
checks on any host that the packages and their tests compile for it.
`TestLengthFieldBounds` (`internal/capture/length_test.go`) is the table of
the pcap and pcapng length fields at the 32-bit boundary values
(`0x7FFFFFFF`, `0x80000000`, `0xFFFFFFF0`, `0xFFFFFFFF`).

## Lifecycle contract

The long-lived types and the rows of the organisation's contract matrix
(otfabric/engineering `standards/concurrency.md`). A row is covered by the
test named, or does not apply for the reason given, so that a missing test
and a row without meaning do not look the same. `bbmd.BBMD` starts no
goroutine and has no Close: no row applies to it.

| Row | `client.Client` | `client.Subscription`, event streams | `server.Server` | UDP transport (`bip`) |
|---|---|---|---|---|
| L1 Close twice | `TestCloseIdempotent` | `TestCOVTerminalClosedWithFullChannel` | `TestUDPServer` | `bip/udp_internal_test.go` |
| L2 Two goroutines close | `TestCloseFromTwoGoroutines` (`client`) | n/a: closed by its owner or by the Client | `TestCloseFromTwoGoroutines` | n/a: closed by its owner |
| L3 Close during a blocked read | `TestClientCloseDuringReadProperty`, `TestRequestDuringCloseReportsErrClosed`, `TestDiscoverObjectsClosedDuringWait` | closed with the Client (`Close` closes all subscriptions) | `TestCloseWhileHookWaits` (also: `IAm` after `Close` matches `ErrClosed`) | `TestUDPTransportRecvAfterClose` |
| L4 Close during a write | n/a for datagrams (no stream, no partial PDU); segmented send: `TestSegmentedSendClosedDuringWait` | n/a | n/a for datagrams; a segmented reply ends at `Close` (`TestSegmentedReplyDoesNotHoldWorker`) | n/a |
| L5 Close during a handshake | n/a: no association; foreign-device registration: `TestBVLCExchangeClosedAndInvalid` | n/a | n/a | n/a |
| L6 Cancel a pending request | `TestReadPropertyCancelDuringWait`, `TestReadPropertyContextCancelWrapsOutcomeUnknown`, `TestSegmentedSendCancelOutcomeUnknown`, `TestTxCompleteQuarantine` | n/a | the server's own requests: `TestCOVRenewalReleasesPendingNotification` | n/a |
| L7 Response after timeout, or not for this request | `TestTryAllocQuarantineAndExhausted`, `TestReadPropertyWrongSourceIgnoredThenTimeout` | n/a | `TestRequesterAnswers` (wrong source, unknown invoke ID), `TestRequesterAnswerForAnotherService` | n/a |
| L8 Response after reconnect | n/a: connectionless; invoke-ID reuse is the quarantine of L7 | n/a | n/a | n/a |
| L9 Callback calls a public method | not allowed (API.md, "Event delivery"); no test | not allowed; no test | `TestHookCallsServerMethods`; table changes under an object's lock: `TestDeviceTableChangeDuringEventEvaluation`, `TestDeviceRenameDuringTableRead` (`server/object`) | n/a: no callback |
| L10 Callback calls Close | not allowed (`Client.Close`); no test | not allowed; no test | `TestCloseFromApplicationCode` (worker, receive loop, housekeeping, event and notification goroutines), `TestCloseFromTwoHooks` | n/a |
| L11 Slow callback | `TestEventDispatcherOverflowDropNewest`, `TestCOVSendGapAndTerminalWithFullQueue` | the same | `TestSlowHookHoldsOnlyItsWorker`, `TestSegmentedReplyDoesNotHoldWorker`; a slow hook on the housekeeping goroutine delays it (API.md, "Hooks"; no test) | n/a |
| L12 Queue at capacity | the tests of L11, `TestCOVTerminalClosedWithFullChannel` | the same | `TestSlowHookHoldsOnlyItsWorker` (worker queue), `TestReassemblyLimits`, `TestCOVReferenceBounds`, `TestActiveCOVSubscriptionsBounded`, `TestConfirmedNotificationsPerDestination`, `TestAddressCacheFullMakesRoom`, `TestRouteTableFullMakesRoom` | n/a |
| L13 Concurrent sessions | `TestConcurrentRequests` (e2e), `TestChaosConcurrentRequests` | the same | `TestChaosServer` | n/a |
| L14 Failure during construction | `TestNewRejectsInvalidOptions` | n/a | `TestOptionsValidation`, `TestUDPServer` (address that cannot be bound) | `bip/udp_internal_test.go` |
| L15 Shutdown(ctx) | n/a: no graceful shutdown | n/a | n/a: no graceful shutdown | n/a |
| L16 Close racing reconnect | n/a: no reconnect; foreign-device renewal stops at `Close` | n/a | n/a | n/a |

Application code that does not return normally is a contract of its own
for the server: `TestApplicationCodeThatDoesNotReturnNormally`,
`TestCOVNotificationGoesOnAfterObjectFailure`,
`TestDiagnosticSinksThatDoNotReturnNormally`.

Open rows: L9 and L10 for the
client, where the documented answer is "not allowed" and no test shows what
happens.

## Waiting in tests

A test waits for an event, never for an amount of time.

- The `server` tests on the virtual transport run in a `testing/synctest`
  bubble (`inBubble` in `server/harness_internal_test.go`): the server, its
  timers and the test share a virtual clock. "The server has done all it
  will do" is `idle()` (`synctest.Wait`), "nothing is sent" is
  `h.silent(d)`, and a `time.Sleep` in such a test moves the bubble's clock,
  it does not wait. The bubble ends only when every goroutine the server
  started has ended, which is the leak check of these tests.
- `harness.next` waits for the transport to record a packet
  (`virtual.Transport.Sent`), in and outside a bubble.
- `-count=30 -cpu=1,8` over these tests passes; that is the check to repeat
  after a change to them.

Left with sleeps as synchronisation, to be converted the same way:

| Where | What | Why not yet |
|---|---|---|
| `server/bbmd_peer_test.go` (2 tests) | sleeps before asserting what two linked servers sent | no harness: two servers and their own outbox polling |
| `server/capture_test.go` (2 tests and the replay helper) | polls the outbox with 1 ms sleeps | replays recorded traffic with its own pacing |
| `server/chaos_internal_test.go` | sleeps between injected changes and in the router loop | pacing of the load, not a wait for a result |
| `client` (110 `time.Sleep` in 42 files, among them `client/segmentation_test.go`) | sleeps after injecting a frame, polling loops | the client tests have no bubble harness yet; the client's injected clock (`internal/clock`) and the virtual transport are what one would be built on |
| `e2e` (2) | polling loops over loopback UDP | real sockets: a goroutine in a socket call never counts as blocked in a bubble |

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
`FuzzDecodeAcknowledgements` and `FuzzDecodeRequests`
(`service/codec_fuzz_test.go`) run every decoder of a whole service message
through the same invariants: the result does not change when the input
buffer does, the result encodes, and the encoding decodes and encodes to the
same octets again. A decoder whose encoder is stricter than it, or whose
round trip is not stable, is marked so in the table with the reason;
`TestCodecTables` fails when an exported payload decoder has no line, and
`TestDecodersAtElementLimit` runs each decoder at and one past the element
limit with an allocation budget.

A crash found by `make fuzz` is written to
`<package>/testdata/fuzz/<Target>/`; the nightly job uploads corpus and
crashers as an artifact. To promote one: copy the file there if it came from
the artifact, fix the decoder, and commit the file, so that `go test` replays
it.

The nightly matrix is discovered the same way. The `discover` job of
`fuzz-nightly.yml` runs `make fuzz-list`, which prints one
`<package> <target>` line per Fuzz function in the packages of `FUZZ_PKGS`,
and the fuzz job runs one matrix entry per line for 5 minutes. A new target
in one of those packages needs no change to the workflow; a target in
another package needs that package in `FUZZ_PKGS`. The job fails when the
list is empty or when two packages use the same target name, which names the
job and its artifact.

The nightly job runs on Go 1.27, not on the `go.mod` minimum. Go 1.26 can
end a fuzz run whose `-fuzztime` has expired with `context deadline exceeded`
and no failing input ([go.dev/issue/75804](https://go.dev/issue/75804)). Such
a failure records nothing to promote. A failure with a crasher file always
has one.

## When CI runs

| Event | Workflow | What runs |
|---|---|---|
| Pull request | `pr.yml` | `docs.yml` (docsync, otfabric-policy) always; `ci.yml`, `interop.yml` unless every changed file is documentation; then `OT Fabric Required Checks` |
| Push to a branch | none | nothing; dispatch `ci.yml`, `interop.yml` to test a branch without a pull request |
| Push to `main` | `docs.yml` | the light checks; the merged commit was tested as the pull request's merge commit |
| Schedule | `fuzz-nightly.yml` | on `main`: every fuzz target for 5 minutes, and the tests as a 32-bit binary (`GOARCH=386`) |
| Release | `release.yml` | the release workflow tests the release commit again, and builds, vets and tests the direct dependants (bacnetctl and bacnet-interop `own/go-bacnet`) against it before the tag is created |

Documentation means an added or modified `*.md` file outside `.github/`, `testdata/` and `vendor/`, other than `AGENTS.md` and `CLAUDE.md`. A deleted or renamed document, or any other file, runs everything. `OT Fabric Required Checks` fails unless every job that applies succeeded; its summary lists them.
