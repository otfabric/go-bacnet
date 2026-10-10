//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"bytes"
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/device"
	"github.com/otfabric/go-bacnet/service"
)

// The System.IO.BACnet peer (ela-compil/BACnet, the C#/.NET stack behind YABE;
// NuGet package "BACnet" 4.0.0). Its BVLC/NPDU/APDU layers, request dispatch,
// segmentation and DeviceStorage object model are upstream; the adapter
// supplies the fixture loading and the handlers between the two.
//
// Where this peer answers differently from the other four, the test asserts
// what it really does and the comment says so ("Deviation:").

const (
	defaultSysIOBACnetImage = "bacnet-interop-sysiobacnet:local"
	sysioAdapter            = "sysiobacnet"
	sysioMinAdapterVersion  = "0.10.0"

	// The transport caps the APDU at its 1472-octet UDP payload minus BVLC and
	// NPDU headers, so the peer announces 1464 instead of a standard size.
	sysioMaxAPDU = 1464
	// The adapter seeds this many Trend Log records: REAL 20, 21, … one minute
	// apart from 2024-01-01 00:00:00, sequence number = position.
	sysioLogRecords = 4

	sysioPropertyRecordCount          bacnet.PropertyIdentifier = 141
	sysioPropertyFileSize             bacnet.PropertyIdentifier = 42
	sysioPropertyDeviceAddressBinding bacnet.PropertyIdentifier = 30
	sysioPropertyServicesSupported    bacnet.PropertyIdentifier = 97
	sysioPropertyObjectTypesSupported bacnet.PropertyIdentifier = 96
)

func sysiobacnetImage() string {
	return getEnv("SYSIOBACNET_IMAGE", defaultSysIOBACnetImage)
}

// startSysIO starts the peer on the default fixture (device-baseline-v2).
func startSysIO(t *testing.T, env ...string) (deviceFixture, *peerHandle) {
	t.Helper()
	requireAdapterVersion(t, sysiobacnetImage(), sysioMinAdapterVersion)
	dev := loadDeviceFixture(t)
	return dev, startPeer(t, sysiobacnetImage(), sysioAdapter, env...)
}

// startSysIOFixture starts the peer on another device fixture generation.
func startSysIOFixture(t *testing.T, fixture string) (deviceFixture, *peerHandle) {
	t.Helper()
	requireAdapterVersion(t, sysiobacnetImage(), sysioMinAdapterVersion)
	t.Setenv("BACNET_DEVICE_FIXTURE", sysioFixturePath(t, fixture))
	dev := loadDeviceFixture(t)
	if dev.Fixture != fixture {
		t.Fatalf("loaded fixture %q, want %q", dev.Fixture, fixture)
	}
	return dev, startPeer(t, sysiobacnetImage(), sysioAdapter,
		"DEVICE_FIXTURE_FILE=/fixtures/device/"+fixture+".json",
		"FIXTURE="+fixture,
	)
}

func sysioFixturePath(t *testing.T, fixture string) string {
	t.Helper()
	var candidates []string
	if root := os.Getenv("BACNET_INTEROP_ROOT"); root != "" {
		candidates = append(candidates, filepath.Join(root, "fixtures", "device", fixture+".json"))
	}
	candidates = append(candidates,
		filepath.Join("..", "bacnet-interop", "fixtures", "device", fixture+".json"),
		filepath.Join("..", "..", "bacnet-interop", "fixtures", "device", fixture+".json"),
	)
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	failOrSkip(t, "%s.json not found; set BACNET_INTEROP_ROOT", fixture)
	return ""
}

func sysioRequireError(t *testing.T, what string, err error, class, code uint16) *bacnet.ErrorResponse {
	t.Helper()
	var er *bacnet.ErrorResponse
	if !errors.As(err, &er) {
		t.Fatalf("%s: got %T %v, want Error %s/%s", what, err, err, bacnet.ErrorClassName(class), bacnet.ErrorCodeName(code))
	}
	if er.Class != class || er.Code != code {
		t.Fatalf("%s: Error %s/%s, want %s/%s", what,
			bacnet.ErrorClassName(er.Class), bacnet.ErrorCodeName(er.Code),
			bacnet.ErrorClassName(class), bacnet.ErrorCodeName(code))
	}
	return er
}

func sysioRequireReject(t *testing.T, what string, err error, reason uint8) {
	t.Helper()
	var rj *bacnet.RejectError
	if !errors.As(err, &rj) {
		t.Fatalf("%s: got %T %v, want Reject %s", what, err, err, bacnet.RejectReasonName(reason))
	}
	if rj.Reason != reason {
		t.Fatalf("%s: Reject %s, want %s", what, bacnet.RejectReasonName(rj.Reason), bacnet.RejectReasonName(reason))
	}
}

func sysioReadReal(t *testing.T, ctx context.Context, c *client.Client, target client.Target, obj bacnet.ObjectIdentifier) float32 {
	t.Helper()
	v, err := c.ReadProperty(ctx, target, obj, bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue})
	if err != nil {
		t.Fatalf("read present-value of %v: %v", obj, err)
	}
	f, err := bacnet.AsReal(v)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// sysioWaitPresentValue reads notifications until one carries present-value
// == want and returns it.
func sysioWaitPresentValue(t *testing.T, sub client.Subscription, want float32, timeout time.Duration) *service.COVNotification {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case <-deadline:
			t.Fatalf("no COV notification carrying present-value=%v within %v", want, timeout)
		case ev, ok := <-sub.Events():
			if !ok {
				t.Fatal("subscription closed before the notification")
			}
			switch ev.State {
			case client.SubscriptionDegraded, client.SubscriptionExpired, client.SubscriptionClosed:
				t.Fatalf("subscription went %v: %v", ev.State, ev.Err)
			}
			if ev.Notification == nil {
				continue
			}
			for _, v := range ev.Notification.Values {
				if v.Property.Identifier == bacnet.PropertyPresentValue && v.Value.Kind == bacnet.ValueReal && v.Value.Real == want {
					return ev.Notification
				}
			}
		}
	}
}

func sysioLogTime(pos int) service.DateTime {
	ts := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(pos-1) * time.Minute)
	wd := uint8(ts.Weekday())
	if wd == 0 {
		wd = 7
	}
	return service.DateTime{
		Date: bacnet.Date{Year: uint8(ts.Year() - 1900), Month: uint8(ts.Month()), Day: uint8(ts.Day()), Weekday: wd},
		Time: bacnet.Time{Hour: uint8(ts.Hour()), Minute: uint8(ts.Minute()), Second: uint8(ts.Second())},
	}
}

// sysioRequireRange checks a ReadRange reply against the seeded log: items
// records starting at 1-based position firstPos, and the result flags.
func sysioRequireRange(t *testing.T, what string, ack service.ReadRangeACK, items, firstPos int, first, last bool) {
	t.Helper()
	if int(ack.ItemCount) != items || len(ack.LogRecords) != items {
		t.Fatalf("%s: itemCount %d with %d decoded records, want %d", what, ack.ItemCount, len(ack.LogRecords), items)
	}
	if ack.FirstItem() != first || ack.LastItem() != last || ack.MoreItems() {
		t.Fatalf("%s: result flags first=%v last=%v more=%v, want first=%v last=%v more=false", what,
			ack.FirstItem(), ack.LastItem(), ack.MoreItems(), first, last)
	}
	for i, rec := range ack.LogRecords {
		pos := firstPos + i
		if want := sysioLogTime(pos); rec.Timestamp != want {
			t.Fatalf("%s: record %d (position %d) time stamp %+v, want %+v", what, i, pos, rec.Timestamp, want)
		}
		if rec.DatumChoice != service.LogDatumReal || rec.Datum.Kind != bacnet.ValueReal || rec.Datum.Real != float32(19+pos) {
			t.Fatalf("%s: record %d (position %d) logDatum choice %d %+v, want REAL %v", what, i, pos, rec.DatumChoice, rec.Datum, float32(19+pos))
		}
		if rec.StatusFlags == nil || rec.StatusFlags.UnusedBits != 4 || len(rec.StatusFlags.Bytes) != 1 || rec.StatusFlags.Bytes[0] != 0 {
			t.Fatalf("%s: record %d statusFlags %+v, want four clear bits", what, i, rec.StatusFlags)
		}
	}
}

// ---------------------------------------------------------------------------
// Discovery
// ---------------------------------------------------------------------------

// A unicast Who-Is is answered with an I-Am sent back to the requester.
//
// Deviation: Max_APDU_Length_Accepted is 1464, not one of the standard sizes.
func TestSysIOBACnetWhoIsIAm(t *testing.T) {
	dev, peer := startSysIO(t)
	if peer.assertedByReexec {
		return
	}
	c := newDiscoveryClient(t)

	low, high := dev.DeviceInstance, dev.DeviceInstance
	if err := c.SendWhoIs(context.Background(), peer.endpoint, false, client.DiscoveryOptions{
		LowLimit:  &low,
		HighLimit: &high,
	}); err != nil {
		t.Fatalf("SendWhoIs: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		for _, obs := range c.Devices() {
			if obs.Instance != dev.DeviceInstance {
				continue
			}
			caps := obs.Capabilities
			if !caps.MaxAPDULengthAccepted.Known || caps.MaxAPDULengthAccepted.Value != sysioMaxAPDU {
				t.Fatalf("I-Am MaxAPDULengthAccepted = %+v, want %d", caps.MaxAPDULengthAccepted, sysioMaxAPDU)
			}
			if !caps.Segmentation.Known || caps.Segmentation.Value != 0 {
				t.Fatalf("I-Am Segmentation = %+v, want segmented-both(0)", caps.Segmentation)
			}
			if !caps.VendorID.Known || caps.VendorID.Value != 999 {
				t.Fatalf("I-Am VendorID = %+v, want 999", caps.VendorID)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("no I-Am for device %d; devices=%v", dev.DeviceInstance, c.Devices())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// A Who-Is whose instance range excludes the device gets no answer.
func TestSysIOBACnetWhoIsOutOfRangeIsSilent(t *testing.T) {
	dev, peer := startSysIO(t)
	if peer.assertedByReexec {
		return
	}
	c := newDiscoveryClient(t)
	low, high := dev.DeviceInstance+1, dev.DeviceInstance+10
	if err := c.SendWhoIs(context.Background(), peer.endpoint, false, client.DiscoveryOptions{LowLimit: &low, HighLimit: &high}); err != nil {
		t.Fatalf("SendWhoIs: %v", err)
	}
	time.Sleep(1500 * time.Millisecond)
	if got := c.Devices(); len(got) != 0 {
		t.Fatalf("I-Am for a Who-Is that excludes the device: %+v", got)
	}
}

func TestSysIOBACnetWhoHasIHave(t *testing.T) {
	dev, peer := startSysIO(t)
	if peer.assertedByReexec {
		return
	}
	c := newDiscoveryClient(t)
	av, _ := analogValueObject(dev)
	bv := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeBinaryValue, Instance: 1}

	await := func(what string, obj bacnet.ObjectIdentifier, name string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			for _, obs := range c.Objects() {
				if obs.DeviceInstance == dev.DeviceInstance && obs.Object == obj && obs.Name.Value == name {
					return
				}
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s: no I-Have for %s from device %d; objects=%v", what, name, dev.DeviceInstance, c.Objects())
			}
			time.Sleep(50 * time.Millisecond)
		}
	}

	name := bacnet.CharacterString{Value: "AV-1"}
	if err := c.SendWhoHas(context.Background(), peer.endpoint, false, client.WhoHasOptions{Name: &name}); err != nil {
		t.Fatalf("SendWhoHas by name: %v", err)
	}
	await("Who-Has by name", av, "AV-1")
	if err := c.SendWhoHas(context.Background(), peer.endpoint, false, client.WhoHasOptions{Object: &bv}); err != nil {
		t.Fatalf("SendWhoHas by object identifier: %v", err)
	}
	await("Who-Has by object identifier", bv, "BV-1")

	// An object the device does not have: no I-Have.
	missing := bacnet.CharacterString{Value: "NO-SUCH-OBJECT"}
	if err := c.SendWhoHas(context.Background(), peer.endpoint, false, client.WhoHasOptions{Name: &missing}); err != nil {
		t.Fatalf("SendWhoHas for a missing name: %v", err)
	}
	time.Sleep(time.Second)
	if got := c.Objects(); len(got) != 2 {
		t.Fatalf("objects after a Who-Has for a missing name: %+v, want only AV-1 and BV-1", got)
	}
}

// ---------------------------------------------------------------------------
// ReadProperty
// ---------------------------------------------------------------------------

func TestSysIOBACnetReadDeviceObjectName(t *testing.T) {
	dev, peer := startSysIO(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 8*time.Second)
	defer cancel()

	val, err := c.ReadProperty(ctx, peer.target, deviceObject(dev), bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName})
	if err != nil {
		t.Fatalf("ReadProperty object-name: %v", err)
	}
	if name := characterString(t, val); name != dev.DeviceName {
		t.Fatalf("object-name=%q, want %q", name, dev.DeviceName)
	}
}

func TestSysIOBACnetReadAnalogValue(t *testing.T) {
	dev, peer := startSysIO(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 8*time.Second)
	defer cancel()

	av, want := analogValueObject(dev)
	if got := sysioReadReal(t, ctx, c, peer.target, av); math.Abs(float64(got-want)) > 0.01 {
		t.Fatalf("present-value=%v, want %v", got, want)
	}
}

// A Target that names only the endpoint is completed by the client.
func TestSysIOBACnetEndpointOnlyTarget(t *testing.T) {
	dev, peer := startSysIO(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 10*time.Second)
	defer cancel()
	val, err := c.ReadProperty(ctx, client.Target{Endpoint: peer.target.Endpoint}, deviceObject(dev),
		bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName})
	if err != nil {
		t.Fatalf("ReadProperty with endpoint-only target: %v", err)
	}
	if got := characterString(t, val); got != dev.DeviceName {
		t.Fatalf("object-name = %q, want %q", got, dev.DeviceName)
	}
}

