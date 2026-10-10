//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"context"
	"errors"
	"math"
	"net/netip"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/device"
	"github.com/otfabric/go-bacnet/service"
)

// interopPeer names one pinned peer stack.
type interopPeer struct {
	name  string
	image string
}

func stackPeer() interopPeer {
	return interopPeer{"bacnet-stack", getEnv("BACNET_STACK_IMAGE", defaultStackImage)}
}
func bacpypes3Peer() interopPeer {
	return interopPeer{"bacpypes3", getEnv("BACPYPES3_IMAGE", defaultBACpypes3Image)}
}
func bacnet4jPeer() interopPeer { return interopPeer{"bacnet4j", bacnet4jImage()} }
func worldietyPeer() interopPeer {
	return interopPeer{"worldiety", getEnv("WORLDIETY_IMAGE", defaultWorldietyImage)}
}

func allPeers() []interopPeer {
	return []interopPeer{stackPeer(), bacpypes3Peer(), bacnet4jPeer(), worldietyPeer()}
}

// forPeers runs fn once per peer as a subtest with a started peer container.
// fn is not called on the host side of an in-network re-execution.
func forPeers(t *testing.T, peers []interopPeer, fn func(t *testing.T, dev deviceFixture, peer *peerHandle)) {
	t.Helper()
	for _, p := range peers {
		t.Run(p.name, func(t *testing.T) {
			dev := loadDeviceFixture(t)
			peer := startPeer(t, p.image, p.name)
			if peer.assertedByReexec {
				return
			}
			fn(t, dev, peer)
		})
	}
}

func containsObject(list []bacnet.ObjectIdentifier, want bacnet.ObjectIdentifier) bool {
	for _, id := range list {
		if id == want {
			return true
		}
	}
	return false
}

// The Device wildcard instance (4194303) is answered with the real device
// identifier: the standard way to identify a device by address. bacnet-stack
// and BACnet4J implement it; BACpypes3 0.0.110 and the Worldiety fixture
// adapter answer object/unknown-object, which the second test pins down so a
// change in either peer is noticed.
func TestReadDeviceByWildcardInstance(t *testing.T) {
	forPeers(t, []interopPeer{stackPeer(), bacnet4jPeer()}, func(t *testing.T, dev deviceFixture, peer *peerHandle) {
		c := newClient(t)
		ctx, cancel := withTimeout(t, 10*time.Second)
		defer cancel()
		val, err := c.ReadProperty(ctx, peer.target, bacnet.AnyDevice(),
			bacnet.PropertyReference{Identifier: bacnet.PropertyObjectIdentifier})
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
	})
}

func TestReadDeviceByWildcardInstanceUnsupported(t *testing.T) {
	forPeers(t, []interopPeer{bacpypes3Peer(), worldietyPeer()}, func(t *testing.T, _ deviceFixture, peer *peerHandle) {
		c := newClient(t)
		ctx, cancel := withTimeout(t, 10*time.Second)
		defer cancel()
		_, err := c.ReadProperty(ctx, peer.target, bacnet.AnyDevice(),
			bacnet.PropertyReference{Identifier: bacnet.PropertyObjectIdentifier})
		var er *bacnet.ErrorResponse
		if !errors.As(err, &er) || er.Class != bacnet.ErrorClassObject || er.Code != bacnet.ErrorCodeUnknownObject {
			t.Fatalf("got %v, want object/unknown-object (peer does not implement the Device wildcard)", err)
		}
	})
}

// A Target that names only the endpoint is completed by the client.
func TestEndpointOnlyTarget(t *testing.T) {
	forPeers(t, allPeers(), func(t *testing.T, dev deviceFixture, peer *peerHandle) {
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
	})
}

func TestReadObjectList(t *testing.T) {
	forPeers(t, allPeers(), func(t *testing.T, dev deviceFixture, peer *peerHandle) {
		c := newClient(t)
		ctx, cancel := withTimeout(t, 15*time.Second)
		defer cancel()
		list, err := c.ReadObjectList(ctx, peer.target, deviceObject(dev), 0)
		if err != nil {
			t.Fatalf("ReadObjectList: %v", err)
		}
		av, _ := analogValueObject(dev)
		if !containsObject(list, deviceObject(dev)) || !containsObject(list, av) {
			t.Fatalf("object-list %v lacks %v or %v", list, deviceObject(dev), av)
		}
	})
}

