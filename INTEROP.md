# Interoperability

What `go-bacnet` is tested against, scenario by scenario, and how to run it.
What each peer stack can and cannot do is recorded in
[bacnet-interop PEER_SUPPORT.md](https://github.com/otfabric/bacnet-interop/blob/main/PEER_SUPPORT.md).

All of this is evidence against open-source stacks in containers. None of it
is evidence about a particular vendor's controller; see
[docs/FIELD_VALIDATION.md](docs/FIELD_VALIDATION.md). Recorded traffic of
physical controllers (Trane, Tridium, Reliable Controls, Delta, Alerton,
WattStopper, Lithonia and others) is a separate evidence tier, in
[testdata/captures](testdata/captures/README.md): it runs offline in
`make test`, not against the devices.

## How it works

`go-bacnet` owns the assertions (`interop/`, build tag `interop`). The peer
images, device fixtures and codec vectors come from
[`bacnet-interop`](https://github.com/otfabric/bacnet-interop). CI checks out
the release pinned in `interop/bacnet-interop-pin.json`, pulls its images by
digest and runs every test against them; a second lane builds the adapters
from bacnet-interop `main`.

Each test starts its own peer container, so tests do not depend on one
another. A peer that behaves differently from the others is not skipped: the
difference is asserted, so a change in the peer is noticed.

Three kinds of evidence, in decreasing strength:

| Mark | Meaning |
|---|---|
| ✅ | The peer stack executes the service and the result is asserted |
| A | As ✅, but the adapter around the stack supplies the semantics (object model, handler); the stack supplies encoding and transport |
| D | Codec evidence: the stack's own decoder accepted the client's request and reported the decoded fields, or its own encoder produced a PDU the client decoded. Nothing is executed |
| W | No stack has even a codec; the request layout is checked against Wireshark's dissector |
| ⚠ | The peer deviates from the standard or from the other peers; the deviation is asserted |
| — | The peer does not serve it |

## Running it

```bash
make interop-pinned   # pull the pinned images, run everything, fail on anything missing
make interop          # run against locally built images (bacnet-interop `make build`), skip what is missing
make dissector        # check client requests and server replies against Wireshark (needs tshark, no Docker)
```

The interop targets need Docker and a `bacnet-interop` checkout next to this
repository (or `BACNET_INTEROP_ROOT`). `INTEROP_RUN='TestReadRangeAllPager'`
selects tests; `interop/doc.go` lists the environment variables. On Linux the
tests talk to the peer containers directly; on Docker Desktop each test
re-executes inside a Go container on the peer's network, and a full run takes
eight to nine minutes.

A scenario that needs an adapter feature newer than the image it is given
skips with a message naming the bacnet-interop version it needs
(`requireAdapterVersion`), so an older pin stays green instead of failing.

## Peers

Pin: [`bacnet-interop` v0.14.0](https://github.com/otfabric/bacnet-interop/releases/tag/v0.14.0),
all eleven images by digest (`interop/bacnet-interop-pin.json`). Against the
published images nothing fails; the skips are things a stack does not have
and scenarios that need a newer adapter release.

Last full run: `make interop-pinned` on 2026-11-12 against bacnet-interop
v0.14.0 (go-bacnet `67ff46a`, darwin/arm64, Docker 29.9.3, 53 minutes):
279 tests pass, 0 fail, 14 skip; with subtests 821 pass, 0 fail, 135 skip.
Each test and the reason of each skip:
[`interop/results/pinned-v0.14.0.json`](interop/results/pinned-v0.14.0.json).

| Peer | Upstream | Language | Role |
|---|---|---|---|
| bacnet-stack | 1.6.1 | C | Device (+ BBMD) |
| BACpypes3 | 0.0.110 | Python | Device (+ BBMD) |
| BACnet4J | 6.1.1 | Java | Device (+ BBMD) |
| Worldiety | `3cb2aa80` | Go | Native transport and APDU layer; adapter object model |
| System.IO.BACnet | 4.0.0 | C# | Device; the stack behind YABE |
| cB-Sigma (`cbsigma`) | bacnet `v0.2.0` device role, bacnet-sim `v0.1.0` | Go | Device; a simulator with fault switches ([below](#cb-sigma)) |
| bacnet-js (`bacnet-js`) | `@bacnet-js/client` 3.3.2 | TypeScript (Node.js) | Device (adapter object model) and client ([below](#bacnet-js)) |
| ulbios (`ulbios`) | ulbios/bacnet `227d6227` (no tags upstream) | Go | Device and client; Who-Is, ReadProperty and WriteProperty only ([below](#ulbios)) |
| Legacy BACpypes (`bacpypes`) | PyPI `bacpypes` 0.19.0 | Python | Device (+ BBMD) and client ([below](#legacy-bacpypes)) |
| bacnet-stack-router | bacnet-stack 1.6.1 `apps/router` | C | Native BACnet/IP router |
| bip-router | — | Python | Topology aid written for the tests; not evidence about a stack |

Columns below: **stack** = bacnet-stack, **BP3** = BACpypes3, **4J** =
BACnet4J, **Wy** = Worldiety, **SIO** = System.IO.BACnet.

## Discovery and addressing

| Scenario | stack | BP3 | 4J | Wy | SIO | Notes |
|---|:---:|:---:|:---:|:---:|:---:|---|
| Who-Is (unicast) → I-Am | ✅ | ✅ | ✅ | ✅ | ✅ | |
| `Discover` by broadcast, then read through `DeviceObservation.Target()` | ✅ | ✅ | ✅ | ✅ | ✅ | The peers differ in which broadcast address they receive: bacnet-stack only the subnet-directed one, BACnet4J only 255.255.255.255. The client sends both |
| Who-Has → I-Have | ✅ | ✅ | ✅ | A | A | |
| `Target` with only an endpoint | ✅ | ✅ | ✅ | ✅ | ✅ | |
| ReadProperty of the Device wildcard (4194303) | ✅ | ⚠ | ✅ | ⚠ | ⚠ | BACpypes3 and the Worldiety adapter answer unknown-object; System.IO.BACnet answers, echoing the wildcard in its ACK |
| Who-Am-I / You-Are | D | — | — | — | — | bacnet-stack decodes both |

## Reading and writing

| Scenario | stack | BP3 | 4J | Wy | SIO | Notes |
|---|:---:|:---:|:---:|:---:|:---:|---|
| ReadProperty | ✅ | ✅ | ✅ | A | ✅ | |
| ReadProperty of an empty list | ✅ | ✅ | ✅ | | ✅ | Empty value `3E 3F`; also inside ReadPropertyMultiple, except on System.IO.BACnet, which cannot encode it there |
| ReadPropertyMultiple, with a per-property error | ✅ | ✅ | ✅ | A | ✅ | |
| ReadPropertyMultipleBatched (split by property, merge) | ✅ | ✅ | ✅ | A | ✅ | |
| ReadPropertyMultipleBatched shrinking after an Abort | ✅ | | | | | bacnet-stack with a 50-octet client APDU |
| ReadObjectList | ✅ | ✅ | ✅ | A | ✅ | |
| ReadObjectList element by element after an Abort | ✅ | | | | | 47-entry list, 50-octet APDU, no segmentation |
| device.ReadSnapshot | ✅ | ✅ | ✅ | A | ✅ | |
| WriteProperty + readback | ✅ | ✅ | ✅ | A | ✅ | |
| WritePriority / RelinquishPriority | — | ✅ | ✅ | ⚠ | ✅ | The bacnet-stack and Worldiety adapters' Analog Value is not commandable (Worldiety takes the write as a plain one and refuses the relinquish); System.IO.BACnet reads a commanded slot back as a string |
| WritePropertyMultiple + readback | ✅ | A | ✅ | A | A | |
| WritePropertyMultiple failing on a later element | ✅ | ⚠ | ✅ | A | ⚠ | Every peer applies the writes before the failure and none after. The BACpypes3 adapter and System.IO.BACnet answer a plain Error without the failed element |
| ReadRange by position | ✅ | — | ✅ | A | A | |
| ReadRange by sequence number and by time | ✅ | — | ✅ | A | A | Exact records, flags and first sequence number; the stacks differ for a reference outside the buffer |
| ReadRangeAll paging to LAST_ITEM | ✅ | — | ✅ | A | A | By position and by sequence number |
| AtomicReadFile / AtomicWriteFile, stream | ✅ | — | ✅ | — | A | |
| AtomicReadFile / AtomicWriteFile, record | ✅ | — | ✅ | — | A | |
| ReadFileStream / WriteFileStream (chunked) | ✅ | — | ✅ | — | A | |
| File access with the wrong access method | ✅ | — | ✅ | — | | Refused with Error services/invalid-file-access-method (bacnet-stack 1.6.0 executed it) |
| CreateObject / DeleteObject | ✅ | — | ✅ | — | — | |
| AddListElement / RemoveListElement | ✅ | — | ✅ | — | — | Notification Class recipient list |

## COV, events and alarms

| Scenario | stack | BP3 | 4J | Wy | SIO | Notes |
|---|:---:|:---:|:---:|:---:|:---:|---|
| SubscribeCOV, notification, cancel | ✅ | ✅ | ✅ | — | A | Unconfirmed and confirmed notifications |
| COV renewal past the lifetime | ✅ | ✅ | ✅ | — | A | A notification still arrives after the original lifetime |
| SubscribeCOVProperty | — | — | ✅ | — | A | |
| SubscribeCOVPropertyMultiple (subscribe, renew, cancel) | — | D | D | — | — | BACpypes3 decodes the three requests of a managed subscription and answers SimpleACK; BACnet4J parses the request and the cancellation and then rejects them |
| COVNotificationMultiple (received) | — | — | D | — | — | BACnet4J's own encoder, confirmed and unconfirmed; timestamp and time-of-change delivered as sent; the client's SimpleACK is seen by BACnet4J |
| EventNotification received | — | A | A | — | A | |
| GetAlarmSummary | ✅ | — | ✅ | — | — | |
| GetEventInformation | ✅ | — | ✅ | — | A | |
| AcknowledgeAlarm | ✅ | — | ✅ | — | A | With the reported time stamp; a stale one is refused (Error on BACnet4J, Abort on bacnet-stack) |
| GetEnrollmentSummary | — | A | ✅ | — | — | Every filter decoded by two implementations |
| Audit notifications (received) | D | D | — | — | — | Two independent upstream encoders; the confirmed one completes on the client's SimpleACK |
| AuditLogQuery | D | D | — | — | — | BACpypes3 decodes the query; both answer with records from their own encoders |

## Device management and other services

| Scenario | stack | BP3 | 4J | Wy | SIO | Notes |
|---|:---:|:---:|:---:|:---:|:---:|---|
| DeviceCommunicationControl enable | ✅ | ✅ | ✅ | — | — | Opt-in in the client |
| DeviceCommunicationControl disable | ⚠ | ⚠ | ✅ | — | — | bacnet-stack refuses it; BACpypes3 acknowledges and keeps answering; BACnet4J goes silent |
| DeviceCommunicationControl disable-initiation | ✅ | ✅ | ✅ | — | — | The device keeps answering |
| DeviceCommunicationControl password | | ✅ | ✅ | — | — | |
| ReinitializeDevice warmstart | ✅ | A | A | — | A | Opt-in in the client |
| TimeSynchronization / UTCTimeSynchronization | ✅ | ✅ | ✅ | — | A | |
| TextMessage | — | ✅ | ✅ | — | — | |
| PrivateTransfer | ✅ | ✅ | ✅ | — | — | Confirmed: BACpypes3 and BACnet4J |
| WriteGroup | ✅ | ✅ | — | — | — | |
| LifeSafetyOperation | ✅ | — | ✅ | — | — | |
| VT-Open / VT-Data / VT-Close | — | D | — | — | — | Requests and the VT-Open and VT-Data ACKs; no session exists |
| AuthRequest | W | W | W | W | W | No stable stack implements it; each peer's refusal is asserted |

## Outcomes

| Scenario | stack | BP3 | 4J | Wy | SIO | Notes |
|---|:---:|:---:|:---:|:---:|:---:|---|
| Error: unknown-property, unknown-object | ✅ | ✅ | ✅ | A | ✅ | |
| Error: write-access-denied on a read-only property | ✅ | ⚠ | ✅ | A | A | BACpypes3 accepts the write |
| Error with a foreign error-choice | | | ✅ | | | BACnet4J sends 127; the client still delivers the Error |
| Plain Error in answer to WritePropertyMultiple | | ✅ | | | ✅ | Delivered as `*ErrorResponse` |
| Reject: unrecognized-service | ✅ | — | ✅ | | ✅ | |
| Abort | ✅ | ✅ | | | ✅ | segmentation-not-supported; System.IO.BACnet sends Abort without the server bit |
| No answer: read times out after timeout × attempts, write ends outcome-unknown, a context deadline is honoured | ✅ | | ✅ | | | Against an unused port, and against BACnet4J muted with DeviceCommunicationControl |

## Transport and network

| Scenario | stack | BP3 | 4J | Wy | SIO | Notes |
|---|:---:|:---:|:---:|:---:|:---:|---|
| Segmented ComplexACK receive | — | ✅ | ✅ | ⚠ | ✅ | bacnet-stack cannot segment (Abort); Worldiety omits the service choice on continuation segments |
| Segmented confirmed-request send | — | ✅ | ⚠ | ⚠ | ✅ | BACnet4J rejects segmented requests |
| Routed device through bip-router | ✅ | ✅ | ✅ | — | ✅ | |
| Routed device through bacnet-stack-router | ✅ | ✅ | ⚠ | — | ✅ | Who-Is-Router-To-Network, `ResolveTarget`, routed read, global Who-Is; an I-Am carrying both DNET and SNET/SADR from the bacnet-stack device. BACnet4J serves routed requests but does not hear the routed global Who-Is (a subnet broadcast) |
| Foreign-device registration with the peer as BBMD | ✅ | ✅ | ✅ | — | — | |
| Read-BDT, Read-FDT, Delete-FDT entry | ✅ | ✅ | ✅ | — | — | |
| Write-BDT | ⚠ | ⚠ | ✅ | — | — | bacnet-stack and BACpypes3 answer NAK, which is asserted |

An empty cell means the scenario is not run against that peer.

## Dissector oracle

`make dissector` captures every request the client can emit (one or more
per service) from a recording transport and has
Wireshark's `tshark` dissect them. It fails on a frame Wireshark calls
malformed or flags with an expert warning, and, for the requests whose layout
is easy to get wrong, when Wireshark does not find the named fields where the
standard puts them (SubscribeCOVPropertyMultiple, GetEnrollmentSummary
filters including the enrollment filter, AcknowledgeAlarm, LifeSafetyOperation, DeviceCommunicationControl,
AuditLogQuery, Who-Am-I, You-Are, CreateObject).

The same target checks the server's side: `TestDissectorAcceptsServerReplies`
runs the server end-to-end tests (`e2e/server*_test.go`) with
`BACNET_SERVER_FRAMES_DIR` set, which records every datagram the server sends
(ComplexACK, SimpleACK, Error, Reject, segmented replies, I-Am, I-Have,
COV, COV-multiple and event notifications, BVLC results, Read-BDT and
Read-FDT acks), and has tshark dissect them with the same malformed and
expert checks. Server Abort and SegmentACK frames are not in that set.

Limits: it does not check the client's decoding of responses; Wireshark
walks BACnet tags leniently, so a request without listed fields is only
checked for well-formed tags and the right service; and agreement with
Wireshark is not acceptance by a device. Verified with Wireshark 4.2, 4.4 and
4.6.

## Independent clients against the server

The tests above drive peer devices with the go-bacnet client. The reverse
direction runs a peer stack as a **client** against the go-bacnet server
(bacnet-interop `docs/CLIENT_DRIVER_CONTRACT.md`).
`TestBACnetStackClientAgainstServer`, `TestBACnet4JClientAgainstServer`,
`TestBACpypes3ClientAgainstServer`, `TestBACpypesClientAgainstServer`,
`TestBacnetJSClientAgainstServer` and `TestUlbiosClientAgainstServer` start the server in a Go container, once
per scenario file, with the device fixture the file requires. They then run
the client image in client mode on the same docker network, on Linux and
Docker Desktop alike, because some clients bind to the device by Who-Is.
Each result line becomes a subtest: `pass` passes, `fail` and `infra-error`
fail, and `unsupported` and `known-difference` skip. For a scenario whose
expectation is that a request was sent (an unconfirmed service), the
server's hooks report what arrived and the test checks it. The images are
those of the pin (bacnet-interop v0.14.0; client mode exists from v0.12.0),
with each stack unmodified.

| Client | `server-baseline-v1` (23) | `server-services-v1` (36) | `server-bbmd-v1` (5) | `server-extended-v1` (23) |
|---|---|---|---|---|
| bacnet-stack 1.6.1, command-line tools | All pass | 35 pass; `rle.nc1` known difference | All pass | Not run: built without segmentation, no tools for these services |
| BACnet4J 6.1.1, `LocalDevice` | All pass | All pass | 2 pass; Read-BDT, Read-FDT, Write-BDT unsupported (no client for them) | 22 pass; `scovpm.unknown-object-second` known difference |
| BACpypes3 0.0.110, `Application` | All pass | 35 pass; `ale.unknown-object` known difference | All pass | 15 pass; 5 known differences; the 3 `scovpm` scenarios unsupported |
| bacnet-js 3.3.2, `BACnetClient` | 21 pass; `wpm.unknown-object-second`, `wpm.wrong-type` known differences | 29 pass; 5 unsupported (`ale.nc1`, `ale.unknown-object`, `rle.nc1`, `co.not-creatable`, `wg.group1`), 2 not tested (their read-backs) | 2 pass; Read-BDT, Read-FDT, Write-BDT unsupported | 13 pass; 9 unsupported (segmented request, `scovpm`, filtered GetEnrollmentSummary, text messages), 1 not tested |
| ulbios `227d6227` | 5 pass, 5 known differences, 13 unsupported | 4 known differences, 32 unsupported | 1 known difference, 4 unsupported | 2 known differences, 21 unsupported |
| Legacy BACpypes 0.19.0, `Application` | All pass | All pass | All pass | 20 pass; the 3 `scovpm` scenarios unsupported (no APDU) |

The known differences are limits of the client recorded in bacnet-interop
PEER_SUPPORT.md, each with the octets the server sent:

- `rle.nc1`: `bacrle` registers its reply handlers for AddListElement
  (apps/remove-list-element/main.c), so it prints nothing for the
  RemoveListElement acknowledgement. The next scenario reads
  Recipient_List back with `bacrp` and checks the removal.
- `scovpm.unknown-object-second`: BACnet4J decodes
  SubscribeCOVPropertyMultiple-Error as a CHOICE and drops the first failed
  subscription; error class and code match.
- ulbios issues Who-Is (without limits), ReadProperty and WriteProperty
  only; everything else is unsupported. It cannot decode a CharacterString
  (`rp.device.object-name`, `rp.av1.object-name`), a ReadProperty-ACK
  shorter than its own REAL one (`rp.av1.object-type`,
  `rp.bv1.present-value`, the empty Recipient_List), and it decodes an
  object identifier's type shifted left by two (`rp.av1.object-identifier`).
  Its own WriteProperty encoder (`NewWriteProperty` of ulbios' root package, used without a priority)
  carries a REAL followed by a NULL in the value; the server answers
  property/invalid-data-type (`wp.av2.high`, `wp.av2.normal` in two
  files), where the bacnet-stack and BACnet4J devices acknowledge it and the
  BACpypes3 device sends Reject invalid-tag. Clause 15.9.1.1.3 has
  one value of the property's datatype, so the server's answer stands.
- BACpypes3 cannot decode a ChangeList-Error (`ale.unknown-object`), an
  Error of SubscribeCOVProperty (`scovp.unknown-object`,
  `scovp.unknown-property`) or an empty GetAlarmSummary or
  GetEnrollmentSummary ACK (`gas.normal`, `gas.av2.after-normal`,
  `ges.offnormal-filter.normal`), and has no SubscribeCOVPropertyMultiple.
- bacnet-js decodes a WritePropertyMultiple-Error with its plain Error
  decoder (`client.ts _processError`), so the first failed write is lost
  and class and code come out wrong (`wpm.unknown-object-second`,
  `wpm.wrong-type`). Its unsupported scenarios are requests it has no
  encoder for (list elements of a BACnetDestination, CreateObject by type,
  WriteGroup, text messages, COV-multiple, BVLC tables, a filtered
  GetEnrollmentSummary) or cannot segment (a 3000-octet AtomicWriteFile).
  Its 3000-octet AtomicReadFile takes the server's segmented ComplexACK
  (`arf.file3.segmented-reply` passes).

Also not covered: an empty GetEventInformation reply, which `bacge` cannot
decode (also against a bacnet-stack device), and BBMD-to-BBMD forwarding.
`server-baseline-v1` passes with every client against the bacnet-stack
device image too (`make -C ../bacnet-interop client-matrix`), which cross-checks
those expectations. Where the standard leaves the error code open, the
server follows bacnet-stack ([docs/SERVER_SUPPORT.md](docs/SERVER_SUPPORT.md)),
so agreement on those codes is partly by construction.

Defects these runs found in the server, fixed before v0.6.0: a request
repeated after its response was answered from a cache instead of being
executed (the tools reuse invoke IDs, and clause 5.4.5 makes such a request
a new one), so a second DeleteObject reported success; and AtomicWriteFile
past the end of a file was refused where bacnet-stack's storage extends the
file. The BACpypes3 runs found two more: segment 0 of a segmented request
was not acknowledged when the actual window was above 1 (clause 5.4.5
acknowledges it at once), and BACpypes3's segments, 6 octets longer than the
server's Max APDU, were dropped by the decode limit.

## cB-Sigma

`interop/cbsigma_test.go`, against bacnet-interop's `cbsigma` image (from
v0.13.0). Two roles of one image: `CBSIGMA_ROLE=device` is the `device`
package of [cB-Sigma/bacnet](https://github.com/cB-Sigma/bacnet), and
`CBSIGMA_ROLE=sim` is [cB-Sigma/bacnet-sim](https://github.com/cB-Sigma/bacnet-sim),
a test device with no Device object and switches that make it misbehave.
Both are Go, by one author, independent of go-bacnet.

| Scenario | device | sim |
|---|:---:|:---:|
| Who-Is / I-Am; Who-Has by name | ✅ | ✅ Who-Is only |
| Device object-name, object-list | ✅ | — (no Device object) |
| ReadProperty, ReadPropertyMultiple, their errors | ✅ | ✅ |
| Segmented RPM answer, receive window 1, 2, 4 and 16 | ✅ | |
| WriteProperty at a priority, relinquish; write-access-denied | ✅ | ✅ write |
| WritePropertyMultiple; segmented WPM request | ✅ | ✅ |
| COV subscribe, notify, cancel | ✅ | ✅ |
| ReadRange by position, sequence number and time; paged | | ✅ |
| ReadRange by sequence number refused | | ✅ |
| Segmented ReadRange answer, window 1 and 4 | | ✅ |
| A segment left out once | | ⚠ recovered with window 4 (NAK); with window 1 the client times out: the simulator never resends on its own |
| A segmented answer stopped | | ✅ Abort tsm-timeout, the next request succeeds |
| RPM and WPM rejected | | ✅ |
| GetAlarmSummary, GetEventInformation, AcknowledgeAlarm | | ✅ (the answers are the adapter's) |
| Foreign-device registration, Distribute-Broadcast-To-Network, its NAK | | ✅ |
| ReinitializeDevice, DCC with a wrong password, time synchronization, unconfirmed text message and private transfer | | ✅ |

## ulbios

`interop/ulbios_test.go`, against bacnet-interop's `ulbios` image (new in
v0.14.0, `ULBIOS_IMAGE`, "none" skips). [ulbios/bacnet](https://github.com/ulbios/bacnet) is a small MIT Go
library, independent of go-bacnet, pinned by commit `227d6227` (it has no
tags). Its parser knows Who-Is, I-Am, ReadProperty, WriteProperty,
Simple-ACK, Complex-ACK and Error; its encoders REAL, NULL, one-octet
Enumerated, two-octet Unsigned and object identifiers. It has no Reject or
Abort encoder, so whatever it cannot decode or answer gets no reply; the
adapter reports each such request (`"operation":"unanswered"`) and the
tests assert that line.

| Scenario | Result |
|---|---|
| Who-Is unicast without limits, with a range that holds the device, and two that do not | ✅ I-Am with max APDU 1024, no segmentation, vendor 999; none outside the range |
| `Discover` by broadcast, then read through `DeviceObservation.Target()` | ✅ ulbios' broadcast I-Am (NPDU DNET 0xFFFF) |
| ReadProperty of every property ulbios encodes: object identifier, type and list, vendor, max APDU, segmentation, protocol version, system status, present value (REAL and Enumerated), event state, units, relinquish default, priority array | ✅ Protocol_Version comes as `22 00 01`, two octets where clause 20.2.4 asks for one; go-bacnet decodes it |
| ReadProperty errors: unknown object, unknown property (Object_Name, Status_Flags, Out_Of_Service: ulbios has no encoder for them) | ✅ |
| WriteProperty at priority 8, without priority (slot 16), priority 1 (`WritePriority`), a Binary Value; the priority array afterwards | ✅ |
| WriteProperty errors: unknown object, unknown property, write-access-denied, invalid-data-type (Enumerated, Unsigned, REAL to the wrong object), value-out-of-range | ✅ definite Errors, not outcome-unknown |
| A property identifier above 255, an array index (read and write), NULL (relinquish), CharacterString, Boolean, priority 17 | ⚠ no reply: a read ends in `ErrTimeout`, a write as outcome-unknown. Clause 18.8 would have a Reject |
| ReadPropertyMultiple, ReadRange, an unrecognized service; WritePropertyMultiple, SubscribeCOV, DeviceCommunicationControl, ReinitializeDevice | ⚠ no reply: `ErrTimeout`; outcome-unknown for the four that change state (client service policy). The device still answers afterwards |
| An Object_List longer than the client's Max_APDU (fixture v9, 206 octets) | ⚠ no reply and no Abort (clause 5.4.5), so `ReadObjectList`, which falls back to element reads on an Abort only, ends in `ErrTimeout` |
| A request with segmented-response-accepted set, with `ULBIOS_KEEP_SA=1` | ⚠ ulbios' parser does not recognise it (it compares the whole first octet); the same request without SA is answered. The default image clears the bit (adapter-shim) |

The ⚠ rows are observations: the subtest asserts go-bacnet's outcome and
the adapter's reason, then skips. Unknown-object and write-access-denied
also run in the cross-peer `TestReadPropertyUnknownObjectError` and
`TestWritePropertyReadOnlyError`.

BACnet/IPv6 and BACnet/SC are out of scope: go-bacnet is a BACnet/IP (IPv4)
client.

## bacnet-js

`interop/bacnetjs_test.go`, against bacnet-interop's `bacnet-js` image
(new in v0.14.0; `BACNETJS_IMAGE`, default `bacnet-interop-bacnet-js:local`,
"none" skips). Upstream is [@bacnet-js/client](https://github.com/bacnet-js/client)
3.3.2 (TypeScript, MIT, the node-bacstack lineage), independent of go-bacnet.
It decodes every request and encodes every answer; it has no object model
and no service procedures, so those are the adapter's (A below). The suite
runs on `device-baseline-v2` and, for ReadRange, files, object lifecycle and
alarms, on `device-server-v1`. bacnet-js is also in the cross-peer tests
(wildcard Device instance, endpoint-only target, object list, batched RPM,
relinquish, read-only write, snapshot, unknown object, ReadRange pager, COV
renewal, broadcast discovery).

| Scenario | bacnet-js |
|---|:---:|
| Who-Is in and out of range, Who-Has by name, by identifier, unknown name | ✅ |
| ReadProperty, object-list (whole, `[0]`, invalid index), wildcard Device instance, errors | A |
| ReadPropertyMultiple with per-property errors; ALL | A |
| WriteProperty, WritePriority, relinquish, wrong type, write-access-denied | A |
| WritePropertyMultiple of two specifications; WritePropertyMultiple-Error naming the first failed write; the earlier write stays done | A |
| Segmented WPM request (client segments for a 50-octet target) | ✅ reassembled by upstream |
| RPM ALL to a client that accepts 50 octets | ✅ Abort segmentation-not-supported (no segmented send upstream) |
| SubscribeCOV confirmed and unconfirmed, SubscribeCOVProperty with increment, errors | A |
| ReadRange by position (positive and negative count), sequence number, time; paged; errors | A |
| AtomicReadFile / AtomicWriteFile stream and record, append, errors, 3000 octets in one read refused, in chunks read | A |
| CreateObject, DeleteObject and their errors | A |
| GetEventInformation, GetEnrollmentSummary, AcknowledgeAlarm and their errors | A |
| GetEnrollmentSummary with an event-state filter | ⚠ offnormal answered; normal gets no answer |
| GetAlarmSummary with an alarm | ⚠ go-bacnet refuses the ACK (context tags) |
| ReinitializeDevice, DeviceCommunicationControl | A |
| TimeSynchronization, UTC time synchronization, Confirmed/UnconfirmedPrivateTransfer | ✅ |
| AddListElement, RemoveListElement | ✅ Error services/reject-unrecognized-service |
| ConfirmedTextMessage | — no answer |

The two ⚠ rows are upstream defects, recorded as such and asserted so a
change is noticed (docs/COMPATIBILITY.md). Neither is a go-bacnet defect:
`AlarmSummary.encode` writes the object identifier, alarm state and
acknowledged transitions with context tags 12, 9 and 8
(`cc 00 80 00 02 99 03 8a 05 60` for AV-2) where clause 13.10.2 has
application tags, and `GetEnrollmentSummary.decode` treats the primitive
`[2]` event-state filter of clause 13.11.1.1.3 as an opening tag: `29 00`
decodes by accident, `29 02` throws inside upstream and the request is
dropped.

## Legacy BACpypes

`interop/bacpypes_test.go`, against bacnet-interop's `bacpypes` image
(new in v0.14.0; `BACPYPES_IMAGE`, "none" skips). PyPI
`bacpypes` 0.19.0, the asyncore code base that BACpypes3 replaced: the same
author, a separate package and repository. Agreement between it and
BACpypes3 is weaker evidence than agreement with an unrelated stack.

| Scenario | Result |
|---|---|
| Who-Is / I-Am (range, unicast, broadcast); Who-Has by name and identifier | ✅ |
| ReadProperty, ReadPropertyMultiple, their errors | ✅ |
| WriteProperty at a priority, relinquish | ✅ |
| WriteProperty of the wrong datatype; to Description | ⚠ Reject invalid-tag; write-access-denied (read-only in BACpypes) |
| WritePropertyMultiple, firstFailedWriteAttempt | A |
| Segmented RPM answer, seven device/client window pairs, device Max APDU 128 | ✅ |
| Segmented WPM request | ✅ |
| Segmented answer at client Max APDU 1476 | ⚠ 1481-octet segments; see below |
| SubscribeCOV confirmed and unconfirmed; SubscribeCOVProperty | ✅; ⚠ an unknown property is accepted |
| AtomicReadFile / AtomicWriteFile, stream and record, 3000 octets segmented | ✅ (contents A) |
| ReadRange by position and sequence number | A |
| DCC disable-initiation (Who-Is still answered), password; DCC disable | ✅; ⚠ acknowledged, then silent until enable |
| ReinitializeDevice; messaging services | A |
| CreateObject, DeleteObject, event, alarm and enrollment summaries | ✅ Reject unrecognized-service |
| Read-BDT, Read-FDT, Delete-FDT, FD registration, segmented answer as BBMD | ✅ |
| FDT remaining time; Write-BDT; Delete-FDT of an absent entry | ⚠ TTL + 5 (clause J.5.2.3: + 30); NAK 0x0010; NAK 0x0050 |

**Oversized segments (peer behaviour):** BACpypes fills each segment of a
ComplexACK with Max-APDU-accepted octets of service data and puts the
5-octet header in front. A raw-socket probe recorded segments of 1481, 1481
and 62 octets for the 3000-octet file at 1476 advertised (1029, 1029 and 966
at 1024). Clause 20.1.2.5 bounds an APDU by the receiver's
Max_APDU_Length_Accepted. The go-bacnet client advertises 1476 and by
default receives APDUs up to the datagram bound, as the server does
(BACpypes3 sends such segments too), so the read succeeds;
`TestBACpypesOversizedSegments` asserts the answer at the default limits and
the timeout with `DecodeLimits.MaxAPDUSize` 1476, which drops the longer
segments.

## Against our own released image

`interop/otfabric_test.go` runs this commit against
`ghcr.io/otfabric/bacnet-interop-otfabric` (bacnet-interop `own/go-bacnet`),
which is the latest go-bacnet release behind the bacnet-interop contracts. The
released client runs every scenario file against this commit's server, and
this commit's client reads the released device. The test runs only with
`BACNET_INTEROP_OTFABRIC_IMAGE` set, which the manual **Self-interop** workflow
does. It finds a wire change that breaks an earlier release. It is not
independent evidence, because both sides are go-bacnet, so it adds nothing to
the peer results above. The released client reports `reinit.undefined-state`
as `unsupported`, because its `service.EncodeReinitializeDevice` refuses state 9.

## What the peer tests found

Legacy BACpypes sends segments 5 octets longer than the Max APDU the client
advertises (above, "Oversized segments"); up to v0.7.0 the client dropped
them at its default decode limit and the read timed out. Fixed in v0.8.0:
the client receives APDUs up to the datagram bound by default, as the server
does; `TestSegmentLongerThanAdvertisedMaxAPDU` keeps it fixed.

With a receive window above 1 (`client.WithSegmentWindow`), go-bacnet up to
v0.5.x did not acknowledge segment 0 of a segmented answer, so every one
timed out; found against cB-Sigma and BACpypes3, fixed in v0.6.0, and
`TestSegmentedAnswerWithReceiveWindow` keeps it fixed.

Defects in go-bacnet that only showed up against real stacks, all fixed in
v0.4.0 ([RELEASE.md](RELEASE.md)):

- Broadcasts went to 255.255.255.255 only and reached two of four stacks.
- An empty list could not be read (every stack sends `3E 3F`).
- `ReadRangeAll` stopped after the first page.
- The audit codecs did not match what bacnet-stack and BACpypes3 encode.
- An Error with a foreign error-choice, and a plain Error in answer to
  WritePropertyMultiple, were dropped or reported as malformed.
- Router-Busy and Router-Available were swapped; the NPDU hop count sat in
  the wrong place when both addresses were present.
