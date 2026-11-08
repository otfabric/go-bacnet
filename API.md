# API reference

Behavioural contract for the BACnet/IP client and server. Prefer this document over guessing from
sibling OT Fabric libraries. Generated GoDoc remains the source for signatures.

## Package dependency direction

```text
bacnet (root leaf types)
  ↑
  ├── bip          BACnet/IP endpoints, Transport, UDP transport (not root)
  ├── bvlc         BVLC framing
  ├── npdu         network PDU
  ├── apdu         application PDU
  ├── service      service payloads (Who-Is, RP, RPM, WP, COV, …)
  ├── client       supervisory runtime (composes layers + bip)
  ├── server       BACnet/IP server runtime (composes layers + bip)
  │   └── object   Device and standard objects
  └── bbmd         optional BBMD for a server
```

Rules (enforced by `internal/imports`):

- Root `bacnet` must not import `bip`, `bvlc`, `npdu`, `apdu`, `service`, or `client`.
- Sibling codecs (`bvlc`, `npdu`, `apdu`) may import root only; they must not import each other or `client`.
- `service` may import root and `apdu` helpers as needed; it must not import `bvlc`, `npdu`, `bip`, or `client`.
- `client` is the composition root for BACnet/IP client networking, `server`
  for serving a device; neither imports the other, and `server/object` imports
  only root and `service`.

See [ARCHITECTURE.md](ARCHITECTURE.md).

## Framing policy

`apdu.Parse` and `npdu.Parse` are strict: reserved bits, undefined MaxAPDU
codes, MoreFollows without SegmentedMessage, invalid segment windows, global
broadcast with non-zero DADR, and malformed router network lists return
`ErrMalformed`. See [PROTOCOL.md](PROTOCOL.md).

## Address and MAC

`bacnet.Address` is data-link independent:

| Constructor | Meaning |
|-------------|---------|
| `LocalStation(mac)` | Local unicast |
| `LocalBroadcast()` | Local broadcast |
| `RemoteStation(net, mac)` | Remote unicast |
| `RemoteBroadcast(net)` | Remote broadcast |
| `GlobalBroadcast()` | Global broadcast |

`bacnet.MAC` is immutable and comparable (max 7 octets retained for BACnet/IP).
Use `NewMAC` / `MustMAC`. `Bytes()` returns a copy.

BACnet/IP UDP peers are **`bip.Endpoint`** (`netip.AddrPort`). They are intentionally
**not** in the root package so MS/TP, IPv6, and SC can attach later without
polluting leaf types.

## Application values

`bacnet.ApplicationValue` is the generic decoded value model (Null, Boolean,
Unsigned, Signed, Real, Double, OctetString, CharacterString, BitString,
Enumerated, Date, Time, ObjectIdentifier, constructed/context nesting).

Helpers: `NullValue`, `BoolValue`, `UnsignedValue`, `SignedValue`, `RealValue`,
`DoubleValue`, `EnumValue`, `ObjectIDValue`, plus `AsReal` / `AsUnsigned` /
`AsBool` / `AsObjectID`.

Unknown enumerations, object types and property identifiers decode without
hard failure when the wire encoding is valid.

## Client lifecycle

```go
c, err := client.New(opts...)
if err != nil {
    return err
}
defer c.Close()
```

- One `Client` is one local BACnet/IP data-link endpoint: one UDP socket,
  bound to port 47808 on all interfaces unless `WithPort` / `WithLocalAddr`
  say otherwise.
- `New` performs I/O: it validates the options (`ErrInvalidConfig`), opens
  the socket and starts the receive loop and, with `WithForeignDevice`, the
  registration loop. It does not wait for the registration; see
  `ForeignDeviceRegistered`.
- A `Client` is safe for concurrent use. Requests are multiplexed on the one
  socket by invoke ID (up to 255 in flight); a request that finds none free
  waits until its context ends.
- `Close` is idempotent and safe to call concurrently with requests. Pending
  requests return `ErrClosed` (wrapped in `OutcomeUnknownError` for a write
  that was already sent), COV subscriptions end with a `Closed` event, the
  socket is closed and all client goroutines have exited when it returns.
  It does not cancel COV subscriptions on the devices and does not unregister
  the foreign device; both lapse with their lifetime. Do not call `Close`
  from an event handler or diagnostic callback.
