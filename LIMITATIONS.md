# Limitations

What go-bacnet does not do, or does with restrictions. Per-capability status
and evidence: [docs/CLIENT_SUPPORT.md](docs/CLIENT_SUPPORT.md). Peer quirks:
[docs/COMPATIBILITY.md](docs/COMPATIBILITY.md).

## Certification and validation

- **Nothing here is certified.** There is no BTL listing and no formal PICS;
  [docs/CLIENT_PROFILE.md](docs/CLIENT_PROFILE.md) is a descriptive profile.
- **No physical devices are recorded.** Evidence comes from five open-source
  stacks in containers and from Wireshark's dissector
  ([INTEROP.md](INTEROP.md)). The checklist for field runs is
  [docs/FIELD_VALIDATION.md](docs/FIELD_VALIDATION.md).

## Not implemented

- A BACnet server or device object model; a BBMD server.
- MS/TP, BACnet/IPv6, BACnet/SC.
- A full schedule object database (the Weekly, Exception, Calendar and
  HostNPort codecs and the property decode helpers exist).
- Authentication or encryption: BACnet/IP has none and this library adds
  none ([SECURITY.md](SECURITY.md)).

## Partial

- **AuthRequest**: an opaque payload, codec and unit evidence only; no stable
  stack implements the service.
- **VT-Open, VT-Close, VT-Data**: codec evidence only, no session anywhere;
  inbound VT-Data is not handled.
- **Network-layer messages** beyond those the client needs: typed codecs and
  raw preservation; the client is not a router.
- **Segmented ComplexACK receive** uses window 1.
- **Vendor extensions and newer revision constructs** are preserved as raw
  ASN.1 where typed models are incomplete.

## Evidence weaker than multi-peer

Implemented, but not run against two or more stacks: see the "Interop
evidence" column of [docs/CLIENT_SUPPORT.md](docs/CLIENT_SUPPORT.md). Examples:
Write-Broadcast-Distribution-Table and segmented confirmed-request send
(one peer), SubscribeCOVPropertyMultiple and COVNotificationMultiple (codec
evidence: no stack executes the subscription), WritePropertyMultipleBatched
and ReadFileRecords (codec or unit evidence).
