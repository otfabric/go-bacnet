//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/service"
)

// Legacy BACpypes peer: PyPI bacpypes 0.19.0, the asyncore predecessor of
// BACpypes3, bacnet-interop adapter bacpypes (new in bacnet-interop 0.14.0).
// It is by the same author as BACpypes3 but a separate code base; see
// bacnet-interop PEER_SUPPORT.md for how independent the two are.
//
// The device serves upstream's services (Who-Is, Who-Has, ReadProperty,
// ReadPropertyMultiple, WriteProperty, SubscribeCOV, SubscribeCOVProperty,
// AtomicReadFile, AtomicWriteFile, DeviceCommunicationControl). The adapter
// adds WritePropertyMultiple, ReadRange, ReinitializeDevice and sinks for
// TimeSynchronization, text messages, private transfer and WriteGroup
// (adapter-shim): those tests say what the adapter does over the BACpypes
// codec, not what a stack does. BACNET_BBMD=1 runs the upstream BIPBBMD.

const (
	defaultBACpypesImage      = "bacnet-interop-bacpypes:local"
	bacpypesMinAdapterVersion = "0.14.0"
	bacpypesBBMD              = "BACNET_BBMD=1"
)

func bacpypesImage() string { return getEnv("BACPYPES_IMAGE", defaultBACpypesImage) }

func bacpypesPeer() interopPeer { return interopPeer{"bacpypes", bacpypesImage()} }

func startBACpypes(t *testing.T, env ...string) *peerHandle {
	t.Helper()
	requireAdapterVersion(t, bacpypesImage(), bacpypesMinAdapterVersion)
	return startPeer(t, bacpypesImage(), "bacpypes", env...)
}

// startBACpypesServerFixture serves device-server-v1 (files, the ten-record
// Trend Log, AV-2, NC-1).
func startBACpypesServerFixture(t *testing.T, env ...string) (deviceFixture, *peerHandle) {
	t.Helper()
	t.Setenv("BACNET_DEVICE_FIXTURE", deviceServerV1Path(t))
	dev := loadDeviceFixture(t)
	peer := startBACpypes(t, append([]string{
		"DEVICE_FIXTURE_FILE=/fixtures/device/device-server-v1.json",
		"FIXTURE=device-server-v1",
	}, env...)...)
	return dev, peer
}

func deviceServerV1Path(t *testing.T) string {
	t.Helper()
	for _, p := range []string{
		filepath.Join("..", "bacnet-interop", "fixtures", "device", "device-server-v1.json"),
		filepath.Join("..", "..", "bacnet-interop", "fixtures", "device", "device-server-v1.json"),
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Fatal("device-server-v1.json not found; open a sibling bacnet-interop checkout")
	return ""
}

// ---- discovery and reads --------------------------------------------------

func TestBACpypesWhoIsIAm(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startBACpypes(t)
	if peer.assertedByReexec {
		return
	}
	c := newDiscoveryClient(t)

	// A Who-Is whose range excludes the device gets no I-Am (clause 16.10.1.1).
	ctx, cancel := withTimeout(t, 10*time.Second)
	defer cancel()
	low, high := dev.DeviceInstance+1, dev.DeviceInstance+10
	if err := c.SendWhoIs(ctx, peer.endpoint, false, client.DiscoveryOptions{LowLimit: &low, HighLimit: &high}); err != nil {
		t.Fatalf("SendWhoIs: %v", err)
	}
	time.Sleep(1500 * time.Millisecond)
	for _, obs := range c.Devices() {
		if obs.Instance == dev.DeviceInstance {
			t.Fatalf("I-Am for a Who-Is range %d..%d", low, high)
		}
	}

	whoIsFinds(t, c, peer, dev.DeviceInstance, false)
	whoIsFinds(t, c, peer, dev.DeviceInstance, true)
	for _, obs := range c.Devices() {
		if obs.Instance != dev.DeviceInstance {
			continue
		}
		caps := obs.Capabilities
		// LocalDeviceObject's maxApduLengthAccepted default is 1024; the
		// adapter sets segmented-both and vendor 999.
		if !caps.MaxAPDULengthAccepted.Known || caps.MaxAPDULengthAccepted.Value != 1024 {
			t.Fatalf("I-Am max-APDU %+v, want 1024", caps.MaxAPDULengthAccepted)
		}
		if !caps.Segmentation.Known || caps.Segmentation.Value != 0 {
			t.Fatalf("I-Am segmentation %+v, want segmented-both (0)", caps.Segmentation)
		}
		if !caps.VendorID.Known || caps.VendorID.Value != 999 {
			t.Fatalf("I-Am vendor %+v, want 999", caps.VendorID)
		}
		return
	}
	t.Fatalf("no I-Am for %d", dev.DeviceInstance)
}

func TestBACpypesReadPropertyAndErrors(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startBACpypes(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()
	name, err := readPropertyRetry(t, ctx, c, peer.target, deviceObject(dev), bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName})
	if err != nil {
		t.Fatalf("device object-name: %v", err)
	}
	if got := characterString(t, name); got != dev.DeviceName {
		t.Fatalf("object-name %q, want %q", got, dev.DeviceName)
	}
	list, err := c.ReadObjectList(ctx, peer.target, deviceObject(dev), 0)
	if err != nil {
		t.Fatalf("ReadObjectList: %v", err)
	}
	av, wantPV := analogValueObject(dev)
	for _, o := range []bacnet.ObjectIdentifier{deviceObject(dev), av, {Type: bacnet.ObjectTypeBinaryValue, Instance: 1}, {Type: bacnet.ObjectTypeTrendLog, Instance: 0}} {
		if !containsObject(list, o) {
			t.Fatalf("object-list %v lacks %v", list, o)
		}
	}
	if pv := readReal(t, ctx, c, peer, av); math.Abs(float64(pv-wantPV)) > 0.01 {
		t.Fatalf("AV present-value %v, want %v", pv, wantPV)
	}
	// Array index 0 of the object list is its length.
	idx := uint32(0)
	n, err := c.ReadProperty(ctx, peer.target, deviceObject(dev), bacnet.PropertyReference{Identifier: bacnet.PropertyObjectList, ArrayIndex: &idx})
	if err != nil {
		t.Fatalf("object-list[0]: %v", err)
	}
	if got, err := bacnet.AsUnsigned(n); err != nil || int(got) != len(list) {
		t.Fatalf("object-list[0] = %v (%v), want %d", n, err, len(list))
	}
	_, err = c.ReadProperty(ctx, peer.target, bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 999},
		bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue})
	requireBACnetError(t, "unknown object", err, bacnet.ErrorClassObject, bacnet.ErrorCodeUnknownObject)
	_, err = c.ReadProperty(ctx, peer.target, av, bacnet.PropertyReference{Identifier: unknownPropertyID})
	requireBACnetError(t, "unknown property", err, bacnet.ErrorClassProperty, bacnet.ErrorCodeUnknownProperty)
	big := uint32(500)
	_, err = c.ReadProperty(ctx, peer.target, deviceObject(dev), bacnet.PropertyReference{Identifier: bacnet.PropertyObjectList, ArrayIndex: &big})
	requireBACnetError(t, "object-list[500]", err, bacnet.ErrorClassProperty, bacnet.ErrorCodeInvalidArrayIndex)
}

