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

Pin: [`bacnet-interop` v0.13.0](https://github.com/otfabric/bacnet-interop/releases/tag/v0.13.0),
all eight images by digest (`interop/bacnet-interop-pin.json`). Against the
published images nothing fails; the skips are things a stack does not have
and scenarios that need a newer adapter release.

| Peer | Upstream | Language | Role |
|---|---|---|---|
| bacnet-stack | 1.6.1 | C | Device (+ BBMD) |
| BACpypes3 | 0.0.110 | Python | Device (+ BBMD) |
| BACnet4J | 6.1.1 | Java | Device (+ BBMD) |
| Worldiety | `3cb2aa80` | Go | Native transport and APDU layer; adapter object model |
| System.IO.BACnet | 4.0.0 | C# | Device; the stack behind YABE |
| cB-Sigma (`cbsigma`) | bacnet `v0.2.0` device role, bacnet-sim `v0.1.0` | Go | Device; a simulator with fault switches ([below](#cb-sigma)) |
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
`TestBACnetStackClientAgainstServer`, `TestBACnet4JClientAgainstServer` and
`TestBACpypes3ClientAgainstServer` start the server in a Go container, once
per scenario file, with the device fixture the file requires. They then run
the client image in client mode on the same docker network, on Linux and
Docker Desktop alike, because some clients bind to the device by Who-Is.
Each result line becomes a subtest: `pass` passes, `fail` and `infra-error`
fail, and `unsupported` and `known-difference` skip. For a scenario whose
expectation is that a request was sent (an unconfirmed service), the
server's hooks report what arrived and the test checks it. The images are
those of the pin (bacnet-interop v0.13.0; client mode exists from v0.12.0),
with each stack unmodified.

| Client | `server-baseline-v1` (23) | `server-services-v1` (36) | `server-bbmd-v1` (5) | `server-extended-v1` (23) |
|---|---|---|---|---|
| bacnet-stack 1.6.1, command-line tools | All pass | 35 pass; `rle.nc1` known difference | All pass | Not run: built without segmentation, no tools for these services |
| BACnet4J 6.1.1, `LocalDevice` | All pass | All pass | 2 pass; Read-BDT, Read-FDT, Write-BDT unsupported (no client for them) | 22 pass; `scovpm.unknown-object-second` known difference |
| BACpypes3 0.0.110, `Application` | All pass | 35 pass; `ale.unknown-object` known difference | All pass | 15 pass; 5 known differences; the 3 `scovpm` scenarios unsupported |

The known differences are limits of the client recorded in bacnet-interop
PEER_SUPPORT.md, each with the octets the server sent:

- `rle.nc1`: `bacrle` registers its reply handlers for AddListElement
  (apps/remove-list-element/main.c), so it prints nothing for the
  RemoveListElement acknowledgement. The next scenario reads
  Recipient_List back with `bacrp` and checks the removal.
- `scovpm.unknown-object-second`: BACnet4J decodes
  SubscribeCOVPropertyMultiple-Error as a CHOICE and drops the first failed
  subscription; error class and code match.
- BACpypes3 cannot decode a ChangeList-Error (`ale.unknown-object`), an
  Error of SubscribeCOVProperty (`scovp.unknown-object`,
  `scovp.unknown-property`) or an empty GetAlarmSummary or
  GetEnrollmentSummary ACK (`gas.normal`, `gas.av2.after-normal`,
  `ges.offnormal-filter.normal`), and has no SubscribeCOVPropertyMultiple.

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

BACnet/IPv6 and BACnet/SC are out of scope: go-bacnet is a BACnet/IP (IPv4)
client.

## What the peer tests found

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