// The Device wildcard instance (4194303) is resolved by the upstream
// DeviceStorage: the value read is the device's real identifier.
//
// Deviation: the ReadProperty-ACK's own objectIdentifier still says
// device,4194303. bacnet-stack and BACnet4J put the real instance there.
func TestSysIOBACnetReadDeviceByWildcardInstance(t *testing.T) {
	dev, peer := startSysIO(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 10*time.Second)
	defer cancel()
	ref := bacnet.PropertyReference{Identifier: bacnet.PropertyObjectIdentifier}
	val, err := c.ReadProperty(ctx, peer.target, bacnet.AnyDevice(), ref)
	if err != nil {
		t.Fatalf("ReadProperty device:4194303 object-identifier: %v", err)
	}
	id, err := bacnet.AsObjectID(val)
	if err != nil {
		t.Fatal(err)
	}
	if id != deviceObject(dev) {
		t.Fatalf("device identifier = %v, want %v", id, deviceObject(dev))
	}

	payload, err := service.EncodeReadProperty(service.ReadPropertyRequest{Object: bacnet.AnyDevice(), Property: ref})
	if err != nil {
		t.Fatal(err)
	}
	pdu, err := c.InvokeConfirmed(ctx, peer.target, apdu.ServiceReadProperty, payload, client.ConfirmedInvokeOptions{})
	if err != nil || pdu.ComplexACK == nil {
		t.Fatalf("raw ReadProperty device:4194303: %v", err)
	}
	ack, err := service.DecodeReadPropertyACK(pdu.ComplexACK.Payload, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if ack.Object != bacnet.AnyDevice() {
		t.Fatalf("ACK objectIdentifier = %v: System.IO.BACnet 4.0.0 echoed the wildcard; update the deviation note", ack.Object)
	}
}

func TestSysIOBACnetReadPropertyUnknownPropertyError(t *testing.T) {
	dev, peer := startSysIO(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 8*time.Second)
	defer cancel()

	_, err := c.ReadProperty(ctx, peer.target, deviceObject(dev), bacnet.PropertyReference{Identifier: unknownPropertyID})
	if err == nil {
		t.Fatal("expected Error PDU for unknown property")
	}
	assertErrorUnknownProperty(t, err)
}

func TestSysIOBACnetReadPropertyUnknownObjectError(t *testing.T) {
	_, peer := startSysIO(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 10*time.Second)
	defer cancel()
	missing := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogInput, Instance: 4000000}
	_, err := c.ReadProperty(ctx, peer.target, missing, bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue})
	sysioRequireError(t, "ReadProperty of a missing object", err, bacnet.ErrorClassObject, bacnet.ErrorCodeUnknownObject)
}

// Object_List by array index: 0 is the element count, N the Nth identifier,
// and an index past the end a definite Error.
func TestSysIOBACnetReadObjectListByIndex(t *testing.T) {
	dev, peer := startSysIO(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 15*time.Second)
	defer cancel()

	at := func(i uint32) (bacnet.ApplicationValue, error) {
		return c.ReadProperty(ctx, peer.target, deviceObject(dev),
			bacnet.PropertyReference{Identifier: bacnet.PropertyObjectList, ArrayIndex: &i})
	}
	v, err := at(0)
	if err != nil {
		t.Fatalf("object-list[0]: %v", err)
	}
	count, err := bacnet.AsUnsigned(v)
	if err != nil {
		t.Fatal(err)
	}
	if int(count) != len(dev.Objects) {
		t.Fatalf("object-list[0] = %d, want %d (every fixture object)", count, len(dev.Objects))
	}
	av, _ := analogValueObject(dev)
	var seen []bacnet.ObjectIdentifier
	for i := uint32(1); i <= uint32(count); i++ {
		v, err := at(i)
		if err != nil {
			t.Fatalf("object-list[%d]: %v", i, err)
		}
		id, err := bacnet.AsObjectID(v)
		if err != nil {
			t.Fatalf("object-list[%d]: %v", i, err)
		}
		seen = append(seen, id)
	}
	if seen[0] != deviceObject(dev) || !containsObject(seen, av) {
		t.Fatalf("object-list by index = %v, want the device first and %v in it", seen, av)
	}
	_, err = at(uint32(count) + 1)
	sysioRequireError(t, "object-list past the end", err, bacnet.ErrorClassProperty, bacnet.ErrorCodeInvalidArrayIndex)

	list, err := c.ReadObjectList(ctx, peer.target, deviceObject(dev), 0)
	if err != nil {
		t.Fatalf("ReadObjectList: %v", err)
	}
	if len(list) != len(seen) {
		t.Fatalf("ReadObjectList = %v, by index = %v", list, seen)
	}
	for i := range list {
		if list[i] != seen[i] {
			t.Fatalf("ReadObjectList = %v, by index = %v", list, seen)
		}
	}
}

// Deviation: the upstream DeviceStorage treats every property as a list of
// values. An array index on a property that is not an array is answered
// instead of refused with property/property-is-not-an-array: index 0 gives the
// value count (1) and index 1 the value itself.
func TestSysIOBACnetArrayIndexOnScalarProperty(t *testing.T) {
	dev, peer := startSysIO(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 10*time.Second)
	defer cancel()

	zero, one, two := uint32(0), uint32(1), uint32(2)
	name := func(i *uint32) (bacnet.ApplicationValue, error) {
		return c.ReadProperty(ctx, peer.target, deviceObject(dev),
			bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName, ArrayIndex: i})
	}
	v, err := name(&zero)
	if err != nil {
		t.Fatalf("object-name[0]: %v", err)
	}
	if n, err := bacnet.AsUnsigned(v); err != nil || n != 1 {
		t.Fatalf("object-name[0] = %+v (%v), want Unsigned 1", v, err)
	}
	v, err = name(&one)
	if err != nil {
		t.Fatalf("object-name[1]: %v", err)
	}
	if got := characterString(t, v); got != dev.DeviceName {
		t.Fatalf("object-name[1] = %q, want %q", got, dev.DeviceName)
	}
	_, err = name(&two)
	sysioRequireError(t, "object-name[2]", err, bacnet.ErrorClassProperty, bacnet.ErrorCodeInvalidArrayIndex)
}

// ---------------------------------------------------------------------------
// ReadPropertyMultiple
// ---------------------------------------------------------------------------

func TestSysIOBACnetReadPropertyMultiple(t *testing.T) {
	dev, peer := startSysIO(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 8*time.Second)
	defer cancel()

	results, err := c.ReadPropertyMultiple(ctx, peer.target, []service.ReadAccessSpecification{
		{
			Object: deviceObject(dev),
			Properties: []bacnet.PropertyReference{
				{Identifier: bacnet.PropertyObjectName},
				{Identifier: bacnet.PropertyObjectIdentifier},
			},
		},
	})
	if err != nil {
		t.Fatalf("ReadPropertyMultiple: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("objects=%d, want 1", len(results))
	}
	if got := characterString(t, findPropertyValue(t, results[0], bacnet.PropertyObjectName)); got != dev.DeviceName {
		t.Fatalf("object-name=%q, want %q", got, dev.DeviceName)
	}
	id, err := bacnet.AsObjectID(findPropertyValue(t, results[0], bacnet.PropertyObjectIdentifier))
	if err != nil || id != deviceObject(dev) {
		t.Fatalf("object-identifier=%v (%v), want %v", id, err, deviceObject(dev))
	}
}

func TestSysIOBACnetReadPropertyMultiplePartialError(t *testing.T) {
	dev, peer := startSysIO(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 8*time.Second)
	defer cancel()

	results, err := c.ReadPropertyMultiple(ctx, peer.target, []service.ReadAccessSpecification{
		{
			Object: deviceObject(dev),
			Properties: []bacnet.PropertyReference{
				{Identifier: bacnet.PropertyObjectName},
				{Identifier: unknownPropertyID},
			},
		},
	})
	if err != nil {
		t.Fatalf("ReadPropertyMultiple transaction error: %v", err)
	}
	assertRPMPartialUnknownProperty(t, results, dev)
}

// A missing object in a ReadPropertyMultiple is a per-property
// object/unknown-object result next to the good object, not a failed request.
func TestSysIOBACnetReadPropertyMultipleUnknownObject(t *testing.T) {
	dev, peer := startSysIO(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 8*time.Second)
	defer cancel()

	missing := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogInput, Instance: 4000000}
	results, err := c.ReadPropertyMultiple(ctx, peer.target, []service.ReadAccessSpecification{
		{Object: deviceObject(dev), Properties: []bacnet.PropertyReference{{Identifier: bacnet.PropertyObjectName}}},
		{Object: missing, Properties: []bacnet.PropertyReference{{Identifier: bacnet.PropertyPresentValue}}},
	})
	if err != nil {
		t.Fatalf("ReadPropertyMultiple: %v", err)
	}
	if len(results) != 2 || results[1].Object != missing || len(results[1].Properties) != 1 {
		t.Fatalf("results = %+v, want the device and the missing object", results)
	}
	if got := characterString(t, findPropertyValue(t, results[0], bacnet.PropertyObjectName)); got != dev.DeviceName {
		t.Fatalf("object-name=%q, want %q", got, dev.DeviceName)
	}
	sysioRequireError(t, "missing object in RPM", results[1].Properties[0].Err, bacnet.ErrorClassObject, bacnet.ErrorCodeUnknownObject)
}

