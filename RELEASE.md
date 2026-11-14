# go-bacnet Releases

## v0.9.0

**Date:** 2026-11-14
**Previous release:** v0.8.0

Requires **Go 1.26+**. Interop pin unchanged: [`bacnet-interop` v0.14.0](https://github.com/otfabric/bacnet-interop/releases/tag/v0.14.0). This release tightens the server's and the client's bounds on what a peer can make them build, keep or send, extends `Hooks.BeforeWrite` to two more write paths, and changes how the BBMD passes on Forwarded-NPDUs. The new server options are additive; the client changes are visible where noted.

### Added

- `server.WithCOVReferenceLimit(n)` (default 4096) bounds the monitored references across all COV subscriptions; beyond it a subscription is refused with resources / no-space-to-add-list-element, as one beyond `WithCOVLimits` is.
- `server.WithSegmentLimits(server.SegmentLimits{…})` bounds segmented requests being reassembled (64 in total, 16 per origin, 4 MiB buffered) and segmented replies being sent (32 in total, 8 per origin); beyond them the request is answered with Abort out-of-resources ([ERRORS.md](ERRORS.md#rejects-and-aborts)).
- `object.WriteGuard`: an optional interface of the `Runtime` bound to a Device; the server's implements it with `Hooks.BeforeWrite`.

### Changed

- Server: a datagram whose immediate peer is not a valid IPv4 endpoint is discarded before BVLC handling, with a `bvlc` diagnostic, with or without a BBMD. Before, a Forwarded-NPDU from such a peer was served and the BVLC NAK paths answered it.
- Server: `Hooks.BeforeWrite` is now called for AtomicWriteFile (a write of the File's File_Size whose value carries the start position and the data) and for every write a Schedule makes to the objects in its List_Of_Object_Property_References, for the referenced object. An application whose hook refuses a property now also refuses it when a Schedule writes it.
- Server: one SubscribeCOVPropertyMultiple holds at most as many references as fit one notification in the server's Max APDU (58 at 1476); beyond it the request gets the error naming the first reference over the cap. A COV notification larger than one APDU is not sent and is reported as a `cov` diagnostic.
- Server: ReadPropertyMultiple and Active_COV_Subscriptions are built against the requester's reply budget and stop with the same Abort as before as soon as it is exceeded; GetEventInformation finds the largest list that fits by binary search and honours the request deadline.
- Server: a segmented reply is sent from its own goroutine, so it no longer holds a request worker, and has an overall deadline after which the requester gets Abort tsm-timeout. `Close` waits for it.
- Server: a confirmed COV notification still pending when its subscription is replaced, cancelled or expires is cancelled, so its invoke ID is released.
- BBMD: a Forwarded-NPDU is passed on (to the local network and the FDT) only when it comes from a BDT peer; from any other node it is delivered locally only. The FDT distribution skips the sender and BDT peers. Register-Foreign-Device from a BDT peer or from the BBMD itself is answered with a NAK. A Forwarded-NPDU carrying a confirmed request is discarded, as an Original-Broadcast-NPDU already was.
- Client: a packet whose immediate peer is not a valid IPv4 endpoint is dropped right after BVLC parsing, with a `bvlc` diagnostic, and the source-path match of a pending request fails closed for it.
- Client: `ReadRangeAll`'s `MaxItems` counts the items kept, not the records the device claims: a page counts as the larger of its ItemCount and the items decoded, and a page beyond the budget is cut (`Partial`, `StoppedReason "MaxItems"`). When a page of typed log records is cut, its flat `ItemData` is nil.
- Client: `ReadPropertyMultipleBatched` returns `ErrProtocolViolation` when a result's properties are not the requested references, in order; a reference to ALL, REQUIRED or OPTIONAL may expand to several. `ReadObjectList` never returns more than `maxItems` entries, and `device.ReadSnapshot` keeps only the requested properties of the requested objects.
- Client: the device and object registries keep a recency list, so expiry and eviction no longer scan every entry on each observation.

### Fixed

- Server: AtomicWriteFile with a stream start position near the 32-bit limit, and a byTime ReadRange of a Trend Log with a very large count, computed their bounds in `int` and could go wrong on 32-bit platforms; both use 64-bit arithmetic.
- Server: the stored segmented-request header no longer keeps the request payload.

### Tests

- `server`: `bounds_internal_test.go` (COV reference bounds and notification size, Active_COV_Subscriptions, ReadPropertyMultiple budget, AtomicWriteFile and Schedule hooks, GetEventInformation, reassembly and reply limits, segmented reply off the worker), `bbmd_peer_test.go` (non-IPv4 peers, two server BBMDs forward once).
- `bbmd`: `peer_test.go` (non-IPv4 peers, Forwarded-NPDU from a non-peer, BDT peers as foreign devices, no loop between peers).
- `server/object`: `bounds32_test.go` (File and Trend Log bounds at the 32-bit limits).
- `client`: `peer_bounds_test.go` (registry maintenance independent of size, non-IPv4 immediate peers, ReadRangeAll budget, RPM result matching, ReadObjectList cap); `TestReadRangeAllPagesUntilLastItem` now counts items. `device`: `snapshot_bounds_test.go`.

---

## v0.8.0

**Date:** 2026-11-08
**Previous release:** v0.7.0

Requires **Go 1.26+**. Interop pin: [`bacnet-interop` v0.14.0](https://github.com/otfabric/bacnet-interop/releases/tag/v0.14.0), which adds three independent peers, each as a device and as a client: ulbios/bacnet (commit `227d6227`), legacy BACpypes 0.19.0 and @bacnet-js/client 3.3.2. The go-bacnet client runs against their devices and they run as clients against the go-bacnet server ([INTEROP.md](INTEROP.md)). The legacy BACpypes runs found one client defect, fixed; a second, in foreign-device BVLC-Result routing, showed up as an intermittent failure of go-bacnet's own end-to-end `TestServerBBMDTables` while the BBMD scenarios were wired. Not a conformance claim.

### Changed

- The client's default decode limits are `bacnet.DefaultDecodeLimits` with `MaxAPDUSize` raised to `MaxDatagramSize`, as the server's are, so it receives an APDU longer than the Max APDU it advertises. It still advertises at most 1476 octets (code 5, the largest encodable size, clause 20.1.2.5); `WithDecodeLimits` with a `MaxAPDUSize` keeps a strict bound and advertises it. Legacy BACpypes sends segments of Max APDU octets of service data plus the 5-octet header (1481 octets for a 1476-octet requester); the client dropped them and the read timed out.

### Fixed

- A foreign device that read its BBMD's BDT or FDT while its registration was pending gave the registration's successful-completion BVLC-Result (Annex J.2.1.1) to the read, which returned no entries, and the registration never completed. A read is answered by its ACK (Annex J.2.4, J.2.8), and Register-Foreign-Device-NAK and Distribute-Broadcast-To-Network-NAK never answer a table operation, so these Results now go to the registration ([API.md](API.md#foreign-device--forwarded-npdu)).

### Tests

- `interop`: `ulbios_test.go`, `bacpypes_test.go` and `bacnetjs_test.go` drive the three peer devices (`ULBIOS_IMAGE`, `BACPYPES_IMAGE`, `BACNETJS_IMAGE`; `none` skips); the cross-peer tables of `client_helpers_test.go` include bacnet-js and, for what it serves, ulbios. `TestUlbiosClientAgainstServer`, `TestBACpypesClientAgainstServer` and `TestBacnetJSClientAgainstServer` run the four scenario files with each new client against the server. Recorded peer behaviour is asserted before a test is skipped, so a change in it is noticed.
- `client`: `TestSegmentLongerThanAdvertisedMaxAPDU`, `TestDefaultDecodeLimitsAndAdvertisedMaxAPDU` and `bvlc_fd_result_test.go` on the virtual transport.
- `make interop-pinned` and the interop workflow pass the three new images from the pin.

### Interoperability

Recorded peer behaviour ([docs/COMPATIBILITY.md](docs/COMPATIBILITY.md), [INTEROP.md](INTEROP.md)): ulbios does not answer what it cannot decode (no Reject or Abort encoder) and encodes Unsigned in two octets; legacy BACpypes rejects a value of the wrong datatype with Reject invalid-tag, answers FDT remaining time TTL + 5 and Write-BDT with NAK 0x0010; bacnet-js encodes a GetAlarmSummary ACK with context tags, which go-bacnet refuses, and cannot send a segmented answer.

---

## v0.7.0

**Date:** 2026-11-08
**Previous release:** v0.6.0

Requires **Go 1.26+**. Interop pin unchanged: [`bacnet-interop` v0.12.0](https://github.com/otfabric/bacnet-interop/releases/tag/v0.12.0). go-bacnet is now tested against recorded traffic of physical BACnet devices: 234 captures from [kargs.net/captures](https://kargs.net/captures/) and [EmreEkin/ICS-Pcaps](https://github.com/EmreEkin/ICS-Pcaps), committed byte-identical under clean names (two over 50 MB fetched on demand), 38 of them identifying the devices by model and firmware (Trane Tracer SC, UC400 and BSV; Tridium NiagaraAX; Reliable Controls MACH-ProSys, ETHER-Link and MACH2; Delta Controls DSC-1616E, DNT-T103 and DNT-T221; Alerton BCM-Eth and VLC-853; WattStopper LMBC-300; Lithonia SYSC MLX; Contemporary Controls BASRT-B and others). Every suite runs over every committed capture in `make test`, and 20 golden cases compare 1,072 datagrams field by field with Wireshark ([testdata/captures](testdata/captures/README.md)). They found three defects, all fixed. No live device was tested; nothing here is a conformance claim.

### Fixed

- `service.DecodeReadRangeACK` decoded every Log_Buffer as BACnetLogRecord. It refused a Trend Log Multiple's BACnetLogMultipleRecords and an Event Log's BACnetEventLogRecords ("not context boolean"), so `client.ReadRange` and `ReadRangeAll` failed on both, and the client aborted a segmented answer. Recorded: a PolarSoft device's Trend Log Multiple and an Automated Logic device's Event Log.
- The server acknowledged ReinitializeDevice START_BACKUP to ABORT_RESTORE without a `Hooks.Reinitialize`, though it has no backup or restore procedure. Without a hook those states now get services/service-request-denied, as a recorded WattStopper LMBC-300 answers (the recorded PolarSoft server and a Lithonia device refuse them with a Reject; a Delta DAC-T305, which supports backup, acknowledges). COLDSTART, WARMSTART and ACTIVATE_CHANGES are still acknowledged, and a hook still decides every state.

- `service.EncodeEventNotification` dropped the `parameters [2]` list of an extended [9] notification when it was empty; it is not OPTIONAL (clause 13.3). Recorded: an Automated Logic (vendor 24) notification in `CEN_9_11.pcap`.

### Added

- `service.ReadRangeACK.LogMultipleRecords` (`service.LogMultipleRecord`, `LogMultipleValue`) and `EventLogRecords` (`service.EventLogRecord`), chosen by the object type of the answer; `LogDataChoice`; `DecodeLogMultipleRecords`, `DecodeEventLogRecords`, `EncodeLogMultipleRecord`, `EncodeEventLogRecord`. `EncodeReadRangeACK` encodes both, and `client.ReadRangeAll` collects them (`ReadRangePageResult.LogMultipleRecords`, `EventLogRecords`).
- `bacnet.ObjectTypeEventLog` (25).
- `testdata/captures`: the captures (`kargs/`, `icspcaps/`, and `*-non-ip/` for MS/TP, ARCNET and Ethernet), [README.md](testdata/captures/README.md) (sources, names, evidence classes, devices, differences, every file with its original name), [manifest.json](testdata/captures/manifest.json) (original name, URL, SHA256, class and its basis, devices, go-bacnet and Wireshark counts), SHA256SUMS, CACHED, the golden cases next to their captures and the machine-readable coverage report `report/`.
- `make captures-fetch` downloads the two captures over 50 MB into a git-ignored cache; `make captures-extended` runs the corpus suites over them too; `make captures-generate` and `make captures-report` rebuild the corpus and the report.

### Tests

- `internal/capture` reads pcap and pcapng (Ethernet, VLAN, Linux cooked, raw IPv4; IPv4 reassembly), decodes BVLC, NPDU, APDU and the services, reassembles segmented messages and correlates confirmed transactions by requester, responder and invoke ID. Every golden datagram decodes as Wireshark 4.6.9 decodes it, field by field, and encodes again to the recorded octets at every layer; go-bacnet refuses exactly what Wireshark marks malformed, apart from classified differences.
- `TestCorpus`, `TestCorpusReplay` and `TestCorpusServerParity` run over every committed capture: 286,597 datagrams decoded and re-encoded, 124,363 transactions replayed by the client (799 segmented), 10,163 server answers equal to the devices' and 4,006 classified differences.
- `client`: `TestCaptureReplay` runs the client against 254 recorded answers of the golden cases (12 segmented). The client's requests carry the recorded service octets, its SegmentACKs the recorded sequence numbers and windows, and it returns the recorded values and errors. `TestCaptureReplayDisrupted` replays segmented answers with segments duplicated, lost or swapped.
- `server`: `TestCaptureServerParity` gives the server the recorded requests whose answer does not depend on the device's objects; 150 answers equal the devices', the other 66 are classified. `TestCaptureBVLLResultParity` checks the recorded BVLC-Result NAK codes. `TestCaptureMutatedRequests` sends about 20,000 malformed variants of the recorded requests.
- `TestReinitializeDeviceWithoutHook`; the recorded Trend Log Multiple and Event Log answers as unit tests that decode and encode byte for byte; `FuzzDecodeLogMultipleRecords`.
- The BVLC, NPDU and APDU fuzz targets, `FuzzServerReceive`, `FuzzClientReceive` and ten service decoder fuzz targets are seeded with the recorded octets.

### Interoperability

Recorded differences, classified in [testdata/captures/README.md](testdata/captures/README.md#differences-that-remain) and [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md): a Lithonia SYSC MLX answers a ReadPropertyMultiple with an empty property list, and a request with the reserved max-APDU code 6, which go-bacnet refuses; older devices answer a password failure with class services, go-bacnet with class security; a plugfest router and a WattStopper WebLink forward malformed NPDUs or APDUs, which go-bacnet refuses.

---

## v0.6.0

**Date:** 2026-11-06
**Previous release:** v0.5.0

Requires **Go 1.26+**. Interop pin: [`bacnet-interop` v0.12.0](https://github.com/otfabric/bacnet-interop/releases/tag/v0.12.0). A BACnet/IP server, verified against the go-bacnet client and against three independent clients in bacnet-interop client-mode images: the bacnet-stack command-line tools, BACnet4J and BACpypes3. [docs/SERVER_SUPPORT.md](docs/SERVER_SUPPORT.md) lists each capability and its evidence.

### Added

- Package `server` serves one `object.Device` on a BACnet/IP endpoint. It answers ReadProperty, ReadPropertyMultiple, WriteProperty, WritePropertyMultiple, ReadRange, Who-Is and Who-Has, Add/RemoveListElement, CreateObject and DeleteObject, AtomicReadFile and AtomicWriteFile. It also answers SubscribeCOV, SubscribeCOVProperty and SubscribeCOVPropertyMultiple, AcknowledgeAlarm, GetAlarmSummary, GetEventInformation and GetEnrollmentSummary (all filters), DeviceCommunicationControl and ReinitializeDevice. Time synchronization, private transfer, text message and WriteGroup are served through `Hooks`.
- The server sends segmented replies and reassembles segmented requests. A request repeated while it executes is dropped; after the response, the same invoke ID is a new request (clause 5.4.5). Handlers run on a bounded worker pool, and a full queue is answered with Abort out-of-resources.
- Confirmed event notifications to a remote-station recipient go through the router learnt from received SNET and I-Am-Router-To-Network; the server sends Who-Is-Router-To-Network while none is known.
- Package `server/object` provides the Device object; analog, binary and multi-state inputs, outputs and values; Notification Class, Trend Log and File; Calendar, Schedule, CharacterString Value, Integer Value, Positive Integer Value and Structured View. Analog, binary, and multi-state input and value points support intrinsic reporting: out-of-range, change-of-state, change-of-reliability, and Fault_Values for multi-state points.
- AtomicWriteFile past the end of a file extends it: a stream file is zero-filled up to the start position, records are appended after the last one, within the file's maximum size.
- Package `bbmd` is an optional BBMD for a server. It keeps the BDT and FDT, registers foreign devices and forwards broadcasts.
- `service.DecodeReadPropertyMultiple` (the request), `service.EncodeCOVNotification`, `service.RecipientProcess`, `service.ErrUndefinedEnumeration` (wraps `ErrMalformed`; returned for a DeviceCommunicationControl or ReinitializeDevice enumeration outside the defined values), and the request decoders the server uses.
- Object type constants for Structured View (29), CharacterString Value (40), Integer Value (45) and Positive Integer Value (48); property identifier constants and names used by the server's objects; error codes inconsistent-configuration (129) and inconsistent-object-type (130); BVLC-Result code constants in `bvlc`.

### Changed

- The `Transport` interface, `InboundPacket`, `OutboundPacket` and the UDP transport move to package `bip` (`bip.Transport`, `bip.ListenUDP`). `client.Transport`, `client.InboundPacket` and `client.OutboundPacket` are type aliases of the `bip` types, so existing code compiles unchanged.
- **Breaking:** `service.GetEnrollmentSummaryRequest.EnrollmentFilter` is a `*service.RecipientProcess` (BACnetRecipientProcess, clause 13.11.1) instead of the unused `*service.EnrollmentFilter`. The client now encodes it.
- `service.DecodeDeviceCommunicationControl` and `DecodeReinitializeDevice` wrap `ErrUndefinedEnumeration` for an out-of-range enumeration. `errors.Is(err, bacnet.ErrMalformed)` still holds.
- The client's segmentation and B/IP framing moved to `internal/segment` and `internal/link`, shared with the server. Behaviour is unchanged apart from the fix below.

### Fixed

- After 256 received segments the client's SegmentACK named sequence number 0 instead of 255.
- The server accepts an APDU longer than its Max_APDU_Length_Accepted, up to the datagram limit, unless `WithDecodeLimits` sets `MaxAPDUSize`. BACpypes3 0.0.110 sends segments 6 octets longer than the device's Max APDU, which the server dropped.
- The first segment of a segmented message is acknowledged at once, whatever the window (clause 5.4.4 and 5.4.5). Before, the server answered a segmented request, and a client with `WithSegmentWindow` above 1 a segmented ComplexACK, only when the window filled, so a peer that waits for the SegmentACK of segment 0 (BACpypes3) timed out. Later windows count from segment 0.
- A segmented request from the client sends segment 0 alone and waits for its SegmentACK, which carries the actual window, before sending more (clause 5.4.4). Before, it sent a whole proposed window first.
- A change-of-state event notification whose new state is boolean-value (BACnetPropertyStates choice [0], clause 21) decoded as an enumerated value; it now decodes as a Boolean, and a content octet other than 0 or 1 is `ErrMalformed`.

### Tests

- `e2e/server*_test.go`: the go-bacnet client against the server over loopback UDP, including every built-in object type read with ReadPropertyMultiple ALL, device management and hook services, COV-property subscriptions, and the BBMD tables.
- `make dissector` also dissects every datagram the server sends in those tests (`TestDissectorAcceptsServerReplies`).
- `TestBACnetStackClientAgainstServer`, `TestBACnet4JClientAgainstServer` and `TestBACpypes3ClientAgainstServer` run each stack's client mode with `server-baseline-v1`, `server-services-v1`, `server-bbmd-v1` and (BACnet4J, BACpypes3) `server-extended-v1` against the server ([INTEROP.md](INTEROP.md#independent-clients-against-the-server)).
- Interop pin moved to bacnet-interop v0.12.0 (container contract 1.1, client mode).
- `examples/server`: a device with a setpoint, a drifting input and a fan, which prints every write.
- `TestChaosServer` covers concurrent requesters, retransmissions, junk and object changes under `-race`. It runs in `make chaos`.
- `FuzzServerReceive` and `FuzzHandleBVLC` run in `make fuzz` and nightly. `FuzzDecodeReadPropertyMultiple` is added to the nightly run.
- `server` and `bbmd` check for leaked goroutines in `TestMain`.

---

## v0.5.0

**Date:** 2026-11-04
**Previous release:** v0.4.1

Requires **Go 1.26+**. Interop pin: [`bacnet-interop` v0.11.3](https://github.com/otfabric/bacnet-interop/releases/tag/v0.11.3).

### Fixed

- Decoded values that do not fit their Go field are refused with `ErrMalformed` instead of keeping their low bits. Before, a VT session identifier of 256 became 0, a file start position of 2^31 became -2^31, a record count of 2^32+1 became 1, a message or write priority of 256 became 0, and an extended vendor identifier of 65536 became 0. ERRORS.md lists the fields.
- `device.ReadSnapshot` no longer keeps the low 32 bits of an oversized Protocol_Revision or Max_APDU_Length_Accepted. It also refuses a Vendor_Identifier above Unsigned16. Each case is reported in `PartialErrors`.
- `ReadFileStream` and `ReadFileRecords` return `ErrProtocolViolation` when the device reports a position or count whose next position wraps the signed 32-bit range.
- `WriteFileStream` with start -1 (end of file) wrote the second chunk at offset `n-1`. Later chunks now follow the position the device reported in its ACK. A write that would end past octet 2^31-1 is refused before anything is sent.
- Segmented requests: a Segment-ACK NAK that names a segment never sent ended the transfer, and the device's answer was then taken as success. Such a NAK now aborts with `ErrProtocolViolation`. A device that alternated ACK 0 and NAK 255 kept a request without a deadline running forever. NAKs that rewind behind acknowledged segments are now ignored, so the segment timer ends the request.
- `ReadObjectList` no longer lets the reported Object_List length size its allocation or run its index loop past 2^32-1.

### Changed

- Go 1.26 floor (`go fix` applied). CI tests on Go 1.26 and 1.27, with shared workflows v3.3.1.
- IP_PKTINFO ancillary data is encoded without `unsafe`.
- `internal/fixtures` reads the corpus through `os.Root`.

### Tests

- `e2e/hostile_test.go` adds a hostile device on loopback UDP.
- `client/chaos_segack_test.go` adds chaos on the segmented request path.
- `FuzzClientReceive` fuzzes the client receive path. It runs in `make fuzz` and nightly.
- Range tables for the service decoders and the device snapshot.
- SAST: gosec and opengrep at low/low report 0 open findings (89 before).

---

## v0.4.1

**Date:** 2026-10-31
**Previous release:** v0.4.0

### Changed

- Policy 1.2: build output in `bin/` and `release/`, a per-package coverage floor, and more tests and CI. No change in behaviour.

---

## v0.4.0

**Date:** 2026-10-10  
**Previous release:** [v0.3.0](https://github.com/otfabric/go-bacnet/releases/tag/v0.3.0)

### Summary

Review release. The client and the codecs were audited against the standard,
against five peer stacks and against Wireshark's dissector; the defects found
are fixed, the client reports invalid configuration instead of adjusting it,
remote errors carry their standard names, and logging is available through
`log/slog`. The `bacnetctl` command moved to its own repository,
[otfabric/bacnetctl](https://github.com/otfabric/bacnetctl); this module is a
library only again. Requires **Go 1.23+**.

Tested against bacnet-stack 1.6.1, BACpypes3 0.0.110, BACnet4J 6.1.1,
Worldiety `3cb2aa80` and System.IO.BACnet 4.0.0 (206 peer scenarios), and
against Wireshark 4.2–4.6 for the request layouts.

**Interop pin → [`bacnet-interop` v0.10.0](https://github.com/otfabric/bacnet-interop/releases/tag/v0.10.0)**
(`interop/bacnet-interop-pin.json`, seven images by digest, two of them new:
`sysiobacnet` and `bacnet-stack-router`). This version needs that release:
five codec vectors of the fixture corpus were corrected together with the
encoders they were derived from, so `internal/fixtures` fails against the
v0.9.0 corpus.

**This release changes behaviour and some exported identifiers.** Read
"Changed" and "Removed" before upgrading. Still not validated against
physical devices ([field validation](docs/FIELD_VALIDATION.md)).

### Fixed

Client:

- **Closing the client while a subscription request was in flight** could
  panic with "close of closed channel": `Close` and the failing subscribe both
  ended the half-made subscription.
- **A request overtaken by `Close`** could fail with the transport's error
  ("use of closed network connection") instead of `ErrClosed`.
- **Invoke-ID allocation** reserved nothing: two concurrent requests could be
  given the same invoke ID when few were free, and more requests than the
  configured maximum could be in flight. Allocation now reserves the ID.
- **Data race** between a request being sent and `Close` (the "sent" flag).
- **Receive loop** spun at full speed while the socket kept failing (interface
  down). It now backs off (5 ms doubling to 500 ms) and reports a `transport`
  diagnostic.
- **ReadProperty of the Device wildcard** (`device,4194303`) failed with
  `ErrProtocolViolation` because the reply names the real device instance.
- **`ReadRangeAll`** stopped after the first page whenever a page returned all
  the items it asked for (the device clears MORE_ITEMS then), and `MaxItems`
  counted decoded elements instead of records. Found by the new peer tests,
  on all three peers that serve ReadRange.
- **`ReadPropertyMultipleBatched`** split by object only, so one object with
  many properties was never split; retried after Abort reason 5
  (security-error) instead of 1 (buffer-overflow) and 11 (apdu-too-long); and
  returned results per batch instead of per input specification.
- **A `Target` with only an endpoint** sent the request and then discarded the
  answer as coming from the wrong source, ending in a timeout. The address of
  a directly reachable device is now derived from the endpoint.
- **Broadcasts went to 255.255.255.255 only.** Devices listen for the
  broadcast address of their subnet, and many do not receive the limited
  broadcast: of the five peer stacks, a broadcast Who-Is reached two. A
  broadcast is now sent to the directed broadcast address of every local
  subnet (or of the `WithInterface` interface) and to 255.255.255.255.
  The operating system sends 255.255.255.255 on one interface only, that of
  the default route: on a host with several interfaces a device that hears
  only this form was not reached on the others. On Linux and macOS the
  limited broadcast is now sent on each interface in turn (found by the peer
  tests in CI, whose runner has a Docker bridge next to its uplink).
  Found by running the CLI against the peers; the peer tests had only used
  unicast Who-Is.
- **A plain Error in answer to WritePropertyMultiple** (System.IO.BACnet
  sends one instead of a WritePropertyMultiple-Error) was reported as a
  malformed frame. It is now an `*ErrorResponse` with the device's class and
  code.
- **An Error PDU naming another service than the request** (BACnet4J answers
  a refused SubscribeCOVPropertyMultiple with error-choice 127) was dropped,
  so the caller got a timeout, or outcome-unknown for a write. Invoke ID and
  source identify the transaction; the Error now completes the request.
- **Unsolicited confirmed requests** were ignored, leaving the sender to
  retry. They are answered with Reject (unrecognized-service), Abort
  (segmentation-not-supported) for segmented ones, and Reject for a
  notification that does not decode.

Codecs (each with a regression test; layouts marked ✓ were also checked
against Wireshark's dissector):

- **NPDU Hop Count** was written and read directly after DADR. It belongs
  after SADR, which matters whenever both a destination and a source network
  address are present, e.g. an I-Am from a routed device answering a global
  Who-Is: such frames were misparsed. ✓
- **Router-Busy-To-Network and Router-Available-To-Network were swapped**
  (`npdu.NetMsgRouterBusyToNetwork` is X'04', `…Available…` X'05'): a router
  announcing congestion was treated as available again, and the reverse. ✓
- **An empty list or array could not be read.** A property whose value is an
  empty list (Device_Address_Binding or Active_COV_Subscriptions of a device
  without any, for example) arrives as an empty value (`3E 3F`), which
  ReadProperty refused as malformed; in a ReadPropertyMultiple reply it
  discarded every other result too. It now decodes as a constructed value
  without elements. Reproduced on bacnet-stack, BACnet4J, BACpypes3 and
  System.IO.BACnet; found by the fifth peer's test suite.
- **Audit services did not interoperate.** The audit notification request was
  encoded and expected as three loose fields instead of
  `notifications [0] SEQUENCE OF BACnetAuditNotification`, so notifications
  from bacnet-stack and BACpypes3 produced no events; AuditLogQuery lacked the
  required query parameters and BACpypes3 could not decode it. Both are
  rewritten to Clause 21: the client now decodes the notifications and log
  records of two independent upstream encoders, and BACpypes3 decodes the
  query field by field. ✓
- **ReadRange ACK**: a peer-controlled item count sized an allocation for
  Log_Buffer records (up to hundreds of gigabytes requested from a 36-octet
  datagram). The count is now bounded by the data present.
- **Error PDUs with a structured body** (CreateObject, AddListElement,
  RemoveListElement, ConfirmedPrivateTransfer, VT-Close,
  SubscribeCOVPropertyMultiple) were reported as `ErrMalformed`; they now
  yield `*ErrorResponse` with the device's class and code.
- **COV notifications carrying a priority** (`BACnetPropertyValue.priority`)
  were rejected as malformed and dropped.
- **SubscribeCOVPropertyMultiple** request ✓, **GetEnrollmentSummary**
  priority filter ✓, **COVNotificationMultiple** time-of-change, **HostNPort**,
  out-of-range / floating-limit **notification parameters**: tags did not
  follow the standard's ASN.1.
- **The BACnet `Error` was encoded with context tags** (`09 02 19 20`) by
  `EncodeBACnetError` and therefore by the ReadPropertyMultiple-ACK and
  WritePropertyMultiple-Error encoders. It is two application ENUMERATEDs
  (`91 02 91 20`). Only code that plays the server side (simulators, tests)
  was affected; decoding always accepted the standard form and still accepts
  the old one.
- **CalendarEntry** date and weekNDay, **Date_List**, **Daily/WeeklySchedule**
  could not decode real wire data.
- **`AppendTag`** silently emitted an empty context tag for a typed value.
- **Life-safety enumerations**: `LifeSafetyMode`, `LifeSafetyState` and
  `LockStatus` had wrong values (see "Changed").
- **Tag decoder**: non-canonical application Booleans, out-of-range BitString
  unused-bit counts and invalid extended tag numbers were accepted; a length
  check could overflow on 32-bit platforms.
- Identifiers, array indexes and counts wider than 32 bits were truncated
  instead of rejected; `EncodeWriteProperty` accepted priorities outside
  1..16; several decoded values aliased the receive buffer.
- **GetEnrollmentSummary ACK** entries without the optional notification
  class, and **You-Are** with the optional MAC address or without a device
  identifier, were rejected.

### Added

- `client.WithLogger(*slog.Logger)`: leveled logging of client diagnostics
  ([OBSERVABILITY.md](OBSERVABILITY.md)). The client stays silent without it.
- `bacnet.ErrInvalidConfig`; `client.New` validates options.
- `bacnet.ErrorClassName`, `ErrorCodeName`, `RejectReasonName`,
  `AbortReasonName` and constants for the standard values; remote error
  messages now read `class=object(1) code=unknown-object(31)`.
- `bacnet.AnyDevice`, `WildcardInstance`, `ObjectIdentifier.Matches`.
- `DiscoveryOptions.Window` / `WhoHasOptions.Window`: `Discover` and
  `DiscoverObjects` return a nil error after the window.
- `DeviceObservation.Target()` and `.DeviceID()`.
- `ReadObjectList` falls back to reading the list element by element when the
  device cannot return it in one reply.
- `client.ErrNoRoute` (wraps `ErrUnsupported`) from `ResolveTarget`;
  `Client.AllRoutes()`.
- `Client.SubscribeCOVMultiple`: a managed SubscribeCOVPropertyMultiple
  subscription (renewed at half the lifetime, cancelled by `Close`) with
  `COVMultipleOptions.MaxNotificationDelay`. `SubscriptionEvent.Multiple`
  carries the whole COVNotificationMultiple, with its timestamp and each
  value's time of change. `SubscribeCOVPropertyMultipleRequest.Cancellation`
  encodes the cancellation (no issue-confirmed-notifications, no lifetime).
  Before, a COVNotificationMultiple reached the caller only as per-object
  notifications of a single-object subscription that happened to have the
  same process identifier, without any time.
- `service.VTDataACK`, `DecodeVTDataACK`, `EncodeVTDataACK`.
- `bacnet.PropertyAll`, `PropertyRequired`, `PropertyOptional` (the special
  identifiers of ReadPropertyMultiple).
- `CharacterString.Text()` for UCS-2, UCS-4 and ISO 8859-1 strings and
  `CharacterEncoding…` constants.
- `apdu.DecodeError` / `ErrorDetail` (first-failed-element number of
  structured errors); `service.PropertyValue.Priority`;
  `SubscribeCOVPropertyMultipleRequest.MaxNotificationDelay`;
  `COVNotificationMultipleValue.TimeOfChange`; `YouAre.MAC`;
  `AcknowledgmentFilter…` constants; `bacnet.ContextUnsigned32`,
  `ContextReal`, `ContextDate`, `AppendContextReal`; `HostNPort.None`.
- `*service.WritePropertyMultipleError` unwraps to an `*ErrorResponse`.
- Chaos tests for the client (`make chaos`, and at normal load in every
  `go test -race`): concurrent requests, subscriptions and `Close` against
  in-process devices that misbehave at random. The client's unit tests now
  run in parallel. Both found the two `Close` races under "Fixed"; a test
  that changed the shared service-policy table under running requests (a data
  race in CI) no longer does.
- `make interop-pinned` (pulls the pinned peer images and runs everything)
  and `make dissector` (Wireshark oracle for every request the client emits).
- Peer tests for the Device wildcard, endpoint-only targets, object lists,
  batched ReadPropertyMultiple, priority writes and relinquish, device
  snapshot, unknown-object and write-access-denied errors, unanswered
  requests (timeout, retransmission count, outcome-unknown, context
  deadline), ReadRange paging, GetEventInformation, AcknowledgeAlarm,
  SubscribeCOVProperty, COV renewal past the lifetime, chunked file read and
  write, and DCC "disable" being refused.
- [ARCHITECTURE.md](ARCHITECTURE.md) (was `docs/PACKAGE_DESIGN.md`, now with
  the client runtime), [OBSERVABILITY.md](OBSERVABILITY.md), compiled
  examples in `client/example_test.go`.

### Changed

- **`client.New` fails on invalid options** instead of ignoring them:
  `WithSegmentWindow` outside 1..127, negative `WithTransactionOptions`
  values, `WithPort` outside 0..65535, and `WithDeviceManagementEnabled` /
  `WithNetworkManagementEnabled` with a wrong confirmation string (previously
  the option silently stayed off). Errors wrap `ErrInvalidConfig`; the ones
  that already existed still also wrap `ErrMalformed`.
- **`client.New` closes an injected transport when it fails.**
- **`ReadPropertyMultipleBatched`** returns exactly one result per input
  specification and rejects a specification without properties.
- **`ResolveTarget`** returns `ErrNoRoute` (still `errors.Is`
  `ErrUnsupported`).
- **A `Target` without a valid endpoint** fails with `ErrMalformed` before
  sending.
- **Error texts** of `ErrorResponse`, `RejectError`, `AbortError` and
  `WritePropertyMultipleError` include names. Do not parse them.
- **Broadcast destinations** (see "Fixed"): a device that receives both the
  directed and the limited broadcast answers a Who-Is twice.
- **`npdu.NetMsgRouterBusyToNetwork` / `NetMsgRouterAvailableToNetwork`**
  exchanged values.
- **Diagnostics**: receive failures are reported with kind `transport`
  (previously `malformed`).
- **Life-safety enumerations follow the standard.** `LifeSafetyMode`: `Armed`
  4→5, `Disarmed` 5→6, `Prearmed` 6→7; added `Unmanned`, `Slow`, `Fast`,
  `Disconnected`, `Enabled`, `Disabled`, `AutomaticReleaseDisabled`,
  `Default`. `LifeSafetyState`: `Duress` … `TestSupervisory` are each one
  lower (14..23). `LockStatus`: `LockStatusFault` is now `LockStatusLockFault`,
  `Unused` 5→3, `Unknown` 3→4.
- **`Client.VTData` returns `(service.VTDataACK, error)`** instead of
  `error`: the acknowledgement tells whether the device took all of the data,
  and how much if not, which the caller needs in order to resend the rest. A
  SimpleACK in reply to VT-Data is now `ErrProtocolViolation`.
- **`DecodeSubscribeCOVPropertyMultiple`** reports a request without
  issue-confirmed-notifications and lifetime as `Cancellation`.
- **Wire bytes changed** for `EncodeSubscribeCOVPropertyMultiple`,
  `EncodeGetEnrollmentSummary` (priority filter), `EncodeHostNPort`,
  `EncodeCOVNotificationMultiple` (time-of-change), out-of-range and
  floating-limit `EncodeNotificationParameters`, `EncodeYouAre` (a zero
  device is omitted), `EncodeBACnetError` / `EncodeReadPropertyMultipleACK` /
  `EncodeWritePropertyMultipleError`, and `npdu.Append` with both addresses.
  `DecodeSubscribeCOVPropertyMultiple` rejects the pre-standard layout.
- **Stricter decoding**: see the tag-decoder, NPDU (SNET 65535 rejected) and
  32-bit items under "Fixed". `npdu.Append` ignores `Control` bits other than
  expecting-reply and rejects remote network 0 / 65535.
- `DecodeCalendarEntry` and `DecodeHostNPort` errors wrap `ErrMalformed`.

- **Audit API (package `service`)**: `AuditNotification` carries the 17
  fields of the standard (`SourceDevice` and `TargetDevice` are
  `AuditRecipient`); `AuditLogQueryRequest` has `ByTarget` / `BySource`,
  `StartAtSequenceNumber` and `RequestedCount` and needs exactly one of the
  two queries; `AuditLogQueryACK.Records` is `[]AuditLogRecordResult`. One
  request carrying several notifications yields one `AuditStreamEvent` each.
  Client method signatures are unchanged.

### Removed

- `service.EncodeAuditNotification` / `DecodeAuditNotification` (use
  `EncodeAuditNotificationRequest` / `DecodeAuditNotificationRequest`),
  `AuditNotification.TimeStamp` and `.Remaining`, `AuditLogQueryRequest.Query`.
- **`cmd/bacnetctl`** and its release binaries. Use
  [otfabric/bacnetctl](https://github.com/otfabric/bacnetctl).
- `LifeSafetyMode` constants that are not in the standard (`Quiet`,
  `FaultSealed`, `FaultUnsealed`, `FaultImmediate`, `FaultDelayed`,
  `PullActive`, `Manual`, `Interlocked`, `Blocked`, `Tamper`, `TestActive…`,
  `TestHold…`), `LifeSafetyStateHoldupAlarm`, `LockStatusLockedOut`.

### Deprecated

- `ErrTransactionCapacity`, `ErrResponseSourceMismatch`, `ErrDeliveryDropped`:
  never returned by any code path.
- `ReadRangePageOptions.StopOnError`: had no effect.

### Known limitations

- AuthRequest has no implementation in any stable peer stack; its encoding is
  an opaque `[0]` wrapper checked against nothing but Wireshark's outline.
  All five peers refuse the service.
- VT: the requests are decoded by BACpypes3's own classes, and its VT-Open
  and VT-Data ACKs by the client; no stack executes a VT session and the
  client does not handle VT-Data sent to it.
- SubscribeCOVPropertyMultiple: no peer stack executes the subscription.
  BACpypes3 decodes subscribe, renewal and cancellation; BACnet4J encodes the
  notifications. Subscribe → change → notify has not been seen against one
  device.
- The audit *service frames* are confirmed by Wireshark and by adapter-declared
  frames around the stacks' own data-type codecs; no stable stack has the
  services themselves.
- Proprietary network-layer message types (≥ 0x80) carry no vendor ID field.
- The dissector oracle checks requests only.

---

## v0.3.0

**Date:** 2026-08-03  
**Previous release:** [v0.2.5](https://github.com/otfabric/go-bacnet/releases/tag/v0.2.5)  
**Tag:** [`v0.3.0`](https://github.com/otfabric/go-bacnet/releases/tag/v0.3.0) @ `c4da6d9`

### Summary

Client-completeness release: confirmed-service policy registry, BVLC BDT/FDT
management, multi-route state, adaptive RPM/WPM/file/ReadRange helpers, optional
event dispatcher and `device` snapshot, Schedule/Calendar/HostNPort constructed
types, property-aware decoding, and expanded NotificationParameters. Pin moves
to [`bacnet-interop` v0.9.0](https://github.com/otfabric/bacnet-interop/releases/tag/v0.9.0)
@ `180006f`. Requires **Go 1.23+**.

Still **production-candidate** until
[field validation](docs/FIELD_VALIDATION.md) is complete.

### Added

- **ConfirmedServicePolicy** / **ConfirmedServiceCapabilities** registry —
  retransmit safety, outcome-unknown after send, `OperationClass` opt-in, and
  success-response form separated from segmentation capabilities
- **`WithNetworkManagementEnabled`** — opt-in for BVLC Write-BDT
  (`ErrNetworkManagementDisabled` without confirm)
- Typed **BVLC BDT/FDT** codecs (`bvlc.BDTEntry` / `FDTEntry`, `IPv4Mask`) and
  client APIs: `ReadBroadcastDistributionTable`,
  `WriteBroadcastDistributionTable`, `ReadForeignDeviceTable`,
  `DeleteForeignDeviceTableEntry` (`*BVLCOperationError` on non-zero Result)
- Multi-route cache with **transaction-stable** next-hop selection; Busy /
  Available / Reject update route state for later transactions only
- Adaptive helpers: `ReadPropertyMultipleBatched`,
  `WritePropertyMultipleBatched` (Completed/Failed/Unknown),
  `ReadFileStream` / `ReadFileRecords` / `WriteFileStream`, `ReadRangeAll`,
  `ReadObjectList` / `ReadPropertyList` / `WritePriority` / `RelinquishPriority`
- Optional **`WithEventDispatcher`** (bounded workers, overflow policy, panic
  recovery); confirmed EventNotifications still SimpleACK'd before enqueue
- Optional **`device.ReadSnapshot`** via `PropertyReader` (client does not
  import `device`)
- Constructed types: DateRange / DateTime / TimeStamp, CalendarEntry,
  WeeklySchedule / ExceptionSchedule, HostNPort
- `DecodePropertyValue` with `ApplicationValue` fallback; access / life-safety
  enums; expanded object-type and property-identifier constants; ID name maps
- NotificationParameters: change-of-life-safety, extended, complex-event-type
- NPDU network-message constants and codecs for Reject-Message-To-Network,
  I-Could-Be-Router-To-Network, Network-Number-Is validation
- `internal/faulty` transport for fault-injection tests; `bacnet.ErrBusy`
- Interop: BDT/FDT / Write-BDT / Delete-FDT scenarios
  (`interop/bvlc_mgmt_test.go`) against bacnet-stack, BACpypes3, BACnet4J

### Changed

- Pin → [`bacnet-interop` v0.9.0](https://github.com/otfabric/bacnet-interop/releases/tag/v0.9.0)
  (`interop/bacnet-interop-pin.json`) — digests from the published release
- Typed confirmed helpers look up the policy registry for retransmission and
  outcome-unknown wrapping (broader coverage than the prior hard-coded set)
- `InvokeConfirmed` / `ConfirmedInvokeOptions`: additive `Retransmit` and
  `SideEffecting` fields; default remains non-retransmitting; outcome-unknown
  wraps only when `SideEffecting` is true
- Retransmit send failures emit diagnostics instead of being silently ignored
- Docs: CLIENT_SUPPORT / INTEROP / API / PLAN reflect closed client-completeness
  backlog and BBMD peer limits (Write-BDT NAK on stack/BACpypes3; BACnet4J live)

### Breaking (v0.x)

- `npdu.NetMsgRouterNetworkNumberIs` renamed to `npdu.NetMsgNetworkNumberIs`
  (same wire value `0x13`)

### Evidence

| Gate | Result | Link |
|---|---|---|
| Shared CI (lint, Go matrix, race, coverage, PR fuzz) | green | [30777818976](https://github.com/otfabric/go-bacnet/actions/runs/30777818976) |
| Pinned interop `v0.9.0` | green | [30777818818](https://github.com/otfabric/go-bacnet/actions/runs/30777818818) (job *Pinned release peers*) |
| bacnet-interop main compat | green | same run (job *bacnet-interop main compat*) |
| Pin | [`bacnet-interop` v0.9.0](https://github.com/otfabric/bacnet-interop/releases/tag/v0.9.0) @ `180006f` | `interop/bacnet-interop-pin.json` |
| GitHub Release | published | [v0.3.0](https://github.com/otfabric/go-bacnet/releases/tag/v0.3.0) |

Live BBMD / FDT scenarios on the pinned digests:

| Scenario | Peers | Test / evidence |
|---|---|---|
| Read-BDT | bacnet-stack + BACpypes3 + BACnet4J | `Test*ReadBDT` |
| Read-FDT (after FD) | bacnet-stack + BACpypes3 + BACnet4J | `Test*ReadFDT*` |
| Write-BDT | BACnet4J OK; stack/BACpypes3 NAK asserted | `Test*WriteBDT*` |
| Delete-FDT | bacnet-stack + BACpypes3 + BACnet4J | `Test*DeleteFDT*` |

Prior application-service and FDR scenarios from `v0.2.5` remain on the new
digests.

### Notes / known skips

- Worldiety BBMD / native multi-homed BIP router unavailable at pin (routed
  tests use `bip-router`; see bacnet-interop `PEER_SUPPORT.md`)
- Audit / AuthRequest / VT remain codec/unit — no peer servers at pin
- Field validation still pending ([FIELD_VALIDATION.md](docs/FIELD_VALIDATION.md))
- Library coverage gate held at ≥90%

---

## v0.2.5

**Date:** 2026-08-03  
**Previous release:** [v0.2.4](https://github.com/otfabric/go-bacnet/releases/tag/v0.2.4)  
**Tag:** [`v0.2.5`](https://github.com/otfabric/go-bacnet/releases/tag/v0.2.5) @ `867048a`

### Summary

Interop pin and documentation update. **No new client APIs.** Pin moves to
[`bacnet-interop` v0.8.0](https://github.com/otfabric/bacnet-interop/releases/tag/v0.8.0)
@ `34f42dc` (native router/BBMD audit; bacnet-stack peer-as-BBMD confirmed).
Requires **Go 1.23+**.

### Changed

- Pin → [`bacnet-interop` v0.8.0](https://github.com/otfabric/bacnet-interop/releases/tag/v0.8.0)
  (`interop/bacnet-interop-pin.json`) — digests from the published release
- Interop: `TestBacnetStackForeignDeviceWhoIsReadProperty` (peer-as-BBMD / FDR)
- Docs: lean client-support baseline; INTEROP BBMD matrix; CLIENT_SUPPORT
  Register-Foreign-Device multi-peer evidence; PLAN client-completeness backlog

### Evidence

| Gate | Result | Link |
|---|---|---|
| Shared CI (lint, Go matrix, race, coverage, PR fuzz) | green | [30773312858](https://github.com/otfabric/go-bacnet/actions/runs/30773312858) |
| Pinned interop `v0.8.0` | green | [30773312737](https://github.com/otfabric/go-bacnet/actions/runs/30773312737) |
| Pin | [`bacnet-interop` v0.8.0](https://github.com/otfabric/bacnet-interop/releases/tag/v0.8.0) @ `34f42dc` | `interop/bacnet-interop-pin.json` |

---

## v0.2.4

**Date:** 2026-08-03  
**Previous release:** [v0.2.3](https://github.com/otfabric/go-bacnet/releases/tag/v0.2.3)  
**Tag:** [`v0.2.4`](https://github.com/otfabric/go-bacnet/releases/tag/v0.2.4) @ `daef989`

### Summary

Evidence and wire-correctness patch after
[`v0.2.3`](https://github.com/otfabric/go-bacnet/releases/tag/v0.2.3).
**No new client APIs.** Pin moves to
[`bacnet-interop` v0.7.0](https://github.com/otfabric/bacnet-interop/releases/tag/v0.7.0)
@ `224f51b`. Requires **Go 1.23+**.

Container-interoperability validated; [field validation](docs/FIELD_VALIDATION.md)
pending.

### Fixed

- Who-Am-I / You-Are: application-tagged SEQUENCE (ASHRAE unnamed fields),
  matching bacnet-stack `whoami` / `youare` (was incorrect context tags)
- LifeSafetyOperation `requestingSource`: context-primitive CharacterString
  (was constructed wrapper); matches BACnet4J + bacnet-stack
- TextMessage: `messageClass` CHOICE + context-primitive `messageText` (was
  numeric-only class + constructed text); enables BACnet4J / BACpypes3 receipt

### Changed

- Pin → [`bacnet-interop` v0.7.0](https://github.com/otfabric/bacnet-interop/releases/tag/v0.7.0)
  (`interop/bacnet-interop-pin.json`)
- Interop harness retains peer `{"event":"operation",…}` JSONL after ready
  (and tees to an evidence file for Docker Desktop re-exec)

### Evidence

| Gate | Result | Link |
|---|---|---|
| Shared CI (lint, Go matrix, race, coverage, PR fuzz) | green | [30771576116](https://github.com/otfabric/go-bacnet/actions/runs/30771576116) |
| Pinned interop `v0.7.0` | green | [30771575928](https://github.com/otfabric/go-bacnet/actions/runs/30771575928) |
| bacnet-interop main compat | green | same interop run |
| Pin | [`bacnet-interop` v0.7.0](https://github.com/otfabric/bacnet-interop/releases/tag/v0.7.0) | `interop/bacnet-interop-pin.json` |
| GitHub Release | published | [v0.2.4](https://github.com/otfabric/go-bacnet/releases/tag/v0.2.4) |

Live scenarios on the pinned digests:

| Scenario | Peers | Test / evidence |
|---|---|---|
| GetAlarmSummary (AV Out_Of_Range) | BACnet4J + bacnet-stack | `TestGetAlarmSummaryOutOfRange` |
| GetEnrollmentSummary (EE-1) | BACnet4J | `TestBACnet4JGetEnrollmentSummary` |
| SubscribeCOVPropertyMultiple / COVNotificationMultiple | — | codec/unit only |
| Messaging semantic receipt (v6 `operation` JSONL) | BACnet4J + BACpypes3 + bacnet-stack | `TestMessagingSemanticReceipt` |
| Who-Am-I / You-Are (v7) | bacnet-stack | `TestBacnetStackWhoAmIYouAre` |
| LifeSafetyOperation LSP-1 (v8) | BACnet4J + bacnet-stack | `TestLifeSafetyOperation` |
| Audit notification / AuditLogQuery / AuthRequest / VT | — | codec/unit only |

Prior File / Create-Delete / NC list live scenarios from `v0.2.3` remain on the
new digests.

### Notes / known skips

- Worldiety segmented WPM/RPM: continuation ServiceChoice deviation (see
  bacnet-interop `PEER_SUPPORT.md`)
- Open: native router/BBMD peer audit (bacnet-interop `BLOCKERS.md`)
- Library coverage gate held at ≥90%

---

## v0.2.3

**Date:** 2026-08-02  
**Previous release:** [v0.2.2](https://github.com/otfabric/go-bacnet/releases/tag/v0.2.2)  
**Tag:** [`v0.2.3`](https://github.com/otfabric/go-bacnet/releases/tag/v0.2.3) @ `86ffd31`

### Summary

Correctness and evidence patch after
[`v0.2.2`](https://github.com/otfabric/go-bacnet/releases/tag/v0.2.2).
**No new services.** Wire fixes for file / CreateObject / AtomicWriteFile ACK,
plus live-multi-peer File, Create/Delete, and NC list evidence. Pin moves to
[`bacnet-interop` v0.6.0](https://github.com/otfabric/bacnet-interop/releases/tag/v0.6.0)
@ `f4ea3de`. Requires **Go 1.23+**.

Still **production-candidate** until the
[field validation](docs/FIELD_VALIDATION.md) is complete.

### Fixed

- `EncodeAtomicReadFile` / `EncodeAtomicWriteFile`: application ObjectIdentifier
  plus access CHOICE `[0]` stream / `[1]` record (was context file `[0]` and
  CHOICE `[1]`/`[2]`)
- `EncodeAtomicWriteFileACK` / `DecodeAtomicWriteFileACK`: context-primitive
  Signed `[0]`/`[1]` (was constructed SEQUENCE)
- `EncodeCreateObject` / `DecodeCreateObject`: objectSpecifier `[0]` CHOICE plus
  listOfInitialValues `[1]` (was bare CHOICE + list `[2]`)
- Request decoders reject the pre-v0.2.3 forms; record write includes
  `recordCount`
- Codec goldens + malformed old-form fixtures via bacnet-interop `v0.6.0`

### Changed

- Pin → [`bacnet-interop` v0.6.0](https://github.com/otfabric/bacnet-interop/releases/tag/v0.6.0)
  (`interop/bacnet-interop-pin.json`)

### Evidence

Green on tag SHA `86ffd31`:

| Gate | Result | Link |
|---|---|---|
| Shared CI (lint, Go matrix, race, coverage, PR fuzz) | green | [30766339592](https://github.com/otfabric/go-bacnet/actions/runs/30766339592) |
| Pinned interop `v0.6.0` | green | [30766339454](https://github.com/otfabric/go-bacnet/actions/runs/30766339454) (job *Pinned release peers*) |
| bacnet-interop main compat | green | same run (job *bacnet-interop main compat*) |
| Pin | [`bacnet-interop` v0.6.0](https://github.com/otfabric/bacnet-interop/releases/tag/v0.6.0) | `interop/bacnet-interop-pin.json` |
| GitHub Release | published | [v0.2.3](https://github.com/otfabric/go-bacnet/releases/tag/v0.2.3) |

Live-multi-peer scenarios on the pinned digests:

| Scenario | Peers | Test |
|---|---|---|
| AtomicReadFile stream/record | BACnet4J + bacnet-stack | `TestAtomicReadFileStream` / `Record` |
| AtomicWriteFile stream + readback | BACnet4J + bacnet-stack | `TestAtomicWriteFileStreamReadback` |
| CreateObject / DeleteObject | BACnet4J + bacnet-stack | `TestCreateDeleteObject` |
| AddListElement / RemoveListElement (NC Recipient_List) | BACnet4J + bacnet-stack | `TestAddRemoveListElementRecipientList` |
| GetAlarmSummary (AV Out_Of_Range) | BACnet4J | `TestBACnet4JGetAlarmSummary` |

### Notes / known skips

- Worldiety segmented WPM/RPM: `upstream-deviation` (B6; see bacnet-interop `PEER_SUPPORT.md`)
- Batch A–C live scenarios and the `v0.7.0` pin landed in **v0.2.4**
- Library coverage gate held at ≥90%

---

## v0.2.2

**Date:** 2026-08-02  
**Previous release:** [v0.2.1](https://github.com/otfabric/go-bacnet/releases/tag/v0.2.1)  
**Tag:** [`v0.2.2`](https://github.com/otfabric/go-bacnet/releases/tag/v0.2.2) @ `a277aea`

### Summary

Service-breadth minor: Worldiety as a fourth digest-pinned peer oracle, plus
codecs/APIs for alarm/enrollment/COV-multiple, file, list/lifecycle, messaging,
audit/identity, and life-safety/VT. Pin moves to
[`bacnet-interop` v0.5.0](https://github.com/otfabric/bacnet-interop/releases/tag/v0.5.0).
Requires **Go 1.23+**.

Still **production-candidate** until the
[field validation](docs/FIELD_VALIDATION.md) is complete.

### Added

- Worldiety as a fourth digest-pinned peer oracle (`interop/worldiety_test.go`):
  Who-Is/Who-Has, RP/RPM/WP/WPM, ReadRange (unsegmented)
- GetAlarmSummary / GetEnrollmentSummary / SubscribeCOVPropertyMultiple
- Confirmed/Unconfirmed COVNotificationMultiple receive (+ encode helper)
- OpenEventStream / TransitionOf; OpenAuditStream
- AtomicReadFile / AtomicWriteFile (+ FileChunkBounds)
- AddListElement / RemoveListElement / CreateObject / DeleteObject
- Confirmed/Unconfirmed PrivateTransfer and TextMessage; TimeSynchronization /
  UTCTimeSynchronization; WriteGroup
- AuditLogQuery / AuthRequest / Who-Am-I / You-Are; audit notification receive
- LifeSafetyOperation; VT-Open / VT-Close / VT-Data
- Additional NotificationParameters typed CHOICEs (command-failure, floating-limit,
  buffer-ready, unsigned-range)
- Request decoders for the new confirmed/unconfirmed services
- Codec goldens for the new services via `bacnet-interop/fixtures/codec`

### Changed

- CI pinned interop lane pulls and runs Worldiety digests from `v0.5.0`
- Interop pin file includes `worldiety` image digest
- Portable loopback interface selection for coverage-stable `WithInterface` tests
  (Linux `lo` / Darwin `lo0`)

### Evidence

Green on tag SHA `a277aea`:

| Gate | Result | Link |
|---|---|---|
| Shared CI (lint, Go matrix, race, coverage, PR fuzz) | green | [30757758929](https://github.com/otfabric/go-bacnet/actions/runs/30757758929) |
| Pinned interop `v0.5.0` (+ Worldiety) | green | [30757758742](https://github.com/otfabric/go-bacnet/actions/runs/30757758742) (job *Pinned release peers*) |
| bacnet-interop main compat | green | same run (job *bacnet-interop main compat*) |
| Pin | [`bacnet-interop` v0.5.0](https://github.com/otfabric/bacnet-interop/releases/tag/v0.5.0) | `interop/bacnet-interop-pin.json` |
| GitHub Release | published | [v0.2.2](https://github.com/otfabric/go-bacnet/releases/tag/v0.2.2) |

### Notes / known skips

- Worldiety segmented WPM/RPM skipped (peer segment service-choice — B6)
- At tag time: BACnet4J AtomicReadFile / Create-Delete still skipped (B8/B9) —
  closed in [`v0.2.3`](https://github.com/otfabric/go-bacnet/releases/tag/v0.2.3)
- Broader live multi-peer coverage for COV-multiple, messaging diagnostics,
  audit, life-safety/VT, and topology/bbmd v2 modes remains partial
  (see `bacnet-interop/BLOCKERS.md`)
- Library coverage gate held at ≥90%

---

## v0.2.1

**Date:** 2026-08-02  
**Previous release:** [v0.2.0](https://github.com/otfabric/go-bacnet/releases/tag/v0.2.0)

### Summary

Correctness and decoder-strictness patch on the `v0.2.0` supervisory-client
surface: service-choice correlation, segmented confirmed-request overhead and
SegmentACK direction, bounded segmentation control sends, broader
outcome-unknown wrapping, EventNotification receive-path safety, and fail-closed
decoders for WPM / ReadRange / events / DCC / ReinitializeDevice. Pin moves to
[`bacnet-interop` v0.4.2](https://github.com/otfabric/bacnet-interop/releases/tag/v0.4.2).
Requires **Go 1.23+**.

Still **production-candidate** until the
[field validation](docs/FIELD_VALIDATION.md) is complete.

### Fixed

- Service-choice correlation no longer treats choice `0` (AcknowledgeAlarm) as
  “unknown”; SimpleACK / ComplexACK / Error and every ComplexACK segment compare
  unconditionally
- Segmented confirmed-request first-segment overhead corrected (`6`, not `7`)
- Outbound segmented-request SegmentACK delivery requires `Server=true`
- Segmentation control sends (SegmentACK / Abort) use a bounded timeout
- Side-effecting services wrap Close and ambiguous transport failures after
  transmission has been attempted as `*OutcomeUnknownError`
- EventNotification handler: receive-path deadlock warning; panics recovered
- Documentation: segmented PDUs carry service choice on every segment (not an
  ASHRAE “omit after segment 0” peer quirk)
- Decoder strictness: WPM / ReadRange / EventNotification / DCC /
  ReinitializeDevice reject duplicate fields and unsigned overflows; ResultFlags
  shape checked; Log_Buffer requires successful LogRecord split; known
  NotificationParameters CHOICEs fail closed on malformed contents (unknown /
  unimplemented CHOICEs stay opaque)

### Added

- Fuzz targets for WPM, ReadRange(+ACK), LogRecords, Who-Has/I-Have,
  EventNotification, NotificationParameters, GetEventInformationACK,
  AcknowledgeAlarm, DCC, ReinitializeDevice
- Negative codec fixtures in `bacnet-interop` for WPM/ReadRange/DCC/Reinit
  duplicates and overflow

### Changed

- Pin → [`bacnet-interop` v0.4.2](https://github.com/otfabric/bacnet-interop/releases/tag/v0.4.2)
  (`interop/bacnet-interop-pin.json`)
- `make fuzz` discovers `Fuzz*` targets via `go test -list` and runs each with an
  anchored `-fuzz='^Name$'` so substring matches cannot skip or double-run

### Evidence

Pre-tag green on `main` @ `614c4d9` (re-verify / attach on the exact tag SHA when
cutting):

| Gate | Result | Link |
|---|---|---|
| Shared CI (lint, Go matrix, race, coverage, PR fuzz) | green on `614c4d9` | [30725263974](https://github.com/otfabric/go-bacnet/actions/runs/30725263974) |
| Pinned interop `v0.4.2` | green | [30725263801](https://github.com/otfabric/go-bacnet/actions/runs/30725263801) (job *Pinned release peers*) |
| bacnet-interop main compat | green | same run (job *bacnet-interop main compat*) |
| Pin | [`bacnet-interop` v0.4.2](https://github.com/otfabric/bacnet-interop/releases/tag/v0.4.2) | `interop/bacnet-interop-pin.json` |

### Notes

- No public API break intended; behaviour and decoder strictness only.
- BACnet4J still rejects segmented confirmed-request *receive* (reject reason 9);
  see [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md).

---

## v0.2.0

**Date:** 2026-08-02  
**Previous release:** [v0.1.1](https://github.com/otfabric/go-bacnet/releases/tag/v0.1.1)

### Summary

Supervisory-client breadth release: WritePropertyMultiple, ReadRange (including
typed Trend Log records), Who-Has / I-Have, EventNotification receive with typed
common NotificationParameters, AcknowledgeAlarm, GetEventInformation, opt-in
DeviceCommunicationControl / ReinitializeDevice, and windowed segmented
confirmed-request send. Pin moves to
[`bacnet-interop` v0.4.1](https://github.com/otfabric/bacnet-interop/releases/tag/v0.4.1)
(BACpypes3 **0.0.106**). Requires **Go 1.23+**.

Still **production-candidate** until the
[field validation](docs/FIELD_VALIDATION.md) is complete.

### Added

- **WritePropertyMultiple** service codecs + `Client.WritePropertyMultiple`
  (SimpleACK success; `*service.WritePropertyMultipleError` for first-failed
  write; exact-APDU retransmit disabled; outcome-unknown after send)
- **Segmented confirmed-request send** (proposed window 16, `WithSegmentWindow`):
  SegmentACK/NAK handling with windowed in-flight send, segment timeout → client
  Abort, peer Segmentation evidence gate, MaxSegments preflight; enables large
  WPM when the peer can receive segments. Segmented ComplexACK receive defaults
  to actual window 1 so peers that wait for SegmentACK before the next segment
  (BACpypes3 / BACnet4J) do not stall
- Confirmed-request / ComplexACK codecs: segmented PDUs carry service choice on
  every segment (constant across reassembly; BACpypes3/BACnet4J agree)
- **ReadRange** codecs + `Client.ReadRange` (byPosition / bySequenceNumber /
  byTime / all; resultFlags helpers; retransmit enabled)
- Typed **BACnetLogRecord** split: `service.LogRecord` /
  `ReadRangeACK.LogRecords` when property is `Log_Buffer`
- **Who-Has / I-Have** codecs + `SendWhoHas` / `DiscoverObjects` / `Objects()`
  with a bounded object-observation registry (shared retention options)
- **EventNotification** receive (Confirmed/Unconfirmed) + handler API;
  typed **NotificationParameters** for change-of-state / bitstring / value /
  out-of-range (opaque `NotificationParams` retained); **AcknowledgeAlarm**;
  **GetEventInformation**
- **DeviceCommunicationControl** / **ReinitializeDevice** (opt-in via
  `WithDeviceManagementEnabled`; outcome-unknown after send)
- Runnable examples: `examples/read-range`, `examples/write-multiple`,
  `examples/who-has`, `examples/events`
- Context helpers: `AppendContextSigned`, `AppendContextBitString`,
  `AppendContextCharacterString`, `AppendContextTime` (+ matching `Context*`)
- Constants: `ObjectTypeTrendLog`, `PropertyLogBuffer`
- Interop tests: three-peer WPM + Who-Has; BACpypes3 segmented WPM send;
  bacnet-stack/BACnet4J ReadRange; BACpypes3/BACnet4J EventNotification receive;
  three-peer ReinitializeDevice warmstart; bacnet-stack DCC enable;
  BACpypes3/BACnet4J segmented RPM ComplexACK receive
- Hermetic codec fixtures for the new services (via bacnet-interop)

### Fixed

- Routed interop harness: bind `bip-router` by static address→network (not
  Docker `eth0`/`eth1` order), which was dropping DNET forwards when iface
  order swapped after `create` + `network connect`

### Changed

- Pin → [`bacnet-interop` v0.4.1](https://github.com/otfabric/bacnet-interop/releases/tag/v0.4.1)
  (BACpypes3 **0.0.106**; bacnet-stack 1.6.0; BACnet4J 6.1.0; digest-pinned GHCR images)
- `DecodeReadRangeACK` populates `LogRecords` for Log_Buffer; otherwise trusts
  wire `itemCount` when complex item tag streams do not 1:1 match flat items

### Evidence

| Gate | Result | Link |
|---|---|---|
| Shared CI (lint, Go matrix, race, coverage, PR fuzz) | green on `8bee3e6` | [30722438815](https://github.com/otfabric/go-bacnet/actions/runs/30722438815) |
| Pinned interop `v0.4.1` | green | [30722438676](https://github.com/otfabric/go-bacnet/actions/runs/30722438676) (job *Pinned release peers*) |
| bacnet-interop main compat | green | same run (job *bacnet-interop main compat*) |
| Pin | [`bacnet-interop` v0.4.1](https://github.com/otfabric/bacnet-interop/releases/tag/v0.4.1) | `interop/bacnet-interop-pin.json` |

### Notes

- No public API break intended; additive services and options only.
- BACnet4J rejects segmented confirmed-request *receive* (reject reason 9);
  segmented WPM send evidence uses BACpypes3. See
  [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md).

---

## v0.1.1

**Date:** 2026-08-01  
**Previous release:** [v0.1.0](https://github.com/otfabric/go-bacnet/releases/tag/v0.1.0)

### Summary

Hardening and evidence release for release-grade **production-candidate**
status. Bounded device-observation retention, IPv4-only B/IP endpoint
validation, strict APDU/NPDU framing, hermetic CI coverage, PR and nightly
fuzz, and pin to [`bacnet-interop` v0.2.1](https://github.com/otfabric/bacnet-interop/releases/tag/v0.2.1)
(28 fixtures including service-layer negatives). Requires **Go 1.23+**.

Still not **production-usable** until the
[field validation](docs/FIELD_VALIDATION.md) is complete.

### Added

- `WithRegistryOptions` / bounded observation registry (TTL, global cap,
  per-instance path cap) with eviction diagnostics
- Window-scoped `Discover` results (`Devices()` remains the full snapshot)
- Strict APDU/NPDU framing rejections (reserved bits, undefined MaxAPDU,
  MoreFollows without SEG, invalid windows, global-broadcast DADR, router
  network lists)
- `npdu.DecodeNetworkList`
- CI **library coverage gate** (`make coverage`, hermetic pinned fixtures)
- CI **PR fuzz** and scheduled **nightly fuzz** (`fuzz-nightly.yml`)
- APDU/NPDU fuzz targets
- [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md) — positioning and evidence matrix

### Changed

- Horizon 1 `bip.Endpoint.IsValid` requires IPv4
- Inbound Distribute-Broadcast-To-Network ignored (client is not a BBMD)
- Diagnostic callbacks documented as synchronous
- Confirmed COV ACK-before-admission documented and tested
- Fixture expected-error helper supports multi-stage (APDU → service) failures
- Pin file → bacnet-interop `v0.2.1` digests
- PLAN / README: provisional PC until this tag; competitive positioning noted

### Evidence (Batch 4D)

| Gate | Result | Link |
|---|---|---|
| Shared CI (lint, Go 1.23–1.26, **race**) | green on `6256fd2` | [CI run](https://github.com/otfabric/go-bacnet/actions/runs/30705297706) |
| Library coverage gate (≥90%) | green | same run, job *Library coverage gate* |
| PR fuzz (15s/target) | green | same run, job *PR fuzz* |
| Pinned interop `v0.2.1` (run 1) | green | [30705297526](https://github.com/otfabric/go-bacnet/actions/runs/30705297526) |
| Pinned interop `v0.2.1` (run 2) | green | [30707779897](https://github.com/otfabric/go-bacnet/actions/runs/30707779897) |
| bacnet-interop main compat | green | both interop runs |
| Nightly fuzz (5m/target, manual) | green | [30707778922](https://github.com/otfabric/go-bacnet/actions/runs/30707778922) |
| Local `make test-race` + `make coverage` | 90.2% ≥ 90% | operator workstation, 2026-08-01 |

### Notes

- No public API break intended; additive options and stricter malformed
  rejection only.
- Segmented confirmed-request *send*, WPM, server/BBMD-server remain out of
  scope for this tag (added in later releases).

---

## v0.1.0

**Date:** 2026-08-01  
**Previous release:** none (first tagged release)

### Summary

First tagged release of a pure-Go BACnet/IP supervisory client and protocol
foundation. Provides BVLC/NPDU/APDU/service codecs, a UDP client runtime for
discovery and confirmed services, COV subscriptions, routed addressing, optional
foreign-device registration, and the `bacnetctl` CLI. Requires **Go 1.23+**.

Interoperability has been exercised against bacnet-stack, BACpypes3, and
BACnet4J peers (plus a bip-router topology aid) via digest-pinned images from
[`bacnet-interop`](https://github.com/otfabric/bacnet-interop) `v0.2.0`. This
is software-oracle evidence, not a claim of vendor or BTL certification.

### Added

#### Codecs and leaf types

- **BVLC** — Original-Unicast / Original-Broadcast / Forwarded-NPDU, BVLC-Result,
  Register-Foreign-Device encode/decode with length limits
- **NPDU** — local and routed (`DNET`/`DADR`, `SNET`/`SADR`) framing
- **APDU** — confirmed/unconfirmed request, SimpleACK, ComplexACK (including
  segmented receive), Error, Reject, Abort, SegmentACK
- **Service payloads** — Who-Is / I-Am, ReadProperty / ReadPropertyACK,
  ReadPropertyMultiple ACK (including per-property errors), WriteProperty
  (priority and NULL relinquish), SubscribeCOV / SubscribeCOVProperty,
  COV notification
- **Root types** — `Address` / `MAC`, object and property identifiers,
  application `Value` helpers, BACnet error class/code, decode limits

#### Client runtime

- **UDP BACnet/IP transport** on default port **47808** (`0xBAC0`), with
  injectable transport for tests
- **Who-Is / I-Am discovery** and a device observation registry (max APDU,
  segmentation, vendor)
- **ReadProperty**, **ReadPropertyMultiple**, **WriteProperty**
- **Confirmed-request transactions** — invoke IDs, timeouts, optional
  exact-APDU retransmission; abort / reject / error handling
- **Segmented ComplexACK reassembly** and SegmentACK exchange (segmented
  confirmed *request* send is not included)
- **Routed targets** — router cache, Who-Is-Router-To-Network, DNET/DADR path
  matching on responses
- **BBMD Forwarded-NPDU receive** and optional **foreign-device registration**
  (TTL renew / BVLC-Result correlation)
- **COV** — SubscribeCOV and SubscribeCOVProperty; confirmed and unconfirmed
  notifications; renew / cancel; subscription event channel with gap signalling
- **`bacnetctl`** — decode, discover, read, write, and version helpers

#### Documentation and evidence

- [PROTOCOL.md](PROTOCOL.md) — short BACnet primer mapped to this library
- [API.md](API.md), [ERRORS.md](ERRORS.md), [INTEROP.md](INTEROP.md),
  [SECURITY.md](SECURITY.md)
- Fixture corpus consumer for [`bacnet-interop`](https://github.com/otfabric/bacnet-interop)
  goldens (`BACNET_INTEROP_ROOT`)
- `-tags=interop` peer scenarios against pinned adapter digests
  (`interop/bacnet-interop-pin.json`)

### Not in this release

- Native MS/TP, BACnet/IPv6, BACnet/SC
- Full BBMD server / BDT management / multi-BBMD failover
- Full BACnet server or device object model
- Alarms, schedules, trends, WritePropertyMultiple
- Segmented confirmed-request *send*
- BTL certification or vendor hardware claims

### Notes

- Public APIs may still evolve in later v0.x tags; treat this as the first
  usable tagged baseline, not a stability freeze.
- Library unit coverage gate is **90%** (CLI and Docker interop packages
  excluded from the percentage).

---
