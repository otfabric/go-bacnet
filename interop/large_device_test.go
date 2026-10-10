//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/service"
)

// largeDeviceFixture is the device model whose Object_List does not fit one
// 50-octet APDU. Only the bacnet-stack adapter ships it (bacnet-interop
// 0.10.0).
const largeDeviceFixture = "device-baseline-v9"

// smallAPDU is the Max_APDU_Length_Accepted the constrained client advertises.
const smallAPDU = 50

func deviceFixturePath(t *testing.T, fixture string) string {
	t.Helper()
	name := fixture + ".json"
	var candidates []string
	if root := os.Getenv("BACNET_INTEROP_ROOT"); root != "" {
		candidates = append(candidates, filepath.Join(root, "fixtures", "device", name))
	}
	candidates = append(candidates,
		filepath.Join("..", "bacnet-interop", "fixtures", "device", name),
		filepath.Join("..", "..", "bacnet-interop", "fixtures", "device", name),
	)
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	failOrSkip(t, "%s not found; set BACNET_INTEROP_ROOT to a bacnet-interop checkout that has it", name)
	return ""
}

func largeDeviceFixturePath(t *testing.T) string {
	return deviceFixturePath(t, largeDeviceFixture)
}

var fixtureObjectTypes = map[string]bacnet.ObjectType{
	"device":       bacnet.ObjectTypeDevice,
	"analog-value": bacnet.ObjectTypeAnalogValue,
	"binary-value": bacnet.ObjectTypeBinaryValue,
	"trend-log":    bacnet.ObjectTypeTrendLog,
}

// fixtureObjects returns the identifiers of every object the fixture declares.
func fixtureObjects(t *testing.T, dev deviceFixture) []bacnet.ObjectIdentifier {
	t.Helper()
	out := make([]bacnet.ObjectIdentifier, 0, len(dev.Objects))
	for _, o := range dev.Objects {
		typ, ok := fixtureObjectTypes[o.Type]
		if !ok {
			t.Fatalf("fixture %s: object type %q is not mapped by this test", dev.Fixture, o.Type)
		}
		out = append(out, bacnet.ObjectIdentifier{Type: typ, Instance: o.Instance})
	}
	return out
}

func requireTooLargeAbort(t *testing.T, what string, err error) {
	t.Helper()
	var abort *bacnet.AbortError
	if !errors.As(err, &abort) {
		t.Fatalf("%s: got %v, want an Abort (bacnet-stack cannot segment a reply)", what, err)
	}
	if abort.Reason != bacnet.AbortReasonSegmentationNotSupported {
		t.Fatalf("%s: abort reason %s(%d), want segmentation-not-supported",
			what, bacnet.AbortReasonName(abort.Reason), abort.Reason)
	}
	t.Logf("%s refused: %v", what, err)
}