func TestBACpypesReadPropertyMultiple(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startBACpypes(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()
	av, wantPV := analogValueObject(dev)
	results, err := c.ReadPropertyMultiple(ctx, peer.target, []service.ReadAccessSpecification{
		{Object: deviceObject(dev), Properties: []bacnet.PropertyReference{
			{Identifier: bacnet.PropertyObjectName}, {Identifier: bacnet.PropertyProtocolRevision}, {Identifier: unknownPropertyID}}},
		{Object: av, Properties: []bacnet.PropertyReference{{Identifier: bacnet.PropertyPresentValue}, {Identifier: bacnet.PropertyStatusFlags}}},
		{Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 999}, Properties: []bacnet.PropertyReference{{Identifier: bacnet.PropertyPresentValue}}},
	})
	if err != nil {
		t.Fatalf("RPM: %v", err)
	}
	if got := characterString(t, findPropertyValue(t, results[0], bacnet.PropertyObjectName)); got != dev.DeviceName {
		t.Fatalf("object-name %q", got)
	}
	var er *bacnet.ErrorResponse
	if e := results[0].Properties[2].Err; !errors.As(e, &er) || er.Code != bacnet.ErrorCodeUnknownProperty {
		t.Fatalf("unknown property in RPM: %v", e)
	}
	if pv, err := bacnet.AsReal(findPropertyValue(t, results[1], bacnet.PropertyPresentValue)); err != nil || math.Abs(float64(pv-wantPV)) > 0.01 {
		t.Fatalf("AV present-value %v %v", pv, err)
	}
	if e := results[2].Properties[0].Err; !errors.As(e, &er) || er.Code != bacnet.ErrorCodeUnknownObject {
		t.Fatalf("unknown object in RPM: %v", e)
	}
	// ALL and REQUIRED are expanded by the device (clause 15.7.3.1.2).
	for _, which := range []bacnet.PropertyIdentifier{bacnet.PropertyAll, bacnet.PropertyRequired} {
		res, err := c.ReadPropertyMultiple(ctx, peer.target, []service.ReadAccessSpecification{{Object: av, Properties: []bacnet.PropertyReference{{Identifier: which}}}})
		if err != nil {
			t.Fatalf("RPM %d: %v", which, err)
		}
		if len(res) != 1 || len(res[0].Properties) < 5 {
			t.Fatalf("RPM %d: %+v", which, res)
		}
		if got := characterString(t, findPropertyValue(t, res[0], bacnet.PropertyObjectName)); got != "AV-1" {
			t.Fatalf("RPM %d object-name %q", which, got)
		}
	}
}

// ---- writes ---------------------------------------------------------------

func TestBACpypesWritePriorityAndRelinquish(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startBACpypes(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()
	av, baseline := analogValueObject(dev)
	if err := c.WritePriority(ctx, peer.target, av, 8, bacnet.RealValue(42)); err != nil {
		t.Fatalf("WritePriority 8: %v", err)
	}
	if pv := readReal(t, ctx, c, peer, av); pv != 42 {
		t.Fatalf("present-value after write %v, want 42", pv)
	}
	if err := c.WritePriority(ctx, peer.target, av, 5, bacnet.RealValue(7)); err != nil {
		t.Fatalf("WritePriority 5: %v", err)
	}
	if pv := readReal(t, ctx, c, peer, av); pv != 7 {
		t.Fatalf("present-value with priority 5 %v, want 7", pv)
	}
	if err := c.RelinquishPriority(ctx, peer.target, av, 5); err != nil {
		t.Fatalf("RelinquishPriority 5: %v", err)
	}
	if pv := readReal(t, ctx, c, peer, av); pv != 42 {
		t.Fatalf("present-value after relinquishing 5 %v, want 42", pv)
	}
	if err := c.RelinquishPriority(ctx, peer.target, av, 8); err != nil {
		t.Fatalf("RelinquishPriority 8: %v", err)
	}
	if pv := readReal(t, ctx, c, peer, av); math.Abs(float64(pv-baseline)) > 0.01 {
		t.Fatalf("present-value after relinquish %v, want %v", pv, baseline)
	}
	err := c.WriteProperty(ctx, peer.target, av, bacnet.PropertyReference{Identifier: bacnet.PropertyObjectIdentifier},
		bacnet.ObjectIDValue(bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 77}), nil)
	requireBACnetError(t, "write object-identifier", err, bacnet.ErrorClassProperty, bacnet.ErrorCodeWriteAccessDenied)
	// Description is an OptionalProperty, which upstream makes read-only
	// (bacpypes/object.py: mutable=False).
	err = c.WriteProperty(ctx, peer.target, av, bacnet.PropertyReference{Identifier: bacnet.PropertyDescription},
		bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: strings.Repeat("d", 10)}}, nil)
	requireBACnetError(t, "write description", err, bacnet.ErrorClassProperty, bacnet.ErrorCodeWriteAccessDenied)
	err = c.WriteProperty(ctx, peer.target, bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 999},
		bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}, bacnet.RealValue(1), nil)
	requireBACnetError(t, "write unknown object", err, bacnet.ErrorClassObject, bacnet.ErrorCodeUnknownObject)
	// Peer behaviour: BACpypes casts the value to the property's datatype
	// while it decodes the request and answers a mismatch with Reject
	// invalid-tag, where clause 15.9.1.3.1 has Error property/invalid-data-type.
	prio := uint8(8)
	err = c.WriteProperty(ctx, peer.target, av, bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}, bacnet.BoolValue(true), &prio)
	requireReject(t, "write BOOLEAN to a REAL present-value", err, bacnet.RejectReasonInvalidTag)
}

