//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"bytes"
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/service"
)

// bacnet-js peer, bacnet-interop adapter bacnet-js: @bacnet-js/client
// (TypeScript on Node.js, MIT), independent of go-bacnet. Upstream decodes
// every request and encodes every answer; the object model, the dispatch and
// the service procedures are adapter code (adapter-shim in bacnet-interop
// adapters/inventory.yaml). Upstream behaviour these tests record is cited
// with its source file in @bacnet-js/client 3.3.2 (bacnet-interop
// adapters/bacnet-js/README.md has the list).

const (
	defaultBacnetJSImage      = "bacnet-interop-bacnet-js:local"
	bacnetJSMinAdapterVersion = "0.14.0"
	bacnetJSAdapter           = "bacnet-js"
	deviceServerFixture       = "device-server-v1"
)

func bacnetJSImage() string { return getEnv("BACNETJS_IMAGE", defaultBacnetJSImage) }

func bacnetJSPeer() interopPeer { return interopPeer{bacnetJSAdapter, bacnetJSImage()} }

// withBacnetJS adds the bacnet-js peer to a cross-peer table unless its
// image is "none" (a bacnet-interop pin without it).
func withBacnetJS(peers ...interopPeer) []interopPeer {
	if bacnetJSImage() == "none" {
		return peers
	}
	return append(append([]interopPeer(nil), peers...), bacnetJSPeer())
}

func startBacnetJS(t *testing.T, env ...string) *peerHandle {
	t.Helper()
	requireAdapterVersion(t, bacnetJSImage(), bacnetJSMinAdapterVersion)
	return startPeer(t, bacnetJSImage(), bacnetJSAdapter, env...)
}

// startBacnetJSServer runs the image on device-server-v1: alarming AV-2,
// Notification Class, Trend Log, files and creatable Analog Values.
func startBacnetJSServer(t *testing.T) *peerHandle {
	t.Helper()
	requireAdapterVersion(t, bacnetJSImage(), bacnetJSMinAdapterVersion)
	t.Setenv("BACNET_DEVICE_FIXTURE", deviceFixturePath(t, deviceServerFixture))
	return startPeer(t, bacnetJSImage(), bacnetJSAdapter,
		"FIXTURE="+deviceServerFixture,
		"DEVICE_FIXTURE_FILE=/fixtures/device/"+deviceServerFixture+".json")
}

func objectID(typ bacnet.ObjectType, inst uint32) bacnet.ObjectIdentifier {
	return bacnet.ObjectIdentifier{Type: typ, Instance: inst}
}

func prop(id bacnet.PropertyIdentifier) bacnet.PropertyReference {
	return bacnet.PropertyReference{Identifier: id}
}

func requireAbort(t *testing.T, what string, err error, reason uint8) {
	t.Helper()
	var ab *bacnet.AbortError
	if !errors.As(err, &ab) || ab.Reason != reason || !ab.Server {
		t.Fatalf("%s: got %v, want a server Abort reason %d", what, err, reason)
	}
}

// ---- device-baseline-v2 -----------------------------------------------------

