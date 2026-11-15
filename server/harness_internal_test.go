// SPDX-License-Identifier: MIT

package server

import (
	"net/netip"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/internal/virtual"
	"github.com/otfabric/go-bacnet/npdu"
	"github.com/otfabric/go-bacnet/server/object"
	"github.com/otfabric/go-bacnet/service"
)

var (
	serverEP = bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.1:47808"))
	clientEP = bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.2:47808"))
	av1      = bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	pvRef    = bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
)

// harness runs a server on a virtual transport; tests inject frames from
// clientEP and read the server's answers from the outbox.
type harness struct {
	t *testing.T
	// synced: the harness was made in a synctest bubble (inBubble).
	synced bool
	s      *Server
	tr     *virtual.Transport
	dev    *object.Device
	av     *object.Point
	mu     sync.Mutex
	seen   int
	diag   []Diagnostic
}

func newHarness(t *testing.T, opts ...Option) *harness {
	t.Helper()
	dev, err := object.NewDevice(object.DeviceConfig{Instance: 100, Name: "dev", VendorID: 999})
	if err != nil {
		t.Fatal(err)
	}
	av, err := object.NewAnalogValue(object.PointConfig{Instance: 1, Name: "AV-1", Value: 1, Commandable: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := dev.Add(av); err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, synced: bubbled(t), dev: dev, av: av, tr: virtual.New(serverEP, nil, 512)}
	all := append([]Option{
		WithTransport(h.tr),
		WithTransactionOptions(200*time.Millisecond, 1, 200*time.Millisecond),
		WithDiagnosticFunc(func(d Diagnostic) {
			h.mu.Lock()
			h.diag = append(h.diag, d)
			h.mu.Unlock()
		}),
	}, opts...)
	s, err := New(dev, all...)
	if err != nil {
		t.Fatal(err)
	}
	h.s = s
	t.Cleanup(func() { _ = s.Close() })
	return h
}

func (h *harness) injectFrom(from bip.Endpoint, fn bvlc.Function, n npdu.NPDU) {
	h.t.Helper()
	raw, err := npdu.Append(nil, n)
	if err != nil {
		h.t.Fatal(err)
	}
	frame, err := bvlc.Append(nil, bvlc.Message{Function: fn, Payload: raw})
	if err != nil {
		h.t.Fatal(err)
	}
	h.tr.Inject(bip.InboundPacket{Data: frame, ImmediatePeer: from})
}

func (h *harness) inject(a []byte) {
	h.injectFrom(clientEP, bvlc.FunctionOriginalUnicastNPDU, npdu.NPDU{Version: npdu.Version1, ExpectingReply: true, APDU: a})
}

func (h *harness) injectRaw(data []byte) {
	h.tr.Inject(bip.InboundPacket{Data: data, ImmediatePeer: clientEP})
}

// confirmed sends a confirmed request with a large Max APDU that accepts
// segmentation.
func (h *harness) confirmed(invokeID, svc uint8, payload []byte) {
	h.inject(apdu.AppendConfirmedRequest(nil, apdu.ConfirmedRequest{
		SegmentedResponseAccepted: true, MaxSegments: 7, MaxAPDU: 5, InvokeID: invokeID, ServiceChoice: svc, Payload: payload,
	}))
}

func (h *harness) unconfirmed(svc uint8, payload []byte) {
	h.inject(apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{ServiceChoice: svc, Payload: payload}))
}

type sent struct {
	pkt  bip.OutboundPacket
	bvlc bvlc.Message
	npdu npdu.NPDU
	pdu  apdu.PDU
}

// next waits for the next packet the server sends. It waits for the
// transport to record one, not for an amount of time; the timeout only ends
// a test in which none comes.
func (h *harness) next() sent {
	h.t.Helper()
	timeout := time.After(nextTimeout)
	for {
		recorded := h.tr.Sent() // before the outbox is read
		out := h.tr.Outbox()
		h.mu.Lock()
		if h.seen < len(out) {
			p := out[h.seen]
			h.seen++
			h.mu.Unlock()
			return decodeSent(h.t, p)
		}
		h.mu.Unlock()
		select {
		case <-recorded:
		case <-timeout:
			h.t.Fatal("no packet sent")
			return sent{}
		}
	}
}

// nextTimeout bounds the wait for a packet that never comes.
const nextTimeout = 10 * time.Second