func TestBACpypesWritePropertyMultipleReadbackReset(t *testing.T) {
	requireAdapterVersion(t, bacpypesImage(), bacpypesMinAdapterVersion)
	runWritePropertyMultipleReadbackReset(t, bacpypesImage(), "bacpypes")
}

// Adapter shim: BACpypes 0.19.0 ships do_WritePropertyMultipleRequest
// commented out (bacpypes/service/object.py). The adapter's handler stops at
// the first failing write and answers WritePropertyMultiple-Error with
// firstFailedWriteAttempt (clause 15.10.1.3.2).
func TestBACpypesWritePropertyMultipleFirstFailed(t *testing.T) {
	requireAdapterVersion(t, bacpypesImage(), bacpypesMinAdapterVersion)
	runWritePropertyMultipleFirstFailed(t, bacpypesPeer(), []wpmFailure{
		{name: "unknown property", property: unknownPropertyID, value: bacnet.UnsignedValue(1),
			class: bacnet.ErrorClassProperty, code: bacnet.ErrorCodeUnknownProperty},
		{name: "read-only property", property: bacnet.PropertyObjectType, value: bacnet.EnumValue(uint32(bacnet.ObjectTypeBinaryValue)),
			class: bacnet.ErrorClassProperty, code: bacnet.ErrorCodeWriteAccessDenied},
		{name: "wrong datatype", property: bacnet.PropertyPresentValue, value: bacnet.BoolValue(true),
			class: bacnet.ErrorClassProperty, code: bacnet.ErrorCodeInvalidDataType},
		{name: "unknown object", unknownObject: true,
			class: bacnet.ErrorClassObject, code: bacnet.ErrorCodeUnknownObject},
	})
}

// ---- segmentation ---------------------------------------------------------

func bacpypesSegmentedRPM(dev deviceFixture) []service.ReadAccessSpecification {
	av, _ := analogValueObject(dev)
	return []service.ReadAccessSpecification{
		{Object: deviceObject(dev), Properties: []bacnet.PropertyReference{{Identifier: bacnet.PropertyAll}}},
		{Object: av, Properties: []bacnet.PropertyReference{{Identifier: bacnet.PropertyAll}}},
	}
}

// Segmented ComplexACKs from BACpypes (clause 5.4.4): the client advertises
// 50 octets; the device proposes window BACNET_SEGMENT_WINDOW (the SMAP
// proposedWindowSize, upstream default 2) and go-bacnet answers with its
// receive window.
func TestBACpypesSegmentedAnswers(t *testing.T) {
	for _, tc := range []struct {
		deviceWindow, clientWindow uint8
	}{
		{1, 1}, {1, 4}, {2, 1}, {2, 16}, {4, 2}, {4, 4}, {8, 16},
	} {
		t.Run(fmt.Sprintf("device-window-%d/client-window-%d", tc.deviceWindow, tc.clientWindow), func(t *testing.T) {
			dev := loadDeviceFixture(t)
			peer := startBACpypes(t, fmt.Sprintf("BACNET_SEGMENT_WINDOW=%d", tc.deviceWindow))
			if peer.assertedByReexec {
				return
			}
			c := newClientOpts(t, client.WithAdvertisedMaxAPDU(50), client.WithSegmentWindow(tc.clientWindow))
			ctx, cancel := withTimeout(t, 20*time.Second)
			defer cancel()
			results, err := c.ReadPropertyMultiple(ctx, peer.target, bacpypesSegmentedRPM(dev))
			if err != nil {
				t.Fatalf("segmented RPM: %v", err)
			}
			if got := characterString(t, findPropertyValue(t, results[0], bacnet.PropertyObjectName)); got != dev.DeviceName {
				t.Fatalf("object-name %q", got)
			}
			if got := characterString(t, findPropertyValue(t, results[1], bacnet.PropertyObjectName)); got != "AV-1" {
				t.Fatalf("AV object-name %q", got)
			}
		})
	}
}

// A device with a small max-APDU (BACNET_MAX_APDU=128) segments every
// larger answer, whatever the client accepts.
func TestBACpypesSegmentedAnswerSmallDeviceAPDU(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startBACpypes(t, "BACNET_MAX_APDU=128")
	if peer.assertedByReexec {
		return
	}
	c := newDiscoveryClient(t)
	whoIsFinds(t, c, peer, dev.DeviceInstance, false)
	for _, obs := range c.Devices() {
		if obs.Instance == dev.DeviceInstance && obs.Capabilities.MaxAPDULengthAccepted.Value != 128 {
			t.Fatalf("I-Am max-APDU %d, want 128", obs.Capabilities.MaxAPDULengthAccepted.Value)
		}
	}
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()
	results, err := c.ReadPropertyMultiple(ctx, peer.target, bacpypesSegmentedRPM(dev))
	if err != nil {
		t.Fatalf("RPM from a 128-octet device: %v", err)
	}
	if got := characterString(t, findPropertyValue(t, results[0], bacnet.PropertyObjectName)); got != dev.DeviceName {
		t.Fatalf("object-name %q", got)
	}
}

