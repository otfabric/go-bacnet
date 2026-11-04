// SPDX-License-Identifier: MIT

package e2e_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"math/rand"
	"net"
	"net/netip"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/npdu"
	"github.com/otfabric/go-bacnet/service"
)

// The hostile device answers the client over loopback UDP with malformed and
// adversarial frames. Every test asserts that the client returns an error or
// ignores the frame, that it never returns a value taken from a frame it
// should have dropped, that a call ends within its timeout, and that the
// client is still usable afterwards.

const (
	hostileAPDUTimeout = 300 * time.Millisecond
	// hangBound is how long a call that should time out may take: the APDU
	// timeout, no retries, plus scheduling slack.
	hangBound = 3 * time.Second
)

var (
	poisonValue = bacnet.RealValue(666) // value only a dropped frame carries
	goodValue   = bacnet.RealValue(21.5)
)

// hostile is a scripted adversarial BACnet/IP device.
type hostile struct {
	t    testing.TB
	conn *net.UDPConn
	done chan struct{}

	// confirmed answers a confirmed request. It runs on the receive goroutine.
	confirmed func(h *hostile, req *apdu.ConfirmedRequest, from *net.UDPAddr)
	// other sees every other APDU the client sends (Segment-ACK, Abort...).
	other func(h *hostile, pdu apdu.PDU, from *net.UDPAddr)

	aborts      atomic.Int64 // Abort PDUs received from the client
	segmentACKs atomic.Int64
}

func startHostile(t *testing.T, confirmed func(*hostile, *apdu.ConfirmedRequest, *net.UDPAddr), other ...func(*hostile, apdu.PDU, *net.UDPAddr)) *hostile {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	h := &hostile{t: t, conn: conn, done: make(chan struct{}), confirmed: confirmed}
	if len(other) > 0 {
		h.other = other[0]
	}
	go h.serve()
	t.Cleanup(func() {
		_ = conn.Close()
		<-h.done
	})
	return h
}

func (h *hostile) endpoint() bip.Endpoint {
	a := h.conn.LocalAddr().(*net.UDPAddr)
	return bip.NewEndpoint(netip.AddrPortFrom(netip.AddrFrom4([4]byte{127, 0, 0, 1}), uint16(a.Port)))
}

func (h *hostile) target() client.Target {
	return client.Target{Endpoint: h.endpoint(), MaxAPDU: 1476}
}

func (h *hostile) serve() {
	defer close(h.done)
	buf := make([]byte, 2048)
	for {
		n, from, err := h.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		frame := append([]byte(nil), buf[:n]...)
		msg, err := bvlc.Parse(frame, limits)
		if err != nil {
			continue
		}
		nn, _, err := npdu.Parse(msg.Payload, limits)
		if err != nil || nn.NetworkMessage {
			continue
		}
		pdu, err := apdu.Parse(nn.APDU, limits)
		if err != nil {
			continue
		}
		switch {
		case pdu.ConfirmedRequest != nil:
			if h.confirmed != nil {
				h.confirmed(h, pdu.ConfirmedRequest, from)
			}
		default:
			if pdu.Abort != nil {
				h.aborts.Add(1)
			}
			if pdu.SegmentACK != nil {
				h.segmentACKs.Add(1)
			}
			if h.other != nil {
				h.other(h, pdu, from)
			}
		}
	}
}

func (h *hostile) raw(to *net.UDPAddr, frame []byte) {
	if _, err := h.conn.WriteToUDP(frame, to); err != nil && !errors.Is(err, net.ErrClosed) {
		h.t.Errorf("hostile send: %v", err)
	}
}

func (h *hostile) apdu(to *net.UDPAddr, pdu []byte) { h.raw(to, frameAPDU(pdu)) }

// frameAPDU wraps an APDU in a local NPDU and an Original-Unicast BVLC.
func frameAPDU(pdu []byte) []byte {
	n, err := npdu.Append(nil, npdu.NPDU{Version: 1, APDU: pdu})
	if err != nil {
		panic(err)
	}
	return frameNPDU(n)
}

// frameNPDU wraps raw NPDU bytes in an Original-Unicast BVLC.
func frameNPDU(n []byte) []byte {
	frame, err := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionOriginalUnicastNPDU, Payload: n})
	if err != nil {
		panic(err)
	}
	return frame
}

func complexACK(invokeID, service uint8, payload []byte) []byte {
	return apdu.AppendComplexACK(nil, apdu.ComplexACK{InvokeID: invokeID, ServiceChoice: service, Payload: payload})
}

func rpACK(t testing.TB, object bacnet.ObjectIdentifier, v bacnet.ApplicationValue) []byte {
	t.Helper()
	ack, err := service.EncodeReadPropertyACK(service.ReadPropertyACK{Object: object, Property: presentValue, Value: v})
	if err != nil {
		t.Fatal(err)
	}
	return ack
}

func udpAddr(e bip.Endpoint) *net.UDPAddr {
	return net.UDPAddrFromAddrPort(e.Addr)
}