func TestSysIOBACnetReadPropertyMultipleBatched(t *testing.T) {
	dev, peer := startSysIO(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()
	av, wantPV := analogValueObject(dev)
	specs := []service.ReadAccessSpecification{
		{Object: deviceObject(dev), Properties: []bacnet.PropertyReference{
			{Identifier: bacnet.PropertyObjectName},
			{Identifier: bacnet.PropertyObjectIdentifier},
			{Identifier: unknownPropertyID},
		}},
		{Object: av, Properties: []bacnet.PropertyReference{
			{Identifier: bacnet.PropertyObjectName},
			{Identifier: bacnet.PropertyPresentValue},
		}},
	}
	// Two properties per transaction: the device spec is split over two
	// requests and must come back as one result again.
	results, err := c.ReadPropertyMultipleBatched(ctx, peer.target, specs, client.RPMBatchOptions{MaxPropertiesPerBatch: 2})
	if err != nil {
		t.Fatalf("ReadPropertyMultipleBatched: %v", err)
	}
	if len(results) != len(specs) {
		t.Fatalf("%d results, want one per spec (%d)", len(results), len(specs))
	}
	if results[0].Object != deviceObject(dev) || results[1].Object != av {
		t.Fatalf("result objects %v, %v", results[0].Object, results[1].Object)
	}
	if len(results[0].Properties) != 3 || len(results[1].Properties) != 2 {
		t.Fatalf("property counts %d, %d; want 3, 2", len(results[0].Properties), len(results[1].Properties))
	}
	if got := characterString(t, findPropertyValue(t, results[0], bacnet.PropertyObjectName)); got != dev.DeviceName {
		t.Fatalf("device object-name = %q, want %q", got, dev.DeviceName)
	}
	sysioRequireError(t, "unknown property result", results[0].Properties[2].Err, bacnet.ErrorClassProperty, bacnet.ErrorCodeUnknownProperty)
	pv, err := bacnet.AsReal(findPropertyValue(t, results[1], bacnet.PropertyPresentValue))
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(float64(pv-wantPV)) > 0.01 {
		t.Fatalf("present-value = %v, want %v", pv, wantPV)
	}
}

// ReadPropertyMultiple of the property "all": DeviceStorage answers with every
// property it stores for the object.
//
// Deviations:
//   - "required" and "optional" are not expanded: each comes back as a
//     per-property property/unknown-property error.
//   - BACnet 4.0.0 cannot encode an empty list in a ReadPropertyMultiple-ACK
//     (ASN1.encode_read_access_result reads the first value unchecked), so a
//     request that covers the Device's empty Device_Address_Binding, "all"
//     included, is answered with Abort (other). ReadProperty returns the same
//     property correctly.
func TestSysIOBACnetReadPropertyMultipleAll(t *testing.T) {
	dev, peer := startSysIO(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()
	av, wantPV := analogValueObject(dev)
	all := func(o bacnet.ObjectIdentifier, id bacnet.PropertyIdentifier) ([]service.ReadAccessResult, error) {
		return c.ReadPropertyMultiple(ctx, peer.target, []service.ReadAccessSpecification{
			{Object: o, Properties: []bacnet.PropertyReference{{Identifier: id}}},
		})
	}

	results, err := all(av, bacnet.PropertyAll)
	if err != nil {
		t.Fatalf("ReadPropertyMultiple all of %v: %v", av, err)
	}
	if len(results) != 1 || results[0].Object != av {
		t.Fatalf("results = %+v", results)
	}
	want := []bacnet.PropertyIdentifier{
		bacnet.PropertyObjectIdentifier, bacnet.PropertyObjectName, bacnet.PropertyObjectType, bacnet.PropertyDescription,
		bacnet.PropertyPresentValue, bacnet.PropertyStatusFlags, bacnet.PropertyEventState, 81, /* out-of-service */
		bacnet.PropertyUnits, bacnet.PropertyPriorityArray, bacnet.PropertyRelinquishDefault,
	}
	if len(results[0].Properties) != len(want) {
		t.Fatalf("%d properties, want %d: %+v", len(results[0].Properties), len(want), results[0].Properties)
	}
	for i, p := range results[0].Properties {
		if p.Property.Identifier != want[i] || p.Err != nil {
			t.Fatalf("property %d = %v (err %v), want %v", i, p.Property.Identifier, p.Err, want[i])
		}
	}
	pv, err := bacnet.AsReal(findPropertyValue(t, results[0], bacnet.PropertyPresentValue))
	if err != nil || math.Abs(float64(pv-wantPV)) > 0.01 {
		t.Fatalf("present-value = %v (%v), want %v", pv, err, wantPV)
	}
	prio := findPropertyValue(t, results[0], bacnet.PropertyPriorityArray)
	if prio.Kind != bacnet.ValueConstructed || len(prio.Elements) != 16 {
		t.Fatalf("priority-array = %+v, want 16 elements", prio)
	}
	for i, el := range prio.Elements {
		if el.Context || el.Value.Kind != bacnet.ValueNull {
			t.Fatalf("priority-array[%d] = %+v, want NULL", i+1, el)
		}
	}

	for _, id := range []bacnet.PropertyIdentifier{bacnet.PropertyRequired, bacnet.PropertyOptional} {
		results, err := all(av, id)
		if err != nil {
			t.Fatalf("ReadPropertyMultiple %v of %v: %v", id, av, err)
		}
		if len(results) != 1 || len(results[0].Properties) != 1 {
			t.Fatalf("ReadPropertyMultiple %v: %+v, want one unexpanded result", id, results)
		}
		sysioRequireError(t, "special property identifier", results[0].Properties[0].Err,
			bacnet.ErrorClassProperty, bacnet.ErrorCodeUnknownProperty)
	}

	requireAbortOther := func(what string, err error) {
		t.Helper()
		var abort *bacnet.AbortError
		if !errors.As(err, &abort) || abort.Reason != bacnet.AbortReasonOther {
			t.Fatalf("%s: got %T %v, want Abort (other): BACnet 4.0.0 could not encode an empty list here; "+
				"a result means upstream fixed it: update the deviation note", what, err, err)
		}
	}
	_, err = all(deviceObject(dev), bacnet.PropertyAll)
	requireAbortOther("ReadPropertyMultiple all of the device", err)
	_, err = c.ReadPropertyMultiple(ctx, peer.target, []service.ReadAccessSpecification{{
		Object: deviceObject(dev),
		Properties: []bacnet.PropertyReference{
			{Identifier: bacnet.PropertyObjectName},
			{Identifier: sysioPropertyDeviceAddressBinding},
		},
	}})
	requireAbortOther("ReadPropertyMultiple of an empty list", err)

	// An array index out of range fails the whole request too, with an Error.
	nine := uint32(9)
	_, err = c.ReadPropertyMultiple(ctx, peer.target, []service.ReadAccessSpecification{{
		Object: deviceObject(dev),
		Properties: []bacnet.PropertyReference{
			{Identifier: bacnet.PropertyObjectName},
			{Identifier: bacnet.PropertyObjectList, ArrayIndex: &nine},
		},
	}})
	sysioRequireError(t, "ReadPropertyMultiple with an index past the end", err,
		bacnet.ErrorClassProperty, bacnet.ErrorCodeInvalidArrayIndex)
}

func TestSysIOBACnetDeviceSnapshot(t *testing.T) {
	dev, peer := startSysIO(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 30*time.Second)
	defer cancel()
	snap, err := device.ReadSnapshot(ctx, c, peer.target, dev.DeviceInstance, device.SnapshotOptions{})
	if err != nil {
		t.Fatalf("ReadSnapshot: %v (partial: %v)", err, snap.PartialErrors)
	}
	if snap.ObjectName != dev.DeviceName {
		t.Fatalf("snapshot name = %q, want %q", snap.ObjectName, dev.DeviceName)
	}
	if snap.MaxAPDU != sysioMaxAPDU || snap.VendorID != 999 {
		t.Fatalf("snapshot MaxAPDU=%d VendorID=%d, want %d and 999", snap.MaxAPDU, snap.VendorID, sysioMaxAPDU)
	}
	av, _ := analogValueObject(dev)
	found := false
	for _, o := range snap.Objects {
		if o.Object == av {
			found = true
		}
	}
	if !found || len(snap.Objects) != len(dev.Objects) {
		t.Fatalf("snapshot has %d objects (AV-1 found: %v), want all %d fixture objects: %+v",
			len(snap.Objects), found, len(dev.Objects), snap.Objects)
	}
}

// ---------------------------------------------------------------------------
// Reject / Abort
// ---------------------------------------------------------------------------

// BacnetClient itself rejects a confirmed service nobody handles.
func TestSysIOBACnetRejectUnrecognizedService(t *testing.T) {
	_, peer := startSysIO(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 8*time.Second)
	defer cancel()

	_, err := c.InvokeConfirmed(ctx, peer.target, unrecognizedConfirmedService, nil, client.ConfirmedInvokeOptions{
		SegmentedResponseAccepted: true,
	})
	if err == nil {
		t.Fatal("expected Reject PDU for unrecognized service")
	}
	assertRejectUnrecognized(t, err)
}

// Deviation: services the adapter does not serve on this fixture are refused
// the same way, with Reject unrecognized-service: DeviceCommunicationControl
// always; GetEventInformation and AcknowledgeAlarm because device-baseline-v2
// has no Notification Class (the adapter serves them on v3); GetAlarmSummary
// because BacnetClient 4.0.0 rejects it whatever the application registers.
// The other peers answer these with a result or an Error.
func TestSysIOBACnetUnservedServicesRejected(t *testing.T) {
	dev, peer := startSysIO(t)
	if peer.assertedByReexec {
		return
	}
	c := newClientOpts(t, client.WithDeviceManagementEnabled(client.DeviceManagementConfirm))
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()

	err := c.DeviceCommunicationControl(ctx, peer.target, service.DeviceCommunicationControlRequest{
		EnableDisable: service.EnableDisableEnable,
	})
	sysioRequireReject(t, "DeviceCommunicationControl", err, bacnet.RejectReasonUnrecognizedService)

	_, err = c.GetEventInformation(ctx, peer.target, service.GetEventInformationRequest{})
	sysioRequireReject(t, "GetEventInformation", err, bacnet.RejectReasonUnrecognizedService)

	_, err = c.GetAlarmSummary(ctx, peer.target)
	sysioRequireReject(t, "GetAlarmSummary", err, bacnet.RejectReasonUnrecognizedService)

	av, _ := analogValueObject(dev)
	err = c.AcknowledgeAlarm(ctx, peer.target, service.AcknowledgeAlarmRequest{
		ProcessIdentifier:    1,
		EventObject:          av,
		EventStateAcked:      2,
		TimeStamp:            service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 1},
		AcknowledgmentSource: bacnet.CharacterString{Value: "go-bacnet interop"},
		TimeOfAcknowledgment: service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 1},
	})
	sysioRequireReject(t, "AcknowledgeAlarm", err, bacnet.RejectReasonUnrecognizedService)

	// The peer still answers afterwards.
	if _, err := c.ReadProperty(ctx, peer.target, deviceObject(dev), bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName}); err != nil {
		t.Fatalf("read after the rejected requests: %v", err)
	}
}