// Segmented confirmed requests to BACpypes: a WritePropertyMultiple of eight
// Present_Value writes to a target that accepts 50 octets. (Description is
// read-only in BACpypes, so the 80-character Description of
// runSegmentedWritePropertyMultipleSend is not used.)
func TestBACpypesSegmentedRequests(t *testing.T) {
	for _, tc := range []struct {
		deviceWindow, clientWindow uint8
	}{
		{2, 1}, {2, 2}, {2, 16}, {1, 4}, {4, 4},
	} {
		t.Run(fmt.Sprintf("device-window-%d/client-window-%d", tc.deviceWindow, tc.clientWindow), func(t *testing.T) {
			dev := loadDeviceFixture(t)
			peer := startBACpypes(t, fmt.Sprintf("BACNET_SEGMENT_WINDOW=%d", tc.deviceWindow))
			if peer.assertedByReexec {
				return
			}
			// The I-Am tells the client that the device accepts segmented
			// requests.
			c := newDiscoveryClientOpts(t, client.WithSegmentWindow(tc.clientWindow))
			whoIsFinds(t, c, peer, dev.DeviceInstance, false)
			ctx, cancel := withTimeout(t, 20*time.Second)
			defer cancel()
			av, _ := analogValueObject(dev)
			target := peer.target
			target.MaxAPDU = 50
			prio := uint8(10)
			var vals []service.WritePropertyValue
			for i := 0; i < 8; i++ {
				vals = append(vals, service.WritePropertyValue{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
					Value: bacnet.RealValue(float32(60 + i)), Priority: &prio})
			}
			if err := c.WritePropertyMultiple(ctx, target, []service.WriteAccessSpecification{{Object: av, Properties: vals}}); err != nil {
				t.Fatalf("segmented WPM: %v", err)
			}
			if pv := readReal(t, ctx, c, peer, av); pv != 67 {
				t.Fatalf("present-value after segmented WPM %v, want 67", pv)
			}
		})
	}
}

// ---- COV ------------------------------------------------------------------

func TestBACpypesCOV(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		t.Run(fmt.Sprintf("confirmed-%v", confirmed), func(t *testing.T) {
			dev := loadDeviceFixture(t)
			peer := startBACpypes(t)
			if peer.assertedByReexec {
				return
			}
			c := newClient(t)
			ctx, cancel := withTimeout(t, 30*time.Second)
			defer cancel()
			av, _ := analogValueObject(dev)
			sub, err := c.SubscribeCOV(ctx, peer.target, av, client.COVOptions{Lifetime: 120, IssueConfirmed: confirmed, BufferSize: 8})
			if err != nil {
				t.Fatalf("SubscribeCOV: %v", err)
			}
			defer func() { _ = sub.Close() }()
			// The initial notification after a subscription (clause 13.1.2).
			first := waitCOVNotification(t, sub, 8*time.Second)
			if first.MonitoredObject != av || first.InitiatingDevice != deviceObject(dev) {
				t.Fatalf("initial notification %+v", first)
			}
			prio := uint8(8)
			if err := c.WriteProperty(ctx, peer.target, av, bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}, bacnet.RealValue(63.5), &prio); err != nil {
				t.Fatalf("WriteProperty: %v", err)
			}
			deadline := time.Now().Add(8 * time.Second)
			for {
				note := waitCOVNotification(t, sub, time.Until(deadline))
				if v, ok := covValue(note, bacnet.PropertyPresentValue); ok && v.Kind == bacnet.ValueReal && v.Real == 63.5 {
					if _, ok := covValue(note, bacnet.PropertyStatusFlags); !ok {
						t.Fatalf("COV notification without status-flags: %+v", note)
					}
					break
				}
			}
			if err := sub.Close(); err != nil {
				t.Fatalf("cancel COV: %v", err)
			}
		})
	}
}

func TestBACpypesCOVUnknownObject(t *testing.T) {
	peer := startBACpypes(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 15*time.Second)
	defer cancel()
	_, err := c.SubscribeCOV(ctx, peer.target, bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 999}, client.COVOptions{Lifetime: 60})
	requireBACnetError(t, "SubscribeCOV unknown object", err, bacnet.ErrorClassObject, bacnet.ErrorCodeUnknownObject)
}

func TestBACpypesCOVProperty(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startBACpypes(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 30*time.Second)
	defer cancel()
	av, _ := analogValueObject(dev)
	inc := float32(1)
	sub, err := c.SubscribeCOVProperty(ctx, peer.target, av, bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
		client.COVOptions{Lifetime: 120, IssueConfirmed: true, BufferSize: 8}, &inc)
	if err != nil {
		t.Fatalf("SubscribeCOVProperty: %v", err)
	}
	defer func() { _ = sub.Close() }()
	prio := uint8(8)
	if err := c.WriteProperty(ctx, peer.target, av, bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}, bacnet.RealValue(30), &prio); err != nil {
		t.Fatalf("WriteProperty: %v", err)
	}
	deadline := time.Now().Add(8 * time.Second)
	for {
		note := waitCOVNotification(t, sub, time.Until(deadline))
		if v, ok := covValue(note, bacnet.PropertyPresentValue); ok && v.Kind == bacnet.ValueReal && v.Real == 30 {
			break
		}
	}
	if err := sub.Close(); err != nil {
		t.Fatalf("cancel: %v", err)
	}
}

