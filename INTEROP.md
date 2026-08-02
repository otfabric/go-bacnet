# Interoperability

## Ownership split

| Repository | Owns |
|------------|------|
| [bacnet-interop](https://github.com/otfabric/bacnet-interop) | Adapter containers (bacnet-stack, BACpypes3, BACnet4J, Worldiety), `bip-router` topology aid, fixtures, readiness contract, golden packets, [`COVERAGE.md`](https://github.com/otfabric/bacnet-interop/blob/main/COVERAGE.md) |
| `go-bacnet` (`interop/` with `-tags=interop`) | Assertions against those adapters using this library |

Same pattern as `mms-interop` / `go-mms`: interop infra stays out of the library
module; the library owns behavioural tests.

Peers: **bacnet-stack**, **BACpypes3**, **BACnet4J**, and **Worldiety**
(server peer; fixture payload shims). Topology aid: **bip-router**. Worldiety
client probes are deferred until `go-bacnet` has a server (see
[docs/COMPATIBILITY.md](docs/COMPATIBILITY.md) and [PLAN.md](PLAN.md)).

## Fixtures (Gate 3, no Docker)

Independently generated wire goldens live in `bacnet-interop/fixtures/codec/` and
are indexed by `fixtures/manifest.json`. Library unit tests load them through
`internal/fixtures` (sibling path `../bacnet-interop` or `BACNET_INTEROP_ROOT`).

```bash
# From go-bacnet with bacnet-interop checked out as a sibling:
GOWORK=off go test ./internal/fixtures/ -count=1
```

Live device semantics for peer containers:
`bacnet-interop/fixtures/device/device-baseline-v2.json` (device instance `1234`;
includes TrendLog for ReadRange). Fixture generations `v3`–`v8`, `topology-v2`,
and `bbmd-v2` are checked in under `bacnet-interop/fixtures/` for the
client-completeness roadmap; live peer alignment for those generations is still
partial (see bacnet-interop `BLOCKERS.md`). The harness also accepts `device-baseline-v1`
when v2 is absent.

## Running peer adapters

Build images from a sibling `bacnet-interop` checkout, then run assertions:

```bash
# bacnet-interop
make build   # stack + bacpypes3 + bacnet4j + worldiety + bip-router

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
| `WORLDIETY_IMAGE` | `bacnet-interop-worldiety:local` | Worldiety peer image |
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

Current scenarios default to fixture `device-baseline-v2` (device instance `1234`);
selected tests also exercise `device-baseline-v3`/`v4`/`v5`/`v6` where peers serve
them. Pinned release:
[`bacnet-interop` v0.6.0](https://github.com/otfabric/bacnet-interop/releases/tag/v0.6.0)
@ `f4ea3de` (BACpypes3 **0.0.106**; Worldiety + File/lifecycle/NC list adapters).
Prior green on `v0.2.2` @ `a277aea` with pin `v0.5.0`:
[30757758742](https://github.com/otfabric/go-bacnet/actions/runs/30757758742).
Pending `v0.2.3` must re-prove pinned + main-compat against `v0.6.0`.

| Scenario | bacnet-stack | BACpypes3 | BACnet4J | Worldiety | Interop test |
|----------|:---:|:---:|:---:|:---:|---|
| Directed Who-Is → I-Am (MaxAPDU/VendorID) | ✓ | ✓ | ✓ | ✓ | `TestBacnetStackWhoIsIAm`, `TestBACpypes3WhoIsIAm`, `TestBACnet4JWhoIsIAm`, `TestWorldietyWhoIsIAm` |
| Who-Has → I-Have | ✓ | ✓ | ✓ | ✓ | `…WhoHasIHave`, `TestWorldietyWhoHasIHave` |
| ReadProperty device object-name | ✓ | ✓ | ✓ | ✓ | `…ReadDeviceObjectName`, `TestWorldietyReadDeviceObjectName` |
| ReadProperty AV present-value | ✓ | ✓ | ✓ | ✓ | `…ReadAnalogValue`, `TestWorldietyReadAnalogValuePresentValue` |
| ReadProperty unknown-property → Error | ✓ | ✓ | ✓ | ✓ | `…UnknownPropertyError`, `TestWorldietyReadPropertyUnknownPropertyError` |
| Unrecognized service → Reject | ✓ | — | ✓ | — | `TestBacnetStackRejectUnrecognizedService`, `TestBACnet4JRejectUnrecognizedService` |
| Abort (segmentation path) | ✓ | ✓ | — | — | `TestBacnetStackAbortSegmentationNotSupported`, `TestBACpypes3AbortWhenSegmentedResponseNotAccepted` |
| ReadPropertyMultiple success | ✓ | ✓ | ✓ | ✓ | `…ReadPropertyMultiple`, `TestWorldietyReadPropertyMultiple` |
| RPM partial property Error | ✓ | ✓ | ✓ | — | `…ReadPropertyMultiplePartialError` |
| WriteProperty + readback + restore | ✓ | ✓ | ✓ | ✓ | `…WritePropertyReadbackReset`, `TestWorldietyWritePropertyReadbackReset` |
| WritePropertyMultiple + readback + restore | ✓ | ✓ | ✓ | ✓ | `…WritePropertyMultipleReadbackReset`, `TestWorldietyWritePropertyMultipleReadbackReset` |
| Segmented RPM ComplexACK reassembly | — | ✓ | ✓ | skip | Worldiety skip B6 (service-choice on segments); BACpypes3/4J ✓ |
| Segmented confirmed-request send (WPM) | — | ✓ | — | skip | Worldiety skip B6; BACnet4J rejects segmented confirmed receive |
| ReadRange byPosition (TrendLog) | ✓ | — | ✓ | ✓ | `…ReadRangeByPosition`, `TestWorldietyReadRangeByPosition` |
| AtomicReadFile stream/record (`device-baseline-v4`) | ✓ | unsupported | ✓ | — | **live-multi-peer** (stack + BACnet4J); BACpypes3 0.0.106 has no File server; Worldiety loader rejects `file` |
| AtomicWriteFile stream + readback (`device-baseline-v4`) | ✓ | unsupported | ✓ | — | **live-multi-peer**; needs AtomicWriteFile ACK context-Signed wire (`v0.2.3`) |
| CreateObject / DeleteObject (`device-baseline-v5`) | ✓ | unsupported | ✓ | — | **live-multi-peer** (stack + BACnet4J); BACpypes3 lacks `do_CreateObject`; Worldiety has no Create/Delete handlers |
| AddListElement / RemoveListElement NC Recipient_List (`device-baseline-v3`) | ✓ | — | ✓ | — | **live-multi-peer**; Destination bytes BACnet4J-compatible |
| GetAlarmSummary after AV Out_Of_Range (`device-baseline-v3`) | — | — | ✓ | — | `TestBACnet4JGetAlarmSummary`; COV-multiple still unsupported upstream on BACnet4J 6.1.0 |
| TimeSynchronization / UnconfirmedTextMessage (`device-baseline-v6`) | — | — | ✓ | — | `TestBACnet4JTimeSynchronization` (send-only; B7d semantic diagnostics open) |
| Codec goldens (alarm/enrollment/COV-multiple/file/list/messaging/audit/VT) | n/a | n/a | n/a | n/a | Consumed via `internal/fixtures` from `bacnet-interop/fixtures/codec` |
| COV subscribe / notify / cancel | ✓ | ✓ | ✓ | — | `…COVSubscribeNotifyCancel` |
| COV renew | — | ✓ | — | — | `TestBACpypes3COVRenew` |
| EventNotification receive | — | ✓ | ✓ | — | `…EventNotificationReceive` (`BACNET_EMIT_EVENT=1`) |
| DeviceCommunicationControl (enable) | ✓ | — | — | — | `TestBacnetStackDeviceCommunicationControlEnable` |
| ReinitializeDevice warmstart | ✓ | ✓ | ✓ | — | `…ReinitializeDeviceWarmstart` |
| Routed Who-Is-Router → ResolveTarget → RP | — | ✓ | — | — | `TestBACpypes3RoutedWhoIsRouterReadProperty` |
| Routed RP (explicit next-hop DNET/DADR) | ✓ | ✓ | ✓ | — | `TestBACnetStackRoutedReadProperty`, `TestBACnet4JRoutedReadProperty` |
| Foreign-device register + DBTN Who-Is → RP | — | ✓ | ✓ | — | `…ForeignDeviceWhoIsReadProperty` |

See [RELEASE.md](RELEASE.md) for the current tag evidence and
[docs/REAL_DEVICE_GATE.md](docs/REAL_DEVICE_GATE.md) for the open path to
production-usable.

Topology notes:

- Routed scenarios use `bip-router` on two docker networks (client net `1`,
  device net `2`) and **always** re-exec assertions inside the client network.
  Phrase evidence as client addressing validated through this topology aid with
  independent endpoint stacks — not as independent BACnet-router interoperability.
- Routed harnesses assign static `/24`s and IPs, then start `bip-router` with
  explicit `addr=` → BACnet network bindings (not `eth0`/`eth1` order). Docker
  does not guarantee interface order after `create` + `network connect`; binding
  by iface name silently swaps net numbers and drops DNET forwards.
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
| **production-candidate** | Current — supervisory client + oracle/lab interop evidence (`v0.2.2`; pending `v0.2.3` + pin `v0.6.0`); **no** claim of multi-vendor hardware readiness |
| **production-usable** | Real-device gate met — see [docs/REAL_DEVICE_GATE.md](docs/REAL_DEVICE_GATE.md) |

Do not claim vendor hardware interoperability from container oracles alone.
