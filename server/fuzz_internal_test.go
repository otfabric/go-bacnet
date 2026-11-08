// SPDX-License-Identifier: MIT

package server

import (
	"net/netip"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet/internal/capture"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/internal/virtual"
	"github.com/otfabric/go-bacnet/npdu"
	"github.com/otfabric/go-bacnet/server/object"
	"github.com/otfabric/go-bacnet/service"
)

var fuzzPeer = bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.2:47808"))

// fuzzDevice serves one object of each kind the handlers act on.
func fuzzDevice(tb testing.TB) *object.Device {
	tb.Helper()
	dev, err := object.NewDevice(object.DeviceConfig{Instance: 100, Name: "dev", VendorID: 999})
	if err != nil {
		tb.Fatal(err)
	}
	dev.RegisterFactory(bacnet.ObjectTypeAnalogValue, func(instance uint32, name string) (object.Object, error) {
		return object.NewAnalogValue(object.PointConfig{Instance: instance, Name: name})
	})
	av, err := object.NewAnalogValue(object.PointConfig{Instance: 1, Name: "AV-1", Commandable: true})
	if err != nil {
		tb.Fatal(err)
	}
	ai, err := object.NewAnalogInput(object.PointConfig{Instance: 9, Name: "AI-9", Value: 50, Events: &object.EventConfig{
		NotificationClass: 7, EventEnable: 7, HighLimit: 80, LowLimit: 20, LimitEnable: object.LimitEnableHigh | object.LimitEnableLow,
	}})
	if err != nil {
		tb.Fatal(err)
	}
	nc, err := object.NewNotificationClass(object.NotificationClassConfig{Instance: 7, Name: "NC-7"})
	if err != nil {
		tb.Fatal(err)
	}
	msv, err := object.NewMultiStateValue(object.PointConfig{Instance: 2, Name: "MSV-2", NumberOfStates: 4, Events: &object.EventConfig{
		NotificationClass: 7, EventEnable: 7, AlarmValues: []uint32{3}, FaultValues: []uint32{4},
	}})
	if err != nil {
		tb.Fatal(err)
	}
	f, err := object.NewFile(object.FileConfig{Instance: 1, Name: "F-1", Data: []byte("hello")})
	if err != nil {
		tb.Fatal(err)
	}
	tl, err := object.NewTrendLog(object.TrendLogConfig{Instance: 1, Name: "TL-1", BufferSize: 8, Enable: true})
	if err != nil {
		tb.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		_ = tl.Log(time.Date(2026, 1, 1, 0, i, 0, 0, time.Local), bacnet.RealValue(float32(i)), nil)
	}
	for _, o := range []object.Object{av, ai, nc, msv, f, tl} {
		if err := dev.Add(o); err != nil {
			tb.Fatal(err)
		}
	}
	return dev
}

func frame(tb testing.TB, a []byte) []byte {
	raw, err := npdu.Append(nil, npdu.NPDU{Version: npdu.Version1, ExpectingReply: true, APDU: a})
	if err != nil {
		tb.Fatal(err)
	}
	out, err := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionOriginalUnicastNPDU, Payload: raw})
	if err != nil {
		tb.Fatal(err)
	}
	return out
}