func newHostileClient(t *testing.T, opts ...client.Option) *client.Client {
	t.Helper()
	opts = append([]client.Option{
		client.WithLocalAddr("127.0.0.1:0"),
		client.WithTransactionOptions(hostileAPDUTimeout, 0, time.Second),
	}, opts...)
	c, err := client.New(opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// bounded runs fn and fails the test when it takes longer than limit.
func bounded(t *testing.T, limit time.Duration, fn func()) {
	t.Helper()
	start := time.Now()
	fn()
	if d := time.Since(start); d > limit {
		t.Fatalf("call took %v, bound %v", d, limit)
	}
}

// poisonedFrames are frames that carry a ComplexACK for the pending request
// with poisonValue, each broken so that the client must drop it.
func poisonedFrames(t testing.TB, invokeID uint8) map[string][]byte {
	t.Helper()
	good := complexACK(invokeID, apdu.ServiceReadProperty, rpACK(t, analogValue, poisonValue))
	valid := frameAPDU(good)
	frames := map[string][]byte{}

	longer := append([]byte(nil), valid...)
	binary.BigEndian.PutUint16(longer[2:4], uint16(len(valid)+10))
	frames["bvlc length beyond datagram"] = longer

	shorter := append([]byte(nil), valid...)
	binary.BigEndian.PutUint16(shorter[2:4], uint16(len(valid)-3))
	frames["bvlc length short of datagram"] = shorter

	tiny := append([]byte(nil), valid...)
	binary.BigEndian.PutUint16(tiny[2:4], 2)
	frames["bvlc length below header"] = tiny

	wrongType := append([]byte(nil), valid...)
	wrongType[0] = 0x82
	frames["bvlc type not Annex J"] = wrongType

	unknownFn := append([]byte(nil), valid...)
	unknownFn[1] = 0x7F
	frames["bvlc unknown function"] = unknownFn

	for _, cut := range []int{1, 3, 4, 5, 6, 8, len(valid) - 1} {
		frames["truncated to "+itoa(cut)] = valid[:cut]
	}

	// Each NPDU below is wrapped in a correct BVLC so only the NPDU is wrong.
	frames["npdu version 2"] = frameNPDU(append([]byte{0x02, 0x00}, good...))
	frames["npdu reserved control bits"] = frameNPDU(append([]byte{0x01, 0x50}, good...))
	// SNET present: the answer claims to come from a remote station, not
	// the local station the target names.
	frames["npdu from remote network"] = frameNPDU(append([]byte{0x01, 0x08, 0x00, 0x07, 0x01, 0x2A}, good...))
	// SNET 0xFFFF and SLEN 0 are not a source station.
	frames["npdu source global broadcast"] = frameNPDU(append([]byte{0x01, 0x08, 0xFF, 0xFF, 0x00}, good...))
	// DNET present with DLEN 7 but only 2 bytes of address follow.
	frames["npdu DADR overruns"] = frameNPDU([]byte{0x01, 0x20, 0x00, 0x05, 0x07, 0x01, 0x02})
	// Network-layer message bit with the APDU as message body.
	frames["npdu network message"] = frameNPDU(append([]byte{0x01, 0x80, 0x7F}, good...))

	frames["apdu garbage"] = frameAPDU([]byte{0xFF, 0xFF, 0xFF, 0xFF})
	frames["apdu empty"] = frameNPDU([]byte{0x01, 0x00})
	frames["wrong invoke id"] = frameAPDU(complexACK(invokeID+1, apdu.ServiceReadProperty, rpACK(t, analogValue, poisonValue)))
	return frames
}

func itoa(n int) string {
	var b [20]byte
	i := len(b)
	for {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
		if n == 0 {
			return string(b[i:])
		}
	}
}

func TestHostileMalformedBeforeValidAnswer(t *testing.T) {
	var mu sync.Mutex
	var current string
	h := startHostile(t, func(h *hostile, req *apdu.ConfirmedRequest, from *net.UDPAddr) {
		mu.Lock()
		name := current
		mu.Unlock()
		frames := poisonedFrames(h.t, req.InvokeID)
		h.raw(from, frames[name])
		h.apdu(from, complexACK(req.InvokeID, req.ServiceChoice, rpACK(h.t, analogValue, goodValue)))
	})
	c := newHostileClient(t)
	ctx := ctxT(t)
	for name := range poisonedFrames(t, 0) {
		mu.Lock()
		current = name
		mu.Unlock()
		t.Run(name, func(t *testing.T) {
			var v bacnet.ApplicationValue
			var err error
			bounded(t, hangBound, func() { v, err = c.ReadProperty(ctx, h.target(), analogValue, presentValue) })
			if err != nil {
				t.Fatalf("valid answer after the bad frame: %v", err)
			}
			if f, _ := bacnet.AsReal(v); f != 21.5 {
				t.Fatalf("client took the value %v from a frame it should have dropped", v)
			}
		})
	}
}

func TestHostileOnlyMalformedTimesOut(t *testing.T) {
	h := startHostile(t, func(h *hostile, req *apdu.ConfirmedRequest, from *net.UDPAddr) {
		for _, f := range poisonedFrames(h.t, req.InvokeID) {
			h.raw(from, f)
		}
	})
	c := newHostileClient(t)
	ctx := ctxT(t)
	for i := 0; i < 3; i++ {
		var err error
		bounded(t, hangBound, func() { _, err = c.ReadProperty(ctx, h.target(), analogValue, presentValue) })
		if !errors.Is(err, bacnet.ErrTimeout) {
			t.Fatalf("round %d: %v, want a timeout", i, err)
		}
	}
}

// TestHostileBadAnswerContent: an answer that matches the transaction but
// whose content is wrong ends the request with an error, never with the
// value it carries.
func TestHostileBadAnswerContent(t *testing.T) {
	cases := map[string]func(t testing.TB, invokeID uint8) []byte{
		"ack for another object": func(t testing.TB, id uint8) []byte {
			return complexACK(id, apdu.ServiceReadProperty, rpACK(t, missing, poisonValue))
		},
		"trailing bytes after ack": func(t testing.TB, id uint8) []byte {
			return complexACK(id, apdu.ServiceReadProperty, append(rpACK(t, analogValue, poisonValue), 0x00, 0x00))
		},
		"ack truncated inside the value": func(t testing.TB, id uint8) []byte {
			p := rpACK(t, analogValue, poisonValue)
			return complexACK(id, apdu.ServiceReadProperty, p[:len(p)-3])
		},
		"ack with oversized length": func(t testing.TB, id uint8) []byte {
			// Application REAL with an extended length of 0xFFFFFFFF.
			p := mustE(bacnet.AppendContextObjectID(nil, 0, analogValue))
			p = mustE(bacnet.AppendContextUnsigned(p, 1, uint64(bacnet.PropertyPresentValue)))
			p = append(p, 0x3E, 0x45, 0xFE, 0xFF, 0xFF, 0xFF, 0xFF, 0x3F)
			return complexACK(id, apdu.ServiceReadProperty, p)
		},
		"ack with deep nesting": func(t testing.TB, id uint8) []byte {
			p := mustE(bacnet.AppendContextObjectID(nil, 0, analogValue))
			p = mustE(bacnet.AppendContextUnsigned(p, 1, uint64(bacnet.PropertyPresentValue)))
			p = append(p, bytes.Repeat([]byte{0x3E}, 400)...)
			p = append(p, bytes.Repeat([]byte{0x3F}, 400)...)
			return complexACK(id, apdu.ServiceReadProperty, p)
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			h := startHostile(t, func(h *hostile, req *apdu.ConfirmedRequest, from *net.UDPAddr) {
				h.apdu(from, build(h.t, req.InvokeID))
			})
			c := newHostileClient(t)
			ctx := ctxT(t)
			var v bacnet.ApplicationValue
			var err error
			bounded(t, hangBound, func() { v, err = c.ReadProperty(ctx, h.target(), analogValue, presentValue) })
			if err == nil {
				t.Fatalf("bad answer accepted as %v", v)
			}
		})
	}
}

func TestHostileWrongServiceChoice(t *testing.T) {
	h := startHostile(t, func(h *hostile, req *apdu.ConfirmedRequest, from *net.UDPAddr) {
		// Right invoke ID, wrong service: an ACK for a WriteProperty.
		h.apdu(from, apdu.AppendSimpleACK(nil, apdu.SimpleACK{InvokeID: req.InvokeID, ServiceChoice: apdu.ServiceWriteProperty}))
		h.apdu(from, complexACK(req.InvokeID, apdu.ServiceReadPropertyMultiple, rpACK(h.t, analogValue, poisonValue)))
	})
	c := newHostileClient(t)
	ctx := ctxT(t)
	var v bacnet.ApplicationValue
	var err error
	bounded(t, hangBound, func() { v, err = c.ReadProperty(ctx, h.target(), analogValue, presentValue) })
	if err == nil {
		t.Fatalf("ACK for another service accepted: %v", v)
	}
}

// TestHostileOutOfRangeAcks: every value below is one the device encodes
// wider than the field the client decodes it into. Each is chosen so that a
// silent truncation lands on a plausible value (0 or 1), so only a decoder
// that range-checks returns an error.
func TestHostileOutOfRangeAcks(t *testing.T) {
	file := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 1}
	trend := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeTrendLog, Instance: 1}
	logBuffer := bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer}

	answers := map[uint8][]byte{} // by service choice
	var mu sync.Mutex
	h := startHostile(t, func(h *hostile, req *apdu.ConfirmedRequest, from *net.UDPAddr) {
		mu.Lock()
		payload, ok := answers[req.ServiceChoice]
		mu.Unlock()
		if ok {
			h.apdu(from, complexACK(req.InvokeID, req.ServiceChoice, payload))
		}
	})
	c := newHostileClient(t)
	ctx := ctxT(t)

	app := func(b []byte, v bacnet.ApplicationValue) []byte { return mustE(bacnet.AppendApplicationValue(b, v)) }
	tagged := func(b []byte, tag uint8, vs ...bacnet.ApplicationValue) []byte {
		els := make([]bacnet.Element, len(vs))
		for i, v := range vs {
			els[i] = bacnet.Element{Value: v}
		}
		return mustE(bacnet.AppendContextTagged(b, tag, els))
	}
	data := bacnet.ApplicationValue{Kind: bacnet.ValueOctetString, OctetString: []byte{1}}
	readRangeACK := func(itemCount, firstSeq uint64) []byte {
		b := mustE(bacnet.AppendContextObjectID(nil, 0, trend))
		b = mustE(bacnet.AppendContextUnsigned(b, 1, uint64(bacnet.PropertyLogBuffer)))
		b = mustE(bacnet.AppendContextBitString(b, 3, bacnet.BitString{UnusedBits: 5, Bytes: []byte{0xE0}}))
		b = mustE(bacnet.AppendContextUnsigned(b, 4, itemCount))
		b = tagged(b, 5)
		return mustE(bacnet.AppendContextUnsigned(b, 6, firstSeq))
	}

	cases := []struct {
		name    string
		service uint8
		payload []byte
		call    func() (any, error)
		valid   bool // control case: in range, must succeed
	}{
		{
			name: "control VT-Open session id 255", service: apdu.ServiceVTOpen, valid: true,
			payload: app(nil, bacnet.UnsignedValue(255)),
			call: func() (any, error) {
				return c.VTOpen(ctx, h.target(), service.VTOpenRequest{VTClass: 0, LocalVTSessionIdentifier: 1})
			},
		},
		{
			name: "control AtomicReadFile record", service: apdu.ServiceAtomicReadFile, valid: true,
			payload: tagged(app(nil, bacnet.BoolValue(true)), 1, bacnet.SignedValue(0), bacnet.UnsignedValue(1), data),
			call: func() (any, error) {
				return c.AtomicReadFile(ctx, h.target(), service.AtomicReadFileRequest{File: file, Access: service.FileAccessRecord, Count: 1})
			},
		},
		{
			name: "control AtomicWriteFile start 0", service: apdu.ServiceAtomicWriteFile, valid: true,
			payload: mustE(bacnet.AppendContextSigned(nil, 0, 0)),
			call: func() (any, error) {
				return c.AtomicWriteFile(ctx, h.target(), service.AtomicWriteFileRequest{File: file, Access: service.FileAccessStream, Data: []byte{1}})
			},
		},
		{
			name: "control ReadRange", service: apdu.ServiceReadRange, valid: true,
			payload: readRangeACK(0, maxUint32),
			call: func() (any, error) {
				return c.ReadRange(ctx, h.target(), service.ReadRangeRequest{Object: trend, Property: logBuffer})
			},
		},
		{
			name: "VT-Open session id 256", service: apdu.ServiceVTOpen,
			payload: app(nil, bacnet.UnsignedValue(256)),
			call: func() (any, error) {
				return c.VTOpen(ctx, h.target(), service.VTOpenRequest{VTClass: 0, LocalVTSessionIdentifier: 1})
			},
		},
		{
			name: "AtomicReadFile stream start 2^32", service: apdu.ServiceAtomicReadFile,
			payload: tagged(app(nil, bacnet.BoolValue(true)), 0, bacnet.SignedValue(1<<32), data),
			call: func() (any, error) {
				return c.AtomicReadFile(ctx, h.target(), service.AtomicReadFileRequest{File: file, Access: service.FileAccessStream, Count: 1})
			},
		},
		{
			name: "AtomicReadFile record start 2^32", service: apdu.ServiceAtomicReadFile,
			payload: tagged(app(nil, bacnet.BoolValue(true)), 1, bacnet.SignedValue(1<<32), bacnet.UnsignedValue(1), data),
			call: func() (any, error) {
				return c.AtomicReadFile(ctx, h.target(), service.AtomicReadFileRequest{File: file, Access: service.FileAccessRecord, Count: 1})
			},
		},
		{
			name: "AtomicReadFile record count 2^32+1", service: apdu.ServiceAtomicReadFile,
			payload: tagged(app(nil, bacnet.BoolValue(true)), 1, bacnet.SignedValue(0), bacnet.UnsignedValue(1<<32+1), data),
			call: func() (any, error) {
				return c.AtomicReadFile(ctx, h.target(), service.AtomicReadFileRequest{File: file, Access: service.FileAccessRecord, Count: 1})
			},
		},
		{
			name: "AtomicWriteFile start 2^32", service: apdu.ServiceAtomicWriteFile,
			payload: mustE(bacnet.AppendContextSigned(nil, 0, 1<<32)),
			call: func() (any, error) {
				return c.AtomicWriteFile(ctx, h.target(), service.AtomicWriteFileRequest{File: file, Access: service.FileAccessStream, Data: []byte{1}})
			},
		},
		{
			name: "ReadRange first sequence 2^32+1", service: apdu.ServiceReadRange,
			payload: readRangeACK(0, 1<<32+1),
			call: func() (any, error) {
				return c.ReadRange(ctx, h.target(), service.ReadRangeRequest{Object: trend, Property: logBuffer})
			},
		},
		{
			name: "ReadRange item count 2^32", service: apdu.ServiceReadRange,
			payload: readRangeACK(1<<32, 1),
			call: func() (any, error) {
				return c.ReadRange(ctx, h.target(), service.ReadRangeRequest{Object: trend, Property: logBuffer})
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mu.Lock()
			answers = map[uint8][]byte{tc.service: tc.payload}
			mu.Unlock()
			var got any
			var err error
			bounded(t, hangBound, func() { got, err = tc.call() })
			if tc.valid {
				if err != nil {
					t.Fatalf("in-range ACK refused: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("out-of-range ACK accepted as %+v", got)
			}
			if errors.Is(err, bacnet.ErrTimeout) {
				t.Fatalf("the ACK never reached the decoder: %v", err)
			}
		})
	}
}

const maxUint32 = 1<<32 - 1

func mustE(b []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return b
}

// iAm builds an I-Am APDU from raw application values, so that any field
// can be out of range.
func iAm(t testing.TB, device []byte, maxAPDU, segmentation, vendor bacnet.ApplicationValue) []byte {
	t.Helper()
	p := append([]byte(nil), device...)
	p = mustE(bacnet.AppendApplicationValue(p, maxAPDU))
	p = mustE(bacnet.AppendApplicationValue(p, segmentation))
	p = mustE(bacnet.AppendApplicationValue(p, vendor))
	return apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{ServiceChoice: apdu.ServiceIAm, Payload: p})
}

