// SPDX-License-Identifier: MIT

package e2e_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bbmd"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/server"
	"github.com/otfabric/go-bacnet/server/object"
	"github.com/otfabric/go-bacnet/service"
)

const serverInstance = 4321

var (
	av1 = bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	ai1 = bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogInput, Instance: 1}
	bv1 = bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeBinaryValue, Instance: 1}
	ms1 = bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeMultiStateValue, Instance: 1}
	dev = bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: serverInstance}
)

type testServer struct {
	*server.Server
	dev *object.Device
	ai  *object.Point
}

// startServer runs a go-bacnet server on loopback with a small object
// model: AV-1 (commandable), AI-1, BV-1, MSV-1.
func startServer(t *testing.T, opts ...server.Option) *testServer {
	t.Helper()
	d, err := object.NewDevice(object.DeviceConfig{
		Instance: serverInstance, Name: "e2e-server", VendorID: 999, VendorName: "otfabric",
		ModelName: "go-bacnet server", FirmwareRevision: "e2e", ApplicationSoftwareVersion: "e2e",
	})
	if err != nil {
		t.Fatal(err)
	}
	av, err := object.NewAnalogValue(object.PointConfig{Instance: 1, Name: "AV-1", Units: 62, Value: 21.5, Commandable: true})
	if err != nil {
		t.Fatal(err)
	}
	ai, err := object.NewAnalogInput(object.PointConfig{Instance: 1, Name: "AI-1", Units: 62, Value: 10, COVIncrement: 1})
	if err != nil {
		t.Fatal(err)
	}
	bv, err := object.NewBinaryValue(object.PointConfig{Instance: 1, Name: "BV-1", ActiveText: "on", InactiveText: "off"})
	if err != nil {
		t.Fatal(err)
	}
	ms, err := object.NewMultiStateValue(object.PointConfig{Instance: 1, Name: "MSV-1", NumberOfStates: 3, StateText: []string{"a", "b", "c"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range []object.Object{av, ai, bv, ms} {
		if err := d.Add(o); err != nil {
			t.Fatal(err)
		}
	}
	all := []server.Option{server.WithLocalAddr("127.0.0.1:0"), server.WithTransactionOptions(500*time.Millisecond, 1, 500*time.Millisecond)}
	if os.Getenv(framesDirEnv) != "" {
		tr, err := bip.ListenUDP(bip.UDPConfig{LocalAddr: "127.0.0.1:0"})
		if err != nil {
			t.Fatal(err)
		}
		// A test that passes its own transport overrides this one.
		t.Cleanup(func() { _ = tr.Close() })
		all = append(all, server.WithTransport(recordFrames(t, tr)))
	}
	all = append(all, opts...)
	s, err := server.New(d, all...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return &testServer{Server: s, dev: d, ai: ai}
}

// framesDirEnv names a directory that receives every datagram the server
// sends, one file of hex lines per test. The dissector suite in interop/
// sets it and passes the frames to Wireshark.
const framesDirEnv = "BACNET_SERVER_FRAMES_DIR"

// frameRecorder copies each datagram the server sends. It is per test: no
// package-level state.
type frameRecorder struct {
	bip.Transport
	mu     sync.Mutex
	frames [][]byte
}

func (r *frameRecorder) Send(ctx context.Context, pkt bip.OutboundPacket) error {
	r.mu.Lock()
	r.frames = append(r.frames, append([]byte(nil), pkt.Data...))
	r.mu.Unlock()
	return r.Transport.Send(ctx, pkt)
}

// recordFrames wraps tr when framesDirEnv is set and writes the frames when
// the test ends; otherwise it returns tr unchanged.
func recordFrames(t *testing.T, tr bip.Transport) bip.Transport {
	t.Helper()
	dir := os.Getenv(framesDirEnv)
	if dir == "" {
		return tr
	}
	r := &frameRecorder{Transport: tr}
	t.Cleanup(func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		var b strings.Builder
		for _, f := range r.frames {
			b.WriteString(hex.EncodeToString(f))
			b.WriteByte('\n')
		}
		name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()) + ".hex"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(b.String()), 0o600); err != nil {
			t.Error(err)
		}
	})
	return r
}

func serverTarget(s *testServer) client.Target {
	return client.Target{Endpoint: s.Local(), MaxAPDU: 1476}
}

func TestServerDiscovery(t *testing.T) {
	s := startServer(t)
	c := newClient(t)
	ctx := ctxT(t)
	lo, hi := uint32(4000), uint32(5000)
	if err := c.SendWhoIs(ctx, s.Local(), false, client.DiscoveryOptions{LowLimit: &lo, HighLimit: &hi}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		for _, o := range c.Devices() {
			if o.Instance == serverInstance {
				return o.Capabilities.MaxAPDULengthAccepted.Value == 1476 && o.Capabilities.Segmentation.Value == 0
			}
		}
		return false
	}, "I-Am")
	name := bacnet.CharacterString{Value: "AV-1"}
	if err := c.SendWhoHas(ctx, s.Local(), false, client.WhoHasOptions{Name: &name}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		for _, o := range c.Objects() {
			if o.Object == av1 && o.DeviceInstance == serverInstance {
				return true
			}
		}
		return false
	}, "I-Have")
}

