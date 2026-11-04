// SPDX-License-Identifier: MIT

package client

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/npdu"
	"github.com/otfabric/go-bacnet/service"
)

// Chaos on the client's segmented-request path: devices that accept
// segmented requests and answer the segments with storms of duplicate, stale,
// misaddressed and impossible Segment-ACKs, abort mid-transfer, fall silent,
// or acknowledge segments never sent. Every call must end, a call that
// succeeds must have delivered the whole request, and no transfer may be
// left behind.

// Behaviours of a segment-receiving device, drawn per transaction.
const (
	segRxHonest = iota
	segRxStorm  // every Segment-ACK comes with a burst of bogus ones
	segRxAbort  // Abort after a random segment
	segRxSilent // stop acknowledging after a random segment
	segRxFuture // acknowledge a segment the client has not sent
	segRxNAK    // one NAK of an earlier segment, then honest
	segRxBehaviours
)

// segRxMaxAPDU is the max APDU the devices announce: small, so that every
// write below is segmented.
const segRxMaxAPDU = 128

type segRxTx struct {
	behaviour int
	next      uint8 // next expected sequence number
	stopAt    int   // segment after which Abort / silence / future ACK
	nakDone   bool
	payload   []byte
	done      bool
}

// segRxWorld is the device side: it reassembles segmented WriteProperty
// requests and remembers the values it wrote.
type segRxWorld struct {
	net *chaosNet
	rnd *chaosRand
	lim bacnet.DecodeLimits
	mix atomic.Pointer[[segRxBehaviours]int]

	mu      sync.Mutex
	txs     map[[2]int]*segRxTx // device, invoke ID
	written map[[2]uint32]string
}

func newSegRxWorld(seed int64, mix [segRxBehaviours]int) *segRxWorld {
	w := &segRxWorld{
		net: newChaosNet(), rnd: newChaosRand(seed), lim: bacnet.DefaultDecodeLimits(),
		txs: map[[2]int]*segRxTx{}, written: map[[2]uint32]string{},
	}
	w.mix.Store(&mix)
	w.net.onSend = w.onSend
	return w
}

func (w *segRxWorld) pick() int {
	mix := w.mix.Load()
	total := 0
	for _, v := range mix {
		total += v
	}
	n := w.rnd.Intn(total)
	for b, v := range mix {
		if n < v {
			return b
		}
		n -= v
	}
	return segRxHonest
}

func (w *segRxWorld) ack(dev int, invokeID, seq uint8, nak bool) []byte {
	return chaosFrame(apdu.AppendSegmentACK(nil, apdu.SegmentACK{
		NegativeACK: nak, Server: true, InvokeID: invokeID, SequenceNumber: seq, ActualWindowSize: 1,
	}))
}