func deviceID(instance uint32) []byte {
	return mustE(bacnet.AppendApplicationValue(nil, bacnet.ObjectIDValue(bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: instance})))
}

// roundTrip performs one ReadProperty against h: once it returns, the client
// has processed every datagram h sent before it (one receive goroutine,
// loopback keeps order).
func roundTrip(t *testing.T, ctx context.Context, c *client.Client, h *hostile) {
	t.Helper()
	var v bacnet.ApplicationValue
	var err error
	bounded(t, hangBound, func() { v, err = c.ReadProperty(ctx, h.target(), analogValue, presentValue) })
	if err != nil {
		t.Fatalf("client unusable after the hostile traffic: %v", err)
	}
	if f, _ := bacnet.AsReal(v); f != 21.5 {
		t.Fatalf("read %v", v)
	}
}

func answerReads(h *hostile, req *apdu.ConfirmedRequest, from *net.UDPAddr) {
	if req.ServiceChoice == apdu.ServiceReadProperty {
		h.apdu(from, complexACK(req.InvokeID, req.ServiceChoice, rpACK(h.t, analogValue, goodValue)))
	}
}

func TestHostileIAmOutOfRange(t *testing.T) {
	h := startHostile(t, answerReads)
	c := newHostileClient(t)
	ctx := ctxT(t)
	to := udpAddr(c.LocalEndpoint())

	seg := bacnet.EnumValue(3)
	bad := map[uint32][]byte{
		101: iAm(t, deviceID(101), bacnet.UnsignedValue(1476), seg, bacnet.UnsignedValue(65536)),
		102: iAm(t, deviceID(102), bacnet.UnsignedValue(1<<32+1476), seg, bacnet.UnsignedValue(1)),
		103: iAm(t, deviceID(103), bacnet.UnsignedValue(65536+480), seg, bacnet.UnsignedValue(1)),
		104: iAm(t, deviceID(104), bacnet.UnsignedValue(1476), bacnet.EnumValue(256+3), bacnet.UnsignedValue(1)),
		105: iAm(t, deviceID(105), bacnet.UnsignedValue(1476), seg, bacnet.UnsignedValue(1<<32+999)),
		106: iAm(t, deviceID(106), bacnet.SignedValue(-1), seg, bacnet.UnsignedValue(1)),
		// A 5-byte object identifier whose low four bytes are Device 107.
		107: iAm(t, []byte{0xC5, 0x05, 0xFF, 0x02, 0x00, 0x00, 0x6B}, bacnet.UnsignedValue(1476), seg, bacnet.UnsignedValue(1)),
		// Not a Device object.
		108: iAm(t, mustE(bacnet.AppendApplicationValue(nil, bacnet.ObjectIDValue(bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 108}))),
			bacnet.UnsignedValue(1476), seg, bacnet.UnsignedValue(1)),
	}
	for _, pdu := range bad {
		h.apdu(to, pdu)
	}
	h.apdu(to, iAm(t, deviceID(200), bacnet.UnsignedValue(1476), seg, bacnet.UnsignedValue(65535)))
	roundTrip(t, ctx, c, h)

	var sawGood bool
	for _, o := range c.Devices() {
		if _, isBad := bad[o.Instance]; isBad {
			t.Errorf("an I-Am with an out-of-range field was recorded: %+v", o)
		}
		if o.Instance == 200 {
			sawGood = true
		}
	}
	if !sawGood {
		t.Fatal("the valid I-Am was not recorded")
	}
}

