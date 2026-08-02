# Package design

Package layout and dependency rules for `go-bacnet`.

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
├── cmd/bacnetctl/       CLI
├── internal/            clock, diag, virtual transport, import tests
├── interop/             //go:build interop peer assertions
└── docs/                baseline, capabilities, design
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
   cmd / applications
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

## Composition

`client.New` owns:

1. Transport (UDP or injected `Transport`)
2. Receive loop: BVLC → NPDU → APDU → dispatch
3. Transaction manager (invoke IDs, timeout, optional exact-APDU retransmit)
4. Device observation registry
5. Router cache, optional foreign-device state, COV subscription manager
6. Segmented ComplexACK reassembly

Callers use `client` for networked operations and may use codecs directly for
offline decode/encode.

## Byte ownership

| Layer | Rule |
|-------|------|
| Parse input | Caller owns the datagram buffer |
| Parsed payload slices | May alias input until the next reuse of that buffer |
| Long-lived `ApplicationValue` | Must `Clone()` |
| Encode `Append*` | Appends to caller-owned `dst`; does not retain `dst` |
| Client outbound APDU | Copied into the transaction table |

See also [API.md](../API.md#byte-ownership).

## Why `bip.Endpoint` is not root

Root addresses stay data-link neutral (`Address` + `MAC`). Concrete BACnet/IP
UDP endpoints live in `bip` so future MS/TP, IPv6, and BACnet/SC modules can
attach without forcing IP types into every leaf API.
