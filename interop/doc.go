//go:build interop

// SPDX-License-Identifier: MIT

// Package interop holds peer assertions gated by -tags=interop.
//
// Adapter images and fixtures live in github.com/otfabric/bacnet-interop.
// Tests start peer containers, wait for the JSON readiness event, exercise
// the go-bacnet client, and tear the containers down.
//
//	GOWORK=off go test -tags=interop -count=1 ./interop/...
//
// Environment (all optional):
//
//	BACNET_STACK_IMAGE   default bacnet-interop-bacnet-stack:local
//	BACPYPES3_IMAGE      default bacnet-interop-bacpypes3:local
//	BACNET_INTEROP_SKIP  if non-empty, skip all peer tests
//
// See INTEROP.md.
package interop
