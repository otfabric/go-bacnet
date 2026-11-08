//go:build interop

// SPDX-License-Identifier: MIT

// Package interop holds peer assertions gated by -tags=interop.
//
// Adapter images and fixtures live in github.com/otfabric/bacnet-interop.
// Tests start peer containers, wait for the JSON readiness event, exercise
// the go-bacnet client, and tear the containers down.
//
//	make interop-pinned    # pull the images pinned in bacnet-interop-pin.json and run
//	make interop           # run against the images the variables below name
//
// Environment (all optional):
//
//	BACNET_INTEROP_ROOT      bacnet-interop checkout (default ../bacnet-interop)
//	BACNET_STACK_IMAGE       default bacnet-interop-bacnet-stack:local
//	BACPYPES3_IMAGE          default bacnet-interop-bacpypes3:local
//	BACNET4J_IMAGE           default bacnet-interop-bacnet4j:local
//	WORLDIETY_IMAGE          default bacnet-interop-worldiety:local
//	BIP_ROUTER_IMAGE         default bacnet-interop-bip-router:local
//	SYSIOBACNET_IMAGE        default bacnet-interop-sysiobacnet:local ("none" skips its tests)
//	BACNET_STACK_ROUTER_IMAGE  default bacnet-interop-bacnet-stack-router:local ("none" skips its test)
//	CBSIGMA_IMAGE            default bacnet-interop-cbsigma:local ("none" skips its tests)
//	ULBIOS_IMAGE             default bacnet-interop-ulbios:local ("none" skips its tests)
//	BACNET_INTEROP_REQUIRED  if non-empty, a missing image, fixture or Docker fails instead of skipping
//	BACNET_INTEROP_SKIP      if non-empty, skip all peer tests
//	BACNET_INTEROP_GO_IMAGE  Go image for the in-network re-execution on Docker Desktop
//	                         and for the server container
//	BACNET_INTEROP_SERVE     set inside the server container only
//
// On Linux the tests reach the peer containers directly. On Docker Desktop
// (macOS, Windows) each test re-executes itself inside a Go container on the
// peer's network, compiling this working tree there.
//
// TestBACnetStackClientAgainstServer, TestBACnet4JClientAgainstServer,
// TestBACpypes3ClientAgainstServer and TestUlbiosClientAgainstServer reverse
// the roles: the go-bacnet server runs in a Go container and a peer image in
// client mode drives it (bacnet-interop docs/CLIENT_DRIVER_CONTRACT.md). An
// image without client mode is skipped.
//
// The dissector tag (-tags=dissector, make dissector) checks client requests
// and the server's replies against Wireshark's tshark.
//
// See INTEROP.md.
package interop