- `WithTransport` replaces the UDP socket (tests, custom data links). The
  client owns the transport from then on: `Close` closes it, and so does a
  failing `New`. The `Transport` contract is in its GoDoc. `client.Transport`,
  `InboundPacket` and `OutboundPacket` are aliases of the `bip` types since
  v0.6.0, where the UDP transport (`bip.ListenUDP`) now lives.

### Addressing a device

Confirmed calls take a `client.Target`:

| Device | `Address` | `Endpoint` |
|---|---|---|
| Directly reachable over BACnet/IP | leave zero (filled in from the endpoint) or `LocalStation(mac)` | the device's IP and port |
| Behind a BACnet router | `RemoteStation(network, mac)` | the router's IP and port |

`DeviceObservation.Target()` builds the right form from a discovery result,
`ResolveTarget` fills in the router for a remote address from the route cache.
`Target.MaxAPDU` overrides the size limit used for requests; zero uses what
the device announced in its I-Am, or 480 octets if it was never seen.
Responses are accepted only from the path the target describes; anything else
is dropped and the request times out. A `Target` without a valid endpoint
fails with `ErrMalformed` before anything is sent.

### Timeouts and cancellation

| Phase | Ends with |
|---|---|
| Waiting for a free invoke ID | `ctx.Err()` |
| Sending | the transport error; for a write, wrapped in `OutcomeUnknownError` |
| Waiting for the response | `ErrTimeout` after APDU timeout × attempts, or `ctx.Err()` |
| Receiving a segmented response | `*AbortError` (tsm-timeout) when a segment is overdue |
| `Close` during any phase | `ErrClosed` |

Defaults: APDU timeout 3 s, 3 retransmissions, segment timeout 2 s
(`WithTransactionOptions`). Only services that are safe to repeat are
retransmitted (reads); a write is sent once. The timeouts are per client and
per request: a deadline on one call never affects another. Cancelling a
request releases its invoke ID after a quarantine of one APDU timeout, so a
late answer cannot be taken for the answer to a later request; the socket and
other requests are unaffected. For a write, cancellation or timeout after the
send is reported as `OutcomeUnknownError`, and `errors.Is` still finds
`context.Canceled`, `context.DeadlineExceeded` or `ErrTimeout` inside it.

BVLC management calls (`ReadBroadcastDistributionTable` and the like) and the
foreign-device registration use the APDU timeout once, without retries.

## Discovery

- `SendWhoIs(ctx, dest, broadcast, opts)` — directed or broadcast Who-Is.
- `Discover(ctx, opts)` — broadcast Who-Is, then collect I-Am. With
  `opts.Window` it returns after that time with a nil error; without it, it
  collects until `ctx` ends and returns `ctx.Err()`. Either way it returns the
  devices seen since the call started; `Devices()` returns the full retained
  registry snapshot. Results are in no particular order.
- Devices answer by broadcast to port 47808: a client bound to another port
  only sees answers to a unicast `SendWhoIs`.
- A broadcast is one datagram to the directed broadcast address of each
  local IPv4 subnet (all interfaces that are up, or the one named with
  `WithInterface`) plus one to 255.255.255.255 on each of those interfaces
  (on Linux and macOS; elsewhere once, on the interface of the default
  route). Devices differ in which of
  the two their socket receives; one that receives both answers twice, which
  the registry absorbs.
- A device whose own broadcast address is misconfigured, or that sits behind
  an IP router without a BBMD, is not reached by any broadcast: use
  `SendWhoIs` with its address, or `WithForeignDevice`.
- `WithRegistryOptions(opts RegistryOptions) Option` bounds observation
  retention (`RegistryOptions{MaxObservations, MaxPathsPerInstance,
  ObservationTTL}`: global cap, per-instance path cap, TTL; zero fields take
  the defaults 4096 / 8 / 30m).
