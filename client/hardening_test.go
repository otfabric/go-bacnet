// SPDX-License-Identifier: MIT

package client

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/internal/virtual"
	"github.com/otfabric/go-bacnet/npdu"
	"github.com/otfabric/go-bacnet/service"
)

// servePeer answers every confirmed request with the APDU reply returns; a
// nil reply leaves the request unanswered.
func servePeer(ctx context.Context, env *virtualPair, reply func(req *apdu.ConfirmedRequest) []byte) {
	limits := bacnet.DefaultDecodeLimits()
	for {
		pkt, err := env.PeerTr.Recv(ctx)
		if err != nil {
			return
		}
		msg, err := bvlc.Parse(pkt.Data, limits)
		if err != nil {
			continue
		}
		n, _, err := npdu.Parse(msg.Payload, limits)
		if err != nil || len(n.APDU) == 0 {
			continue
		}
		pdu, err := apdu.Parse(n.APDU, limits)
		if err != nil || pdu.ConfirmedRequest == nil {
			continue
		}
		out := reply(pdu.ConfirmedRequest)
		if out == nil {
			continue
		}
		nraw, _ := npdu.Append(nil, npdu.NPDU{Version: npdu.Version1, APDU: out})
		frame, _ := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionOriginalUnicastNPDU, Payload: nraw})
		_ = env.PeerTr.Send(ctx, virtual.OutboundPacket{Data: frame, Destination: env.Local})
	}
}

// decodeRPMRequest is the peer-side decoder of a ReadPropertyMultiple request.
func decodeRPMRequest(payload []byte, limits bacnet.DecodeLimits) ([]service.ReadAccessSpecification, error) {
	els, _, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return nil, err
	}
	var specs []service.ReadAccessSpecification
	for _, el := range els {
		switch {
		case el.TagNumber == 0 && !bacnet.IsContextConstructed(el):
			id, err := bacnet.ContextObjectID(el)
			if err != nil {
				return nil, err
			}
			specs = append(specs, service.ReadAccessSpecification{Object: id})
		case el.TagNumber == 1 && bacnet.IsContextConstructed(el) && len(specs) > 0:
			last := &specs[len(specs)-1]
			for _, in := range el.Value.Elements {
				u, err := bacnet.ContextUnsigned(in)
				if err != nil {
					return nil, err
				}
				switch in.TagNumber {
				case 0:
					last.Properties = append(last.Properties, bacnet.PropertyReference{Identifier: bacnet.PropertyIdentifier(u)})
				case 1:
					idx := uint32(u)
					last.Properties[len(last.Properties)-1].ArrayIndex = &idx
				}
			}
		default:
			return nil, errors.New("unexpected RPM request element")
		}
	}
	if len(specs) == 0 {
		return nil, errors.New("empty RPM request")
	}
	return specs, nil
}

