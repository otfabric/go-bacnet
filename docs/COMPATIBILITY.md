# Compatibility

Concrete peer quirks and hardware results. Service coverage lives in
[CLIENT_SUPPORT.md](CLIENT_SUPPORT.md); scenario-by-peer results in
[INTEROP.md](../INTEROP.md).

## Open-source peer observations

Pinned digests: [bacnet-interop v0.9.0](https://github.com/otfabric/bacnet-interop/releases/tag/v0.9.0)
(`interop/bacnet-interop-pin.json`).

| Peer | Observation |
|---|---|
| **Worldiety** | Native BACnet/IP, NPDU, and APDU runtime. Fixture adapter supplies object model and selected application-service semantics. Segmented continuation APDUs omit ServiceChoice — use unsegmented paths for required scenarios. |
| **BACnet4J** | Rejects segmented confirmed-request receive. GetEnrollmentSummary is the only live peer for that service at the pin. WriteGroup and COV-multiple not implemented. Who-Am-I / You-Are not available. Peer-as-BBMD via env (Read/Write-BDT, Read/Delete-FDT, FDR). |
| **bacnet-stack** | Broad native application surface. GetEnrollmentSummary, ConfirmedPrivateTransfer, TextMessage, and COV-multiple unsupported at 1.6.0. Who-Am-I / You-Are live. BBMD_ENABLED with self-seeded BDT / Accept-FD (FDR + Read-BDT/FDT + Delete-FDT live). Write-BDT NAK at PR≥17. May Abort some segmented ComplexACK paths. Upstream router app not packaged. |
| **BACpypes3** | Strong COV (including renew) and segmented ASE. File / CreateObject / ReadRange / GetAlarmSummary / GetEnrollmentSummary unsupported at 0.0.106. Peer-as-BBMD via env (Read-BDT/FDT, Delete-FDT; Write-BDT NAK). Some WPM / EventNotification / Reinit paths are adapter-assisted. |
| **bip-router** | Topology aid for dual-homed BIP routing only — not a peer oracle. |

Routed client addressing is validated through the OT Fabric interop router with
independent endpoint stacks behind it.

## Hardware devices

Field runs use [FIELD_VALIDATION.md](FIELD_VALIDATION.md). No physical devices
are recorded yet.

| Device | Firmware | Topology | Library | Date | Services / notes |
|---|---|---|---|---|---|
| — | — | — | — | — | Field validation pending |