// Requests the stack cannot decode. A ReadProperty without its parameters is
// rejected with missing-required-parameter; a truncated WriteProperty and a
// garbage ReadPropertyMultiple are aborted with reason other.
//
// Deviation: like every Abort this stack sends, these carry no SRV bit.
func TestSysIOBACnetMalformedRequests(t *testing.T) {
	dev, peer := startSysIO(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()
	raw := func(svc uint8, payload []byte) error {
		_, err := c.InvokeConfirmed(ctx, peer.target, svc, payload, client.ConfirmedInvokeOptions{})
		return err
	}
	requireAbortOther := func(what string, err error) {
		t.Helper()
		var abort *bacnet.AbortError
		if !errors.As(err, &abort) || abort.Reason != bacnet.AbortReasonOther || abort.Server {
			t.Fatalf("%s: got %T %v, want Abort (other) without the SRV bit", what, err, err)
		}
	}

	sysioRequireReject(t, "ReadProperty without parameters", raw(apdu.ServiceReadProperty, nil),
		bacnet.RejectReasonMissingRequiredParameter)
	// objectIdentifier only.
	sysioRequireReject(t, "ReadProperty without a property", raw(apdu.ServiceReadProperty, []byte{0x0c, 0x02, 0x00, 0x00, 0x01}),
		bacnet.RejectReasonMissingRequiredParameter)
	// objectIdentifier cut short.
	requireAbortOther("truncated WriteProperty", raw(apdu.ServiceWriteProperty, []byte{0x0c, 0x00, 0x80, 0x00, 0x01}))
	requireAbortOther("garbage ReadPropertyMultiple", raw(apdu.ServiceReadPropertyMultiple, []byte{0xff, 0xff}))

	if _, err := c.ReadProperty(ctx, peer.target, deviceObject(dev), bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName}); err != nil {
		t.Fatalf("read after the malformed requests: %v", err)
	}
}

// A reply that needs segmentation to a client that does not accept segmented
// responses is aborted with segmentation-not-supported.
//
// Deviation: the stack encodes its Abort PDUs without the SRV bit (first octet
// 0x70 instead of 0x71), so the client sees an Abort "sent by a client".
func TestSysIOBACnetAbortSegmentationNotSupported(t *testing.T) {
	dev, peer := startSysIO(t, "BACNET_MAX_APDU=50")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 10*time.Second)
	defer cancel()

	av, _ := analogValueObject(dev)
	// AV-1's description is longer than one 50-octet APDU.
	payload, err := service.EncodeReadProperty(service.ReadPropertyRequest{
		Object:   av,
		Property: bacnet.PropertyReference{Identifier: bacnet.PropertyDescription},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.InvokeConfirmed(ctx, peer.target, apdu.ServiceReadProperty, payload, client.ConfirmedInvokeOptions{
		SegmentedResponseAccepted: false,
	})
	var abort *bacnet.AbortError
	if !errors.As(err, &abort) {
		t.Fatalf("got %T %v, want an Abort", err, err)
	}
	if abort.Reason != bacnet.AbortReasonSegmentationNotSupported {
		t.Fatalf("Abort reason %s, want segmentation-not-supported", bacnet.AbortReasonName(abort.Reason))
	}
	if abort.Server {
		t.Fatal("Abort carries the SRV bit: System.IO.BACnet 4.0.0 did not set it; update the deviation note")
	}
}

// ---------------------------------------------------------------------------
// WriteProperty
// ---------------------------------------------------------------------------

func TestSysIOBACnetWritePropertyReadbackReset(t *testing.T) {
	dev, peer := startSysIO(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 15*time.Second)
	defer cancel()

	av, baseline := analogValueObject(dev)
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	prio := uint8(8)
	written := baseline + 2.25
	if err := c.WriteProperty(ctx, peer.target, av, prop, bacnet.RealValue(written), &prio); err != nil {
		t.Fatalf("WriteProperty: %v", err)
	}
	if got := sysioReadReal(t, ctx, c, peer.target, av); math.Abs(float64(got-written)) > 0.01 {
		t.Fatalf("present-value=%v, want %v", got, written)
	}
	if err := c.WriteProperty(ctx, peer.target, av, prop, bacnet.RealValue(baseline), &prio); err != nil {
		t.Fatalf("restore WriteProperty: %v", err)
	}
	if got := sysioReadReal(t, ctx, c, peer.target, av); math.Abs(float64(got-baseline)) > 0.01 {
		t.Fatalf("present-value after restore=%v, want %v", got, baseline)
	}
}

// Refused writes come back as definite Errors and change nothing.
func TestSysIOBACnetWritePropertyErrors(t *testing.T) {
	dev, peer := startSysIO(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 15*time.Second)
	defer cancel()
	av, baseline := analogValueObject(dev)
	prio := uint8(8)

	// Read-only property.
	objType := bacnet.PropertyReference{Identifier: bacnet.PropertyObjectType}
	err := c.WriteProperty(ctx, peer.target, av, objType, bacnet.EnumValue(uint32(bacnet.ObjectTypeBinaryValue)), nil)
	sysioRequireError(t, "write object-type", err, bacnet.ErrorClassProperty, bacnet.ErrorCodeWriteAccessDenied)
	var unknown *bacnet.OutcomeUnknownError
	if errors.As(err, &unknown) {
		t.Fatalf("definite Error reported as outcome-unknown: %v", err)
	}
	val, err := c.ReadProperty(ctx, peer.target, av, objType)
	if err != nil {
		t.Fatal(err)
	}
	if val.Enumerated != uint32(bacnet.ObjectTypeAnalogValue) {
		t.Fatalf("object-type changed to %d", val.Enumerated)
	}

	// The Device Object_Identifier is read-only too (three of the other four
	// peers accept this write).
	err = c.WriteProperty(ctx, peer.target, deviceObject(dev), bacnet.PropertyReference{Identifier: bacnet.PropertyObjectIdentifier},
		bacnet.ApplicationValue{Kind: bacnet.ValueObjectID, ObjectID: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 4321}}, nil)
	sysioRequireError(t, "write device object-identifier", err, bacnet.ErrorClassProperty, bacnet.ErrorCodeWriteAccessDenied)

	pv := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	// Unknown object and unknown property.
	missing := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 4000000}
	err = c.WriteProperty(ctx, peer.target, missing, pv, bacnet.RealValue(1), &prio)
	sysioRequireError(t, "write to a missing object", err, bacnet.ErrorClassObject, bacnet.ErrorCodeUnknownObject)
	err = c.WriteProperty(ctx, peer.target, av, bacnet.PropertyReference{Identifier: unknownPropertyID}, bacnet.RealValue(1), nil)
	sysioRequireError(t, "write to a missing property", err, bacnet.ErrorClassProperty, bacnet.ErrorCodeUnknownProperty)

	// Wrong datatype.
	err = c.WriteProperty(ctx, peer.target, av, pv,
		bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: "hot"}}, &prio)
	sysioRequireError(t, "write a string to present-value", err, bacnet.ErrorClassProperty, bacnet.ErrorCodeInvalidDataType)

	// Priority 6 is reserved for minimum on/off: DeviceStorage refuses it.
	six := uint8(6)
	err = c.WriteProperty(ctx, peer.target, av, pv, bacnet.RealValue(55), &six)
	sysioRequireError(t, "write at priority 6", err, bacnet.ErrorClassProperty, bacnet.ErrorCodeWriteAccessDenied)

	if got := sysioReadReal(t, ctx, c, peer.target, av); math.Abs(float64(got-baseline)) > 0.01 {
		t.Fatalf("present-value = %v after refused writes, want %v", got, baseline)
	}
}

// WritePriority commands a slot of the priority array; RelinquishPriority
// writes NULL to it and the value falls back to Relinquish_Default.
//
// Deviation: DeviceStorage keeps the priority array as text under the NULL
// tag, so a commanded slot reads back as CharacterString "77.5", not REAL
// 77.5. Present_Value itself is a REAL.
func TestSysIOBACnetWriteAndRelinquishPriority(t *testing.T) {
	dev, peer := startSysIO(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()
	av, _ := analogValueObject(dev)
	slot := func(prio uint32) bacnet.ApplicationValue {
		t.Helper()
		v, err := c.ReadProperty(ctx, peer.target, av,
			bacnet.PropertyReference{Identifier: bacnet.PropertyPriorityArray, ArrayIndex: &prio})
		if err != nil {
			t.Fatalf("read priority-array[%d]: %v", prio, err)
		}
		return v
	}

	zero := uint32(0)
	size, err := c.ReadProperty(ctx, peer.target, av, bacnet.PropertyReference{Identifier: bacnet.PropertyPriorityArray, ArrayIndex: &zero})
	if err != nil {
		t.Fatalf("read priority-array[0]: %v", err)
	}
	if n, err := bacnet.AsUnsigned(size); err != nil || n != 16 {
		t.Fatalf("priority-array[0] = %+v (%v), want 16", size, err)
	}

	before := sysioReadReal(t, ctx, c, peer.target, av)
	const prio = 5
	if v := slot(prio); v.Kind != bacnet.ValueNull {
		t.Fatalf("priority-array[%d] = %+v before any write, want NULL", prio, v)
	}
	if err := c.WritePriority(ctx, peer.target, av, prio, bacnet.RealValue(77.5)); err != nil {
		t.Fatalf("WritePriority: %v", err)
	}
	if got := sysioReadReal(t, ctx, c, peer.target, av); math.Abs(float64(got-77.5)) > 0.01 {
		t.Fatalf("present-value after WritePriority = %v, want 77.5", got)
	}
	if v := slot(prio); v.Kind != bacnet.ValueCharacterString || v.Character.Value != "77.5" {
		t.Fatalf("priority-array[%d] = %+v, want CharacterString \"77.5\" (System.IO.BACnet 4.0.0 DeviceStorage); "+
			"a REAL here means upstream fixed it: update the deviation note", prio, v)
	}

	// A lower-priority command does not win.
	if err := c.WritePriority(ctx, peer.target, av, 10, bacnet.RealValue(11)); err != nil {
		t.Fatalf("WritePriority 10: %v", err)
	}
	if got := sysioReadReal(t, ctx, c, peer.target, av); math.Abs(float64(got-77.5)) > 0.01 {
		t.Fatalf("present-value = %v with priorities 5 and 10 commanded, want 77.5", got)
	}
	if err := c.RelinquishPriority(ctx, peer.target, av, prio); err != nil {
		t.Fatalf("RelinquishPriority: %v", err)
	}
	if v := slot(prio); v.Kind != bacnet.ValueNull {
		t.Fatalf("priority-array[%d] after relinquish = %+v, want NULL", prio, v)
	}
	if got := sysioReadReal(t, ctx, c, peer.target, av); math.Abs(float64(got-11)) > 0.01 {
		t.Fatalf("present-value = %v after relinquishing 5, want 11 (priority 10)", got)
	}
	if err := c.RelinquishPriority(ctx, peer.target, av, 10); err != nil {
		t.Fatalf("RelinquishPriority 10: %v", err)
	}
	if got := sysioReadReal(t, ctx, c, peer.target, av); math.Abs(float64(got-before)) > 0.01 {
		t.Fatalf("present-value after relinquish = %v, want %v again", got, before)
	}

	// A write without a priority lands in slot 16.
	if err := c.WriteProperty(ctx, peer.target, av, bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}, bacnet.RealValue(33), nil); err != nil {
		t.Fatalf("WriteProperty without priority: %v", err)
	}
	if v := slot(16); v.Kind == bacnet.ValueNull {
		t.Fatal("priority-array[16] is NULL after a write without priority")
	}
	if got := sysioReadReal(t, ctx, c, peer.target, av); math.Abs(float64(got-33)) > 0.01 {
		t.Fatalf("present-value = %v after a write without priority, want 33", got)
	}
}