- `DiscoveryOptions.Address` may override the NPDU destination:
  - unset → GlobalBroadcast when `broadcast` is true; no DNET when false
  - `RemoteBroadcast(dnet)` with `broadcast=false` and `dest=router` probes a
    remote BACnet network via a known next hop

When foreign-device registration is active, broadcast Who-Is uses
Distribute-Broadcast-To-Network to the configured BBMD.

## Routing

- `WhoIsRouterToNetwork(ctx, network)` — local-broadcast Who-Is-Router-To-Network
  (`network` nil = query all).
- `WhoIsRouterToNetworkAt(ctx, dest, broadcast, network)` — same message to a
  specific hop (use unicast when the router IP is known, e.g. docker topology).
- I-Am-Router-To-Network updates an internal next-hop cache.
- `ResolveTarget(addr, direct)` — for remote addresses, fills `Target.Endpoint`
  from the cache; if unknown and `direct` is invalid, returns `ErrNoRoute`
  (which wraps `ErrUnsupported`).

Confirmed requests to `RemoteStation` / `RemoteBroadcast` encode DNET/DADR and
send the BVLC frame to `Target.Endpoint` (the next hop).

## Foreign device / Forwarded-NPDU

- `WithForeignDevice(fd ForeignDeviceConfig) Option`, with
  `ForeignDeviceConfig{BBMD bip.Endpoint; TTL time.Duration}`, enables
  registration with one BBMD and starts a renewer loop.
- TTL must be a whole number of seconds and at least 2s (0 → default 60s). Wire
  TTL and renew scheduling use the same normalized duration.
- BVLC-Result correlation is by pending attempt + BBMD peer only: the wire has
  no generation field, so a delayed Result for an earlier attempt to the same
  BBMD is indistinguishable from a Result for the current attempt.
- `ForeignDeviceRegistered()` reports whether the last registration succeeded.
- When registered, local broadcasts are sent as Distribute-Broadcast-To-Network
  to the BBMD.
- Forwarded-NPDU is accepted; when FD mode is active, only the configured BBMD
  may be the immediate peer for Forwarded-NPDU.