func TestReadPropertyMultipleBatched(t *testing.T) {
	forPeers(t, allPeers(), func(t *testing.T, dev deviceFixture, peer *peerHandle) {
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
		var er *bacnet.ErrorResponse
		if e := results[0].Properties[2].Err; !errors.As(e, &er) || er.Code != bacnet.ErrorCodeUnknownProperty {
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
}

// WritePriority commands a slot of the priority array; RelinquishPriority
// writes NULL to it and the value falls back. The bacnet-stack adapter's
// Analog Value has no Priority_Array (unknown-property), so it is left out.
func TestWriteAndRelinquishPriority(t *testing.T) {
	forPeers(t, []interopPeer{bacpypes3Peer(), bacnet4jPeer()}, func(t *testing.T, dev deviceFixture, peer *peerHandle) {
		c := newClient(t)
		ctx, cancel := withTimeout(t, 20*time.Second)
		defer cancel()
		av, _ := analogValueObject(dev)
		pvRef := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
		readPV := func() float32 {
			t.Helper()
			v, err := c.ReadProperty(ctx, peer.target, av, pvRef)
			if err != nil {
				t.Fatalf("read present-value: %v", err)
			}
			f, err := bacnet.AsReal(v)
			if err != nil {
				t.Fatal(err)
			}
			return f
		}
		slot := func(prio uint32) bacnet.ApplicationValue {
			t.Helper()
			v, err := c.ReadProperty(ctx, peer.target, av,
				bacnet.PropertyReference{Identifier: bacnet.PropertyPriorityArray, ArrayIndex: &prio})
			if err != nil {
				t.Fatalf("read priority-array[%d]: %v", prio, err)
			}
			return v
		}

		before := readPV()
		const prio = 5
		if err := c.WritePriority(ctx, peer.target, av, prio, bacnet.RealValue(77.5)); err != nil {
			t.Fatalf("WritePriority: %v", err)
		}
		if got := readPV(); math.Abs(float64(got-77.5)) > 0.01 {
			t.Fatalf("present-value after WritePriority = %v, want 77.5", got)
		}
		if v := slot(prio); v.Kind != bacnet.ValueReal || math.Abs(float64(v.Real-77.5)) > 0.01 {
			t.Fatalf("priority-array[%d] = %+v, want REAL 77.5", prio, v)
		}
		if err := c.RelinquishPriority(ctx, peer.target, av, prio); err != nil {
			t.Fatalf("RelinquishPriority: %v", err)
		}
		if v := slot(prio); v.Kind != bacnet.ValueNull {
			t.Fatalf("priority-array[%d] after relinquish = %+v, want NULL", prio, v)
		}
		if got := readPV(); math.Abs(float64(got-before)) > 0.01 {
			t.Fatalf("present-value after relinquish = %v, want %v again", got, before)
		}
	})
}

func TestDeviceSnapshot(t *testing.T) {
	forPeers(t, allPeers(), func(t *testing.T, dev deviceFixture, peer *peerHandle) {
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
		av, _ := analogValueObject(dev)
		found := false
		for _, o := range snap.Objects {
			if o.Object == av {
				found = true
			}
		}
		if !found {
			t.Fatalf("snapshot objects lack %v: %+v", av, snap.Objects)
		}
	})
}

func TestReadPropertyUnknownObjectError(t *testing.T) {
	forPeers(t, allPeers(), func(t *testing.T, _ deviceFixture, peer *peerHandle) {
		c := newClient(t)
		ctx, cancel := withTimeout(t, 10*time.Second)
		defer cancel()
		missing := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogInput, Instance: 4000000}
		_, err := c.ReadProperty(ctx, peer.target, missing, bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue})
		var er *bacnet.ErrorResponse
		if !errors.As(err, &er) {
			t.Fatalf("got %T %v, want *ErrorResponse", err, err)
		}
		if er.Class != bacnet.ErrorClassObject || er.Code != bacnet.ErrorCodeUnknownObject {
			t.Fatalf("error %s/%s, want object/unknown-object",
				bacnet.ErrorClassName(er.Class), bacnet.ErrorCodeName(er.Code))
		}
	})
}

// Writing a read-only property must come back as a definite remote Error,
// not as an ambiguous outcome. Object_Type is read-only in every stack (the
// Device Object_Identifier is not: three of the four peers accept that write).
// BACpypes3 0.0.110 accepts even this write, so it is left out.
func TestWritePropertyReadOnlyError(t *testing.T) {
	forPeers(t, []interopPeer{stackPeer(), bacnet4jPeer(), worldietyPeer()}, func(t *testing.T, dev deviceFixture, peer *peerHandle) {
		c := newClient(t)
		ctx, cancel := withTimeout(t, 10*time.Second)
		defer cancel()
		av, _ := analogValueObject(dev)
		objType := bacnet.PropertyReference{Identifier: bacnet.PropertyObjectType}
		err := c.WriteProperty(ctx, peer.target, av, objType, bacnet.EnumValue(uint32(bacnet.ObjectTypeBinaryValue)), nil)
		var er *bacnet.ErrorResponse
		if !errors.As(err, &er) {
			t.Fatalf("got %T %v, want *ErrorResponse", err, err)
		}
		var unknown *bacnet.OutcomeUnknownError
		if errors.As(err, &unknown) {
			t.Fatalf("definite Error reported as outcome-unknown: %v", err)
		}
		t.Logf("%s refuses the write with %s/%s", t.Name(), bacnet.ErrorClassName(er.Class), bacnet.ErrorCodeName(er.Code))
		val, err := c.ReadProperty(ctx, peer.target, av, objType)
		if err != nil {
			t.Fatal(err)
		}
		if val.Enumerated != uint32(bacnet.ObjectTypeAnalogValue) {
			t.Fatalf("object-type changed to %d", val.Enumerated)
		}
	})
}

// Nothing listens on the port next to the peer's: requests get no answer.
// Reads must end in bacnet.ErrTimeout after timeout × (retries + 1); a write
// is sent once and ends as outcome-unknown. The same paths against a peer
// that was muted with DeviceCommunicationControl are covered by
// TestBACnet4JDeviceCommunicationControlDisable; this test needs no peer
// cooperation and also checks the retransmission timing and a context
// deadline.
func TestUnansweredRequestTimeouts(t *testing.T) {
	forPeers(t, []interopPeer{stackPeer()}, func(t *testing.T, dev deviceFixture, peer *peerHandle) {
		const apduTimeout = 500 * time.Millisecond
		const retries = 2
		c := newClientOpts(t, client.WithTransactionOptions(apduTimeout, retries, time.Second))
		ctx, cancel := withTimeout(t, 30*time.Second)
		defer cancel()
		name := bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName}
		if _, err := c.ReadProperty(ctx, peer.target, deviceObject(dev), name); err != nil {
			t.Fatalf("read from the live peer: %v", err)
		}

		ap := peer.target.Endpoint.Addr
		silent := client.Target{Endpoint: bip.NewEndpoint(netip.AddrPortFrom(ap.Addr(), ap.Port()+1))}

		start := time.Now()
		_, err := c.ReadProperty(ctx, silent, deviceObject(dev), name)
		elapsed := time.Since(start)
		if !errors.Is(err, bacnet.ErrTimeout) {
			t.Fatalf("read without an answer: %v, want ErrTimeout", err)
		}
		var unknown *bacnet.OutcomeUnknownError
		if errors.As(err, &unknown) {
			t.Fatalf("a read timeout must not be outcome-unknown: %v", err)
		}
		want := apduTimeout * (retries + 1)
		if elapsed < want-100*time.Millisecond || elapsed > want+2*time.Second {
			t.Fatalf("read gave up after %v, want about %v (timeout × (retries+1))", elapsed, want)
		}

		av, _ := analogValueObject(dev)
		prio := uint8(8)
		start = time.Now()
		err = c.WriteProperty(ctx, silent, av, bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
			bacnet.RealValue(33), &prio)
		elapsed = time.Since(start)
		if !errors.As(err, &unknown) || !errors.Is(err, bacnet.ErrTimeout) {
			t.Fatalf("write without an answer: %v, want OutcomeUnknownError wrapping ErrTimeout", err)
		}
		if elapsed > apduTimeout+time.Second {
			t.Fatalf("write waited %v: writes must not be retransmitted (one APDU timeout = %v)", elapsed, apduTimeout)
		}

		// The client is still usable and a context deadline is honoured.
		short, cancelShort := withTimeout(t, 200*time.Millisecond)
		defer cancelShort()
		if _, err := c.ReadProperty(short, silent, deviceObject(dev), name); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("read with a 200ms deadline: %v, want context.DeadlineExceeded", err)
		}
		if _, err := c.ReadProperty(ctx, peer.target, deviceObject(dev), name); err != nil {
			t.Fatalf("read from the live peer afterwards: %v", err)
		}
	})
}