func TestBacnetJSDiscovery(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startBacnetJS(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	whoIsFinds(t, c, peer, dev.DeviceInstance, false)
	for _, obs := range c.Devices() {
		if obs.Instance == dev.DeviceInstance {
			// iAmResponse encodes transport.getMaxPayload() (1482) as
			// Max_APDU_Length_Accepted (client.ts iAmResponse,
			// transport.ts getMaxPayload); the Device object says 1476.
			t.Logf("I-Am capabilities: %+v", obs.Capabilities)
		}
	}

	ctx, cancel := withTimeout(t, 15*time.Second)
	defer cancel()
	// Outside the range: no I-Am.
	low, high := dev.DeviceInstance+1, dev.DeviceInstance+10
	before := len(c.Devices())
	if err := c.SendWhoIs(ctx, peer.endpoint, false, client.DiscoveryOptions{LowLimit: &low, HighLimit: &high}); err != nil {
		t.Fatalf("SendWhoIs: %v", err)
	}
	time.Sleep(time.Second)
	if got := len(c.Devices()); got != before {
		t.Fatalf("Who-Is %d-%d: %d devices, want %d", low, high, got, before)
	}

	av, _ := analogValueObject(dev)
	name := bacnet.CharacterString{Value: "AV-1"}
	objs, err := c.DiscoverObjects(ctx, client.WhoHasOptions{Name: &name, Window: 2 * time.Second})
	if err != nil {
		t.Fatalf("Who-Has by name: %v", err)
	}
	if len(objs) != 1 || objs[0].Object != av || objs[0].DeviceInstance != dev.DeviceInstance || objs[0].Name.Value != "AV-1" {
		t.Fatalf("Who-Has AV-1: %+v", objs)
	}
	objs, err = c.DiscoverObjects(ctx, client.WhoHasOptions{Object: &av, Window: 2 * time.Second})
	if err != nil || len(objs) != 1 || objs[0].Name.Value != "AV-1" {
		t.Fatalf("Who-Has by object: %+v %v", objs, err)
	}
	none := bacnet.CharacterString{Value: "NoSuchName"}
	objs, err = c.DiscoverObjects(ctx, client.WhoHasOptions{Name: &none, Window: 2 * time.Second})
	if err != nil || len(objs) != 0 {
		t.Fatalf("Who-Has NoSuchName: %+v %v", objs, err)
	}
}

func TestBacnetJSReadProperty(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startBacnetJS(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()
	devObj := deviceObject(dev)
	av, wantPV := analogValueObject(dev)

	name, err := c.ReadProperty(ctx, peer.target, devObj, prop(bacnet.PropertyObjectName))
	if err != nil || characterString(t, name) != dev.DeviceName {
		t.Fatalf("device object-name: %v %v", name, err)
	}
	// The wildcard Device instance (clause 12.11.1) addresses this device.
	if _, err := c.ReadProperty(ctx, peer.target, objectID(bacnet.ObjectTypeDevice, 4194303), prop(bacnet.PropertyObjectName)); err != nil {
		t.Fatalf("wildcard device instance: %v", err)
	}
	list, err := c.ReadObjectList(ctx, peer.target, devObj, 0)
	if err != nil {
		t.Fatalf("ReadObjectList: %v", err)
	}
	if len(list) != len(dev.Objects) || !containsObject(list, av) || !containsObject(list, devObj) {
		t.Fatalf("object-list %v, want the %d fixture objects", list, len(dev.Objects))
	}
	n, err := c.ReadProperty(ctx, peer.target, devObj, bacnet.PropertyReference{Identifier: bacnet.PropertyObjectList, ArrayIndex: ptrTo(uint32(0))})
	if err != nil || n.Kind != bacnet.ValueUnsigned || n.Unsigned != uint64(len(dev.Objects)) {
		t.Fatalf("object-list[0]: %+v %v", n, err)
	}
	_, err = c.ReadProperty(ctx, peer.target, devObj, bacnet.PropertyReference{Identifier: bacnet.PropertyObjectList, ArrayIndex: ptrTo(uint32(99))})
	requireBACnetError(t, "object-list[99]", err, bacnet.ErrorClassProperty, bacnet.ErrorCodeInvalidArrayIndex)

	props, err := c.ReadPropertyList(ctx, peer.target, av, 0)
	if err != nil || len(props) == 0 {
		t.Fatalf("ReadPropertyList: %v %v", props, err)
	}
	if pv := readReal(t, ctx, c, peer, av); math.Abs(float64(pv-wantPV)) > 0.01 {
		t.Fatalf("AV present-value %v, want %v", pv, wantPV)
	}
	seg, err := c.ReadProperty(ctx, peer.target, devObj, prop(bacnet.PropertySegmentation))
	if err != nil || seg.Kind != bacnet.ValueEnumerated || seg.Enumerated != 2 {
		t.Fatalf("segmentation-supported: %+v %v (want segmented-receive)", seg, err)
	}

	_, err = c.ReadProperty(ctx, peer.target, objectID(bacnet.ObjectTypeAnalogValue, 999), prop(bacnet.PropertyPresentValue))
	requireBACnetError(t, "unknown object", err, bacnet.ErrorClassObject, bacnet.ErrorCodeUnknownObject)
	_, err = c.ReadProperty(ctx, peer.target, av, prop(unknownPropertyID))
	requireBACnetError(t, "unknown property", err, bacnet.ErrorClassProperty, bacnet.ErrorCodeUnknownProperty)
	_, err = c.ReadProperty(ctx, peer.target, av, bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue, ArrayIndex: ptrTo(uint32(1))})
	requireBACnetError(t, "present-value[1]", err, bacnet.ErrorClassProperty, bacnet.ErrorCodePropertyIsNotAnArray)
	if tl, ok := trendLogObject(dev); ok {
		_, err = c.ReadProperty(ctx, peer.target, tl, prop(bacnet.PropertyLogBuffer))
		requireBACnetError(t, "ReadProperty log-buffer", err, bacnet.ErrorClassProperty, bacnet.ErrorCodeReadAccessDenied)
	}
}

func TestBacnetJSReadPropertyMultiple(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startBacnetJS(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()
	av, _ := analogValueObject(dev)
	missing := objectID(bacnet.ObjectTypeAnalogValue, 999)
	res, err := c.ReadPropertyMultiple(ctx, peer.target, []service.ReadAccessSpecification{
		{Object: av, Properties: []bacnet.PropertyReference{prop(bacnet.PropertyObjectName), prop(bacnet.PropertyPresentValue), prop(unknownPropertyID)}},
		{Object: missing, Properties: []bacnet.PropertyReference{prop(bacnet.PropertyObjectName)}},
	})
	if err != nil {
		t.Fatalf("RPM: %v", err)
	}
	if len(res) != 2 || len(res[0].Properties) != 3 || len(res[1].Properties) != 1 {
		t.Fatalf("RPM shape: %+v", res)
	}
	if r := res[0].Properties[0]; r.Err != nil || characterString(t, r.Value) != "AV-1" {
		t.Fatalf("RPM object-name: %+v", r)
	}
	requireBACnetError(t, "RPM unknown property", res[0].Properties[2].Err, bacnet.ErrorClassProperty, bacnet.ErrorCodeUnknownProperty)
	requireBACnetError(t, "RPM unknown object", res[1].Properties[0].Err, bacnet.ErrorClassObject, bacnet.ErrorCodeUnknownObject)

	all, err := c.ReadPropertyMultiple(ctx, peer.target, []service.ReadAccessSpecification{
		{Object: av, Properties: []bacnet.PropertyReference{prop(bacnet.PropertyAll)}},
	})
	if err != nil || len(all) != 1 || len(all[0].Properties) < 8 {
		t.Fatalf("RPM ALL: %+v %v", all, err)
	}
	for _, r := range all[0].Properties {
		if r.Err != nil {
			t.Errorf("RPM ALL %v: %v", r.Property, r.Err)
		}
	}
}

func TestBacnetJSWriteAndCOV(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startBacnetJS(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 40*time.Second)
	defer cancel()
	av, baseline := analogValueObject(dev)

	for _, confirmed := range []bool{false, true} {
		sub, err := c.SubscribeCOV(ctx, peer.target, av, client.COVOptions{Lifetime: 60, IssueConfirmed: confirmed, BufferSize: 8})
		if err != nil {
			t.Fatalf("SubscribeCOV confirmed=%v: %v", confirmed, err)
		}
		// The initial notification of a new subscription (clause 13.14.1.1.4).
		note := waitCOVNotification(t, sub, 5*time.Second)
		if note.MonitoredObject != av || note.InitiatingDevice != deviceObject(dev) || len(note.Values) != 2 {
			t.Fatalf("initial COV notification: %+v", note)
		}
		if note.Values[0].Property.Identifier != bacnet.PropertyPresentValue || note.Values[1].Property.Identifier != bacnet.PropertyStatusFlags {
			t.Fatalf("COV values: %+v", note.Values)
		}
		want := float32(42)
		if confirmed {
			want = 43
		}
		if err := c.WritePriority(ctx, peer.target, av, 8, bacnet.RealValue(want)); err != nil {
			t.Fatalf("WritePriority 8: %v", err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for {
			n := waitCOVNotification(t, sub, time.Until(deadline))
			if n.Values[0].Value.Kind == bacnet.ValueReal && n.Values[0].Value.Real == want {
				break
			}
		}
		if confirmed {
			awaitOperation(t, peer, "confirmed-cov-notification", 5*time.Second)
		}
		if err := sub.Close(); err != nil {
			t.Fatalf("cancel subscription: %v", err)
		}
	}
	if err := c.RelinquishPriority(ctx, peer.target, av, 8); err != nil {
		t.Fatalf("RelinquishPriority 8: %v", err)
	}
	if pv := readReal(t, ctx, c, peer, av); math.Abs(float64(pv-baseline)) > 0.01 {
		t.Fatalf("present-value after relinquish %v, want %v", pv, baseline)
	}

	// SubscribeCOVProperty with a COV increment of 5: a change of 1 is not
	// reported, a change of 10 is (clause 13.15.1.1.6).
	inc := float32(5)
	sub, err := c.SubscribeCOVProperty(ctx, peer.target, av, prop(bacnet.PropertyPresentValue), client.COVOptions{Lifetime: 60, BufferSize: 8}, &inc)
	if err != nil {
		t.Fatalf("SubscribeCOVProperty: %v", err)
	}
	_ = waitCOVNotification(t, sub, 5*time.Second)
	if err := c.WritePriority(ctx, peer.target, av, 8, bacnet.RealValue(baseline+1)); err != nil {
		t.Fatal(err)
	}
	if err := c.WritePriority(ctx, peer.target, av, 8, bacnet.RealValue(baseline+10)); err != nil {
		t.Fatal(err)
	}
	n := waitCOVNotification(t, sub, 5*time.Second)
	if n.Values[0].Value.Real != baseline+10 {
		t.Fatalf("COV-property notification %v, want %v (the change of 1 is below the increment)", n.Values[0].Value, baseline+10)
	}
	_ = sub.Close()
	_ = c.RelinquishPriority(ctx, peer.target, av, 8)

	_, err = c.SubscribeCOV(ctx, peer.target, objectID(bacnet.ObjectTypeAnalogValue, 999), client.COVOptions{Lifetime: 60})
	requireBACnetError(t, "SubscribeCOV unknown object", err, bacnet.ErrorClassObject, bacnet.ErrorCodeUnknownObject)
	_, err = c.SubscribeCOVProperty(ctx, peer.target, av, prop(unknownPropertyID), client.COVOptions{Lifetime: 60}, nil)
	requireBACnetError(t, "SubscribeCOVProperty unknown property", err, bacnet.ErrorClassProperty, bacnet.ErrorCodeUnknownProperty)

	err = c.WriteProperty(ctx, peer.target, av, prop(bacnet.PropertyPresentValue),
		bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: "x"}}, nil)
	requireBACnetError(t, "write wrong type", err, bacnet.ErrorClassProperty, bacnet.ErrorCodeInvalidDataType)
	err = c.WriteProperty(ctx, peer.target, av, prop(bacnet.PropertyObjectName),
		bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: "x"}}, nil)
	requireBACnetError(t, "write object-name", err, bacnet.ErrorClassProperty, bacnet.ErrorCodeWriteAccessDenied)
	err = c.WritePriority(ctx, peer.target, objectID(bacnet.ObjectTypeAnalogValue, 999), 8, bacnet.RealValue(1))
	requireBACnetError(t, "write unknown object", err, bacnet.ErrorClassObject, bacnet.ErrorCodeUnknownObject)
}

