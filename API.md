# API reference

Behavioural contract for Horizon 1. Prefer this document over guessing from
sibling OT Fabric libraries. Generated GoDoc remains the source for signatures.

## Package dependency direction

```text
bacnet (root leaf types)
  ↑
  ├── bip          BACnet/IP endpoints (not root)
  ├── bvlc         BVLC framing
  ├── npdu         network PDU
  ├── apdu         application PDU
  ├── service      service payloads (Who-Is, RP, RPM, WP, COV, …)
  └── client       supervisory runtime (composes layers + UDP)
```

Rules (enforced by `internal/imports`):

- Root `bacnet` must not import `bip`, `bvlc`, `npdu`, `apdu`, `service`, or `client`.
- Sibling codecs (`bvlc`, `npdu`, `apdu`) may import root only; they must not import each other or `client`.
- `service` may import root and `apdu` helpers as needed; it must not import `bvlc`, `npdu`, `bip`, or `client`.
- `client` is the composition root for Horizon 1 networking.

See [docs/PACKAGE_DESIGN.md](docs/PACKAGE_DESIGN.md).

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

`bacnet.MAC` is immutable and comparable (max 7 octets retained for Horizon 1).
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

- One `Client` is one local BACnet/IP data-link endpoint.
- `New` starts the receive loop (and optional foreign-device renewer).
- `Close` is idempotent: aborts pending transactions with `ErrClosed`, closes
  COV subscriptions, stops FD registration, closes the transport, waits for
  goroutines.
- Prefer `WithTransport` in tests; otherwise UDP binds to port 47808 unless
  overridden with `WithPort` / `WithLocalAddr`.

Confirmed calls take `client.Target` (`Address` + immediate `bip.Endpoint` +
optional origin / MaxAPDU). Discovery populates `Devices()` observations.

## Discovery

- `SendWhoIs(ctx, dest, broadcast, opts)` — directed or broadcast Who-Is.
- `Discover(ctx, opts)` — local-broadcast Who-Is until `ctx` ends (or client closes).
  Returns only observations whose `LastSeen` falls in this discovery window;
  `Devices()` returns the full retained registry snapshot.
- `WithRegistryOptions` bounds observation retention (global cap, per-instance
  path cap, TTL; defaults 4096 / 8 / 30m).
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
  from the cache; if unknown and `direct` is invalid, returns `ErrUnsupported`.

Confirmed requests to `RemoteStation` / `RemoteBroadcast` encode DNET/DADR and
send the BVLC frame to `Target.Endpoint` (the next hop).

## Foreign device / Forwarded-NPDU

- `WithForeignDevice(ForeignDeviceConfig{BBMD, TTL})` enables registration with
  one BBMD and starts a renewer loop.
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

Horizon 1 does **not** implement a BBMD server or multi-BBMD failover.

## Transaction retransmission vs application retry

Exact-APDU **retransmission** is owned by the client transaction manager and is
distinct from any application-level **retry** of a logical operation.

| Service | Default retransmit | Notes |
|---------|--------------------|-------|
| ReadProperty / RPM / ReadRange / GetEventInformation | Enabled | Safe to resend identical APDU |
| WriteProperty | Disabled | After send, timeout/cancel → `*OutcomeUnknownError` |
| WritePropertyMultiple | Disabled | After send (any segment), timeout/cancel → `*OutcomeUnknownError`; Error PDU → `*service.WritePropertyMultipleError` (first-failed only) |
| SubscribeCOV / SubscribeCOVProperty | Disabled | After send, timeout/cancel → `*OutcomeUnknownError` (incl. cancel/renew) |
| AcknowledgeAlarm | Disabled | After send → `*OutcomeUnknownError` |
| DeviceCommunicationControl / ReinitializeDevice | Disabled | Opt-in only; after send → `*OutcomeUnknownError` |

`WithTransactionOptions` sets APDU timeout, retransmit count and segment timeout.
Application code that retries these operations after `OutcomeUnknownError` must
treat execution as possibly already applied (orphaned remote subscription or
double write).

## RPM result model

`ReadPropertyMultiple` returns `[]service.ReadAccessResult`.

- Top-level `error` means the whole transaction failed (transport, Abort,
  Reject, malformed ACK, timeout).
- Per-property BACnet errors live in `PropertyResult.Err`; other properties in
  the same ACK may still succeed.
- Always inspect property results even when `err == nil`.

## ReadRange

`ReadRange` returns `service.ReadRangeACK` with `ResultFlags`, `ItemCount`,
`ItemData`, optional `FirstSequence`, and `LogRecords` when the property is
`Log_Buffer` and the itemData stream is well-formed `BACnetLogRecord`
SEQUENCEs. Prefer `LogRecords` for typed Trend Log access; `ItemData` remains
the flat tag stream for generic ranges. Inspect `FirstItem` / `LastItem` /
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

`WithDiagnosticFunc(func(Diagnostic))` installs an optional **synchronous**
callback on the receive and timeout paths. Callbacks must return promptly and
must not panic. Nil/omitted is silent. There is no default logger.

## COV subscription

`SubscribeCOV` / `SubscribeCOVProperty` return a `Subscription`:

- `Events() <-chan SubscriptionEvent` — notifications, state changes, errors
- `Close()` — best-effort unsubscribe; stops renewals

`SubscriptionEvent` fields:

| Field | Meaning |
|-------|---------|
| `Sequence` | Monotonic per subscription for successfully queued events |
| `Gap` | Sticky loss indicator: true on the next delivered event after one or more drops |
| `Notification` | Parsed COV notification when present |
| `State` | Lifecycle state carried with the event |
| `Err` | Optional error (drops, renew failure, close cause) |

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
- ReadRangeACK with property `Log_Buffer` requires a successful typed
  `LogRecord` split; known NotificationParameters CHOICEs reject malformed
  contents (unknown/unimplemented CHOICEs remain opaque)