// A device whose Object_List does not fit the client's Max_APDU and that
// cannot segment aborts the whole-array read. ReadObjectList must then read
// the list element by element and return the same list an unconstrained
// client gets in one reply.
func TestBacnetStackReadObjectListElementByElement(t *testing.T) {
	requireAdapterVersion(t, stackPeer().image, "0.10.0")
	forPeersWithFixture(t, []interopPeer{stackPeer()}, largeDeviceFixturePath, largeDeviceFixture,
		func(t *testing.T, peer *peerHandle) {
			dev := loadDeviceFixture(t)
			if dev.Fixture != largeDeviceFixture {
				t.Fatalf("loaded fixture %q, want %q", dev.Fixture, largeDeviceFixture)
			}
			ctx, cancel := withTimeout(t, 60*time.Second)
			defer cancel()
			device := deviceObject(dev)
			objectList := bacnet.PropertyReference{Identifier: bacnet.PropertyObjectList}

			// Reference: an unconstrained client gets the list in one reply.
			wide := newClient(t)
			want, err := wide.ReadObjectList(ctx, peer.target, device, 0)
			if err != nil {
				t.Fatalf("ReadObjectList with the default Max_APDU: %v", err)
			}
			for _, id := range fixtureObjects(t, dev) {
				if !containsObject(want, id) {
					t.Fatalf("object-list lacks fixture object %v: %v", id, want)
				}
			}
			if len(want) < len(dev.Objects) {
				t.Fatalf("object-list has %d entries, the fixture declares %d", len(want), len(dev.Objects))
			}
			// Five octets per identifier: the list alone must exceed the APDU.
			if 5*len(want) <= smallAPDU {
				t.Fatalf("object-list of %d entries fits %d octets; the fixture is too small for this test", len(want), smallAPDU)
			}

			// Precondition: the constrained whole-array read is refused.
			narrow := newClientWithAdvertisedMaxAPDU(t, smallAPDU)
			_, err = narrow.ReadProperty(ctx, peer.target, device, objectList)
			requireTooLargeAbort(t, "ReadProperty object-list with Max_APDU 50", err)

			// The element count is still readable (array index 0).
			zero := uint32(0)
			countVal, err := narrow.ReadProperty(ctx, peer.target, device,
				bacnet.PropertyReference{Identifier: bacnet.PropertyObjectList, ArrayIndex: &zero})
			if err != nil {
				t.Fatalf("ReadProperty object-list[0]: %v", err)
			}
			count, err := bacnet.AsUnsigned(countVal)
			if err != nil {
				t.Fatal(err)
			}
			if count != uint64(len(want)) {
				t.Fatalf("object-list[0] = %d, the whole list has %d entries", count, len(want))
			}

			got, err := narrow.ReadObjectList(ctx, peer.target, device, 0)
			if err != nil {
				t.Fatalf("ReadObjectList with Max_APDU 50: %v (read %d of %d)", err, len(got), len(want))
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("element-by-element object-list differs from the whole read\n got: %v\nwant: %v", got, want)
			}
			t.Logf("object-list of %d entries read element by element; the whole read aborts at Max_APDU %d", len(got), smallAPDU)

			// maxItems caps the element-by-element read too.
			capped, err := narrow.ReadObjectList(ctx, peer.target, device, 5)
			if err != nil {
				t.Fatalf("ReadObjectList maxItems=5: %v", err)
			}
			if !reflect.DeepEqual(capped, want[:5]) {
				t.Fatalf("ReadObjectList maxItems=5 = %v, want %v", capped, want[:5])
			}
		})
}

