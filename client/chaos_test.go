// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/netip"
	"os"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/npdu"
	"github.com/otfabric/go-bacnet/service"
)

// Chaos tests: many goroutines drive one Client against devices that
// misbehave at random (lost, late, duplicated, misaddressed, truncated and
// segmented replies, refusals, floods of unsolicited frames) on a real clock
// with short timeouts. They assert what must hold whatever the devices do:
//
//   - no call hangs, and Close returns;
//   - a call that succeeds returns the value of the object it asked for,
//     never another request's and never one a third party sent;
//   - a call that fails fails with an error of a known category, and a
//     cancelled one keeps its context error;
//   - when the traffic stops, no transaction, reassembly or subscription is
//     left behind;
//   - a subscription's events are in sequence and end with Closed.
//
// A panic or a data race in a handler fails the run by itself (go test -race).
// Every test runs for a few seeds in parallel. BACNET_CHAOS_SEED pins one
// seed, BACNET_CHAOS_SCALE multiplies the load (make chaos).

const (
	chaosAPDUTimeout = 40 * time.Millisecond
	chaosPoison      = 0xDEADBEEF // carried by every reply that must not be accepted
	chaosDevices     = 4
)

func chaosScale() int {
	n, _ := strconv.Atoi(os.Getenv("BACNET_CHAOS_SCALE"))
	if n < 1 {
		n = 1
	}
	return n
}

// chaosSeeds runs fn as a parallel subtest per seed.
func chaosSeeds(t *testing.T, fn func(t *testing.T, seed int64)) {
	seeds := []int64{1, 2, 3}
	if v := os.Getenv("BACNET_CHAOS_SEED"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			t.Fatalf("BACNET_CHAOS_SEED: %v", err)
		}
		seeds = []int64{n}
	} else if testing.Short() {
		seeds = seeds[:1]
	}
	for _, seed := range seeds {
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			t.Parallel()
			fn(t, seed)
		})
	}
}

// chaosRand is a rand.Rand safe for concurrent use.
type chaosRand struct {
	mu sync.Mutex
	r  *rand.Rand
}

func newChaosRand(seed int64) *chaosRand { return &chaosRand{r: rand.New(rand.NewSource(seed))} }

func (c *chaosRand) Intn(n int) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.r.Intn(n)
}

func (c *chaosRand) bytes(n int) []byte {
	b := make([]byte, n)
	c.mu.Lock()
	c.r.Read(b)
	c.mu.Unlock()
	return b
}

// chaosNet is the Transport under the client. Send hands each datagram to
// the device model; replies come back through inject after a delay. Neither
// ever blocks the client.
type chaosNet struct {
	local   bip.Endpoint
	in      chan InboundPacket
	done    chan struct{}
	once    sync.Once
	onSend  func(OutboundPacket)
	devices []bip.Endpoint
}

func newChaosNet() *chaosNet {
	n := &chaosNet{
		local: bip.NewEndpoint(netip.MustParseAddrPort("10.9.0.1:47808")),
		in:    make(chan InboundPacket, 64),
		done:  make(chan struct{}),
	}
	for i := 0; i < chaosDevices; i++ {
		n.devices = append(n.devices, bip.NewEndpoint(netip.AddrPortFrom(netip.AddrFrom4([4]byte{10, 9, 0, byte(10 + i)}), 47808)))
	}
	return n
}

func (n *chaosNet) Local() bip.Endpoint { return n.local }

func (n *chaosNet) Send(ctx context.Context, pkt OutboundPacket) error {
	select {
	case <-n.done:
		return errors.New("chaos: closed")
	default:
	}
	if n.onSend != nil {
		pkt.Data = append([]byte(nil), pkt.Data...)
		n.onSend(pkt)
	}
	return nil
}

func (n *chaosNet) Recv(ctx context.Context) (InboundPacket, error) {
	select {
	case pkt := <-n.in:
		return pkt, nil
	case <-n.done:
		return InboundPacket{}, errors.New("chaos: closed")
	case <-ctx.Done():
		return InboundPacket{}, ctx.Err()
	}
}

func (n *chaosNet) Close() error {
	n.once.Do(func() { close(n.done) })
	return nil
}

// inject delivers a raw datagram from an endpoint after delay.
func (n *chaosNet) inject(from bip.Endpoint, frame []byte, delay time.Duration) {
	time.AfterFunc(delay, func() {
		select {
		case n.in <- InboundPacket{Data: frame, ImmediatePeer: from, ReceivedAt: time.Now()}:
		case <-n.done:
		}
	})
}

// injectInOrder delivers datagrams one after the other.
func (n *chaosNet) injectInOrder(from bip.Endpoint, frames [][]byte, gap time.Duration) {
	go func() {
		for _, frame := range frames {
			select {
			case n.in <- InboundPacket{Data: frame, ImmediatePeer: from, ReceivedAt: time.Now()}:
			case <-n.done:
				return
			}
			if gap > 0 {
				time.Sleep(gap)
			}
		}
	}()
}

func (n *chaosNet) deviceIndex(ep bip.Endpoint) int {
	for i, d := range n.devices {
		if d == ep {
			return i
		}
	}
	return -1
}

func (n *chaosNet) target(i int) Target {
	ep := n.devices[i]
	mac := append(ep.Addr.Addr().AsSlice(), 0xBA, 0xC0)
	return Target{Address: bacnet.LocalStation(bacnet.MustMAC(mac)), Endpoint: ep, MaxAPDU: 480}
}