// floodRetries lets the reads between flood bursts retry. A burst can overrun
// the socket receive buffer, so the kernel may drop the reply to the read that
// follows it (seen under -race on CI); that is UDP, not a client defect. The
// read is idempotent and hangBound still caps the wait.
var floodRetries = client.WithTransactionOptions(hostileAPDUTimeout, 3, time.Second)

func TestHostileIAmFloodIsBounded(t *testing.T) {
	const maxObs = 32
	h := startHostile(t, answerReads)
	c := newHostileClient(t, floodRetries, client.WithRegistryOptions(client.RegistryOptions{MaxObservations: maxObs, MaxPathsPerInstance: 2}))
	ctx := ctxT(t)
	to := udpAddr(c.LocalEndpoint())

	var before runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	seg := bacnet.EnumValue(3)
	for i := uint32(0); i < 5000; i++ {
		h.apdu(to, iAm(t, deviceID(i), bacnet.UnsignedValue(1476), seg, bacnet.UnsignedValue(uint64(i%65536))))
		if i%500 == 499 {
			roundTrip(t, ctx, c, h) // pace the flood so the socket buffer does not drop it all
		}
	}
	roundTrip(t, ctx, c, h)
	if n := len(c.Devices()); n > maxObs {
		t.Fatalf("registry holds %d observations, bound %d", n, maxObs)
	}
	var after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&after)
	if grow := int64(after.HeapInuse) - int64(before.HeapInuse); grow > 32<<20 {
		t.Fatalf("heap grew %d bytes over an I-Am flood with a %d-entry registry", grow, maxObs)
	}
}