// outboxAPDUs decodes every APDU the client sent since index since.
func outboxAPDUs(tr *virtual.Transport, since int) []apdu.PDU {
	limits := bacnet.DefaultDecodeLimits()
	var out []apdu.PDU
	for _, pkt := range tr.Outbox()[since:] {
		msg, err := bvlc.Parse(pkt.Data, limits)
		if err != nil {
			continue
		}
		n, _, err := npdu.Parse(msg.Payload, limits)
		if err != nil || len(n.APDU) == 0 {
			continue
		}
		if pdu, err := apdu.Parse(n.APDU, limits); err == nil {
			out = append(out, pdu)
		}
	}
	return out
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// Two requests that allocate before either registers must not share an invoke
// ID, and an allocation counts against the transaction capacity at once.
func TestInvokeIDAllocationReserves(t *testing.T) {
	m := newTxManager(2, time.Now)
	m.mu.Lock()
	a, okA := m.tryAllocLocked()
	b, okB := m.tryAllocLocked()
	_, okC := m.tryAllocLocked()
	m.mu.Unlock()
	if !okA || !okB || a == b {
		t.Fatalf("allocations a=%d(%v) b=%d(%v) must be distinct", a, okA, b, okB)
	}
	if okC {
		t.Fatal("third allocation exceeded capacity 2")
	}
	m.register(&pendingTx{invokeID: a, result: make(chan txResult, 1)})
	m.register(&pendingTx{invokeID: b, result: make(chan txResult, 1)})
	if len(m.reserved) != 0 || len(m.inUse) != 2 {
		t.Fatalf("reserved=%d inUse=%d after register", len(m.reserved), len(m.inUse))
	}

	// One free ID left among quarantined ones: a second allocator must not
	// wrap around to the ID the first one was just given.
	m = newTxManager(255, time.Now)
	until := time.Now().Add(time.Hour)
	for id := 1; id < 256; id++ {
		m.quarantine[uint8(id)] = until
	}
	m.mu.Lock()
	first, ok1 := m.tryAllocLocked()
	_, ok2 := m.tryAllocLocked()
	m.mu.Unlock()
	if !ok1 || first != 0 || ok2 {
		t.Fatalf("first=%d(%v) second ok=%v, want only ID 0 once", first, ok1, ok2)
	}
}

func TestNewRejectsInvalidOptions(t *testing.T) {
	cases := map[string]Option{
		"segment window 0":       WithSegmentWindow(0),
		"segment window 128":     WithSegmentWindow(128),
		"negative apdu timeout":  WithTransactionOptions(-time.Second, 0, 0),
		"negative retries":       WithTransactionOptions(0, -1, 0),
		"negative segment":       WithTransactionOptions(0, 0, -time.Second),
		"port too large":         WithPort(70000),
		"wrong device confirm":   WithDeviceManagementEnabled("yes"),
		"wrong network confirm":  WithNetworkManagementEnabled("yes"),
		"advertised max too low": WithAdvertisedMaxAPDU(10),
		"dispatcher no handler":  WithEventDispatcher(EventDispatcherConfig{}),
		"foreign device no bbmd": WithForeignDevice(ForeignDeviceConfig{}),
	}
	for name, opt := range cases {
		t.Run(name, func(t *testing.T) {
			tr := virtual.New(bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.1:47808")), nil, 4)
			c, err := New(WithTransport(AdaptVirtual(tr)), opt)
			if err == nil {
				_ = c.Close()
				t.Fatal("New accepted the option")
			}
			if !errors.Is(err, bacnet.ErrInvalidConfig) {
				t.Fatalf("error %v does not wrap ErrInvalidConfig", err)
			}
			// New owns an injected transport, also when it fails.
			if err := tr.Send(context.Background(), virtual.OutboundPacket{}); err == nil {
				t.Fatal("transport left open after failed New")
			}
		})
	}
}

func TestUnservedConfirmedRequestIsRejected(t *testing.T) {
	env := newVirtualPair(t)
	since := len(env.ClientTr.Outbox())
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendConfirmedRequest(nil, apdu.ConfirmedRequest{
		MaxAPDU:       5,
		InvokeID:      77,
		ServiceChoice: apdu.ServiceReadProperty,
		Payload:       []byte{0x0C, 0x02, 0x00, 0x00, 0x01, 0x19, 0x55},
	}))
	waitFor(t, "Reject", func() bool { return len(outboxAPDUs(env.ClientTr, since)) > 0 })
	pdu := outboxAPDUs(env.ClientTr, since)[0]
	if pdu.Reject == nil || pdu.Reject.InvokeID != 77 || pdu.Reject.Reason != bacnet.RejectReasonUnrecognizedService {
		t.Fatalf("got %+v, want Reject invoke=77 reason=unrecognized-service", pdu)
	}
}

func TestMalformedConfirmedNotificationIsRejected(t *testing.T) {
	env := newVirtualPair(t)
	since := len(env.ClientTr.Outbox())
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendConfirmedRequest(nil, apdu.ConfirmedRequest{
		MaxAPDU:       5,
		InvokeID:      9,
		ServiceChoice: apdu.ServiceConfirmedCOVNotification,
		Payload:       []byte{0x09},
	}))
	waitFor(t, "Reject", func() bool { return len(outboxAPDUs(env.ClientTr, since)) > 0 })
	pdu := outboxAPDUs(env.ClientTr, since)[0]
	if pdu.Reject == nil || pdu.Reject.InvokeID != 9 {
		t.Fatalf("got %+v, want Reject invoke=9", pdu)
	}
}

