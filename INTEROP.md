# Interoperability

Executable scenario results for `go-bacnet` against pinned open-source BACnet
stacks. Peer capability notes live in
[bacnet-interop PEER_SUPPORT.md](https://github.com/otfabric/bacnet-interop/blob/main/PEER_SUPPORT.md).
How to run tests locally: [CONTRIBUTING.md](CONTRIBUTING.md) and
[interop/](interop/).

## How interop testing works

`go-bacnet` owns assertions. Peer images, fixtures, and ready contracts live in
[`bacnet-interop`](https://github.com/otfabric/bacnet-interop). CI checks out a
digest-pinned release (`interop/bacnet-interop-pin.json`) and runs
`-tags=interop` tests against bacnet-stack, BACpypes3, BACnet4J, and Worldiety.
`bip-router` is a topology aid for routed scenarios, not a peer oracle.

Live cells mean a passing automated test against that peer at the pin. Some
Worldiety application semantics are supplied by the fixture adapter (native
transport still exercised). Codec-only rows mean the client API exists but no
pinned peer exposes a usable server path.

## Pinned peers

Pin: [`bacnet-interop` v0.9.0](https://github.com/otfabric/bacnet-interop/releases/tag/v0.9.0)
@ `180006f` — see `interop/bacnet-interop-pin.json`.

| Peer | Upstream | Role |
|---|---|---|
| bacnet-stack | 1.6.0 | C executable oracle |
| BACpypes3 | 0.0.106 | Python oracle (+ BBMD) |
| BACnet4J | 6.1.0 | Java oracle (+ BBMD) |
| Worldiety | `3cb2aa80` | Go peer (native transport; fixture object model for some services) |
| bip-router | topology aid | Routed BIP↔BIP only |

| Symbol | Meaning |
|---|---|
| ✅ | Live passing test |
| — | Stack lacks usable server capability at the pin |
| ⚠ | Known conflicting implementation |
| C | Codec / unit evidence only |

## Application-service matrix

| Scenario | stack | BACpypes3 | 4J | Worldiety | Notes |
|---|:---:|:---:|:---:|:---:|---|
| Who-Is → I-Am | ✅ | ✅ | ✅ | ✅ | |
| Who-Has → I-Have | ✅ | ✅ | ✅ | ✅ | |
| ReadProperty | ✅ | ✅ | ✅ | ✅ | |
| ReadPropertyMultiple | ✅ | ✅ | ✅ | ✅ | |
| WriteProperty / WPM + readback | ✅ | ✅ | ✅ | ✅ | |
| ReadRange byPosition | ✅ | — | ✅ | ✅ | |
| AtomicRead/WriteFile | ✅ | — | ✅ | — | |
| CreateObject / DeleteObject | ✅ | — | ✅ | — | |
| Add/RemoveListElement (NC) | ✅ | — | ✅ | — | |
| GetAlarmSummary | ✅ | — | ✅ | — | |
| GetEnrollmentSummary | — | — | ✅ | — | Single-peer |
| SubscribeCOV / notify / cancel | ✅ | ✅ | ✅ | — | Renew: BACpypes3 |
| SubscribeCOVPropertyMultiple | C | C | C | C | No peer server |
| COVNotificationMultiple | C | C | C | C | No peer emit |
| EventNotification receive | — | ✅ | ✅ | — | |
| AcknowledgeAlarm / GetEventInformation | ✅ | ✅ | ✅ | — | Peer-dependent |
| Messaging (time / text / PT / group) | ✅ | ✅ | ✅ | — | Per-service gaps in PEER_SUPPORT |
| Who-Am-I / You-Are | ✅ | — | — | — | `TestBacnetStackWhoAmIYouAre` |
| LifeSafetyOperation | ✅ | — | ✅ | — | |
| Audit / AuthRequest / VT | C | C | C | C | |
| DCC enable | ✅ | — | — | — | Opt-in |
| ReinitializeDevice warmstart | ✅ | ✅ | ✅ | — | Opt-in |

## Transport, network, and deviations

| Scenario | stack | BACpypes3 | 4J | Worldiety | Notes |
|---|:---:|:---:|:---:|:---:|---|
| BACnet/IP unicast / broadcast | ✅ | ✅ | ✅ | ✅ | |
| Segmented ComplexACK receive | ⚠ | ✅ | ✅ | ⚠ | Stack may Abort; Worldiety continuation omits ServiceChoice |
| Segmented confirmed-request send | — | ✅ | ⚠ | ⚠ | 4J rejects; Worldiety unsegmented-only for required scenarios |
| Routed remote (via bip-router) | ✅ | ✅ | ✅ | — | Topology aid |
| Peer-as-BBMD / FDR | ✅ | ✅ | ✅ | — | Stack BBMD_ENABLED; BACpypes3/4J via `BACNET_BBMD=1` |
| Read-BDT | ✅ | ✅ | ✅ | — | `Test*ReadBDT` |
| Read-FDT (after FD) | ✅ | ✅ | ✅ | — | Empty table alone is not evidence |
| Write-BDT | ⚠ | ⚠ | ✅ | — | Stack/BACpypes3 NAK (asserted); BACnet4J identity write |
| Delete-FDT | ✅ | ✅ | ✅ | — | Register → delete → absence |
| Native multi-homed BIP router | — | — | — | — | Routed tests use `bip-router` aid |

### Known deviations

- **Worldiety** segmented ConfirmedRequest / ComplexACK continuations omit
  ServiceChoice — required Worldiety scenarios stay unsegmented.
- **BACnet4J** rejects segmented confirmed-request receive.
- **bacnet-stack** may Abort on some segmented ComplexACK paths; Write-BDT
  BVLC is NAK at Protocol_Revision ≥ 17.
- No peer image packages a native multi-homed BIP router; see
  bacnet-interop `PEER_SUPPORT.md`.
