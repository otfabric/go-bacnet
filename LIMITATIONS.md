# Limitations

What go-bacnet does not do, or does with restrictions. Per-capability status
and evidence: [docs/CLIENT_SUPPORT.md](docs/CLIENT_SUPPORT.md) and
[docs/SERVER_SUPPORT.md](docs/SERVER_SUPPORT.md). Peer quirks:
[docs/COMPATIBILITY.md](docs/COMPATIBILITY.md).

## Certification and validation

- **Nothing here is certified.** There is no BTL listing and no formal PICS;
  [docs/CLIENT_PROFILE.md](docs/CLIENT_PROFILE.md) is a descriptive profile.
- **No physical devices are recorded.** Evidence comes from five open-source
  stacks in containers and from Wireshark's dissector
  ([INTEROP.md](INTEROP.md)). The checklist for field runs is
  [docs/FIELD_VALIDATION.md](docs/FIELD_VALIDATION.md).

## Not implemented

- A BACnet router (no routing between networks, for client or server).
- Server objects beyond Device, analog/binary/multi-state points,
  Notification Class, Trend Log, File, Calendar, Schedule, CharacterString,
  Integer and Positive Integer Value, and Structured View; other types need
  an application `object.Object`. The server keeps state in memory only.
- MS/TP, BACnet/IPv6, BACnet/SC.
- On the client side, a schedule object database: the client decodes
  Weekly_Schedule, Exception_Schedule, calendar entries and HostNPort, but
  keeps no model of a remote schedule.
- Authentication or encryption: BACnet/IP has none and this library adds
  none ([SECURITY.md](SECURITY.md)).

## Partial

- **AuthRequest**: an opaque payload, codec and unit evidence only; no stable
  stack implements the service.
- **VT-Open, VT-Close, VT-Data**: codec evidence only, no session anywhere;
  inbound VT-Data is not handled.
- **Network-layer messages** beyond those the client needs: typed codecs and
  raw preservation; the client is not a router.
- **Segmented ComplexACK receive** uses window 1 by default
  (`client.WithSegmentWindow` raises it).
- **Server intrinsic reporting**: out-of-range, change-of-state and
  change-of-reliability only. A Multi-state Output does not report (its
  algorithm, command-failure on Feedback_Value, is not implemented), and
  multi-state points do not re-alarm on a different alarm value or apply
  Time_Delay_Normal. A confirmed event notification to a broadcast recipient
  is not sent.
- **Vendor extensions and newer revision constructs** are preserved as raw
  ASN.1 where typed models are incomplete.

## Evidence weaker than multi-peer

Implemented, but not run against two or more stacks: see the "Interop
evidence" column of [docs/CLIENT_SUPPORT.md](docs/CLIENT_SUPPORT.md). Examples:
Write-Broadcast-Distribution-Table and segmented confirmed-request send
(one peer), SubscribeCOVPropertyMultiple and COVNotificationMultiple (codec
evidence: no stack executes the subscription), WritePropertyMultipleBatched
and ReadFileRecords (codec or unit evidence).

## Server evidence

Three independent clients drive the server, each an unmodified upstream
stack in a bacnet-interop v0.12.0 client-mode image: the bacnet-stack 1.6.1
command-line tools, BACnet4J 6.1.1 and BACpypes3 0.0.110. Between them they
cover discovery, reads and writes and their error cases, SubscribeCOV,
SubscribeCOVProperty, SubscribeCOVPropertyMultiple (BACnet4J only),
ReadRange, DeviceCommunicationControl, ReinitializeDevice, stream, record and
segmented AtomicReadFile and AtomicWriteFile, AddListElement and
RemoveListElement, CreateObject and DeleteObject, GetEventInformation,
GetAlarmSummary, GetEnrollmentSummary and AcknowledgeAlarm, time
synchronization, text messages, private transfer, WriteGroup, and the BBMD
as seen by a foreign device. Not covered by any of them: BBMD-to-BBMD
forwarding and the object types other than those of the `device-server-v1`
fixture (Calendar, Schedule, Structured View and the value types are
verified against the go-bacnet client only, `e2e/server*_test.go`). Where
the standard leaves the error code open the server follows bacnet-stack, so
agreement on those codes is partly by construction
([docs/SERVER_SUPPORT.md](docs/SERVER_SUPPORT.md)). Every datagram the server
sends in the e2e tests is dissected by Wireshark (`make dissector`), except
Abort and SegmentACK frames.