func chaosFrame(apduBytes []byte) []byte {
	nraw, err := npdu.Append(nil, npdu.NPDU{Version: npdu.Version1, APDU: apduBytes})
	if err != nil {
		panic(err)
	}
	frame, err := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionOriginalUnicastNPDU, Payload: nraw})
	if err != nil {
		panic(err)
	}
	return frame
}

// chaosValue is what device dev holds in Present_Value of instance.
func chaosValue(dev int, instance uint32) uint64 { return uint64(dev+1)<<32 | uint64(instance) }

// Reply behaviours of a chaos device, drawn per request (a retransmission
// draws again).
const (
	chaosOK = iota
	chaosDelayed
	chaosLate
	chaosDuplicate
	chaosDrop
	chaosSegmented
	chaosSegmentedBroken
	chaosError
	chaosReject
	chaosAbort
	chaosWrongInvoke
	chaosWrongSource
	chaosTruncated
	chaosGarbage
	chaosBehaviours
)

// chaosMix is a weight per behaviour.
type chaosMix [chaosBehaviours]int

var (
	chaosHonest = chaosMix{chaosOK: 1}
	chaosSilent = chaosMix{chaosDrop: 1}
	chaosWild   = chaosMix{
		chaosOK: 40, chaosDelayed: 10, chaosLate: 4, chaosDuplicate: 6, chaosDrop: 6,
		chaosSegmented: 10, chaosSegmentedBroken: 4, chaosError: 4, chaosReject: 3, chaosAbort: 3,
		chaosWrongInvoke: 3, chaosWrongSource: 3, chaosTruncated: 2, chaosGarbage: 2,
	}
)

type chaosSub struct {
	dev       int
	processID uint32
	object    bacnet.ObjectIdentifier
	multiple  bool
}

// chaosWorld models the devices behind a chaosNet.
type chaosWorld struct {
	net    *chaosNet
	rnd    *chaosRand
	limits bacnet.DecodeLimits
	mix    atomic.Pointer[chaosMix]

	requests atomic.Int64

	mu   sync.Mutex
	subs []chaosSub
}

func newChaosWorld(seed int64, mix chaosMix) *chaosWorld {
	w := &chaosWorld{net: newChaosNet(), rnd: newChaosRand(seed), limits: bacnet.DefaultDecodeLimits()}
	w.mix.Store(&mix)
	w.net.onSend = w.onSend
	return w
}

