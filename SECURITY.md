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

The server (`server`, `server/object`, `bbmd`) answers any peer that can
reach its port, so whoever can send it a datagram can read every property
and make every write its objects accept. It has no access control beyond
these:

- `Hooks.BeforeWrite` sees every client write to an existing object
  (WriteProperty, each write of WritePropertyMultiple, AddListElement,
  RemoveListElement) and can refuse it with a BACnet error; `BeforeDelete`
  does the same for DeleteObject. CreateObject works only for object types
  the application enables with `Device.RegisterFactory`; its initial values
  are written without `BeforeWrite`. AtomicWriteFile also passes
  `BeforeWrite` (a write of the File's File_Size whose value carries the
  start position and the data), and so does every write a Schedule makes to
  the objects in its List_Of_Object_Property_References: the hook is asked
  for the referenced object, not only for the Schedule.
- `WithPassword` makes DeviceCommunicationControl and ReinitializeDevice
  require a password (sent in clear text). Without it none is checked.
- A broadcast Who-Is gets a broadcast I-Am, so the server can be used to
  make one datagram produce one broadcast. Bind it to one interface
  (`WithInterface`, `WithLocalAddr`) on networks where that matters.
- The `bbmd` package configures its BDT locally only:
  Write-Broadcast-Distribution-Table is refused. Foreign-device registration
  is accepted from any peer unless `RejectRegistrations` is set, except
  from a BDT peer or the BBMD itself; a registered foreign device can have
  its broadcasts distributed. A Forwarded-NPDU is passed on (to the local
  network and the FDT) only when it comes from a BDT peer; from any other
  node it is delivered locally only. It is never sent back to its sender or
  to a BDT peer through the FDT.

Server resource bounds: request workers and their queue (`WithWorkers`,
default 4 and 64; a confirmed request that finds the queue full gets Abort
out-of-resources, an unconfirmed one is dropped), COV subscriptions and
their lifetime (`WithCOVLimits`, default 256), the monitored references
across all COV subscriptions (`WithCOVReferenceLimit`, default 4096; one
SubscribeCOVPropertyMultiple holds at most as many references as fit one
notification), the FDT (`bbmd.Config.MaxForeignDevices`, default 128),
segments per message (`WithSegmentation`), segmented requests being
reassembled and segmented replies being sent, in total and per origin
(`WithSegmentLimits`), and the decode limits below (`WithDecodeLimits`).
A reply is built against the requester's reply budget and stops once it
exceeds it (ReadPropertyMultiple, Active_COV_Subscriptions,
GetEventInformation). A segmented reply runs off the request workers and
has an overall deadline. A COV notification larger than one APDU is not
sent and is reported as a diagnostic.

DeviceCommunicationControl and ReinitializeDevice passwords are sent in clear
text, as the protocol defines. The library never logs them
([OBSERVABILITY.md](OBSERVABILITY.md)).

## Allocation limits

All public parsers honour `bacnet.DecodeLimits` (datagram size, APDU size,
constructed depth, element counts, string/octet/bit lengths, segment counts,
reassembled APDU size). Application-configured hard bounds win over
peer-advertised maxima. The client and the server default to
`bacnet.DefaultDecodeLimits` with `MaxAPDUSize` raised to `MaxDatagramSize`
(65535), so an APDU longer than the advertised Max APDU is still received;
`WithDecodeLimits` sets a lower bound. Malformed or hostile inputs should fail with
`ErrMalformed` / `ErrLimitExceeded` rather than unbounded allocation.

Device observation retention is bounded by `client.RegistryOptions`
(defaults: 4096 observations, 8 paths per instance, 30-minute TTL). Hostile
I-Am floods must not grow process memory without bound; eviction is reported
via diagnostics.

BACnet/IP endpoints are IPv4-only. Custom transports that inject IPv6
`ImmediatePeer` values are diagnosed and discarded before BVLC handling,
in the client, the server and the BBMD; they must not panic. The client's
source-path match fails closed for such a peer.

## Supported versions

| Version | Supported |
|---------|-----------|
| Latest release | Yes |
| Older releases | No |


## Reporting a vulnerability

Report security vulnerabilities **privately** — do not open a public GitHub
issue.

Send a report to: **security@otfabric.io**

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
