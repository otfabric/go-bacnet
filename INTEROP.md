# Interoperability

## Ownership split

| Repository | Owns |
|------------|------|
| [bacnet-interop](https://github.com/otfabric/bacnet-interop) | Adapter containers (bacnet-stack, BACpypes3, BACnet4J), `bip-router` topology aid, fixtures, readiness contract, golden packets, [`COVERAGE.md`](https://github.com/otfabric/bacnet-interop/blob/main/COVERAGE.md) |
| `go-bacnet` (`interop/` with `-tags=interop`) | Assertions against those adapters using this library |

Same pattern as `mms-interop` / `go-mms`: interop infra stays out of the library
module; the library owns behavioural tests.

Horizon 1 peers: **bacnet-stack**, **BACpypes3**, and **BACnet4J**. Topology aid:
**bip-router** (not a peer oracle). Additional oracles may be added later
without changing ownership — planned Go peers include **worldiety/bacnet**
(primary modern competitor) and optionally NubeDev for behavioral comparison
(see [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md) and [PLAN.md](PLAN.md)).

## Fixtures (Gate 3, no Docker)

Independently generated wire goldens live in `bacnet-interop/fixtures/codec/` and
are indexed by `fixtures/manifest.json`. Library unit tests load them through
`internal/fixtures` (sibling path `../bacnet-interop` or `BACNET_INTEROP_ROOT`).

```bash
# From go-bacnet with bacnet-interop checked out as a sibling:
GOWORK=off go test ./internal/fixtures/ -count=1
```

Live device semantics for peer containers:
`bacnet-interop/fixtures/device/device-baseline-v1.json` (device instance `1234`).

## Running peer adapters

Build images from a sibling `bacnet-interop` checkout, then run assertions:

```bash
# bacnet-interop
make build   # bacnet-stack + bacpypes3 + bacnet4j + bip-router

# go-bacnet
make interop
# equivalent:
GOWORK=off go test -tags=interop -count=1 ./interop/...
```

| Variable | Default | Purpose |
|----------|---------|---------|
| `BACNET_STACK_IMAGE` | `bacnet-interop-bacnet-stack:local` | bacnet-stack peer image |
| `BACPYPES3_IMAGE` | `bacnet-interop-bacpypes3:local` | BACpypes3 peer image |
| `BACNET4J_IMAGE` | `bacnet-interop-bacnet4j:local` | BACnet4J peer image |
| `BIP_ROUTER_IMAGE` | `bacnet-interop-bip-router:local` | Dual-homed BIP↔BIP topology router |
| `BACNET_INTEROP_GO_IMAGE` | `golang:<go-minor>` | Image used for in-network re-exec on Docker Desktop / routed tests |
| `BACNET_INTEROP_SKIP` | unset | Skip all peer tests when set (forbidden when required) |
| `BACNET_INTEROP_REQUIRED` | unset | Fail instead of skip when Docker/images are missing |
| `BACNET_INTEROP_ROOT` | sibling `../bacnet-interop` | Fixture/device JSON checkout |

Local runs may skip when Docker or images are unavailable.

CI (`.github/workflows/interop.yml`) runs two lanes:

1. **Pinned** — loads `interop/bacnet-interop-pin.json` (ref + GHCR digests),
   checks out that bacnet-interop tag for fixtures, pulls published images.
2. **main compat** — checks out `bacnet-interop@main`, builds adapters from
   source, runs the same required assertions (detects upcoming breakage).

Both lanes set `BACNET_INTEROP_ROOT` to the checked-out fixtures tree (also
honored by Docker re-exec for routed tests), require
`BACNET_INTEROP_REQUIRED=1`, and upload repository SHA evidence artifacts.
Use `make interop-required` locally with `:local` images or the same digest pins.

Current scenarios (fixture `device-baseline-v1`, device instance `1234`):

| Scenario | bacnet-stack | BACpypes3 | BACnet4J | Interop test |
|----------|:---:|:---:|:---:|---|
| Directed Who-Is → I-Am (MaxAPDU/VendorID) | ✓ | ✓ | ✓ | `TestBacnetStackWhoIsIAm`, `TestBACpypes3WhoIsIAm`, `TestBACnet4JWhoIsIAm` |
| ReadProperty device object-name | ✓ | ✓ | ✓ | `TestBacnetStackReadDeviceObjectName`, `TestBACpypes3ReadDeviceObjectName`, `TestBACnet4JReadDeviceObjectName` |
| ReadProperty AV present-value | ✓ | ✓ | ✓ | `TestBacnetStackReadAnalogValue`, `TestBACpypes3ReadAnalogValue`, `TestBACnet4JReadAnalogValue` |
| ReadProperty unknown-property → Error | ✓ | ✓ | ✓ | `TestBacnetStackReadPropertyUnknownPropertyError`, `TestBACpypes3ReadPropertyUnknownPropertyError`, `TestBACnet4JReadPropertyUnknownPropertyError` |
| Unrecognized service → Reject | ✓ | — | ✓ | `TestBacnetStackRejectUnrecognizedService`, `TestBACnet4JRejectUnrecognizedService` |
| Abort (segmentation path) | ✓ | ✓ | — | `TestBacnetStackAbortSegmentationNotSupported`, `TestBACpypes3AbortWhenSegmentedResponseNotAccepted` |
| ReadPropertyMultiple success | ✓ | ✓ | ✓ | `TestBacnetStackReadPropertyMultiple`, `TestBACpypes3ReadPropertyMultiple`, `TestBACnet4JReadPropertyMultiple` |
| RPM partial property Error | ✓ | ✓ | ✓ | `TestBacnetStackReadPropertyMultiplePartialError`, `TestBACpypes3ReadPropertyMultiplePartialError`, `TestBACnet4JReadPropertyMultiplePartialError` |
| WriteProperty + readback + restore | ✓ | ✓ | ✓ | `TestBacnetStackWritePropertyReadbackReset`, `TestBACpypes3WritePropertyReadbackReset`, `TestBACnet4JWritePropertyReadbackReset` |
| Segmented RPM ComplexACK reassembly | — | ✓ | ✓ | `TestBACpypes3SegmentedReadPropertyMultiple`, `TestBACnet4JSegmentedReadPropertyMultiple` |
| COV subscribe / notify / cancel | ✓ | ✓ | ✓ | `TestBacnetStackCOVSubscribeNotifyCancel`, `TestBACpypes3COVSubscribeNotifyCancel`, `TestBACnet4JCOVSubscribeNotifyCancel` |
| COV renew | — | ✓ | — | `TestBACpypes3COVRenew` |
| Routed Who-Is-Router → ResolveTarget → RP | — | ✓ | — | `TestBACpypes3RoutedWhoIsRouterReadProperty` (cache learn best-effort under Docker Desktop; hard assert is routed RP; unit coverage in `client/routing_test.go`) |
| Routed RP (explicit next-hop DNET/DADR) | ✓ | ✓ | ✓ | `TestBACnetStackRoutedReadProperty`, `TestBACnet4JRoutedReadProperty`; BACpypes3 via Who-Is-Router test path |
| Foreign-device register + DBTN Who-Is → RP | — | ✓ | ✓ | `TestBACpypes3ForeignDeviceWhoIsReadProperty`, `TestBACnet4JForeignDeviceWhoIsReadProperty` |

See [PLAN.md](PLAN.md) for the closed production-candidate batches and the open
real-device gate toward production-usable.

Topology notes:

- Routed scenarios use `bip-router` on two docker networks (client net `1`,
  device net `2`) and **always** re-exec assertions inside the client network.
  Phrase evidence as client addressing validated through this topology aid with
  independent endpoint stacks — not as independent BACnet-router interoperability.
- Who-Is-Router is directed at the router hop (`WhoIsRouterToNetworkAt`); remote
  I-Am observation is best-effort.
- BBMD/FD uses BACpypes3 or BACnet4J with `BACNET_BBMD=1` on a single network
  (Register-Foreign-Device + Distribute-Broadcast-To-Network).
- Abort (segmentation-not-supported) is asserted against bacnet-stack and
  BACpypes3 only; BACnet4J segments ComplexACK instead (see segmented RPM row).
- COV renew remains BACpypes3-only for now.

On Linux, single-peer tests talk to the peer container IP on a dedicated docker
network. On Docker Desktop (macOS/Windows), the same test is re-executed inside
that network via a `golang` image (`BACNET_INTEROP_GO_IMAGE`). See
`bacnet-interop/COVERAGE.md` for registered limitations.

## Production-candidate vs production-usable

| Label | Meaning |
|-------|---------|
| **alpha** | Pre-hardening / incomplete oracle evidence |
| **production-candidate** | Current — P0 wire/runtime closed with oracle/lab interop evidence; **no** claim of multi-vendor hardware readiness |
| **production-usable** | Real-device gate met — see [docs/REAL_DEVICE_GATE.md](docs/REAL_DEVICE_GATE.md) |

Do not claim vendor hardware interoperability from container oracles alone.