func (w *chaosWorld) client(t *testing.T, opts ...Option) *Client {
	t.Helper()
	base := []Option{
		WithTransport(w.net),
		WithTransactionOptions(chaosAPDUTimeout, 1, chaosAPDUTimeout),
	}
	c, err := New(append(base, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func (w *chaosWorld) pick() int {
	mix := w.mix.Load()
	total := 0
	for _, weight := range mix {
		total += weight
	}
	n := w.rnd.Intn(total)
	for b, weight := range mix {
		if n < weight {
			return b
		}
		n -= weight
	}
	return chaosOK
}

func (w *chaosWorld) ms(max int) time.Duration {
	return time.Duration(w.rnd.Intn(max*1000+1)) * time.Microsecond
}

// onSend is the device side of every datagram the client sends.
func (w *chaosWorld) onSend(pkt OutboundPacket) {
	dev := w.net.deviceIndex(pkt.Destination)
	if pkt.Broadcast || dev < 0 {
		return
	}
	msg, err := bvlc.Parse(pkt.Data, w.limits)
	if err != nil {
		return
	}
	n, _, err := npdu.Parse(msg.Payload, w.limits)
	if err != nil || len(n.APDU) == 0 {
		return
	}
	pdu, err := apdu.Parse(n.APDU, w.limits)
	if err != nil || pdu.ConfirmedRequest == nil {
		return // the client's own ACKs, segment ACKs and aborts
	}
	req := pdu.ConfirmedRequest
	w.requests.Add(1)
	from := w.net.devices[dev]

	// The reply a correct device would send, and the same reply carrying
	// the poison value for the misdirected variants.
	var good, poison []byte
	complexACK := false
	switch req.ServiceChoice {
	case apdu.ServiceReadProperty:
		rp, err := service.DecodeReadProperty(req.Payload, w.limits)
		if err != nil {
			w.net.inject(from, chaosFrame(apdu.AppendReject(nil, apdu.RejectPDU{InvokeID: req.InvokeID, Reason: bacnet.RejectReasonInvalidTag})), 0)
			return
		}
		complexACK = true
		good, _ = service.EncodeReadPropertyACK(service.ReadPropertyACK{
			Object: rp.Object, Property: rp.Property, Value: bacnet.UnsignedValue(chaosValue(dev, rp.Object.Instance)),
		})
		poison, _ = service.EncodeReadPropertyACK(service.ReadPropertyACK{
			Object: rp.Object, Property: rp.Property, Value: bacnet.UnsignedValue(chaosPoison),
		})
	case apdu.ServiceSubscribeCOV, apdu.ServiceSubscribeCOVProperty, apdu.ServiceSubscribeCOVPropertyMultiple:
		w.recordSubscription(dev, req)
	default:
		w.net.inject(from, chaosFrame(apdu.AppendReject(nil, apdu.RejectPDU{InvokeID: req.InvokeID, Reason: bacnet.RejectReasonUnrecognizedService})), 0)
		return
	}
	reply := func(invokeID uint8, payload []byte) []byte {
		if !complexACK {
			return chaosFrame(apdu.AppendSimpleACK(nil, apdu.SimpleACK{InvokeID: invokeID, ServiceChoice: req.ServiceChoice}))
		}
		return chaosFrame(apdu.AppendComplexACK(nil, apdu.ComplexACK{InvokeID: invokeID, ServiceChoice: req.ServiceChoice, Payload: payload}))
	}

	behaviour := w.pick()
	if !complexACK {
		// A misdirected SimpleACK cannot be told from the right one, and
		// there is nothing to segment or truncate.
		switch behaviour {
		case chaosSegmented, chaosSegmentedBroken, chaosTruncated, chaosWrongInvoke:
			behaviour = chaosOK
		}
	}
	switch behaviour {
	case chaosOK:
		w.net.inject(from, reply(req.InvokeID, good), 0)
	case chaosDelayed:
		w.net.inject(from, reply(req.InvokeID, good), w.ms(30))
	case chaosLate:
		// After the client has given up; the invoke ID may be in use again.
		w.net.inject(from, reply(req.InvokeID, good), 2*chaosAPDUTimeout+w.ms(80))
	case chaosDuplicate:
		w.net.inject(from, reply(req.InvokeID, good), 0)
		w.net.inject(from, reply(req.InvokeID, good), w.ms(5))
		w.net.inject(from, reply(req.InvokeID, good), w.ms(60))
	case chaosDrop:
	case chaosSegmented, chaosSegmentedBroken:
		w.net.injectInOrder(from, w.segments(req, good, behaviour == chaosSegmentedBroken), w.ms(1))
	case chaosError:
		w.net.inject(from, chaosFrame(apdu.AppendError(nil, apdu.ErrorPDU{
			InvokeID: req.InvokeID, ServiceChoice: req.ServiceChoice, Payload: []byte{0x91, 0x02, 0x91, 0x20},
		})), w.ms(3))
	case chaosReject:
		w.net.inject(from, chaosFrame(apdu.AppendReject(nil, apdu.RejectPDU{InvokeID: req.InvokeID, Reason: uint8(w.rnd.Intn(10))})), w.ms(3))
	case chaosAbort:
		w.net.inject(from, chaosFrame(apdu.AppendAbort(nil, apdu.AbortPDU{Server: true, InvokeID: req.InvokeID, Reason: uint8(w.rnd.Intn(12))})), w.ms(3))
	case chaosWrongInvoke:
		// Lands on nobody, or on another request in flight to this device.
		w.net.inject(from, reply(req.InvokeID+uint8(1+w.rnd.Intn(254)), poison), w.ms(3))
	case chaosWrongSource:
		// The right invoke ID from a device that was not asked.
		other := w.net.devices[(dev+1+w.rnd.Intn(chaosDevices-1))%chaosDevices]
		w.net.inject(other, reply(req.InvokeID, poison), w.ms(3))
	case chaosTruncated:
		frame := reply(req.InvokeID, good)
		w.net.inject(from, frame[:len(frame)-1-w.rnd.Intn(len(good))], w.ms(3))
	case chaosGarbage:
		w.net.inject(from, w.rnd.bytes(1+w.rnd.Intn(64)), w.ms(3))
	}
}

// segments splits a ComplexACK payload into segments sent as one window.
// broken damages the sequence: a missing, repeated or reordered segment, or
// one that never ends.
func (w *chaosWorld) segments(req *apdu.ConfirmedRequest, payload []byte, broken bool) [][]byte {
	count := 2 + w.rnd.Intn(4)
	if count > len(payload) {
		count = len(payload)
	}
	size := (len(payload) + count - 1) / count
	var frames [][]byte
	for seq := 0; seq*size < len(payload); seq++ {
		end := (seq + 1) * size
		if end > len(payload) {
			end = len(payload)
		}
		frames = append(frames, chaosFrame(apdu.AppendComplexACK(nil, apdu.ComplexACK{
			SegmentedMessage:   true,
			MoreFollows:        end < len(payload),
			InvokeID:           req.InvokeID,
			SequenceNumber:     uint8(seq),
			ProposedWindowSize: uint8(count),
			ServiceChoice:      req.ServiceChoice,
			Payload:            payload[seq*size : end],
		})))
	}
	if !broken {
		return frames
	}
	switch i := w.rnd.Intn(len(frames)); w.rnd.Intn(4) {
	case 0: // lost
		frames = append(frames[:i:i], frames[i+1:]...)
	case 1: // repeated
		frames = append(frames[:i+1:i+1], frames[i:]...)
	case 2: // reordered
		j := w.rnd.Intn(len(frames))
		frames[i], frames[j] = frames[j], frames[i]
	case 3: // never finishes
		frames = frames[:len(frames)-1]
	}
	return frames
}

func (w *chaosWorld) recordSubscription(dev int, req *apdu.ConfirmedRequest) {
	sub := chaosSub{dev: dev}
	switch req.ServiceChoice {
	case apdu.ServiceSubscribeCOV:
		r, err := service.DecodeSubscribeCOV(req.Payload, w.limits)
		if err != nil || r.Cancellation {
			return
		}
		sub.processID, sub.object = r.ProcessIdentifier, r.MonitoredObject
	case apdu.ServiceSubscribeCOVProperty:
		r, err := service.DecodeSubscribeCOVProperty(req.Payload, w.limits)
		if err != nil || r.Cancellation {
			return
		}
		sub.processID, sub.object = r.ProcessIdentifier, r.MonitoredObject
	case apdu.ServiceSubscribeCOVPropertyMultiple:
		r, err := service.DecodeSubscribeCOVPropertyMultiple(req.Payload, w.limits)
		if err != nil || r.Cancellation {
			return
		}
		sub.processID, sub.object, sub.multiple = r.SubscriberProcessIdentifier, r.Subscriptions[0].Object, true
	}
	w.mu.Lock()
	w.subs = append(w.subs, sub)
	if len(w.subs) > 256 {
		w.subs = w.subs[len(w.subs)-256:]
	}
	w.mu.Unlock()
}

// notify sends one COV notification for a recorded subscription (or for a
// process identifier nobody has) in a random form. A notification from a
// device other than the subscription's carries the poison value.
func (w *chaosWorld) notify() {
	w.mu.Lock()
	var sub chaosSub
	if len(w.subs) > 0 {
		sub = w.subs[w.rnd.Intn(len(w.subs))]
	}
	w.mu.Unlock()
	if w.rnd.Intn(8) == 0 {
		sub.processID += uint32(1 + w.rnd.Intn(1000))
	}
	from, value := w.net.devices[sub.dev], float32(sub.processID)
	if w.rnd.Intn(8) == 0 {
		from, value = w.net.devices[(sub.dev+1)%chaosDevices], chaosPoison
	}
	device := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: uint32(sub.dev)}
	pv := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}

	var payload []byte
	confirmed, unconfirmed := uint8(apdu.ServiceConfirmedCOVNotification), uint8(apdu.ServiceUnconfirmedCOV)
	if sub.multiple || w.rnd.Intn(6) == 0 {
		confirmed, unconfirmed = apdu.ServiceConfirmedCOVNotificationMultiple, apdu.ServiceUnconfirmedCOVNotificationMultiple
		payload, _ = service.EncodeCOVNotificationMultiple(service.COVNotificationMultiple{
			SubscriberProcessIdentifier: sub.processID, InitiatingDevice: device, TimeRemaining: 60,
			Objects: []service.COVNotificationMultipleObject{{Object: sub.object, Values: []service.COVNotificationMultipleValue{
				{Property: pv, Value: bacnet.RealValue(value), TimeOfChange: &bacnet.Time{Hour: 1}},
			}}},
		})
	} else {
		payload = chaosCOVNotification(sub.processID, device, sub.object, value)
	}
	if w.rnd.Intn(2) == 0 {
		w.net.inject(from, chaosFrame(apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{ServiceChoice: unconfirmed, Payload: payload})), 0)
		return
	}
	w.net.inject(from, chaosFrame(apdu.AppendConfirmedRequest(nil, apdu.ConfirmedRequest{
		InvokeID: uint8(w.rnd.Intn(256)), ServiceChoice: confirmed, MaxAPDU: 5, Payload: payload,
	})), 0)
}

