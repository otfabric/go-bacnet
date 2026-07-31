# Real-device gate

## Policy

| Status | Requirement |
|--------|-------------|
| **alpha** | Pre-hardening / incomplete oracle evidence |
| **production-candidate** | Current — wire/runtime P0 closed; reproducible oracle/lab interop evidence ([PLAN.md](../PLAN.md) Batches 1–4). Does **not** require this real-device checklist. |
| **production-usable** | Allowed in docs and release notes only after the checklist below is complete. |

**Production-usable** requires successful smoke testing against **≥ 2 real,
independent BACnet/IP devices** (distinct vendors or independent implementations —
not two instances of the same simulator image).

Container oracles in `bacnet-interop` (bacnet-stack, BACpypes3, BACnet4J; plus the
`bip-router` topology aid) are necessary but **not sufficient** for this gate.

## Checklist

- [ ] Device A: Who-Is / I-Am discovery (instance, address, max APDU, segmentation, vendor ID observed)
- [ ] Device B: same discovery path on an independent device
- [ ] ReadProperty of a stable property on each device
- [ ] ReadPropertyMultiple (at least one multi-property read) on at least one device
- [ ] WriteProperty where safe (or documented skip if device is read-only in the lab)
- [ ] Optional: COV subscribe/notify if the device supports it
- [ ] Optional: routed or Forwarded-NPDU path if the lab topology provides it
- [ ] Evidence recorded (date, operators, firmware/model, sanitized notes) in release notes or an internal lab log linked from RELEASE.md
- [ ] No unresolved critical defects from the smoke run

Until every required item is checked, README and RELEASE language must not say
**production-usable**. Do not claim multi-vendor hardware interoperability
without this evidence. **production-candidate** is claimed once
[PLAN.md](../PLAN.md) Batches 1–4 are closed (already done for the current tree).
