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
([bacnet-interop v0.2.1](https://github.com/otfabric/bacnet-interop/releases/tag/v0.2.1)).
Scenarios: [INTEROP.md](../INTEROP.md), `go test -tags=interop ./interop/...`.

| Peer | Role | Transport / topology | Scenarios (summary) | Evidence |
|---|---|---|---|---|
| bacnet-stack | Independent C peer | BACnet/IP UDP; optional routed via bip-router | Who-Is/I-Am, RP, RPM (+ partial errors), WP, COV, Error/Reject/Abort paths as supported | Pinned GHCR digest + GitHub Actions interop artifacts |
| BACpypes3 | Independent Python peer | BACnet/IP; peer-as-BBMD; COV renew | RP/RPM/WP/COV; FD/BBMD; segmentation where exercised | Same |
| BACnet4J | Independent Java peer | BACnet/IP; peer-as-BBMD; segmentation | RP/RPM/WP/COV; FD/BBMD; segmented ComplexACK | Same |
| bip-router | Topology aid (**not** a peer oracle) | Dual-homed BIP↔BIP | Routed client DNET/DADR + SNET/SADR path matching | Same |

Phrase for routing until a second independent router exists:

> Routed client addressing and response handling validated through the OT Fabric
> interop router with independent endpoint stacks behind it.

## Planned Go peers (not yet adapters)

| Project | Why | Direction |
|---|---|---|
| worldiety/bacnet | Strongest modern Go competitor; server/BBMD/router/WPM surface | Fourth independent peer; later bidirectional once OT Fabric has a server |
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
| Docker / published-UDP broadcast | Global broadcast may not reach containers | Directed Who-Is (`broadcast=false`) supported |
| Application- vs context-tagged BACnet Error | Mixed peer encodings | Decoder accepts the forms covered by fixtures/tests; expand here when new forms appear |
| Character-set mislabeling (UTF-8/Latin-1/UCS) | Common on older European controllers | Not yet a Horizon-1 compatibility focus; track when string codecs expand |

## Survey references (not peers)

Kept for lineage / survey only unless promoted above:

- alexbeltran/gobacnet — historical foundation; explicitly unsuitable for dependable use
- maxzerker/bacnet — experimental single-device COV client
- flywave/go-bacnet, kazukiigeta/bacnet — older or minimal framing experiments