// Peer behaviour: BACpypes 0.19.0 ignores monitoredPropertyIdentifier
// (bacpypes/service/cov.py do_SubscribeCOVPropertyRequest reads prop_id and
// never checks it), so a subscription to a property the object does not have
// is accepted and reports Present_Value and Status_Flags. Clause 13.15.1.3.1
// has an Error for that.
func TestBACpypesCOVPropertyUnknownPropertyAccepted(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startBACpypes(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()
	av, _ := analogValueObject(dev)
	sub, err := c.SubscribeCOVProperty(ctx, peer.target, av, bacnet.PropertyReference{Identifier: unknownPropertyID},
		client.COVOptions{Lifetime: 60, BufferSize: 8}, nil)
	if err != nil {
		t.Fatalf("SubscribeCOVProperty unknown property: %v; BACpypes 0.19.0 accepts it", err)
	}
	defer func() { _ = sub.Close() }()
	note := waitCOVNotification(t, sub, 8*time.Second)
	if _, ok := covValue(note, bacnet.PropertyPresentValue); !ok {
		t.Fatalf("notification %+v", note)
	}
}

// newDiscoveryClientOpts is newDiscoveryClient with extra options.
func newDiscoveryClientOpts(t *testing.T, opts ...client.Option) *client.Client {
	t.Helper()
	dev := loadDeviceFixture(t)
	port := dev.Port
	if port == 0 {
		port = bip.DefaultPort
	}
	c, err := client.New(append([]client.Option{
		client.WithLocalAddr(fmt.Sprintf("0.0.0.0:%d", port)),
		client.WithPort(port),
		client.WithTransactionOptions(5*time.Second, 1, 2*time.Second),
	}, opts...)...)
	if err != nil {
		t.Fatalf("client.New discovery: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func covValue(note *service.COVNotification, id bacnet.PropertyIdentifier) (bacnet.ApplicationValue, bool) {
	for _, v := range note.Values {
		if v.Property.Identifier == id {
			return v.Value, true
		}
	}
	return bacnet.ApplicationValue{}, false
}

// ---- files ----------------------------------------------------------------

// AtomicReadFile and AtomicWriteFile are upstream's FileServices; the file
// contents live in the adapter's subclasses of the abstract
// LocalStreamAccessFileObject and LocalRecordAccessFileObject.
func TestBACpypesFiles(t *testing.T) {
	dev, peer := startBACpypesServerFixture(t)
	if peer.assertedByReexec {
		return
	}
	// The I-Am tells the client that the device accepts segmented requests.
	// The client advertises 1024 octets; see TestBACpypesOversizedSegments
	// for 1476.
	c := newDiscoveryClientOpts(t, client.WithAdvertisedMaxAPDU(1024))
	whoIsFinds(t, c, peer, dev.DeviceInstance, false)
	ctx, cancel := withTimeout(t, 30*time.Second)
	defer cancel()
	stream := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 1}
	record := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 2}
	big := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 3}

	ack, err := c.AtomicReadFile(ctx, peer.target, service.AtomicReadFileRequest{File: stream, Access: service.FileAccessStream, Count: 64})
	if err != nil || string(ack.Data) != "hello-world-stream-file-data" || !ack.EndOfFile {
		t.Fatalf("ARF stream: %q eof=%v %v", ack.Data, ack.EndOfFile, err)
	}
	ack, err = c.AtomicReadFile(ctx, peer.target, service.AtomicReadFileRequest{File: stream, Access: service.FileAccessStream, StartPosition: 6, Count: 5})
	if err != nil || string(ack.Data) != "world" || ack.EndOfFile {
		t.Fatalf("ARF stream middle: %q eof=%v %v", ack.Data, ack.EndOfFile, err)
	}
	ack, err = c.AtomicReadFile(ctx, peer.target, service.AtomicReadFileRequest{File: record, Access: service.FileAccessRecord, StartPosition: 1, Count: 2})
	if err != nil || len(ack.Records) != 2 || string(ack.Records[0]) != "record-0002" || !ack.EndOfFile {
		t.Fatalf("ARF record: %q eof=%v %v", ack.Records, ack.EndOfFile, err)
	}

	// 3000 octets do not fit one APDU: a segmented ComplexACK.
	want := fileThreeData()
	ack, err = c.AtomicReadFile(ctx, peer.target, service.AtomicReadFileRequest{File: big, Access: service.FileAccessStream, Count: 3000})
	if err != nil || !bytes.Equal(ack.Data, want) {
		t.Fatalf("segmented ARF: %d octets, %v", len(ack.Data), err)
	}
	// A segmented request: write the 3000 octets back reversed.
	rev := make([]byte, len(want))
	for i := range want {
		rev[len(want)-1-i] = want[i]
	}
	wack, err := c.AtomicWriteFile(ctx, peer.target, service.AtomicWriteFileRequest{File: big, Access: service.FileAccessStream, Data: rev})
	if err != nil || wack.StartPosition != 0 {
		t.Fatalf("segmented AWF: %+v %v", wack, err)
	}
	ack, err = c.AtomicReadFile(ctx, peer.target, service.AtomicReadFileRequest{File: big, Access: service.FileAccessStream, Count: 3000})
	if err != nil || !bytes.Equal(ack.Data, rev) {
		t.Fatalf("read back after segmented AWF: %v", err)
	}

	// Append (start -1) to the record file (clause 15.2.1.1.3).
	wack, err = c.AtomicWriteFile(ctx, peer.target, service.AtomicWriteFileRequest{File: record, Access: service.FileAccessRecord, StartPosition: -1,
		Records: [][]byte{[]byte("record-0004")}})
	if err != nil || wack.StartPosition != 3 {
		t.Fatalf("AWF record append: %+v %v", wack, err)
	}
	ack, err = c.AtomicReadFile(ctx, peer.target, service.AtomicReadFileRequest{File: record, Access: service.FileAccessRecord, StartPosition: 3, Count: 1})
	if err != nil || len(ack.Records) != 1 || string(ack.Records[0]) != "record-0004" {
		t.Fatalf("ARF appended record: %q %v", ack.Records, err)
	}

	_, err = c.AtomicReadFile(ctx, peer.target, service.AtomicReadFileRequest{File: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 99}, Access: service.FileAccessStream, Count: 1})
	requireBACnetError(t, "ARF unknown file", err, bacnet.ErrorClassObject, bacnet.ErrorCodeUnknownObject)
	_, err = c.AtomicReadFile(ctx, peer.target, service.AtomicReadFileRequest{File: stream, Access: service.FileAccessRecord, Count: 1})
	requireBACnetError(t, "ARF record access to a stream file", err, bacnet.ErrorClassServices, bacnet.ErrorCodeInvalidFileAccessMethod)
}

