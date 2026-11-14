# Error reference

`go-bacnet` uses normal Go errors: sentinels for stable local categories, typed
values for remote PDUs and structured detail. Prefer `errors.Is` / `errors.As`.
Do not parse error strings.

## Local / runtime sentinels

Defined in package `bacnet`:

| Sentinel | Meaning |
|----------|---------|
| `ErrClosed` | Client closed; pending work aborted |
| `ErrTimeout` | Confirmed-request APDU timeout (no definitive remote PDU) |
| `ErrMalformed` | Encoding structurally invalid |
| `ErrUnsupported` | Valid but not supported by this implementation |
| `ErrProtocolViolation` | Unexpected PDU type or illegal peer behaviour |
| `ErrAPDUTooLarge` | Request exceeds remote max APDU without permitted segmentation |
| `ErrTrailingData` | Decode left unexpected trailing octets |
| `ErrLimitExceeded` | Decode / reassembly hit configured `DecodeLimits` |
| `ErrInvalidConfig` | `client.New`, `server.New` or `bbmd.New` was given an option value it cannot run with |
| `ErrBusy` | A BVLC management exchange is already in flight on this client |
| `ErrDeviceManagementDisabled` | DeviceCommunicationControl / ReinitializeDevice without `WithDeviceManagementEnabled` |

Package `client` adds `ErrNoRoute` (no known router to a remote network),
`ErrNetworkManagementDisabled` and `ErrMissingServicePolicy`; all three wrap
`bacnet.ErrUnsupported`.

`ErrTransactionCapacity`, `ErrResponseSourceMismatch` and `ErrDeliveryDropped`
are still exported but deprecated: no code path returns them. A request that
finds every invoke ID in use waits for one until its context ends; a response
from an unexpected source is dropped and reported as a diagnostic, and the
request runs into its timeout; dropped notifications are signalled by the
`Gap` field of the next event.

`ErrMalformed` also marks invalid *arguments* to a call (an empty RPM
specification list, a priority outside 1..16, a `Target` without an endpoint):
nothing is sent in that case.

## Configuration errors

`client.New` validates its options and fails with an error wrapping
`ErrInvalidConfig` that names the option, instead of running with a silently
changed setup:

```go
c, err := client.New(client.WithSegmentWindow(0))
// err: bacnet: invalid configuration: segment window 0 outside 1..127
errors.Is(err, bacnet.ErrInvalidConfig) // true
```

Several rejected options are reported together (`errors.Join`). An address
that cannot be bound (port 47808 already in use, unknown interface) is
returned as the operating-system error, not as `ErrInvalidConfig`.

## Typed local errors

### `*APDUTooLargeError`

Wraps `ErrAPDUTooLarge`. Fields: `EncodedSize`, `RemoteMax`,
`SegmentationSupported`.

```go
var tooLarge *bacnet.APDUTooLargeError
if errors.As(err, &tooLarge) {
    // inspect sizes / segmentation
}
if errors.Is(err, bacnet.ErrAPDUTooLarge) {
    // category check
}
```

### `*OutcomeUnknownError`

Returned when a request **may have executed** remotely but no definitive
response was observed. After transmission has been attempted, timeout, context
cancellation, `Close`, or an ambiguous transport failure wraps as
`*OutcomeUnknownError` for:

every service that changes the device, i.e. every entry of the
confirmed-service policy registry with `OutcomeUnknownAfterSend`
(`client.ConfirmedServicePolicyFor`): WriteProperty, WritePropertyMultiple,
SubscribeCOV / SubscribeCOVProperty / SubscribeCOVPropertyMultiple (including
cancellation and renewal), AcknowledgeAlarm, AtomicWriteFile, CreateObject,
DeleteObject, Add/RemoveListElement, ConfirmedPrivateTransfer,
ConfirmedTextMessage, LifeSafetyOperation, VT services, AuthRequest,
DeviceCommunicationControl and ReinitializeDevice. Reads return the plain
cause (`ErrTimeout`, `ctx.Err()`, `ErrClosed`).

```go
var unknown *bacnet.OutcomeUnknownError
if errors.As(err, &unknown) {
    // Operation name in unknown.Operation; Cause is the underlying wait error
}
```

`Unwrap` returns `Cause`. Application retries after this error are at-least-once
and may double-apply a write or leave an orphaned remote subscription.

### `*client.BVLCOperationError`

Returned by the BVLC management calls (`ReadBroadcastDistributionTable`,
`WriteBroadcastDistributionTable`, `ReadForeignDeviceTable`,
`DeleteForeignDeviceTableEntry`) when the BBMD answers with a non-zero
BVLC-Result. Fields: `Operation` (`Read-BDT`, `Write-BDT`, `Read-FDT`,
`Delete-FDT`) and `Code` (the BVLC-Result code). Unwraps to
`ErrProtocolViolation`.