func TestBacnetJSWritePropertyMultiple(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startBacnetJS(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()
	av, _ := analogValueObject(dev)
	prio := uint8(8)
	wpv := func(v float32) []service.WritePropertyValue {
		return []service.WritePropertyValue{{Property: prop(bacnet.PropertyPresentValue), Value: bacnet.RealValue(v), Priority: &prio}}
	}
	// Two specifications: upstream's WritePropertyMultiple.decode does not
	// stop at the first one's closing tag, so the adapter decodes each alone
	// (adapter-shim); without that the request would get no answer.
	if err := c.WritePropertyMultiple(ctx, peer.target, []service.WriteAccessSpecification{
		{Object: av, Properties: wpv(30)},
		{Object: av, Properties: []service.WritePropertyValue{{Property: prop(bacnet.PropertyDescription),
			Value: bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: "wpm"}}}}},
	}); err != nil {
		t.Fatalf("WPM two specifications: %v", err)
	}
	if pv := readReal(t, ctx, c, peer, av); pv != 30 {
		t.Fatalf("present-value after WPM %v", pv)
	}
	err := c.WritePropertyMultiple(ctx, peer.target, []service.WriteAccessSpecification{
		{Object: av, Properties: wpv(31)},
		{Object: objectID(bacnet.ObjectTypeAnalogValue, 999), Properties: wpv(1)},
	})
	var wpmErr *service.WritePropertyMultipleError
	if !errors.As(err, &wpmErr) || wpmErr.Class != bacnet.ErrorClassObject || wpmErr.Code != bacnet.ErrorCodeUnknownObject ||
		wpmErr.FirstFailed != objectID(bacnet.ObjectTypeAnalogValue, 999) || wpmErr.FirstProperty.Identifier != bacnet.PropertyPresentValue {
		t.Fatalf("WPM unknown object second: %v", err)
	}
	// Clause 15.10.1.3.1: the write before the failed one stays done.
	if pv := readReal(t, ctx, c, peer, av); pv != 31 {
		t.Fatalf("present-value after the failed WPM %v, want 31", pv)
	}
	_ = c.RelinquishPriority(ctx, peer.target, av, 8)
}