// bacnet-stack 1.6.1 refuses DeviceCommunicationControl "disable" (removed
// from the standard at Protocol_Revision 20) with a definite Error.
func TestBacnetStackDeviceCommunicationControlDisableDenied(t *testing.T) {
	forPeers(t, []interopPeer{stackPeer()}, func(t *testing.T, _ deviceFixture, peer *peerHandle) {
		c := newClientOpts(t, client.WithDeviceManagementEnabled(client.DeviceManagementConfirm))
		ctx, cancel := withTimeout(t, 10*time.Second)
		defer cancel()
		err := c.DeviceCommunicationControl(ctx, peer.target, service.DeviceCommunicationControlRequest{
			EnableDisable: service.EnableDisableDisable,
		})
		var er *bacnet.ErrorResponse
		if !errors.As(err, &er) || er.Class != bacnet.ErrorClassServices || er.Code != bacnet.ErrorCodeServiceRequestDenied {
			t.Fatalf("DCC disable: %v, want services/service-request-denied", err)
		}
	})
}

// ReadRangeAll pages through a log buffer one record at a time. A page that
// returns the one record it asked for has MORE_ITEMS clear on every peer,
// which must not end the paging.
func TestReadRangeAllPager(t *testing.T) {
	forPeers(t, []interopPeer{stackPeer(), bacnet4jPeer(), worldietyPeer()}, func(t *testing.T, dev deviceFixture, peer *peerHandle) {
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
			Count:          100,
		}
		whole, err := c.ReadRange(ctx, peer.target, req)
		if err != nil {
			t.Fatalf("ReadRange: %v", err)
		}
		if whole.ItemCount < 2 {
			t.Skipf("%s serves %d log record(s); paging needs at least 2", t.Name(), whole.ItemCount)
		}
		count := func(r client.ReadRangePageResult) (n uint32) {
			for _, p := range r.Pages {
				n += p.ItemCount
			}
			return n
		}
		// One record per page, bounded: as many pages as records asked for.
		want := min(whole.ItemCount, 5)
		req.Count = 1
		paged, err := c.ReadRangeAll(ctx, peer.target, req, client.ReadRangePageOptions{PageCount: 1, MaxItems: int(want)})
		if err != nil {
			t.Fatalf("ReadRangeAll: %v (stopped: %q after %d pages)", err, paged.StoppedReason, len(paged.Pages))
		}
		if got := count(paged); got != want || len(paged.Pages) != int(want) {
			t.Fatalf("paged %d records in %d pages (%q), want %d records in as many pages",
				got, len(paged.Pages), paged.StoppedReason, want)
		}
		// A log the peer returns whole in one reply (LAST_ITEM set) must be
		// paged to its natural end, not to a limit.
		if !whole.LastItem() {
			t.Logf("%s: first reply holds %d records without LAST_ITEM; natural end not exercised", t.Name(), whole.ItemCount)
			return
		}
		all, err := c.ReadRangeAll(ctx, peer.target, req, client.ReadRangePageOptions{PageCount: 1})
		if err != nil {
			t.Fatalf("ReadRangeAll to the end: %v", err)
		}
		if got := count(all); got != whole.ItemCount || all.Partial {
			t.Fatalf("paged %d records (partial=%v %q), the log holds %d", got, all.Partial, all.StoppedReason, whole.ItemCount)
		}
	})
}