// ---------------------------------------------------------------------------
// WritePropertyMultiple
// ---------------------------------------------------------------------------

func TestSysIOBACnetWritePropertyMultipleReadbackReset(t *testing.T) {
	dev, peer := startSysIO(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 15*time.Second)
	defer cancel()

	av, baseline := analogValueObject(dev)
	pv := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	desc := bacnet.PropertyReference{Identifier: bacnet.PropertyDescription}
	prio := uint8(8)
	written := baseline + 2.5

	write := func(v float32, text string) error {
		return c.WritePropertyMultiple(ctx, peer.target, []service.WriteAccessSpecification{{
			Object: av,
			Properties: []service.WritePropertyValue{
				{Property: pv, Value: bacnet.RealValue(v), Priority: &prio},
				{Property: desc, Value: bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: text}}},
			},
		}})
	}
	if err := write(written, "written by WPM"); err != nil {
		t.Fatalf("WritePropertyMultiple: %v", err)
	}
	if got := sysioReadReal(t, ctx, c, peer.target, av); math.Abs(float64(got-written)) > 0.01 {
		t.Fatalf("after WPM present-value=%v, want %v", got, written)
	}
	v, err := c.ReadProperty(ctx, peer.target, av, desc)
	if err != nil {
		t.Fatal(err)
	}
	if got := characterString(t, v); got != "written by WPM" {
		t.Fatalf("after WPM description=%q", got)
	}
	if err := write(baseline, "restored"); err != nil {
		t.Fatalf("WritePropertyMultiple restore: %v", err)
	}
}

// A WritePropertyMultiple whose second write is refused: the first write is
// applied and the request ends with an Error PDU.
//
// Deviation: System.IO.BACnet has no WritePropertyMultiple-Error encoder. Its
// Error PDU for service 16 carries the bare error-class and error-code (two
// application-tagged enumerations), without the [0] errorType wrapper and
// without [1] firstFailedWriteAttempt. go-bacnet reads that plain body as the
// device's *ErrorResponse; there is no first-failed reference to report, so it
// is not a *WritePropertyMultipleError.
func TestSysIOBACnetWritePropertyMultipleError(t *testing.T) {
	dev, peer := startSysIO(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 15*time.Second)
	defer cancel()

	av, baseline := analogValueObject(dev)
	pv := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	prio := uint8(8)
	written := baseline + 4
	specs := []service.WriteAccessSpecification{{
		Object: av,
		Properties: []service.WritePropertyValue{
			{Property: pv, Value: bacnet.RealValue(written), Priority: &prio},
			{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyObjectType}, Value: bacnet.EnumValue(5)},
		},
	}}
	err := c.WritePropertyMultiple(ctx, peer.target, specs)
	er := sysioRequireError(t, "WritePropertyMultiple with a read-only property", err,
		bacnet.ErrorClassProperty, bacnet.ErrorCodeWriteAccessDenied)
	if er.Service != apdu.ServiceWritePropertyMultiple {
		t.Fatalf("Error service %d, want WritePropertyMultiple (%d)", er.Service, apdu.ServiceWritePropertyMultiple)
	}
	var unknown *bacnet.OutcomeUnknownError
	if errors.As(err, &unknown) {
		t.Fatalf("a remote Error PDU reported as outcome-unknown: %v", err)
	}
	var wpmErr *service.WritePropertyMultipleError
	if errors.As(err, &wpmErr) {
		t.Fatalf("a plain Error body reported as WritePropertyMultiple-Error with first-failed %v: the peer sent none", wpmErr.FirstFailed)
	}
	if got := sysioReadReal(t, ctx, c, peer.target, av); math.Abs(float64(got-written)) > 0.01 {
		t.Fatalf("present-value = %v, want %v: the write before the failed one is applied", got, written)
	}

	// The Error PDU on the wire: service 16, error-class property(2),
	// error-code write-access-denied(40).
	payload, err := service.EncodeWritePropertyMultiple(specs)
	if err != nil {
		t.Fatal(err)
	}
	pdu, err := c.InvokeConfirmed(ctx, peer.target, apdu.ServiceWritePropertyMultiple, payload, client.ConfirmedInvokeOptions{})
	if pdu.Error == nil {
		t.Fatalf("raw WritePropertyMultiple: %T %v, want an Error PDU", err, err)
	}
	if want := []byte{0x91, 0x02, 0x91, 0x28}; pdu.Error.ServiceChoice != apdu.ServiceWritePropertyMultiple || !bytes.Equal(pdu.Error.Payload, want) {
		t.Fatalf("Error PDU service %d payload %x, want service %d payload %x",
			pdu.Error.ServiceChoice, pdu.Error.Payload, apdu.ServiceWritePropertyMultiple, want)
	}
}

// ---------------------------------------------------------------------------
// COV
// ---------------------------------------------------------------------------

func sysioRunCOV(t *testing.T, confirmed bool) {
	dev, peer := startSysIO(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 30*time.Second)
	defer cancel()

	av, baseline := analogValueObject(dev)
	pv := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	sub, err := c.SubscribeCOV(ctx, peer.target, av, client.COVOptions{
		Lifetime:       60,
		IssueConfirmed: confirmed,
		BufferSize:     16,
	})
	if err != nil {
		t.Fatalf("SubscribeCOV: %v", err)
	}
	defer func() { _ = sub.Close() }()

	select {
	case ev := <-sub.Events():
		if ev.State != client.SubscriptionActive {
			t.Fatalf("first event state=%v, want Active", ev.State)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for SubscriptionActive")
	}

	// The subscription is answered with an initial notification of the
	// current value.
	initial := sysioWaitPresentValue(t, sub, baseline, 8*time.Second)
	if initial.MonitoredObject != av || initial.InitiatingDevice != deviceObject(dev) {
		t.Fatalf("initial notification from %v for %v", initial.InitiatingDevice, initial.MonitoredObject)
	}

	prio := uint8(8)
	for i, written := range []float32{baseline + 3.5, baseline + 7} {
		if err := c.WriteProperty(ctx, peer.target, av, pv, bacnet.RealValue(written), &prio); err != nil {
			t.Fatalf("WriteProperty %d to trigger COV: %v", i, err)
		}
		note := sysioWaitPresentValue(t, sub, written, 8*time.Second)
		if note.MonitoredObject != av {
			t.Fatalf("COV object=%v, want %v", note.MonitoredObject, av)
		}
		if note.TimeRemaining == 0 || note.TimeRemaining > 60 {
			t.Fatalf("time remaining %d, want within the 60 s lifetime", note.TimeRemaining)
		}
		var sawFlags bool
		for _, v := range note.Values {
			if v.Property.Identifier == bacnet.PropertyStatusFlags && v.Value.Kind == bacnet.ValueBitString {
				sawFlags = true
			}
		}
		if !sawFlags || len(note.Values) != 2 {
			t.Fatalf("notification values %+v, want present-value and status-flags", note.Values)
		}
	}

	if err := sub.Close(); err != nil {
		t.Fatalf("Subscription.Close (cancel): %v", err)
	}
	// The cancelled subscription leaves the peer responsive: with confirmed
	// notifications a peer still waiting for an acknowledgement would stall.
	if err := c.WriteProperty(ctx, peer.target, av, pv, bacnet.RealValue(baseline), &prio); err != nil {
		t.Fatalf("WriteProperty after cancel: %v", err)
	}
	if got := sysioReadReal(t, ctx, c, peer.target, av); math.Abs(float64(got-baseline)) > 0.01 {
		t.Fatalf("present-value = %v after cancel, want %v", got, baseline)
	}
}

func TestSysIOBACnetCOVSubscribeNotifyCancel(t *testing.T) { sysioRunCOV(t, false) }

func TestSysIOBACnetCOVConfirmedNotifications(t *testing.T) { sysioRunCOV(t, true) }

func TestSysIOBACnetSubscribeCOVProperty(t *testing.T) {
	dev, peer := startSysIO(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()
	av, baseline := analogValueObject(dev)
	pv := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	increment := float32(1)
	sub, err := c.SubscribeCOVProperty(ctx, peer.target, av, pv, client.COVOptions{Lifetime: 60, BufferSize: 8}, &increment)
	if err != nil {
		t.Fatalf("SubscribeCOVProperty: %v", err)
	}
	defer func() { _ = sub.Close() }()

	prio := uint8(8)
	written := baseline + 5
	if err := c.WriteProperty(ctx, peer.target, av, pv, bacnet.RealValue(written), &prio); err != nil {
		t.Fatalf("WriteProperty: %v", err)
	}
	note := sysioWaitPresentValue(t, sub, written, 8*time.Second)
	if note.MonitoredObject != av {
		t.Fatalf("notification for %v, want %v", note.MonitoredObject, av)
	}
	// A write to another property of the object is not notified.
	if err := c.WriteProperty(ctx, peer.target, av, bacnet.PropertyReference{Identifier: bacnet.PropertyDescription},
		bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: "not monitored"}}, nil); err != nil {
		t.Fatalf("WriteProperty description: %v", err)
	}
	select {
	case ev := <-sub.Events():
		if ev.Notification != nil {
			t.Fatalf("notification after a write to an unmonitored property: %+v", ev.Notification)
		}
	case <-time.After(1500 * time.Millisecond):
	}
	if err := sub.Close(); err != nil {
		t.Fatalf("cancel subscription: %v", err)
	}

	// A property the object does not have cannot be subscribed.
	_, err = c.SubscribeCOVProperty(ctx, peer.target, av, bacnet.PropertyReference{Identifier: unknownPropertyID},
		client.COVOptions{Lifetime: 60}, nil)
	sysioRequireError(t, "SubscribeCOVProperty of a missing property", err, bacnet.ErrorClassProperty, bacnet.ErrorCodeUnknownProperty)
}

// A subscription outlives its lifetime: the client renews at half of it and
// notifications still arrive after the original lifetime has passed.
func TestSysIOBACnetCOVRenewKeepsSubscriptionAlive(t *testing.T) {
	dev, peer := startSysIO(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 40*time.Second)
	defer cancel()
	av, baseline := analogValueObject(dev)
	pv := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	const lifetime = 4 * time.Second
	sub, err := c.SubscribeCOV(ctx, peer.target, av, client.COVOptions{Lifetime: uint32(lifetime / time.Second), BufferSize: 32})
	if err != nil {
		t.Fatalf("SubscribeCOV: %v", err)
	}
	defer func() { _ = sub.Close() }()
	start := time.Now()

	renewals := 0
	deadline := time.After(3 * lifetime)
	for renewals < 2 {
		select {
		case <-deadline:
			t.Fatalf("saw %d renewals in %v", renewals, 3*lifetime)
		case ev, ok := <-sub.Events():
			if !ok {
				t.Fatal("subscription closed")
			}
			switch ev.State {
			case client.SubscriptionRenewing:
				renewals++
			case client.SubscriptionDegraded, client.SubscriptionExpired, client.SubscriptionClosed:
				t.Fatalf("subscription went %v: %v", ev.State, ev.Err)
			}
		}
	}
	if wait := lifetime + time.Second - time.Since(start); wait > 0 {
		time.Sleep(wait)
	}

	prio := uint8(8)
	written := baseline + 7
	if err := c.WriteProperty(ctx, peer.target, av, pv, bacnet.RealValue(written), &prio); err != nil {
		t.Fatalf("WriteProperty: %v", err)
	}
	sysioWaitPresentValue(t, sub, written, 8*time.Second)
}

