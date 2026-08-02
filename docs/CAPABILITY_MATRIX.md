# Capability matrix (supervisory client)

Derived from [`capabilities.yaml`](capabilities.yaml). Includes WPM, ReadRange,
Who-Has/I-Have, EventNotification receive, opt-in DCC/Reinit, and segmented
confirmed-request send; evidenced under [INTEROP.md](../INTEROP.md) /
[RELEASE.md](../RELEASE.md) `v0.2.0` / `v0.2.1`.

| Area | Capability | Horizon 1 |
|------|------------|-----------|
| Data link | BACnet/IP IPv4 UDP | Required |
| Data link | Default port 47808 (`0xBAC0`) | Required |
| Data link | Foreign-device registration | Optional |
| Data link | Receive BBMD Forwarded-NPDU | Required |
| Data link | BBMD server / BDT management | Unsupported |
| Service | Who-Is | Required |
| Service | I-Am (receive / observe) | Required |
| Service | Who-Has | Required |
| Service | I-Have (receive / observe) | Required |
| Service | ReadProperty | Required |
| Service | ReadPropertyMultiple | Required |
| Service | WriteProperty | Required |
| Service | SubscribeCOV | Required |
| Service | SubscribeCOVProperty | Optional |
| Service | WritePropertyMultiple | Required |
| Service | ReadRange | Required |
| Service | EventNotification (receive) | Required |
| Service | AcknowledgeAlarm | Required |
| Service | GetEventInformation | Required |
| Service | DeviceCommunicationControl | Optional (opt-in) |
| Service | ReinitializeDevice | Optional (opt-in) |
| Network | Routed access (DNET/DADR) | Required |
| Network | Who-Is-Router-To-Network | Required |
| Network | I-Am-Router-To-Network (receive) | Required |
| Segmentation | Segmented ComplexACK receive | Required |
| Segmentation | Segmented confirmed request send | Required |
| Other | Native MS/TP | Unsupported |
| Other | BACnet/IPv6 | Unsupported |
| Other | BACnet/SC | Unsupported |
| Other | Full server / device model | Unsupported |

See also [BIBB_SUPPORT.md](BIBB_SUPPORT.md) and
[profiles/HORIZON1_CLIENT_PICS.md](profiles/HORIZON1_CLIENT_PICS.md).