func TestSegmentedConfirmedRequestIsAborted(t *testing.T) {
	env := newVirtualPair(t)
	since := len(env.ClientTr.Outbox())
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendConfirmedRequest(nil, apdu.ConfirmedRequest{
		SegmentedMessage:   true,
		MoreFollows:        true,
		MaxAPDU:            5,
		InvokeID:           12,
		ProposedWindowSize: 4,
		ServiceChoice:      apdu.ServiceConfirmedEventNotification,
		Payload:            []byte{0x09, 0x01},
	}))
	waitFor(t, "Abort", func() bool { return len(outboxAPDUs(env.ClientTr, since)) > 0 })
	pdu := outboxAPDUs(env.ClientTr, since)[0]
	if pdu.Abort == nil || !pdu.Abort.Server || pdu.Abort.InvokeID != 12 ||
		pdu.Abort.Reason != bacnet.AbortReasonSegmentationNotSupported {
		t.Fatalf("got %+v, want server Abort invoke=12 segmentation-not-supported", pdu)
	}
}

// A confirmed request in transit to another network is not for the client.
func TestRoutedConfirmedRequestIsIgnored(t *testing.T) {
	env := newVirtualPair(t)
	since := len(env.ClientTr.Outbox())
	injectUnicastNPDUWithSource(t, env.ClientTr, env.Peer, env.Clk.Now(), npdu.NPDU{
		Version:     npdu.Version1,
		Destination: bacnet.RemoteStation(7, bacnet.MustMAC([]byte{1})),
		HopCount:    255,
		APDU: apdu.AppendConfirmedRequest(nil, apdu.ConfirmedRequest{
			MaxAPDU: 5, InvokeID: 3, ServiceChoice: apdu.ServiceReadProperty, Payload: []byte{0x0C, 0x02, 0x00, 0x00, 0x01, 0x19, 0x55},
		}),
	})
	time.Sleep(30 * time.Millisecond)
	if got := outboxAPDUs(env.ClientTr, since); len(got) != 0 {
		t.Fatalf("client answered a routed request: %+v", got)
	}
}

func TestReadPropertyDeviceWildcard(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	real := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1234}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyObjectIdentifier}
	go servePeer(ctx, env, func(req *apdu.ConfirmedRequest) []byte {
		payload, _ := service.EncodeReadPropertyACK(service.ReadPropertyACK{
			Object: real, Property: prop, Value: bacnet.ObjectIDValue(real),
		})
		return apdu.AppendComplexACK(nil, apdu.ComplexACK{InvokeID: req.InvokeID, ServiceChoice: req.ServiceChoice, Payload: payload})
	})
	val, err := env.Client.ReadProperty(ctx, env.Target, bacnet.AnyDevice(), prop)
	if err != nil {
		t.Fatalf("wildcard read: %v", err)
	}
	if val.ObjectID != real {
		t.Fatalf("got %v, want %v", val.ObjectID, real)
	}
	// A reply for a different object type is still a violation.
	other := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	if _, err := env.Client.ReadProperty(ctx, env.Target, other, prop); !errors.Is(err, bacnet.ErrProtocolViolation) {
		t.Fatalf("mismatched reply: %v, want ErrProtocolViolation", err)
	}
}