// Subscriptions the adapter refuses.
func TestSysIOBACnetSubscribeCOVErrors(t *testing.T) {
	dev, peer := startSysIO(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 15*time.Second)
	defer cancel()

	missing := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 4000000}
	_, err := c.SubscribeCOV(ctx, peer.target, missing, client.COVOptions{Lifetime: 60})
	sysioRequireError(t, "SubscribeCOV of a missing object", err, bacnet.ErrorClassObject, bacnet.ErrorCodeUnknownObject)

	// The Device object does not report COV.
	_, err = c.SubscribeCOV(ctx, peer.target, deviceObject(dev), client.COVOptions{Lifetime: 60})
	sysioRequireError(t, "SubscribeCOV of the device object", err,
		bacnet.ErrorClassObject, bacnet.ErrorCodeOptionalFunctionalityNotSupported)
}

// ---------------------------------------------------------------------------
// ReadRange (Trend Log)
// ---------------------------------------------------------------------------

func TestSysIOBACnetReadRange(t *testing.T) {
	dev, peer := startSysIO(t)
	if peer.assertedByReexec {
		return
	}
	tl, ok := trendLogObject(dev)
	if !ok {
		failOrSkip(t, "device fixture has no trend-log (need device-baseline-v2)")
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()

	v, err := c.ReadProperty(ctx, peer.target, tl, bacnet.PropertyReference{Identifier: sysioPropertyRecordCount})
	if err != nil {
		t.Fatalf("ReadProperty record-count: %v", err)
	}
	if n, err := bacnet.AsUnsigned(v); err != nil || n != sysioLogRecords {
		t.Fatalf("record-count = %+v (%v), want %d", v, err, sysioLogRecords)
	}

	read := func(what string, by service.ReadRangeBy, ref uint32, count int32) service.ReadRangeACK {
		t.Helper()
		ack, err := c.ReadRange(ctx, peer.target, service.ReadRangeRequest{
			Object:         tl,
			Property:       bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer},
			By:             by,
			ReferenceIndex: ref,
			Count:          count,
		})
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		if ack.Object != tl || ack.Property.Identifier != bacnet.PropertyLogBuffer {
			t.Fatalf("%s: ACK for %v property %v", what, ack.Object, ack.Property.Identifier)
		}
		return ack
	}

	sysioRequireRange(t, "position 1 count 2", read("position 1 count 2", service.ReadRangeByPosition, 1, 2), 2, 1, true, false)
	sysioRequireRange(t, "position 2 count 2", read("position 2 count 2", service.ReadRangeByPosition, 2, 2), 2, 2, false, false)
	sysioRequireRange(t, "position 3 count 100", read("position 3 count 100", service.ReadRangeByPosition, 3, 100), 2, 3, false, true)
	sysioRequireRange(t, "position 1 count 100", read("position 1 count 100", service.ReadRangeByPosition, 1, 100), sysioLogRecords, 1, true, true)
	// A negative count reads backwards from the reference; records stay in log order.
	sysioRequireRange(t, "position 4 count -2", read("position 4 count -2", service.ReadRangeByPosition, 4, -2), 2, 3, false, true)
	sysioRequireRange(t, "position 2 count -5", read("position 2 count -5", service.ReadRangeByPosition, 2, -5), 2, 1, true, false)
	// A reference outside the log selects nothing.
	sysioRequireRange(t, "position 9 count 2", read("position 9 count 2", service.ReadRangeByPosition, 9, 2), 0, 0, false, false)
	// No range: the whole log.
	sysioRequireRange(t, "whole log", read("whole log", service.ReadRangeAll, 0, 0), sysioLogRecords, 1, true, true)

	// By sequence number: firstSequenceNumber is reported, and equals the position here.
	seq := read("sequence 2 count 2", service.ReadRangeBySequenceNumber, 2, 2)
	sysioRequireRange(t, "sequence 2 count 2", seq, 2, 2, false, false)
	if seq.FirstSequence == nil || *seq.FirstSequence != 2 {
		t.Fatalf("sequence 2 count 2: firstSequenceNumber %v, want 2", seq.FirstSequence)
	}
	if pos := read("position 2 count 2", service.ReadRangeByPosition, 2, 2); pos.FirstSequence != nil {
		t.Fatalf("by position: firstSequenceNumber %d present, want it absent", *pos.FirstSequence)
	}

	// By time: the records after the reference time.
	byTime, err := c.ReadRange(ctx, peer.target, service.ReadRangeRequest{
		Object:        tl,
		Property:      bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer},
		By:            service.ReadRangeByTime,
		ReferenceTime: sysioLogTime(2),
		Count:         5,
	})
	if err != nil {
		t.Fatalf("by time: %v", err)
	}
	sysioRequireRange(t, "after record 2", byTime, 2, 3, false, true)
	if byTime.FirstSequence == nil || *byTime.FirstSequence != 3 {
		t.Fatalf("by time: firstSequenceNumber %v, want 3", byTime.FirstSequence)
	}

	// Not a log buffer, and a missing object.
	_, err = c.ReadRange(ctx, peer.target, service.ReadRangeRequest{
		Object: tl, Property: bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName},
		By: service.ReadRangeByPosition, ReferenceIndex: 1, Count: 1,
	})
	sysioRequireError(t, "ReadRange of object-name", err, bacnet.ErrorClassServices, bacnet.ErrorCodePropertyIsNotAList)
	_, err = c.ReadRange(ctx, peer.target, service.ReadRangeRequest{
		Object:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeTrendLog, Instance: 4000000},
		Property: bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer},
		By:       service.ReadRangeByPosition, ReferenceIndex: 1, Count: 1,
	})
	sysioRequireError(t, "ReadRange of a missing log", err, bacnet.ErrorClassObject, bacnet.ErrorCodeUnknownObject)
}

// ReadRangeAll pages through the log one record at a time. A page that
// returns the one record it asked for has MORE_ITEMS clear, which must not
// end the paging; LAST_ITEM on the fourth page does.
func TestSysIOBACnetReadRangeAllPager(t *testing.T) {
	dev, peer := startSysIO(t)
	if peer.assertedByReexec {
		return
	}
	tl, ok := trendLogObject(dev)
	if !ok {
		failOrSkip(t, "device fixture has no trend-log")
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 30*time.Second)
	defer cancel()
	req := service.ReadRangeRequest{
		Object:         tl,
		Property:       bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer},
		By:             service.ReadRangeByPosition,
		ReferenceIndex: 1,
		Count:          1,
	}
	all, err := c.ReadRangeAll(ctx, peer.target, req, client.ReadRangePageOptions{PageCount: 1})
	if err != nil {
		t.Fatalf("ReadRangeAll: %v (stopped: %q after %d pages)", err, all.StoppedReason, len(all.Pages))
	}
	if len(all.Pages) != sysioLogRecords || len(all.LogRecords) != sysioLogRecords || all.Partial {
		t.Fatalf("paged %d records in %d pages (partial=%v %q), want %d records in as many pages",
			len(all.LogRecords), len(all.Pages), all.Partial, all.StoppedReason, sysioLogRecords)
	}
	for i, rec := range all.LogRecords {
		if rec.Timestamp != sysioLogTime(i+1) || rec.Datum.Real != float32(20+i) {
			t.Fatalf("paged record %d = %+v", i, rec)
		}
	}
	// A bound below the log size ends the paging as partial.
	two, err := c.ReadRangeAll(ctx, peer.target, req, client.ReadRangePageOptions{PageCount: 1, MaxItems: 2})
	if err != nil {
		t.Fatalf("ReadRangeAll bounded: %v", err)
	}
	if len(two.LogRecords) != 2 || len(two.Pages) != 2 {
		t.Fatalf("bounded paging: %d records in %d pages (%q), want 2 in 2", len(two.LogRecords), len(two.Pages), two.StoppedReason)
	}
}

// ---------------------------------------------------------------------------
// AtomicReadFile / AtomicWriteFile (device-baseline-v4)
// ---------------------------------------------------------------------------

const sysioStreamFileContent = "hello-world-stream-file-data"

func TestSysIOBACnetAtomicReadFileStream(t *testing.T) {
	_, peer := startSysIOFixture(t, "device-baseline-v4")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 15*time.Second)
	defer cancel()
	file := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 1}

	ack, err := c.AtomicReadFile(ctx, peer.target, service.AtomicReadFileRequest{
		File: file, Access: service.FileAccessStream, StartPosition: 0, Count: 64,
	})
	if err != nil {
		t.Fatalf("AtomicReadFile: %v", err)
	}
	if string(ack.Data) != sysioStreamFileContent || !ack.EndOfFile || ack.StartPosition != 0 {
		t.Fatalf("data=%q eof=%v start=%d, want the whole file with EOF", ack.Data, ack.EndOfFile, ack.StartPosition)
	}
	// A read inside the file: no EOF.
	ack, err = c.AtomicReadFile(ctx, peer.target, service.AtomicReadFileRequest{
		File: file, Access: service.FileAccessStream, StartPosition: 6, Count: 5,
	})
	if err != nil {
		t.Fatalf("AtomicReadFile 6+5: %v", err)
	}
	if string(ack.Data) != "world" || ack.EndOfFile || ack.StartPosition != 6 {
		t.Fatalf("data=%q eof=%v start=%d, want \"world\" at 6 without EOF", ack.Data, ack.EndOfFile, ack.StartPosition)
	}
	// File_Size follows the content.
	v, err := c.ReadProperty(ctx, peer.target, file, bacnet.PropertyReference{Identifier: sysioPropertyFileSize})
	if err != nil {
		t.Fatalf("ReadProperty file-size: %v", err)
	}
	if n, err := bacnet.AsUnsigned(v); err != nil || int(n) != len(sysioStreamFileContent) {
		t.Fatalf("file-size = %+v (%v), want %d", v, err, len(sysioStreamFileContent))
	}

	// A start position past the end.
	_, err = c.AtomicReadFile(ctx, peer.target, service.AtomicReadFileRequest{
		File: file, Access: service.FileAccessStream, StartPosition: 1000, Count: 4,
	})
	sysioRequireError(t, "read past the end", err, bacnet.ErrorClassServices, bacnet.ErrorCodeInvalidFileStartPosition)
	// Record access to the stream file.
	_, err = c.AtomicReadFile(ctx, peer.target, service.AtomicReadFileRequest{
		File: file, Access: service.FileAccessRecord, StartPosition: 0, Count: 1,
	})
	sysioRequireError(t, "record access to a stream file", err, bacnet.ErrorClassServices, bacnet.ErrorCodeInvalidFileAccessMethod)
	// Stream access to the record-access file.
	_, err = c.AtomicReadFile(ctx, peer.target, service.AtomicReadFileRequest{
		File: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 2}, Access: service.FileAccessStream, StartPosition: 0, Count: 4,
	})
	sysioRequireError(t, "stream access to a record file", err, bacnet.ErrorClassServices, bacnet.ErrorCodeInvalidFileAccessMethod)
	_, err = c.AtomicReadFile(ctx, peer.target, service.AtomicReadFileRequest{
		File: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 9}, Access: service.FileAccessStream, StartPosition: 0, Count: 4,
	})
	sysioRequireError(t, "a missing file", err, bacnet.ErrorClassObject, bacnet.ErrorCodeUnknownObject)
}