func TestServerReadWrite(t *testing.T) {
	s := startServer(t)
	c := newClient(t)
	ctx := ctxT(t)
	tg := serverTarget(s)

	v, err := c.ReadProperty(ctx, tg, av1, presentValue)
	if err != nil || v.Real != 21.5 {
		t.Fatalf("read AV-1: %v %+v", err, v)
	}
	// The wildcard Device instance reads the server's own Device object.
	v, err = c.ReadProperty(ctx, tg, bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: bacnet.WildcardInstance}, bacnet.PropertyReference{Identifier: bacnet.PropertyObjectIdentifier})
	if err != nil || v.ObjectID != dev {
		t.Fatalf("wildcard device: %v %+v", err, v)
	}
	// Commandable write at priority 8, then relinquish.
	p8 := uint8(8)
	if err := c.WriteProperty(ctx, tg, av1, presentValue, bacnet.RealValue(42), &p8); err != nil {
		t.Fatal(err)
	}
	if v, _ := c.ReadProperty(ctx, tg, av1, presentValue); v.Real != 42 {
		t.Fatalf("after write: %+v", v)
	}
	idx := uint32(8)
	if v, err := c.ReadProperty(ctx, tg, av1, bacnet.PropertyReference{Identifier: bacnet.PropertyPriorityArray, ArrayIndex: &idx}); err != nil || v.Real != 42 {
		t.Fatalf("priority array [8]: %v %+v", err, v)
	}
	if err := c.WriteProperty(ctx, tg, av1, presentValue, bacnet.NullValue(), &p8); err != nil {
		t.Fatal(err)
	}
	if v, _ := c.ReadProperty(ctx, tg, av1, presentValue); v.Real != 21.5 {
		t.Fatalf("after relinquish: %+v", v)
	}
	// Object_List length and first entry.
	zero := uint32(0)
	if v, err := c.ReadProperty(ctx, tg, dev, bacnet.PropertyReference{Identifier: bacnet.PropertyObjectList, ArrayIndex: &zero}); err != nil || v.Unsigned != 5 {
		t.Fatalf("object list length: %v %+v", err, v)
	}
}

func remoteError(t *testing.T, err error, class, code uint16) {
	t.Helper()
	var e *bacnet.ErrorResponse
	if !errors.As(err, &e) || e.Class != class || e.Code != code {
		t.Fatalf("got %v, want %s/%s", err, bacnet.ErrorClassName(class), bacnet.ErrorCodeName(code))
	}
}

func TestServerErrors(t *testing.T) {
	s := startServer(t)
	c := newClient(t)
	ctx := ctxT(t)
	tg := serverTarget(s)

	_, err := c.ReadProperty(ctx, tg, bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 99}, presentValue)
	remoteError(t, err, bacnet.ErrorClassObject, bacnet.ErrorCodeUnknownObject)
	_, err = c.ReadProperty(ctx, tg, av1, bacnet.PropertyReference{Identifier: bacnet.PropertyStateText})
	remoteError(t, err, bacnet.ErrorClassProperty, bacnet.ErrorCodeUnknownProperty)
	idx := uint32(1)
	_, err = c.ReadProperty(ctx, tg, av1, bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue, ArrayIndex: &idx})
	remoteError(t, err, bacnet.ErrorClassProperty, bacnet.ErrorCodePropertyIsNotAnArray)
	idx = 17
	_, err = c.ReadProperty(ctx, tg, av1, bacnet.PropertyReference{Identifier: bacnet.PropertyPriorityArray, ArrayIndex: &idx})
	remoteError(t, err, bacnet.ErrorClassProperty, bacnet.ErrorCodeInvalidArrayIndex)
	// Wrong datatype, out of range, read-only, input not out of service.
	err = c.WriteProperty(ctx, tg, av1, presentValue, bacnet.UnsignedValue(1), nil)
	remoteError(t, err, bacnet.ErrorClassProperty, bacnet.ErrorCodeInvalidDataType)
	err = c.WriteProperty(ctx, tg, ms1, presentValue, bacnet.UnsignedValue(4), nil)
	remoteError(t, err, bacnet.ErrorClassProperty, bacnet.ErrorCodeValueOutOfRange)
	err = c.WriteProperty(ctx, tg, av1, bacnet.PropertyReference{Identifier: bacnet.PropertyUnits}, bacnet.EnumValue(1), nil)
	remoteError(t, err, bacnet.ErrorClassProperty, bacnet.ErrorCodeWriteAccessDenied)
	err = c.WriteProperty(ctx, tg, ai1, presentValue, bacnet.RealValue(1), nil)
	remoteError(t, err, bacnet.ErrorClassProperty, bacnet.ErrorCodeWriteAccessDenied)
	if err := c.WriteProperty(ctx, tg, ai1, bacnet.PropertyReference{Identifier: bacnet.PropertyOutOfService}, bacnet.BoolValue(true), nil); err != nil {
		t.Fatal(err)
	}
	if err := c.WriteProperty(ctx, tg, ai1, presentValue, bacnet.RealValue(1), nil); err != nil {
		t.Fatalf("out-of-service input write: %v", err)
	}
	// Services the server does not serve are rejected.
	err = c.LifeSafetyOperation(ctx, tg, service.LifeSafetyOperationRequest{RequestingProcessIdentifier: 1, RequestingSource: "e2e", Request: 1})
	var rj *bacnet.RejectError
	if !errors.As(err, &rj) || rj.Reason != bacnet.RejectReasonUnrecognizedService {
		t.Fatalf("life safety operation: %v", err)
	}
}

