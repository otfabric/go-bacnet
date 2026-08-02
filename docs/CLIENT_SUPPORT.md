# Client support

Authoritative declaration of BACnet client capabilities in `go-bacnet` and how
well each is validated. Per-peer results live in [INTEROP.md](../INTEROP.md).
Behavioural contracts live in [API.md](../API.md) and [ERRORS.md](../ERRORS.md).

**Library version:** [v0.2.4](https://github.com/otfabric/go-bacnet/releases/tag/v0.2.4)  
**Interop pin:** [bacnet-interop v0.8.0](https://github.com/otfabric/bacnet-interop/releases/tag/v0.8.0)

| Client support | Meaning |
|---|---|
| Implemented | Exported client and/or codec path exists |
| Partial | Implemented for common cases; gaps noted |
| Not implemented | Not provided by this library |

| Interop evidence | Meaning |
|---|---|
| Multi-peer | Live test against two or more independent stacks |
| Single-peer | Live test against one stack |
| Codec/unit | Unit, codec, or fixture evidence only |
| Not yet exercised | Implemented but no dedicated interop scenario yet |

---

## BACnet/IP and BVLC

| Capability | Client support | Interop evidence | Notes |
|---|---|---|---|
| Original-Unicast-NPDU | Implemented | Multi-peer | Default BACnet/IP path |
| Original-Broadcast-NPDU | Implemented | Multi-peer | Discovery and broadcasts |
| Forwarded-NPDU receive | Implemented | Multi-peer | BBMD forwarded frames |
| Register-Foreign-Device | Implemented | Multi-peer | FD against bacnet-stack, BACpypes3, BACnet4J |
| Distribute-Broadcast-To-Network | Partial | Not yet exercised | Client send path exists; limited live exercise |
| BVLC Result | Implemented | Codec/unit | |
| Read-Broadcast-Distribution-Table | Partial | Not yet exercised | Client helpers; native peer audit open |
| Write-Broadcast-Distribution-Table | Partial | Not yet exercised | |
| Read-Foreign-Device-Table | Partial | Not yet exercised | |
| Delete-Foreign-Device-Table-Entry | Partial | Not yet exercised | |
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
| Other network-layer messages | Partial | Not yet exercised | Raw preserve where decoded; full NL set incomplete |

## APDU and transaction layer

| Capability | Client support | Interop evidence | Notes |
|---|---|---|---|
| Confirmed requests | Implemented | Multi-peer | |
| Unconfirmed requests | Implemented | Multi-peer | |
| SimpleACK / ComplexACK | Implemented | Multi-peer | |
| Error / Reject / Abort | Implemented | Multi-peer | |
| Segmented confirmed-request send | Implemented | Single-peer | BACpypes3 live; BACnet4J rejects |
| Segmented ComplexACK receive | Implemented | Multi-peer | Window 1; Worldiety continuation deviation |
| Timeout and cancellation | Implemented | Codec/unit | See API.md |
| Outcome-unknown after send | Implemented | Codec/unit | Documented for writes |

## Object and property services

| Capability | Client support | Interop evidence | Notes |
|---|---|---|---|
| ReadProperty | Implemented | Multi-peer | |
| ReadPropertyMultiple | Implemented | Multi-peer | |
| WriteProperty | Implemented | Multi-peer | Priority + NULL relinquish |
| WritePropertyMultiple | Implemented | Multi-peer | First-failed Error model |
| ReadRange | Implemented | Multi-peer | byPosition / sequence / time; typed LogRecord |
| AddListElement / RemoveListElement | Implemented | Multi-peer | Notification Class list |
| CreateObject / DeleteObject | Implemented | Multi-peer | |

## Discovery

| Capability | Client support | Interop evidence | Notes |
|---|---|---|---|
| Who-Is / I-Am | Implemented | Multi-peer | |
| Who-Has / I-Have | Implemented | Multi-peer | |
| Who-Am-I / You-Are | Implemented | Single-peer | bacnet-stack only at pin |

## COV and event / alarm

| Capability | Client support | Interop evidence | Notes |
|---|---|---|---|
| SubscribeCOV | Implemented | Multi-peer | Subscribe / notify / cancel; renew single-peer |
| SubscribeCOVProperty | Implemented | Codec/unit | |
| SubscribeCOVPropertyMultiple | Implemented | Codec/unit | No peer server support at pin |
| COVNotification / ConfirmedCOVNotification | Implemented | Multi-peer | |
| COVNotificationMultiple | Implemented | Codec/unit | No peer emit at pin |
| EventNotification receive | Implemented | Multi-peer | Common NotificationParameters typed |
| AcknowledgeAlarm | Implemented | Multi-peer | |
| GetAlarmSummary | Implemented | Multi-peer | |
| GetEnrollmentSummary | Implemented | Single-peer | BACnet4J only |
| GetEventInformation | Implemented | Multi-peer | |

## Files

| Capability | Client support | Interop evidence | Notes |
|---|---|---|---|
| AtomicReadFile stream / record | Implemented | Multi-peer | |
| AtomicWriteFile stream / record | Implemented | Multi-peer | |

## Device management and advanced services

| Capability | Client support | Interop evidence | Notes |
|---|---|---|---|
| DeviceCommunicationControl | Implemented | Single-peer | Explicit opt-in |
| ReinitializeDevice | Implemented | Multi-peer | Explicit opt-in |
| PrivateTransfer | Implemented | Multi-peer | Confirmed path peer-dependent |
| TextMessage | Implemented | Multi-peer | |
| TimeSynchronization / UTCTimeSynchronization | Implemented | Multi-peer | |
| WriteGroup | Implemented | Multi-peer | Not on BACnet4J at pin |
| AuditLogQuery | Implemented | Codec/unit | |
| AuthRequest | Implemented | Codec/unit | |
| LifeSafetyOperation | Implemented | Multi-peer | |
| VT-Open / VT-Close / VT-Data | Implemented | Codec/unit | |

## Explicitly out of scope

| Capability | Client support | Notes |
|---|---|---|
| Native MS/TP | Not implemented | |
| Full BACnet server / device model | Not implemented | |
| Schedules as first-class helpers | Not implemented | Raw/property access may still work |
| BTL certification | Not implemented | Descriptive profile only |

See also [CLIENT_PROFILE.md](CLIENT_PROFILE.md) and [STANDARD_BASELINE.md](STANDARD_BASELINE.md).