func TestSysIOBACnetAtomicWriteFileStreamReadback(t *testing.T) {
	_, peer := startSysIOFixture(t, "device-baseline-v4")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 15*time.Second)
	defer cancel()

	file := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 1}
	payload := []byte("WRITEBACK")
	ack, err := c.AtomicWriteFile(ctx, peer.target, service.AtomicWriteFileRequest{
		File: file, Access: service.FileAccessStream, StartPosition: 0, Data: payload,
	})
	if err != nil {
		t.Fatalf("AtomicWriteFile: %v", err)
	}
	if ack.StartPosition != 0 {
		t.Fatalf("write ack start=%d", ack.StartPosition)
	}
	read, err := c.AtomicReadFile(ctx, peer.target, service.AtomicReadFileRequest{
		File: file, Access: service.FileAccessStream, StartPosition: 0, Count: 64,
	})
	if err != nil {
		t.Fatalf("AtomicReadFile after write: %v", err)
	}
	want := string(payload) + sysioStreamFileContent[len(payload):]
	if string(read.Data) != want {
		t.Fatalf("readback=%q want=%q (overwrite in place)", read.Data, want)
	}

	// Start position -1 appends and the ACK reports where.
	ack, err = c.AtomicWriteFile(ctx, peer.target, service.AtomicWriteFileRequest{
		File: file, Access: service.FileAccessStream, StartPosition: -1, Data: []byte("+tail"),
	})
	if err != nil {
		t.Fatalf("AtomicWriteFile append: %v", err)
	}
	if int(ack.StartPosition) != len(want) {
		t.Fatalf("append ack start=%d, want %d", ack.StartPosition, len(want))
	}
	read, err = c.AtomicReadFile(ctx, peer.target, service.AtomicReadFileRequest{
		File: file, Access: service.FileAccessStream, StartPosition: int32(len(want)), Count: 64,
	})
	if err != nil {
		t.Fatalf("AtomicReadFile of the appended tail: %v", err)
	}
	if string(read.Data) != "+tail" || !read.EndOfFile {
		t.Fatalf("tail=%q eof=%v", read.Data, read.EndOfFile)
	}
}

// ReadFileStream and WriteFileStream move a file in chunks far smaller than
// the file, so every chunk boundary and the end-of-file flag are exercised.
func TestSysIOBACnetFileStreamHelpers(t *testing.T) {
	_, peer := startSysIOFixture(t, "device-baseline-v4")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 30*time.Second)
	defer cancel()
	file := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 1}

	got, err := c.ReadFileStream(ctx, peer.target, file, client.FileReadOptions{ChunkSize: 7})
	if err != nil {
		t.Fatalf("ReadFileStream: %v", err)
	}
	if !bytes.Equal(got, []byte(sysioStreamFileContent)) {
		t.Fatalf("ReadFileStream = %q, want %q", got, sysioStreamFileContent)
	}

	payload := []byte("the quick brown fox jumps over the lazy dog 0123456789")
	outcomes, err := c.WriteFileStream(ctx, peer.target, file, 0, payload, client.FileWriteOptions{ChunkSize: 10})
	if err != nil {
		t.Fatalf("WriteFileStream: %v (outcomes %+v)", err, outcomes)
	}
	if len(outcomes) != 6 { // 54 octets in chunks of 10
		t.Fatalf("%d chunks written, want 6: %+v", len(outcomes), outcomes)
	}
	for i, o := range outcomes {
		if o.Err != nil || o.StartPosition != int32(i*10) {
			t.Fatalf("chunk %d: %+v", i, o)
		}
	}
	back, err := c.ReadFileStream(ctx, peer.target, file, client.FileReadOptions{ChunkSize: 16})
	if err != nil {
		t.Fatalf("ReadFileStream after write: %v", err)
	}
	if !bytes.Equal(back, payload) {
		t.Fatalf("readback %q, want exactly what was written %q", back, payload)
	}

	// The whole file in one AtomicReadFile-ACK to a client that accepts 50
	// octets: the stack segments it.
	small := newClientWithAdvertisedMaxAPDU(t, 50)
	ack, err := small.AtomicReadFile(ctx, peer.target, service.AtomicReadFileRequest{
		File: file, Access: service.FileAccessStream, StartPosition: 0, Count: uint32(len(payload)),
	})
	if err != nil {
		t.Fatalf("segmented AtomicReadFile: %v", err)
	}
	if !bytes.Equal(ack.Data, payload) || !ack.EndOfFile {
		t.Fatalf("segmented AtomicReadFile data=%q eof=%v, want %q with EOF", ack.Data, ack.EndOfFile, payload)
	}
}

// ---------------------------------------------------------------------------
// Segmentation
// ---------------------------------------------------------------------------

// The peer segments a ComplexACK that does not fit the Max APDU the client
// advertises (50 octets); the client reassembles it.
func TestSysIOBACnetSegmentedReadPropertyMultiple(t *testing.T) {
	dev, peer := startSysIO(t, "BACNET_MAX_APDU=50")
	if peer.assertedByReexec {
		return
	}
	c := newClientWithAdvertisedMaxAPDU(t, 50)
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()

	av, wantPV := analogValueObject(dev)
	results, err := c.ReadPropertyMultiple(ctx, peer.target, []service.ReadAccessSpecification{
		{
			Object: deviceObject(dev),
			Properties: []bacnet.PropertyReference{
				{Identifier: bacnet.PropertyObjectName},
				{Identifier: bacnet.PropertyObjectIdentifier},
				{Identifier: bacnet.PropertyObjectType},
				{Identifier: bacnet.PropertyVendorIdentifier},
				{Identifier: bacnet.PropertyModelName},
				{Identifier: bacnet.PropertyDescription},
				{Identifier: bacnet.PropertySystemStatus},
				{Identifier: bacnet.PropertyProtocolVersion},
				{Identifier: bacnet.PropertyProtocolRevision},
				{Identifier: bacnet.PropertyMaxAPDULength},
				{Identifier: bacnet.PropertySegmentation},
			},
		},
		{
			Object: av,
			Properties: []bacnet.PropertyReference{
				{Identifier: bacnet.PropertyObjectName},
				{Identifier: bacnet.PropertyPresentValue},
				{Identifier: bacnet.PropertyDescription},
				{Identifier: bacnet.PropertyStatusFlags},
				{Identifier: bacnet.PropertyUnits},
			},
		},
	})
	if err != nil {
		t.Fatalf("segmented ReadPropertyMultiple: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("objects=%d, want 2", len(results))
	}
	if got := characterString(t, findPropertyValue(t, results[0], bacnet.PropertyObjectName)); got != dev.DeviceName {
		t.Fatalf("object-name=%q, want %q", got, dev.DeviceName)
	}
	maxAPDU, err := bacnet.AsUnsigned(findPropertyValue(t, results[0], bacnet.PropertyMaxAPDULength))
	if err != nil || maxAPDU != 50 {
		t.Fatalf("max-apdu-length-accepted = %d (%v), want 50", maxAPDU, err)
	}
	pv, err := bacnet.AsReal(findPropertyValue(t, results[1], bacnet.PropertyPresentValue))
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(float64(pv-wantPV)) > 0.01 {
		t.Fatalf("present-value=%v, want %v", pv, wantPV)
	}
	if got := characterString(t, findPropertyValue(t, results[1], bacnet.PropertyDescription)); len(got) < 50 {
		t.Fatalf("AV description %q is shorter than one APDU: the reply was not forced to segment", got)
	}
}

// The peer also segments when only the client is small: its own Max APDU is
// the default, the client advertises 50.
func TestSysIOBACnetSegmentedReadPropertyToSmallClient(t *testing.T) {
	dev, peer := startSysIO(t)
	if peer.assertedByReexec {
		return
	}
	c := newClientWithAdvertisedMaxAPDU(t, 50)
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()

	av, _ := analogValueObject(dev)
	v, err := c.ReadProperty(ctx, peer.target, av, bacnet.PropertyReference{Identifier: bacnet.PropertyDescription})
	if err != nil {
		t.Fatalf("segmented ReadProperty: %v", err)
	}
	want := ""
	for _, o := range dev.Objects {
		if o.Type == "analog-value" {
			want = o.Description
		}
	}
	if got := characterString(t, v); got != want || len(got) < 50 {
		t.Fatalf("description = %q, want the %d-octet fixture description", got, len(want))
	}
}

// A reply of about 650 octets to a client that accepts 50: more segments than
// the window of 10 the stack proposes, so the transfer spans several windows
// and SegmentACKs. A segmented ReadRange-ACK is reassembled the same way.
func TestSysIOBACnetSegmentedManySegments(t *testing.T) {
	dev, peer := startSysIO(t)
	if peer.assertedByReexec {
		return
	}
	c := newClientWithAdvertisedMaxAPDU(t, 50)
	ctx, cancel := withTimeout(t, 30*time.Second)
	defer cancel()

	av, wantPV := analogValueObject(dev)
	bv := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeBinaryValue, Instance: 1}
	tl, ok := trendLogObject(dev)
	if !ok {
		failOrSkip(t, "device fixture has no trend-log")
	}
	var specs []service.ReadAccessSpecification
	for _, o := range []bacnet.ObjectIdentifier{av, bv, tl} {
		specs = append(specs, service.ReadAccessSpecification{Object: o, Properties: []bacnet.PropertyReference{{Identifier: bacnet.PropertyAll}}})
	}
	specs = append(specs, service.ReadAccessSpecification{Object: deviceObject(dev), Properties: []bacnet.PropertyReference{
		{Identifier: sysioPropertyServicesSupported},
		{Identifier: sysioPropertyObjectTypesSupported},
		{Identifier: bacnet.PropertyObjectList},
		{Identifier: bacnet.PropertyModelName},
		{Identifier: bacnet.PropertyVendorName},
		{Identifier: bacnet.PropertyObjectName},
	}})
	results, err := c.ReadPropertyMultiple(ctx, peer.target, specs)
	if err != nil {
		t.Fatalf("segmented ReadPropertyMultiple: %v", err)
	}
	if len(results) != len(specs) {
		t.Fatalf("%d results, want %d", len(results), len(specs))
	}
	total := 0
	for i, r := range results {
		if r.Object != specs[i].Object {
			t.Fatalf("result %d is for %v, want %v", i, r.Object, specs[i].Object)
		}
		for _, p := range r.Properties {
			if p.Err != nil {
				t.Fatalf("%v property %v: %v", r.Object, p.Property.Identifier, p.Err)
			}
		}
		total += len(r.Properties)
	}
	if total < 35 {
		t.Fatalf("%d properties in the reply: too few to need more than one window of segments", total)
	}
	pv, err := bacnet.AsReal(findPropertyValue(t, results[0], bacnet.PropertyPresentValue))
	if err != nil || math.Abs(float64(pv-wantPV)) > 0.01 {
		t.Fatalf("present-value = %v (%v), want %v", pv, err, wantPV)
	}
	if got := characterString(t, findPropertyValue(t, results[3], bacnet.PropertyObjectName)); got != dev.DeviceName {
		t.Fatalf("device object-name = %q, want %q: the last property of the last segment", got, dev.DeviceName)
	}
	list := findPropertyValue(t, results[3], bacnet.PropertyObjectList)
	if list.Kind != bacnet.ValueConstructed || len(list.Elements) != len(dev.Objects) {
		t.Fatalf("object-list = %+v, want %d identifiers", list, len(dev.Objects))
	}

	ack, err := c.ReadRange(ctx, peer.target, service.ReadRangeRequest{
		Object:         tl,
		Property:       bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer},
		By:             service.ReadRangeByPosition,
		ReferenceIndex: 1,
		Count:          sysioLogRecords,
	})
	if err != nil {
		t.Fatalf("segmented ReadRange: %v", err)
	}
	sysioRequireRange(t, "segmented ReadRange", ack, sysioLogRecords, 1, true, true)
}

