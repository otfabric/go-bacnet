# Server plan

Status: **phases 0–5 implemented in go-bacnet v0.6.0; reverse interop
with three independent clients** (bacnet-stack, BACnet4J and BACpypes3
client modes, bacnet-interop v0.12.0 and later; bacnet-stack without segmentation). What is implemented and how it is verified is in
[SERVER_SUPPORT.md](SERVER_SUPPORT.md); where the implementation departs from
this plan is listed under [Deviations](#deviations-from-the-plan).
It scopes a BACnet/IP server (a device) for go-bacnet next to the existing
client, so that go-bacnet can be tested client ↔ server internally and
against independent stacks in both directions. Peer-side work is in
bacnet-interop [docs/CLIENT_DRIVER_CONTRACT.md](https://github.com/otfabric/bacnet-interop/blob/main/docs/CLIENT_DRIVER_CONTRACT.md).

Baseline: ANSI/ASHRAE 135-2024 ([STANDARD_BASELINE.md](STANDARD_BASELINE.md)),
Annex J over IPv4. Clause numbers below are the sections an implementation
must cite; behaviour is not specified here beyond what the standard says.
They are from memory of the edition structure and are checked against the
2024 text when each phase starts.

## Goals

1. A library-grade BACnet/IP device: a Device object, standard objects, the
   data-sharing, device-management, COV and alarm/event services a supervisory
   client uses, and optional BBMD.
2. Full internal end to end: every client service that has a server
   counterpart is exercised go-bacnet client → go-bacnet server, in process
   and over UDP, with the same chaos and race discipline as the client.
3. Reverse interop: independent client stacks drive the go-bacnet server
   (bacnet-interop client-driver images). Together with today's direction
   that gives a client × server matrix with evidence per cell.

## Non-goals (this plan)

- BACnet/IPv6, BACnet/SC, MS/TP, routing between networks (no router role).
- BTL listing or a conformance claim. The PICS (Annex A) is written from the
  evidence, not ahead of it.
- Persistence. The application owns storage; the server keeps state in memory
  and offers hooks.
- Object types beyond the list in "Objects". Others plug in through the
  `Object` interface.
- VT, audit-log server, authentication and life-safety operations: decoders
  exist, but no server behaviour is planned until a client use case asks.

## Starting point

What can be reused from the client today:

| Area | Reusable | Missing for a server |
|---|---|---|
| BVLC / NPDU / APDU codecs | All, incl. Error/Reject/Abort/SegmentACK `Append*` | — |
| Service codecs | Request decoders for RP, WP, WPM, ReadRange, SubscribeCOV(Property)(Multiple), CreateObject, DeleteObject, DCC, ReinitializeDevice, list elements, files, alarm/event, Who-Is/Who-Has; ACK encoders for RP, RPM, ReadRange, CreateObject, files, alarm/event summaries; `EncodeIAm`, `EncodeIHave`, `EncodeEventNotification`, `EncodeCOVNotificationMultiple`, `EncodeWritePropertyMultipleError` | `DecodeReadPropertyMultiple` (request), `EncodeCOVNotification` (single), any ACK encoder found missing per service |
| Transport | `client.Transport`, UDP transport with per-interface broadcast, `internal/virtual`, `internal/faulty` | Lives in `client`; must be shared without the server importing `client` |
| Segmentation | Segmented request send, segmented ComplexACK receive (client) | The opposite pair: segmented request **receive**, segmented ComplexACK **send** with window and SegmentACK handling (clause 5, responding state machine 5.4.5) |
| Confirmed receive | Client already SimpleACKs notifications and Rejects/Aborts other confirmed requests | Real dispatch, responder state machine, duplicate detection |
| BBMD | Client side of FD registration, BDT/FDT reads/writes | BDT/FDT tables, forwarding, FD registration server (Annex J.4, J.5) |

## Package layout

```text
go-bacnet/
├── bip/              + Transport interface, UDP transport (moved from client)
├── internal/
│   ├── segment/      shared segmentation windows, timers, reassembly
│   └── link/         shared receive-loop helpers (parse chain, diagnostics)
├── client/           unchanged API; client.Transport = bip.Transport (alias)
├── server/           new: runtime, dispatch, responder TSM, COV/event engine
│   └── object/       new: standard object implementations
├── bbmd/             new, optional: BDT/FDT and forwarding on a bip.Transport
└── e2e/              client ↔ server end-to-end suites
```

Dependency rules (added to `internal/imports`):

| Package | May import | Must not import |
|---|---|---|
| `server` | root, codecs, `service`, `bip`, `internal/*`, `server/object` | `client`, `device`, `bbmd` |
| `server/object` | root, `service` | `server` runtime, `client`, codecs below `service` |
| `bbmd` | root, `bvlc`, `bip`, `internal/*` | `client`, `server` |
| `client` | as today | `server`, `bbmd` |
| `e2e`, `interop` | everything | — |

Moving `Transport` and the UDP implementation to `bip` is a v0 API change; a
type alias in `client` keeps existing callers compiling. The move is phase 0
and its own PR.

## Object model

```go
// Object is one BACnet object served by a Device. Implementations must be
// safe for concurrent use; the server never holds a lock while calling them.
type Object interface {
    Identifier() bacnet.ObjectIdentifier
    Name() string
    ReadProperty(ctx context.Context, ref bacnet.PropertyReference) (bacnet.ApplicationValue, error)
    WriteProperty(ctx context.Context, w Write) error
    // all properties in a stable order, and the required ones (RPM ALL /
    // REQUIRED / OPTIONAL)
    Properties() (all []bacnet.PropertyIdentifier, required map[bacnet.PropertyIdentifier]bool)
}
```

- Errors returned by objects are `*bacnet.ErrorResponse`-shaped class/code
  pairs (clause 18); anything else is mapped to `device/operational-problem`
  and reported as a diagnostic, never leaked.
- `Device` holds the object table (identifier and name indexes; Object_Name
  unique within the device, clause 12), the Device object (12.11): Object_List, Protocol_Services_
  Supported and Protocol_Object_Types_Supported **computed** from what is
  registered, Database_Revision incremented on create/delete/rename,
  Max_APDU_Length_Accepted, Segmentation_Supported, APDU timeout/retries,
  System_Status, Local_Time/Local_Date from an injectable clock.
- Commandable objects get a shared priority array and Relinquish_Default
  (19.2); `NULL` relinquishes.
- Optional interfaces: `COVReporter` (COV_Increment, value change),
  `EventReporter` (intrinsic reporting, clause 13), `ListModifier`
  (Add/RemoveListElement), `RangeReader` (ReadRange), `FileAccessor` (atomic
  read/write), `Deletable` (DeleteObject) and `Poller` (periodic work such as
  Trend Log, Calendar and Schedule).
- Creation goes through `Device.RegisterFactory`, one factory per object
  type.
- Application hooks (`server.Hooks`): `BeforeWrite` (veto or accept),
  `BeforeDelete`, `Reinitialize`, `TimeSync`, `PrivateTransfer`,
  `TextMessage` and `WriteGroup`. Hooks run off the receive loop with the
  request's deadline. A service that acts only through its hook
  (time synchronization, private transfer, text message, WriteGroup) is not
  listed in Protocol_Services_Supported without it, and a confirmed request
  then gets Reject unrecognized-service, as bacnet-stack does for a service
  it does not handle.

### Objects (built in, `server/object`)

| Priority | Objects |
|---|---|
| 1 | Device, Analog Input/Output/Value, Binary Input/Output/Value, Multi-state Input/Output/Value |
| 2 | Notification Class, Trend Log (buffer + ReadRange), File (stream and record) |
| 3 | Calendar, Schedule, Characterstring Value, Integer / Positive Integer Value, Structured View |

## Services

Per service: the clause, and the client API that the internal e2e suite uses
against it. "Phase" refers to the phases below.

| Phase | Service | Clause | Notes |
|---|---|---|---|
| 1 | Who-Is / I-Am | 16.10 | Range filter, unicast or broadcast reply |
| 1 | Who-Has / I-Have | 16.9 | By name and by identifier |
| 1 | ReadProperty | 15.5 | Array index, Device wildcard instance (4194303) |
| 1 | ReadPropertyMultiple | 15.7 | ALL / REQUIRED / OPTIONAL, per-property errors |
| 1 | WriteProperty | 15.9 | Priority, NULL relinquish, datatype/range checks |
| 1 | WritePropertyMultiple | 15.10 | In order, stop at first failure, WritePropertyMultiple-Error |
| 1 | Error / Reject / Abort | 18, 5.4 | Unrecognized service, missing parameters, buffer overflow |
| 2 | Segmented ComplexACK send / segmented request receive | 5, 5.4.5 | Window size negotiation, SegmentACK, timers; Abort when not supported |
| 2 | Repeated confirmed requests | 5.4.5 | Same source + invoke ID while the request executes: dropped. After the response, the same invoke ID starts a new transaction (clause 5.4.5); bacnet-stack tools reuse invoke IDs |
| 2 | DeviceCommunicationControl | 16.1 | Disable / disable-initiation, duration, password |
| 2 | ReinitializeDevice | 16.4 | Hook only; password |
| 2 | TimeSynchronization / UTCTimeSynchronization | 16.7, 16.8 | Hook only |
| 2 | ReadRange | 15.8 | By position, sequence number, time |
| 3 | SubscribeCOV / SubscribeCOVProperty | 13.14, 13.15 | Lifetime, cancellation, confirmed and unconfirmed notifications, Active_COV_Subscriptions |
| 3 | SubscribeCOVPropertyMultiple / COVNotificationMultiple | 13.16, 13.17 | Closes the end-to-end gap in PLAN.md priority 4 |
| 3 | Event notifications, AcknowledgeAlarm, GetEventInformation, GetAlarmSummary, GetEnrollmentSummary | 13 | Intrinsic reporting on analog, binary and multi-state input and value objects via Notification Class recipients |
| 4 | CreateObject / DeleteObject | 15.3, 15.4 | Factories per type; Database_Revision |
| 4 | AddListElement / RemoveListElement | 15.1, 15.2 | |
| 4 | AtomicReadFile / AtomicWriteFile | 14.1, 14.2 | |
| 4 | ConfirmedPrivateTransfer / UnconfirmedPrivateTransfer, TextMessage, WriteGroup | 16.2–16.6, 15.11 | Hooks |
| 5 | BBMD: BDT, FDT, Register-Foreign-Device, Distribute-Broadcast-To-Network, Forwarded-NPDU | Annex J.4, J.5 | `bbmd` package; opt-in. Distribute-Broadcast-To-Network only from a registered foreign device (J.4.5) |

Unlisted confirmed services get a Reject (unrecognized-service), as the
client does today.

## Runtime

- One `server.Server` per BACnet/IP endpoint: one transport, one Device.
  A process can run a client and a server on separate ports, or share one
  endpoint later (out of scope until there is a use case).
- One receive loop parses and classifies. Confirmed requests are handed to a
  **bounded** worker pool (default small, configurable); when it is full the
  server answers Abort (out-of-resources) instead of queueing without bound.
- Responder transaction table keyed by (source, invoke ID): requests in
  progress, segmented reassembly, segmented reply windows. Bounded like the
  client's state ([ARCHITECTURE.md](../ARCHITECTURE.md#state-and-bounds)).
- COV and event engines: one goroutine each, driven by object change
  callbacks and an injectable clock. Outbound confirmed notifications use the
  server's own requester (`server/requester.go`, unsegmented requests only);
  the segment window and sequence rules (`internal/segment`) and the BVLC/NPDU
  framing (`internal/link`) are shared with the client, not the `client`
  package.
- No package-level state (hard rule 5 in AGENTS.md); options are validated in
  `New`, logging through an injected `slog.Logger`, silent by default.
- `Close` stops accepting, aborts in-flight transactions, cancels hooks'
  contexts and waits for every goroutine (`internal/leak` in tests).

## Testing

| Layer | What |
|---|---|
| Unit | Each service handler against encoded requests, including every error path in the clause |
| Fuzz | New request decoders; the server's receive path as one fuzz target (no panic, bounded memory, always a well-formed reply or silence) |
| e2e (`e2e/`) | go-bacnet client ↔ go-bacnet server over `internal/virtual` and over UDP loopback: every row of the services table |
| Chaos | Server variant of `client/chaos_test.go`: many concurrent clients, drops, duplicates, reordering, broken segments, abrupt Close; invariants: no leaked transactions/subscriptions when idle, no double execution of a duplicate, Database_Revision monotonic |
| Race | `make test-race`, `make chaos` extended to `./server` |
| Dissector | Server replies checked with tshark (`make dissector`) |
| Interop | bacnet-interop client-driver images against the server; the existing peer-device suites unchanged |
| bacnetctl | The CLI against the server as a second consumer test |

Coverage stays ≥ 90 (`COVERAGE_MIN`).

## Phases

| Phase | Deliverable | Exit criterion |
|---|---|---|
| 0 | Move Transport/UDP to `bip`, extract segmentation and the parse chain to `internal`; add missing codecs (`DecodeReadPropertyMultiple`, `EncodeCOVNotification`) | Client API unchanged apart from the alias; `make check`, `make chaos`, `make interop-pinned` green |
| 1 | `server` + `server/object` priority 1; phase-1 services; e2e for them | e2e green; bacnet-stack command-line client (bacnet-interop) passes RP/RPM/WP/WPM/Who-Is/Who-Has |
| 2 | Segmentation, duplicate handling, DCC, reinitialize, time sync, ReadRange, Trend Log | Segmented RPM read by BACnet4J and BACpypes3 clients |
| 3 | COV (incl. multiple) and alarm/event | Subscribe → change → notify against at least two independent clients |
| 4 | Lifecycle, list, file, messaging services; priority 2–3 objects | Multi-peer for each row where a peer client supports it |
| 5 | `bbmd` | Foreign-device registration from two independent clients; broadcast distribution across two subnets |

Each phase: docs/SERVER_SUPPORT.md, API.md, ERRORS.md and RELEASE.md updated
in the same PR.

## Decisions (agreed 2026-11-06)

1. **Repository**: the server lives in go-bacnet, sharing the codecs and one
   module version. No separate go-bacnet-server module.
2. **Support document**: a separate `docs/SERVER_SUPPORT.md`, with the same
   evidence vocabulary as CLIENT_SUPPORT.md; the client document is not
   extended with a server column.
3. **Handler execution**: every confirmed-request handler runs on the bounded
   worker pool, read-only services included; the receive loop only parses,
   classifies and answers what needs no handler (Reject, Abort, duplicates).
   The pool size default is set in phase 1 from benchmark evidence.
4. **Releases**: phase 0 (refactor and missing codecs) and the server
   phases ship together as v0.6.0. (Agreed as v0.5.0 and v0.6.x; v0.5.0 was
   released with other work before phase 0 landed, and a released version
   is not reused. Phase 0 was merged with the server, so it has no release
   of its own.)

## Deviations from the plan

Recorded when the phases were implemented, and what became of each:

| # | Deviation | Now |
|---|---|---|
| 1 | Versions: phase 0 and the server are both v0.6.0 | Plan aligned (Decision 4). v0.5.0 is released and cannot be reused; phase 0 was merged with the server |
| 2 | No shared segmentation or parse-chain package | Closed: `internal/segment` (window, sequence and reassembly rules) and `internal/link` (Forwarded-NPDU origin, B/IP MAC, outbound framing) are used by client and server |
| 3 | Object model: `Name`/`Properties`, `object.Write`, the optional interface and hook names | Plan aligned (Object model). The implemented names are the public v0 API |
| 4 | Priority-3 objects not built in | Closed: Calendar, Schedule, CharacterString / Integer / Positive Integer Value and Structured View |
| 5 | Hook-only services are not advertised without their hook | Plan aligned (Object model). Advertising a service the application does not handle would make the device claim what it cannot do; bacnet-stack behaves the same |
| 6 | AtomicWriteFile past the end refused | Closed: a stream file is zero-filled up to the start, records are appended after the last one, as bacnet-stack's storage does (ports/posix/bacfile-posix.c) |
| 7 | Distribute-Broadcast-To-Network from registered foreign devices only | Plan aligned (Services). This follows the Annex J.4.5 text; bacnet-stack does not check. Accepting it from any host would let unregistered hosts have the BBMD broadcast on their behalf |
| 8 | GetEnrollmentSummary enrollment filter ignored | Closed: decoded and applied against Recipient_List (same device, or same network and MAC, and process identifier) |
| 9a | Notification Class Priority 0 meant 255 in the configuration | Closed: `NotificationClassConfig.Priority` is a pointer; nil means 255, 0 is priority 0 |
| 9b | No confirmed notification to a remote-station recipient | Closed: sent through the router learnt from SNET and I-Am-Router-To-Network (clause 6.4); Who-Is-Router-To-Network while none is known |
| 9c | No confirmed notification to a broadcast recipient | Kept: a confirmed request is a transaction with one peer (clause 5.4). Reported as a diagnostic |
| 10 | Multi-state points have no intrinsic reporting | Closed for Multi-state Input and Value (CHANGE_OF_STATE 13.3.2, FAULT_STATE 13.4.5). A Multi-state Output does not report: its algorithm is COMMAND_FAILURE on Feedback_Value (clause 12.19), which is not implemented |
| 11 | Exit criteria naming independent clients | Met for phases 1–5 except broadcast distribution across two subnets: bacnet-stack, BACnet4J and BACpypes3 client modes pass the phase 1, 3 (COV subscribe and notification, events), 4 and 5 (foreign-device registration) scenarios, and BACnet4J and BACpypes3 the phase 2 segmentation scenarios. BBMD-to-BBMD forwarding has no independent evidence |
| 12 | Duplicate requests answered from a cache | Closed in favour of clause 5.4.5: a request with the invoke ID of a completed transaction is a new request. The cache answered a second DeleteObject from bacnet-stack with the first one's success |
| 13 | Server confirmed requests use their own requester, not the client's | Kept: the server sends only unsegmented notifications; the client's requester carries segmentation, retries per target and the device-management guard, none of which a notification needs |

Not done from the Testing table: the e2e suite runs over UDP loopback, not
also over `internal/virtual` (the server's chaos and unit tests use it), and
bacnetctl has not been run against the server.