func TestServerReadPropertyMultiple(t *testing.T) {
	s := startServer(t)
	c := newClient(t)
	ctx := ctxT(t)
	res, err := c.ReadPropertyMultiple(ctx, serverTarget(s), []service.ReadAccessSpecification{
		{Object: av1, Properties: []bacnet.PropertyReference{{Identifier: bacnet.PropertyAll}}},
		{Object: dev, Properties: []bacnet.PropertyReference{{Identifier: bacnet.PropertyRequired}}},
		{Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 7}, Properties: []bacnet.PropertyReference{{Identifier: bacnet.PropertyPresentValue}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 3 {
		t.Fatalf("%d results", len(res))
	}
	found := map[bacnet.PropertyIdentifier]bool{}
	for _, p := range res[0].Properties {
		if p.Err != nil {
			t.Errorf("AV-1 %v: %v", p.Property.Identifier, p.Err)
		}
		found[p.Property.Identifier] = true
	}
	for _, id := range []bacnet.PropertyIdentifier{bacnet.PropertyObjectName, bacnet.PropertyPresentValue, bacnet.PropertyPriorityArray, bacnet.PropertyStatusFlags, bacnet.PropertyUnits, bacnet.PropertyPropertyList} {
		if !found[id] {
			t.Errorf("ALL lacks %v", id)
		}
	}
	for _, p := range res[1].Properties {
		if p.Err != nil {
			t.Errorf("Device %v: %v", p.Property.Identifier, p.Err)
		}
	}
	if len(res[1].Properties) < 20 {
		t.Errorf("Device REQUIRED returned %d properties", len(res[1].Properties))
	}
	var e *bacnet.ErrorResponse
	if !errors.As(res[2].Properties[0].Err, &e) || e.Code != bacnet.ErrorCodeUnknownObject {
		t.Errorf("unknown object: %v", res[2].Properties[0].Err)
	}
}

func TestServerWritePropertyMultiple(t *testing.T) {
	s := startServer(t)
	c := newClient(t)
	ctx := ctxT(t)
	tg := serverTarget(s)
	err := c.WritePropertyMultiple(ctx, tg, []service.WriteAccessSpecification{
		{Object: av1, Properties: []service.WritePropertyValue{{Property: presentValue, Value: bacnet.RealValue(5)}}},
		{Object: bv1, Properties: []service.WritePropertyValue{{Property: presentValue, Value: bacnet.EnumValue(1)}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// The second write fails: the first stays applied, the third is not tried.
	err = c.WritePropertyMultiple(ctx, tg, []service.WriteAccessSpecification{
		{Object: av1, Properties: []service.WritePropertyValue{
			{Property: presentValue, Value: bacnet.RealValue(6)},
			{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyUnits}, Value: bacnet.EnumValue(1)},
			{Property: presentValue, Value: bacnet.RealValue(7)},
		}},
	})
	var wpm *service.WritePropertyMultipleError
	if !errors.As(err, &wpm) || wpm.FirstFailed != av1 || wpm.FirstProperty.Identifier != bacnet.PropertyUnits || wpm.Code != bacnet.ErrorCodeWriteAccessDenied {
		t.Fatalf("got %v", err)
	}
	if v, _ := c.ReadProperty(ctx, tg, av1, presentValue); v.Real != 6 {
		t.Fatalf("AV-1 = %+v, want 6", v)
	}
}

func TestServerCOV(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		s := startServer(t)
		c := newClient(t)
		ctx := ctxT(t)
		sub, err := c.SubscribeCOV(ctx, serverTarget(s), ai1, client.COVOptions{Lifetime: 60, IssueConfirmed: confirmed})
		if err != nil {
			t.Fatal(err)
		}
		next := func() service.COVNotification {
			t.Helper()
			deadline := time.After(3 * time.Second)
			for {
				select {
				case ev := <-sub.Events():
					if ev.Err != nil {
						t.Fatalf("subscription error: %v", ev.Err)
					}
					if ev.Notification != nil {
						return *ev.Notification
					}
				case <-deadline:
					t.Fatal("no COV notification")
					return service.COVNotification{}
				}
			}
		}
		n := next() // initial
		if n.MonitoredObject != ai1 || n.InitiatingDevice != dev || len(n.Values) != 2 || n.Values[0].Value.Real != 10 {
			t.Fatalf("initial: %+v", n)
		}
		// Below the COV increment of 1: no notification; above: one.
		_ = s.ai.SetPresentValue(bacnet.RealValue(10.5))
		_ = s.ai.SetPresentValue(bacnet.RealValue(11.2))
		if n := next(); n.Values[0].Value.Real != 11.2 {
			t.Fatalf("after change: %+v", n)
		}
		quiet := time.After(200 * time.Millisecond)
	drain:
		for {
			select {
			case ev := <-sub.Events():
				if ev.Notification != nil {
					t.Fatalf("unexpected notification %+v", ev.Notification)
				}
			case <-quiet:
				break drain
			}
		}
		if err := sub.Close(); err != nil {
			t.Fatal(err)
		}
		_ = s.ai.SetPresentValue(bacnet.RealValue(20))
	}
}

var _ = context.Background

func startLogServer(t *testing.T, opts ...server.Option) (*testServer, *object.TrendLog, time.Time) {
	t.Helper()
	s := startServer(t, opts...)
	tl, err := object.NewTrendLog(object.TrendLogConfig{Instance: 1, Name: "TL-1", BufferSize: 500, Enable: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.dev.Add(tl); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.Local)
	for i := 0; i < 400; i++ {
		_ = tl.Log(base.Add(time.Duration(i)*time.Second), bacnet.RealValue(float32(i)), nil)
	}
	return s, tl, base
}

func TestServerReadRange(t *testing.T) {
	s, tl, base := startLogServer(t)
	c := newClient(t)
	ctx := ctxT(t)
	tg := serverTarget(s)
	id := tl.Identifier()
	lb := bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer}

	_, err := c.ReadProperty(ctx, tg, id, lb)
	remoteError(t, err, bacnet.ErrorClassProperty, bacnet.ErrorCodeReadAccessDenied)
	// A segmented reply carries all 400 records.
	ack, err := c.ReadRange(ctx, tg, service.ReadRangeRequest{Object: id, Property: lb, By: service.ReadRangeByPosition, ReferenceIndex: 1, Count: 400})
	if err != nil || ack.ItemCount != 400 || !ack.FirstItem() || !ack.LastItem() || ack.MoreItems() {
		t.Fatalf("by position: %v count %d flags %v", err, ack.ItemCount, ack.ResultFlags)
	}
	// Without segmentation the reply is cut to one APDU and says so.
	ns, _, _ := startLogServer(t, server.WithSegmentation(0, 0))
	ack, err = c.ReadRange(ctx, serverTarget(ns), service.ReadRangeRequest{Object: id, Property: lb, By: service.ReadRangeByPosition, ReferenceIndex: 1, Count: 400})
	if err != nil || ack.ItemCount == 0 || ack.ItemCount >= 400 || !ack.MoreItems() || ack.LastItem() {
		t.Fatalf("truncated: %v count %d flags %v", err, ack.ItemCount, ack.ResultFlags)
	}
	// By sequence number names the first sequence; by time starts after
	// the reference.
	ack, err = c.ReadRange(ctx, tg, service.ReadRangeRequest{Object: id, Property: lb, By: service.ReadRangeBySequenceNumber, ReferenceIndex: 100, Count: 10})
	if err != nil || ack.ItemCount != 10 || ack.FirstSequence == nil || *ack.FirstSequence != 100 || ack.LogRecords[0].Datum.Real != 99 {
		t.Fatalf("by sequence: %v %+v", err, ack)
	}
	ref := base.Add(9 * time.Second)
	ack, err = c.ReadRange(ctx, tg, service.ReadRangeRequest{Object: id, Property: lb, By: service.ReadRangeByTime,
		ReferenceTime: service.DateTime{Date: bacnet.Date{Year: uint8(ref.Year() - 1900), Month: uint8(ref.Month()), Day: uint8(ref.Day()), Weekday: 7}, Time: bacnet.Time{Hour: 0, Minute: 0, Second: 9}}, Count: 5})
	if err != nil || ack.ItemCount != 5 || ack.LogRecords[0].Datum.Real != 10 || ack.FirstSequence == nil || *ack.FirstSequence != 11 {
		t.Fatalf("by time: %v %+v", err, ack)
	}
	// The client's pager walks the log page by page.
	all, err := c.ReadRangeAll(ctx, tg, service.ReadRangeRequest{Object: id, Property: lb, By: service.ReadRangeByPosition, ReferenceIndex: 1}, client.ReadRangePageOptions{PageCount: 64})
	if err != nil || len(all.LogRecords) != 400 || all.Partial {
		t.Fatalf("pager: %v %d %+v", err, len(all.LogRecords), all.StoppedReason)
	}
	_, err = c.ReadRange(ctx, tg, service.ReadRangeRequest{Object: av1, Property: presentValue, By: service.ReadRangeByPosition, ReferenceIndex: 1, Count: 1})
	remoteError(t, err, bacnet.ErrorClassServices, bacnet.ErrorCodePropertyIsNotAList)
	_, err = c.ReadRange(ctx, tg, service.ReadRangeRequest{Object: av1, Property: bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer}, By: service.ReadRangeByPosition, ReferenceIndex: 1, Count: 1})
	remoteError(t, err, bacnet.ErrorClassProperty, bacnet.ErrorCodeUnknownProperty)
}

func TestServerFiles(t *testing.T) {
	s := startServer(t)
	content := bytes.Repeat([]byte("0123456789"), 300) // 3000 octets: segmented reads
	sf, err := object.NewFile(object.FileConfig{Instance: 1, Name: "stream", FileType: "bin", Data: content})
	if err != nil {
		t.Fatal(err)
	}
	rf, err := object.NewFile(object.FileConfig{Instance: 2, Name: "records", Record: true, Records: [][]byte{[]byte("one"), []byte("two")}})
	if err != nil {
		t.Fatal(err)
	}
	ro, err := object.NewFile(object.FileConfig{Instance: 3, Name: "ro", ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range []object.Object{sf, rf, ro} {
		if err := s.dev.Add(o); err != nil {
			t.Fatal(err)
		}
	}
	c := newClient(t)
	ctx := ctxT(t)
	tg := serverTarget(s)

	got, err := c.ReadFileStream(ctx, tg, sf.Identifier(), client.FileReadOptions{ChunkSize: 2000})
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("stream read: %v (%d octets)", err, len(got))
	}
	if _, err := c.WriteFileStream(ctx, tg, sf.Identifier(), -1, []byte("tail"), client.FileWriteOptions{}); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(sf.Data(), []byte("9tail")) {
		t.Fatal("append not applied")
	}
	recs, err := c.ReadFileRecords(ctx, tg, rf.Identifier(), 0, client.FileReadOptions{})
	if err != nil || len(recs) != 2 || string(recs[1]) != "two" {
		t.Fatalf("records: %v %q", err, recs)
	}
	ack, err := c.AtomicWriteFile(ctx, tg, service.AtomicWriteFileRequest{File: rf.Identifier(), Access: service.FileAccessRecord, StartPosition: -1, Records: [][]byte{[]byte("three")}})
	if err != nil || ack.StartPosition != 2 || len(rf.Records()) != 3 {
		t.Fatalf("record append: %v %+v", err, ack)
	}

	_, err = c.AtomicReadFile(ctx, tg, service.AtomicReadFileRequest{File: rf.Identifier(), Access: service.FileAccessStream, Count: 1})
	remoteError(t, err, bacnet.ErrorClassServices, bacnet.ErrorCodeInvalidFileAccessMethod)
	_, err = c.AtomicReadFile(ctx, tg, service.AtomicReadFileRequest{File: sf.Identifier(), Access: service.FileAccessStream, StartPosition: 999999, Count: 1})
	remoteError(t, err, bacnet.ErrorClassServices, bacnet.ErrorCodeInvalidFileStartPosition)
	_, err = c.AtomicReadFile(ctx, tg, service.AtomicReadFileRequest{File: av1, Access: service.FileAccessStream, Count: 1})
	remoteError(t, err, bacnet.ErrorClassServices, bacnet.ErrorCodeInconsistentObjectType)
	_, err = c.AtomicReadFile(ctx, tg, service.AtomicReadFileRequest{File: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 99}, Access: service.FileAccessStream, Count: 1})
	remoteError(t, err, bacnet.ErrorClassObject, bacnet.ErrorCodeUnknownObject)
	_, err = c.AtomicWriteFile(ctx, tg, service.AtomicWriteFileRequest{File: ro.Identifier(), Access: service.FileAccessStream, Data: []byte("x")})
	remoteError(t, err, bacnet.ErrorClassObject, bacnet.ErrorCodeFileAccessDenied)
	_, err = c.AtomicWriteFile(ctx, tg, service.AtomicWriteFileRequest{File: av1, Access: service.FileAccessStream, Data: []byte("x")})
	remoteError(t, err, bacnet.ErrorClassServices, bacnet.ErrorCodeInconsistentObjectType)
}

func TestServerListsAndObjects(t *testing.T) {
	s := startServer(t)
	nc, err := object.NewNotificationClass(object.NotificationClassConfig{Instance: 1, Name: "NC-1", MaxRecipients: 2})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.dev.Add(nc); err != nil {
		t.Fatal(err)
	}
	s.dev.RegisterFactory(bacnet.ObjectTypeAnalogValue, func(instance uint32, name string) (object.Object, error) {
		return object.NewAnalogValue(object.PointConfig{Instance: instance, Name: name, Units: 95})
	})
	c := newClient(t)
	ctx := ctxT(t)
	tg := serverTarget(s)
	rl := bacnet.PropertyReference{Identifier: bacnet.PropertyRecipientList}
	peerDev := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 77}
	d1 := object.Destination{ValidDays: 0x7F, To: bacnet.Time{Hour: 23, Minute: 59, Second: 59}, Device: &peerDev, ProcessID: 1, Transitions: 7}
	d2 := d1
	d2.ProcessID = 2
	d3 := d1
	d3.ProcessID = 3

	if err := c.AddListElement(ctx, tg, service.ListElementRequest{Object: nc.Identifier(), Property: rl, Elements: append(d1.Elements(), d2.Elements()...)}); err != nil {
		t.Fatal(err)
	}
	v, err := c.ReadProperty(ctx, tg, nc.Identifier(), rl)
	if err != nil {
		t.Fatal(err)
	}
	if ds, _, err := object.ParseDestinations(v.Elements); err != nil || len(ds) != 2 || ds[1].ProcessID != 2 {
		t.Fatalf("Recipient_List: %v %+v", err, ds)
	}
	err = c.AddListElement(ctx, tg, service.ListElementRequest{Object: nc.Identifier(), Property: rl, Elements: d3.Elements()})
	remoteError(t, err, bacnet.ErrorClassResources, bacnet.ErrorCodeNoSpaceToAddListElement)
	err = c.RemoveListElement(ctx, tg, service.ListElementRequest{Object: nc.Identifier(), Property: rl, Elements: d3.Elements()})
	remoteError(t, err, bacnet.ErrorClassServices, bacnet.ErrorCodeListElementNotFound)
	if err := c.RemoveListElement(ctx, tg, service.ListElementRequest{Object: nc.Identifier(), Property: rl, Elements: d1.Elements()}); err != nil {
		t.Fatal(err)
	}
	err = c.AddListElement(ctx, tg, service.ListElementRequest{Object: av1, Property: presentValue, Elements: d1.Elements()})
	remoteError(t, err, bacnet.ErrorClassServices, bacnet.ErrorCodePropertyIsNotAList)
	err = c.AddListElement(ctx, tg, service.ListElementRequest{Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeNotificationClass, Instance: 9}, Property: rl, Elements: d1.Elements()})
	remoteError(t, err, bacnet.ErrorClassObject, bacnet.ErrorCodeUnknownObject)

	// CreateObject with initial values, then DeleteObject.
	avType := bacnet.ObjectTypeAnalogValue
	ack, err := c.CreateObject(ctx, tg, service.CreateObjectRequest{ObjectType: &avType, InitialValues: []service.PropertyValue{
		{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName}, Value: bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: "made"}}},
		{Property: presentValue, Value: bacnet.RealValue(7)},
	}})
	if err != nil || ack.Object.Type != avType || ack.Object.Instance != 0 {
		t.Fatalf("create: %v %+v", err, ack)
	}
	if v, err := c.ReadProperty(ctx, tg, ack.Object, presentValue); err != nil || v.Real != 7 {
		t.Fatalf("created PV: %v %+v", err, v)
	}
	if v, err := c.ReadProperty(ctx, tg, ack.Object, bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName}); err != nil || v.Character.Value != "made" {
		t.Fatalf("created name: %v %+v", err, v)
	}
	// A failing initial value names its position and leaves no object.
	inst := uint32(5)
	_, err = c.CreateObject(ctx, tg, service.CreateObjectRequest{ObjectIdentifier: &bacnet.ObjectIdentifier{Type: avType, Instance: inst}, InitialValues: []service.PropertyValue{
		{Property: presentValue, Value: bacnet.RealValue(1)},
		{Property: presentValue, Value: bacnet.BoolValue(true)},
	}})
	remoteError(t, err, bacnet.ErrorClassProperty, bacnet.ErrorCodeInvalidDataType)
	if _, ok := s.dev.Object(bacnet.ObjectIdentifier{Type: avType, Instance: inst}); ok {
		t.Fatal("failed create left the object")
	}
	_, err = c.CreateObject(ctx, tg, service.CreateObjectRequest{ObjectIdentifier: &av1})
	remoteError(t, err, bacnet.ErrorClassObject, bacnet.ErrorCodeObjectIdentifierAlreadyExists)
	bvType := bacnet.ObjectTypeBinaryValue
	_, err = c.CreateObject(ctx, tg, service.CreateObjectRequest{ObjectType: &bvType})
	remoteError(t, err, bacnet.ErrorClassObject, bacnet.ErrorCodeDynamicCreationNotSupported)
	lsType := bacnet.ObjectType(12) // Loop
	_, err = c.CreateObject(ctx, tg, service.CreateObjectRequest{ObjectType: &lsType})
	remoteError(t, err, bacnet.ErrorClassObject, bacnet.ErrorCodeUnsupportedObjectType)

	if err := c.DeleteObject(ctx, tg, service.DeleteObjectRequest(ack)); err != nil {
		t.Fatal(err)
	}
	_, err = c.ReadProperty(ctx, tg, ack.Object, presentValue)
	remoteError(t, err, bacnet.ErrorClassObject, bacnet.ErrorCodeUnknownObject)
	err = c.DeleteObject(ctx, tg, service.DeleteObjectRequest{Object: av1})
	remoteError(t, err, bacnet.ErrorClassObject, bacnet.ErrorCodeObjectDeletionNotPermitted)
	err = c.DeleteObject(ctx, tg, service.DeleteObjectRequest{Object: dev})
	remoteError(t, err, bacnet.ErrorClassObject, bacnet.ErrorCodeObjectDeletionNotPermitted)
	err = c.DeleteObject(ctx, tg, service.DeleteObjectRequest(ack))
	remoteError(t, err, bacnet.ErrorClassObject, bacnet.ErrorCodeUnknownObject)
}

