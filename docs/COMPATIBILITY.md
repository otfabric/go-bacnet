# Compatibility

Executable and lab evidence for `go-bacnet`. Prefer this table over informal
README vendor lists. Container oracles are necessary but not sufficient for
**production-usable** status — see [REAL_DEVICE_GATE.md](REAL_DEVICE_GATE.md).

## Positioning

| Claim | Status |
|---|---|
| Highest-quality Go BACnet **supervisory-client** foundation | Supported by current evidence architecture (strict framing, transactions, pinned multi-peer interop, docs) |
| Broadest modern pure-Go BACnet **stack** | **Not claimed** — see [worldiety/bacnet](https://github.com/worldiety/bacnet) for server/BBMD/router/WPM breadth |
| Multi-vendor hardware interoperability | **Not claimed** until the real-device gate is complete |

Do not merge competitor codebases. Turn strong competitors into
[`bacnet-interop`](https://github.com/otfabric/bacnet-interop) adapters when useful.

## Software peers (digest-pinned)

Pin: [`interop/bacnet-interop-pin.json`](../interop/bacnet-interop-pin.json)
([bacnet-interop v0.6.0](https://github.com/otfabric/bacnet-interop/releases/tag/v0.6.0)).
Scenarios: [INTEROP.md](../INTEROP.md), `go test -tags=interop ./interop/...`.

| Peer | Role | Transport / topology | Scenarios (summary) | Evidence |
|---|---|---|---|---|
| bacnet-stack | Independent C peer (1.6.0) | BACnet/IP UDP; optional routed via bip-router | Who-Is/I-Am, Who-Has, RP/RPM/WP/WPM, ReadRange, COV, DCC, Reinit, File, Create/Delete, NC list, Error/Reject/Abort as supported | Pinned GHCR digest (`v0.6.0`); mostly **upstream-native** |
| BACpypes3 | Independent Python peer (0.0.106) | BACnet/IP; peer-as-BBMD; COV renew | RP/RPM/WP/WPM, Who-Has, COV, FD/BBMD, segmented ComplexACK + segmented confirmed send, EventNotification, Reinit | Same; WPM execute + EventNotification emit are **adapter-shim** (stack/segmentation still native) |
| BACnet4J | Independent Java peer (6.1.0) | BACnet/IP; peer-as-BBMD; segmentation | RP/RPM/WP/WPM, Who-Has, ReadRange, COV, FD/BBMD, segmented ComplexACK receive, EventNotification, Reinit, File, Create/Delete, NC list, GetAlarmSummary, TimeSync (`device-baseline-v6`) | Same; EventNotification emit may use adapter assist (`BACNET_EMIT_EVENT`); COV-multiple still unsupported upstream |
| Worldiety | Independent Go peer (`3cb2aa80`) | BACnet/IP; native ASE segmentation | Who-Is/Who-Has, RP/RPM/WP/WPM, ReadRange (unsegmented) | Pinned GHCR digest (`v0.6.0`); service payloads **adapter-shim**; segmented interop skipped (B6) |
| bip-router | Topology aid (**not** a peer oracle) | Dual-homed BIP↔BIP (static addr→network bind) | Routed client DNET/DADR + SNET/SADR path matching | Same |

Evidence types: **upstream-native** (peer implements the service), **adapter-shim**
(peer stack handles the wire; adapter supplies service behaviour),
**topology aid**, **fixture-only**, **real device**. See
[`bacnet-interop` `adapters/inventory.yaml`](https://github.com/otfabric/bacnet-interop/blob/main/adapters/inventory.yaml)
and `COVERAGE.md`.

Phrase for routing until a second independent router exists:

> Routed client addressing and response handling validated through the OT Fabric
> interop router with independent endpoint stacks behind it.

## Other Go peers

| Project | Why | Direction |
|---|---|---|
| worldiety/bacnet | Strongest modern Go competitor | **Adapter pinned** in `bacnet-interop` v0.6.0; reverse client→go-bacnet-server later |
| NubeDev/bacnet | Historical field / routed MS/TP comparison | Behavioral and packet comparison only (GPL-2.0 — no code merge) |
| ulbios/bacnet | Older codec decomposition | Optional historical codec compatibility |

## Real devices

| Device | Vendor / model | Firmware | Transport / topology | Scenarios passed | Date | Evidence | Notes |
|---|---|---|---|---|---|---|---|
| — | — | — | — | — | — | — | Gate open; record rows here when lab runs complete |

Suggested inspiration for lab selection (not evidence): NubeDev’s historical
mentions of Johnson Controls, EasyIO, Reliable Controls, Honeywell — always
capture exact model/firmware and sanitized notes.

## Known field / peer deviations

| Deviation | Observation | Library handling |
|---|---|---|
| Delayed BVLC-Result after FD register | Wire has no generation field; late Result for an earlier attempt at the same BBMD cannot be distinguished | Documented in FD options; correlate by pending attempt + BBMD peer only |
| Peers sizing segment payload to advertised MaxAPDU | Some stacks omit APDU header reserve | Prefer `WithAdvertisedMaxAPDU` < parser `MaxAPDUSize` when coaxing segmentation |
| Segmented confirmed / ComplexACK service choice | Every segment is a full Confirmed-Request / ComplexACK PDU and carries service choice | Encode, decode, and reassembly require a constant service choice across segments |
| BACnet4J segmented confirmed-request receive | Rejects segmented WritePropertyMultiple (reason 9) | Interop send evidence uses BACpypes3; BACnet4J test skipped |
| Docker / published-UDP broadcast | Global broadcast may not reach containers | Directed Who-Is (`broadcast=false`) supported |
| Application- vs context-tagged BACnet Error | Mixed peer encodings | Decoder accepts the forms covered by fixtures/tests; expand here when new forms appear |
| Character-set mislabeling (UTF-8/Latin-1/UCS) | Common on older European controllers | Not yet a Horizon-1 compatibility focus; track when string codecs expand |

## Survey references (not peers)

Kept for lineage / survey only unless promoted above:

- alexbeltran/gobacnet — historical foundation; explicitly unsuitable for dependable use
- maxzerker/bacnet — experimental single-device COV client
- flywave/go-bacnet, kazukiigeta/bacnet — older or minimal framing experiments