```go
var bvlcErr *client.BVLCOperationError
if errors.As(err, &bvlcErr) {
    // bvlcErr.Operation, bvlcErr.Code
}
```

### `*client.BVLCResultError`

A non-zero BVLC-Result to a Register-Foreign-Device request (`Code` holds the
result code; unwraps to `ErrProtocolViolation`). Registration runs in the
background (`client.WithForeignDevice`), so this error is not returned from a
call: it is reported as a diagnostic (`client.WithLogger`, `client.WithDiagnosticFunc`) and
`ForeignDeviceRegistered` reports false until a later attempt succeeds.

## Remote typed errors

These represent peer PDUs, not local validation failures:

| Type | PDU |
|------|-----|
| `*ErrorResponse` | Error (invoke ID, service, class, code) |
| `*service.WritePropertyMultipleError` | WritePropertyMultiple-Error (class/code + first-failed object/property) |
| `*RejectError` | Reject (invoke ID, reason) |
| `*AbortError` | Abort (invoke ID, server flag, reason) |

A remote error is a **definite** answer: the device received the request and
refused it. It is never wrapped in `OutcomeUnknownError`.

```go
var remote *bacnet.ErrorResponse
if errors.As(err, &remote) {
    switch {
    case remote.Class == bacnet.ErrorClassObject && remote.Code == bacnet.ErrorCodeUnknownObject:
        // no such object on the device
    case remote.Code == bacnet.ErrorCodeWriteAccessDenied:
        // read-only property
    }
}
```

Classes, codes and reasons are the standard enumerations. `bacnet` exports
constants for the common ones (`ErrorClass…`, `ErrorCode…`, `RejectReason…`,
`AbortReason…`) and `ErrorClassName`, `ErrorCodeName`, `RejectReasonName`,
`AbortReasonName` for all of them; the `Error()` texts use the names, e.g.
`bacnet: error invoke=7 service=12 class=object(1) code=unknown-object(31)`.

The services with a structured Error (CreateObject, AddListElement,
RemoveListElement, ConfirmedPrivateTransfer, VT-Close,
SubscribeCOVPropertyMultiple) are reported as `*ErrorResponse` with the class
and code of the embedded error.

A WritePropertyMultiple failure is a `*service.WritePropertyMultipleError`
naming the first write that failed; the writes encoded before it may already
have been applied. It unwraps to an `*ErrorResponse` with the same class and
code (invoke ID zero), so code that handles remote errors generically with
`errors.As(err, &remote)` sees it too.

An Abort with reason `segmentation-not-supported`, `buffer-overflow` or
`apdu-too-long` means the *answer* did not fit: ask for less. `ReadObjectList`
and `ReadPropertyMultipleBatched` do that automatically.

Wire status must stay distinct from `ErrMalformed`, `ErrTimeout`, and
`ErrClosed`.

## Context error preservation

Cancellation and deadlines are preserved for `errors.Is`:

- Queue wait / invoke-ID allocation returns `ctx.Err()` directly when cancelled.
- Confirmed-request wait returns `ctx.Err()` on cancel; for side-effecting
  services after send it is wrapped as `*OutcomeUnknownError{Cause: ctx.Err()}`.
- `Discover` with `DiscoveryOptions.Window` returns `nil` when the window has
  elapsed. Without a window it collects until the context ends and returns
  `ctx.Err()`; the observations gathered are returned in both cases.

```go
if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
    // still works when wrapped by OutcomeUnknownError
}
```

## RPM partial results