// covNote encodes an unconfirmed COV notification for process pid.
func covNote(t testing.TB, pid uint32, object bacnet.ObjectIdentifier, v bacnet.ApplicationValue) []byte {
	t.Helper()
	note := mustE(bacnet.AppendContextUnsigned(nil, 0, uint64(pid)))
	note = mustE(bacnet.AppendContextObjectID(note, 1, bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: deviceInstance}))
	note = mustE(bacnet.AppendContextObjectID(note, 2, object))
	note = mustE(bacnet.AppendContextUnsigned(note, 3, 60))
	note = append(note, 0x4E)
	note = mustE(bacnet.AppendContextUnsigned(note, 0, uint64(bacnet.PropertyPresentValue)))
	note = append(note, 0x2E)
	note = mustE(bacnet.AppendApplicationValue(note, v))
	note = append(note, 0x2F, 0x4F)
	return apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{ServiceChoice: apdu.ServiceUnconfirmedCOV, Payload: note})
}

func TestHostileCOVFloodIsBounded(t *testing.T) {
	var pid atomic.Uint32
	var subscriber atomic.Pointer[net.UDPAddr]
	h := startHostile(t, func(h *hostile, req *apdu.ConfirmedRequest, from *net.UDPAddr) {
		switch req.ServiceChoice {
		case apdu.ServiceSubscribeCOV:
			s, err := service.DecodeSubscribeCOV(req.Payload, limits)
			if err != nil {
				h.t.Errorf("subscribe: %v", err)
				return
			}
			pid.Store(s.ProcessIdentifier)
			subscriber.Store(from)
			h.apdu(from, apdu.AppendSimpleACK(nil, apdu.SimpleACK{InvokeID: req.InvokeID, ServiceChoice: req.ServiceChoice}))
		default:
			answerReads(h, req, from)
		}
	})
	c := newHostileClient(t, floodRetries)
	ctx := ctxT(t)
	const buffer = 4
	sub, err := c.SubscribeCOV(ctx, h.target(), analogValue, client.COVOptions{Lifetime: 60, BufferSize: buffer})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sub.Close() }()
	to := subscriber.Load()

	for i := 0; i < 3000; i++ {
		h.apdu(to, covNote(t, pid.Load(), analogValue, bacnet.RealValue(float32(i))))
		// Notifications with a wrong process id or object must be ignored.
		h.apdu(to, covNote(t, pid.Load()+1, analogValue, poisonValue))
		h.apdu(to, covNote(t, pid.Load(), missing, poisonValue))
		if i%500 == 499 {
			roundTrip(t, ctx, c, h)
		}
	}
	roundTrip(t, ctx, c, h)

	queued := 0