// chaosCOVNotification encodes a COVNotification with one Present_Value.
func chaosCOVNotification(processID uint32, device, object bacnet.ObjectIdentifier, value float32) []byte {
	dst, _ := bacnet.AppendContextUnsigned(nil, 0, uint64(processID))
	dst, _ = bacnet.AppendContextObjectID(dst, 1, device)
	dst, _ = bacnet.AppendContextObjectID(dst, 2, object)
	dst, _ = bacnet.AppendContextUnsigned(dst, 3, 60)
	dst = append(dst, 0x4E)
	dst, _ = bacnet.AppendContextUnsigned(dst, 0, uint64(bacnet.PropertyPresentValue))
	dst = append(dst, 0x2E)
	dst, _ = bacnet.AppendTag(dst, bacnet.Element{Value: bacnet.RealValue(value)})
	return append(dst, 0x2F, 0x4F)
}

// noise injects one frame nobody asked for: random bytes at every layer, and
// every APDU type with a random invoke ID.
func (w *chaosWorld) noise(from bip.Endpoint) {
	id := uint8(w.rnd.Intn(256))
	var a []byte
	switch w.rnd.Intn(14) {
	case 0:
		w.net.inject(from, w.rnd.bytes(w.rnd.Intn(80)), 0)
		return
	case 1: // a BVLC header in front of anything
		raw := append([]byte{0x81, byte(w.rnd.Intn(13)), 0, 0}, w.rnd.bytes(w.rnd.Intn(40))...)
		raw[2], raw[3] = byte(len(raw)>>8), byte(len(raw))
		w.net.inject(from, raw, 0)
		return
	case 2: // a well-formed frame around a random NPDU
		frame, _ := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionOriginalUnicastNPDU, Payload: append([]byte{1}, w.rnd.bytes(w.rnd.Intn(30))...)})
		w.net.inject(from, frame, 0)
		return
	case 3:
		a = w.rnd.bytes(1 + w.rnd.Intn(30))
	case 4:
		a = apdu.AppendSimpleACK(nil, apdu.SimpleACK{InvokeID: id, ServiceChoice: uint8(w.rnd.Intn(40))})
	case 5:
		a = apdu.AppendComplexACK(nil, apdu.ComplexACK{InvokeID: id, ServiceChoice: uint8(w.rnd.Intn(40)), Payload: w.rnd.bytes(w.rnd.Intn(20))})
	case 6:
		a = apdu.AppendComplexACK(nil, apdu.ComplexACK{
			SegmentedMessage: true, MoreFollows: w.rnd.Intn(2) == 0, InvokeID: id, SequenceNumber: uint8(w.rnd.Intn(256)),
			ProposedWindowSize: uint8(w.rnd.Intn(256)), ServiceChoice: apdu.ServiceReadProperty, Payload: w.rnd.bytes(w.rnd.Intn(20)),
		})
	case 7:
		a = apdu.AppendSegmentACK(nil, apdu.SegmentACK{
			NegativeACK: w.rnd.Intn(2) == 0, Server: w.rnd.Intn(2) == 0, InvokeID: id,
			SequenceNumber: uint8(w.rnd.Intn(256)), ActualWindowSize: uint8(w.rnd.Intn(256)),
		})
	case 8:
		a = apdu.AppendError(nil, apdu.ErrorPDU{InvokeID: id, ServiceChoice: uint8(w.rnd.Intn(40)), Payload: w.rnd.bytes(w.rnd.Intn(8))})
	case 9:
		a = apdu.AppendReject(nil, apdu.RejectPDU{InvokeID: id, Reason: uint8(w.rnd.Intn(256))})
	case 10:
		a = apdu.AppendAbort(nil, apdu.AbortPDU{Server: w.rnd.Intn(2) == 0, InvokeID: id, Reason: uint8(w.rnd.Intn(256))})
	case 11: // a confirmed request, possibly a segment of one
		a = apdu.AppendConfirmedRequest(nil, apdu.ConfirmedRequest{
			SegmentedMessage: w.rnd.Intn(3) == 0, MoreFollows: w.rnd.Intn(2) == 0, InvokeID: id, MaxAPDU: 5,
			SequenceNumber: uint8(w.rnd.Intn(256)), ProposedWindowSize: uint8(w.rnd.Intn(256)),
			ServiceChoice: uint8(w.rnd.Intn(40)), Payload: w.rnd.bytes(w.rnd.Intn(20)),
		})
	case 12:
		a = apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{ServiceChoice: uint8(w.rnd.Intn(16)), Payload: w.rnd.bytes(w.rnd.Intn(30))})
	case 13:
		w.notify()
		return
	}
	w.net.inject(from, chaosFrame(a), 0)
}