func (w *segRxWorld) onSend(pkt OutboundPacket) {
	dev := w.net.deviceIndex(pkt.Destination)
	if pkt.Broadcast || dev < 0 {
		return
	}
	msg, err := bvlc.Parse(pkt.Data, w.lim)
	if err != nil {
		return
	}
	n, _, err := npdu.Parse(msg.Payload, w.lim)
	if err != nil || len(n.APDU) == 0 {
		return
	}
	pdu, err := apdu.Parse(n.APDU, w.lim)
	if err != nil {
		return
	}
	from := w.net.devices[dev]
	if a := pdu.Abort; a != nil {
		w.mu.Lock()
		delete(w.txs, [2]int{dev, int(a.InvokeID)})
		w.mu.Unlock()
		return
	}
	req := pdu.ConfirmedRequest
	if req == nil {
		return
	}
	if !req.SegmentedMessage {
		// Unsegmented requests are not what this test drives.
		w.net.inject(from, chaosFrame(apdu.AppendReject(nil, apdu.RejectPDU{InvokeID: req.InvokeID, Reason: bacnet.RejectReasonOther})), 0)
		return
	}

	key := [2]int{dev, int(req.InvokeID)}
	w.mu.Lock()
	defer w.mu.Unlock()
	tx := w.txs[key]
	if req.SequenceNumber == 0 && (tx == nil || tx.done || tx.next != 0) {
		tx = &segRxTx{behaviour: w.pick(), stopAt: w.rnd.Intn(4)}
		w.txs[key] = tx
	}
	if tx == nil || tx.done {
		return
	}
	seq := int(req.SequenceNumber)
	switch {
	case req.SequenceNumber != tx.next:
		// Out of order or a repeat: NAK with the last segment received in
		// order (255 when there is none).
		w.net.inject(from, w.ack(dev, req.InvokeID, tx.next-1, true), 0)
		return
	case tx.behaviour == segRxAbort && seq == tx.stopAt:
		tx.done = true
		w.net.inject(from, chaosFrame(apdu.AppendAbort(nil, apdu.AbortPDU{Server: true, InvokeID: req.InvokeID, Reason: bacnet.AbortReasonOther})), 0)
		return
	case tx.behaviour == segRxSilent && seq >= tx.stopAt:
		return
	case tx.behaviour == segRxFuture && seq == tx.stopAt:
		tx.done = true
		w.net.inject(from, w.ack(dev, req.InvokeID, req.SequenceNumber+40, false), 0)
		return
	case tx.behaviour == segRxNAK && seq == tx.stopAt && seq > 0 && !tx.nakDone:
		// Claims the segment got lost: the client must resend from it.
		tx.nakDone = true
		w.net.inject(from, w.ack(dev, req.InvokeID, req.SequenceNumber-1, true), 0)
		return
	}
	tx.payload = append(tx.payload, req.Payload...)
	tx.next++
	w.net.inject(from, w.ack(dev, req.InvokeID, req.SequenceNumber, false), 0)
	if tx.behaviour == segRxStorm {
		bogus := [][]byte{
			w.ack(dev, req.InvokeID, req.SequenceNumber, false),   // duplicate
			w.ack(dev, req.InvokeID, req.SequenceNumber-1, false), // stale
			w.ack(dev, req.InvokeID+1, req.SequenceNumber, false), // another transaction
			chaosFrame(apdu.AppendSegmentACK(nil, apdu.SegmentACK{Server: false, InvokeID: req.InvokeID, SequenceNumber: req.SequenceNumber + 1, ActualWindowSize: 1})),
			chaosFrame(apdu.AppendSegmentACK(nil, apdu.SegmentACK{Server: true, InvokeID: req.InvokeID, SequenceNumber: req.SequenceNumber, ActualWindowSize: 0})),
			chaosFrame(apdu.AppendSegmentACK(nil, apdu.SegmentACK{Server: true, InvokeID: req.InvokeID, SequenceNumber: req.SequenceNumber, ActualWindowSize: 255})),
		}
		for range 1 + w.rnd.Intn(8) {
			w.net.inject(from, bogus[w.rnd.Intn(len(bogus))], w.msDur(2))
		}
		// The same storm from a device that was not asked.
		other := w.net.devices[(dev+1)%chaosDevices]
		w.net.inject(other, w.ack(dev, req.InvokeID, req.SequenceNumber+1, false), 0)
	}
	if req.MoreFollows {
		return
	}
	tx.done = true
	wp, err := service.DecodeWriteProperty(tx.payload, w.lim)
	if err != nil {
		w.net.inject(from, chaosFrame(apdu.AppendReject(nil, apdu.RejectPDU{InvokeID: req.InvokeID, Reason: bacnet.RejectReasonOther})), 0)
		return
	}
	w.written[[2]uint32{uint32(dev), wp.Object.Instance}] = string(wp.Value.OctetString)
	w.net.inject(from, chaosFrame(apdu.AppendSimpleACK(nil, apdu.SimpleACK{InvokeID: req.InvokeID, ServiceChoice: req.ServiceChoice})), w.msDur(3))
}

