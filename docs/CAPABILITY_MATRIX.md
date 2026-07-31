# Capability matrix (Horizon 1)

Derived from [`capabilities.yaml`](capabilities.yaml).

| Area | Capability | Horizon 1 |
|------|------------|-----------|
| Data link | BACnet/IP IPv4 UDP | Required |
| Data link | Default port 47808 (`0xBAC0`) | Required |
| Data link | Foreign-device registration | Optional |
| Data link | Receive BBMD Forwarded-NPDU | Required |
| Data link | BBMD server / BDT management | Unsupported |
| Service | Who-Is | Required |
| Service | I-Am (receive / observe) | Required |
| Service | ReadProperty | Required |
| Service | ReadPropertyMultiple | Required |
| Service | WriteProperty | Required |
| Service | SubscribeCOV | Required |
| Service | SubscribeCOVProperty | Optional |
| Service | WritePropertyMultiple | Unsupported |
| Network | Routed access (DNET/DADR) | Required |
| Network | Who-Is-Router-To-Network | Required |
| Network | I-Am-Router-To-Network (receive) | Required |
| Segmentation | Segmented ComplexACK receive | Required |
| Segmentation | Segmented confirmed request send | Optional |
| Other | Native MS/TP | Unsupported |
| Other | BACnet/IPv6 | Unsupported |
| Other | BACnet/SC | Unsupported |
| Other | Full server / device model | Unsupported |

See also [BIBB_SUPPORT.md](BIBB_SUPPORT.md) and
[profiles/HORIZON1_CLIENT_PICS.md](profiles/HORIZON1_CLIENT_PICS.md).
