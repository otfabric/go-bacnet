# Security Policy

## Protocol trust model (BACnet/IP)

BACnet/IP as implemented here is **unencrypted and unauthenticated**. Any peer
that can inject UDP datagrams on the path can forge Who-Is/I-Am, property
responses, COV notifications, and network messages.

Do **not** treat Device instance number alone as a trust or identity boundary.
Duplicate instance announcements from different paths are diagnosed; callers must
decide how to resolve conflicting observations.

BACnet/SC is **out of scope** for this library.

The client answers what it receives only in three ways: a SimpleACK to a
well-formed COV, event or audit notification (before it knows whether the
notification belongs to a subscription), SegmentACKs for a response it asked
for, and a Reject or Abort to any other confirmed request. Each is one small
datagram to the sender's address, never a broadcast. COV notifications are
delivered to the application only when they match a subscription's process
identifier, object and source path; event and audit notifications are
delivered from any sender. A source address can be spoofed like everything
else in BACnet/IP.

DeviceCommunicationControl and ReinitializeDevice passwords are sent in clear
text, as the protocol defines. The library never logs them
([OBSERVABILITY.md](OBSERVABILITY.md)).

## Allocation limits

All public parsers honour `bacnet.DecodeLimits` (datagram size, APDU size,
constructed depth, element counts, string/octet/bit lengths, segment counts,
reassembled APDU size). Application-configured hard bounds win over
peer-advertised maxima. Malformed or hostile inputs should fail with
`ErrMalformed` / `ErrLimitExceeded` rather than unbounded allocation.

Device observation retention is bounded by `client.RegistryOptions`
(defaults: 4096 observations, 8 paths per instance, 30-minute TTL). Hostile
I-Am floods must not grow process memory without bound; eviction is reported
via diagnostics.

BACnet/IP endpoints are IPv4-only. Custom transports that inject IPv6
`ImmediatePeer` values are diagnosed and discarded; they must not panic.

## Reporting a vulnerability

Report security vulnerabilities **privately** — do not open a public GitHub
issue.

Send a report to: **security@otfabric.com**

Include:

- Description and potential impact
- Steps to reproduce or a minimal proof-of-concept
- Affected version / commit

**Expected acknowledgement:** within 5 business days.

We coordinate fix and disclosure timing with reporters and aim to ship a patch
within 90 days of confirmation.

## Scope

In scope:

- Panics from malformed BVLC / NPDU / APDU / service payloads
- Memory exhaustion via peer-controlled lengths or nesting
- Data races detectable with the race detector
- Defects that let a remote peer crash or deadlock the process

Out of scope:

- Inherent lack of BACnet/IP confidentiality or authenticity
- Issues in dependencies not maintained here
- Attacks requiring physical access to the host

## Disclosure

Coordinated disclosure. Please allow a reasonable fix window before public
disclosure. Reporters are credited in release notes unless they prefer anonymity.