func (w *segRxWorld) msDur(max int) time.Duration {
	return time.Duration(w.rnd.Intn(max*1000+1)) * time.Microsecond
}

// announce makes every device known as accepting segmented requests.
func (w *segRxWorld) announce(t *testing.T, c *Client) {
	t.Helper()
	for i, ep := range w.net.devices {
		iam, err := service.EncodeIAm(service.IAm{
			Device:        bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: uint32(100 + i)},
			MaxAPDULength: segRxMaxAPDU, Segmentation: segmentationBoth, VendorID: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		w.net.inject(ep, chaosFrame(apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{ServiceChoice: apdu.ServiceIAm, Payload: iam})), 0)
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(c.Devices()) < len(w.net.devices) {
		if time.Now().After(deadline) {
			t.Fatal("the devices' I-Am were not recorded")
		}
		time.Sleep(time.Millisecond)
	}
}

func (w *segRxWorld) target(dev int) Target {
	t := w.net.target(dev)
	t.MaxAPDU = segRxMaxAPDU
	return t
}

// segRxWrite writes a value too large for one APDU and checks the outcome.
func segRxWrite(t *testing.T, w *segRxWorld, c *Client, instance uint32, tally *chaosTally) error {
	dev := w.rnd.Intn(chaosDevices)
	object := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: instance}
	value := bytes.Repeat([]byte{byte(instance)}, 200+int(instance%300))
	start := time.Now()
	err := c.WriteProperty(context.Background(), w.target(dev), object,
		bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
		bacnet.ApplicationValue{Kind: bacnet.ValueOctetString, OctetString: value}, nil)
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("segmented WriteProperty took %v (error %v)", took, err)
	}
	if err == nil {
		w.mu.Lock()
		got, ok := w.written[[2]uint32{uint32(dev), instance}]
		w.mu.Unlock()
		if !ok || got != string(value) {
			t.Errorf("device %d instance %d: write succeeded but the device holds %d bytes, want %d", dev, instance, len(got), len(value))
		}
		tally.add("ok")
		return nil
	}
	var unknown *bacnet.OutcomeUnknownError
	if errors.As(err, &unknown) {
		err = unknown.Cause
	}
	kind := chaosErrorKind(err)
	if kind == "" {
		t.Errorf("segmented WriteProperty failed with an error of no known category: %T %v", err, err)
	}
	tally.add(kind)
	return err
}

func TestChaosSegmentACKStorm(t *testing.T) {
	chaosSeeds(t, func(t *testing.T, seed int64) {
		w := newSegRxWorld(seed, [segRxBehaviours]int{
			segRxHonest: 30, segRxStorm: 30, segRxAbort: 10, segRxSilent: 10, segRxFuture: 10, segRxNAK: 10,
		})
		c := newSegRxClient(t, w)
		w.announce(t, c)
		var instance atomic.Uint32
		var tally chaosTally
		chaosGo(t, "segmented writes", 16, 120*time.Second, func(int) {
			for range 15 * chaosScale() {
				_ = segRxWrite(t, w, c, instance.Add(1), &tally)
			}
		})
		t.Logf("outcomes %v", tally.counts)
		if tally.counts["ok"] == 0 {
			t.Error("no segmented write succeeded: the test is not exercising the success path")
		}
		chaosIdle(t, c)

		honest := [segRxBehaviours]int{segRxHonest: 1}
		w.mix.Store(&honest)
		time.Sleep(4 * chaosAPDUTimeout)
		for i := range 10 {
			if err := segRxWrite(t, w, c, instance.Add(1), &tally); err != nil {
				t.Fatalf("write %d after the storm: %v", i, err)
			}
		}
		chaosIdle(t, c)
		chaosClose(t, c)
	})
}

