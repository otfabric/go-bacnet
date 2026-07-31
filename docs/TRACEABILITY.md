# Traceability

Maps Horizon 1 services to concrete tests. **135.1 test IDs remain placeholders**
until mapped against a licensed 135.1-2023 baseline
([`standard-baseline.yaml`](standard-baseline.yaml)).

| Service / behaviour | Placeholder 135.1 ID | Go tests |
|---------------------|----------------------|----------|
| Who-Is / I-Am discovery | `H1-DM-DDB-001` | `service/discovery_test.go`, `service/discovery_strict_test.go`; `client/client_test.go` (`TestDiscoverIAmVirtual`); interop `Test*WhoIsIAm` |
| ReadProperty | `H1-DS-RP-001` | `service/property_test.go`, `service/discovery_strict_test.go` (`TestDecodeReadProperty*`); interop `Test*ReadDeviceObjectName`, `Test*ReadAnalogValue`, `Test*ReadPropertyUnknownPropertyError` |
| ReadPropertyMultiple | `H1-DS-RPM-001` | `service/read_property_multiple*`, `service/rpm_error_test.go`; interop `Test*ReadPropertyMultiple*` |
| WriteProperty (+ priority / NULL) | `H1-DS-WP-001` | `service/write_property*`; interop `Test*WritePropertyReadbackReset` |
| SubscribeCOV / notifications | `H1-DS-COV-001` | `service/cov*`; `client/cov_test.go`; interop `Test*COVSubscribeNotifyCancel`, `TestBACpypes3COVRenew` |
| Routed addressing / router msgs | `H1-NM-RT-001` | `client/path_test.go`, `client/routing_test.go`, `npdu`; interop `Test*Routed*`, `TestBACpypes3RoutedWhoIsRouterReadProperty` |
| Foreign-device registration | `H1-BVLC-FD-001` | `client/bbmd_test.go`; interop `TestBACpypes3ForeignDeviceWhoIsReadProperty`, `TestBACnet4JForeignDeviceWhoIsReadProperty` |
| Forwarded-NPDU receive | `H1-BVLC-FWD-001` | `bvlc`; client FD path; fixtures `bvlc-forwarded-npdu-empty` |
| Segmented ComplexACK reassembly | `H1-SEG-ACK-001` | `client/segmentation_test.go`, `client/transaction_test.go`; interop `Test*SegmentedReadPropertyMultiple` |
| Error / Reject / Abort mapping | `H1-APDU-ERR-001` | `apdu`; `client/invoke.go` + interop `Test*Reject*`, `Test*Abort*` |
| Decode limits / malformed tags | `H1-DEC-LIM-001` | root tags/fuzz; `internal/fixtures` corpus (`decode_tag`, `decode_bvlc` negative) |
| Fixture corpus (executable ops) | `H1-FIX-001` | `internal/fixtures/corpus_test.go` (all `fixtures/codec/*` operations) |
| Interop vs bacnet-stack / BACpypes3 / BACnet4J | `H1-IOP-*` | `interop/` (`-tags=interop`); see `INTEROP.md` |

Update placeholder IDs to real 135.1 initiation references when the licensed
mapping is complete.
