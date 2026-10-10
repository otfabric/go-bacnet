# Observability

`go-bacnet` is silent by default. It never writes to `log.Default`,
`slog.Default`, stdout or stderr. Two opt-in hooks expose what the client sees
on the network beyond the results of your own calls.

| Hook | Use it for |
|---|---|
| `client.WithLogger(*slog.Logger)` | Operational logging |
| `client.WithDiagnosticFunc(func(client.Diagnostic))` | Counters, tests, custom routing of diagnostics |

Both may be set; each diagnostic then goes to both. There is no metrics
interface: count diagnostics by `Kind` in a `WithDiagnosticFunc` callback if
you need counters.

## What is reported

A diagnostic is something the client noticed that no caller is waiting for: a
frame it dropped, a route that changed, a registration that failed, a
notification it could not queue. Errors of your own requests are *returned*,
not logged.

```go
type Diagnostic struct {
    Kind    string         // stable, bounded set (below)
    Message string
    Fields  map[string]any // small, per-kind; may be nil
}
```

| Kind | Log level | Reported when |
|---|---|---|
| `transport` | Warn | The socket failed to receive; the client retries with backoff (5 ms doubling to 500 ms) |
| `foreign_device` | Warn | Registration with the BBMD failed or was refused; a BVLC-Result or Forwarded-NPDU came from a peer that is not the BBMD |
| `queue_drop` | Warn | A COV notification was dropped because the subscription's event buffer was full |
| `registry_eviction` | Warn | A device or object observation was evicted (TTL, per-instance path limit, global limit) |
| `duplicate_device_instance` | Warn | The same device instance announced itself from a second address |
| `router` | Info | I-Am-Router-To-Network, Router-Busy/Available, Reject-Message-To-Network and other network-layer messages |
| `malformed` | Debug | A datagram failed BVLC, NPDU, APDU or service decoding and was dropped |
| `unknown_invoke_id` | Debug | A response matched no pending transaction (late answer, or not for this client) |
| `wrong_source` | Debug | A response carried a pending invoke ID but came from another address than the request went to |
| `wrong_service` | Debug | A response carried a pending invoke ID with a different service choice |
| `unexpected_apdu` | Debug | A PDU the client does not handle; a confirmed request it rejected or aborted; a failed SegmentACK/Abort/Reject send; an event handler panic; event-dispatcher overflow |
| `bvlc` | Debug | An ignored BVLC function, an unsolicited BVLC management response, a non-IPv4 endpoint |
| `cov` | Debug | The SimpleACK for a confirmed COV notification could not be sent |
| `event` | Debug | The SimpleACK for a confirmed event or audit notification could not be sent |
| `transaction` | Debug | A retransmission could not be sent |

`Kind` values are stable within a major version and safe to use as a metric
label. `Message` and `Fields` are for humans and may change; do not parse
them. Do not use `Fields` values as metric labels: they include addresses and
instance numbers.

## Logging

```go
logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
c, err := client.New(client.WithLogger(logger.With("component", "bacnet")))
```

Each entry has the diagnostic's message, a `kind` attribute and the
diagnostic's fields as attributes. Levels follow the table: at `Info` you see
lost functionality and route changes; `Debug` adds every dropped frame, which
on a busy or hostile network is one entry per offending datagram. A nil logger
disables logging.

Entries below the handler's level are not built. The diagnostic itself is
created before the level is known, so a client without any hook still pays a
small allocation per *diagnosed* frame, never per normally processed frame.

## What is never logged

Diagnostics carry addresses, device instances, invoke IDs, service choices,
network numbers and error texts. They never carry property values, written
values, file data, the passwords of DeviceCommunicationControl and
ReinitializeDevice, or raw frames. There is no raw-frame hook; capture traffic
with a packet capture on UDP port 47808, which Wireshark dissects.

Error texts in a diagnostic can quote a decode failure such as an unexpected
tag number. They do not include the offending octets.

## Callback rules

`WithDiagnosticFunc` callbacks and `slog` handlers run **synchronously on the
goroutine that noticed the condition**, in most cases the receive loop:

- return promptly: a slow callback delays every response the client is waiting for;
- do not call methods of the same `Client` from the callback;
- do not panic;
- expect concurrent calls, since timers and request goroutines also report.

The same rules hold for `WithEventNotificationHandler`. For event handling
that takes time use `WithEventDispatcher` (bounded worker pool) or
`OpenEventStream` (bounded channel); see [API.md](API.md#event-delivery).

## Signals that are results, not diagnostics

| Signal | Where |
|---|---|
| Dropped COV notifications | `SubscriptionEvent.Gap` on the next delivered event; state `Degraded` |
| Dropped EventNotifications in a stream | `EventStreamEvent.Gap` |
| Failed COV renewal | `SubscriptionEvent{State: SubscriptionDegraded, Err: …}` |
| Foreign-device registration state | `Client.ForeignDeviceRegistered()` |
| Known devices, objects and routes | `Client.Devices()`, `Objects()`, `Routes(network)` |