// quiet asserts that nothing is sent for d. In a synctest bubble (inBubble)
// that is silent: the server is idle and d passes on the bubble's clock.
// Outside one it is a sleep, which proves less: only the tests that cannot
// run in a bubble are left with it (TESTING.md).
func (h *harness) quiet(d time.Duration) {
	h.t.Helper()
	if h.synced {
		h.silent(d)
		return
	}
	time.Sleep(d)
	h.nothingSent()
}

func (h *harness) nothingSent() {
	h.t.Helper()
	out := h.tr.Outbox()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.seen < len(out) {
		h.t.Fatalf("unexpected packet: %+v", decodeSent(h.t, out[h.seen]))
	}
}

// inBubble runs a test in a testing/synctest bubble: the server, its
// timers and the test share a virtual clock, and "the server has done all
// it will do" is an event (idle), not a guess about how long it takes.
// Only for tests on the virtual transport: a goroutine in a socket call
// never counts as blocked.
func inBubble(t *testing.T, fn func(t *testing.T)) {
	t.Helper()
	synctest.Test(t, func(t *testing.T) {
		inBubbles.Store(t, struct{}{})
		defer inBubbles.Delete(t)
		fn(t)
	})
}

// inBubbles holds the tests that run in a bubble, so that the helpers
// shared with the other tests (quiet, waitUntil) know which way to wait.
var inBubbles sync.Map

func bubbled(t *testing.T) bool {
	_, ok := inBubbles.Load(t)
	return ok
}

// idle returns when every goroutine of the bubble is blocked: every frame
// injected so far is handled, and whatever follows needs time to pass.
func idle() { synctest.Wait() }

// silent asserts, in a bubble, that the server sends nothing more: not for
// what it has received (idle), and not during the next d of virtual time.
func (h *harness) silent(d time.Duration) {
	h.t.Helper()
	idle()
	h.nothingSent()
	if d > 0 {
		time.Sleep(d) // virtual: the bubble's clock
		idle()
		h.nothingSent()
	}
}

// diagnostics returns the diagnostics of a kind reported so far.
func (h *harness) diagnostics(kind string) []Diagnostic {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []Diagnostic
	for _, d := range h.diag {
		if d.Kind == kind {
			out = append(out, d)
		}
	}
	return out
}

func decodeSent(t *testing.T, p bip.OutboundPacket) sent {
	t.Helper()
	m, err := bvlc.Parse(p.Data, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	s := sent{pkt: p, bvlc: m}
	if m.Function != bvlc.FunctionOriginalUnicastNPDU && m.Function != bvlc.FunctionOriginalBroadcastNPDU {
		return s
	}
	n, _, err := npdu.Parse(m.Payload, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	s.npdu = n
	if n.NetworkMessage {
		return s
	}
	pdu, err := apdu.Parse(n.APDU, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	s.pdu = pdu
	return s
}

func (h *harness) expectType(want apdu.PDUType) sent {
	h.t.Helper()
	got := h.next()
	if got.pdu.Type != want {
		h.t.Fatalf("got %+v, want type %#x", got.pdu, want)
	}
	return got
}

func (h *harness) expectError(class, code uint16) {
	h.t.Helper()
	got := h.expectType(apdu.TypeError)
	c, k, err := apdu.DecodeErrorClassCode(got.pdu.Error.Payload, bacnet.DefaultDecodeLimits())
	if err != nil || c != class || k != code {
		h.t.Fatalf("error %s/%s (%v), want %s/%s", bacnet.ErrorClassName(c), bacnet.ErrorCodeName(k), err, bacnet.ErrorClassName(class), bacnet.ErrorCodeName(code))
	}
}

func (h *harness) expectReject(reason uint8) {
	h.t.Helper()
	got := h.expectType(apdu.TypeReject)
	if got.pdu.Reject.Reason != reason {
		h.t.Fatalf("reject reason %d, want %d", got.pdu.Reject.Reason, reason)
	}
}

func (h *harness) expectAbort(reason uint8) {
	h.t.Helper()
	got := h.expectType(apdu.TypeAbort)
	if got.pdu.Abort.Reason != reason || !got.pdu.Abort.Server {
		h.t.Fatalf("abort %+v, want reason %d", got.pdu.Abort, reason)
	}
}

func (h *harness) diagnosed(kind string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, d := range h.diag {
		if d.Kind == kind {
			return true
		}
	}
	return false
}

func mustEncode(t *testing.T) func([]byte, error) []byte {
	return func(b []byte, err error) []byte {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
}

func readPV(t *testing.T) []byte {
	return mustEncode(t)(service.EncodeReadProperty(service.ReadPropertyRequest{Object: av1, Property: pvRef}))
}