// chaosGo runs fn in n goroutines and fails the test, with every goroutine's
// stack, if they have not all returned within limit.
func chaosGo(t *testing.T, what string, n int, limit time.Duration, fn func(worker int)) {
	t.Helper()
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fn(i)
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(limit):
		buf := make([]byte, 1<<20)
		t.Fatalf("%s: not finished after %v\n%s", what, limit, buf[:runtime.Stack(buf, true)])
	}
}

// chaosClose closes the client and fails if Close hangs: Close waits for
// every goroutine the client started.
func chaosClose(t *testing.T, c *Client) {
	t.Helper()
	chaosGo(t, "Close", 1, 10*time.Second, func(int) { _ = c.Close() })
}

// chaosIdle waits until the client holds no transaction, reassembly, segment
// transmission or subscription.
func chaosIdle(t *testing.T, c *Client) {
	t.Helper()
	var tx, reserved, rx, send, subs int
	deadline := time.Now().Add(5 * time.Second)
	for {
		c.tx.mu.Lock()
		tx, reserved = len(c.tx.inUse), len(c.tx.reserved)
		c.tx.mu.Unlock()
		c.seg.mu.Lock()
		rx = len(c.seg.active)
		c.seg.mu.Unlock()
		c.seg.send.mu.Lock()
		send = len(c.seg.send.active)
		c.seg.send.mu.Unlock()
		c.subs.mu.Lock()
		subs = len(c.subs.byID)
		c.subs.mu.Unlock()
		if tx+reserved+rx+send+subs == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("left behind after the traffic stopped: %d transactions, %d reserved invoke IDs, %d reassemblies, %d segment transmissions, %d subscriptions",
				tx, reserved, rx, send, subs)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// chaosErrorKind names the category of a request error, or "" for an error
// no caller could act on.
func chaosErrorKind(err error) string {
	var (
		remote *bacnet.ErrorResponse
		reject *bacnet.RejectError
		abort  *bacnet.AbortError
	)
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline"
	case errors.Is(err, bacnet.ErrClosed):
		return "closed"
	case errors.Is(err, bacnet.ErrTimeout):
		return "timeout"
	case errors.As(err, &remote):
		return "error-pdu"
	case errors.As(err, &reject):
		return "reject"
	case errors.As(err, &abort):
		return "abort"
	case errors.Is(err, bacnet.ErrProtocolViolation):
		return "protocol-violation"
	case errors.Is(err, bacnet.ErrMalformed), errors.Is(err, bacnet.ErrTrailingData), errors.Is(err, bacnet.ErrLimitExceeded):
		return "malformed"
	}
	return ""
}

type chaosTally struct {
	mu     sync.Mutex
	counts map[string]int
}

func (c *chaosTally) add(kind string) {
	c.mu.Lock()
	if c.counts == nil {
		c.counts = make(map[string]int)
	}
	c.counts[kind]++
	c.mu.Unlock()
}

// chaosRead reads one fresh object from a random device and checks the
// outcome. ctx decides how the call may end early.
func chaosRead(t *testing.T, ctx context.Context, w *chaosWorld, c *Client, instance uint32, tally *chaosTally) error {
	dev := w.rnd.Intn(chaosDevices)
	object := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: instance}
	start := time.Now()
	value, err := c.ReadProperty(ctx, w.net.target(dev), object, bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue})
	// Two transmissions, each with a possibly segmented reply, and slack for
	// a loaded machine.
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("ReadProperty took %v (error %v)", took, err)
	}
	if err == nil {
		got, uerr := bacnet.AsUnsigned(value)
		if uerr != nil || got != chaosValue(dev, instance) {
			t.Errorf("device %d instance %d: read %+v, want %#x (another request's reply, or one that should have been ignored)",
				dev, instance, value, chaosValue(dev, instance))
		}
		tally.add("ok")
		return nil
	}
	kind := chaosErrorKind(err)
	switch kind {
	case "":
		t.Errorf("ReadProperty failed with an error of no known category: %T %v", err, err)
	case "canceled", "deadline":
		if ctx.Err() == nil {
			t.Errorf("ReadProperty returned %v, but its context has not ended", err)
		}
	}
	tally.add(kind)
	return err
}

