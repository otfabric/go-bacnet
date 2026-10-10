# Architecture

Package layout, dependency rules and the runtime structure of the client in
`go-bacnet`. The behavioural contract is in [API.md](API.md).

## Layout

```text
go-bacnet/
├── *.go                 root package bacnet — leaf types only
├── bip/                 BACnet/IP endpoints (bip.Endpoint)
├── bvlc/                BVLC framing codec
├── npdu/                NPDU codec
├── apdu/                APDU codec
├── service/             service request/ACK codecs
├── client/              supervisory runtime + UDP transport
├── device/              optional device snapshot on top of a PropertyReader
├── internal/            clock, diag, virtual and fault-injecting transports,
│                        fixture corpus loader, import-boundary test
├── interop/             //go:build interop peer assertions
├── examples/            runnable samples
└── docs/                baseline, capabilities, field validation
```

There is no separate `encoding/`, `object/`, `transaction/`, or `network/` tree
in this module. Transaction and segmentation state live inside `client`.

## Dependency rules

```text
root (leaf types; transport-neutral)
        ↓
 bvlc │ npdu │ apdu     ← sibling codecs; may import root;
        ↓                  must NOT import one another
      service           (root + apdu; payload codecs only)
        ↓
      client / runtime  (composes BVLC→NPDU→APDU→service; uses bip)
        ↓
   applications (for example the bacnetctl CLI, a separate repository)
```

Receive-path composition (runtime, not import edges):

```text
UDP datagram → bvlc.Parse → npdu.Parse → apdu.Parse → service.Decode...
```

| Package | May import | Must not import |
|---------|------------|-----------------|
| `bacnet` | stdlib only (for leaf types) | `bip`, `bvlc`, `npdu`, `apdu`, `service`, `client` |
| `bip` | root, stdlib | wire codecs, `client` |
| `bvlc` | root | `npdu`, `apdu`, `service`, `client` |
| `npdu` | root | `bvlc`, `apdu`, `service`, `client` |
| `apdu` | root | `bvlc`, `npdu`, `service`, `client` |
| `service` | root, `apdu` as needed | `bvlc`, `npdu`, `bip`, `client` |
| `client` | root + siblings + `internal/*` | — (composition root) |

Enforced by `go test ./internal/imports/...`.

## Sibling codecs

`bvlc`, `npdu`, and `apdu` are **siblings**, not a deep import stack of each
other. Composition happens in `client` (and optionally in tools that call
parsers in order). Each codec:

- Validates lengths against `bacnet.DecodeLimits`
- May return payloads that **alias** the input buffer
- Stays free of sockets and goroutines

## Client runtime

One `client.Client` is one BACnet/IP data-link endpoint: one UDP socket (or
one injected `Transport`), one invoke-ID space, one set of caches.

```text
                         ┌──────────────────────── Client ────────────────────────┐
 caller goroutines       │                                                        │
   ReadProperty ─────────┼─► encode ─► txManager (invoke ID, timer) ─► Transport ─┼─► UDP
   SubscribeCOV …        │                    ▲                                   │
                         │                    │ result channel                    │
                         │  receive loop (one goroutine)                          │
   UDP ──► Transport ────┼─► bvlc.Parse ─► npdu.Parse ─► apdu.Parse ─► dispatch   │
                         │      │               │                        │        │
                         │   BVLC results   network messages     ┌───────┴──────┐ │
                         │   (FD, BDT/FDT)  (router cache)       │ responses    │ │
                         │                                       │ I-Am/I-Have  │ │
                         │                                       │ COV / events │ │
                         │                                       └──────────────┘ │
                         └────────────────────────────────────────────────────────┘
```

### Goroutines

| Goroutine | Started by | Ends |
|---|---|---|
| Receive loop | `New` | `Close` |
| Foreign-device registration and renewal | `New` with `WithForeignDevice` | `Close` |
| Segment reassembly timer, one per segmented response in flight | first segment | reassembly done, timeout, or `Close` |
| COV renewal, one per subscription | `SubscribeCOV…` | `Subscription.Close` or `Close` |
| Event dispatcher workers | `New` with `WithEventDispatcher` | `Close` |

`Close` waits for all of them. A request runs on the goroutine of its caller;
the client starts no goroutine per request.

### A confirmed request

1. The caller's goroutine encodes the service payload, reserves an invoke ID
   (waiting for a free one if all 256 are in use, until its context ends) and
   registers a pending transaction with the path the answer must come from.
2. It sends the APDU, or the segments of it when the request exceeds the
   peer's Max APDU and the peer is known to accept segmented requests, and
   waits on the transaction's result channel, its APDU timer, its context and
   the client's close signal.
3. The receive loop matches an arriving SimpleACK, ComplexACK, Error, Reject
   or Abort by invoke ID, source path and service choice. Frames that do not
   match are dropped and reported as diagnostics; they never complete a
   transaction. A segmented ComplexACK is reassembled by the receive loop,
   which sends the SegmentACKs, before the transaction completes.
4. On an APDU timeout the request is retransmitted if the service is safe to
   repeat and retries remain. An invoke ID released after a request went out is
   quarantined for one APDU
   timeout so a late answer cannot be taken for the answer to a new request.

Retransmission, the outcome-unknown rule for writes and the operation-class
opt-ins are data in one table, the confirmed-service policy registry
(`client/service_policy.go`); the typed helpers do not decide them one by one.

### What runs on the receive loop

Decoding, transaction completion, the SimpleACK for a confirmed notification,
SegmentACKs, registry and route-cache updates, the synchronous
EventNotification handler and the diagnostic callback all run on the receive
loop. That keeps ordering simple and means a slow handler or callback stalls
reception: use `WithEventDispatcher` or `OpenEventStream` for work that takes
time, and do not call the Client from a handler.

The client answers confirmed requests only for the notification services it
consumes (COV, COV-multiple, event, audit). Any other confirmed request gets a
Reject (unrecognized-service), a segmented one an Abort
(segmentation-not-supported), and a request in transit to another network is
ignored: the client is neither a server nor a router.

### State and bounds

| State | Bound |
|---|---|
| Pending transactions | 255 (invoke-ID space) |
| Device observations (I-Am) | `RegistryOptions`: 4096 entries, 8 paths per instance, 30 min TTL |
| Object observations (I-Have) | same options |
| Routes | 8 next hops per network, 256 in total, 10 min TTL |
| Reassembly | `DecodeLimits.MaxSegments`, `MaxReassembledAPDU` |
| COV and event queues | per-subscription / per-stream buffer; overflow drops and sets `Gap` |

Nothing a peer sends can grow client memory beyond these bounds.

## Byte ownership

| Layer | Rule |
|-------|------|
| Parse input | Caller owns the datagram buffer |
| Parsed payload slices | May alias input until the next reuse of that buffer |
| Long-lived `ApplicationValue` | Must `Clone()` |
| Encode `Append*` | Appends to caller-owned `dst`; does not retain `dst` |
| Client outbound APDU | Copied into the transaction table |

See also [API.md](API.md#byte-ownership).

## Why `bip.Endpoint` is not root

Root addresses stay data-link neutral (`Address` + `MAC`). Concrete BACnet/IP
UDP endpoints live in `bip` so future MS/TP, IPv6, and BACnet/SC modules can
attach without forcing IP types into every leaf API.
