//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"bytes"
	"context"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/service"
)

// System.IO.BACnet peer, continued: broadcast discovery, routers, record
// files, alarms. See sysiobacnet_test.go for the peer and its conventions.

func sysioPeer() interopPeer { return interopPeer{sysioAdapter, sysiobacnetImage()} }

// ---------------------------------------------------------------------------
// Broadcast Who-Is
// ---------------------------------------------------------------------------

// Discover with a real broadcast, as TestBroadcastDiscover does for the other
// peers: the client sends its Who-Is to the subnet's broadcast address and to
// 255.255.255.255.
func TestSysIOBACnetBroadcastDiscover(t *testing.T) {
	dev, peer := startSysIO(t)
	if peer.assertedByReexec {
		return
	}
	c := newDiscoveryClient(t)
	ctx, cancel := withTimeout(t, 15*time.Second)
	defer cancel()

	found, err := c.Discover(ctx, client.DiscoveryOptions{Window: 3 * time.Second})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	var obs *client.DeviceObservation
	for i := range found {
		if found[i].Instance == dev.DeviceInstance {
			obs = &found[i]
		}
	}
	if obs == nil {
		t.Fatalf("device %d did not answer the broadcast Who-Is; found %+v", dev.DeviceInstance, found)
	}
	if !obs.Address.Equal(peer.target.Address) {
		t.Fatalf("discovered at %v, the peer is %v", obs.Address, peer.target.Address)
	}
	if obs.Target().Endpoint != peer.target.Endpoint {
		t.Fatalf("Target() endpoint %v, the peer is %v", obs.Target().Endpoint, peer.target.Endpoint)
	}
	val, err := c.ReadProperty(ctx, obs.Target(), obs.DeviceID(), bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName})
	if err != nil {
		t.Fatalf("ReadProperty through the discovered target: %v", err)
	}
	if got := characterString(t, val); got != dev.DeviceName {
		t.Fatalf("object-name %q, want %q", got, dev.DeviceName)
	}
}

// Which broadcast the stack hears, and how it answers, established with plain
// datagrams: one Who-Is to the subnet-directed broadcast address from UDP
// 47808, one to 255.255.255.255.
//
// The second is sent from a socket bound to the local address on the peer's
// subnet. A host with several interfaces (a CI runner with its Docker
// bridges) sends 255.255.255.255 only where its default route points; the
// bound source address makes it leave on the peer's network instead.
//
// The stack's socket is bound to 0.0.0.0:47808, so it receives both. The
// adapter answers every Who-Is with a unicast I-Am (Original-Unicast-NPDU) to
// the datagram's source.
func TestSysIOBACnetBroadcastWhoIsForms(t *testing.T) {
	_, peer := startSysIO(t)
	if peer.assertedByReexec {
		return
	}
	peerIP := peer.target.Endpoint.Addr.Addr()

	// The directed broadcast address of the subnet the peer is on.
	var directed, local net.IP
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok || ipn.IP.To4() == nil || !ipn.Contains(net.IP(peerIP.AsSlice())) {
			continue
		}
		ip4, mask := ipn.IP.To4(), ipn.Mask
		directed = net.IPv4(ip4[0]|^mask[0], ip4[1]|^mask[1], ip4[2]|^mask[2], ip4[3]|^mask[3])
		local = ip4
	}
	if directed == nil {
		t.Fatalf("no local interface shares a subnet with the peer %v", peerIP)
	}

	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: 47808})
	if err != nil {
		t.Fatalf("listen UDP 47808: %v", err)
	}
	defer conn.Close()
	pinned, err := net.ListenUDP("udp4", &net.UDPAddr{IP: local})
	if err != nil {
		t.Fatalf("listen UDP on %v: %v", local, err)
	}
	defer pinned.Close()

	// Original-Broadcast-NPDU, NPDU with DNET 0xFFFF and hop count 255, Who-Is.
	whoIs := []byte{0x81, 0x0b, 0x00, 0x0c, 0x01, 0x20, 0xff, 0xff, 0x00, 0xff, 0x10, 0x08}
	for _, form := range []struct {
		name string
		conn *net.UDPConn
		dst  net.IP
	}{
		{"subnet-directed broadcast " + directed.String(), conn, directed},
		{"limited broadcast 255.255.255.255 from " + local.String(), pinned, net.IPv4bcast},
	} {
		conn := form.conn
		if _, err := conn.WriteToUDP(whoIs, &net.UDPAddr{IP: form.dst, Port: 47808}); err != nil {
			t.Fatalf("%s: send Who-Is: %v", form.name, err)
		}
		buf := make([]byte, 1500)
		deadline := time.Now().Add(3 * time.Second)
		answered := false
		for !answered {
			_ = conn.SetReadDeadline(deadline)
			n, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				t.Fatalf("%s: no I-Am from the peer within 3 s (%v): the stack did not receive this broadcast form", form.name, err)
			}
			frame := buf[:n]
			fromIP, _ := netip.AddrFromSlice(from.IP.To4())
			// Our own broadcast comes back to us; skip everything but an I-Am from the peer.
			if fromIP != peerIP || n < 8 || !bytes.Contains(frame[4:], []byte{0x10, 0x00, 0xc4}) {
				continue
			}
			answered = true
			if frame[1] != 0x0a {
				t.Fatalf("%s: I-Am BVLC function 0x%02x, want 0x0a Original-Unicast-NPDU: the adapter answered the requester directly", form.name, frame[1])
			}
			// NPDU control octet: no DNET, no SNET - a plain local unicast.
			if frame[4] != 0x01 || frame[5] != 0x00 {
				t.Fatalf("%s: I-Am NPDU % x, want version 1 control 0x00", form.name, frame[4:6])
			}
			t.Logf("%s: received; I-Am came back as Original-Unicast-NPDU from %v: % x", form.name, from, frame)
		}
	}
}