// A segmented confirmed request (go-bacnet segments for a 50-octet target):
// upstream reassembles it and acknowledges every window (client.ts
// _processSegment).
func TestBacnetJSSegmentedRequest(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startBacnetJS(t)
	if peer.assertedByReexec {
		return
	}
	// The client segments only for a peer whose I-Am says it can receive
	// segments (clause 5.2: segmented-receive here).
	c := newDiscoveryClient(t)
	whoIsFinds(t, c, peer, dev.DeviceInstance, false)
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()
	av, _ := analogValueObject(dev)
	target := peer.target
	target.MaxAPDU = 50
	text := strings.Repeat("x", 200)
	if err := c.WritePropertyMultiple(ctx, target, []service.WriteAccessSpecification{{Object: av, Properties: []service.WritePropertyValue{{
		Property: prop(bacnet.PropertyDescription),
		Value:    bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: text}},
	}}}}); err != nil {
		t.Fatalf("segmented WPM: %v", err)
	}
	v, err := c.ReadProperty(ctx, peer.target, av, prop(bacnet.PropertyDescription))
	if err != nil || characterString(t, v) != text {
		t.Fatalf("description after segmented WPM: %v", err)
	}
}

// The adapter answers a ComplexACK larger than the requester accepts with
// Abort segmentation-not-supported (clauses 5.2, 5.4.5): upstream sends no
// segmented message.
func TestBacnetJSSegmentedResponseRefused(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startBacnetJS(t)
	if peer.assertedByReexec {
		return
	}
	c := newClientWithAdvertisedMaxAPDU(t, 50)
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()
	av, _ := analogValueObject(dev)
	_, err := c.ReadPropertyMultiple(ctx, peer.target, []service.ReadAccessSpecification{
		{Object: av, Properties: []bacnet.PropertyReference{prop(bacnet.PropertyAll)}},
	})
	requireAbort(t, "RPM ALL at max APDU 50", err, bacnet.AbortReasonSegmentationNotSupported)
	awaitOperation(t, peer, "reply-too-long", 3*time.Second)
}