The client is not a BBMD and does not fail over between BBMDs. A server can be a BBMD with package `bbmd` ([BBMD](#bbmd)).

## Confirmed-service policy registry

Every typed confirmed helper looks up `ConfirmedServicePolicy` (safety) and
`ConfirmedServiceCapabilities` (legal segmentation). Policy drives retransmission
defaults, outcome-unknown wrapping, and `OperationClass` opt-in
(`WithDeviceManagementEnabled`, `WithNetworkManagementEnabled`).
`SuccessResponse` is SimpleACK or ComplexACK on success only; Error / Reject /
Abort remain valid terminals. Raw `InvokeConfirmed` takes explicit retransmit /
side-effect options.

## Transaction retransmission vs application retry

Exact-APDU **retransmission** is owned by the client transaction manager and is
distinct from any application-level **retry**. Defaults come from the policy
registry (reads typically enabled; writes / COV / device management disabled).

```go
func WithTransactionOptions(apduTimeout time.Duration, retryCount int, segmentTimeout time.Duration) Option
```

`WithTransactionOptions` sets APDU timeout, retransmit count and segment timeout.
A zero timeout keeps its default; `retryCount` 0 disables retransmission; a
negative value makes `New` fail with `ErrInvalidConfig`.
Retries after `OutcomeUnknownError` must treat execution as possibly applied.

## BVLC BDT/FDT and routing

Typed `ReadBroadcastDistributionTable`, `WriteBroadcastDistributionTable`
(network-management opt-in), `ReadForeignDeviceTable`, and
`DeleteForeignDeviceTableEntry` target a BBMD. Non-zero BVLC-Result →
`*BVLCOperationError`.

`ResolveTarget` selects a next hop once; Busy / Available / Reject update route
**state** for later transactions only. Side-effecting requests are never
transparently retried on another route.

## Adaptive helpers

- `ReadPropertyMultipleBatched` splits a read into as many transactions as
  the device's Max APDU requires (by property, also inside one object) and
  returns one result per input specification, in input order. When the device
  aborts because the *reply* did not fit, the batch is halved and retried.
  On error the results gathered so far are returned with it.
- `ReadObjectList` reads the whole Object_List and, when the device cannot
  return it in one reply, falls back to reading it element by element
  (ReadPropertyMultiple, or ReadProperty where that is unsupported).
- `WritePropertyMultipleBatched` sends explicit batches and reports each as
  Completed, Failed or Unknown; it stops at the first batch that did not
  complete and never repeats one.
- `WritePriority` / `RelinquishPriority` command and release one slot of the
  priority array of Present_Value.
- `ReadFileStream` / `ReadFileRecords` / `WriteFileStream` move a file in
  chunks (`WriteFileStream` reports the outcome per chunk). With a negative
  start position (end of file) each `WriteFileStream` chunk after the first
  goes to the position the device's ACK reported plus the chunk length.
- `ReadRangeAll` pages byPosition or bySequenceNumber until the device
  reports LAST_ITEM or a limit (`MaxItems` in records, `MaxBytes`,
  `MaxPages`) is reached; `Partial` and `StoppedReason` say which.
- `device.ReadSnapshot` reads a device's identity and objects through a
  `PropertyReader` (the client does not import `device`).

`ReadProperty` accepts the Device wildcard `bacnet.AnyDevice()` (instance
4194303): a device answers it with its own identifier, which is how to
identify a device known only by address. Not every stack implements it.

## Event delivery

Default `WithEventNotificationHandler` is synchronous on the receive path.
`WithEventDispatcher` adds a bounded worker queue (`Workers`, `BufferSize`,
`Overflow` DropNewest/DropOldest) with panic recovery; Confirmed notifications
are still SimpleACK'd before enqueue. `OpenEventStream` remains an independent
bounded consumer.

## RPM result model

```go
func (c *Client) ReadPropertyMultiple(ctx context.Context, target Target, specs []service.ReadAccessSpecification) ([]service.ReadAccessResult, error)
```

`ReadPropertyMultiple` returns `[]service.ReadAccessResult`.

- Top-level `error` means the whole transaction failed (transport, Abort,
  Reject, malformed ACK, timeout).
- Per-property BACnet errors live in `PropertyResult.Err`; other properties in
  the same ACK may still succeed.
- Always inspect property results even when `err == nil`.

## ReadRange

`ReadRange` returns `service.ReadRangeACK` with `ResultFlags`, `ItemCount`,
`ItemData`, optional `FirstSequence`, and, when the property is
`Log_Buffer`, the typed records of the object's record type:
`LogRecords` (BACnetLogRecord) for a Trend Log or another object,
`LogMultipleRecords` (`service.LogMultipleRecord`, BACnetLogMultipleRecord:
log status, the list of logged values or a time change) for a Trend Log
Multiple, and `EventLogRecords` (`service.EventLogRecord`,
BACnetEventLogRecord: log status, the event notification or a time change)
for an Event Log. `ItemData` remains the flat tag stream for generic
ranges. Inspect `FirstItem` / `LastItem` /
`MoreItems` for paging. Item values are cloned on decode (caller-owned).

## Who-Has / I-Have

`SendWhoHas` / `DiscoverObjects` mirror Who-Is / Discover. I-Have updates a
separate bounded **object** observation registry (`Objects()`), using the same
`WithRegistryOptions` retention policy as device observations. Device and
object registries do not share entries.

## EventNotification / alarms

Inbound Confirmed/Unconfirmed EventNotification is distinct from COV.
`WithEventNotificationHandler` / `SetEventNotificationHandler` delivers decoded
notifications; confirmed indications are SimpleACK'd before the handler runs.
The handler is synchronous on the receive path — return promptly and do not
call Client methods that wait for responses. Handler panics are recovered.
`AcknowledgeAlarm` and `GetEventInformation` are typed initiate helpers.
`EventNotification.Parameters` types common NotificationParameters CHOICEs
(change-of-state, change-of-bitstring, change-of-value, out-of-range);
`NotificationParams` retains the opaque constructed CHOICE body for all cases.

## Device management (opt-in)

`DeviceCommunicationControl` and `ReinitializeDevice` require
`WithDeviceManagementEnabled(DeviceManagementConfirm)`. Without the opt-in,
calls return `bacnet.ErrDeviceManagementDisabled`. These services can mute or
reboot a peer; treat post-send failures as outcome-unknown.

## Experimental: `InvokeConfirmed`

Raw confirmed-request escape hatch for arbitrary service choice + payload
(retransmission disabled). Used by interop probes for Reject/Abort. Prefer typed
helpers for production traffic; this surface may change in v0.x. Callers own the
payload through send. See `client.InvokeConfirmed` GoDoc.

## Diagnostics

The client is silent by default. `WithLogger(*slog.Logger)` logs, and
`WithDiagnosticFunc(func(Diagnostic))` reports, what the client notices that
no caller is waiting for: dropped frames, route changes, failed registration,
dropped notifications. Both run synchronously on the receive and timeout
paths. Kinds, levels and callback rules are in
[OBSERVABILITY.md](OBSERVABILITY.md).

## Requests from devices

The client answers confirmed requests only for the notifications it consumes
(COV, COV-multiple, event, audit): they are acknowledged with a SimpleACK when
they decode and rejected when they do not. Every other confirmed request is
answered with Reject `unrecognized-service`, a segmented one with Abort
`segmentation-not-supported`, so the sender's transaction ends instead of
retrying. Requests in transit to another BACnet network are ignored.

## COV subscription

`SubscribeCOV` / `SubscribeCOVProperty` / `SubscribeCOVMultiple` return a
`Subscription`:

- `Events() <-chan SubscriptionEvent` — notifications, state changes, errors
- `Close()` — best-effort unsubscribe; stops renewals

`SubscriptionEvent` fields:

| Field | Meaning |
|-------|---------|
| `Sequence` | Monotonic per subscription for successfully queued events |
| `Gap` | Sticky loss indicator: true on the next delivered event after one or more drops |
| `Notification` | Parsed COV notification when present |
| `Multiple` | The whole COVNotificationMultiple when the device sent one: its timestamp, every object and value, each value's time of change |
| `State` | Lifecycle state carried with the event |
| `Err` | Optional error (drops, renew failure, close cause) |

`SubscribeCOVMultiple` subscribes to several properties of several objects
with one SubscribeCOVPropertyMultiple request
(`[]service.COVMultipleSubscription`, per property an optional COV increment
and `Timestamped`); `COVMultipleOptions` adds `MaxNotificationDelay`. Its
notification events carry `Multiple` only. It is renewed and cancelled like
the others; the cancellation is the request without
issue-confirmed-notifications and lifetime. A device without the service
answers Reject `unrecognized-service` and no subscription is returned.
`SubscribeCOVPropertyMultiple` remains as the raw, unmanaged request.

States: Pending → Active → Renewing / Degraded → Expired / Closed.
Lifetime defaults to 60s; renewals run at half lifetime. The event channel soft
capacity is `BufferSize` (default 16) with one reserved slot so `Closed` can
still be admitted when the queue is full. Overflow sets sticky `gapPending`,
marks Degraded **without** overwriting Closed, and surfaces `Gap=true` on the
next successful delivery. Routed COV delivery uses the same path matcher as
confirmed responses (remote address + expected next hop). Confirmed-notification
SimpleACKs use a bounded APDU-timeout context; send failures are reported via
diagnostics. Confirmed COV indications are **acknowledged before local
admission**: a syntactically valid notification with an unknown process ID,
wrong object, or wrong route still receives SimpleACK and is then discarded
locally (limits peer retries; not a trust signal).

Client `Close` closes all subscriptions.

## Byte ownership

| Surface | Ownership |
|---------|-----------|
| Codec `Parse` payloads (`bvlc.Message.Payload`, `npdu.NPDU.APDU`, tag octet/bit strings) | May **alias** input buffers |
| Values retained beyond the packet | Call `ApplicationValue.Clone()` |
| `MAC.Bytes()` | Returns a **copy** |
| Outbound encode `Append*` | Appends owned bytes to caller `dst` |
| Transaction encoded APDU | Client copies before store |

Do not retain aliased slices after the datagram buffer is reused.

## Segmentation

- Receiving segmented ComplexACK: transaction-owned reassembly with timeouts,
  MaxSegments, path match before state creation, routed SegmentACK
- Sending segmented confirmed requests: proposed window defaults to 16; actual
  window follows SegmentACK. Receiving segmented ComplexACK defaults to actual
  window 1 (ACK every segment) for peer compatibility. `WithSegmentWindow` sets
  both directions (1..127). Used when the unsegmented APDU exceeds the remote
  max **and** registry evidence shows peer Segmentation is `segmented-both` (0)
  or `segmented-receive` (2). Without that evidence, preflight returns
  `*APDUTooLargeError` with `SegmentationSupported=false`
- Segmented Confirmed-Request and ComplexACK PDUs carry service choice on every
  segment; reassembly validates that the choice stays constant
- Segment timeout / cancel / Close / transport failure during send may emit a
  client Abort; after any transmission attempt, side-effecting services wrap as
  outcome-unknown
- EventNotification handlers run on the receive path: do not block or call
  Client RPCs from the callback (deadlock risk); panics are recovered
- ReadRangeACK with property `Log_Buffer` requires a successful typed split
  into the record type of the object (`LogRecords`, `LogMultipleRecords` or
  `EventLogRecords`); known NotificationParameters CHOICEs reject malformed
  contents (unknown/unimplemented CHOICEs remain opaque)

## Server

Package `server` serves one `object.Device` on one BACnet/IP endpoint.
What it implements and how well that is verified is in
[docs/SERVER_SUPPORT.md](docs/SERVER_SUPPORT.md); its errors are in
[ERRORS.md](ERRORS.md#server-errors). A complete, tested version of the
snippet below is [server/example_test.go](server/example_test.go).

```go
dev, err := object.NewDevice(object.DeviceConfig{Instance: 1234, Name: "dev-1234", VendorID: 999})
av, err := object.NewAnalogValue(object.PointConfig{Instance: 1, Name: "setpoint", Value: 21, Commandable: true})
err = dev.Add(av)
s, err := server.New(dev, server.WithLocalAddr("0.0.0.0:47808"))
defer s.Close()
_ = s.IAm()
_ = av.SetPresentValue(bacnet.RealValue(22)) // COV subscribers are notified
```

### Lifecycle

- `New` validates the options (`ErrInvalidConfig`), opens a UDP socket unless
  `WithTransport` is given, binds the Device to the server and starts the
  receive loop, the workers, housekeeping and the event engine. A Device is
  served by one server at a time.
- `Close` stops receiving, cancels the context of running handlers and
  hooks, ends in-flight transactions, COV subscriptions and pending
  notifications, closes the transport and returns when every server
  goroutine has exited. It is idempotent. It sends nothing: subscribers see
  their subscriptions lapse.
- `IAm` broadcasts an I-Am; it is not sent while DeviceCommunicationControl
  disables initiation.

### Options

| Option | Default | Meaning |
|---|---|---|
| `WithTransport`, `WithLocalAddr`, `WithPort`, `WithInterface` | UDP 47808 | Endpoint; the server owns an injected transport |
| `WithWorkers(workers, queue)` | 4, 64 | Bounded handler pool; a full queue gives Abort out-of-resources |
| `WithMaxAPDU` | 1476 | Max_APDU_Length_Accepted |
| `WithSegmentation(maxSegments, window)` | 64, 4 | 0 segments disables segmentation |
| `WithTransactionOptions(apduTimeout, retries, segmentTimeout)` | 3 s, 3, 2 s | The server's own confirmed notifications and segmented replies; also the Device's APDU properties |
| `WithPassword` | none | Required by DeviceCommunicationControl and ReinitializeDevice when set |
| `WithCOVLimits(max, maxLifetime)` | 256, none | With a lifetime limit, indefinite subscriptions are refused |
| `WithBBMD` | none | A `bbmd.BBMD` sharing the endpoint |
| `WithHooks` | none | Application part of device-level services |
| `WithDecodeLimits`, `WithLogger`, `WithDiagnosticFunc` | | As for the client |

### Objects

`object.Device` is the Device object and the object table. Object_Name is
unique within the device; Object_List, Protocol_Services_Supported and
Protocol_Object_Types_Supported are computed from what is served;
Database_Revision increases when an object is added, removed or renamed.
`RegisterFactory` enables CreateObject for a type.

Built-in objects: analog, binary and multi-state inputs, outputs and values
(`object.Point`), `NotificationClass`, `TrendLog`, `File`, `Calendar`,
`Schedule`, CharacterString / Integer / Positive Integer Values
(`object.Value`: `NewCharacterStringValue`, `NewIntegerValue`,
`NewPositiveIntegerValue`) and `StructuredView`. Any type that
implements `object.Object` can be added; optional interfaces add behaviour:

| Interface | Gives |
|---|---|
| `COVReporter` | SubscribeCOV and COV_Increment handling |
| `EventReporter` | Intrinsic reporting and the alarm services |
| `RangeReader` | ReadRange |
| `ListModifier` | AddListElement / RemoveListElement |
| `FileAccessor` | AtomicReadFile / AtomicWriteFile |
| `Deletable` | DeleteObject |
| `Poller` | A tick from housekeeping (polled logging, Calendar and Schedule evaluation) |

Intrinsic reporting is configured with `PointConfig.Events`
(`object.EventConfig`): limits for analog points, `AlarmValue` for binary
points, `AlarmValues` and `FaultValues` (states 1..Number_Of_States) for
Multi-state Input and Value objects. A Multi-state Output with `Events` is
refused. `NotificationClassConfig.Priority` is a `*[3]uint8` (to-offnormal,
to-fault, to-normal); 0 is a valid priority, and nil starts all three at
255, as bacnet-stack does.

`Calendar` and `Schedule` follow the server's clock: each housekeeping tick
(about once a second) re-evaluates Calendar Present_Value against the local
date and Schedule Present_Value against the local date and time, and a
Schedule writes a changed value to the properties in
List_Of_Object_Property_References that belong to objects of the same
device, at Priority_For_Writing (`object.ScheduleConfig`). A Schedule's
special events may name a `Calendar` (`bacnet.SpecialEvent.PeriodObject`).

Objects are safe for concurrent use: the application may change values
(for example `Point.SetPresentValue`, `TrendLog.Log`) while the server reads
them. Errors are BACnet errors made with `object.Err`; anything else is sent
as device/operational-problem.

### Hooks

`Hooks` functions run on a worker with the request's context:
`BeforeWrite` and `BeforeDelete` can refuse with a BACnet error;
`Reinitialize` performs a ReinitializeDevice (without it COLDSTART,
WARMSTART and ACTIVATE_CHANGES are acknowledged and the backup and restore
states START_BACKUP to ABORT_RESTORE get services/service-request-denied);
`TimeSync`, `PrivateTransfer`,
`TextMessage` and `WriteGroup` are the whole service, so without them the
service is not advertised and a confirmed request gets Reject
unrecognized-service.

### Requests and answers

- Every confirmed request runs on a worker; the reply is segmented when it
  does not fit the requester's Max APDU and the requester accepts
  segmentation.
- A repeated request (same requester, invoke ID and content) that arrives
  while the first is still executing is dropped. Once the reply is sent the
  transaction is released (clause 5.4.5): a later request with the same
  requester and invoke ID is a new request and is executed.
- Confirmed requests sent to a broadcast address and requests for another
  network are ignored: the server is not a router. It learns the router of
  a remote network from received SNET and I-Am-Router-To-Network (at most
  64 networks) to send event notifications to remote stations.

### BBMD

`bbmd.New(bbmd.Config{Self, BDT, MaxForeignDevices, RejectRegistrations})`
returns a BBMD to pass to `server.WithBBMD`. `BDT()` and `FDT()` return the
tables; expired foreign devices are dropped after their TTL plus
`bbmd.GracePeriod`. Write-Broadcast-Distribution-Table is refused with a NAK.

