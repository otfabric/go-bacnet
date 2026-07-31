# Standard baseline

Horizon 1 develops against **ANSI/ASHRAE 135-2024** with **Protocol Revision 31**
as the documented baseline. Conformance-oriented mapping uses
**ANSI/ASHRAE 135.1-2023**.

The machine-readable source of truth for edition, revision, and tracked
addenda/interpretations is:

[`standard-baseline.yaml`](standard-baseline.yaml)

Addenda and interpretation lists start empty until each item is checked against
a licensed baseline copy. Update the YAML when tracking begins; keep this file
as short prose only.

The client **adapts** to remote capabilities (I-Am / Device object) and does not
require remote devices to implement Revision 31 features.
