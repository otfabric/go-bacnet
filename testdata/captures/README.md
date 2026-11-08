# Recorded BACnet/IP traffic of physical devices

Captures between real BACnet devices and their clients, recorded by others
at BACnet plugfests and in labs, committed byte-identical. go-bacnet's tests
decode every datagram of every capture and encode it again, correlate every
confirmed transaction, run the client against every recorded answer and
send the recorded requests to the server. Golden cases additionally compare
excerpts field by field with Wireshark. This is evidence from real devices
recorded by others: not a test against a live device, and not a
conformance claim.

## Sources and terms

| Directory | Source | Revision | Terms |
|---|---|---|---|
| `kargs/`, `kargs-non-ip/` | [kargs.net/captures](https://kargs.net/captures/) by Steve Karg (BACnet Stack author) | directory listing of 2026-11-08, 217 files | No licence or redistribution statement on the site |
| `icspcaps/`, `icspcaps-non-ip/` | [EmreEkin/ICS-Pcaps](https://github.com/EmreEkin/ICS-Pcaps/tree/master/BacNET) `BacNET/` | commit `0ffbf023afb4e39373228b54851bb4a1f33a3ab8` | No licence file; 16 of the kargs.net files re-saved as pcap, and `bacnet_test.pcap` |

The files are byte-identical to their sources, checked by
[SHA256SUMS](SHA256SUMS) (`shasum -a 256 -c SHA256SUMS` in this directory).
Two kargs.net files are over 50 MB (`alerton-plugfest.cap`,
`softdel-BTL.cap`) and are not committed: [CACHED](CACHED) lists them with
their SHA256 and URL, `make captures-fetch` downloads them into the
git-ignored `cache/`, and `make captures-extended` runs the corpus suites
over them too. Seven kargs.net files are not captures (text, NCF, CBL) and
are listed in the manifest only.

`*-non-ip/` holds captures without a BACnet/IP datagram (MS/TP, ARCNET,
ISO 8802-2 Ethernet): go-bacnet implements BACnet/IP only, so they are read
and counted but are no evidence for it. Captures in the other directories
may hold such frames as well; the manifest counts them (`non_ip`).

## Names

`<peer>_<device>_<operation>.<ext>`, lower case, words joined by hyphens;
the extension keeps the original format. A golden case is
`<capture name>.json`, next to its capture.

| Part | Meaning |
|---|---|
| peer | The client that ran the exchange. `unknown` throughout: neither source states it |
| device | Vendor and model of the device answering most often, from its own Device object (`trane-uc400`); else the vendor of the most frequent I-Am (`lithonia`, `ashrae` for vendor ID 0); else `unknown`. `bacnet-stack-demo` is a BACnet Stack demo application |
| operation | The original file name |

[manifest.json](manifest.json) maps every file to its original name and
URL, and records its size, SHA256, evidence class and its basis, the
devices it identifies, and the counts go-bacnet and Wireshark 4.6.9 derive
from it.

## Evidence classes

| Class | Meaning | kargs.net | ICS-Pcaps |
|---|---|---:|---:|
| A | The capture holds the device's own Device object answers: model name, and firmware where it reports one | 36 | 2 |
| B | Vendor IDs in I-Am, but no model in the traffic, or identity from another capture of the same lab | 57 | 7 |
| C | Device origin not in the traffic, or no BACnet/IP | 107 | 8 |
| D | BACnet Stack demo applications or traffic corrupted on purpose | 10 | 0 |
| - | Not a capture | 7 | 0 |

Only A and B support a statement about real devices; C and D are parser
regression and fuzz input.

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

Datagrams excerpted from one capture, each with Wireshark 4.6.9's decode
(`tshark -T fields`, PRP dissector off: it claims the Ethernet padding of
short frames and marks them malformed), the devices the capture
identifies, its limitations, the classified differences between go-bacnet
and the recording, and the frames that exposed a defect now fixed. Whole
transactions are kept (every request and answer segment and SegmentACK),
up to a quota per responder, service and answer type.

| File | Case | Original name | Class | Frames |
|---|---|---|:-:|---:|
| `kargs/unknown_lithonia-spak-mstp_alerton-plugfest-2.json` | alerton-plugfest | alerton-plugfest-2.cap | A | 102 |
| `kargs/unknown_unknown_read-file.json` | atomic-read-file-segmented | read-file.cap | C | 48 |
| `kargs/unknown_bacnet-stack-demo_bvlc-fdreg-readprop-47809.json` | bvll-foreign-device | bvlc-fdreg-readprop-47809.cap | B | 28 |
| `kargs/unknown_unknown_bbmd-results.json` | bvll-management-results | BBMD_Results.pcap | C | 8 |
| `kargs/unknown_contemporary-controls-basrt-b_bvlc-bac4-rp.json` | contemporary-controls-basrt | bvlc-bac4-rp.cap | A | 30 |
| `kargs/unknown_wattstopper_cov-testing-1.json` | cov-plugfest-2011 | cov-testing-1.pcap | B | 18 |
| `kargs/unknown_critical-room-control-room-pressure-monitor_bip-readprop.json` | critical-room-control | bip-readprop.cap | A | 19 |
| `kargs/unknown_lithonia-spak-mstp_plugfest-delta-2b.json` | delta-plugfest | plugfest-delta-2b.cap | A | 119 |
| `kargs/unknown_unknown_eventlog-readrange.json` | event-log | eventLog_ReadRange.pcap | B | 2 |
| `kargs/unknown_lithonia-sysc-mlx_btl-plugfest.json` | lithonia-btl-plugfest | btl-plugfest.cap | A | 68 |
| `kargs/unknown_lithonia_reinit-device.json` | lithonia-reinitialize | reinit-device.cap | B | 33 |
| `kargs/unknown_reliable-controls-mach-prosys_testrun8-outside-of-router.json` | reliable-controls-routed | TestRun8 - Outside of Router.pcap | A | 104 |
| `kargs/unknown_unknown_criticalroom55-1.json` | router-drops-apdu-type | CriticalRoom55-1.cap | C | 15 |
| `kargs/unknown_unknown_router.json` | routing-table | router.cap | C | 8 |
| `kargs/unknown_wattstopper-lmbc-300_plugfest-2011-sauter-1.json` | sauter-plugfest-2011 | plugfest-2011-sauter-1.pcap | A | 40 |
| `kargs/unknown_wattstopper-lmbc-300_plugfest-2011-siemens-1.json` | siemens-plugfest-2011 | plugfest-2011-siemens-1.pcap | A | 106 |
| `kargs/unknown_trane-uc400_plugfest-2011-trane-1.json` | trane-plugfest-2011 | plugfest-2011-trane-1.pcap | A | 62 |
| `kargs/unknown_automated-logic_trendlogmultiplereadrangesimple.json` | trend-log-multiple | TrendLogMultipleReadRangeSimple.pcap | B | 100 |
| `kargs/unknown_wattstopper-lmbc-300_plugfest-tridium-1.json` | tridium-plugfest | plugfest-tridium-1.pcap | A | 127 |
| `kargs/unknown_wattstopper-weblink_dri-cave-log-udp-0168-20081216-1117-03.json` | wattstopper-weblink | DRI CAVE log udp-0168-20081216-1117-03.cap | A | 37 |

## What the tests check

Every suite runs in `make test`, offline.

| Test | Over | Checks |
|---|---|---|
| `internal/capture` `TestCorpus` | every committed capture (225 files; 286,597 BACnet/IP datagrams) | SHA256; go-bacnet's counts (decoded, refused by layer, re-encoded byte for byte, re-encode differences, transactions, answered, orphan answers, segmented, SegmentACKs) and device identities equal [manifest.json](manifest.json) |
| `client` `TestCorpusReplay` | every answered transaction of every committed capture | The client sends the request through its public API (ReadProperty, ReadPropertyMultiple, ReadRange, AtomicReadFile, WriteProperty), its request carries the recorded service octets, its SegmentACKs the recorded sequence numbers and windows, and it returns the recorded outcome: 124,363 transactions, 799 segmented. 81 are classified failures: the Room Pressure Monitor's ACKs that name an array index not requested (the client refuses them), and one corrupted test exchange |
| `server` `TestCorpusServerParity` | every committed capture | A stand-in server gets the recorded requests whose answer does not depend on the device's own objects (its Device object, ReinitializeDevice, DeviceCommunicationControl, GetEventInformation, GetAlarmSummary, PrivateTransfer): 10,163 answers equal the devices'; 4,006 differ, every one classified ([report/server-corpus.json](report/server-corpus.json)) |
| `internal/capture` `TestGolden*` | golden cases (1,072 datagrams) | Field by field equal to Wireshark (BVLC function, NPDU control, DNET, SNET, network message, APDU type, service, invoke ID, sequence number, window); go-bacnet refuses exactly what Wireshark marks malformed, apart from classified differences; byte-exact re-encode; device identities; invoke-ID correlation; regression frames decode; about 50,000 mutated variants decode or fail cleanly |
| `client` `TestCaptureReplay`, `TestCaptureReplayDisrupted` | golden cases | As the corpus replay; and segmented answers with segments duplicated (same result), lost or swapped (error within the segment timeout) |
| `server` `TestCaptureServerParity`, `TestCaptureBVLLResultParity`, `TestCaptureMutatedRequests` | golden cases | Parity as above; BVLC-Result NAK codes of a node that is not a BBMD; about 20,000 malformed variants of the recorded requests |
| fuzz targets | golden cases | BVLC, NPDU, APDU, `FuzzServerReceive`, `FuzzClientReceive` and ten service decoders are seeded with the recorded octets |

Over all 235 originals (`make captures-extended`, the two large ones
included): 835,678 BACnet/IP datagrams and 326,262 transactions.
Re-encoding: every decoded datagram whose service has an encoder in
`internal/capture/encode.go` encodes again byte for byte, apart from 21
Trane ReadProperty ACKs that encode an array index in two octets (00 80)
where go-bacnet uses the minimum one (clause 20.2.4).

[report/](report) is the machine-readable coverage report the suites
compute (`decode.json`, `client.json`, `server.json` for the golden cases,
`client-corpus.json`, `server-corpus.json` for the corpus). A stale report
fails the suite that computed it; `make captures-report` rewrites it.

## Found and fixed

| Defect | Evidence | Fix |
|---|---|---|
| `service.DecodeReadRangeACK` decoded every Log_Buffer as BACnetLogRecord. It refused a Trend Log Multiple's BACnetLogMultipleRecords and an Event Log's BACnetEventLogRecords ("not context boolean"), so `client.ReadRange` and `ReadRangeAll` failed on both; the client aborted a segmented answer | trend-log-multiple frames 2 to 799 (PolarSoft, vendor 4), event-log frame 2 (Automated Logic, vendor 24) | `ReadRangeACK.LogMultipleRecords` and `EventLogRecords`, by the object type of the answer. Both recorded answers decode and encode again byte for byte |
| The server acknowledged ReinitializeDevice START_BACKUP to ABORT_RESTORE without a `Reinitialize` hook, though it has no backup or restore procedure | The devices without backup support refuse START_BACKUP: the WattStopper LMBC-300 with services/service-request-denied (siemens-plugfest-2011 frame 216), the PolarSoft server with Reject parameter-out-of-range (frame 293), a Lithonia device with Reject undefined-enumeration (lithonia-reinitialize frame 39). A Delta DAC-T305, which supports backup, acknowledges it (plugfest-delta-2 frame 11242) | Without a hook those states get services/service-request-denied |

| The encoder of an extended [9] event notification dropped an empty `parameters` list, which is not OPTIONAL (clause 13.3) | `kargs/unknown_unknown_cen-9-11.pcap` frame 3 (vendor 24) | `parameters [2]` is always encoded; the recorded notification encodes again byte for byte |

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
| Trane Tracer SC: an array index of 128 in two octets (00 80) | Decoded; encoded in one octet | Non-minimal encoding (clause 20.2.4 asks for the fewest octets) |
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

`make captures-generate` (needs tshark, and `BACNET_CAPTURE_ORIGINALS`
pointing at `<source>/<original name>`) rebuilds `manifest.json`,
`SHA256SUMS`, `CACHED`, the committed captures and the golden cases, using
the case list in `internal/capture/cases_test.go`. Then run
`make captures-report`, `make captures-extended` and `make test`.

## Files

| File | Original name | Class | BACnet/IP datagrams | Note |
|---|---|:-:|---:|---|
| `icspcaps/unknown_bacnet-stack_bacnet-bbmd-on-same-subnet.pcap` | BACnet-BBMD-on-same-subnet.pcap | B | 14 |  |
| `icspcaps/unknown_bacnet-stack-demo_bacnet-mstp-snap-mixed.pcap` | BACnet-MSTP-SNAP-Mixed.pcap | B | 68 |  |
| `icspcaps/unknown_unknown_bacnet-exception-schedule-property-1.pcap` | BACnet-exception-schedule-property-1.pcap | C | 2 |  |
| `icspcaps/unknown_unknown_bacnet-exception-schedule-property-2.pcap` | BACnet-exception-schedule-property-2.pcap | C | 1 |  |
| `icspcaps/unknown_unknown_bacnetarray-element-0.pcap` | BACnetARRAY-element-0.pcap | C | 6 |  |
| `icspcaps/unknown_bacnet-stack_bacnetarray-elements.pcap` | BACnetARRAY-elements.pcap | C | 40 |  |
| `icspcaps/unknown_unknown_bacnetdeviceobjectreference.pcap` | BACnetDeviceObjectReference.pcap | C | 4 |  |
| `icspcaps/unknown_bacnet-stack-demo_bacnetip-mstp-mix.pcap` | BACnetIP-MSTP-Mix.pcap | B | 72 |  |
| `icspcaps/unknown_unknown_bacnetl-schedrpm.pcap` | BACnetL_SchedRPM.pcap | C | 2 |  |
| `icspcaps-non-ip/unknown_unknown_bacnet-arcnet.pcap` | bacnet-arcnet.pcap | C | 0 |  |
| `icspcaps/unknown_ashrae_bacnet-ethernet-device.pcap` | bacnet-ethernet-device.pcap | B | 9 |  |
| `icspcaps/unknown_lithonia_bacnet-ethernet.pcap` | bacnet-ethernet.pcap | B | 8 |  |
| `icspcaps/unknown_bacnet-stack-demo_bacnet-ip.pcap` | bacnet-ip.pcap | B | 833 |  |
| `icspcaps/unknown_lithonia_bacnet-properties.pcap` | bacnet-properties.pcap | B | 2 |  |
| `icspcaps/unknown_lithonia-sysc-mlx_bacnet-services.pcap` | bacnet-services.pcap | A | 7049 |  |
| `icspcaps/unknown_j-c-i-m-s-n-a-e-4-5-1-0-2_bacnet-test.pcap` | bacnet_test.pcap | A | 22 |  |
| `icspcaps-non-ip/unknown_unknown_mstp-20090227094623.pcap` | mstp_20090227094623.pcap | C | 0 |  |
| `kargs/unknown_bacnet-stack_bacnet-bbmd-on-same-subnet.cap` | BACnet-BBMD-on-same-subnet.cap | B | 14 |  |
| `kargs/unknown_bacnet-stack-demo_bacnet-mstp-snap-mixed.cap` | BACnet-MSTP-SNAP-Mixed.cap | B | 68 |  |
| `kargs/unknown_unknown_bacnet-exception-schedule-property-1.pcapng` | BACnet-exception-schedule-property-1.pcapng | C | 2 |  |
| `kargs/unknown_unknown_bacnet-exception-schedule-property-2.pcapng` | BACnet-exception-schedule-property-2.pcapng | C | 1 |  |
| `kargs/unknown_unknown_bacnetarray-element-0.cap` | BACnetARRAY-element-0.cap | C | 6 |  |
| `kargs/unknown_bacnet-stack_bacnetarray-elements.cap` | BACnetARRAY-elements.cap | C | 40 |  |
| `kargs/unknown_unknown_bacnetdeviceobjectreference.pcap` | BACnetDeviceObjectReference.pcap | C | 4 |  |
| `kargs/unknown_bacnet-stack-demo_bacnetip-mstp-mix.cap` | BACnetIP-MSTP-Mix.cap | B | 72 |  |
| `kargs/unknown_unknown_bacnetl-schedrpm.pcapng` | BACnetL_SchedRPM.pcapng | C | 2 |  |
| `kargs/unknown_unknown_bbmd-results.pcap` | BBMD_Results.pcap | C | 8 |  |
| `kargs/unknown_bacnet-stack-demo_bbmd-readproperty.pcap` | BBMD_readproperty.pcap | D | 336 |  |
| `` | Bug 4.ncf | - | - | not committed (not a pcap) |
| `kargs/unknown_unknown_cen-10.pcap` | CEN_10.pcap | C | 1 |  |
| `kargs/unknown_unknown_cen-9-11.pcap` | CEN_9_11.pcap | C | 8 |  |
| `kargs/unknown_bacnet-stack_cov-awf-arf.cap` | COV_AWF_ARF.cap | C | 1350 |  |
| `kargs/unknown_unknown_contexttagabove14sample-1.pcap` | ContextTagAbove14Sample_1.pcap | D | 2 |  |
| `kargs/unknown_unknown_criticalroom55-1.cap` | CriticalRoom55-1.cap | C | 26 |  |
| `kargs/unknown_unknown_criticalroom55-2.cap` | CriticalRoom55-2.cap | C | 280 |  |
| `kargs/unknown_wattstopper-weblink_dri-cave-log-udp-0168-20081216-1117-03.cap` | DRI CAVE log udp-0168-20081216-1117-03.cap | A | 240 |  |
| `` | EastKentuckyExpo-WP1.txt | - | - | not committed (not a pcap) |
| `` | EastKentuckyExpo-WP2.txt | - | - | not committed (not a pcap) |
| `kargs/unknown_unknown_ethereal-misinterpreted-packet.cap` | Ethereal-Misinterpreted-Packet.cap | D | 1 |  |
| `kargs/unknown_unknown_i-am-router-to-network.cap` | I-Am-Router-To-Network.cap | C | 10 |  |
| `` | LGE Test.CBL | - | - | not committed (not a pcap) |
| `` | LGE-LITH.CAP | - | - | not committed (not a pcap) |
| `kargs-non-ip/unknown_unknown_mstp-malformed-packets.pcap` | MSTP_Malformed_Packets.pcap | D | 0 |  |
| `kargs/unknown_unknown_npdu.cap` | NPDU.cap | C | 36 |  |
| `kargs/unknown_bacnet-stack_privatetransfererror-octetstring.cap` | PrivateTransferError-octetstring.cap | C | 5 |  |
| `kargs/unknown_unknown_privatetransfererror.cap` | PrivateTransferError.cap | C | 1 |  |
| `kargs/unknown_unknown_rpm-all-allobjecttypes1.pcap` | RPM_ALL_Allobjecttypes1.pcap | C | 64 |  |
| `kargs/unknown_unknown_readpropertymultiple.cap` | ReadPropertyMultiple.cap | C | 4 |  |
| `kargs/unknown_bacnet-stack-demo_readpropertymultipledeviceall.cap` | ReadPropertyMultipleDeviceAll.cap | D | 2 |  |
| `kargs/unknown_unknown_subordinate-list.pcap` | Subordinate List.pcap | C | 2 |  |
| `kargs/unknown_unknown_subordinate-list2.pcap` | Subordinate List2.pcap | C | 4 |  |
| `kargs/unknown_unknown_subordinatelistdecodesample.pcap` | SubordinateListDecodeSample.pcap | C | 2 |  |
| `kargs/unknown_lithonia_synergyblinkwarn.cap` | SynergyBlinkWarn.cap | B | 42 |  |
| `kargs/unknown_unknown_synergyreadproperties.cap` | SynergyReadProperties.cap | C | 54 |  |
| `kargs/unknown_lithonia_synergywriteproperty.cap` | SynergyWriteProperty.cap | B | 42 |  |
| `kargs/unknown_wattstopper_sysco-1.cap` | Sysco-1.cap | B | 3492 |  |
| `kargs/unknown_wattstopper_sysco-2.cap` | Sysco-2.cap | B | 73 |  |
| `kargs/unknown_wattstopper_sysco-3.cap` | Sysco-3.cap | B | 60 |  |
| `kargs-non-ip/unknown_unknown_ta02-mst.cap` | TA02 MST.cap | C | 0 |  |
| `kargs-non-ip/unknown_unknown_tao2-tes-39a.cap` | TAO2 TES.39A.cap | C | 0 |  |
| `kargs/unknown_unknown_tc51103-btl-9-21-1-x3-bool-ext-3.pcap` | TC51103_BTL-9.21.1.X3_bool_ext_3.pcap | C | 14 |  |
| `kargs/unknown_cornell_tc51103-btl-9-21-1-x3-int-ext-1.pcap` | TC51103_BTL-9.21.1.X3_int_ext_1.pcap | B | 18 |  |
| `kargs/unknown_reliable-controls-ether-link_testrun4-internal-side-of-router.pcap` | TestRun4 - Internal side of Router.pcap | A | 235 |  |
| `kargs/unknown_reliable-controls-ether-link_testrun4-outside-of-router.pcap` | TestRun4 - Outside of Router.pcap | A | 294 |  |
| `kargs/unknown_reliable-controls-mach2_testrun5-internal-side-of-router.pcap` | TestRun5 - Internal side of Router.pcap | A | 128 |  |
| `kargs/unknown_reliable-controls-mach-prosys_testrun5-outside-of-router.pcap` | TestRun5 - Outside of Router.pcap | A | 149 |  |
| `kargs/unknown_reliable-controls-ether-link_testrun8-internal-side-of-router.pcap` | TestRun8 - Internal side of Router.pcap | A | 458 |  |
| `kargs/unknown_reliable-controls-mach-prosys_testrun8-outside-of-router.pcap` | TestRun8 - Outside of Router.pcap | A | 533 |  |
| `kargs/unknown_unknown_timesync-decode-noon.pcap` | TimeSync_Decode_Noon.pcap | C | 1 |  |
| `` | Tower333 Lighting 5min MSTP.ncf | - | - | not committed (not a pcap) |
| `kargs/unknown_wattstopper_tower333-lighting-5min-ip.pcap` | Tower333 lighting 5min IP.pcap | B | 8304 |  |
| `kargs/unknown_unknown_trendlogmultiplereadrange.pcap` | TrendLogMultipleReadRange.pcap | C | 5 |  |
| `kargs/unknown_unknown_trendlogmultiplereadrange2.pcap` | TrendLogMultipleReadRange2.pcap | C | 2 |  |
| `kargs/unknown_polarsoft_trendlogmultiplereadrange3.pcap` | TrendLogMultipleReadRange3.pcap | B | 15 |  |
| `kargs/unknown_automated-logic_trendlogmultiplereadrangesimple.pcap` | TrendLogMultipleReadRangeSimple.pcap | B | 807 |  |
| `kargs/unknown_polarsoft_trendlogmultipleusage.pcap` | TrendLogMultipleUsage.pcap | B | 71 |  |
| `kargs/unknown_unknown_whoisroutertonetwork-test.cap` | WhoIsRouterToNetwork-test.cap | C | 10 |  |
| `kargs/unknown_unknown_whoisroutertonetwork.cap` | WhoIsRouterToNetwork.cap | C | 10 |  |
| `kargs/unknown_alerton-vlc-16160c3_whois-i-am-epics.cap` | WhoIs_I-Am_Epics.cap | A | 96 |  |
| `kargs/unknown_unknown_wiresharkerror-arrayindex.pcap` | WireSharkError_ArrayIndex.pcap | C | 2 |  |
| `kargs/unknown_unknown_wiresharkerror-bufferreadynotification.pcap` | WireSharkError_BufferReadyNotification.pcap | C | 1 |  |
| `kargs/unknown_ashrae_wiresharkofnewobjects.pcap` | WireSharkOfNewObjects.pcap | C | 163 |  |
| `kargs/unknown_bacnet-stack_writeproperty-binaryoutput.cap` | WriteProperty_BinaryOutput.cap | B | 44 |  |
| `kargs/unknown_unknown_action-list.pcap` | action-list.pcap | C | 2 |  |
| `kargs-non-ip/unknown_unknown_aha-220-to-20-lost-b.cap` | aha_220_to_20_lost_b.cap | C | 0 |  |
| `kargs/unknown_lithonia-spak-mstp_alerton-plugfest-2.cap` | alerton-plugfest-2.cap | A | 37633 |  |
| `kargs/unknown_lithonia-spak-mstp_alerton-plugfest-3.cap` | alerton-plugfest-3.cap | A | 2379 |  |
| `kargs/unknown_lithonia-sysc-mlx_alerton-plugfest.cap` | alerton-plugfest.cap | A | 255355 | cache (over 50 MB) |
| `kargs/unknown_unknown_arf-empty-file.cap` | arf-empty-file.cap | C | 2 |  |
| `kargs/unknown_unknown_atomic-empty-file.cap` | atomic-empty-file.cap | C | 4 |  |
| `kargs/unknown_ashrae_atomic-read-file-1470.cap` | atomic-read-file-1470.cap | C | 63 |  |
| `kargs/unknown_ashrae_atomic-read-file-480.cap` | atomic-read-file-480.cap | B | 59 |  |
| `kargs/unknown_ashrae_atomic-read-file-50.cap` | atomic-read-file-50.cap | B | 859 |  |
| `kargs/unknown_ashrae_atomic-read-file-50x1500k.cap` | atomic-read-file-50x1500k.cap | C | 100632 |  |
| `kargs/unknown_unknown_atomic-read-file.cap` | atomic-read-file.cap | C | 128 |  |
| `kargs/unknown_lithonia_atomic-write-file-2.cap` | atomic-write-file-2.cap | B | 3118 |  |
| `kargs/unknown_lithonia_atomic-write-file-3.cap` | atomic-write-file-3.cap | B | 69 |  |
| `kargs/unknown_ashrae_atomic-write-file-480.cap` | atomic-write-file-480.cap | B | 86 |  |
| `kargs/unknown_ashrae_atomic-write-file-50x1k.cap` | atomic-write-file-50x1k.cap | C | 237 |  |
| `kargs/unknown_lithonia_atomic-write-file-seg.cap` | atomic-write-file-seg.cap | B | 574 |  |
| `kargs/unknown_lithonia_atomic-write-file.cap` | atomic-write-file.cap | B | 3119 |  |
| `kargs/unknown_ashrae_atomic-write-file-bad-ack.cap` | atomic_write_file_bad_ack.cap | C | 8 |  |
| `kargs/unknown_alerton_bacapp-malform.cap` | bacapp-malform.cap | D | 1673 |  |
| `kargs-non-ip/unknown_unknown_bacnet-arcnet.cap` | bacnet-arcnet.cap | C | 0 |  |
| `kargs/unknown_ashrae_bacnet-ethernet-device.cap` | bacnet-ethernet-device.cap | B | 9 |  |
| `kargs/unknown_lithonia_bacnet-ethernet.cap` | bacnet-ethernet.cap | B | 8 |  |
| `kargs/unknown_bacnet-stack-demo_bacnet-ip.cap` | bacnet-ip.cap | B | 833 |  |
| `kargs/unknown_lithonia_bacnet-properties.cap` | bacnet-properties.cap | B | 2 |  |
| `kargs/unknown_lithonia-sysc-mlx_bacnet-services.cap` | bacnet-services.cap | A | 7049 |  |
| `kargs/unknown_lithonia_bacnet-stack-services.cap` | bacnet-stack-services.cap | B | 87 |  |
| `kargs/unknown_bacnet-stack-demo_bacrpm-test.cap` | bacrpm-test.cap | D | 112 |  |
| `kargs-non-ip/unknown_unknown_bad-hub-restart.cap` | bad_hub_restart.cap | C | 0 |  |
| `kargs/unknown_lithonia_bip-discover.cap` | bip-discover.cap | B | 178 |  |
| `kargs/unknown_critical-room-control-room-pressure-monitor_bip-readprop-2.cap` | bip-readprop-2.cap | A | 14138 |  |
| `kargs/unknown_critical-room-control-room-pressure-monitor_bip-readprop.cap` | bip-readprop.cap | A | 4631 |  |
| `kargs/unknown_critical-room-control-room-pressure-monitor_bip-readwrite-test.cap` | bip-readwrite-test.cap | A | 25626 |  |
| `kargs/unknown_unknown_bo-command-failure.pcap` | bo_command_failure.pcap | C | 1 |  |
| `kargs/unknown_unknown_bo-command-failure-original.pcap` | bo_command_failure_original.pcap | C | 8 |  |
| `kargs/unknown_lithonia-sysc-mlx_btl-plugfest.cap` | btl-plugfest.cap | A | 645 |  |
| `kargs/unknown_contemporary-controls-basrt-b_bvlc-bac4-rp.cap` | bvlc-bac4-rp.cap | A | 126 |  |
| `kargs/unknown_bacnet-stack-demo_bvlc-bac4.cap` | bvlc-bac4.cap | B | 63 |  |
| `kargs/unknown_bacnet-stack-demo_bvlc-fdreg-readprop-47809.cap` | bvlc-fdreg-readprop-47809.cap | B | 337 |  |
| `kargs/unknown_bacnet-stack_bvlc-loop.cap` | bvlc-loop.cap | C | 110 |  |
| `kargs-non-ip/unknown_unknown_bvlc.pcap` | bvlc.pcap | C | 0 |  |
| `kargs-non-ip/unknown_unknown_cimetrics-mstp.pcap` | cimetrics_mstp.pcap | C | 0 |  |
| `kargs/unknown_lithonia_config-tool-discover.cap` | config-tool-discover.cap | B | 10 |  |
| `kargs/unknown_unknown_confirmedeventnotification.cap` | confirmedEventNotification.cap | C | 10 |  |
| `kargs/unknown_wattstopper_cov-testing-1.pcap` | cov-testing-1.pcap | B | 285 |  |
| `kargs/unknown_alerton_cov-testing-2.pcap` | cov-testing-2.pcap | B | 270 |  |
| `kargs/unknown_wattstopper_cov-testing-3.pcap` | cov-testing-3.pcap | B | 2388 |  |
| `kargs/unknown_bacnet-stack_device-address-binding.pcap` | device-address-binding.pcap | C | 4 |  |
| `kargs/unknown_bacnet-stack-demo_epics-1.cap` | epics-1.cap | B | 497 |  |
| `kargs/unknown_lithonia-sysc-mlx_epics-2.cap` | epics-2.cap | A | 1957 |  |
| `kargs/unknown_unknown_eventlog-readrange.pcap` | eventLog_ReadRange.pcap | C | 2 |  |
| `kargs/unknown_unknown_eventlog-rpm.pcap` | eventLog_rpm.pcap | C | 2 |  |
| `kargs/unknown_unknown_eventtimestamp-rp.pcap` | eventTimeStamp_rp.pcap | C | 2 |  |
| `kargs/unknown_unknown_eventtimestamp-rpm.pcap` | eventTimeStamp_rpm.pcap | C | 2 |  |
| `kargs/unknown_bacnet-stack_foreign-device-npdu.cap` | foreign-device-npdu.cap | C | 126 |  |
| `kargs/unknown_unknown_geteventinformation.pcap` | getEventInformation.pcap | C | 2 |  |
| `kargs/unknown_wattstopper_i-am-vendor-id-over-255.cap` | i-am-vendor-id-over-255.cap | B | 11 |  |
| `kargs-non-ip/unknown_unknown_lmbc-300-bootload.pcap` | lmbc-300-bootload.pcap | C | 0 |  |
| `kargs/unknown_unknown_load-control-properties.cap` | load-control-properties.cap | C | 2 |  |
| `kargs/unknown_bacnet-stack-demo_load-control.cap` | load-control.cap | D | 6 |  |
| `kargs/unknown_unknown_log-buffer-readrange.pcap` | log-buffer_readRange.pcap | C | 2 |  |
| `kargs/unknown_unknown_loop2.cap` | loop2.cap | C | 2 |  |
| `kargs-non-ip/unknown_unknown_mstp-cimetrics.cap` | mstp-cimetrics.cap | C | 0 |  |
| `kargs-non-ip/unknown_unknown_mstp-test-4.cap` | mstp-test-4.cap | C | 0 |  |
| `kargs/unknown_bacnet-stack_mstp-whois-basrt-mix.cap` | mstp-whois-basrt-mix.cap | B | 22 |  |
| `kargs/unknown_bacnet-stack_mstp-whois-basrt-mix2.cap` | mstp-whois-basrt-mix2.cap | B | 22 |  |
| `kargs/unknown_alerton_mstp-whois-iam.cap` | mstp-whois-iam.cap | B | 19 |  |
| `kargs-non-ip/unknown_unknown_mstp-20090227094623.cap` | mstp_20090227094623.cap | C | 0 |  |
| `kargs-non-ip/unknown_unknown_mstp-20090304105820.cap` | mstp_20090304105820.cap | C | 0 |  |
| `kargs-non-ip/unknown_unknown_mstp-20090304110410.cap` | mstp_20090304110410.cap | C | 0 |  |
| `kargs-non-ip/unknown_unknown_mstp-20090807145500.cap` | mstp_20090807145500.cap | C | 0 |  |
| `kargs-non-ip/unknown_unknown_mstp-20091013121352.cap` | mstp_20091013121352.cap | C | 0 |  |
| `kargs-non-ip/unknown_unknown_mstp-20091013121410.cap` | mstp_20091013121410.cap | C | 0 |  |
| `kargs-non-ip/unknown_unknown_mstp-20091013122053.cap` | mstp_20091013122053.cap | C | 0 |  |
| `kargs-non-ip/unknown_unknown_mstp-20091013122451.cap` | mstp_20091013122451.cap | C | 0 |  |
| `kargs-non-ip/unknown_unknown_mstp-20091013123108.cap` | mstp_20091013123108.cap | C | 0 |  |
| `kargs-non-ip/unknown_unknown_mstp-20091013124218.cap` | mstp_20091013124218.cap | C | 0 |  |
| `kargs-non-ip/unknown_unknown_mstp-20091013130259.cap` | mstp_20091013130259.cap | C | 0 |  |
| `kargs-non-ip/unknown_unknown_mstp-20091013162906.cap` | mstp_20091013162906.cap | C | 0 |  |
| `kargs-non-ip/unknown_unknown_mstp-20091014093519.cap` | mstp_20091014093519.cap | C | 0 |  |
| `kargs-non-ip/unknown_unknown_mstp-20091014112427.cap` | mstp_20091014112427.cap | C | 0 |  |
| `kargs-non-ip/unknown_unknown_mstp-20140225214217.cap` | mstp_20140225214217.cap | C | 0 |  |
| `kargs/unknown_wattstopper-li-nb_mstp-mix-basrt-v124.cap` | mstp_mix_basrt_V124.cap | A | 529 |  |
| `kargs/unknown_wattstopper-li-nb_mstp-mix-basrt-v124-bad.cap` | mstp_mix_basrt_V124_bad.cap | D | 421 |  |
| `kargs-non-ip/unknown_unknown_mstp-wtap.cap` | mstp_wtap.cap | C | 0 |  |
| `kargs/unknown_wattstopper_nb-binary-output.cap` | nb-binary-output.cap | B | 244 |  |
| `kargs-non-ip/unknown_unknown_plugfest-2011-delta-1.pcap` | plugfest-2011-delta-1.pcap | C | 0 |  |
| `kargs-non-ip/unknown_unknown_plugfest-2011-delta-2.pcap` | plugfest-2011-delta-2.pcap | C | 0 |  |
| `kargs-non-ip/unknown_unknown_plugfest-2011-delta-3.pcap` | plugfest-2011-delta-3.pcap | C | 0 |  |
| `kargs/unknown_wattstopper-lmbc-300_plugfest-2011-mstp-roundtable.pcap` | plugfest-2011-mstp-roundtable.pcap | A | 1170 |  |
| `kargs/unknown_wattstopper-lmbc-300_plugfest-2011-sauter-1.pcap` | plugfest-2011-sauter-1.pcap | A | 1360 |  |
| `kargs/unknown_wattstopper-lmbc-300_plugfest-2011-siemens-1.pcap` | plugfest-2011-siemens-1.pcap | A | 5263 |  |
| `kargs/unknown_trane-uc400_plugfest-2011-trane-1.pcap` | plugfest-2011-trane-1.pcap | A | 2260 |  |
| `kargs/unknown_lithonia-spak-mstp_plugfest-delta-2.cap` | plugfest-delta-2.cap | A | 14246 |  |
| `kargs/unknown_lithonia-spak-mstp_plugfest-delta-2b.cap` | plugfest-delta-2b.cap | A | 10628 |  |
| `kargs/unknown_wattstopper-lmbc-300_plugfest-tridium-1.pcap` | plugfest-tridium-1.pcap | A | 3583 |  |
| `kargs/unknown_polarsoft-9260-linux-server_plugfest-tridium-2.pcap` | plugfest-tridium-2.pcap | A | 644 |  |
| `kargs/unknown_unknown_polarsoft-free-range-router-init-routing-table.cap` | polarsoft-free-range-router-init-routing-table.cap | C | 12 |  |
| `kargs/unknown_critical-room-control-room-pressure-monitor_polarsoft-free-range-router.cap` | polarsoft-free-range-router.cap | A | 219 |  |
| `kargs/unknown_lithonia-sysc-mlx_properties.cap` | properties.cap | A | 488 |  |
| `kargs/unknown_unknown_read-file.cap` | read-file.cap | C | 1327 |  |
| `kargs/unknown_lithonia-sysc-mlx_read-properties.cap` | read-properties.cap | A | 117 |  |
| `kargs-non-ip/unknown_unknown_read-property-bad.cap` | read-property-bad.cap | C | 0 |  |
| `kargs/unknown_lithonia-sysc-mlx_read-property-epics.cap` | read-property-epics.cap | A | 2882 |  |
| `kargs-non-ip/unknown_unknown_read-property-synergy.cap` | read-property-synergy.cap | C | 0 |  |
| `kargs-non-ip/unknown_unknown_read-property.cap` | read-property.cap | C | 0 |  |
| `kargs/unknown_ashrae_readfile.cap` | readfile.cap | B | 349 |  |
| `kargs/unknown_unknown_readrange-malformed.cap` | readrange_malformed.cap | D | 1 |  |
| `kargs/unknown_lithonia_reinit-device.cap` | reinit-device.cap | B | 282 |  |
| `kargs/unknown_unknown_router.cap` | router.cap | C | 26 |  |
| `kargs/unknown_unknown_routers.cap` | routers.cap | C | 18 |  |
| `kargs/unknown_wattstopper-li-nb-24_rp-device.cap` | rp-device.cap | A | 90 |  |
| `kargs/unknown_unknown_rp-shed-level.cap` | rp-shed-level.cap | C | 2 |  |
| `kargs/unknown_lithonia_rp.cap` | rp.cap | B | 10 |  |
| `kargs/unknown_critical-room-control_rpm-error.cap` | rpm-error.cap | B | 87 |  |
| `kargs/unknown_lithonia-sysc-mlx_rpm.cap` | rpm.cap | A | 4 |  |
| `kargs/unknown_unknown_rpm-multiple-scheduler-bug.pcap` | rpm_multiple_scheduler_bug.pcap | C | 2 |  |
| `kargs/unknown_schneider-electric_schedule-rpm-foreign.pcap` | schedule-rpm-foreign.pcap | B | 30 |  |
| `kargs/unknown_unknown_signed-value-negative.cap` | signed_value_negative.cap | C | 2 |  |
| `kargs/unknown_unknown_single-rpm.pcap` | single-RPM.pcap | C | 2 |  |
| `kargs/unknown_lithonia-sysc-mlx_softdel-btl.cap` | softdel-BTL.cap | A | 293726 | cache (over 50 MB) |
| `kargs/unknown_unknown_special-events.pcap` | special-events.pcap | C | 3 |  |
| `kargs/unknown_lithonia_startup-exchange.cap` | startup-exchange.cap | B | 9 |  |
| `kargs/unknown_wattstopper_state-text.cap` | state_text.cap | B | 28 |  |
| `kargs/unknown_wattstopper_state-text-good.cap` | state_text_good.cap | B | 124 |  |
| `kargs/unknown_unknown_subordinatelist-rpm.pcap` | subordinatelist_rpm.pcap | C | 2 |  |
| `kargs/unknown_lithonia_synergy-binding-2x63y.cap` | synergy-binding-2x63y.cap | B | 38 |  |
| `kargs/unknown_lithonia-sysc-mlx_synergy-broken-rpm.cap` | synergy-broken-rpm.cap | A | 54 |  |
| `kargs/unknown_lithonia-sysc-mlx_synergy-device.cap` | synergy-device.cap | A | 438 |  |
| `` | tac-plugfest-epics.txt | - | - | not committed (not a pcap) |
| `kargs/unknown_unknown_time-sync.cap` | time-sync.cap | C | 1 |  |
| `kargs/unknown_unknown_tridium-jace2.pcap` | tridium jace2.pcap | C | 1437 |  |
| `kargs-non-ip/unknown_unknown_u-4-mstp.pcap` | u+4_MSTP.pcap | C | 0 |  |
| `kargs/unknown_lithonia_weekend.cap` | weekend.cap | B | 1450 |  |
| `kargs/unknown_lithonia_who-has-i-have.cap` | who-has-I-have.cap | B | 111 |  |
| `kargs/unknown_lithonia_who-has.cap` | who-has.cap | B | 69 |  |
| `kargs/unknown_lithonia_who-is-i-am.cap` | who-is-i-am.cap | B | 10 |  |
| `kargs-non-ip/unknown_unknown_whois-basrtp-b-1.cap` | whois-basrtp-b-1.cap | C | 0 |  |
| `kargs-non-ip/unknown_unknown_whois-basrtp-b-2.cap` | whois-basrtp-b-2.cap | C | 0 |  |
| `kargs/unknown_lithonia_whois-iam.cap` | whois-iam.cap | B | 6 |  |
| `kargs/unknown_unknown_wireshark-bbmderror.pcap` | wireshark_BBMDError.pcap | C | 8 |  |
| `kargs/unknown_unknown_wireshark-cen-9-11.pcap` | wireshark_CEN_9_11.pcap | C | 8 |  |
| `kargs/unknown_ashrae_wp-rp-index.cap` | wp-rp-index.cap | B | 191 |  |
| `kargs/unknown_unknown_wp-weekly-schedule-index.cap` | wp-weekly-schedule-index.cap | C | 13 |  |
| `kargs/unknown_unknown_wp-weekly-schedule-test.cap` | wp-weekly-schedule-test.cap | C | 28 |  |
| `kargs/unknown_unknown_wp-weekly-schedule.cap` | wp_weekly_schedule.cap | C | 20 |  |
| `kargs/unknown_bacnet-stack_write-property-array.cap` | write-property-array.cap | C | 50 |  |
| `kargs/unknown_unknown_write-property-multiple.cap` | write-property-multiple.cap | C | 2 |  |
| `kargs/unknown_wattstopper_write-property-wattstopper-panel.cap` | write-property-wattstopper-panel.cap | B | 30 |  |
| `kargs/unknown_lithonia_write-property.cap` | write-property.cap | B | 54 |  |
| `kargs/unknown_lithonia_write-property2.cap` | write-property2.cap | B | 18 |  |
