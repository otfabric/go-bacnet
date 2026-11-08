# Recorded BACnet/IP traffic of physical devices

Captures between real BACnet devices and their clients, recorded by others
at BACnet plugfests and in labs. go-bacnet's tests decode the recorded
datagrams, compare every one with Wireshark's decode, encode them again,
run the client against the recorded answers and send the recorded requests
to the server. This is evidence from real devices recorded by others. It is
not a test against a live device, and it is not a conformance claim.

## Source and terms

| Source | Revision | Terms |
|---|---|---|
| [kargs.net/captures](https://kargs.net/captures/) by Steve Karg (BACnet Stack author) | directory listing of 2026-11-08: 217 files, 246 MB | No licence or redistribution statement |

[github.com/EmreEkin/ICS-Pcaps](https://github.com/EmreEkin/ICS-Pcaps/tree/master/BacNET)
`BacNET/` holds 16 of the same files, re-saved as pcap, with no licence
either. It is not used separately.

**The original captures are not in this repository.** Public availability
is not permission to redistribute. Each golden case below commits only the
individual datagrams it needs, as hex excerpts with their frame numbers,
time stamps and addresses. The owner may revisit that judgement: with
written permission from the source, the originals could be committed
byte-identical, as in go-s7comm.
[SHA256SUMS](SHA256SUMS) pins every original, and
`make captures-fetch` downloads them into the git-ignored `cache/` and
verifies every hash.

## Evidence classes

| Class | Meaning | Files |
|---|---|---:|
| A | The capture holds the device's own Device object answers: model name, and firmware where it reports one (read by go-bacnet and cross-checked with Wireshark) | 36 |
| B | Vendor IDs in I-Am, but no model in the traffic, or identity from another capture of the same lab | 57 |
| C | Device origin not in the traffic, or no BACnet/IP (MS/TP, ARCNET, Ethernet only) | 107 |
| D | BACnet Stack demo applications (vendor 260, models "GNU", "Simple BACnet Server") or traffic corrupted on purpose | 10 |
| - | Not a pcap or pcapng file (text, NCF, CBL) | 7 |

Only A and B support a statement about real devices. C and D are parser
regression and fuzz input. [manifest.json](manifest.json) records for each
of the 217 files its URL, SHA256, size, class and the basis for it, the
devices it identifies, and the counts go-bacnet and Wireshark derive from
it.

## Devices

From the devices' own answers (vendor name, model name, firmware revision)
in the golden cases. "Routed" devices sit on another BACnet network behind
the router named before them.

| Device | Firmware | Golden case |
|---|---|---|
| Trane Tracer SC (system controller, router) | 2.6.30_HwVer12A-hydra, application v3.00.0558 | trane-plugfest-2011 |
| Trane UC400, routed through the Tracer SC | 1.00.042 | trane-plugfest-2011 |
| Trane BSV | v6.97 | trane-plugfest-2011 |
| Tridium NiagaraAX Station / BACnet Advanced Workstation (software) | 3.6.38 / 3.7.18 | tridium-plugfest |
| PolarSoft 9260 Linux Server (router) | 3.30 | tridium-plugfest, sauter-plugfest-2011 |
| WattStopper LMBC-300, routed through the PolarSoft server | 04.22 | tridium-plugfest, siemens-plugfest-2011, sauter-plugfest-2011 |
| Reliable Controls MACH-ProSys | 7.06.14765 | reliable-controls-routed |
| Reliable Controls ETHER-Link (router) / MACH2 behind it | 6.64 / 6.43 | reliable-controls-routed |
| Delta Controls DSC-1616E (router) / DNT-T103 / DNT-T221 behind it | 15604 / 3319 / 0028 | delta-plugfest |
| Alerton BCM-Eth (router) / VLC-853 behind it / Envision for BACtalk | Loader 2.00 (Build 16) / RL Z.01d / - | alerton-plugfest |
| Lithonia Lighting SYSC MLX | 2x63w, 2x65j | lithonia-btl-plugfest, delta-plugfest, alerton-plugfest |
| Critical Room Control Room Pressure Monitor, routed | 5.5 | critical-room-control |
| Contemporary Controls BASRT-B (router, UDP 47812) | 1.24 | contemporary-controls-basrt |
| WattStopper WebLink (router) | - | wattstopper-weblink |

Siemens (vendor 7), Fr. Sauter AG (80), PolarSoft (4) and Automated Logic
(24) devices also appear with their vendor ID only (class B).

## Golden cases

`golden/<id>.json`: the datagrams excerpted from one capture, each with
Wireshark 4.6.9's decode of it (`tshark -T fields`, PRP dissector off: it
claims the Ethernet padding of short frames and marks them malformed). Each
case also holds the devices it identifies, its limitations, the classified
differences between go-bacnet and the recording, and the frames that
exposed a defect now fixed. The generator keeps whole transactions (every
request and answer segment and SegmentACK), up to a quota per responder,
service and answer type, and per sender for unconfirmed, network-layer and
BVLL messages, so a case covers what the device did, not every repetition.

| Case | Class | Frames | Covers |
|---|:-:|---:|---|
| trane-plugfest-2011 | A | 62 | ReadProperty and ReadPropertyMultiple of three Trane devices, one routed |
| tridium-plugfest | A | 127 | NiagaraAX and a routed LMBC-300: reads, writes, SubscribeCOV, COV notifications |
| reliable-controls-routed | A | 104 | Routed reads (DNET/SNET); three-segment answers with window 1 |
| delta-plugfest | A | 119 | Segmented requests and answers with windows 1 and 2, AtomicWriteFile, events, ReinitializeDevice |
| alerton-plugfest | A | 102 | Answers in up to nine segments acknowledged one by one |
| lithonia-btl-plugfest | A | 68 | BTL test client: empty property list, reserved max-APDU code, DeviceCommunicationControl, Who-Has |
| siemens-plugfest-2011 | A | 106 | Siemens ConfirmedPrivateTransfer 511, ReinitializeDevice START_BACKUP, GetEventInformation |
| sauter-plugfest-2011 | A | 40 | Sauter devices with the PolarSoft router and an LMBC-300 |
| critical-room-control | A | 19 | Routed reads of a Room Pressure Monitor |
| contemporary-controls-basrt | A | 30 | A BACnet router on UDP 47812 |
| wattstopper-weblink | A | 37 | A router whose NPDUs carry SNET after the hop count |
| trend-log-multiple | B | 100 | ReadRange of a Trend Log Multiple (segmented) and a Trend Log |
| event-log | B | 2 | ReadRange of an Event Log |
| lithonia-reinitialize | B | 33 | ReinitializeDevice with the undefined state 99 |
| bvll-foreign-device | B | 28 | Register-Foreign-Device, Distribute-Broadcast-To-Network, Forwarded-NPDU |
| cov-plugfest-2011 | B | 18 | SubscribeCOV and COV notifications |
| atomic-read-file-segmented | C | 48 | AtomicReadFile answers in up to five segments, window 16 |
| bvll-management-results | C | 8 | Write/Read-BDT, Read-FDT, Register-Foreign-Device to a node that is not a BBMD |
| router-drops-apdu-type | C | 15 | A router that drops the first APDU octet of the answers it forwards |
| routing-table | C | 8 | Who-Is-Router, I-Am-Router, Initialize-Routing-Table(-Ack) |

20 cases, 1,072 datagrams.

## What the tests check

| Test | Checks |
|---|---|
| `internal/capture` `TestGoldenMatchesWireshark` | Every datagram decodes as Wireshark decodes it: BVLC function, NPDU control, DNET and SNET, network message type, APDU type, service, invoke ID, sequence number and window. go-bacnet refuses exactly the datagrams Wireshark marks malformed, apart from the classified differences |
| `TestGoldenReencode` | What go-bacnet decodes it encodes again to the recorded octets, at every layer: BVLC, NPDU, APDU and service. That covers every service with an encoder in `internal/capture/encode.go`: Who-Is, I-Am, ReadProperty(Multiple) and their ACKs, WriteProperty(Multiple), SubscribeCOV, COV and event notifications, ReadRange, AtomicRead/WriteFile, ReinitializeDevice, DeviceCommunicationControl, TimeSynchronization, PrivateTransfer and Who-Has/I-Have |
| `TestGoldenDevices` | The devices a case names are identified from the excerpt alone, as the generator found them in the whole capture |
| `TestGoldenTransactions` | Every answer has its request (invoke-ID correlation per requester and responder), and answers the service it was asked |
| `TestGoldenRegressions` | The datagrams that exposed a defect decode completely |
| `TestGoldenMutationsDecode` | Every truncation, wrong BVLC lengths and seeded bit flips of every datagram (about 50,000 variants) decode or fail without a panic |
| `client` `TestCaptureReplay` | The client runs against the recorded answers: 254 transactions (ReadProperty, ReadPropertyMultiple, ReadRange, AtomicReadFile, WriteProperty), 12 of them segmented. Its request decodes to the recorded one with the same service octets. Every SegmentACK it sends has the sequence number and actual window the recorded requester sent. It returns the recorded value, or the recorded Error class and code, Reject or Abort reason |
| `client` `TestCaptureReplayDisrupted` | The recorded segmented answers with duplicated segments give the same result; with the last segment lost or the first two swapped the call fails within the segment timeout |
| `server` `TestCaptureServerParity` | A server standing in for the recorded device gets the recorded requests that do not depend on the device's own objects: its Device object (same instance), ReinitializeDevice, DeviceCommunicationControl, GetEventInformation, GetAlarmSummary, PrivateTransfer. 150 answers equal the device's; the 66 that differ are classified (below) |
| `server` `TestCaptureBVLLResultParity` | Write-BDT, Read-BDT, Read-FDT and Register-Foreign-Device to a server without a BBMD get the recorded NAK codes |
| `server` `TestCaptureMutatedRequests` | About 20,000 malformed variants of the recorded requests: every answer is well-formed, and the server still answers afterwards |
| fuzz targets | `FuzzParseBVLC`, `FuzzParseNPDU`, `FuzzParseAPDU`, `FuzzServerReceive`, `FuzzClientReceive` and ten service decoders are seeded with the recorded octets (`capture.LayerSeeds`) |
| `TestExtendedCorpus` (`make captures-extended`) | All 217 originals: SHA256, and go-bacnet's counts and device identities equal [manifest.json](manifest.json). 827,546 BACnet/IP datagrams: 825,495 decode, 2,051 are refused; 322,355 transactions are correlated, 53,852 of them segmented |

[report/](report) is the machine-readable coverage report the suites
compute: per case in `decode.json` and `client.json`, and per answer kind in
`server.json`. A stale report fails the suite that computed it.
`make captures-report` rewrites it.

## Found and fixed

| Defect | Evidence | Fix |
|---|---|---|
| `service.DecodeReadRangeACK` decoded every Log_Buffer as BACnetLogRecord. It refused a Trend Log Multiple's BACnetLogMultipleRecords and an Event Log's BACnetEventLogRecords ("not context boolean"), so `client.ReadRange` and `ReadRangeAll` failed on both; the client aborted a segmented answer | trend-log-multiple frames 2 to 799 (PolarSoft, vendor 4), event-log frame 2 (Automated Logic, vendor 24) | `ReadRangeACK.LogMultipleRecords` and `EventLogRecords`, by the object type of the answer. Both recorded answers decode and encode again byte for byte |
| The server acknowledged ReinitializeDevice START_BACKUP to ABORT_RESTORE without a `Reinitialize` hook, though it has no backup or restore procedure | Every recorded device refuses START_BACKUP: the WattStopper LMBC-300 with services/service-request-denied (siemens-plugfest-2011 frame 216), the PolarSoft server with Reject parameter-out-of-range (frame 293), a Lithonia device with Reject undefined-enumeration (lithonia-reinitialize frame 39) | Without a hook those states get services/service-request-denied |

The capture reader itself first took a NetBIOS name query whose
transaction ID starts with 0x81 for BACnet. On a port outside 0xBAC0 to
0xBACF it now requires a well-formed BVLL header.

## Differences that remain

Listed in each golden case and in `report/server.json`, with their
classification:

| Recorded | go-bacnet | Classification |
|---|---|---|
| A BTL test client's ReadPropertyMultiple with an empty listOfPropertyReferences; the SYSC MLX answers an empty listOfResults | Refuses it; the server answers Reject invalid-tag | Intentional implementation difference (no evidence the standard requires accepting it) |
| A request with max-APDU code 6 (reserved, clause 20.1.2.5); the SYSC MLX answers Abort other | Refuses the APDU; the server drops it | Intentional implementation difference |
| Password failure answered with class services by the SYSC MLX (DeviceCommunicationControl), the Delta DSC-1616E and a Lithonia device (ReinitializeDevice) | Class security | Vendor difference |
| START_BACKUP refused with Reject (undefined-enumeration, parameter-out-of-range) | Error services/service-request-denied, as the LMBC-300 answers | Vendor difference |
| DeviceCommunicationControl DISABLE acknowledged by a 2005 device | services/service-request-denied: DISABLE is deprecated from protocol revision 20 | Protocol revision |
| ConfirmedPrivateTransfer 511 of Siemens devices with untagged octets in serviceParameters | Refused (Wireshark marks them malformed too) | Vendor-specific non-conformant encoding |
| A router forwarding answers without their first APDU octet; a WattStopper WebLink putting SNET after the hop count without the SNET bit | Refused (Wireshark reads them as a Reject or a ConfirmedRequest) | Peer defect |
| PolarSoft's vendor and model names: Latin-1 "®" in a string marked UTF-8 | Kept as the octets received | Vendor quirk |
| Unknown or extra optional properties, unsupported services (Reject unrecognized-service), answers too large for the requester | Different answers | Object model or capability of the device, not protocol behaviour |

## Not covered

- BACnet on MS/TP, ARCNET and ISO 8802-2 Ethernet: 83 files hold such
  frames. They are counted in the manifest (`non_ip`) and not decoded:
  go-bacnet implements BACnet/IP only.
- The server against values of the recorded devices' objects: its answers
  depend on the objects it holds, so only the requests listed above are
  compared.
- Segmented requests from the client: the client chooses its own segment
  size, so the recorded segmented requests (Delta OWS340) are decoded and
  correlated but not replayed.
- No live device was tested. Nothing here is a BTL or conformance result.

## Updating

`make captures-generate` (needs tshark and the cache) rebuilds
`manifest.json`, `SHA256SUMS` and `golden/` from the originals, using the
case list in `internal/capture/cases_test.go`. Then run
`make captures-report`, `make captures-extended` and `make test`.