// Peer behaviour: BACpypes fills each segment of a ComplexACK with
// Max-APDU-accepted octets of service data and puts the 5-octet segment
// header in front, so a client that accepts 1476 octets gets 1481-octet
// APDUs (recorded with a raw-socket probe of the bacnet-interop image:
// segments of 1481, 1481 and 62 octets for the 3000-octet file; with 1024
// advertised, 1029, 1029 and 966). Clause 20.1.2.5 bounds an APDU by the
// receiver's Max_APDU_Length_Accepted. go-bacnet's client advertises 1476
// and, by default, receives APDUs up to the datagram bound, so the answer is
// read (the server does the same for BACpypes3's requests). With
// DecodeLimits.MaxAPDUSize set to 1476 the client discards the longer
// segments and the read times out: the peer behaviour stays visible.
func TestBACpypesOversizedSegments(t *testing.T) {
	_, peer := startBACpypesServerFixture(t)
	if peer.assertedByReexec {
		return
	}
	req := service.AtomicReadFileRequest{File: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 3}, Access: service.FileAccessStream, Count: 3000}
	t.Run("default-decode-limits", func(t *testing.T) {
		c := newClientOpts(t)
		ctx, cancel := withTimeout(t, 30*time.Second)
		defer cancel()
		ack, err := c.AtomicReadFile(ctx, peer.target, req)
		if err != nil || !bytes.Equal(ack.Data, fileThreeData()) {
			t.Fatalf("ARF with 1481-octet segments, default limits: %d octets, %v", len(ack.Data), err)
		}
	})
	t.Run("strict-decode-limit-1476", func(t *testing.T) {
		limits := bacnet.DefaultDecodeLimits()
		limits.MaxAPDUSize = 1476
		c := newClientOpts(t, client.WithDecodeLimits(limits))
		ctx, cancel := withTimeout(t, 30*time.Second)
		defer cancel()
		_, err := c.AtomicReadFile(ctx, peer.target, req)
		if !errors.Is(err, bacnet.ErrTimeout) {
			t.Fatalf("ARF with 1481-octet segments, MaxAPDUSize 1476: %v, want ErrTimeout (segments longer than the decode limit are dropped)", err)
		}
	})
}

// fileThreeData is FILE-STREAM-3 of device-server-v1: octet i = (7 i + 3) mod 256.
func fileThreeData() []byte {
	out := make([]byte, 3000)
	for i := range out {
		out[i] = byte((7*i + 3) % 256)
	}
	return out
}

func TestBACpypesFileFixtureV4(t *testing.T) {
	requireAdapterVersion(t, bacpypesImage(), bacpypesMinAdapterVersion)
	t.Run("stream", func(t *testing.T) { runAtomicReadFileStream(t, bacpypesImage(), "bacpypes") })
	t.Run("record", func(t *testing.T) { runAtomicReadFileRecord(t, bacpypesImage(), "bacpypes") })
	t.Run("write-readback", func(t *testing.T) { runAtomicWriteFileStreamReadback(t, bacpypesImage(), "bacpypes") })
}

// ---- ReadRange (adapter shim) -------------------------------------------

// Adapter shim: BACpypes 0.19.0 has the ReadRange APDU and no server
// handler. The adapter's do_ReadRangeRequest reads the Trend Log's
// Log_Buffer by position and by sequence number (clause 15.8).
func TestBACpypesReadRange(t *testing.T) {
	dev, peer := startBACpypesServerFixture(t)
	if peer.assertedByReexec {
		return
	}
	tl, _ := trendLogObject(dev)
	c := newClient(t)
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()
	ref := bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer}
	byPos, err := c.ReadRange(ctx, peer.target, service.ReadRangeRequest{Object: tl, Property: ref, By: service.ReadRangeByPosition, ReferenceIndex: 2, Count: 3})
	if err != nil || byPos.ItemCount != 3 || len(byPos.LogRecords) != 3 || byPos.LogRecords[0].Datum.Real != 1 || byPos.FirstItem() || byPos.LastItem() {
		t.Fatalf("by position: %+v %v", byPos, err)
	}
	back, err := c.ReadRange(ctx, peer.target, service.ReadRangeRequest{Object: tl, Property: ref, By: service.ReadRangeByPosition, ReferenceIndex: 10, Count: -2})
	if err != nil || back.ItemCount != 2 || back.LogRecords[1].Datum.Real != 9 || !back.LastItem() {
		t.Fatalf("by position backwards: %+v %v", back, err)
	}
	bySeq, err := c.ReadRange(ctx, peer.target, service.ReadRangeRequest{Object: tl, Property: ref, By: service.ReadRangeBySequenceNumber, ReferenceIndex: 1, Count: 2})
	if err != nil || bySeq.ItemCount != 2 || bySeq.FirstSequence == nil || *bySeq.FirstSequence != 1 || !bySeq.FirstItem() {
		t.Fatalf("by sequence: %+v %v", bySeq, err)
	}
	all, err := c.ReadRangeAll(ctx, peer.target, service.ReadRangeRequest{Object: tl, Property: ref, By: service.ReadRangeByPosition, ReferenceIndex: 1, Count: 4}, client.ReadRangePageOptions{})
	if err != nil || len(all.LogRecords) != 10 {
		t.Fatalf("ReadRangeAll: %d records, %v", len(all.LogRecords), err)
	}
	av, _ := analogValueObject(dev)
	_, err = c.ReadRange(ctx, peer.target, service.ReadRangeRequest{Object: av, Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}, By: service.ReadRangeByPosition, ReferenceIndex: 1, Count: 1})
	requireBACnetError(t, "ReadRange of a non-list", err, bacnet.ErrorClassServices, bacnet.ErrorCodePropertyIsNotAList)
}

// ---- device management ----------------------------------------------------

func startBACpypesDCC(t *testing.T, env ...string) *dccSession {
	t.Helper()
	requireAdapterVersion(t, bacpypesImage(), bacpypesMinAdapterVersion)
	return startDCCSession(t, bacpypesPeer(), env...)
}