// ---------------------------------------------------------------------------
// Behind a router
// ---------------------------------------------------------------------------

// The device on BACnet network 2 behind bip-router, the client on network 1:
// routed ReadProperty, ReadPropertyMultiple and WriteProperty and a Who-Is
// broadcast to the remote network, each answered through the router.
func TestSysIOBACnetRoutedBipRouter(t *testing.T) {
	requireAdapterVersion(t, sysiobacnetImage(), sysioMinAdapterVersion)
	dev := loadDeviceFixture(t)
	topo := startRoutedTopology(t, sysiobacnetImage(), sysioAdapter)
	if topo.assertedByReexec {
		return
	}
	c := newDiscoveryClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	deviceAddr := topo.remoteAddress()
	target := client.Target{Address: deviceAddr, Endpoint: topo.router, MaxAPDU: 1476}
	val, err := readPropertyRetry(t, ctx, c, target, deviceObject(dev), bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName})
	if err != nil {
		t.Fatalf("routed ReadProperty: %v", err)
	}
	if name := characterString(t, val); name != dev.DeviceName {
		t.Fatalf("object-name=%q, want %q", name, dev.DeviceName)
	}

	av, wantPV := analogValueObject(dev)
	results, err := c.ReadPropertyMultiple(ctx, target, []service.ReadAccessSpecification{
		{Object: deviceObject(dev), Properties: []bacnet.PropertyReference{{Identifier: bacnet.PropertyObjectIdentifier}}},
		{Object: av, Properties: []bacnet.PropertyReference{{Identifier: bacnet.PropertyPresentValue}}},
	})
	if err != nil {
		t.Fatalf("routed ReadPropertyMultiple: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("routed RPM: %d results, want 2", len(results))
	}
	if pv, err := bacnet.AsReal(findPropertyValue(t, results[1], bacnet.PropertyPresentValue)); err != nil || pv != wantPV {
		t.Fatalf("routed RPM present-value = %v (%v), want %v", pv, err, wantPV)
	}

	// A write, so a routed SimpleACK is covered too.
	prio := uint8(8)
	pvRef := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	if err := c.WriteProperty(ctx, target, av, pvRef, bacnet.RealValue(wantPV+1), &prio); err != nil {
		t.Fatalf("routed WriteProperty: %v", err)
	}
	if got := sysioReadReal(t, ctx, c, target, av); got != wantPV+1 {
		t.Fatalf("routed readback = %v, want %v", got, wantPV+1)
	}

	// Who-Is to the remote network's broadcast through the router. (A global
	// Who-Is is asserted behind the native router; through the pinned
	// bip-router 0.9.0 it produced no I-Am here.) The stack answers the requester directly: its I-Am is a unicast
	// addressed to the client's network (DNET 1 / DADR) and handed to the
	// router. bip-router delivers such a frame on the final network without
	// SNET/SADR (its documented "final unicast delivery omits SNET"), so here
	// the I-Am is seen coming from the router's own address. The device's
	// network address in a routed I-Am is asserted behind the native router.
	low, high := dev.DeviceInstance, dev.DeviceInstance
	awaitViaRouter := func(what string) {
		t.Helper()
		deadline := time.Now().Add(4 * time.Second)
		for {
			for _, obs := range c.Devices() {
				if obs.Instance == dev.DeviceInstance && obs.ImmediatePeer.Equal(topo.router) {
					if !obs.Capabilities.MaxAPDULengthAccepted.Known || obs.Capabilities.MaxAPDULengthAccepted.Value != sysioMaxAPDU {
						t.Fatalf("%s: routed I-Am MaxAPDU %+v, want %d", what, obs.Capabilities.MaxAPDULengthAccepted, sysioMaxAPDU)
					}
					t.Logf("%s: I-Am for device %d through the router %v, seen at %v", what, obs.Instance, obs.ImmediatePeer, obs.Address)
					return
				}
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s: no I-Am through the router %v; devices=%+v", what, topo.router, c.Devices())
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	if err := c.SendWhoIs(ctx, topo.router, false, client.DiscoveryOptions{
		LowLimit: &low, HighLimit: &high, Address: bacnet.RemoteBroadcast(topo.remoteNet),
	}); err != nil {
		t.Fatalf("remote-broadcast Who-Is: %v", err)
	}
	awaitViaRouter("Who-Is to the remote network")
}

// The same behind bacnet-stack's own router, which gives no return-path
// assistance: the reply only arrives if the stack itself addresses it to the
// requester's network (DNET/DADR) and sends it to the router. runNativeRouter
// also asserts the I-Am: System.IO.BACnet answers the requester directly, so
// the router delivers it with SNET/SADR and without a DNET.
func TestSysIOBACnetNativeRouter(t *testing.T) {
	requireAdapterVersion(t, sysiobacnetImage(), sysioMinAdapterVersion)
	runNativeRouter(t, sysioPeer(), nil)
}

// ---------------------------------------------------------------------------
// Record-access files (device-baseline-v4)
// ---------------------------------------------------------------------------

var sysioRecordFile = bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 2}

func sysioRequireRecords(t *testing.T, what string, got [][]byte, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: %d records %q, want %q", what, len(got), got, want)
	}
	for i := range want {
		if string(got[i]) != want[i] {
			t.Fatalf("%s: record %d = %q, want %q (all: %q)", what, i, got[i], want[i], got)
		}
	}
}

func TestSysIOBACnetAtomicReadFileRecord(t *testing.T) {
	_, peer := startSysIOFixture(t, "device-baseline-v4")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 15*time.Second)
	defer cancel()
	read := func(start int32, count uint32) service.AtomicReadFileACK {
		t.Helper()
		ack, err := c.AtomicReadFile(ctx, peer.target, service.AtomicReadFileRequest{
			File: sysioRecordFile, Access: service.FileAccessRecord, StartPosition: start, Count: count,
		})
		if err != nil {
			t.Fatalf("AtomicReadFile record %d+%d: %v", start, count, err)
		}
		if ack.Access != service.FileAccessRecord || ack.StartPosition != start || int(ack.RecordCount) != len(ack.Records) {
			t.Fatalf("AtomicReadFile record %d+%d: ACK %+v", start, count, ack)
		}
		return ack
	}

	one := read(0, 1)
	sysioRequireRecords(t, "first record", one.Records, "record-0001")
	if one.EndOfFile {
		t.Fatal("EOF on the first of three records")
	}
	all := read(0, 10)
	sysioRequireRecords(t, "all records", all.Records, "record-0001", "record-0002", "record-0003")
	if !all.EndOfFile {
		t.Fatal("no EOF on a read past the last record")
	}
	last := read(2, 1)
	sysioRequireRecords(t, "last record", last.Records, "record-0003")
	if !last.EndOfFile {
		t.Fatal("no EOF on the last record")
	}

	// The paging helper, one record per request.
	paged, err := c.ReadFileRecords(ctx, peer.target, sysioRecordFile, 0, client.FileReadOptions{ChunkSize: 1})
	if err != nil {
		t.Fatalf("ReadFileRecords: %v", err)
	}
	sysioRequireRecords(t, "ReadFileRecords", paged, "record-0001", "record-0002", "record-0003")

	v, err := c.ReadProperty(ctx, peer.target, sysioRecordFile, bacnet.PropertyReference{Identifier: sysioPropertyRecordCount})
	if err != nil {
		t.Fatalf("ReadProperty record-count: %v", err)
	}
	if n, err := bacnet.AsUnsigned(v); err != nil || n != 3 {
		t.Fatalf("record-count = %+v (%v), want 3", v, err)
	}

	_, err = c.AtomicReadFile(ctx, peer.target, service.AtomicReadFileRequest{
		File: sysioRecordFile, Access: service.FileAccessRecord, StartPosition: 9, Count: 1,
	})
	sysioRequireError(t, "start record past the end", err, bacnet.ErrorClassServices, bacnet.ErrorCodeInvalidFileStartPosition)
}

