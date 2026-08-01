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
| `ErrTransactionCapacity` | Invoke-ID / transaction table exhausted |
| `ErrMalformed` | Encoding structurally invalid |
| `ErrUnsupported` | Valid but not supported by this implementation |
| `ErrResponseSourceMismatch` | Response did not match expected source path |
| `ErrProtocolViolation` | Unexpected PDU type or illegal peer behaviour |
| `ErrAPDUTooLarge` | Request exceeds remote max APDU without permitted segmentation |
| `ErrTrailingData` | Decode left unexpected trailing octets |
| `ErrLimitExceeded` | Decode / reassembly hit configured `DecodeLimits` |

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
response was observed. After the APDU was sent, timeout or context cancellation
wraps as `*OutcomeUnknownError` for:

- `WriteProperty`
- `WritePropertyMultiple` (including after any request segment was sent)
- `SubscribeCOV`
- `SubscribeCOVProperty` (including cancellation and renewal)
- `AcknowledgeAlarm`
- `DeviceCommunicationControl`
- `ReinitializeDevice`

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

WritePropertyMultiple failures are **not** `*ErrorResponse`: BACnet reports only
the first failed write. Properties encoded before that attempt may already have
been applied.

Wire status must stay distinct from `ErrMalformed`, `ErrTimeout`, and
`ErrClosed`.

`ErrDeviceManagementDisabled` is returned when DeviceCommunicationControl or
ReinitializeDevice is called without `WithDeviceManagementEnabled`.

## Context error preservation

Cancellation and deadlines are preserved for `errors.Is`:

- Queue wait / invoke-ID allocation returns `ctx.Err()` directly when cancelled.
- Confirmed-request wait returns `ctx.Err()` on cancel; for side-effecting
  services after send it is wrapped as `*OutcomeUnknownError{Cause: ctx.Err()}`.
- `Discover` returns when the context ends; the error is typically
  `context.Canceled` or `context.DeadlineExceeded` while observations remain
  available via the return value / `Devices()`.

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