drain:
	for {
		select {
		case ev := <-sub.Events():
			queued++
			if ev.Notification != nil && len(ev.Notification.Values) > 0 {
				if f, _ := bacnet.AsReal(ev.Notification.Values[0].Value); f == 666 {
					t.Fatalf("a notification for another subscription was delivered: %+v", ev.Notification)
				}
			}
		default:
			break drain
		}
	}
	if queued > buffer+1 {
		t.Fatalf("%d events queued, buffer %d", queued, buffer)
	}

	// The loss is reported on the next delivered event.
	h.apdu(to, covNote(t, pid.Load(), analogValue, bacnet.RealValue(-1)))
	select {
	case ev := <-sub.Events():
		if !ev.Gap {
			t.Fatalf("event after an overflow without Gap: %+v", ev)
		}
	case <-time.After(hangBound):
		t.Fatal("no event after the flood")
	}
}

// junkFrames are unsolicited frames a device on the network can send at any
// time.
func junkFrames(t testing.TB) [][]byte {
	t.Helper()
	whoIs := apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{ServiceChoice: apdu.ServiceWhoIs})
	iam := iAm(t, deviceID(300), bacnet.UnsignedValue(1476), bacnet.EnumValue(3), bacnet.UnsignedValue(1))
	return [][]byte{
		// DNET 1234, hop count 0 and 255.
		frameNPDU(append([]byte{0x01, 0x20, 0x04, 0xD2, 0x01, 0x05, 0x00}, iam...)),
		frameNPDU(append([]byte{0x01, 0x20, 0x04, 0xD2, 0x01, 0x05, 0xFF}, iam...)),
		// Global broadcast DNET with DLEN 0 and a hop count but nothing after.
		frameNPDU([]byte{0x01, 0x20, 0xFF, 0xFF, 0x00}),
		// DNET and SNET with SLEN 0 (a remote station without a MAC).
		frameNPDU(append([]byte{0x01, 0x28, 0x00, 0x05, 0x00, 0x00, 0x07, 0x00, 0x10}, iam...)),
		// SNET 0 is not a network number.
		frameNPDU(append([]byte{0x01, 0x08, 0x00, 0x00, 0x01, 0x01}, iam...)),
		// DLEN 255.
		frameNPDU(append([]byte{0x01, 0x20, 0x00, 0x05, 0xFF}, bytes.Repeat([]byte{0xAA}, 300)...)),
		// Network-layer messages: I-Am-Router-To-Network with an odd length,
		// Router-Busy, an unknown proprietary type with no vendor id.
		frameNPDU([]byte{0x01, 0x80, 0x01, 0x00}),
		frameNPDU([]byte{0x01, 0x80, 0x04, 0xFF}),
		frameNPDU([]byte{0x01, 0x80, 0x80}),
		// A confirmed request to a client, which serves nothing.
		frameAPDU(apdu.AppendConfirmedRequest(nil, apdu.ConfirmedRequest{InvokeID: 1, ServiceChoice: apdu.ServiceReadProperty, MaxAPDU: 5, Payload: []byte{0x0C, 0, 0, 0, 1, 0x19, 0x55}})),
		frameAPDU(whoIs),
		// Unsolicited ACKs, Segment-ACK, Abort, Reject, Error for nothing.
		frameAPDU(apdu.AppendSimpleACK(nil, apdu.SimpleACK{InvokeID: 9, ServiceChoice: apdu.ServiceWriteProperty})),
		frameAPDU(apdu.AppendSegmentACK(nil, apdu.SegmentACK{InvokeID: 9, Server: true, SequenceNumber: 200, ActualWindowSize: 127})),
		frameAPDU(apdu.AppendAbort(nil, apdu.AbortPDU{InvokeID: 9, Server: true, Reason: 255})),
		frameAPDU([]byte{0x60, 0x09, 0xFF}),
		frameAPDU([]byte{0x50, 0x09, 0x0C}),
		// Forwarded-NPDU with a truncated origin.
		{0x81, 0x04, 0x00, 0x08, 0x7F, 0x00, 0x00, 0x01},
		// BVLC-Result and Register-Foreign-Device sent to a client.
		{0x81, 0x00, 0x00, 0x06, 0x00, 0x30},
		{0x81, 0x05, 0x00, 0x06, 0xFF, 0xFF},
		// Empty and one-byte datagrams.
		{},
		{0x81},
	}
}