func newSegRxClient(t *testing.T, w *segRxWorld) *Client {
	t.Helper()
	c, err := New(
		WithTransport(w.net),
		WithTransactionOptions(chaosAPDUTimeout, 1, chaosAPDUTimeout),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// TestSegmentACKRewindLivelock: a device that acknowledges the first segment
// and then NAKs back to the start, over and over, makes progress forever by
// the client's count. A call with no deadline must still end.
func TestSegmentACKRewindLivelock(t *testing.T) {
	w := newSegRxWorld(1, [segRxBehaviours]int{segRxHonest: 1})
	w.net.onSend = func(pkt OutboundPacket) {
		dev := w.net.deviceIndex(pkt.Destination)
		if dev < 0 {
			return
		}
		msg, err := bvlc.Parse(pkt.Data, w.lim)
		if err != nil {
			return
		}
		n, _, err := npdu.Parse(msg.Payload, w.lim)
		if err != nil {
			return
		}
		pdu, err := apdu.Parse(n.APDU, w.lim)
		if err != nil || pdu.ConfirmedRequest == nil || !pdu.ConfirmedRequest.SegmentedMessage {
			return
		}
		req := pdu.ConfirmedRequest
		from := w.net.devices[dev]
		if req.SequenceNumber == 0 {
			w.net.inject(from, w.ack(dev, req.InvokeID, 0, false), 0)
			w.net.inject(from, w.ack(dev, req.InvokeID, 255, true), time.Millisecond)
		}
	}
	c := newSegRxClient(t, w)
	w.announce(t, c)
	done := make(chan error, 1)
	go func() {
		done <- c.WriteProperty(context.Background(), w.target(0),
			bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
			bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
			bacnet.ApplicationValue{Kind: bacnet.ValueOctetString, OctetString: make([]byte, 400)}, nil)
	}()
	// The APDU timeout is 40 ms with one retry; 100 times that is generous.
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a write the device never completed succeeded")
		}
	case <-time.After(4 * time.Second):
		_ = c.Close()
		if err := <-done; err == nil {
			t.Error("the write ended without error after Close")
		}
		t.Fatal("WriteProperty with no deadline never ended: a device that ACKs segment 0 and NAKs back to the start keeps the transfer alive forever")
	}
	chaosClose(t, c)
}

// TestSegmentNAKBeyondSent: a NAK naming a segment the client has not sent
// yet cannot be honoured; the client must not take it as "everything up to
// there arrived" and finish the transfer with segments never sent.
func TestSegmentNAKBeyondSent(t *testing.T) {
	w := newSegRxWorld(1, [segRxBehaviours]int{segRxHonest: 1})
	var segmentsSeen atomic.Int32
	w.net.onSend = func(pkt OutboundPacket) {
		dev := w.net.deviceIndex(pkt.Destination)
		if dev < 0 {
			return
		}
		msg, err := bvlc.Parse(pkt.Data, w.lim)
		if err != nil {
			return
		}
		n, _, err := npdu.Parse(msg.Payload, w.lim)
		if err != nil {
			return
		}
		pdu, err := apdu.Parse(n.APDU, w.lim)
		if err != nil || pdu.ConfirmedRequest == nil || !pdu.ConfirmedRequest.SegmentedMessage {
			return
		}
		req := pdu.ConfirmedRequest
		segmentsSeen.Add(1)
		if req.SequenceNumber == 0 {
			from := w.net.devices[dev]
			w.net.inject(from, w.ack(dev, req.InvokeID, 200, true), 0)
			w.net.inject(from, chaosFrame(apdu.AppendSimpleACK(nil, apdu.SimpleACK{InvokeID: req.InvokeID, ServiceChoice: req.ServiceChoice})), 5*time.Millisecond)
		}
	}
	c := newSegRxClient(t, w)
	w.announce(t, c)
	err := c.WriteProperty(context.Background(), w.target(0),
		bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
		bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
		bacnet.ApplicationValue{Kind: bacnet.ValueOctetString, OctetString: make([]byte, 4000)}, nil)
	if err == nil {
		t.Fatalf("write reported successful after the client sent only %d segments of a 4000-octet request (about 33 needed)", segmentsSeen.Load())
	}
	chaosClose(t, c)
}