// Many goroutines read distinct objects from devices that misbehave in every
// way at once, some calls on short deadlines or cancelled midway.
func TestChaosConcurrentRequests(t *testing.T) {
	chaosSeeds(t, func(t *testing.T, seed int64) {
		w := newChaosWorld(seed, chaosWild)
		c := w.client(t)
		const workers = 48
		perWorker := 40 * chaosScale()
		var instance atomic.Uint32
		var tally chaosTally

		chaosGo(t, "requests", workers, 120*time.Second, func(int) {
			for i := 0; i < perWorker; i++ {
				ctx, cancel := context.WithCancel(context.Background())
				switch w.rnd.Intn(10) {
				case 0, 1: // a deadline that may fall anywhere in the transaction
					ctx, cancel = context.WithTimeout(context.Background(), w.ms(60))
				case 2: // cancelled from outside
					time.AfterFunc(w.ms(40), cancel)
				}
				_ = chaosRead(t, ctx, w, c, instance.Add(1), &tally)
				cancel()
			}
		})

		t.Logf("%d requests reached the devices; outcomes %v", w.requests.Load(), tally.counts)
		if tally.counts["ok"] < workers*perWorker/4 {
			t.Errorf("only %d of %d reads succeeded: the test is not exercising the success path", tally.counts["ok"], workers*perWorker)
		}
		chaosIdle(t, c)

		// The devices behave again: so must the client.
		w.mix.Store(&chaosHonest)
		time.Sleep(4 * chaosAPDUTimeout) // late replies of the chaos phase
		for i := 0; i < 50; i++ {
			if err := chaosRead(t, context.Background(), w, c, instance.Add(1), &tally); err != nil {
				t.Fatalf("read %d after the chaos: %v", i, err)
			}
		}
		chaosIdle(t, c)
		chaosClose(t, c)
	})
}

// More requests than there are invoke IDs, against devices that never
// answer: every call waits its turn and ends, and the IDs come back.
func TestChaosInvokeIDExhaustion(t *testing.T) {
	chaosSeeds(t, func(t *testing.T, seed int64) {
		w := newChaosWorld(seed, chaosSilent)
		c := w.client(t)
		var instance atomic.Uint32
		var tally chaosTally

		chaosGo(t, "requests", 600, 60*time.Second, func(int) {
			ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
			defer cancel()
			if err := chaosRead(t, ctx, w, c, instance.Add(1), &tally); err == nil {
				t.Error("a read succeeded against a device that never answers")
			}
		})
		t.Logf("outcomes %v", tally.counts)
		chaosIdle(t, c)

		w.mix.Store(&chaosHonest)
		chaosGo(t, "recovery", 300, 60*time.Second, func(int) {
			if err := chaosRead(t, context.Background(), w, c, instance.Add(1), &tally); err != nil {
				t.Errorf("read after the devices came back: %v", err)
			}
		})
		chaosIdle(t, c)
		chaosClose(t, c)
	})
}