func TestBacnetJSDeviceManagementAndUnconfirmed(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startBacnetJS(t)
	if peer.assertedByReexec {
		return
	}
	c := newClientOpts(t, client.WithDeviceManagementEnabled(client.DeviceManagementConfirm))
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()
	if err := c.ReinitializeDevice(ctx, peer.target, service.ReinitializeDeviceRequest{State: service.ReinitializedWarmstart}); err != nil {
		t.Fatalf("ReinitializeDevice warmstart: %v", err)
	}
	awaitOperation(t, peer, "reinitialize-device", 3*time.Second)
	err := c.ReinitializeDevice(ctx, peer.target, service.ReinitializeDeviceRequest{State: service.ReinitializedStartBackup})
	requireBACnetError(t, "ReinitializeDevice startbackup", err, bacnet.ErrorClassServices, bacnet.ErrorCodeOptionalFunctionalityNotSupported)

	one := uint16(1)
	if err := c.DeviceCommunicationControl(ctx, peer.target, service.DeviceCommunicationControlRequest{
		EnableDisable: service.EnableDisableDisableInitiation, TimeDuration: &one}); err != nil {
		t.Fatalf("DCC disable-initiation: %v", err)
	}
	awaitOperation(t, peer, "device-communication-control", 3*time.Second)
	if err := c.DeviceCommunicationControl(ctx, peer.target, service.DeviceCommunicationControlRequest{
		EnableDisable: service.EnableDisableEnable}); err != nil {
		t.Fatalf("DCC enable: %v", err)
	}
	err = c.DeviceCommunicationControl(ctx, peer.target, service.DeviceCommunicationControlRequest{
		EnableDisable: service.EnableDisableDisable, TimeDuration: &one})
	requireBACnetError(t, "DCC disable", err, bacnet.ErrorClassServices, bacnet.ErrorCodeServiceRequestDenied)

	now := bacnetDateTime(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	if err := c.TimeSynchronization(ctx, peer.endpoint, false, service.TimeSynchronization(now)); err != nil {
		t.Fatalf("TimeSynchronization: %v", err)
	}
	awaitOperation(t, peer, "time-synchronization", 3*time.Second)
	if err := c.UTCTimeSynchronization(ctx, peer.endpoint, false, service.TimeSynchronization(now)); err != nil {
		t.Fatalf("UTCTimeSynchronization: %v", err)
	}
	awaitOperation(t, peer, "utc-time-synchronization", 3*time.Second)
	if err := c.UnconfirmedPrivateTransfer(ctx, peer.endpoint, false, service.PrivateTransfer{VendorID: 999, ServiceNumber: 1}); err != nil {
		t.Fatalf("UnconfirmedPrivateTransfer: %v", err)
	}
	awaitOperation(t, peer, "unconfirmed-private-transfer", 3*time.Second)
	if err := c.ConfirmedPrivateTransfer(ctx, peer.target, service.PrivateTransfer{VendorID: 999, ServiceNumber: 1}); err != nil {
		t.Fatalf("ConfirmedPrivateTransfer: %v", err)
	}
	awaitOperation(t, peer, "confirmed-private-transfer", 3*time.Second)
	_ = dev
}

// ---- device-server-v1 -------------------------------------------------------

func TestBacnetJSReadRange(t *testing.T) {
	peer := startBacnetJSServer(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()
	tl := objectID(bacnet.ObjectTypeTrendLog, 1)
	ref := prop(bacnet.PropertyLogBuffer)
	stamp := func(min int) time.Time {
		return time.Date(2009, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(min) * time.Minute)
	}
	check := func(what string, ack service.ReadRangeACK, first int, n int) {
		t.Helper()
		if len(ack.LogRecords) != n || int(ack.ItemCount) != n {
			t.Fatalf("%s: %d records (item count %d), want %d", what, len(ack.LogRecords), ack.ItemCount, n)
		}
		for i, r := range ack.LogRecords {
			k := first + i
			want := bacnetDateTime(stamp(15 * k))
			if r.Timestamp.Date.Year != want.Date.Year || r.Timestamp.Date.Day != want.Date.Day ||
				r.Timestamp.Time.Hour != want.Time.Hour || r.Timestamp.Time.Minute != want.Time.Minute {
				t.Fatalf("%s record %d at %+v, want %+v", what, i, r.Timestamp, want)
			}
			if r.Datum.Kind != bacnet.ValueReal || r.Datum.Real != float32(k) {
				t.Fatalf("%s record %d datum %+v, want REAL %d", what, i, r.Datum, k)
			}
		}
	}
	ack, err := c.ReadRange(ctx, peer.target, service.ReadRangeRequest{Object: tl, Property: ref, By: service.ReadRangeByPosition, ReferenceIndex: 4, Count: 3})
	if err != nil {
		t.Fatalf("ReadRange by position: %v", err)
	}
	check("by position 4,3", ack, 3, 3)
	// Every requested record is returned: MORE_ITEMS clear (clause 15.8,
	// Result Flags), FIRST_ITEM and LAST_ITEM clear for records 4..6 of 10.
	if ack.FirstItem() || ack.LastItem() || ack.MoreItems() {
		t.Fatalf("by position 4,3: result flags %+v, want none set", ack.ResultFlags)
	}
	ack, err = c.ReadRange(ctx, peer.target, service.ReadRangeRequest{Object: tl, Property: ref, By: service.ReadRangeByPosition, ReferenceIndex: 10, Count: -2})
	if err != nil {
		t.Fatalf("ReadRange by position, negative count: %v", err)
	}
	check("by position 10,-2", ack, 8, 2)
	if ack.FirstItem() || !ack.LastItem() || ack.MoreItems() {
		t.Fatalf("by position 10,-2: result flags %+v, want LAST_ITEM only", ack.ResultFlags)
	}
	ack, err = c.ReadRange(ctx, peer.target, service.ReadRangeRequest{Object: tl, Property: ref, By: service.ReadRangeBySequenceNumber, ReferenceIndex: 2, Count: 2})
	if err != nil {
		t.Fatalf("ReadRange by sequence number: %v", err)
	}
	check("by sequence 2,2", ack, 1, 2)
	if ack.FirstSequence == nil || *ack.FirstSequence != 2 {
		t.Fatalf("first sequence %v, want 2", ack.FirstSequence)
	}
	ack, err = c.ReadRange(ctx, peer.target, service.ReadRangeRequest{Object: tl, Property: ref, By: service.ReadRangeByTime,
		ReferenceTime: bacnetDateTime(stamp(20)), Count: 2})
	if err != nil {
		t.Fatalf("ReadRange by time: %v", err)
	}
	check("by time 00:20,2", ack, 2, 2)
	all, err := c.ReadRangeAll(ctx, peer.target, service.ReadRangeRequest{Object: tl, Property: ref, By: service.ReadRangeByPosition, ReferenceIndex: 1, Count: 4}, client.ReadRangePageOptions{})
	if err != nil || len(all.LogRecords) != 10 {
		t.Fatalf("ReadRangeAll: %d records, %v", len(all.LogRecords), err)
	}

	_, err = c.ReadRange(ctx, peer.target, service.ReadRangeRequest{Object: objectID(bacnet.ObjectTypeAnalogValue, 1),
		Property: prop(bacnet.PropertyPresentValue), By: service.ReadRangeByPosition, ReferenceIndex: 1, Count: 1})
	requireBACnetError(t, "ReadRange present-value", err, bacnet.ErrorClassServices, bacnet.ErrorCodePropertyIsNotAList)
	_, err = c.ReadRange(ctx, peer.target, service.ReadRangeRequest{Object: objectID(bacnet.ObjectTypeTrendLog, 9),
		Property: ref, By: service.ReadRangeByPosition, ReferenceIndex: 1, Count: 1})
	requireBACnetError(t, "ReadRange unknown object", err, bacnet.ErrorClassObject, bacnet.ErrorCodeUnknownObject)
}

func TestBacnetJSFiles(t *testing.T) {
	peer := startBacnetJSServer(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 30*time.Second)
	defer cancel()
	f1 := objectID(bacnet.ObjectTypeFile, 1)
	f2 := objectID(bacnet.ObjectTypeFile, 2)
	f3 := objectID(bacnet.ObjectTypeFile, 3)

	data, err := c.ReadFileStream(ctx, peer.target, f1, client.FileReadOptions{})
	if err != nil || string(data) != "hello-world-stream-file-data" {
		t.Fatalf("ReadFileStream file 1: %q %v", data, err)
	}
	if _, err := c.AtomicWriteFile(ctx, peer.target, service.AtomicWriteFileRequest{File: f1, Access: service.FileAccessStream, StartPosition: 0, Data: []byte("HELLO")}); err != nil {
		t.Fatalf("AtomicWriteFile file 1: %v", err)
	}
	awaitOperation(t, peer, "atomic-write-file", 3*time.Second)
	data, err = c.ReadFileStream(ctx, peer.target, f1, client.FileReadOptions{})
	if err != nil || string(data) != "HELLO-world-stream-file-data" {
		t.Fatalf("file 1 after write: %q %v", data, err)
	}
	// Append (start position -1, clause 14.2.1.1.3).
	ack, err := c.AtomicWriteFile(ctx, peer.target, service.AtomicWriteFileRequest{File: f1, Access: service.FileAccessStream, StartPosition: -1, Data: []byte("!")})
	if err != nil || ack.StartPosition != int32(len("HELLO-world-stream-file-data")) {
		t.Fatalf("append: %+v %v", ack, err)
	}

	recs, err := c.ReadFileRecords(ctx, peer.target, f2, 0, client.FileReadOptions{})
	if err != nil || len(recs) != 3 || string(recs[0]) != "record-0001" || string(recs[2]) != "record-0003" {
		t.Fatalf("ReadFileRecords file 2: %q %v", recs, err)
	}
	_, err = c.AtomicReadFile(ctx, peer.target, service.AtomicReadFileRequest{File: f2, Access: service.FileAccessStream, StartPosition: 0, Count: 10})
	requireBACnetError(t, "stream read of a record file", err, bacnet.ErrorClassServices, bacnet.ErrorCodeInvalidFileAccessMethod)
	_, err = c.AtomicReadFile(ctx, peer.target, service.AtomicReadFileRequest{File: objectID(bacnet.ObjectTypeFile, 99), Access: service.FileAccessStream, Count: 10})
	requireBACnetError(t, "unknown file", err, bacnet.ErrorClassObject, bacnet.ErrorCodeUnknownObject)
	_, err = c.AtomicReadFile(ctx, peer.target, service.AtomicReadFileRequest{File: f1, Access: service.FileAccessStream, StartPosition: 1000, Count: 10})
	requireBACnetError(t, "start beyond the end", err, bacnet.ErrorClassServices, bacnet.ErrorCodeInvalidFileStartPosition)

	// 3000 octets in one ACK do not fit 1476: Abort segmentation-not-supported.
	_, err = c.AtomicReadFile(ctx, peer.target, service.AtomicReadFileRequest{File: f3, Access: service.FileAccessStream, StartPosition: 0, Count: 3000})
	requireAbort(t, "read 3000 octets", err, bacnet.AbortReasonSegmentationNotSupported)
	// In chunks the whole file reads: octet i = (7 i + 3) mod 256.
	data, err = c.ReadFileStream(ctx, peer.target, f3, client.FileReadOptions{})
	if err != nil || len(data) != 3000 {
		t.Fatalf("ReadFileStream file 3: %d octets, %v", len(data), err)
	}
	want := make([]byte, 3000)
	for i := range want {
		want[i] = byte((7*i + 3) % 256)
	}
	if !bytes.Equal(data, want) {
		t.Fatal("file 3 content differs from the fixture")
	}
}

func TestBacnetJSObjectLifecycle(t *testing.T) {
	peer := startBacnetJSServer(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()
	av150 := objectID(bacnet.ObjectTypeAnalogValue, 150)
	ack, err := c.CreateObject(ctx, peer.target, service.CreateObjectRequest{ObjectIdentifier: &av150, InitialValues: []service.PropertyValue{
		{Property: prop(bacnet.PropertyObjectName), Value: bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: "AV-created"}}},
	}})
	if err != nil || ack.Object != av150 {
		t.Fatalf("CreateObject av,150: %+v %v", ack, err)
	}
	awaitOperation(t, peer, "create-object", 3*time.Second)
	name, err := c.ReadProperty(ctx, peer.target, av150, prop(bacnet.PropertyObjectName))
	if err != nil || characterString(t, name) != "AV-created" {
		t.Fatalf("created object name: %v %v", name, err)
	}
	_, err = c.CreateObject(ctx, peer.target, service.CreateObjectRequest{ObjectIdentifier: &av150, InitialValues: []service.PropertyValue{
		{Property: prop(bacnet.PropertyObjectName), Value: bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: "again"}}},
	}})
	requireBACnetError(t, "CreateObject existing", err, bacnet.ErrorClassObject, bacnet.ErrorCodeObjectIdentifierAlreadyExists)
	bv := objectID(bacnet.ObjectTypeBinaryValue, 7)
	_, err = c.CreateObject(ctx, peer.target, service.CreateObjectRequest{ObjectIdentifier: &bv, InitialValues: []service.PropertyValue{
		{Property: prop(bacnet.PropertyObjectName), Value: bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: "bv"}}},
	}})
	requireBACnetError(t, "CreateObject binary-value", err, bacnet.ErrorClassObject, bacnet.ErrorCodeDynamicCreationNotSupported)

	if err := c.DeleteObject(ctx, peer.target, service.DeleteObjectRequest{Object: av150}); err != nil {
		t.Fatalf("DeleteObject: %v", err)
	}
	err = c.DeleteObject(ctx, peer.target, service.DeleteObjectRequest{Object: av150})
	requireBACnetError(t, "DeleteObject again", err, bacnet.ErrorClassObject, bacnet.ErrorCodeUnknownObject)
	err = c.DeleteObject(ctx, peer.target, service.DeleteObjectRequest{Object: objectID(bacnet.ObjectTypeDevice, 1234)})
	requireBACnetError(t, "DeleteObject device", err, bacnet.ErrorClassObject, bacnet.ErrorCodeObjectDeletionNotPermitted)
}