func TestHostileUnsolicitedJunk(t *testing.T) {
	h := startHostile(t, answerReads)
	c := newHostileClient(t, floodRetries)
	ctx := ctxT(t)
	to := udpAddr(c.LocalEndpoint())
	for round := 0; round < 50; round++ {
		for _, f := range junkFrames(t) {
			h.raw(to, f)
		}
	}
	roundTrip(t, ctx, c, h)

	// Seeded random datagrams, and random bytes behind a valid BVLC/NPDU
	// header, so the APDU parser sees them.
	rng := rand.New(rand.NewSource(0x6261636e)) // "bacn"
	for i := 0; i < 4000; i++ {
		b := make([]byte, rng.Intn(64))
		_, _ = rng.Read(b)
		if i%2 == 0 {
			h.raw(to, b)
		} else {
			h.raw(to, frameNPDU(append([]byte{0x01, 0x00}, b...)))
		}
		if i%500 == 499 {
			roundTrip(t, ctx, c, h)
		}
	}
	roundTrip(t, ctx, c, h)
}

// segmenter answers a ReadProperty with a segmented ComplexACK that never
// ends: each Segment-ACK is answered with the next window of segments.
type segmenter struct {
	size    int   // payload bytes per segment
	window  uint8 // proposed window size
	abortAt int   // > 0: send an Abort in place of this segment
	first   uint8 // sequence number of the first segment sent

	mu       sync.Mutex
	invokeID uint8
	from     *net.UDPAddr
	next     int // next segment to send
	sent     int
}

// send transmits every segment not yet sent up to and including upTo. A
// segment is sent once: answering each Segment-ACK with a full window again
// would make the device, not the client, amplify the traffic.
func (s *segmenter) send(h *hostile, upTo int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ; s.next <= upTo && s.next < 250; s.next++ {
		seq := s.next
		if s.abortAt > 0 && seq == s.abortAt {
			h.apdu(s.from, apdu.AppendAbort(nil, apdu.AbortPDU{InvokeID: s.invokeID, Server: true, Reason: bacnet.AbortReasonOther}))
			s.next = 250
			return
		}
		s.sent++
		h.apdu(s.from, apdu.AppendComplexACK(nil, apdu.ComplexACK{
			SegmentedMessage: true, MoreFollows: true,
			InvokeID: s.invokeID, SequenceNumber: uint8(seq) + s.first, ProposedWindowSize: s.window,
			ServiceChoice: apdu.ServiceReadProperty, Payload: bytes.Repeat([]byte{0x00}, s.size),
		}))
	}
}

