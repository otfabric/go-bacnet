# Compatibility

Concrete peer quirks and hardware results. Service coverage lives in
[CLIENT_SUPPORT.md](CLIENT_SUPPORT.md); scenario-by-peer results in
[INTEROP.md](../INTEROP.md).

## Open-source peer observations

Peer images: bacnet-interop v0.13.0 (`interop/bacnet-interop-pin.json`). The
complete list of peer behaviours is in
[bacnet-interop PEER_SUPPORT.md](https://github.com/otfabric/bacnet-interop/blob/main/PEER_SUPPORT.md).

| Peer | Observation |
|---|---|
| **Worldiety** | Native BACnet/IP, NPDU, and APDU runtime. Fixture adapter supplies object model and selected application-service semantics. Segmented continuation APDUs omit ServiceChoice — use unsegmented paths for required scenarios. |
| **BACnet4J** | Rejects segmented confirmed-request receive. The only stack that executes GetEnrollmentSummary itself; the only one with a COVNotificationMultiple encoder (6.1.1). WriteGroup and COV-multiple not implemented. Who-Am-I / You-Are not available. Peer-as-BBMD via env (Read/Write-BDT, Read/Delete-FDT, FDR). |
| **bacnet-stack** | Broad native application surface. GetEnrollmentSummary, ConfirmedPrivateTransfer, TextMessage, and COV-multiple unsupported at 1.6.1. Who-Am-I / You-Are decoded. BBMD_ENABLED with self-seeded BDT / Accept-FD (FDR + Read-BDT/FDT + Delete-FDT live). Write-BDT NAK at PR≥17. May Abort some segmented ComplexACK paths. Upstream router app not packaged. |
| **BACpypes3** | Strong COV (including renew) and segmented ASE. File / CreateObject / ReadRange / GetAlarmSummary unsupported at 0.0.110 (GetEnrollmentSummary through an adapter handler). Peer-as-BBMD via env (Read-BDT/FDT, Delete-FDT; Write-BDT NAK). Some WPM / EventNotification / Reinit paths are adapter-assisted. |
| **System.IO.BACnet** | C# stack behind YABE (4.0.0). Announces Max APDU 1464; sends Abort without the server bit; answers a failed WritePropertyMultiple with a plain Error; cannot return an empty list inside ReadPropertyMultiple; reads a commanded priority-array slot back as a string. Alarm services and record files are adapter-supplied. |
| **cB-Sigma** | Go, two roles of one image (bacnet-interop 0.13.0). The device role (cB-Sigma/bacnet v0.2.0) serves a Device object, RP/RPM/WP/WPM with priority arrays, COV and segmentation both ways, nothing else. The simulator (bacnet-sim v0.1.0) has no Device object and answers Who-Has only for its demo object; it serves ReadRange, alarm and event summaries the adapter encodes, and peer-as-BBMD (Register-FD, Distribute-Broadcast); a segment it drops on purpose is never resent on its own, only after a NAK. |
| **bacnet-js** | TypeScript, `@bacnet-js/client` 3.3.2 (bacnet-interop image not released yet). Upstream decodes and encodes; the object model and the service procedures are the adapter's. Announces Max_APDU_Length_Accepted 1482 in I-Am (its UDP payload size); its Device object says 1476. Reassembles segmented requests, sends no segmented message: a larger answer is Abort segmentation-not-supported. GetAlarmSummary-ACK uses context tags 12, 9, 8 where clause 13.10.2 has application tags (go-bacnet refuses it). A GetEnrollmentSummary event-state filter is decoded as constructed: offnormal works by accident, normal is dropped. As a client: no WritePropertyMultiple-Error decoder, BACnetDateTime decoded with the weekday as the day, no segmented requests. |
| **ulbios** | Go, ulbios/bacnet at commit `227d6227` (bacnet-interop 0.14.0). Who-Is, ReadProperty and WriteProperty only, with REAL, one-octet Enumerated, two-octet Unsigned (also where one octet would do, against clause 20.2.4), NULL and object identifiers; no CharacterString, Boolean or BitString. No Reject or Abort encoder: anything it cannot decode (other services, property identifiers above 255, array indexes, NULL writes, a request with segmented-response-accepted set, an answer longer than the client's Max APDU) gets no reply. Its parser decodes an object identifier's type shifted left by two and refuses a ReadProperty-ACK shorter than 17 octets; its WriteProperty encoder sends a REAL followed by a NULL in the value. The adapter supplies the object model and works around the decoders (adapter-shim). |
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

Field runs use [FIELD_VALIDATION.md](FIELD_VALIDATION.md). No live field
run is recorded yet. Traffic of physical devices recorded by others, and how
go-bacnet's client and server compare with it, is in
[testdata/captures](../testdata/captures/README.md); the device quirks it
shows:

| Device | Quirk | go-bacnet |
|---|---|---|
| Lithonia SYSC MLX 2x63w | Answers a ReadPropertyMultiple with an empty listOfPropertyReferences with an empty listOfResults; answers max-APDU code 6 (reserved) with Abort other | Refuses both |
| Lithonia, Delta DSC-1616E | Password failure with error class services | Class security |
| WattStopper LMBC-300 04.22, PolarSoft 9260 3.30, Lithonia | Refuse ReinitializeDevice START_BACKUP (service-request-denied, Reject parameter-out-of-range, Reject undefined-enumeration) | The server without a `Reinitialize` hook answers service-request-denied |
| Siemens (vendor 7) | ConfirmedPrivateTransfer 511 with untagged octets in serviceParameters | Refused, as Wireshark marks it malformed |
| PolarSoft 9260 3.30 | Latin-1 "®" in vendor and model names marked UTF-8 | Octets kept as received |
| Critical Room Control Room Pressure Monitor 5.5 | Vendor and model names end in a NUL | Kept as received |
| A plugfest router (172.16.86.6) | Forwards answers without their first APDU octet | Refused |
| WattStopper WebLink | NPDUs with SNET after the hop count, SNET bit clear | Refused |
| Alerton Envision, Delta DNT-T103, Reliable Controls | Actual window 1 for every segmented answer, SegmentACK for each segment | Client acknowledges as they do (replayed) |

| Device | Firmware | Topology | Library | Date | Services / notes |
|---|---|---|---|---|---|
| — | — | — | — | — | Field validation pending |
