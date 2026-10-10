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
| `ErrInvalidConfig` | `client.New` was given an option value it cannot run with |
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