func TestSysIOBACnetAtomicWriteFileRecordReadback(t *testing.T) {
	_, peer := startSysIOFixture(t, "device-baseline-v4")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 15*time.Second)
	defer cancel()
	write := func(start int32, records ...string) service.AtomicWriteFileACK {
		t.Helper()
		var recs [][]byte
		for _, r := range records {
			recs = append(recs, []byte(r))
		}
		ack, err := c.AtomicWriteFile(ctx, peer.target, service.AtomicWriteFileRequest{
			File: sysioRecordFile, Access: service.FileAccessRecord, StartPosition: start, Records: recs,
		})
		if err != nil {
			t.Fatalf("AtomicWriteFile record at %d: %v", start, err)
		}
		if ack.Access != service.FileAccessRecord {
			t.Fatalf("AtomicWriteFile record at %d: ACK access %v, want record", start, ack.Access)
		}
		return ack
	}
	readAll := func() [][]byte {
		t.Helper()
		ack, err := c.AtomicReadFile(ctx, peer.target, service.AtomicReadFileRequest{
			File: sysioRecordFile, Access: service.FileAccessRecord, StartPosition: 0, Count: 20,
		})
		if err != nil {
			t.Fatalf("AtomicReadFile record: %v", err)
		}
		return ack.Records
	}

	// Overwrite the second record in place.
	if ack := write(1, "REPLACED"); ack.StartPosition != 1 {
		t.Fatalf("overwrite ACK start record %d, want 1", ack.StartPosition)
	}
	sysioRequireRecords(t, "after the overwrite", readAll(), "record-0001", "REPLACED", "record-0003")

	// Start record -1 appends; the ACK says where.
	if ack := write(-1, "appended-A", "appended-B"); ack.StartPosition != 3 {
		t.Fatalf("append ACK start record %d, want 3", ack.StartPosition)
	}
	sysioRequireRecords(t, "after the append", readAll(), "record-0001", "REPLACED", "record-0003", "appended-A", "appended-B")

	// Stream access to the record file is refused.
	_, err := c.AtomicWriteFile(ctx, peer.target, service.AtomicWriteFileRequest{
		File: sysioRecordFile, Access: service.FileAccessStream, StartPosition: 0, Data: []byte("x"),
	})
	sysioRequireError(t, "stream write to a record file", err, bacnet.ErrorClassServices, bacnet.ErrorCodeInvalidFileAccessMethod)
}

