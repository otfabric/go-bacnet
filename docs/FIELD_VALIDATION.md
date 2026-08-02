# Field validation

Repeatable checklist for recording physical BACnet/IP device evidence.

Field evidence supplements container/stack interoperability. It does **not**
gate continued client development or define universal production readiness.

Record completed runs in [COMPATIBILITY.md](COMPATIBILITY.md).

## Checklist

For each device under test:

| Item | Record |
|---|---|
| Device make / model | |
| Firmware / application version | |
| BACnet protocol revision (if advertised) | |
| Network topology (local / routed / BBMD / FD) | |
| Library version (`go-bacnet` tag) | |
| Interop pin (if any) | |
| Test date | |
| Operator / environment notes | |

### Services exercised

- [ ] Who-Is / I-Am (instance, address, max APDU, segmentation, vendor ID)
- [ ] ReadProperty (stable property)
- [ ] ReadPropertyMultiple (multi-property)
- [ ] WriteProperty (only where safe; otherwise document skip)
- [ ] WritePropertyMultiple (optional)
- [ ] COV subscribe / notify / cancel (optional)
- [ ] Event / alarm path (optional)
- [ ] File / other advanced services (optional)
- [ ] Routed or Forwarded-NPDU path (optional)

### Behaviour notes

| Topic | Observation |
|---|---|
| Segmentation | |
| Timeouts | |
| Error / Reject / Abort responses | |
| Known deviations from ASHRAE interpretation used by go-bacnet | |

## How to publish results

1. Complete this checklist for each device.
2. Add a row under **Hardware devices** in [COMPATIBILITY.md](COMPATIBILITY.md).
3. Optionally link artifacts from the corresponding [RELEASE.md](../RELEASE.md) section.