// Subscriptions of all three kinds are opened and closed from many
// goroutines while the devices flood notifications, for them and for nobody,
// some from the wrong device. Each stream stays in sequence, carries only its
// own notifications and ends with Closed.
func TestChaosSubscriptions(t *testing.T) {
	chaosSeeds(t, func(t *testing.T, seed int64) {
		mix := chaosMix{chaosOK: 80, chaosDelayed: 8, chaosDuplicate: 4, chaosDrop: 3, chaosError: 3, chaosReject: 2}
		w := newChaosWorld(seed, mix)
		c := w.client(t)

		stop := make(chan struct{})
		var flood sync.WaitGroup
		for i := 0; i < 3; i++ {
			flood.Add(1)
			go func() {
				defer flood.Done()
				for {
					select {
					case <-stop:
						return
					default:
						w.notify()
						time.Sleep(w.ms(1))
					}
				}
			}()
		}

		var opened, delivered, gaps atomic.Int64
		rounds := 12 * chaosScale()
		chaosGo(t, "subscribers", 16, 120*time.Second, func(worker int) {
			for round := 0; round < rounds; round++ {
				dev := w.rnd.Intn(chaosDevices)
				object := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: uint32(worker*1000 + round)}
				pv := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
				opts := COVOptions{Lifetime: 1, IssueConfirmed: w.rnd.Intn(2) == 0, BufferSize: 1 + w.rnd.Intn(4)}
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				var (
					sub  Subscription
					err  error
					kind = w.rnd.Intn(3)
				)
				switch kind {
				case 0:
					sub, err = c.SubscribeCOV(ctx, w.net.target(dev), object, opts)
				case 1:
					sub, err = c.SubscribeCOVProperty(ctx, w.net.target(dev), object, pv, opts, nil)
				case 2:
					sub, err = c.SubscribeCOVMultiple(ctx, w.net.target(dev), []service.COVMultipleSubscription{{
						Object: object, Properties: []service.COVPropertyReference{{Property: pv, Timestamped: true}},
					}}, COVMultipleOptions{COVOptions: opts})
				}
				cancel()
				if err != nil {
					if sub != nil {
						t.Errorf("subscribe returned both a subscription and %v", err)
					}
					if chaosErrorKind(err) == "" {
						t.Errorf("subscribe failed with an error of no known category: %T %v", err, err)
					}
					continue
				}
				opened.Add(1)
				id := sub.(*subscription).id

				// Close from one or two goroutines after a while; a slow
				// consumer starts reading only then.
				closed := make(chan struct{})
				go func() {
					time.Sleep(w.ms(40))
					if w.rnd.Intn(3) == 0 {
						go func() { _ = sub.Close() }()
					}
					_ = sub.Close()
					close(closed)
				}()
				if w.rnd.Intn(4) == 0 {
					<-closed
				}

				var last SubscriptionEvent
				timeout := time.After(20 * time.Second)
			events:
				for {
					select {
					case ev, ok := <-sub.Events():
						if !ok {
							break events
						}
						if ev.Sequence <= last.Sequence {
							t.Errorf("subscription %d: sequence %d after %d", id, ev.Sequence, last.Sequence)
						}
						if last.State == SubscriptionClosed {
							t.Errorf("subscription %d: event %+v after Closed", id, ev)
						}
						if ev.Gap {
							gaps.Add(1)
						}
						chaosCheckNotification(t, ev, id, object, kind == 2)
						if ev.Notification != nil || ev.Multiple != nil {
							delivered.Add(1)
						}
						last = ev
					case <-timeout:
						t.Errorf("subscription %d: events not closed 20 s after Close", id)
						break events
					}
				}
				if last.State != SubscriptionClosed {
					t.Errorf("subscription %d: last event %+v, want Closed", id, last)
				}
				<-closed
			}
		})
		close(stop)
		flood.Wait()

		t.Logf("%d subscriptions, %d notifications delivered, %d gaps", opened.Load(), delivered.Load(), gaps.Load())
		if opened.Load() == 0 || delivered.Load() == 0 {
			t.Errorf("%d subscriptions opened and %d notifications delivered: the test is not exercising delivery", opened.Load(), delivered.Load())
		}
		chaosIdle(t, c)
		chaosClose(t, c)
	})
}

// chaosCheckNotification checks that a delivered notification belongs to the
// subscription: its process identifier and object, and not the poison a
// wrong device sent.
func chaosCheckNotification(t *testing.T, ev SubscriptionEvent, id uint32, object bacnet.ObjectIdentifier, multiple bool) {
	t.Helper()
	real := func(v bacnet.ApplicationValue) {
		if got, err := bacnet.AsReal(v); err != nil || got != float32(id) {
			t.Errorf("subscription %d: notification value %+v, want %v (poison is %v)", id, v, float32(id), float32(chaosPoison))
		}
	}
	if n := ev.Notification; n != nil {
		if multiple {
			t.Errorf("subscription %d (multiple): got a single-object notification", id)
		}
		if n.ProcessIdentifier != id || n.MonitoredObject != object || len(n.Values) != 1 {
			t.Errorf("subscription %d on %v: notification %+v", id, object, *n)
			return
		}
		real(n.Values[0].Value)
	}
	if m := ev.Multiple; m != nil {
		if m.SubscriberProcessIdentifier != id || len(m.Objects) != 1 || m.Objects[0].Object != object || len(m.Objects[0].Values) != 1 {
			t.Errorf("subscription %d on %v: multiple notification %+v", id, object, *m)
			return
		}
		real(m.Objects[0].Values[0].Value)
	} else if multiple && ev.Notification == nil && ev.State == SubscriptionActive && ev.Err == nil {
		// A state event; nothing to check.
		return
	}
}