// Under disable-initiation BACpypes keeps answering requests, including
// Who-Is (clause 16.1.1.3.1).
func TestBACpypesDeviceCommunicationControlDisableInitiation(t *testing.T) {
	s := startBACpypesDCC(t)
	if s == nil {
		return
	}
	s.mustControl("disable-initiation for 1 minute", service.EnableDisableDisableInitiation, 1, "")
	s.mustRead("under disable-initiation")
	s.mustAnswerWhoIs("under disable-initiation")
	s.mustControl("enable", service.EnableDisableEnable, 0, "")
	s.mustRead("after enable")
	s.mustAnswerWhoIs("after enable")
}

// BACpypes acknowledges "disable" (which clause 16.1.1.3.1 no longer allows
// from protocol revision 20) and then drops everything except
// DeviceCommunicationControl, ReinitializeDevice and Who-Is: reads time out
// until "enable" arrives.
func TestBACpypesDeviceCommunicationControlDisable(t *testing.T) {
	s := startBACpypesDCC(t)
	if s == nil {
		return
	}
	s.mustControl("disable for 1 minute", service.EnableDisableDisable, 1, "")
	s.mustStaySilent("under disable")
	s.mustControl("enable", service.EnableDisableEnable, 0, "")
	s.mustRead("after enable")
}

func TestBACpypesDeviceCommunicationControlPassword(t *testing.T) {
	s := startBACpypesDCC(t, dccPasswordEnv)
	if s == nil {
		return
	}
	requirePasswordFailure(t, "DCC with a wrong password", s.control(service.EnableDisableDisableInitiation, 1, "wrong"))
	requirePasswordFailure(t, "DCC without a password", s.control(service.EnableDisableDisableInitiation, 1, ""))
	s.mustRead("after rejected DCC")
	s.mustControl("disable-initiation with the password", service.EnableDisableDisableInitiation, 1, dccPassword)
	s.mustControl("enable with the password", service.EnableDisableEnable, 0, dccPassword)
	s.mustRead("after enable")
}

// Adapter shim: BACpypes 0.19.0 has the ReinitializeDevice APDU and no
// server handler.
func TestBACpypesReinitializeDevice(t *testing.T) {
	peer := startBACpypes(t, dccPasswordEnv)
	if peer.assertedByReexec {
		return
	}
	c := newClientOpts(t, client.WithDeviceManagementEnabled(client.DeviceManagementConfirm))
	ctx, cancel := withTimeout(t, 15*time.Second)
	defer cancel()
	pw := bacnet.CharacterString{Value: dccPassword}
	if err := c.ReinitializeDevice(ctx, peer.target, service.ReinitializeDeviceRequest{State: service.ReinitializedWarmstart, Password: &pw}); err != nil {
		t.Fatalf("ReinitializeDevice warmstart: %v", err)
	}
	awaitOperation(t, peer, "reinitialize-device", 3*time.Second)
	wrong := bacnet.CharacterString{Value: "wrong"}
	err := c.ReinitializeDevice(ctx, peer.target, service.ReinitializeDeviceRequest{State: service.ReinitializedWarmstart, Password: &wrong})
	requirePasswordFailure(t, "ReinitializeDevice wrong password", err)
}

// ---- Who-Has --------------------------------------------------------------

