# Client support

Authoritative declaration of BACnet client capabilities in `go-bacnet` and how
well each is validated. Per-peer results live in [INTEROP.md](../INTEROP.md).
Behavioural contracts live in [API.md](../API.md) and [ERRORS.md](../ERRORS.md).
Server capabilities are in [SERVER_SUPPORT.md](SERVER_SUPPORT.md). The UDP
transport the client uses lives in package `bip` from v0.6.0
(`client.Transport` is an alias); the client's behaviour is unchanged.

**Library version:** v0.6.0 (previous release [v0.5.0](https://github.com/otfabric/go-bacnet/releases/tag/v0.5.0))  
**Interop pin:** bacnet-interop v0.13.0 (six peer stacks; see [INTEROP.md](../INTEROP.md))

| Client support | Meaning |
|---|---|
| Implemented | Exported client and/or codec path exists |
| Partial | Implemented for common cases; gaps noted |
| Not implemented | Not provided by this library |

| Interop evidence | Meaning |
|---|---|
| Multi-peer | Live test against two or more independent stacks |
| Single-peer | Live test against one stack |
| Codec evidence | A peer stack's own decoder or encoder agrees with the client's bytes; no stack executes the service |
| Codec/unit | Unit, codec, or fixture evidence only |
| Dissector | No peer serves it; the request layout is checked against Wireshark (`make dissector`) |
| Not yet exercised | Implemented but no dedicated interop scenario yet |

---

## BACnet/IP and BVLC

| Capability | Client support | Interop evidence | Notes |
|---|---|---|---|
| Original-Unicast-NPDU | Implemented | Multi-peer | Default BACnet/IP path |
| Original-Broadcast-NPDU | Implemented | Multi-peer | Discovery and broadcasts |
| Forwarded-NPDU receive | Implemented | Multi-peer | BBMD forwarded frames |
| Register-Foreign-Device | Implemented | Multi-peer | FD against bacnet-stack, BACpypes3, BACnet4J, the cB-Sigma simulator (and its Register-FD NAK) |
| Distribute-Broadcast-To-Network | Implemented | Multi-peer | Used when foreign-device registered |
| BVLC Result | Implemented | Multi-peer | FD registration + BDT/FDT management |
| Read-Broadcast-Distribution-Table | Implemented | Multi-peer | bacnet-stack / BACpypes3 / BACnet4J |
| Write-Broadcast-Distribution-Table | Implemented | Single-peer | Opt-in network management; BACnet4J write OK; stack/BACpypes3 NAK asserted |
| Read-Foreign-Device-Table | Implemented | Multi-peer | After FD registration |
| Delete-Foreign-Device-Table-Entry | Implemented | Multi-peer | Register → delete → absence |
| BBMD server | Not implemented | — | Out of scope |
| BACnet/IPv6 | Not implemented | — | |
| BACnet/SC | Not implemented | — | |

## NPDU and network-layer services

| Capability | Client support | Interop evidence | Notes |
|---|---|---|---|
| Local and remote station addressing | Implemented | Multi-peer | Routed addressing via DNET/DADR |
| Global broadcast | Implemented | Multi-peer | |
| Who-Is-Router-To-Network | Implemented | Multi-peer | Topology aid + peer endpoints |
| I-Am-Router-To-Network receive | Implemented | Multi-peer | |
| Reject / Router-Busy / Router-Available | Implemented | Codec/unit | Updates bounded route state; unit-tested selection |
| Other network-layer messages | Partial | Codec/unit | Typed codecs + raw preserve; client is not a router |

## APDU and transaction layer

| Capability | Client support | Interop evidence | Notes |
|---|---|---|---|
| Confirmed requests | Implemented | Multi-peer | |
| Unconfirmed requests | Implemented | Multi-peer | |
| SimpleACK / ComplexACK | Implemented | Multi-peer | |
| Error / Reject / Abort | Implemented | Multi-peer | |
| Segmented confirmed-request send | Implemented | Multi-peer | BACpypes3 and cB-Sigma (both roles) live; BACnet4J rejects. Segment 0 goes alone; the rest follow in the actual window of its SegmentACK (clause 5.4.4) |
| Segmented ComplexACK receive | Implemented | Multi-peer | Window 1 by default (`WithSegmentWindow`); segment 0 is acknowledged at once, asserted at windows 1, 2, 4 and 16 against cB-Sigma and BACpypes3; a segment lost inside a window is NAKed and recovered (cB-Sigma simulator); Worldiety continuation deviation |
| Timeout and cancellation | Implemented | Single-peer | An unused port, and BACnet4J muted with DeviceCommunicationControl; see API.md |
| Outcome-unknown after send | Implemented | Single-peer | Unanswered write; policy registry covers all side-effecting services |
| Rejecting unserved confirmed requests | Implemented | Codec/unit | Reject / Abort to requests a client does not serve |
| Event dispatcher (async) | Implemented | Codec/unit | Opt-in `WithEventDispatcher`; sync handler default |

## Object and property services

| Capability | Client support | Interop evidence | Notes |
|---|---|---|---|
| ReadProperty | Implemented | Multi-peer | |
| ReadPropertyMultiple | Implemented | Multi-peer | |
| WriteProperty | Implemented | Multi-peer | Priority + NULL relinquish |
| WritePropertyMultiple | Implemented | Multi-peer | First-failed Error model |
| ReadRange | Implemented | Multi-peer | By position, sequence number and time on bacnet-stack, BACnet4J and the cB-Sigma simulator; typed LogRecord. Trend Log Multiple (LogMultipleRecords) and Event Log (EventLogRecords) answers: recorded traffic of a PolarSoft and an Automated Logic device only ([testdata/captures](../testdata/captures/README.md)) |
| ReadProperty of the Device wildcard | Implemented | Multi-peer | bacnet-stack, BACnet4J; not implemented by BACpypes3 / Worldiety adapter |
| ReadRangeAll pager | Implemented | Multi-peer | By position and by sequence number to LAST_ITEM; MaxItems/MaxBytes/MaxPages |
| ReadPropertyMultipleBatched | Implemented | Multi-peer | Split by property and merge; shrink after Abort on bacnet-stack |
| ReadObjectList / ReadPropertyList | Implemented | Multi-peer | Element-by-element fallback on bacnet-stack (47 entries, 50-octet APDU) |
| WritePriority / RelinquishPriority | Implemented | Multi-peer | BACpypes3, BACnet4J, System.IO.BACnet, cB-Sigma (device role) |
| WritePropertyMultipleBatched | Implemented | Codec/unit | Batch Completed/Failed/Unknown |
| AddListElement / RemoveListElement | Implemented | Multi-peer | Notification Class list |
| CreateObject / DeleteObject | Implemented | Multi-peer | |

## Discovery

| Capability | Client support | Interop evidence | Notes |
|---|---|---|---|
| Who-Is / I-Am | Implemented | Multi-peer | |
| Who-Has / I-Have | Implemented | Multi-peer | |
| Who-Am-I / You-Are | Implemented | Codec evidence | bacnet-stack decodes both |

## COV and event / alarm

| Capability | Client support | Interop evidence | Notes |
|---|---|---|---|
| SubscribeCOV | Implemented | Multi-peer | Subscribe / notify / cancel / renew |
| SubscribeCOVProperty | Implemented | Multi-peer | BACnet4J, System.IO.BACnet |
| SubscribeCOVPropertyMultiple | Implemented | Codec evidence | `SubscribeCOVMultiple` is the managed subscription: subscribe, renewal and cancellation decoded by BACpypes3, the request and cancellation parsed by BACnet4J. No stack executes the subscription |
| COVNotification / ConfirmedCOVNotification | Implemented | Multi-peer | |
| COVNotificationMultiple | Implemented | Codec evidence | BACnet4J's encoder, confirmed and unconfirmed. Delivered whole (`SubscriptionEvent.Multiple`) with timestamp and time-of-change |
| EventNotification receive | Implemented | Multi-peer | Practical NotificationParameters typed (incl. life-safety/extended) |
| AcknowledgeAlarm | Implemented | Multi-peer | |
| GetAlarmSummary | Implemented | Multi-peer | |
| GetEnrollmentSummary | Implemented | Multi-peer | BACnet4J executes it; BACpypes3 decodes every filter (adapter handler). The enrollment filter (`EnrollmentFilter`, BACnetRecipientProcess) is encoded since the server gained its decoder; it is covered by unit round trips, the go-bacnet server and the Wireshark dissector, not by the peer scenarios |
| GetEventInformation | Implemented | Multi-peer | |

## Files

| Capability | Client support | Interop evidence | Notes |
|---|---|---|---|
| AtomicReadFile stream / record | Implemented | Multi-peer | |
| AtomicWriteFile stream / record | Implemented | Multi-peer | |
| ReadFileStream | Implemented | Multi-peer | Chunked read to end of file |
| ReadFileRecords | Implemented | Codec/unit | |
| WriteFileStream | Implemented | Multi-peer | Chunked write + readback; per-chunk outcome-unknown unit-tested |

## Device management and advanced services

| Capability | Client support | Interop evidence | Notes |
|---|---|---|---|
| DeviceCommunicationControl | Implemented | Multi-peer | Explicit opt-in. BACnet4J goes silent on disable; bacnet-stack refuses it; BACpypes3 acknowledges without enforcing it |
| ReinitializeDevice | Implemented | Multi-peer | Explicit opt-in |
| PrivateTransfer | Implemented | Multi-peer | Confirmed path peer-dependent |
| TextMessage | Implemented | Multi-peer | |
| TimeSynchronization / UTCTimeSynchronization | Implemented | Multi-peer | |
| WriteGroup | Implemented | Multi-peer | Not on BACnet4J at pin |
| AuditLogQuery | Implemented | Codec evidence | BACpypes3 decodes the query; records from bacnet-stack and BACpypes3 encoders decoded |
| Audit notifications (receive) | Implemented | Codec evidence | bacnet-stack and BACpypes3 encoders |
| AuthRequest | Partial | Codec/unit | Opaque payload; no stable stack implements the service: all five peers refuse it (three Reject unrecognized-service, Worldiety Reject other, BACpypes3 does not answer) |
| LifeSafetyOperation | Implemented | Multi-peer | |
| VT-Open / VT-Close / VT-Data | Partial | Codec evidence | BACpypes3 decodes the requests and encodes the VT-Open and VT-Data ACKs, which the client decodes; no session anywhere; inbound VT-Data is not handled |
| device.ReadSnapshot | Implemented | Multi-peer | All five peers |

## Explicitly out of scope

| Capability | Client support | Notes |
|---|---|---|
| Native MS/TP | Not implemented | |
| Full BACnet server / device model | Not implemented | |
| Full schedule object database | Not implemented | Weekly/Exception/Calendar/HostNPort codecs + property decode helpers exist |
| BTL certification | Not implemented | Descriptive profile only |

See also [CLIENT_PROFILE.md](CLIENT_PROFILE.md) and [STANDARD_BASELINE.md](STANDARD_BASELINE.md).