func TestBacnetJSAlarmsAndEvents(t *testing.T) {
	peer := startBacnetJSServer(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 30*time.Second)
	defer cancel()
	av1 := objectID(bacnet.ObjectTypeAnalogValue, 1)
	av2 := objectID(bacnet.ObjectTypeAnalogValue, 2)

	ges, err := c.GetEnrollmentSummary(ctx, peer.target, service.GetEnrollmentSummaryRequest{})
	if err != nil || len(ges.Entries) != 1 || ges.Entries[0].Object != av2 || ges.Entries[0].EventState != 0 ||
		ges.Entries[0].EventType != 5 || ges.Entries[0].NotificationClass != 1 {
		t.Fatalf("GetEnrollmentSummary normal: %+v %v", ges, err)
	}
	info, err := c.GetEventInformation(ctx, peer.target, service.GetEventInformationRequest{})
	if err != nil || len(info.Summaries) != 0 || info.MoreEvents {
		t.Fatalf("GetEventInformation normal: %+v %v", info, err)
	}

	if err := c.WriteProperty(ctx, peer.target, av2, prop(bacnet.PropertyPresentValue), bacnet.RealValue(90), nil); err != nil {
		t.Fatalf("AV-2 = 90: %v", err)
	}
	awaitOperation(t, peer, "event-state-transition", 3*time.Second)
	info, err = c.GetEventInformation(ctx, peer.target, service.GetEventInformationRequest{})
	if err != nil || len(info.Summaries) != 1 || info.Summaries[0].Object != av2 || info.Summaries[0].EventState != 3 {
		t.Fatalf("GetEventInformation high-limit: %+v %v", info, err)
	}
	ges, err = c.GetEnrollmentSummary(ctx, peer.target, service.GetEnrollmentSummaryRequest{})
	if err != nil || len(ges.Entries) != 1 || ges.Entries[0].EventState != 3 {
		t.Fatalf("GetEnrollmentSummary high-limit: %+v %v", ges, err)
	}
	if err := c.AcknowledgeAlarm(ctx, peer.target, service.AcknowledgeAlarmRequest{
		ProcessIdentifier: 7, EventObject: av2, EventStateAcked: 3,
		TimeStamp:            eventTimeStamp(t, info.Summaries[0].EventTimeStamps, 0),
		AcknowledgmentSource: bacnet.CharacterString{Value: "go-bacnet interop"},
		TimeOfAcknowledgment: service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 1},
	}); err != nil {
		t.Fatalf("AcknowledgeAlarm: %v", err)
	}
	awaitOperation(t, peer, "acknowledge-alarm", 3*time.Second)
	err = c.AcknowledgeAlarm(ctx, peer.target, service.AcknowledgeAlarmRequest{
		ProcessIdentifier: 7, EventObject: av2, EventStateAcked: 3,
		TimeStamp:            service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 99},
		AcknowledgmentSource: bacnet.CharacterString{Value: "go-bacnet interop"},
		TimeOfAcknowledgment: service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 2},
	})
	requireBACnetError(t, "AcknowledgeAlarm wrong time stamp", err, bacnet.ErrorClassServices, bacnet.ErrorCodeInvalidTimeStamp)
	err = c.AcknowledgeAlarm(ctx, peer.target, service.AcknowledgeAlarmRequest{
		ProcessIdentifier: 7, EventObject: av1, EventStateAcked: 3,
		TimeStamp:            service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 0},
		AcknowledgmentSource: bacnet.CharacterString{Value: "go-bacnet interop"},
		TimeOfAcknowledgment: service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 2},
	})
	requireBACnetError(t, "AcknowledgeAlarm AV-1", err, bacnet.ErrorClassObject, bacnet.ErrorCodeNoAlarmConfigured)

	// GetAlarmSummary: upstream's AlarmSummary.encode writes the three
	// values of each summary with context tags 12, 9 and 8 where clause
	// 13.10.2 has application tags; go-bacnet refuses the ACK.
	_, err = c.GetAlarmSummary(ctx, peer.target)
	if err == nil {
		t.Fatal("GetAlarmSummary decoded an ACK with context-tagged summaries")
	}
	t.Logf("GetAlarmSummary (upstream AlarmSummary.encode, context tags): %v", err)

	if err := c.WriteProperty(ctx, peer.target, av2, prop(bacnet.PropertyPresentValue), bacnet.RealValue(50), nil); err != nil {
		t.Fatalf("AV-2 = 50: %v", err)
	}
	sum, err := c.GetAlarmSummary(ctx, peer.target)
	if err != nil || len(sum.Entries) != 0 {
		t.Fatalf("empty GetAlarmSummary: %+v %v", sum, err)
	}
}

