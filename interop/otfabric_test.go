//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"errors"
	"math"
	"os"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/service"
)

// The otfabric image, ghcr.io/otfabric/bacnet-interop-otfabric (bacnet-interop
// own/go-bacnet/README.md), is go-bacnet itself: the latest release, behind
// the container and client-driver contracts. These tests run this commit
// against it in both roles, so that a change on the wire that breaks an
// earlier release shows up. They run only when BACNET_INTEROP_OTFABRIC_IMAGE
// names the image (the manual Self-interop workflow), never in the Interop
// workflow or `make interop-pinned`. A pass is not independent evidence
// (INTEROP.md): both sides are go-bacnet.

const (
	otfabricImageEnv = "BACNET_INTEROP_OTFABRIC_IMAGE"
	otfabricAdapter  = "otfabric"
)

func otfabricImage(t *testing.T) string {
	t.Helper()
	image := getEnv(otfabricImageEnv, "")
	if image == "" && os.Getenv("BACNET_INTEROP_PEER_CONTAINER") != "" {
		// The in-network re-execution (macOS) gets the started peer from
		// the environment (peerFromEnv) and needs no image; skipping here
		// would make the outer run pass without a check.
		return "in-network"
	}
	if image == "" {
		t.Skipf("%s is not set: the self-interop tests run only by hand", otfabricImageEnv)
	}
	return image
}

// The released go-bacnet client runs every scenario file against this
// commit's server.
func TestOtfabricClientAgainstServer(t *testing.T) {
	runClientImage(t, otfabricImage(t), otfabricAdapter, stackClientRuns)
}

// This commit's client against the released go-bacnet device, on
// device-baseline-v2.
func TestOtfabricDevice(t *testing.T) {
	forPeers(t, []interopPeer{{otfabricAdapter, otfabricImage(t)}}, func(t *testing.T, dev deviceFixture, peer *peerHandle) {
		c := newClient(t)
		av, wantPV := analogValueObject(dev)

		t.Run("endpoint-only-target", func(t *testing.T) {
			ctx, cancel := withTimeout(t, 10*time.Second)
			defer cancel()
			val, err := c.ReadProperty(ctx, client.Target{Endpoint: peer.target.Endpoint}, deviceObject(dev),
				bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName})
			if err != nil {
				t.Fatalf("ReadProperty: %v", err)
			}
			if got := characterString(t, val); got != dev.DeviceName {
				t.Fatalf("object-name = %q, want %q", got, dev.DeviceName)
			}
		})

		t.Run("object-list", func(t *testing.T) {
			ctx, cancel := withTimeout(t, 15*time.Second)
			defer cancel()
			list, err := c.ReadObjectList(ctx, peer.target, deviceObject(dev), 0)
			if err != nil {
				t.Fatalf("ReadObjectList: %v", err)
			}
			if !containsObject(list, deviceObject(dev)) || !containsObject(list, av) {
				t.Fatalf("object-list %v lacks %v or %v", list, deviceObject(dev), av)
			}
		})

		t.Run("rpm-batched", func(t *testing.T) {
			ctx, cancel := withTimeout(t, 20*time.Second)
			defer cancel()
			specs := []service.ReadAccessSpecification{
				{Object: deviceObject(dev), Properties: []bacnet.PropertyReference{
					{Identifier: bacnet.PropertyObjectName},
					{Identifier: unknownPropertyID},
				}},
				{Object: av, Properties: []bacnet.PropertyReference{{Identifier: bacnet.PropertyPresentValue}}},
			}
			results, err := c.ReadPropertyMultipleBatched(ctx, peer.target, specs, client.RPMBatchOptions{MaxPropertiesPerBatch: 1})
			if err != nil {
				t.Fatalf("ReadPropertyMultipleBatched: %v", err)
			}
			if len(results) != 2 || len(results[0].Properties) != 2 || len(results[1].Properties) != 1 {
				t.Fatalf("results %+v, want 2 objects with 2 and 1 properties", results)
			}
			var er *bacnet.ErrorResponse
			if e := results[0].Properties[1].Err; !errors.As(e, &er) || er.Code != bacnet.ErrorCodeUnknownProperty {
				t.Fatalf("unknown property result = %v, want unknown-property error", e)
			}
			pv, err := bacnet.AsReal(findPropertyValue(t, results[1], bacnet.PropertyPresentValue))
			if err != nil {
				t.Fatal(err)
			}
			if math.Abs(float64(pv-wantPV)) > 0.01 {
				t.Fatalf("present-value = %v, want %v", pv, wantPV)
			}
		})

		t.Run("unknown-object", func(t *testing.T) {
			ctx, cancel := withTimeout(t, 10*time.Second)
			defer cancel()
			missing := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogInput, Instance: 4000000}
			_, err := c.ReadProperty(ctx, peer.target, missing, bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue})
			var er *bacnet.ErrorResponse
			if !errors.As(err, &er) || er.Class != bacnet.ErrorClassObject || er.Code != bacnet.ErrorCodeUnknownObject {
				t.Fatalf("got %v, want object/unknown-object", err)
			}
		})
	})
}
