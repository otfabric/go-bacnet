# Server support

What the BACnet/IP server in `go-bacnet` (packages `server`,
`server/object`, `bbmd`) implements and how well each part is verified.
Behavioural contracts are in [API.md](../API.md#server) and
[ERRORS.md](../ERRORS.md#server-errors); the scope and its phases are in
[SERVER_PLAN.md](SERVER_PLAN.md). Client capabilities are in
[CLIENT_SUPPORT.md](CLIENT_SUPPORT.md).

**Library version:** v0.8.0  
**Independent clients:** six, each an unmodified upstream stack in a
bacnet-interop client-mode image (bacnet-interop v0.12.0 and later; pin v0.14.0,
`interop/server_clients_test.go`,
[INTEROP.md](../INTEROP.md#independent-clients-against-the-server)):
the bacnet-stack 1.6.1 command-line tools, BACnet4J 6.1.1, BACpypes3
0.0.110, @bacnet-js/client 3.3.2 (bacnet-js) and legacy BACpypes 0.19.0
(PyPI `bacpypes`, BACpypes3's predecessor by the same author). They run four scenario files against the server:
`server-baseline-v1` (discovery, reads and writes), `server-services-v1`
(COV, ReadRange, device management, files, lists, object creation, alarms,
time synchronization, private transfer, WriteGroup), `server-bbmd-v1` (the
client registers as a foreign device with the server's BBMD) and
`server-extended-v1` (segmented requests and replies, SubscribeCOVProperty
and SubscribeCOVPropertyMultiple, GetAlarmSummary, GetEnrollmentSummary,
text messages, ConfirmedPrivateTransfer). A row names the clients whose
scenarios for it pass. The bacnet-stack tools are built without
segmentation and do not run `server-extended-v1`; a `known-difference` or
`unsupported` result is a recorded limit of the client (bacnet-interop
PEER_SUPPORT.md) and is not counted as evidence. The sixth client,
ulbios/bacnet (commit `227d6227`, bacnet-interop 0.14.0), issues Who-Is, ReadProperty and WriteProperty
only: of `server-baseline-v1` it passes `who-is.global`,
`rp.unknown-object`, `wp.av1.pv.priority8`, `wp.av1.pv.relinquish8` and
`wp.unknown-object`; every other scenario of the four files is
`known-difference` or `unsupported`.

| Server support | Meaning |
|---|---|
| Implemented | Served as the clause specifies, with the limits in Notes |
| Partial | Served for common cases; gaps in Notes |
| Hook | Served only when the application installs the hook; without it the service is not advertised in Protocol_Services_Supported, a confirmed request gets Reject unrecognized-service and an unconfirmed one is ignored |
| Not implemented | A confirmed request gets Reject unrecognized-service |

| Evidence | Meaning |
|---|---|
| Internal e2e | The go-bacnet client against the server over UDP loopback (`e2e/server*_test.go`) |
| Unit | Encoded requests against the server on the in-memory transport (`server/*_internal_test.go`), or tests of the object or BBMD package |
| Independent client | An independent stack drives the server through a bacnet-interop client-mode image and the scenario's expectation holds; the stack is named |

`e2e/server_objects_test.go` reads every property of one object of each
built-in type with ReadPropertyMultiple ALL, which is the internal e2e
evidence for the object rows; behaviour such as Schedule evaluation is
covered by the unit tests.

The same go-bacnet codecs encode the requests and decode them in the server,
so internal e2e shows that client and server agree with each other, not that
either agrees with the standard. Error codes chosen where the standard
leaves room follow bacnet-stack (pin 7a53f0a7), as cited in the Notes, so
bacnet-stack agreeing with those codes is partly by construction.

---

## Data sharing

| Service | Clause | Support | Evidence | Notes |
|---|---|---|---|---|
| ReadProperty | 15.5 | Implemented | Internal e2e, Independent client (bacnet-stack, BACnet4J, BACpypes3, legacy BACpypes, bacnet-js; ulbios for unknown-object only) | Array index; index 0 is the array length; Device wildcard instance 4194303 |
| ReadPropertyMultiple | 15.7 | Implemented | Internal e2e, Independent client (bacnet-stack, BACnet4J, BACpypes3, legacy BACpypes, bacnet-js) | ALL, REQUIRED, OPTIONAL; errors per property |
| WriteProperty | 15.9 | Implemented | Internal e2e, Independent client (bacnet-stack, BACnet4J, BACpypes3, legacy BACpypes, bacnet-js, ulbios) | Priority 1..16; NULL relinquishes; `Hooks.BeforeWrite` can refuse. A value of two application values (ulbios' own REAL-then-NULL form) is property/invalid-data-type |
| WritePropertyMultiple | 15.10 | Implemented | Internal e2e, Independent client (bacnet-stack, BACnet4J, BACpypes3, legacy BACpypes, bacnet-js) | In order, stops at the first failure with WritePropertyMultiple-Error |
| ReadRange | 15.8 | Implemented | Internal e2e, Independent client (bacnet-stack, BACnet4J, BACpypes3, legacy BACpypes, bacnet-js) | Trend Log Log_Buffer by position, sequence number and time; the reply is the largest prefix that fits, with MORE_ITEMS. ReadProperty of Log_Buffer gives property/read-access-denied, as bacnet-stack (trendlog.c) |
| Who-Is / I-Am | 16.10 | Implemented | Internal e2e, Independent client (bacnet-stack, BACnet4J, BACpypes3, legacy BACpypes, bacnet-js, ulbios without range) | Range filter; a Who-Is from a remote network is answered by remote broadcast through the router it came from |
| Who-Has / I-Have | 16.9 | Implemented | Internal e2e, Independent client (bacnet-stack, BACnet4J, BACpypes3, legacy BACpypes, bacnet-js) | By name and by identifier |
| WriteGroup | 15.11 | Hook | Internal e2e, Independent client (bacnet-stack, BACnet4J, BACpypes3, legacy BACpypes) | `Hooks.WriteGroup` |

## Lists and objects

| Service | Clause | Support | Evidence | Notes |
|---|---|---|---|---|
| AddListElement / RemoveListElement | 15.1, 15.2 | Implemented | Internal e2e, Independent client (bacnet-stack, BACnet4J, BACpypes3, legacy BACpypes) | Notification Class Recipient_List; atomic; ChangeList-Error names the first failed element. bacnet-stack `bacrle` registers its reply handlers for AddListElement and prints nothing for RemoveListElement (apps/remove-list-element/main.c); the removal is verified by reading Recipient_List back with `bacrp` |
| CreateObject | 15.3 | Implemented | Internal e2e, Independent client (bacnet-stack, BACnet4J, BACpypes3, legacy BACpypes, bacnet-js) | Types with a registered `object.Factory`; initial values written in order and the object removed when one fails (bacnet-stack, create_object.c) |
| DeleteObject | 15.4 | Implemented | Internal e2e, Independent client (bacnet-stack, BACnet4J, BACpypes3, legacy BACpypes, bacnet-js) | Objects that implement `object.Deletable`; never the Device; `Hooks.BeforeDelete`; COV subscriptions on the object end |

## Files

| Service | Clause | Support | Evidence | Notes |
|---|---|---|---|---|
| AtomicReadFile | 14.1 | Implemented | Internal e2e, Independent client (bacnet-stack, BACnet4J, BACpypes3, legacy BACpypes, bacnet-js) (stream) | Stream and record access, in-memory File object or an `object.FileAccessor` |
| AtomicWriteFile | 14.2 | Implemented | Internal e2e, Independent client (bacnet-stack, BACnet4J, BACpypes3, legacy BACpypes, bacnet-js) | Start −1 appends. Past the end a stream file is extended with zero octets and records are appended after the last one, within the file's maximum size, as bacnet-stack's storage does (ports/posix/bacfile-posix.c) |

## Change of value

| Service | Clause | Support | Evidence | Notes |
|---|---|---|---|---|
| SubscribeCOV | 13.14 | Implemented | Internal e2e, Independent client (bacnet-stack, BACnet4J, BACpypes3, legacy BACpypes, bacnet-js) | Lifetime, cancellation, confirmed and unconfirmed notifications |
| SubscribeCOVProperty | 13.15 | Implemented | Internal e2e, Unit, Independent client (BACnet4J, BACpypes3, legacy BACpypes, bacnet-js) | COV increment; Status_Flags carried when the object has it |
| SubscribeCOVPropertyMultiple | 13.16 | Implemented | Internal e2e, Unit, Independent client (BACnet4J) | One subscription per subscriber and process identifier; the error names the first failed subscription |
| COV notifications | 13.6, 13.7, 13.17 | Implemented | Internal e2e, Unit, Independent client (bacnet-stack, BACnet4J, BACpypes3, legacy BACpypes, bacnet-js) (unconfirmed) | Confirmed notifications use the server's own requester with the configured APDU timeout and retries |
| Active_COV_Subscriptions | 12.11 | Implemented | Internal e2e, Unit | One entry per monitored property |

Subscriptions are bounded (`WithCOVLimits`, default 256). With a lifetime
limit set, an indefinite subscription and one longer than the limit are
refused.

## Alarms and events

| Service | Clause | Support | Evidence | Notes |
|---|---|---|---|---|
| Intrinsic reporting | 13.3 | Partial | Internal e2e, Unit, Independent client (bacnet-stack, BACnet4J, BACpypes3, legacy BACpypes, bacnet-js) (analog high limit) | Analog points: out-of-range (high and low limit, deadband, Limit_Enable). Binary points: change-of-state on Alarm_Value. Multi-state Input and Value: change-of-state on Alarm_Values (13.3.2), Fault_Values set Reliability to multi-state-fault (13.4.5). Any point: change-of-reliability to FAULT. Time_Delay holds. Transition rules follow bacnet-stack (ai.c, bi.c) |
| Event notifications | 13.8, 13.9 | Implemented | Internal e2e | Routed by the Notification Class Recipient_List (valid days, times, transitions). A device recipient is resolved from received I-Am; while it is unknown the server sends a Who-Is and skips that notification (bacnet-stack, nc.c). A remote-station recipient is sent through the router learnt from received SNET and I-Am-Router-To-Network (6.4.2); while none is known the server sends Who-Is-Router-To-Network (6.4.1), skips a confirmed notification and sends an unconfirmed one as a local broadcast with DNET |
| AcknowledgeAlarm | 13.5 | Implemented | Internal e2e, Independent client (bacnet-stack, BACnet4J, BACpypes3, legacy BACpypes, bacnet-js) | Time-stamp and event-state checks as bacnet-stack (ai.c); the ack notification follows the SimpleACK |
| GetAlarmSummary | 13.10 | Implemented | Internal e2e, Independent client (BACnet4J, BACpypes3, legacy BACpypes, bacnet-js) | Objects in an off-normal state with notify type alarm |
| GetEventInformation | 13.12 | Implemented | Internal e2e, Independent client (bacnet-stack, BACnet4J, BACpypes3, legacy BACpypes, bacnet-js) | Paging by last received object; MoreEvents when truncated to the reply budget. bacnet-stack `bacge` cannot decode an empty list of event summaries (it fails the same way against a bacnet-stack device), so only a non-empty reply is checked with it |
| GetEnrollmentSummary | 13.11 | Implemented | Internal e2e, Unit, Independent client (BACnet4J, BACpypes3, legacy BACpypes, bacnet-js: unfiltered) | All filters. The enrollment filter keeps objects whose Notification Class Recipient_List holds that recipient (same device, or same network and MAC) and process identifier |

Limits:

- A confirmed notification to a broadcast recipient is not sent: a
  confirmed request is a transaction with one peer (clause 5.4).
  bacnet-stack sends it anyway. A diagnostic reports it.
- The router table holds at most 64 networks; a network not yet known is not
  learnt once it is full. The server does not age routes or act on
  Router-Busy or Reject-Message-To-Network.
- Multi-state points: the optional re-alarm on a different alarm or fault
  value without leaving OFFNORMAL or FAULT is not done, Time_Delay_Normal
  is not implemented, and AddListElement and RemoveListElement do not
  change Alarm_Values and Fault_Values (WriteProperty does). A Multi-state
  Output does not report: its algorithm is command-failure on
  Feedback_Value (clause 12.19).
- Without its Notification Class, a transition is recorded in the object and
  reported as a diagnostic only.

## Device management

| Service | Clause | Support | Evidence | Notes |
|---|---|---|---|---|
| DeviceCommunicationControl | 16.1 | Implemented | Internal e2e, Unit, Independent client (bacnet-stack, BACnet4J, BACpypes3, legacy BACpypes, bacnet-js) | Enable, disable-initiation, duration, password. DISABLE is refused with services/service-request-denied from protocol revision 20 (bacnet-stack, h_dcc.c). Who-Is is answered while initiation is disabled |
| ReinitializeDevice | 16.4 | Implemented | Internal e2e, Unit, Independent client (bacnet-stack, BACnet4J, BACpypes3, legacy BACpypes, bacnet-js) | Password; `Hooks.Reinitialize` performs the action; without it COLDSTART, WARMSTART and ACTIVATE_CHANGES are acknowledged and START_BACKUP to ABORT_RESTORE get services/service-request-denied (no backup procedure), as the recorded WattStopper LMBC-300 answers ([testdata/captures](../testdata/captures/README.md)) |
| TimeSynchronization / UTCTimeSynchronization | 16.7, 16.8 | Hook | Internal e2e, Unit, Independent client (bacnet-stack, BACnet4J, BACpypes3, legacy BACpypes, bacnet-js) | `Hooks.TimeSync` |
| ConfirmedPrivateTransfer / UnconfirmedPrivateTransfer | 16.2, 16.3 | Hook | Internal e2e, Unit, Independent client (bacnet-stack: unconfirmed; BACnet4J, BACpypes3, legacy BACpypes: confirmed; bacnet-js: both) | `Hooks.PrivateTransfer`; the returned elements are the result block |
| ConfirmedTextMessage / UnconfirmedTextMessage | 16.5, 16.6 | Hook | Internal e2e, Unit, Independent client (BACnet4J, BACpypes3, legacy BACpypes) | `Hooks.TextMessage` |
| LifeSafetyOperation, VT, authentication, audit | — | Not implemented | Internal e2e (Reject) | |

## Transactions

| Capability | Clause | Support | Evidence | Notes |
|---|---|---|---|---|
| Segmented ComplexACK | 5.2, 5.4.5 | Implemented | Unit, Internal e2e, Independent client (BACnet4J, BACpypes3, legacy BACpypes, bacnet-js) | Window from the requester's SegmentACK; NAK and timeout resend; Abort tsm-timeout after the retries; Abort segmentation-not-supported or buffer-overflow when the requester cannot take the reply |
| Segmented request reassembly | 5.4.5 | Implemented | Unit, Internal e2e, Independent client (BACnet4J, BACpypes3, legacy BACpypes) | Segment 0 acknowledged at once, later windows counted from it; window capped at the server's; an APDU longer than Max_APDU_Length_Accepted is accepted within the datagram limit (BACpypes3 0.0.110 sends 1482-octet segments to a 1476-octet device; `WithDecodeLimits` can refuse them); out-of-order NAK; Abort buffer-overflow beyond `WithSegmentation` segments or the decode limits; a segment of an unknown transaction gets Abort invalid-apdu-in-this-state |
| Repeated requests | 5.4.5 | Implemented | Unit | Dropped while executing; after the response a request with the same invoke ID is a new request (clause 5.4.5) |
| Bounded workers | — | Implemented | Unit, chaos | A confirmed request that finds the queue full gets Abort out-of-resources |
| Reject of malformed requests | 18.8 | Implemented | Unit | invalid-tag, too-many-arguments, buffer-overflow, undefined-enumeration (decoded enumerations outside the defined values) |

## Objects

| Object | Support | Evidence | Notes |
|---|---|---|---|
| Device | Implemented | Internal e2e, Independent client (bacnet-stack, BACnet4J, BACpypes3, legacy BACpypes, bacnet-js) | Object_List, Protocol_Services_Supported and Protocol_Object_Types_Supported computed from what is served; Database_Revision; Active_COV_Subscriptions |
| Analog Input / Output / Value | Implemented | Internal e2e, Independent client (bacnet-stack, BACnet4J, BACpypes3, legacy BACpypes, bacnet-js) (value) | Outputs commandable; values optionally commandable; COV increment |
| Binary Input / Output / Value | Implemented | Internal e2e, Unit | Polarity, active and inactive text |
| Multi-state Input / Output / Value | Implemented | Internal e2e, Unit | State text; intrinsic reporting on Input and Value (Alarm_Values, Fault_Values) |
| Notification Class | Implemented | Internal e2e, Independent client (bacnet-stack, BACnet4J, BACpypes3, legacy BACpypes) | Priority, Ack_Required, Recipient_List |
| Trend Log | Implemented | Internal e2e, Independent client (bacnet-stack, BACnet4J, BACpypes3, legacy BACpypes, bacnet-js) | Polled logging of a local property or `Log` by the application; stop-when-full; Record_Count writable to 0 |
| File | Implemented | Internal e2e, Independent client (bacnet-stack, BACnet4J, BACpypes3, legacy BACpypes, bacnet-js) | Stream and record files in memory; read-only files |
| Calendar | Implemented | Internal e2e, Unit | Clause 12.9. Present_Value from Date_List against the server clock's local date, evaluated on every poll and when Date_List changes; Date_List writable and changed by AddListElement / RemoveListElement. Calendar entries: date with the BACnetDate special values (any, odd / even month, last / odd / even day; clause 21), dateRange (fields unspecified in either date are skipped, so a fully unspecified end opens the range, as bacnet-stack datetime.c `datetime_wildcard_compare_date`), weekNDay per the clause 21 week-of-month table (bacnet-stack calendar_entry.c computes weeks 1–9 differently; the standard is followed) |
| Schedule | Implemented | Internal e2e, Unit | Clause 12.24. Evaluated on every poll against the server clock: outside Effective_Period Schedule_Default; else the non-NULL value of the in-effect special event with the lowest Event_Priority (first in Exception_Schedule on a tie; period by calendar entry or Calendar reference; this is the implementation's reading of clause 12.24 without a peer reference, because bacnet-stack schedule.c does not evaluate Exception_Schedule), else the day's Weekly_Schedule value, else Schedule_Default. A NULL time value relinquishes to the next layer; before a day's first entry the default applies, as bacnet-stack schedule.c `Schedule_Recalculate_PV`. A changed Present_Value is written at Priority_For_Writing to the referenced properties of local objects; references to other devices are kept but not written (the server initiates no WriteProperty). Present_Value writable while Out_Of_Service; Weekly_Schedule, Exception_Schedule, Effective_Period, List_Of_Object_Property_References writable; arrays not resizable through index 0 (write-access-denied, as schedule.c); one datatype per schedule |
| CharacterString / Integer / Positive Integer Value | Implemented | Internal e2e, Unit | Clauses 12.37, 12.43, 12.44. Optionally commandable; COV of Present_Value and Status_Flags on every change (no COV_Increment); integers held in 32 bits as bacnet-stack iv.c / piv.c, beyond is value-out-of-range |
| Structured View | Implemented | Internal e2e, Unit | Clause 12.29. Node_Type, Node_Subtype, Subordinate_List, Subordinate_Annotations; writable as in bacnet-stack structured_view.c; a whole Subordinate_List write may change its length and the annotations follow; not resizable through index 0 |

## BBMD (`bbmd`)

| Capability | Clause | Support | Evidence | Notes |
|---|---|---|---|---|
| Register-Foreign-Device, FDT | J.5.2 | Implemented | Internal e2e, Unit, Independent client (bacnet-stack, BACnet4J, BACpypes3, legacy BACpypes, bacnet-js) | TTL plus 30 s grace; table bounded (`MaxForeignDevices`) |
| Read-BDT / Read-FDT / Delete-FDT-Entry | J.2 | Implemented | Internal e2e, Unit, Independent client (bacnet-stack, BACpypes3, legacy BACpypes) (Read-BDT, Read-FDT) | |
| Write-BDT | J.4.4.2 | Implemented | Internal e2e, Unit, Independent client (bacnet-stack, BACpypes3, legacy BACpypes) | Answered with a NAK (bacnet-stack, h_bbmd.c) |
| Forwarded-NPDU distribution | J.4.5 | Implemented | Internal e2e, Unit | Two-hop by directed broadcast (IP OR inverted mask); a Forwarded-NPDU from a one-hop peer is rebroadcast locally; not back to the originating foreign device |
| Distribute-Broadcast-To-Network | J.4.5 | Implemented | Internal e2e, Unit, Independent client (bacnet-stack, BACnet4J, BACpypes3, legacy BACpypes, bacnet-js) | From a registered foreign device only; otherwise a NAK. bacnet-stack does not check registration; this follows the clause text |
| Broadcast confirmed requests | — | Implemented | Unit | Discarded, as bacnet-stack |

Without a BBMD the server answers every BBMD function with its NAK.

## Verification

| Check | Scope |
|---|---|
| `make check` | Unit and e2e tests, coverage gate including `server`, `server/object`, `bbmd`, `bip` |
| `make chaos` | `TestChaosServer`: concurrent requesters, retransmissions, junk, object changes and COV under `-race` |
| `make fuzz` | `FuzzServerReceive` (two datagrams to a serving device; every sent frame must parse; Close ends every goroutine), `FuzzHandleBVLC` |
| `make dissector` | `TestDissectorAcceptsServerReplies`: every datagram the server sends in the server e2e tests (acks, errors, rejects, segments, I-Am, I-Have, COV and event notifications, BVLC results and table acks) is dissected by tshark; malformed frames and expert warnings fail. Abort and SegmentACK from the server are not in that set |
| `make interop` | `TestBACnetStackClientAgainstServer`: bacnet-stack client mode against the server with `server-baseline-v1`, `server-services-v1` and `server-bbmd-v1` (needs a bacnet-interop image with client mode) |