// Requests the adapter leaves to upstream: AddListElement and
// RemoveListElement have no listener, and upstream answers them with Error
// services / reject-unrecognized-service (client.ts _processServiceRequest).
// ConfirmedTextMessage is outside upstream's service map and gets no answer.
func TestBacnetJSUnsupportedServices(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startBacnetJSServer(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()
	nc := objectID(bacnet.ObjectTypeNotificationClass, 1)
	elems := []bacnet.Element{{Value: bacnet.ApplicationValue{Kind: bacnet.ValueNull}}}
	err := c.AddListElement(ctx, peer.target, service.ListElementRequest{Object: nc, Property: prop(bacnet.PropertyRecipientList), Elements: elems})
	requireBACnetError(t, "AddListElement", err, bacnet.ErrorClassServices, bacnet.ErrorCodeRejectUnrecognizedService)
	err = c.RemoveListElement(ctx, peer.target, service.ListElementRequest{Object: nc, Property: prop(bacnet.PropertyRecipientList), Elements: elems})
	requireBACnetError(t, "RemoveListElement", err, bacnet.ErrorClassServices, bacnet.ErrorCodeRejectUnrecognizedService)

	short, cancelShort := context.WithTimeout(ctx, 4*time.Second)
	defer cancelShort()
	err = c.ConfirmedTextMessage(short, peer.target, service.TextMessage{TextMessageSourceDevice: deviceObject(dev), Message: "hello"})
	if err == nil {
		t.Fatal("ConfirmedTextMessage: acknowledged by a device without the service")
	}
	t.Logf("ConfirmedTextMessage (outside upstream's service map, no answer): %v", err)
}

// GetEnrollmentSummary with an event-state filter: go-bacnet sends the
// primitive [2] ENUMERATED of clause 13.11.1.1.3. Upstream's decoder
// (services/GetEnrollmentSummary.ts) reads [2] as an opening tag and the next
// octet as an inner tag: the filter offnormal (0x29 0x00) decodes by accident,
// the filter normal (0x29 0x02) throws, and upstream drops the request without
// an answer (client.ts _processServiceRequest).
func TestBacnetJSEnrollmentSummaryFilter(t *testing.T) {
	peer := startBacnetJSServer(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()
	offnormal, normal := uint32(0), uint32(2)
	ges, err := c.GetEnrollmentSummary(ctx, peer.target, service.GetEnrollmentSummaryRequest{EventStateFilter: &offnormal})
	if err != nil || len(ges.Entries) != 0 {
		t.Fatalf("filter offnormal while AV-2 is normal: %+v %v", ges, err)
	}
	short, cancelShort := context.WithTimeout(ctx, 4*time.Second)
	defer cancelShort()
	ges, err = c.GetEnrollmentSummary(short, peer.target, service.GetEnrollmentSummaryRequest{EventStateFilter: &normal})
	if err == nil {
		t.Fatalf("filter normal answered %+v; upstream's decoder used to drop it (recorded in docs/COMPATIBILITY.md)", ges)
	}
	t.Logf("filter normal (upstream GetEnrollmentSummary.decode throws, no answer): %v", err)
}