func TestBACpypesWhoHas(t *testing.T) {
	requireAdapterVersion(t, bacpypesImage(), bacpypesMinAdapterVersion)
	t.Run("by-name", func(t *testing.T) { runWhoHasIHave(t, bacpypesImage(), "bacpypes") })
	t.Run("by-identifier", func(t *testing.T) {
		dev := loadDeviceFixture(t)
		peer := startBACpypes(t)
		if peer.assertedByReexec {
			return
		}
		c := newDiscoveryClient(t)
		ctx, cancel := withTimeout(t, 10*time.Second)
		defer cancel()
		bv := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeBinaryValue, Instance: 1}
		if err := c.SendWhoHas(ctx, peer.endpoint, false, client.WhoHasOptions{Object: &bv}); err != nil {
			t.Fatalf("SendWhoHas: %v", err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			for _, o := range c.Objects() {
				if o.DeviceInstance == dev.DeviceInstance && o.Object == bv && o.Name.Value == "BV-1" {
					return
				}
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("no I-Have for %v: %v", bv, c.Objects())
	})
	t.Run("absent-object", func(t *testing.T) {
		peer := startBACpypes(t)
		if peer.assertedByReexec {
			return
		}
		c := newDiscoveryClient(t)
		ctx, cancel := withTimeout(t, 10*time.Second)
		defer cancel()
		name := bacnet.CharacterString{Value: "no-such-object"}
		if err := c.SendWhoHas(ctx, peer.endpoint, false, client.WhoHasOptions{Name: &name}); err != nil {
			t.Fatalf("SendWhoHas: %v", err)
		}
		time.Sleep(1500 * time.Millisecond)
		if got := c.Objects(); len(got) != 0 {
			t.Fatalf("I-Have for an absent object: %v", got)
		}
	})
}

// ---- services BACpypes does not serve -----------------------------------

// BACpypes answers a confirmed service it has no handler for with Reject
// unrecognized-service (bacpypes/app.py Application.indication).
func TestBACpypesUnsupportedServicesReject(t *testing.T) {
	_, peer := startBACpypesServerFixture(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()
	avType := bacnet.ObjectTypeAnalogValue
	_, err := c.CreateObject(ctx, peer.target, service.CreateObjectRequest{ObjectType: &avType})
	assertRejectUnrecognized(t, err)
	err = c.DeleteObject(ctx, peer.target, service.DeleteObjectRequest{Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 2}})
	assertRejectUnrecognized(t, err)
	_, err = c.GetEventInformation(ctx, peer.target, service.GetEventInformationRequest{})
	assertRejectUnrecognized(t, err)
	_, err = c.GetAlarmSummary(ctx, peer.target)
	assertRejectUnrecognized(t, err)
	_, err = c.GetEnrollmentSummary(ctx, peer.target, service.GetEnrollmentSummaryRequest{})
	assertRejectUnrecognized(t, err)
}

// ---- messaging (adapter shim) -------------------------------------------

// BACpypes 0.19.0 decodes TimeSynchronization, UTCTimeSynchronization, the
// text messages, private transfer and WriteGroup but serves none of them;
// the adapter's sinks report what arrived.
func TestBACpypesMessagingSemanticReceipt(t *testing.T) {
	requireAdapterVersion(t, bacpypesImage(), bacpypesMinAdapterVersion)
	runMessagingSemanticReceipt(t, messagingPeer{
		name:          "bacpypes",
		image:         bacpypesImage(),
		ops:           []string{"time-synchronization", "utc-time-synchronization", "unconfirmed-text-message", "confirmed-text-message", "unconfirmed-private-transfer", "confirmed-private-transfer", "write-group"},
		confirmedPT:   true,
		confirmedText: true,
	})
}

// ---- BBMD role --------------------------------------------------------------

func TestBACpypesReadBDT(t *testing.T) {
	requireAdapterVersion(t, bacpypesImage(), bacpypesMinAdapterVersion)
	runReadBDT(t, bacpypesImage(), "bacpypes", bacpypesBBMD)
}

func TestBACpypesReadFDTAfterFD(t *testing.T) {
	requireAdapterVersion(t, bacpypesImage(), bacpypesMinAdapterVersion)
	runReadFDTAfterFD(t, bacpypesImage(), "bacpypes", bacpypesBBMD)
}

func TestBACpypesDeleteFDTEntry(t *testing.T) {
	requireAdapterVersion(t, bacpypesImage(), bacpypesMinAdapterVersion)
	runDeleteFDTEntry(t, bacpypesImage(), "bacpypes", bacpypesBBMD)
}

// The BBMD's own BDT entry, with an all-ones mask, and the FDT entry of a
// registered foreign device.
//
// Peer behaviour: BIPBBMD.register_foreign_device sets the remaining time to
// TTL + 5 (bacpypes/bvllservice.py), where clause J.5.2.3 adds a 30-second
// grace period; Read-FDT therefore reports at most TTL + 5. Every
// Write-Broadcast-Distribution-Table is NAKed with 0x0010.
func TestBACpypesBBMDTables(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startBACpypes(t, bacpypesBBMD)
	if peer.assertedByReexec {
		return
	}
	c := newClientOpts(t,
		client.WithForeignDevice(client.ForeignDeviceConfig{BBMD: peer.endpoint, TTL: 60 * time.Second}),
		client.WithNetworkManagementEnabled(client.NetworkManagementConfirm))
	deadline := time.Now().Add(8 * time.Second)
	for !c.ForeignDeviceRegistered() {
		if time.Now().After(deadline) {
			t.Fatal("foreign-device registration did not succeed")
		}
		time.Sleep(50 * time.Millisecond)
	}
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()
	bdt, err := c.ReadBroadcastDistributionTable(ctx, peer.endpoint)
	if err != nil || len(bdt) != 1 || bdt[0].Endpoint != peer.endpoint || bdt[0].Mask != (bvlc.IPv4Mask{255, 255, 255, 255}) {
		t.Fatalf("Read-BDT: %+v %v, want the BBMD itself with mask 255.255.255.255", bdt, err)
	}
	fdt, err := c.ReadForeignDeviceTable(ctx, peer.endpoint)
	if err != nil || len(fdt) != 1 {
		t.Fatalf("Read-FDT: %+v %v", fdt, err)
	}
	if fdt[0].TTL != 60 || fdt[0].Remaining > 65 || fdt[0].Remaining < 55 {
		t.Fatalf("FDT entry TTL %d remaining %d, want TTL 60 and remaining at most 65 (BACpypes: TTL + 5)", fdt[0].TTL, fdt[0].Remaining)
	}
	err = c.WriteBroadcastDistributionTable(ctx, peer.endpoint, bdt)
	var op *client.BVLCOperationError
	if !errors.As(err, &op) || op.Code != 0x0010 {
		t.Fatalf("Write-BDT: %v, want NAK 0x0010", err)
	}
	// Distribute-Broadcast-To-Network reaches the BBMD's own device, and a
	// read over the registration works.
	whoIsFinds(t, c, peer, dev.DeviceInstance, true)
	av, want := analogValueObject(dev)
	if pv := readReal(t, ctx, c, peer, av); math.Abs(float64(pv-want)) > 0.01 {
		t.Fatalf("present-value via FD %v", pv)
	}
	// Delete-FDT of an entry that does not exist: NAK 0x0050.
	err = c.DeleteForeignDeviceTableEntry(ctx, peer.endpoint, bip.NewEndpoint(netip.AddrPortFrom(peer.endpoint.Addr.Addr(), 1)))
	if !errors.As(err, &op) || op.Code != 0x0050 {
		t.Fatalf("Delete-FDT of an absent entry: %v, want NAK 0x0050", err)
	}
}

// Segmented answers from the BBMD role at go-bacnet receive windows 1 and 4.
func TestBACpypesBBMDSegmentedAnswer(t *testing.T) {
	for _, w := range []uint8{1, 4} {
		t.Run(fmt.Sprintf("window-%d", w), func(t *testing.T) {
			dev := loadDeviceFixture(t)
			peer := startBACpypes(t, bacpypesBBMD)
			if peer.assertedByReexec {
				return
			}
			c := newClientOpts(t, client.WithAdvertisedMaxAPDU(50), client.WithSegmentWindow(w))
			ctx, cancel := withTimeout(t, 20*time.Second)
			defer cancel()
			results, err := c.ReadPropertyMultiple(ctx, peer.target, bacpypesSegmentedRPM(dev))
			if err != nil {
				t.Fatalf("segmented RPM from the BBMD role: %v", err)
			}
			if got := characterString(t, findPropertyValue(t, results[0], bacnet.PropertyObjectName)); got != dev.DeviceName {
				t.Fatalf("object-name %q", got)
			}
		})
	}
}