func TestServerAlarms(t *testing.T) {
	s := startServer(t)
	var mu sync.Mutex
	var got []client.EventNotificationDelivery
	c, err := client.New(
		client.WithLocalAddr("127.0.0.1:0"),
		client.WithTransactionOptions(300*time.Millisecond, 0, time.Second),
		client.WithEventNotificationHandler(func(d client.EventNotificationDelivery) {
			mu.Lock()
			got = append(got, d)
			mu.Unlock()
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	ctx := ctxT(t)
	tg := serverTarget(s)

	// The recipient is the client's address, confirmed notifications, all
	// transitions, all day.
	lp := c.LocalEndpoint().Addr
	ip := lp.Addr().As4()
	mac := []byte{ip[0], ip[1], ip[2], ip[3], byte(lp.Port() >> 8), byte(lp.Port())}
	nc, err := object.NewNotificationClass(object.NotificationClassConfig{
		Instance: 7, Name: "NC-7", Priority: &[3]uint8{100, 50, 200}, AckRequired: 1 << object.TransitionToOffNormal,
		Recipients: []object.Destination{{ValidDays: 0x7F, To: bacnet.Time{Hour: 23, Minute: 59, Second: 59, Hundredths: 99}, MAC: mac, ProcessID: 42, Confirmed: true, Transitions: 7}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ai, err := object.NewAnalogInput(object.PointConfig{Instance: 9, Name: "AI-9", Value: 50, Events: &object.EventConfig{
		NotificationClass: 7, EventEnable: 7, NotifyType: object.NotifyAlarm, HighLimit: 80, LowLimit: 20, Deadband: 2,
		LimitEnable: object.LimitEnableHigh | object.LimitEnableLow,
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range []object.Object{nc, ai} {
		if err := s.dev.Add(o); err != nil {
			t.Fatal(err)
		}
	}
	notes := func() []service.EventNotification {
		mu.Lock()
		defer mu.Unlock()
		out := make([]service.EventNotification, len(got))
		for i, d := range got {
			out[i] = d.Notification
		}
		return out
	}

	_ = ai.SetPresentValue(bacnet.RealValue(95))
	waitFor(t, func() bool { return len(notes()) == 1 }, "to-offnormal notification")
	n := notes()[0]
	if n.ProcessIdentifier != 42 || n.EventObject != ai.Identifier() || n.ToState != object.EventStateHighLimit || n.Priority != 100 ||
		n.EventType != object.EventTypeOutOfRange || n.AckRequired == nil || !*n.AckRequired || n.FromState == nil || *n.FromState != object.EventStateNormal {
		t.Fatalf("notification: %+v", n)
	}

	sum, err := c.GetAlarmSummary(ctx, tg)
	if err != nil || len(sum.Entries) != 1 || sum.Entries[0].AlarmState != object.EventStateHighLimit {
		t.Fatalf("alarm summary: %v %+v", err, sum)
	}
	info, err := c.GetEventInformation(ctx, tg, service.GetEventInformationRequest{})
	if err != nil || len(info.Summaries) != 1 || info.MoreEvents || info.Summaries[0].Object != ai.Identifier() {
		t.Fatalf("event information: %v %+v", err, info)
	}
	last := ai.Identifier()
	if info, err := c.GetEventInformation(ctx, tg, service.GetEventInformationRequest{LastReceived: &last}); err != nil || len(info.Summaries) != 0 {
		t.Fatalf("after last: %v %+v", err, info)
	}
	notAcked := service.EnrollmentFilter(2)
	enr, err := c.GetEnrollmentSummary(ctx, tg, service.GetEnrollmentSummaryRequest{AcknowledgmentFilter: notAcked})
	if err != nil || len(enr.Entries) != 1 || enr.Entries[0].Priority != 100 || enr.Entries[0].NotificationClass != 7 {
		t.Fatalf("enrollment: %v %+v", err, enr)
	}
	class := uint32(8)
	if enr, err := c.GetEnrollmentSummary(ctx, tg, service.GetEnrollmentSummaryRequest{NotificationClassFilter: &class}); err != nil || len(enr.Entries) != 0 {
		t.Fatalf("enrollment class filter: %v %+v", err, enr)
	}

	ackReq := service.AcknowledgeAlarmRequest{ProcessIdentifier: 42, EventObject: ai.Identifier(), EventStateAcked: object.EventStateHighLimit,
		TimeStamp: n.TimeStamp, AcknowledgmentSource: bacnet.CharacterString{Value: "e2e"}, TimeOfAcknowledgment: object.TimeStampOf(time.Now())}
	if err := c.AcknowledgeAlarm(ctx, tg, ackReq); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return len(notes()) == 2 }, "ack notification")
	if a := notes()[1]; a.NotifyType != object.NotifyAckNotification || a.FromState != nil || a.AckRequired != nil {
		t.Fatalf("ack notification: %+v", a)
	}
	bad := ackReq
	bad.EventStateAcked = object.EventStateFault
	remoteError(t, c.AcknowledgeAlarm(ctx, tg, bad), bacnet.ErrorClassServices, bacnet.ErrorCodeInvalidEventState)
	bad = ackReq
	bad.EventObject = av1
	remoteError(t, c.AcknowledgeAlarm(ctx, tg, bad), bacnet.ErrorClassObject, bacnet.ErrorCodeNoAlarmConfigured)
	bad.EventObject = bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogInput, Instance: 99}
	remoteError(t, c.AcknowledgeAlarm(ctx, tg, bad), bacnet.ErrorClassObject, bacnet.ErrorCodeUnknownObject)

	// Back to normal: to-normal needs no ack, and the summaries empty.
	_ = ai.SetPresentValue(bacnet.RealValue(50))
	waitFor(t, func() bool { return len(notes()) == 3 }, "to-normal notification")
	if n := notes()[2]; n.ToState != object.EventStateNormal || n.Priority != 200 || *n.AckRequired {
		t.Fatalf("to-normal: %+v", n)
	}
	if sum, _ := c.GetAlarmSummary(ctx, tg); len(sum.Entries) != 0 {
		t.Fatalf("summary after normal: %+v", sum)
	}
	if v, err := c.ReadProperty(ctx, tg, ai.Identifier(), bacnet.PropertyReference{Identifier: bacnet.PropertyEventState}); err != nil || v.Enumerated != object.EventStateNormal {
		t.Fatalf("Event_State: %v %+v", err, v)
	}
}

func TestServerBBMDForeignDevice(t *testing.T) {
	tr, err := bip.ListenUDP(bip.UDPConfig{LocalAddr: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := bbmd.New(bbmd.Config{Self: tr.Local()})
	if err != nil {
		t.Fatal(err)
	}
	s := startServer(t, server.WithTransport(recordFrames(t, tr)), server.WithBBMD(b))
	c, err := client.New(
		client.WithLocalAddr("127.0.0.1:0"),
		client.WithTransactionOptions(300*time.Millisecond, 0, time.Second),
		client.WithForeignDevice(client.ForeignDeviceConfig{BBMD: s.Local(), TTL: time.Minute}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	waitFor(t, func() bool { return len(b.FDT()) == 1 }, "foreign device registration")
	if got := b.FDT()[0].Address; !got.Equal(c.LocalEndpoint()) {
		t.Fatalf("FDT entry %v, client %v", got, c.LocalEndpoint())
	}
	// A broadcast Who-Is from the foreign device reaches the server
	// through Distribute-Broadcast-To-Network; its broadcast I-Am comes
	// back to the foreign device as a Forwarded-NPDU.
	ctx := ctxT(t)
	if err := c.SendWhoIs(ctx, bip.Endpoint{}, true, client.DiscoveryOptions{}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		for _, d := range c.Devices() {
			if d.Instance == serverInstance {
				return true
			}
		}
		return false
	}, "I-Am through the BBMD")
	if v, err := c.ReadProperty(ctx, serverTarget(s), av1, presentValue); err != nil || v.Real != 21.5 {
		t.Fatalf("read through BBMD peer: %v %+v", err, v)
	}
}