// The client sends a segmented confirmed request (it claims a 50-octet remote
// Max APDU); the stack reassembles it and executes the WritePropertyMultiple.
func TestSysIOBACnetSegmentedWritePropertyMultipleSend(t *testing.T) {
	dev, peer := startSysIO(t)
	if peer.assertedByReexec {
		return
	}
	c := newDiscoveryClient(t)

	low, high := dev.DeviceInstance, dev.DeviceInstance
	if err := c.SendWhoIs(context.Background(), peer.endpoint, false, client.DiscoveryOptions{
		LowLimit: &low, HighLimit: &high,
	}); err != nil {
		t.Fatalf("SendWhoIs: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var caps client.DeviceCapabilities
	found := false
	for time.Now().Before(deadline) && !found {
		for _, obs := range c.Devices() {
			if obs.Instance == dev.DeviceInstance {
				caps, found = obs.Capabilities, true
			}
		}
		if !found {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if !found {
		t.Fatalf("no I-Am for device %d", dev.DeviceInstance)
	}
	if !caps.Segmentation.Known || caps.Segmentation.Value != 0 {
		t.Fatalf("I-Am Segmentation = %+v, want segmented-both(0)", caps.Segmentation)
	}

	av, _ := analogValueObject(dev)
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyDescription}
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()
	baselineVal, err := c.ReadProperty(ctx, peer.target, av, prop)
	if err != nil {
		t.Fatalf("ReadProperty Description: %v", err)
	}
	baseline := characterString(t, baselineVal)

	target := peer.target
	target.MaxAPDU = 50
	long := strings.Repeat("x", 80) + "-end"
	text := func(s string) []service.WriteAccessSpecification {
		return []service.WriteAccessSpecification{{
			Object: av,
			Properties: []service.WritePropertyValue{{
				Property: prop,
				Value:    bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: s}},
			}},
		}}
	}
	if err := c.WritePropertyMultiple(ctx, target, text(long)); err != nil {
		t.Fatalf("segmented WritePropertyMultiple: %v", err)
	}
	got, err := c.ReadProperty(ctx, peer.target, av, prop)
	if err != nil {
		t.Fatalf("readback: %v", err)
	}
	if characterString(t, got) != long {
		t.Fatalf("description after the segmented write = %q, want %q", characterString(t, got), long)
	}
	if err := c.WritePropertyMultiple(ctx, peer.target, text(baseline)); err != nil {
		t.Fatalf("restore Description: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Device management, time, fixtures
// ---------------------------------------------------------------------------

// ReinitializeDevice is acknowledged by the adapter without a restart; the
// adapter records it as an operation line.
func TestSysIOBACnetReinitializeDeviceWarmstart(t *testing.T) {
	dev, peer := startSysIO(t)
	if peer.assertedByReexec {
		return
	}
	c := newClientOpts(t, client.WithDeviceManagementEnabled(client.DeviceManagementConfirm))
	ctx, cancel := withTimeout(t, 10*time.Second)
	defer cancel()

	if err := c.ReinitializeDevice(ctx, peer.target, service.ReinitializeDeviceRequest{
		State: service.ReinitializedWarmstart,
	}); err != nil {
		t.Fatalf("ReinitializeDevice warmstart: %v", err)
	}
	awaitOperation(t, peer, "reinitialize-device", 5*time.Second)
	if _, err := c.ReadProperty(ctx, peer.target, deviceObject(dev), bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName}); err != nil {
		t.Fatalf("read after ReinitializeDevice: %v", err)
	}
}

// The stack decodes TimeSynchronization and UTCTimeSynchronization and hands
// them to the adapter, which records each as an operation line.
func TestSysIOBACnetTimeSynchronization(t *testing.T) {
	_, peer := startSysIO(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()

	ts := service.TimeSynchronization{
		Date: bacnet.Date{Year: 126, Month: 8, Day: 2, Weekday: 7},
		Time: bacnet.Time{Hour: 12, Minute: 0, Second: 0, Hundredths: 0},
	}
	if err := c.TimeSynchronization(ctx, peer.target.Endpoint, false, ts); err != nil {
		t.Fatalf("TimeSynchronization: %v", err)
	}
	awaitOperation(t, peer, "time-synchronization", 5*time.Second)
	if err := c.UTCTimeSynchronization(ctx, peer.target.Endpoint, false, ts); err != nil {
		t.Fatalf("UTCTimeSynchronization: %v", err)
	}
	awaitOperation(t, peer, "utc-time-synchronization", 5*time.Second)
}

// The frozen device-baseline-v1 (no Trend Log) and device-baseline-v3 (the
// adapter serves everything but the Event Enrollment) load too.
func TestSysIOBACnetOtherFixtures(t *testing.T) {
	for fixture, wantObjects := range map[string][]bacnet.ObjectIdentifier{
		"device-baseline-v1": {
			{Type: bacnet.ObjectTypeDevice, Instance: 1234},
			{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
			{Type: bacnet.ObjectTypeBinaryValue, Instance: 1},
		},
		"device-baseline-v3": {
			{Type: bacnet.ObjectTypeDevice, Instance: 1234},
			{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
			{Type: bacnet.ObjectTypeBinaryValue, Instance: 1},
			{Type: bacnet.ObjectTypeAnalogInput, Instance: 1},
			{Type: bacnet.ObjectTypeNotificationClass, Instance: 1},
			{Type: bacnet.ObjectTypeTrendLog, Instance: 1},
		},
	} {
		t.Run(fixture, func(t *testing.T) {
			dev, peer := startSysIOFixture(t, fixture)
			if peer.assertedByReexec {
				return
			}
			c := newClient(t)
			ctx, cancel := withTimeout(t, 15*time.Second)
			defer cancel()
			val, err := c.ReadProperty(ctx, peer.target, deviceObject(dev), bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName})
			if err != nil {
				t.Fatalf("ReadProperty object-name: %v", err)
			}
			if name := characterString(t, val); name != dev.DeviceName {
				t.Fatalf("object-name=%q, want %q", name, dev.DeviceName)
			}
			list, err := c.ReadObjectList(ctx, peer.target, deviceObject(dev), 0)
			if err != nil {
				t.Fatalf("ReadObjectList: %v", err)
			}
			if len(list) != len(wantObjects) {
				t.Fatalf("object-list = %v, want %v", list, wantObjects)
			}
			for _, want := range wantObjects {
				if !containsObject(list, want) {
					t.Fatalf("object-list = %v lacks %v", list, want)
				}
			}
			av, wantPV := analogValueObject(dev)
			if got := sysioReadReal(t, ctx, c, peer.target, av); math.Abs(float64(got-wantPV)) > 0.01 {
				t.Fatalf("present-value=%v, want %v", got, wantPV)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Empty lists
// ---------------------------------------------------------------------------

// A list with no elements is an empty propertyValue ("3E 3F") on the wire. The
// Device's Device_Address_Binding is one; ReadProperty returns it as an empty
// constructed value. (This peer was the first to have such a property read:
// go-bacnet used to refuse the empty wrapper as malformed. bacnet-stack,
// BACnet4J and BACpypes3 answer property 30 with the same octets.)
//
// System.IO.BACnet itself cannot put an empty list into a
// ReadPropertyMultiple-ACK and aborts instead: see
// TestSysIOBACnetReadPropertyMultipleAll.
func TestSysIOBACnetEmptyListReadProperty(t *testing.T) {
	dev, peer := startSysIO(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 10*time.Second)
	defer cancel()
	ref := bacnet.PropertyReference{Identifier: sysioPropertyDeviceAddressBinding}

	payload, err := service.EncodeReadProperty(service.ReadPropertyRequest{Object: deviceObject(dev), Property: ref})
	if err != nil {
		t.Fatal(err)
	}
	pdu, err := c.InvokeConfirmed(ctx, peer.target, apdu.ServiceReadProperty, payload, client.ConfirmedInvokeOptions{})
	if err != nil || pdu.ComplexACK == nil {
		t.Fatalf("raw ReadProperty device-address-binding: %v", err)
	}
	if want := []byte{0x0c, 0x02, 0x00, 0x04, 0xd2, 0x19, 0x1e, 0x3e, 0x3f}; !bytes.Equal(pdu.ComplexACK.Payload, want) {
		t.Fatalf("ReadProperty-ACK payload % x, want % x (an empty list)", pdu.ComplexACK.Payload, want)
	}

	v, err := c.ReadProperty(ctx, peer.target, deviceObject(dev), ref)
	if err != nil {
		t.Fatalf("Client.ReadProperty of an empty list: %v", err)
	}
	if v.Kind != bacnet.ValueConstructed || len(v.Elements) != 0 {
		t.Fatalf("Client.ReadProperty of an empty list = %+v, want an empty constructed value", v)
	}
}

// The two ACK forms that carry an empty list, decoded without a peer.
func TestSysIOBACnetEmptyListDecode(t *testing.T) {
	// ReadProperty-ACK: device,1234 / device-address-binding / empty list.
	rp := []byte{0x0c, 0x02, 0x00, 0x04, 0xd2, 0x19, 0x1e, 0x3e, 0x3f}
	ack, err := service.DecodeReadPropertyACK(rp, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatalf("DecodeReadPropertyACK(% x): %v, want an empty list", rp, err)
	}
	if ack.Value.Kind != bacnet.ValueConstructed || len(ack.Value.Elements) != 0 {
		t.Fatalf("DecodeReadPropertyACK(% x) value %+v, want an empty constructed value", rp, ack.Value)
	}

	// ReadPropertyMultiple-ACK: device,1234 { object-name "Dev", device-address-binding empty }.
	rpm := []byte{
		0x0c, 0x02, 0x00, 0x04, 0xd2, // [0] objectIdentifier
		0x1e,                                                    // [1] listOfResults
		0x29, 0x4d, 0x4e, 0x75, 0x04, 0x00, 'D', 'e', 'v', 0x4f, // object-name
		0x29, 0x1e, 0x4e, 0x4f, // device-address-binding: empty list
		0x1f,
	}
	results, err := service.DecodeReadPropertyMultipleACK(rpm, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatalf("DecodeReadPropertyMultipleACK(% x): %v, want two property results", rpm, err)
	}
	if len(results) != 1 || len(results[0].Properties) != 2 {
		t.Fatalf("DecodeReadPropertyMultipleACK results %+v, want one object with two properties", results)
	}
	if p := results[0].Properties[1]; p.Err != nil || p.Value.Kind != bacnet.ValueConstructed || len(p.Value.Elements) != 0 {
		t.Fatalf("device-address-binding result %+v, want an empty constructed value", p)
	}
}
