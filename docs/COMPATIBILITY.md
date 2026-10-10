# Compatibility

Concrete peer quirks and hardware results. Service coverage lives in
[CLIENT_SUPPORT.md](CLIENT_SUPPORT.md); scenario-by-peer results in
[INTEROP.md](../INTEROP.md).

## Open-source peer observations

Peer images: bacnet-interop v0.10.0 (`interop/bacnet-interop-pin.json`). The
complete list of peer behaviours is in
[bacnet-interop PEER_SUPPORT.md](https://github.com/otfabric/bacnet-interop/blob/main/PEER_SUPPORT.md).

| Peer | Observation |
|---|---|
| **Worldiety** | Native BACnet/IP, NPDU, and APDU runtime. Fixture adapter supplies object model and selected application-service semantics. Segmented continuation APDUs omit ServiceChoice — use unsegmented paths for required scenarios. |
| **BACnet4J** | Rejects segmented confirmed-request receive. The only stack that executes GetEnrollmentSummary itself; the only one with a COVNotificationMultiple encoder (6.1.1). WriteGroup and COV-multiple not implemented. Who-Am-I / You-Are not available. Peer-as-BBMD via env (Read/Write-BDT, Read/Delete-FDT, FDR). |
| **bacnet-stack** | Broad native application surface. GetEnrollmentSummary, ConfirmedPrivateTransfer, TextMessage, and COV-multiple unsupported at 1.6.1. Who-Am-I / You-Are decoded. BBMD_ENABLED with self-seeded BDT / Accept-FD (FDR + Read-BDT/FDT + Delete-FDT live). Write-BDT NAK at PR≥17. May Abort some segmented ComplexACK paths. Upstream router app not packaged. |
| **BACpypes3** | Strong COV (including renew) and segmented ASE. File / CreateObject / ReadRange / GetAlarmSummary unsupported at 0.0.110 (GetEnrollmentSummary through an adapter handler). Peer-as-BBMD via env (Read-BDT/FDT, Delete-FDT; Write-BDT NAK). Some WPM / EventNotification / Reinit paths are adapter-assisted. |
| **System.IO.BACnet** | C# stack behind YABE (4.0.0). Announces Max APDU 1464; sends Abort without the server bit; answers a failed WritePropertyMultiple with a plain Error; cannot return an empty list inside ReadPropertyMultiple; reads a commanded priority-array slot back as a string. Alarm services and record files are adapter-supplied. |
| **bacnet-stack-router** | Upstream bacnet-stack `apps/router`. Does not decrement the hop count; sends no Reject-Message-To-Network. |
| **bip-router** | Topology aid for dual-homed BIP routing only — not a peer oracle. |

Behaviours the interop tests pin down (each is asserted, so a change in a peer
is noticed):

| Behaviour | bacnet-stack | BACpypes3 | BACnet4J | Worldiety |
|---|---|---|---|---|
| ReadProperty of `device,4194303` | own identifier | Error object/unknown-object | own identifier | Error object/unknown-object (adapter) |
| WriteProperty to an Analog Value's Object_Type | Error property/write-access-denied | accepted | Error property/write-access-denied | Error property/write-access-denied |
| Priority_Array on the fixture Analog Value | Error property/unknown-property | served | served | not exercised |
| DeviceCommunicationControl "disable" | Error services/service-request-denied | not exercised | not exercised | no DCC |
| AcknowledgeAlarm with a stale time stamp | Abort (other) | no handler | Error services/invalid-time-stamp | no handler |
| ReadRange reply holding all requested items | MORE_ITEMS clear, also when the log has more | no ReadRange | same | same |

What this means for applications: do not rely on the Device wildcard to
identify a device (fall back to Who-Is); do not treat a clear MORE_ITEMS flag
as the end of a log (`ReadRangeAll` pages until LAST_ITEM); expect a refusal
as Error *or* Abort; expect an Error whose service field is not the one you
called (BACnet4J), which the client delivers all the same; do not assume a
failed WritePropertyMultiple names the element that failed, and assume the
writes before it were applied.

Routed client addressing is validated through the OT Fabric interop router with
independent endpoint stacks behind it.

## Hardware devices

Field runs use [FIELD_VALIDATION.md](FIELD_VALIDATION.md). No physical devices
are recorded yet.

| Device | Firmware | Topology | Library | Date | Services / notes |
|---|---|---|---|---|---|
| — | — | — | — | — | Field validation pending |