// objectListPeer serves an Object_List of n entries. The whole array is
// refused with an Abort; indexed reads work, through ReadPropertyMultiple
// when rpm is true and through ReadProperty otherwise.
func objectListPeer(env *virtualPair, n int, rpm bool, abortReason uint8, maxPerReply int) func(req *apdu.ConfirmedRequest) []byte {
	dev := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
	entry := func(idx uint32) bacnet.ApplicationValue {
		if idx == 0 {
			return bacnet.UnsignedValue(uint64(n))
		}
		return bacnet.ObjectIDValue(bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: idx})
	}
	limits := bacnet.DefaultDecodeLimits()
	return func(req *apdu.ConfirmedRequest) []byte {
		switch req.ServiceChoice {
		case apdu.ServiceReadProperty:
			rp, err := service.DecodeReadProperty(req.Payload, limits)
			if err != nil || rp.Property.ArrayIndex == nil {
				return apdu.AppendAbort(nil, apdu.AbortPDU{Server: true, InvokeID: req.InvokeID, Reason: abortReason})
			}
			payload, _ := service.EncodeReadPropertyACK(service.ReadPropertyACK{
				Object: dev, Property: rp.Property, Value: entry(*rp.Property.ArrayIndex),
			})
			return apdu.AppendComplexACK(nil, apdu.ComplexACK{InvokeID: req.InvokeID, ServiceChoice: req.ServiceChoice, Payload: payload})
		case apdu.ServiceReadPropertyMultiple:
			if !rpm {
				return apdu.AppendReject(nil, apdu.RejectPDU{InvokeID: req.InvokeID, Reason: bacnet.RejectReasonUnrecognizedService})
			}
			specs, err := decodeRPMRequest(req.Payload, limits)
			if err != nil {
				return nil
			}
			if len(specs[0].Properties) > maxPerReply {
				return apdu.AppendAbort(nil, apdu.AbortPDU{Server: true, InvokeID: req.InvokeID, Reason: bacnet.AbortReasonBufferOverflow})
			}
			res := service.ReadAccessResult{Object: dev}
			for _, p := range specs[0].Properties {
				res.Properties = append(res.Properties, service.PropertyResult{Property: p, Value: entry(*p.ArrayIndex)})
			}
			payload, _ := service.EncodeReadPropertyMultipleACK([]service.ReadAccessResult{res})
			return apdu.AppendComplexACK(nil, apdu.ComplexACK{InvokeID: req.InvokeID, ServiceChoice: req.ServiceChoice, Payload: payload})
		}
		return nil
	}
}

func TestReadObjectListFallsBackToIndexedReads(t *testing.T) {
	for _, tc := range []struct {
		name   string
		rpm    bool
		reason uint8
	}{
		{"rpm after segmentation-not-supported", true, bacnet.AbortReasonSegmentationNotSupported},
		{"rpm after buffer-overflow", true, bacnet.AbortReasonBufferOverflow},
		{"read-property when rpm is unsupported", false, bacnet.AbortReasonAPDUTooLong},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newVirtualPair(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			const n = 45
			// The peer returns at most 10 entries per reply, forcing shrinks.
			go servePeer(ctx, env, objectListPeer(env, n, tc.rpm, tc.reason, 10))
			dev := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
			got, err := env.Client.ReadObjectList(ctx, env.Target, dev, 0)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != n {
				t.Fatalf("got %d entries, want %d", len(got), n)
			}
			for i, id := range got {
				if id.Type != bacnet.ObjectTypeAnalogValue || id.Instance != uint32(i+1) {
					t.Fatalf("entry %d = %v", i, id)
				}
			}
			capped, err := env.Client.ReadObjectList(ctx, env.Target, dev, 7)
			if err != nil || len(capped) != 7 {
				t.Fatalf("maxItems=7: %d entries, err %v", len(capped), err)
			}
		})
	}
}

// An Abort that does not mean "reply too large" must not trigger the fallback.
func TestReadObjectListKeepsOtherAborts(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go servePeer(ctx, env, func(req *apdu.ConfirmedRequest) []byte {
		return apdu.AppendAbort(nil, apdu.AbortPDU{Server: true, InvokeID: req.InvokeID, Reason: bacnet.AbortReasonSecurityError})
	})
	dev := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
	_, err := env.Client.ReadObjectList(ctx, env.Target, dev, 0)
	var abort *bacnet.AbortError
	if !errors.As(err, &abort) || abort.Reason != bacnet.AbortReasonSecurityError {
		t.Fatalf("got %v, want the security-error Abort", err)
	}
	if n := len(outboxAPDUs(env.ClientTr, 0)); n != 1 {
		t.Fatalf("client sent %d requests, want 1", n)
	}
}

