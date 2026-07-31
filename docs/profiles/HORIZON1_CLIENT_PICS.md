# Horizon 1 example client PICS (descriptive)

Example application profile for a supervisory BACnet/IP **client** built on
`go-bacnet`. This is **not** a BTL submission and does not claim certification.

Normative edition: see [`../standard-baseline.yaml`](../standard-baseline.yaml).
Capabilities: see [`../capabilities.yaml`](../capabilities.yaml).

## Product

| Field | Value |
|-------|-------|
| Profile ID | `HORIZON1_CLIENT` |
| Role | A-side (client / supervisory) |
| Data link | BACnet/IP IPv4, UDP port 47808 |
| Protocol revision (local baseline) | 31 (135-2024); remotes may be older |

## Supported services (initiate)

| Service | Support |
|---------|---------|
| Who-Is | Yes |
| ReadProperty | Yes |
| ReadPropertyMultiple | Yes |
| WriteProperty | Yes |
| SubscribeCOV | Yes |
| SubscribeCOVProperty | Optional / as implemented |
| Who-Is-Router-To-Network | Yes (as needed for routing) |

## Supported services (execute / receive)

| Service | Support |
|---------|---------|
| I-Am | Yes (observe into registry) |
| I-Am-Router-To-Network | Yes (cache) |
| UnconfirmedCOVNotification | Yes |
| ConfirmedCOVNotification | Yes (client indication path) |
| Forwarded-NPDU (BVLC) | Yes |

## Segmentation

| Capability | Support |
|------------|---------|
| Segmented ComplexACK receive | Yes (required) |
| Segmented request transmit | Optional |

## Character sets / datatypes

Application-tagged primitives used by Horizon 1 property access, including
Null, Boolean, Unsigned, Signed, Real, Double, Octet String, Character String,
Bit String, Enumerated, Date, Time, Object Identifier, and constructed nesting
within decode limits. Unknown enum/object/property identifiers are tolerated on
decode when the encoding is valid.

## Explicit non-support

Native MS/TP, BACnet/IPv6, BACnet/SC, BBMD server, full device/server object
model, alarms, schedules, trends, WritePropertyMultiple.