// One ReadPropertyMultiple for 41 properties of ten objects cannot be answered
// in 50 octets. ReadPropertyMultipleBatched must shrink its batches after the
// Abort and still return one complete result per specification.
func TestBacnetStackRPMBatchedShrinksAfterAbort(t *testing.T) {
	requireAdapterVersion(t, stackPeer().image, "0.10.0")
	forPeersWithFixture(t, []interopPeer{stackPeer()}, largeDeviceFixturePath, largeDeviceFixture,
		func(t *testing.T, peer *peerHandle) {
			dev := loadDeviceFixture(t)
			ctx, cancel := withTimeout(t, 60*time.Second)
			defer cancel()

			props := []bacnet.PropertyReference{
				{Identifier: bacnet.PropertyObjectName},
				{Identifier: bacnet.PropertyPresentValue},
				{Identifier: bacnet.PropertyUnits},
				{Identifier: bacnet.PropertyStatusFlags},
			}
			type expect struct {
				name string
				pv   float32
			}
			var specs []service.ReadAccessSpecification
			var wantObj []expect
			for _, o := range dev.Objects {
				if o.Type != "analog-value" || o.Instance < 100 || len(specs) == 10 {
					continue
				}
				pv, ok := o.PresentValue.(float64)
				if !ok {
					t.Fatalf("fixture analog-value:%d has no numeric present_value", o.Instance)
				}
				specs = append(specs, service.ReadAccessSpecification{
					Object:     bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: o.Instance},
					Properties: append([]bacnet.PropertyReference(nil), props...),
				})
				wantObj = append(wantObj, expect{name: o.ObjectName, pv: float32(pv)})
			}
			if len(specs) != 10 {
				t.Fatalf("fixture %s has %d analog-value objects with instance >= 100, need 10", dev.Fixture, len(specs))
			}
			// A property-level error in the middle must survive the splitting.
			const errSpec, errProp = 4, 2
			specs[errSpec].Properties = append(specs[errSpec].Properties[:errProp:errProp],
				append([]bacnet.PropertyReference{{Identifier: unknownPropertyID}}, specs[errSpec].Properties[errProp:]...)...)
			total := 0
			for _, s := range specs {
				total += len(s.Properties)
			}

			// Reference: everything in one reply with the default Max_APDU.
			wide := newClient(t)
			want, err := wide.ReadPropertyMultiple(ctx, peer.target, specs)
			if err != nil {
				t.Fatalf("ReadPropertyMultiple with the default Max_APDU: %v", err)
			}

			// Precondition: the same request is refused at Max_APDU 50. This is
			// also the first request the batched helper sends, since all
			// references fit one request and MaxPropertiesPerBatch allows them.
			narrow := newClientWithAdvertisedMaxAPDU(t, smallAPDU)
			_, err = narrow.ReadPropertyMultiple(ctx, peer.target, specs)
			requireTooLargeAbort(t, fmt.Sprintf("ReadPropertyMultiple of %d properties with Max_APDU 50", total), err)

			got, err := narrow.ReadPropertyMultipleBatched(ctx, peer.target, specs,
				client.RPMBatchOptions{MaxPropertiesPerBatch: 64})
			if err != nil {
				t.Fatalf("ReadPropertyMultipleBatched with Max_APDU 50: %v", err)
			}
			if len(got) != len(specs) {
				t.Fatalf("%d results, want one per specification (%d)", len(got), len(specs))
			}
			for i, res := range got {
				if res.Object != specs[i].Object {
					t.Fatalf("result %d is for %v, want %v", i, res.Object, specs[i].Object)
				}
				if len(res.Properties) != len(specs[i].Properties) {
					t.Fatalf("%v: %d property results, want %d", res.Object, len(res.Properties), len(specs[i].Properties))
				}
				for j, pr := range res.Properties {
					if pr.Property.Identifier != specs[i].Properties[j].Identifier {
						t.Fatalf("%v result %d is property %d, want %d (order must follow the request)",
							res.Object, j, pr.Property.Identifier, specs[i].Properties[j].Identifier)
					}
					ref := want[i].Properties[j]
					if pr.Property.Identifier == unknownPropertyID {
						var er *bacnet.ErrorResponse
						if !errors.As(pr.Err, &er) || er.Code != bacnet.ErrorCodeUnknownProperty {
							t.Fatalf("%v unknown property: %v, want unknown-property error", res.Object, pr.Err)
						}
						continue
					}
					if pr.Err != nil {
						t.Fatalf("%v property %d: %v", res.Object, pr.Property.Identifier, pr.Err)
					}
					if !reflect.DeepEqual(pr.Value, ref.Value) {
						t.Fatalf("%v property %d = %+v, the single-reply read gave %+v",
							res.Object, pr.Property.Identifier, pr.Value, ref.Value)
					}
				}
				if name := characterString(t, findPropertyValue(t, res, bacnet.PropertyObjectName)); name != wantObj[i].name {
					t.Fatalf("%v object-name = %q, fixture says %q", res.Object, name, wantObj[i].name)
				}
				pv, err := bacnet.AsReal(findPropertyValue(t, res, bacnet.PropertyPresentValue))
				if err != nil {
					t.Fatal(err)
				}
				if math.Abs(float64(pv-wantObj[i].pv)) > 0.001 {
					t.Fatalf("%v present-value = %v, fixture says %v", res.Object, pv, wantObj[i].pv)
				}
			}
			t.Logf("%d properties of %d objects merged from batches that fit Max_APDU %d", total, len(specs), smallAPDU)
		})
}