// Close while requests are in flight and subscriptions are open: every call
// returns, every stream ends, and the client refuses further work.
func TestChaosCloseDuringTraffic(t *testing.T) {
	chaosSeeds(t, func(t *testing.T, seed int64) {
		for round := 0; round < 4*chaosScale(); round++ {
			w := newChaosWorld(seed+int64(round)*100, chaosWild)
			c := w.client(t)
			var instance atomic.Uint32
			var tally chaosTally

			go func() {
				time.Sleep(w.ms(120))
				for i := 0; i < 3; i++ {
					go func() { _ = c.Close() }()
				}
			}()
			chaosGo(t, "traffic across Close", 40, 60*time.Second, func(worker int) {
				deadline := time.Now().Add(30 * time.Second)
				if worker%5 == 0 {
					chaosSubscribeUntilClosed(t, w, c, worker, deadline)
					return
				}
				for time.Now().Before(deadline) {
					if err := chaosRead(t, context.Background(), w, c, instance.Add(1), &tally); errors.Is(err, bacnet.ErrClosed) {
						return
					}
				}
				t.Error("requests kept going 30 s after Close")
			})
			chaosClose(t, c)

			if _, err := c.ReadProperty(context.Background(), w.net.target(0), bacnet.AnyDevice(),
				bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName}); !errors.Is(err, bacnet.ErrClosed) {
				t.Errorf("ReadProperty on a closed client: %v, want ErrClosed", err)
			}
			if sub, err := c.SubscribeCOV(context.Background(), w.net.target(0), bacnet.AnyDevice(), COVOptions{}); err == nil || sub != nil {
				t.Errorf("SubscribeCOV on a closed client: %v, %v", sub, err)
			}
			c.tx.mu.Lock()
			left := len(c.tx.inUse)
			c.tx.mu.Unlock()
			if left != 0 {
				t.Errorf("%d transactions left after Close", left)
			}
		}
	})
}

// chaosSubscribeUntilClosed keeps a subscription open until the client is
// closed under it and checks its stream ends with Closed.
func chaosSubscribeUntilClosed(t *testing.T, w *chaosWorld, c *Client, worker int, deadline time.Time) {
	object := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeBinaryValue, Instance: uint32(worker)}
	for time.Now().Before(deadline) {
		sub, err := c.SubscribeCOV(context.Background(), w.net.target(worker%chaosDevices), object, COVOptions{Lifetime: 1})
		if errors.Is(err, bacnet.ErrClosed) {
			return
		}
		if err != nil {
			continue
		}
		var last SubscriptionEvent
		timeout := time.After(time.Until(deadline))
		for open := true; open; {
			select {
			case ev, ok := <-sub.Events():
				if open = ok; ok {
					last = ev
				}
			case <-timeout:
				t.Errorf("worker %d: subscription still open 30 s after Close of the client", worker)
				return
			}
		}
		if last.State != SubscriptionClosed {
			t.Errorf("worker %d: stream ended with %+v, want Closed", worker, last)
		}
		return
	}
}

// A storm of frames nobody asked for, from the devices and from strangers,
// while requests run: nothing crashes, misdirected frames complete nothing
// they should not, and afterwards the client works and holds no state.
func TestChaosInboundStorm(t *testing.T) {
	chaosSeeds(t, func(t *testing.T, seed int64) {
		w := newChaosWorld(seed, chaosHonest)
		c := w.client(t)
		stranger := bip.NewEndpoint(netip.MustParseAddrPort("10.9.0.200:47808"))
		var instance atomic.Uint32
		var tally chaosTally

		stop := make(chan struct{})
		var storm sync.WaitGroup
		for i := 0; i < 4; i++ {
			storm.Add(1)
			go func() {
				defer storm.Done()
				for {
					select {
					case <-stop:
						return
					default:
					}
					// Half from an address the client never talked to:
					// those may end no transaction at all.
					from := stranger
					if i%2 == 0 {
						from = w.net.devices[w.rnd.Intn(chaosDevices)]
					}
					w.noise(from)
					time.Sleep(w.ms(1) / 4)
				}
			}()
		}
		perWorker := 60 * chaosScale()
		chaosGo(t, "requests in the storm", 16, 120*time.Second, func(int) {
			for i := 0; i < perWorker; i++ {
				_ = chaosRead(t, context.Background(), w, c, instance.Add(1), &tally)
			}
		})
		close(stop)
		storm.Wait()
		t.Logf("outcomes %v", tally.counts)
		if tally.counts["ok"] == 0 {
			t.Error("no read succeeded in the storm")
		}

		time.Sleep(4 * chaosAPDUTimeout) // reassemblies the storm started time out
		chaosIdle(t, c)
		for i := 0; i < 50; i++ {
			if err := chaosRead(t, context.Background(), w, c, instance.Add(1), &tally); err != nil {
				t.Fatalf("read %d after the storm: %v", i, err)
			}
		}
		chaosIdle(t, c)
		chaosClose(t, c)
	})
}

// Only frames from strangers: no request may fail because of them.
func TestChaosStrangersCannotEndTransactions(t *testing.T) {
	chaosSeeds(t, func(t *testing.T, seed int64) {
		w := newChaosWorld(seed, chaosMix{chaosOK: 3, chaosDelayed: 1})
		c := w.client(t)
		stranger := bip.NewEndpoint(netip.MustParseAddrPort("10.9.0.200:47808"))
		var instance atomic.Uint32
		var tally chaosTally

		stop := make(chan struct{})
		var storm sync.WaitGroup
		for i := 0; i < 4; i++ {
			storm.Add(1)
			go func() {
				defer storm.Done()
				for {
					select {
					case <-stop:
						return
					default:
						w.noise(stranger)
						time.Sleep(w.ms(1) / 4)
					}
				}
			}()
		}
		perWorker := 60 * chaosScale()
		chaosGo(t, "requests", 16, 120*time.Second, func(int) {
			for i := 0; i < perWorker; i++ {
				if err := chaosRead(t, context.Background(), w, c, instance.Add(1), &tally); err != nil {
					t.Errorf("a read failed although only strangers interfered: %v", err)
				}
			}
		})
		close(stop)
		storm.Wait()
		chaosIdle(t, c)
		chaosClose(t, c)
	})
}