// ---------------------------------------------------------------------------
// Alarms (device-baseline-v3)
// ---------------------------------------------------------------------------
//
// The alarm behaviour is the adapter's (intrinsic out-of-range on AV-1, high
// limit 80); every request decoder, the GetEventInformation-ACK and the
// EventNotification encoders are System.IO.BACnet's.

var sysioAlarmAV = bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}

const (
	sysioEventStateNormal    = 0
	sysioEventStateHighLimit = 3
)

// Drive AV-1 out of range, find the alarm with GetEventInformation,
// acknowledge the to-offnormal transition with the time stamp the device
// reported, and return to normal.
func TestSysIOBACnetGetEventInformationAndAcknowledgeAlarm(t *testing.T) {
	_, peer := startSysIOFixture(t, "device-baseline-v3")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 30*time.Second)
	defer cancel()
	pv := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	prio := uint8(8)

	summaryOf := func(what string) (service.EventSummary, bool) {
		t.Helper()
		ack, err := c.GetEventInformation(ctx, peer.target, service.GetEventInformationRequest{})
		if err != nil {
			t.Fatalf("GetEventInformation %s: %v", what, err)
		}
		if ack.MoreEvents {
			t.Fatalf("GetEventInformation %s: moreEvents set", what)
		}
		for _, s := range ack.Summaries {
			if s.Object == sysioAlarmAV {
				return s, true
			}
		}
		return service.EventSummary{}, false
	}
	bit := func(bs bacnet.BitString, i int) bool { return bs.Bytes[0]&(0x80>>i) != 0 }

	if s, ok := summaryOf("before the alarm"); ok {
		t.Fatalf("AV-1 already has an active event: %+v", s)
	}

	if err := c.WriteProperty(ctx, peer.target, sysioAlarmAV, pv, bacnet.RealValue(90), &prio); err != nil {
		t.Fatalf("WriteProperty AV-1=90: %v", err)
	}
	summary, ok := summaryOf("in alarm")
	if !ok {
		t.Fatal("AV-1 not reported by GetEventInformation after Present_Value 90")
	}
	if summary.EventState != sysioEventStateHighLimit {
		t.Fatalf("event state %d, want high-limit(3): %+v", summary.EventState, summary)
	}
	if n := len(summary.EventTimeStamps.Elements); n != 3 {
		t.Fatalf("eventTimeStamps has %d entries, want 3 (to-offnormal, to-fault, to-normal)", n)
	}
	if n := len(summary.EventPriorities.Elements); n != 3 {
		t.Fatalf("eventPriorities has %d entries, want 3", n)
	}
	if summary.NotifyType != 0 {
		t.Fatalf("notifyType %d, want alarm(0)", summary.NotifyType)
	}
	// to-offnormal unacknowledged, the other two acknowledged.
	if at := summary.AcknowledgedTransitions; len(at.Bytes) != 1 || at.UnusedBits != 5 || bit(at, 0) || !bit(at, 1) || !bit(at, 2) {
		t.Fatalf("acknowledgedTransitions %+v, want (false, true, true)", at)
	}
	offnormal := eventTimeStamp(t, summary.EventTimeStamps, 0)
	if offnormal.Choice != service.TimeStampDateTime {
		t.Fatalf("to-offnormal time stamp %+v, want a date-time", offnormal)
	}
	// A transition that never happened carries sequence number 0.
	if never := eventTimeStamp(t, summary.EventTimeStamps, 2); never.Choice != service.TimeStampSequence || never.Sequence != 0 {
		t.Fatalf("to-normal time stamp %+v before any return to normal, want sequence 0", never)
	}

	// The object's own properties follow.
	v, err := c.ReadProperty(ctx, peer.target, sysioAlarmAV, bacnet.PropertyReference{Identifier: bacnet.PropertyEventState})
	if err != nil || v.Enumerated != sysioEventStateHighLimit {
		t.Fatalf("event-state property = %+v (%v), want high-limit(3)", v, err)
	}
	v, err = c.ReadProperty(ctx, peer.target, sysioAlarmAV, bacnet.PropertyReference{Identifier: bacnet.PropertyStatusFlags})
	if err != nil || v.Kind != bacnet.ValueBitString || !bit(v.BitString, 0) {
		t.Fatalf("status-flags = %+v (%v), want in-alarm set", v, err)
	}

	ackReq := func(state uint32, ts service.TimeStamp) service.AcknowledgeAlarmRequest {
		return service.AcknowledgeAlarmRequest{
			ProcessIdentifier:    1,
			EventObject:          sysioAlarmAV,
			EventStateAcked:      state,
			TimeStamp:            ts,
			AcknowledgmentSource: bacnet.CharacterString{Value: "go-bacnet interop"},
			TimeOfAcknowledgment: service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 1},
		}
	}
	// A stale time stamp and a wrong state are refused with definite Errors.
	err = c.AcknowledgeAlarm(ctx, peer.target, ackReq(summary.EventState, service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 40000}))
	sysioRequireError(t, "AcknowledgeAlarm with a wrong time stamp", err, bacnet.ErrorClassServices, bacnet.ErrorCodeInvalidTimeStamp)
	err = c.AcknowledgeAlarm(ctx, peer.target, ackReq(sysioEventStateNormal, offnormal))
	sysioRequireError(t, "AcknowledgeAlarm of a state the object is not in", err, bacnet.ErrorClassServices, bacnet.ErrorCodeInvalidEventState)
	req := ackReq(summary.EventState, offnormal)
	req.EventObject = bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeBinaryValue, Instance: 1}
	err = c.AcknowledgeAlarm(ctx, peer.target, req)
	sysioRequireError(t, "AcknowledgeAlarm of an object without alarms", err, bacnet.ErrorClassObject, bacnet.ErrorCodeNoAlarmConfigured)

	if err := c.AcknowledgeAlarm(ctx, peer.target, ackReq(summary.EventState, offnormal)); err != nil {
		t.Fatalf("AcknowledgeAlarm with the reported time stamp: %v", err)
	}
	awaitOperation(t, peer, "acknowledge-alarm", 5*time.Second)
	summary, ok = summaryOf("after the acknowledgement")
	if !ok {
		t.Fatal("AV-1 dropped out of GetEventInformation while still in alarm")
	}
	if at := summary.AcknowledgedTransitions; !bit(at, 0) || !bit(at, 1) || !bit(at, 2) {
		t.Fatalf("acknowledgedTransitions %+v after the acknowledgement, want all set", at)
	}

	// Back in range: normal and fully acknowledged, so no longer reported.
	if err := c.WriteProperty(ctx, peer.target, sysioAlarmAV, pv, bacnet.RealValue(20), &prio); err != nil {
		t.Fatalf("WriteProperty AV-1=20: %v", err)
	}
	if s, ok := summaryOf("back to normal"); ok {
		t.Fatalf("AV-1 still reported after returning to normal: %+v", s)
	}
	v, err = c.ReadProperty(ctx, peer.target, sysioAlarmAV, bacnet.PropertyReference{Identifier: bacnet.PropertyEventState})
	if err != nil || v.Enumerated != sysioEventStateNormal {
		t.Fatalf("event-state property = %+v (%v), want normal(0)", v, err)
	}
}