func TestRPMBatchedSplitsLargeSpecAndMergesResults(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var requests atomic.Int32
	limits := bacnet.DefaultDecodeLimits()
	go servePeer(ctx, env, func(req *apdu.ConfirmedRequest) []byte {
		requests.Add(1)
		specs, err := decodeRPMRequest(req.Payload, limits)
		if err != nil {
			return nil
		}
		var results []service.ReadAccessResult
		for _, s := range specs {
			res := service.ReadAccessResult{Object: s.Object}
			for _, p := range s.Properties {
				pr := service.PropertyResult{Property: p, Value: bacnet.UnsignedValue(uint64(p.Identifier))}
				if p.Identifier == bacnet.PropertyDescription {
					pr = service.PropertyResult{Property: p, Err: &bacnet.ErrorResponse{
						Class: bacnet.ErrorClassProperty, Code: bacnet.ErrorCodeUnknownProperty,
					}}
				}
				res.Properties = append(res.Properties, pr)
			}
			results = append(results, res)
		}
		payload, _ := service.EncodeReadPropertyMultipleACK(results)
		return apdu.AppendComplexACK(nil, apdu.ComplexACK{InvokeID: req.InvokeID, ServiceChoice: req.ServiceChoice, Payload: payload})
	})
	props := []bacnet.PropertyIdentifier{
		bacnet.PropertyObjectName, bacnet.PropertyPresentValue, bacnet.PropertyDescription,
		bacnet.PropertyStatusFlags, bacnet.PropertyUnits,
	}
	mk := func(inst uint32) service.ReadAccessSpecification {
		s := service.ReadAccessSpecification{Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: inst}}
		for _, p := range props {
			s.Properties = append(s.Properties, bacnet.PropertyReference{Identifier: p})
		}
		return s
	}
	specs := []service.ReadAccessSpecification{mk(1), mk(2)}
	got, err := env.Client.ReadPropertyMultipleBatched(ctx, env.Target, specs, RPMBatchOptions{MaxPropertiesPerBatch: 3})
	if err != nil {
		t.Fatal(err)
	}
	if n := requests.Load(); n != 4 { // 10 properties, 3 per batch
		t.Fatalf("%d transactions, want 4", n)
	}
	if len(got) != len(specs) {
		t.Fatalf("%d results, want one per spec", len(got))
	}
	for i, res := range got {
		if res.Object != specs[i].Object || len(res.Properties) != len(props) {
			t.Fatalf("result %d: object %v with %d properties", i, res.Object, len(res.Properties))
		}
		for j, pr := range res.Properties {
			if pr.Property.Identifier != props[j] {
				t.Fatalf("result %d property %d = %v, want %v", i, j, pr.Property.Identifier, props[j])
			}
			if (pr.Err != nil) != (props[j] == bacnet.PropertyDescription) {
				t.Fatalf("result %d property %v: err=%v", i, props[j], pr.Err)
			}
		}
	}
}

// failingTransport fails every Recv immediately, like a socket whose
// interface went away.
type failingTransport struct {
	recvs  atomic.Int64
	closed chan struct{}
}

func (f *failingTransport) Local() bip.Endpoint                        { return bip.Endpoint{} }
func (f *failingTransport) Send(context.Context, OutboundPacket) error { return nil }
func (f *failingTransport) Close() error                               { close(f.closed); return nil }
func (f *failingTransport) Recv(context.Context) (InboundPacket, error) {
	f.recvs.Add(1)
	return InboundPacket{}, errors.New("network is down")
}

func TestReceiveLoopBacksOffOnTransportErrors(t *testing.T) {
	tr := &failingTransport{closed: make(chan struct{})}
	buf := &syncWriter{w: new(bytes.Buffer)}
	c, err := New(WithTransport(tr), WithLogger(slog.New(slog.NewTextHandler(buf, nil))))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	done := make(chan struct{})
	go func() { _ = c.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not interrupt the receive backoff")
	}
	// 5+10+20+40+80 ms of backoff fit in 150 ms: a handful of attempts, not
	// the millions of a busy loop.
	if n := tr.recvs.Load(); n < 2 || n > 12 {
		t.Fatalf("Recv called %d times in 150ms", n)
	}
	if !strings.Contains(buf.String(), "level=WARN") || !strings.Contains(buf.String(), "kind=transport") {
		t.Fatalf("receive failures not logged at WARN: %q", buf.String())
	}
}

func TestLoggerIsSilentByDefaultAndLeveled(t *testing.T) {
	buf := &syncWriter{w: new(bytes.Buffer)}
	l := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	var diags atomic.Int32
	env := newVirtualPair(t, WithLogger(l), WithDiagnosticFunc(func(Diagnostic) { diags.Add(1) }))
	// A malformed frame is a Debug entry: the callback sees it, an Info logger does not.
	env.ClientTr.Inject(virtual.InboundPacket{Data: []byte{0x01}, ImmediatePeer: env.Peer})
	waitFor(t, "diagnostic", func() bool { return diags.Load() > 0 })
	if out := buf.String(); out != "" {
		t.Fatalf("Debug diagnostic logged at Info level: %q", out)
	}
}