`ReadPropertyMultiple` top-level `error` is whole-transaction failure only.
Property-level BACnet errors appear in `service.PropertyResult.Err` on an
otherwise successful ACK. See [API.md](API.md#rpm-result-model).

## Decode limits

Peer-advertised sizes never override application `DecodeLimits`. Exceeding
limits yields `ErrLimitExceeded` (or a wrap thereof), not unbounded allocation.

## Out-of-range values

A field whose Go type is narrower than its wire encoding is range-checked
when it is decoded; a value that does not fit yields `ErrMalformed`, never its
low bits. This covers 8-bit fields (VT session identifiers, VT data flag,
message and write priorities, acknowledgment filter), 16-bit fields (vendor
identifiers, network numbers, object types, audit counts) and 32-bit fields
(file start positions and counts, ReadRange indexes and counts).
An enumerated request parameter outside the values the standard defines
(DeviceCommunicationControl enable-disable, ReinitializeDevice
reinitialized-state) yields `service.ErrUndefinedEnumeration`, which wraps
`ErrMalformed`; the server answers it with Reject undefined-enumeration.
`device.ReadSnapshot` records an out-of-range Vendor_Identifier (above
Unsigned16), Protocol_Revision or Max_APDU_Length_Accepted (above 32 bits) in
`PartialErrors` and leaves the field zero.

The file helpers (`ReadFileStream`, `ReadFileRecords`, `WriteFileStream`)
return `ErrProtocolViolation` when the device reports a position or count
whose next position would leave the signed 32-bit range, and
`WriteFileStream` returns `ErrLimitExceeded` before sending anything when the
data would end past octet 2^31-1.

`ReadPropertyMultipleBatched` (and so `ReadObjectList`'s element-by-element
fallback) returns `ErrProtocolViolation` when a result's properties are not
the requested references, in order: extra, missing, reordered or other
properties. A reference to ALL, REQUIRED or OPTIONAL may expand to several
results.

## Server errors

The server never returns Go errors to the network. Each request is answered
with a BACnet Error, Reject or Abort PDU (clause 18), or not at all.

### Configuration

`server.New` and `bbmd.New` validate their options like `client.New`: an
error wrapping `bacnet.ErrInvalidConfig` (`server.ErrInvalidConfig` and
`bbmd.ErrInvalidConfig` are the same value) names every rejected option. A
nil Device is a configuration error. A UDP socket that cannot be opened is
returned as the operating-system error.

### Errors from objects and hooks

Objects, `Hooks` and `object.Factory` functions return BACnet errors made
with `object.Err(class, code)`; the server sends them as the Error PDU of the
request. `object.AsError` maps anything else to device/operational-problem,
so an application failure reaches the network only as a well-formed Error.

Package `server/object` exports the errors its objects return:

| Variable | Class / code |
|---|---|
| `ErrUnknownProperty` | property / unknown-property |
| `ErrWriteAccessDenied` | property / write-access-denied |
| `ErrReadAccessDenied` | property / read-access-denied |
| `ErrInvalidDataType` | property / invalid-data-type |
| `ErrValueOutOfRange` | property / value-out-of-range |
| `ErrNotAnArray` | property / property-is-not-an-array |
| `ErrInvalidArrayIndex` | property / invalid-array-index |
| `ErrDuplicateName` | property / duplicate-name |
| `ErrUnknownObject` | object / unknown-object |
| `ErrObjectExists` | object / object-identifier-already-exists |
| `ErrNotDeletable` | object / object-deletion-not-permitted |
| `ErrNotCreatable` | object / dynamic-creation-not-supported |
| `ErrUnsupportedType` | object / unsupported-object-type |
| `ErrNoSpaceForObject` | resources / no-space-for-object |
| `ErrPropertyIsNotAList` | services / property-is-not-a-list |
| `ErrListElementNotFound` | services / list-element-not-found |
| `ErrNoSpaceForListElement` | resources / no-space-to-add-list-element |
| `ErrNoSpaceToWriteProperty` | resources / no-space-to-write-property |
| `ErrInconsistentObjectType` | services / inconsistent-object-type |
| `ErrInvalidFileAccessMethod` | services / invalid-file-access-method |
| `ErrInvalidFileStartPosition` | services / invalid-file-start-position |
| `ErrFileAccessDenied` | object / file-access-denied |
| `ErrFileFull` | object / file-full |
| `ErrInvalidTimeStamp` | services / invalid-time-stamp |
| `ErrInvalidEventState` | services / invalid-event-state |
| `ErrNoAlarmConfigured` | object / no-alarm-configured |

`*object.ListError` carries the 1-based element at which an
AddListElement or RemoveListElement failed; the server sends it as a
ChangeList-Error. A failed CreateObject initial value is sent as a
CreateObject-Error naming the element, and a failed
SubscribeCOVPropertyMultiple as an error naming the first failed
subscription. A subscription beyond `WithCOVLimits` or
`WithCOVReferenceLimit` gets resources / no-space-to-add-list-element; one
SubscribeCOVPropertyMultiple with more references than fit one notification
gets the error naming the first reference over that cap.

### Rejects and Aborts

| Condition | Answer |
|---|---|
| Malformed request | Reject invalid-tag (too-many-arguments for trailing data, buffer-overflow for a decode limit) |
| Enumeration outside the defined values | Reject undefined-enumeration |
| Service not served (or hook not installed) | Reject unrecognized-service |
| Worker queue full | Abort out-of-resources |
| First segment of a request beyond `WithSegmentLimits` (reassemblies in total or per origin, buffered octets), or a segmented reply beyond its reply limits | Abort out-of-resources |
| Reply needs segmentation the requester does not accept, or segmentation disabled | Abort segmentation-not-supported |
| Reply or segmented request beyond the segment limits | Abort buffer-overflow |
| Segment of an unknown transaction | Abort invalid-apdu-in-this-state |
| No SegmentACK after the retries, or a segmented reply past its overall deadline | Abort tsm-timeout |

Remaining failures that are not answers (a malformed frame, a dropped
request, a failed send or notification) are diagnostics; see
[OBSERVABILITY.md](OBSERVABILITY.md#server).