func fuzzServerSeeds(tb testing.TB) [][]byte {
	enc := func(b []byte, err error) []byte {
		if err != nil {
			tb.Fatal(err)
		}
		return b
	}
	conf := func(svc uint8, payload []byte) []byte {
		return frame(tb, apdu.AppendConfirmedRequest(nil, apdu.ConfirmedRequest{SegmentedResponseAccepted: true, MaxSegments: 7, MaxAPDU: 0, InvokeID: 1, ServiceChoice: svc, Payload: payload}))
	}
	file := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 1}
	tl := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeTrendLog, Instance: 1}
	return [][]byte{
		conf(apdu.ServiceReadProperty, enc(service.EncodeReadProperty(service.ReadPropertyRequest{Object: av1, Property: pvRef}))),
		conf(apdu.ServiceReadPropertyMultiple, enc(service.EncodeReadPropertyMultiple([]service.ReadAccessSpecification{{Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 100}, Properties: []bacnet.PropertyReference{{Identifier: bacnet.PropertyAll}}}}))),
		conf(apdu.ServiceWriteProperty, enc(service.EncodeWriteProperty(service.WritePropertyRequest{Object: av1, Property: pvRef, Value: bacnet.RealValue(3)}))),
		conf(apdu.ServiceSubscribeCOV, enc(service.EncodeSubscribeCOV(service.SubscribeCOVRequest{ProcessIdentifier: 1, MonitoredObject: av1, Lifetime: 60}))),
		conf(apdu.ServiceReadRange, enc(service.EncodeReadRange(service.ReadRangeRequest{Object: tl, Property: bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer}}))),
		conf(apdu.ServiceAtomicReadFile, enc(service.EncodeAtomicReadFile(service.AtomicReadFileRequest{File: file, Access: service.FileAccessStream, Count: 5}))),
		conf(apdu.ServiceDeviceCommunicationControl, []byte{0x19, 0x00}),
		conf(apdu.ServiceGetEventInformation, nil),
		conf(apdu.ServiceCreateObject, []byte{0x0E, 0x09, 0x02, 0x0F}),
		frame(tb, apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{ServiceChoice: apdu.ServiceWhoIs})),
		frame(tb, apdu.AppendConfirmedRequest(nil, apdu.ConfirmedRequest{SegmentedMessage: true, MoreFollows: true, ProposedWindowSize: 2, MaxAPDU: 5, InvokeID: 2, ServiceChoice: apdu.ServiceReadProperty, Payload: []byte{0x0C, 0x00}})),
		frame(tb, apdu.AppendSegmentACK(nil, apdu.SegmentACK{InvokeID: 1, SequenceNumber: 0, ActualWindowSize: 4})),
		enc(bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionRegisterForeignDevice, TTL: 60})),
		{0x81, 0x0A, 0x00, 0x04},
	}
}

// FuzzServerReceive feeds two arbitrary datagrams to a serving device: the
// server must not panic, every frame it sends must parse, and Close must
// end every goroutine it started.
func FuzzServerReceive(f *testing.F) {
	for _, s := range capture.LayerSeeds(capture.LayerBVLC, nil) { // recorded devices (testdata/captures)
		f.Add(s, s)
	}
	seeds := fuzzServerSeeds(f)
	for i, s := range seeds {
		f.Add(s, seeds[(i+1)%len(seeds)])
	}
	f.Fuzz(func(t *testing.T, first, second []byte) {
		tr := virtual.New(serverEP, nil, 8)
		s, err := New(fuzzDevice(t), WithTransport(tr), WithTransactionOptions(50*time.Millisecond, 0, 50*time.Millisecond))
		if err != nil {
			t.Fatal(err)
		}
		s.handlePacket(bip.InboundPacket{Data: first, ImmediatePeer: fuzzPeer, ReceivedAt: time.Now()})
		s.handlePacket(bip.InboundPacket{Data: second, ImmediatePeer: fuzzPeer, ReceivedAt: time.Now()})
		closed := make(chan struct{})
		go func() { _ = s.Close(); close(closed) }()
		select {
		case <-closed:
		case <-time.After(5 * time.Second):
			t.Fatal("Close did not return")
		}
		for _, p := range tr.Outbox() {
			m, err := bvlc.Parse(p.Data, bacnet.DefaultDecodeLimits())
			if err != nil {
				t.Fatalf("sent an unparsable BVLC frame % x: %v", p.Data, err)
			}
			if m.Function != bvlc.FunctionOriginalUnicastNPDU && m.Function != bvlc.FunctionOriginalBroadcastNPDU {
				continue
			}
			n, _, err := npdu.Parse(m.Payload, bacnet.DefaultDecodeLimits())
			if err != nil {
				t.Fatalf("sent an unparsable NPDU % x: %v", m.Payload, err)
			}
			if _, err := apdu.Parse(n.APDU, bacnet.DefaultDecodeLimits()); err != nil {
				t.Fatalf("sent an unparsable APDU % x: %v", n.APDU, err)
			}
		}
	})
}