// The adapter sends an UnconfirmedEventNotification for each transition to
// the peer whose write caused it: to-offnormal at 90, to-normal at 20.
func TestSysIOBACnetEventNotificationOnAlarm(t *testing.T) {
	dev, peer := startSysIOFixture(t, "device-baseline-v3")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	got := make(chan client.EventNotificationDelivery, 8)
	c.SetEventNotificationHandler(func(d client.EventNotificationDelivery) {
		select {
		case got <- d:
		default:
		}
	})
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()
	pv := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	prio := uint8(8)

	expect := func(value float32, from, to uint32, ackRequired bool) {
		t.Helper()
		if err := c.WriteProperty(ctx, peer.target, sysioAlarmAV, pv, bacnet.RealValue(value), &prio); err != nil {
			t.Fatalf("WriteProperty AV-1=%v: %v", value, err)
		}
		var d client.EventNotificationDelivery
		select {
		case d = <-got:
		case <-time.After(8 * time.Second):
			t.Fatalf("no EventNotification after Present_Value %v", value)
		}
		n := d.Notification
		if d.Confirmed {
			t.Fatal("expected an unconfirmed EventNotification")
		}
		if n.InitiatingDevice != deviceObject(dev) || n.EventObject != sysioAlarmAV || n.ProcessIdentifier != 1 {
			t.Fatalf("notification from %v for %v process %d", n.InitiatingDevice, n.EventObject, n.ProcessIdentifier)
		}
		if n.EventType != 5 || n.NotifyType != 0 || n.NotificationClass != 1 || n.Priority != 100 {
			t.Fatalf("eventType %d notifyType %d class %d priority %d, want out-of-range(5) alarm(0) 1 100",
				n.EventType, n.NotifyType, n.NotificationClass, n.Priority)
		}
		if n.FromState == nil || *n.FromState != from || n.ToState != to {
			t.Fatalf("transition %v -> %d, want %d -> %d", n.FromState, n.ToState, from, to)
		}
		if n.AckRequired == nil || *n.AckRequired != ackRequired {
			t.Fatalf("ackRequired %v, want %v", n.AckRequired, ackRequired)
		}
		if n.MessageText == nil || n.MessageText.Value != "interop-event" {
			t.Fatalf("messageText %+v", n.MessageText)
		}
		if n.TimeStamp.Choice != service.TimeStampDateTime {
			t.Fatalf("time stamp %+v, want a date-time", n.TimeStamp)
		}
		if n.Parameters == nil || n.Parameters.OutOfRange == nil {
			t.Fatalf("notification parameters %+v, want out-of-range", n.NotificationParams)
		}
		if oor := n.Parameters.OutOfRange; oor.ExceedingValue != value || oor.ExceededLimit != 80 || oor.Deadband != 0 {
			t.Fatalf("out-of-range parameters %+v, want exceeding value %v, limit 80, deadband 0", oor, value)
		}
	}
	expect(90, sysioEventStateNormal, sysioEventStateHighLimit, true)
	expect(20, sysioEventStateHighLimit, sysioEventStateNormal, false)

	// A write that changes no state sends nothing.
	if err := c.WriteProperty(ctx, peer.target, sysioAlarmAV, pv, bacnet.RealValue(25), &prio); err != nil {
		t.Fatalf("WriteProperty AV-1=25: %v", err)
	}
	select {
	case d := <-got:
		t.Fatalf("EventNotification without a transition: %+v", d.Notification)
	case <-time.After(1500 * time.Millisecond):
	}
}

// BACNET_EMIT_EVENT=1, the trigger the BACpypes3 and BACnet4J adapters share:
// one UnconfirmedEventNotification after the first ReadProperty.
func TestSysIOBACnetEventNotificationReceive(t *testing.T) {
	requireAdapterVersion(t, sysiobacnetImage(), sysioMinAdapterVersion)
	runEventNotificationReceive(t, sysiobacnetImage(), sysioAdapter)
}

// Deviation: BacnetClient 4.0.0 answers GetAlarmSummary with Reject
// unrecognized-service even when the application handles alarm requests (the
// dispatch is compiled out upstream), so the adapter cannot serve it.
func TestSysIOBACnetGetAlarmSummaryRejected(t *testing.T) {
	_, peer := startSysIOFixture(t, "device-baseline-v3")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 10*time.Second)
	defer cancel()
	_, err := c.GetAlarmSummary(ctx, peer.target)
	sysioRequireReject(t, "GetAlarmSummary", err, bacnet.RejectReasonUnrecognizedService)
	if _, err := c.GetEventInformation(ctx, peer.target, service.GetEventInformationRequest{}); err != nil {
		t.Fatalf("GetEventInformation on the same peer: %v", err)
	}
}