func startSegmenter(t *testing.T, s *segmenter) *hostile {
	return startHostile(t, func(h *hostile, req *apdu.ConfirmedRequest, from *net.UDPAddr) {
		if !req.SegmentedResponseAccepted {
			h.t.Errorf("the client does not accept segmented responses")
		}
		s.mu.Lock()
		s.invokeID, s.from, s.next, s.sent = req.InvokeID, from, 0, 0
		s.mu.Unlock()
		s.send(h, int(s.window)-1)
	}, func(h *hostile, pdu apdu.PDU, _ *net.UDPAddr) {
		if a := pdu.SegmentACK; a != nil && !a.NegativeACK {
			s.send(h, int(a.SequenceNumber-s.first)+int(a.ActualWindowSize))
		}
	})
}

func TestHostileEndlessSegmentation(t *testing.T) {
	cases := []struct {
		name   string
		size   int
		reason uint8
	}{
		{"too many segments", 16, bacnet.AbortReasonOutOfResources},
		{"reassembly too long", 1400, bacnet.AbortReasonAPDUTooLong},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &segmenter{size: tc.size, window: 4}
			h := startSegmenter(t, s)
			c := newHostileClient(t)
			ctx := ctxT(t)
			var err error
			bounded(t, 5*time.Second, func() { _, err = c.ReadProperty(ctx, h.target(), analogValue, presentValue) })
			var ae *bacnet.AbortError
			if !errors.As(err, &ae) || ae.Reason != tc.reason {
				t.Fatalf("got %v, want an abort with reason %d", err, tc.reason)
			}
			s.mu.Lock()
			sent := s.sent
			s.mu.Unlock()
			if sent > limits.MaxSegments+int(s.window) {
				t.Fatalf("the client asked for %d segments past its limit of %d", sent, limits.MaxSegments)
			}
			waitFor(t, func() bool { return h.aborts.Load() > 0 }, "the client never sent the device an Abort")
		})
	}
}

func TestHostileSegmentationBadStart(t *testing.T) {
	s := &segmenter{size: 16, window: 2, first: 5}
	h := startSegmenter(t, s)
	c := newHostileClient(t)
	ctx := ctxT(t)
	var err error
	bounded(t, hangBound, func() { _, err = c.ReadProperty(ctx, h.target(), analogValue, presentValue) })
	if err == nil {
		t.Fatal("a segmented answer that does not start at sequence 0 was accepted")
	}
}

func TestHostileAbortMidSegmentation(t *testing.T) {
	s := &segmenter{size: 16, window: 2, abortAt: 5}
	h := startSegmenter(t, s)
	c := newHostileClient(t)
	ctx := ctxT(t)
	var err error
	bounded(t, hangBound, func() { _, err = c.ReadProperty(ctx, h.target(), analogValue, presentValue) })
	var ae *bacnet.AbortError
	if !errors.As(err, &ae) || !ae.Server || ae.Reason != bacnet.AbortReasonOther {
		t.Fatalf("got %v, want the device's abort", err)
	}
	// A later, ordinary exchange still works.
	h2 := startHostile(t, answerReads)
	roundTrip(t, ctx, c, h2)
}

func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(hangBound)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal(msg)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestHostileEventNotificationPriority: an EventNotification whose priority
// does not fit Unsigned8 must not reach the handler with a truncated
// priority.
func TestHostileEventNotificationPriority(t *testing.T) {
	var mu sync.Mutex
	var got []service.EventNotification
	h := startHostile(t, answerReads)
	c := newHostileClient(t, client.WithEventNotificationHandler(func(d client.EventNotificationDelivery) {
		mu.Lock()
		got = append(got, d.Notification)
		mu.Unlock()
	}))
	ctx := ctxT(t)
	to := udpAddr(c.LocalEndpoint())

	note := service.EventNotification{
		ProcessIdentifier: 1,
		InitiatingDevice:  bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: deviceInstance},
		EventObject:       analogValue,
		TimeStamp:         service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 1},
		NotificationClass: 1,
		Priority:          0xC8,
		EventType:         0,
		NotifyType:        0,
		ToState:           0,
	}
	payload, err := service.EncodeEventNotification(note)
	if err != nil {
		t.Fatal(err)
	}
	// Priority is context tag 5, one octet: 0x59 0xC8. Widen it to 256, 0x5A 0x01 0x00.
	if bytes.Count(payload, []byte{0x59, 0xC8}) != 1 {
		t.Fatalf("priority not found once in % x", payload)
	}
	wide := bytes.Replace(payload, []byte{0x59, 0xC8}, []byte{0x5A, 0x01, 0x00}, 1)
	h.apdu(to, apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{ServiceChoice: apdu.ServiceUnconfirmedEventNotification, Payload: wide}))
	h.apdu(to, apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{ServiceChoice: apdu.ServiceUnconfirmedEventNotification, Payload: payload}))
	roundTrip(t, ctx, c, h)

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0].Priority != 0xC8 {
		t.Fatalf("deliveries %+v, want only the valid notification (priority 200)", got)
	}
}