// syncWriter lets the test read what a handler wrote from another goroutine.
type syncWriter struct {
	mu sync.Mutex
	w  *bytes.Buffer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

func (s *syncWriter) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.String()
}

func TestDeviceObservationTarget(t *testing.T) {
	dev := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.2:47808"))
	hop := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.254:47808"))
	local, _ := bipMACAddress(dev)
	remote := bacnet.RemoteStation(7, bacnet.MustMAC([]byte{9}))
	caps := DeviceCapabilities{}
	caps.SetIAmFields(1024, segmentationNone, 260)

	for _, tc := range []struct {
		name string
		obs  DeviceObservation
		want Target
	}{
		{"direct", DeviceObservation{Address: local, Origin: dev, ImmediatePeer: dev, Capabilities: caps},
			Target{Address: local, Endpoint: dev, MaxAPDU: 1024}},
		{"forwarded by a BBMD", DeviceObservation{Address: local, Origin: dev, ImmediatePeer: hop, Capabilities: caps},
			Target{Address: local, Endpoint: dev, MaxAPDU: 1024}},
		{"behind a router", DeviceObservation{Address: remote, Origin: hop, ImmediatePeer: hop},
			Target{Address: remote, Endpoint: hop}},
	} {
		if got := tc.obs.Target(); got != tc.want {
			t.Errorf("%s: Target() = %+v, want %+v", tc.name, got, tc.want)
		}
	}
	if id := (DeviceObservation{Instance: 42}).DeviceID(); id.Type != bacnet.ObjectTypeDevice || id.Instance != 42 {
		t.Fatalf("DeviceID() = %v", id)
	}
}

func TestDiscoverWindowReturnsNilError(t *testing.T) {
	env := newVirtualPair(t)
	done := make(chan error, 1)
	var found []DeviceObservation
	go func() {
		var err error
		found, err = env.Client.Discover(context.Background(), DiscoveryOptions{Window: 3 * time.Second})
		done <- err
	}()
	waitFor(t, "Who-Is", func() bool { return len(env.ClientTr.Outbox()) > 0 })
	payload, err := service.EncodeIAm(service.IAm{
		Device:        bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 99},
		MaxAPDULength: 480, Segmentation: segmentationNone, VendorID: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	injectBroadcastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{
		ServiceChoice: apdu.ServiceIAm, Payload: payload,
	}))
	waitFor(t, "I-Am", func() bool { return len(env.Client.Devices()) == 1 })
	select {
	case err := <-done:
		t.Fatalf("Discover returned before the window elapsed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	env.Clk.Advance(3 * time.Second)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Discover with a window: %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Discover did not return after the window")
	}
	if len(found) != 1 || found[0].Instance != 99 || found[0].Target().Endpoint != env.Peer {
		t.Fatalf("found %+v", found)
	}
}

// A Target naming only the endpoint of a directly reachable device must work:
// the reply carries the device's BACnet/IP address, not a zero one.
func TestEndpointOnlyTarget(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	go servePeer(ctx, env, func(req *apdu.ConfirmedRequest) []byte {
		payload, _ := service.EncodeReadPropertyACK(service.ReadPropertyACK{Object: obj, Property: prop, Value: bacnet.RealValue(21.5)})
		return apdu.AppendComplexACK(nil, apdu.ComplexACK{InvokeID: req.InvokeID, ServiceChoice: req.ServiceChoice, Payload: payload})
	})
	val, err := env.Client.ReadProperty(ctx, Target{Endpoint: env.Peer}, obj, prop)
	if err != nil {
		t.Fatalf("endpoint-only target: %v", err)
	}
	if val.Real != 21.5 {
		t.Fatalf("value %v", val)
	}
	if _, err := env.Client.ReadProperty(ctx, Target{}, obj, prop); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("empty target: %v, want ErrMalformed", err)
	}
	if n := len(outboxAPDUs(env.ClientTr, 0)); n != 1 {
		t.Fatalf("%d requests sent, want 1 (nothing for the empty target)", n)
	}
}

