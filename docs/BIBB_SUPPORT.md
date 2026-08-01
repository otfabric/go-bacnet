# BIBB support (Horizon 1)

Exact ASHRAE BIBB names and clause citations require a licensed check against
the tracked baseline in [`standard-baseline.yaml`](standard-baseline.yaml). Until
that review is recorded, this file lists **intended A-side capabilities
descriptively** rather than claiming certified BIBB identifiers.

## Intended A-side capabilities

| Intent (descriptive) | Horizon 1 posture |
|----------------------|-------------------|
| Data sharing — read single property | Required (ReadProperty) |
| Data sharing — read multiple properties | Required (ReadPropertyMultiple) |
| Data sharing — write single property | Required (WriteProperty, priority + NULL relinquish) |
| Data sharing — write multiple properties | Required (WritePropertyMultiple; first-failed Error model) |
| Data sharing — read range | Required (ReadRange; byPosition / bySequence / byTime) |
| Data sharing — change-of-value subscribe | Required (SubscribeCOV); SubscribeCOVProperty optional |
| Device management — dynamic device binding | Required (Who-Is / I-Am observation) |
| Device management — dynamic object binding | Required (Who-Has / I-Have; bounded object registry) |
| Alarm / event — notification receive | Required (Confirmed/Unconfirmed EventNotification) |
| Alarm / event — acknowledge / get event information | Required (AcknowledgeAlarm; GetEventInformation) |
| Device management — communication control / reinitialize | Optional opt-in (DeviceCommunicationControl; ReinitializeDevice) |
| Network management — routed device access | Required (remote Address, router messages as needed) |
| Data link — BACnet/IP Annex J IPv4 | Required |
| Data link — foreign device | Optional registration with one BBMD |
| Data link — BBMD client receive | Required (Forwarded-NPDU); server unsupported |

When licensed baseline mapping is complete, replace descriptive rows with exact
BIBB codes and update [TRACEABILITY.md](TRACEABILITY.md) / the example PICS.