func TestResolveTargetNoRoute(t *testing.T) {
	env := newVirtualPair(t)
	_, err := env.Client.ResolveTarget(bacnet.RemoteStation(9, bacnet.MustMAC([]byte{1})), bip.Endpoint{})
	if !errors.Is(err, ErrNoRoute) || !errors.Is(err, bacnet.ErrUnsupported) || !strings.Contains(err.Error(), "network 9") {
		t.Fatalf("got %v", err)
	}
}

// The pager must keep going while pages are full: a peer that returned the
// one record it was asked for clears MORE_ITEMS although records remain.
// MaxItems counts records, not the elements a record decodes into.
func TestReadRangeAllPagesUntilLastItem(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPriorityArray}
	const total = 5
	limits := bacnet.DefaultDecodeLimits()
	go servePeer(ctx, env, func(req *apdu.ConfirmedRequest) []byte {
		rr, err := service.DecodeReadRange(req.Payload, limits)
		if err != nil || rr.Count != 1 {
			return nil
		}
		idx := rr.ReferenceIndex
		// Each "record" is two elements, as a list of pairs would be.
		payload, err := service.EncodeReadRangeACK(service.ReadRangeACK{
			Object: obj, Property: prop,
			ResultFlags: service.EncodeResultFlags(idx == 1, idx == total, false),
			ItemCount:   1,
			ItemData:    []bacnet.ApplicationValue{bacnet.UnsignedValue(uint64(idx)), bacnet.RealValue(float32(idx))},
		})
		if err != nil {
			return nil
		}
		return apdu.AppendComplexACK(nil, apdu.ComplexACK{InvokeID: req.InvokeID, ServiceChoice: req.ServiceChoice, Payload: payload})
	})
	req := service.ReadRangeRequest{Object: obj, Property: prop, By: service.ReadRangeByPosition, ReferenceIndex: 1, Count: 1}

	all, err := env.Client.ReadRangeAll(ctx, env.Target, req, ReadRangePageOptions{PageCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Pages) != total || all.Partial {
		t.Fatalf("%d pages (partial=%v %q), want %d and a complete result", len(all.Pages), all.Partial, all.StoppedReason, total)
	}

	capped, err := env.Client.ReadRangeAll(ctx, env.Target, req, ReadRangePageOptions{PageCount: 1, MaxItems: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(capped.Pages) != 3 || !capped.Partial || capped.StoppedReason != "MaxItems" {
		t.Fatalf("MaxItems=3: %d pages (partial=%v %q)", len(capped.Pages), capped.Partial, capped.StoppedReason)
	}
}

// Raw network messages as a router sends them: X'04' Router-Busy marks the
// route busy, X'05' Router-Available clears it. The two type numbers were
// swapped up to v0.3.0.
func TestRouterBusyAndAvailableWireTypes(t *testing.T) {
	env := newVirtualPair(t)
	inject := func(msgType byte) {
		t.Helper()
		frame, err := bvlc.Append(nil, bvlc.Message{
			Function: bvlc.FunctionOriginalBroadcastNPDU,
			Payload:  []byte{0x01, 0x80, msgType, 0x07, 0xD1}, // network 2001
		})
		if err != nil {
			t.Fatal(err)
		}
		env.ClientTr.Inject(virtual.InboundPacket{Data: frame, ImmediatePeer: env.Peer, ReceivedAt: env.Clk.Now()})
	}
	state := func() (RouteState, bool) {
		rs := env.Client.Routes(2001)
		if len(rs) != 1 {
			return 0, false
		}
		return rs[0].State, true
	}
	inject(0x01) // I-Am-Router-To-Network
	waitFor(t, "route to network 2001", func() bool { s, ok := state(); return ok && s == RouteAvailable })
	if all := env.Client.AllRoutes(); len(all) != 1 || all[0].Network != 2001 || all[0].NextHop != env.Peer {
		t.Fatalf("AllRoutes() = %+v", all)
	}
	inject(0x04) // Router-Busy-To-Network
	waitFor(t, "route marked busy by X'04'", func() bool { s, _ := state(); return s == RouteBusy })
	if _, err := env.Client.ResolveTarget(bacnet.RemoteStation(2001, bacnet.MustMAC([]byte{1})), bip.Endpoint{}); !errors.Is(err, ErrNoRoute) {
		t.Fatalf("ResolveTarget through a busy router: %v, want ErrNoRoute", err)
	}
	inject(0x05) // Router-Available-To-Network
	waitFor(t, "route available again after X'05'", func() bool { s, _ := state(); return s == RouteAvailable })
}

// A failed WritePropertyMultiple is normally a WritePropertyMultiple-Error
// naming the first failed write. System.IO.BACnet sends a plain Error body
// (91 02 91 28): the class and code must still reach the caller.
func TestWritePropertyMultiplePlainErrorBody(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go servePeer(ctx, env, func(req *apdu.ConfirmedRequest) []byte {
		return []byte{0x50, req.InvokeID, apdu.ServiceWritePropertyMultiple, 0x91, 0x02, 0x91, 0x28}
	})
	av := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	err := env.Client.WritePropertyMultiple(ctx, env.Target, []service.WriteAccessSpecification{{
		Object: av,
		Properties: []service.WritePropertyValue{{
			Property: bacnet.PropertyReference{Identifier: bacnet.PropertyObjectType}, Value: bacnet.EnumValue(5),
		}},
	}})
	var remote *bacnet.ErrorResponse
	if !errors.As(err, &remote) {
		t.Fatalf("got %T %v, want *ErrorResponse", err, err)
	}
	if remote.Class != bacnet.ErrorClassProperty || remote.Code != bacnet.ErrorCodeWriteAccessDenied ||
		remote.Service != apdu.ServiceWritePropertyMultiple {
		t.Fatalf("%+v", remote)
	}
	var wpm *service.WritePropertyMultipleError
	if errors.As(err, &wpm) {
		t.Fatalf("a plain Error carries no first-failed write: %+v", wpm)
	}
	var unknown *bacnet.OutcomeUnknownError
	if errors.As(err, &unknown) {
		t.Fatalf("a definite refusal reported as outcome-unknown: %v", err)
	}
}

// An Error PDU whose error-choice names another service than the request
// still answers the request: invoke ID and source identify the transaction.
// BACnet4J answers a SubscribeCOVPropertyMultiple it refuses with
// error-choice 127. The caller must get the device's class and code, not a
// timeout. An acknowledgement for another service is still dropped.
func TestErrorWithForeignServiceChoiceCompletesTheRequest(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go servePeer(ctx, env, func(req *apdu.ConfirmedRequest) []byte {
		if req.ServiceChoice == apdu.ServiceWriteProperty {
			return []byte{0x50, req.InvokeID, 127, 0x91, 0x02, 0x91, 0x28} // property / write-access-denied
		}
		// A SimpleACK naming another service.
		return apdu.AppendSimpleACK(nil, apdu.SimpleACK{InvokeID: req.InvokeID, ServiceChoice: apdu.ServiceWriteProperty})
	})
	av := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	pv := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}

	err := env.Client.WriteProperty(ctx, env.Target, av, pv, bacnet.RealValue(1), nil)
	var remote *bacnet.ErrorResponse
	if !errors.As(err, &remote) {
		t.Fatalf("got %T %v, want *ErrorResponse", err, err)
	}
	if remote.Class != bacnet.ErrorClassProperty || remote.Code != bacnet.ErrorCodeWriteAccessDenied || remote.Service != 127 {
		t.Fatalf("%+v", remote)
	}
	var unknown *bacnet.OutcomeUnknownError
	if errors.As(err, &unknown) {
		t.Fatalf("a definite refusal reported as outcome-unknown: %v", err)
	}

	// The mismatched SimpleACK does not complete a DeleteObject: it times out.
	done := make(chan error, 1)
	go func() {
		done <- env.Client.DeleteObject(ctx, env.Target, service.DeleteObjectRequest{Object: av})
	}()
	waitFor(t, "DeleteObject request", func() bool { return len(outboxAPDUs(env.ClientTr, 0)) >= 2 })
	time.Sleep(20 * time.Millisecond)
	env.Clk.Advance(2 * time.Second)
	select {
	case err := <-done:
		if !errors.Is(err, bacnet.ErrTimeout) {
			t.Fatalf("mismatched SimpleACK: %v, want a timeout", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("DeleteObject did not time out")
	}
}
