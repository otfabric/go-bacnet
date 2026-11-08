// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/internal/capture"
	"github.com/otfabric/go-bacnet/internal/virtual"
	"github.com/otfabric/go-bacnet/service"
)

// replayResult says how one recorded transaction went.
type replayResult int

const (
	replayed replayResult = iota
	skipped               // the client has no call that sends this request as recorded
)

// TestCaptureReplay runs the client against the answers of the recorded
// devices (testdata/captures): for every answered transaction of a service
// the client can request, the client sends the request through its public
// API, the replayer checks that the client's request decodes to the
// recorded one with the same service payload octets, answers with the
// recorded datagrams (invoke ID rewritten), checks every SegmentACK the
// client sends against the one the recorded requester sent, and the call
// must return what the recorded answer holds.
func TestCaptureReplay(t *testing.T) {
	gs, err := capture.LoadGolden()
	if err != nil {
		t.Fatal(err)
	}
	type caseReport struct {
		ID        string         `json:"id"`
		Replayed  map[string]int `json:"replayed_by_service"`
		Segmented int            `json:"segmented_answers"`
		Skipped   int            `json:"not_replayable"`
	}
	var report []caseReport
	total, segmented := 0, 0
	for _, g := range gs {
		msgs, err := g.Messages()
		if err != nil {
			t.Fatal(err)
		}
		r := caseReport{ID: g.ID, Replayed: map[string]int{}}
		txs, _ := capture.Transactions(msgs)
		for _, tx := range txs {
			if !tx.Answered() {
				continue
			}
			name := fmt.Sprintf("%s/frame%d", g.ID, tx.Request[0].Frame)
			res, err := replay(t, tx)
			if err != nil {
				t.Errorf("%s: %v", name, err)
				continue
			}
			if res != replayed {
				r.Skipped++
				continue
			}
			req, _ := tx.Final()
			r.Replayed[strings.TrimPrefix(fmt.Sprintf("%T", req.Service), "service.")]++
			total++
			if len(tx.Response) > 1 {
				r.Segmented++
				segmented++
			}
		}
		if len(r.Replayed) > 0 {
			report = append(report, r)
		}
	}
	if total < 100 || segmented < 10 {
		t.Fatalf("too few transactions replayed: %d, %d segmented", total, segmented)
	}
	if err := capture.Report("client", report); err != nil {
		t.Error(err)
	}
}

func replay(t *testing.T, tx capture.Transaction) (replayResult, error) {
	t.Helper()
	req, resp := tx.Final()
	if len(tx.Request) > 1 || req.Service == nil {
		return skipped, nil // segmented requests follow the recorded client's max-APDU choice
	}
	for _, a := range tx.SegmentACKs {
		if a.APDU.SegmentACK.NegativeACK {
			return skipped, nil
		}
	}
	for i, s := range tx.Response {
		if s.APDU.Type == apdu.TypeComplexACK && s.APDU.ComplexACK.SegmentedMessage && int(s.APDU.ComplexACK.SequenceNumber) != i {
			return skipped, nil // a retransmission
		}
	}
	call, ok := clientCall(req)
	if !ok {
		return skipped, nil
	}
	window := uint8(1)
	for _, a := range tx.SegmentACKs {
		if !a.APDU.SegmentACK.Server {
			window = a.APDU.SegmentACK.ActualWindowSize
			break
		}
	}
	local := bip.NewEndpoint(req.Origin)
	device := bip.NewEndpoint(req.Dst)
	tr := virtual.New(local, nil, 64)
	peer := virtual.New(device, nil, 64)
	virtual.Link(tr, peer)
	c, err := New(WithTransport(AdaptVirtual(tr)), WithTransactionOptions(2*time.Second, 0, 2*time.Second), WithSegmentWindow(window))
	if err != nil {
		return skipped, err
	}
	defer func() { _ = c.Close() }()
	target := Target{Endpoint: device, MaxAPDU: 1476}
	if req.NPDU.Control&0x20 != 0 {
		target.Address = req.NPDU.Destination
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	errc := make(chan error, 1)
	go func() { errc <- serveRecorded(ctx, peer, tx) }()
	got, callErr := call(ctx, c, target)
	if err := <-errc; err != nil {
		return skipped, err
	}
	return replayed, checkOutcome(resp, got, callErr)
}

type call func(context.Context, *Client, Target) (any, error)

// clientCall maps a recorded request onto the client call that sends it.
func clientCall(req capture.Message) (call, bool) {
	switch r := req.Service.(type) {
	case service.ReadPropertyRequest:
		return func(ctx context.Context, c *Client, tg Target) (any, error) {
			return c.ReadProperty(ctx, tg, r.Object, r.Property)
		}, true
	case []service.ReadAccessSpecification:
		return func(ctx context.Context, c *Client, tg Target) (any, error) {
			return c.ReadPropertyMultiple(ctx, tg, r)
		}, true
	case service.ReadRangeRequest:
		return func(ctx context.Context, c *Client, tg Target) (any, error) {
			return c.ReadRange(ctx, tg, r)
		}, true
	case service.AtomicReadFileRequest:
		return func(ctx context.Context, c *Client, tg Target) (any, error) {
			return c.AtomicReadFile(ctx, tg, r)
		}, true
	case service.WritePropertyRequest:
		return func(ctx context.Context, c *Client, tg Target) (any, error) {
			return nil, c.WriteProperty(ctx, tg, r.Object, r.Property, r.Value, r.Priority)
		}, true
	}
	return nil, false
}

// serveRecorded plays the device's side of one transaction.
func serveRecorded(ctx context.Context, peer *virtual.Transport, tx capture.Transaction) error {
	dec := capture.NewDecoder()
	recv := func() (capture.Message, error) {
		pkt, err := peer.Recv(ctx)
		if err != nil {
			return capture.Message{}, fmt.Errorf("waiting for the client: %w", err)
		}
		return dec.Decode(capture.Datagram{Src: pkt.ImmediatePeer.Addr, Dst: peer.Local().Addr, Data: pkt.Data}), nil
	}
	first, err := recv()
	if err != nil {
		return err
	}
	if first.Err != nil || first.APDU == nil || first.APDU.ConfirmedRequest == nil {
		return fmt.Errorf("client sent %v / %+v", first.Err, first.APDU)
	}
	want := tx.Request[0]
	if !reflect.DeepEqual(first.Service, want.Service) {
		return fmt.Errorf("client request %#v, recorded %#v", first.Service, want.Service)
	}
	if string(first.Payload) != string(want.Payload) {
		return fmt.Errorf("client request payload % x, recorded % x", first.Payload, want.Payload)
	}
	invoke := first.APDU.ConfirmedRequest.InvokeID
	events := mergeByFrame(tx.Response, tx.SegmentACKs)
	for _, ev := range events {
		if ev.APDU.Type == apdu.TypeSegmentACK {
			if ev.APDU.SegmentACK.Server {
				continue
			}
			m, err := recv()
			if err != nil {
				return err
			}
			if m.APDU == nil || m.APDU.SegmentACK == nil {
				return fmt.Errorf("client sent %+v where the recorded requester sent SegmentACK %d", m.APDU, ev.APDU.SegmentACK.SequenceNumber)
			}
			g, w := *m.APDU.SegmentACK, *ev.APDU.SegmentACK
			if g.SequenceNumber != w.SequenceNumber || g.ActualWindowSize != w.ActualWindowSize || g.NegativeACK {
				return fmt.Errorf("client SegmentACK %+v, recorded %+v (frame %d)", g, w, ev.Frame)
			}
			continue
		}
		out := append([]byte(nil), ev.Data...)
		out[len(out)-len(ev.NPDU.APDU)+1] = invoke
		if err := peer.Send(ctx, virtual.OutboundPacket{Data: out, Destination: bip.NewEndpoint(first.Src)}); err != nil {
			return err
		}
	}
	return nil
}

func mergeByFrame(a, b []capture.Message) []capture.Message {
	out := append(append([]capture.Message(nil), a...), b...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Frame < out[j-1].Frame; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// checkOutcome compares what the client returned with the recorded answer.
func checkOutcome(resp capture.Message, got any, err error) error {
	p := resp.APDU
	switch p.Type {
	case apdu.TypeError:
		var er *bacnet.ErrorResponse
		d, ok := resp.Service.(apdu.ErrorDetail)
		if !errors.As(err, &er) || !ok || er.Class != d.Class || er.Code != d.Code {
			return fmt.Errorf("recorded Error %+v, client returned %v", resp.Service, err)
		}
		return nil
	case apdu.TypeReject:
		var re *bacnet.RejectError
		if !errors.As(err, &re) || re.Reason != p.Reject.Reason {
			return fmt.Errorf("recorded Reject %d, client returned %v", p.Reject.Reason, err)
		}
		return nil
	case apdu.TypeAbort:
		var ae *bacnet.AbortError
		if !errors.As(err, &ae) || ae.Reason != p.Abort.Reason {
			return fmt.Errorf("recorded Abort %d, client returned %v", p.Abort.Reason, err)
		}
		return nil
	default: // an ACK: compared below
	}
	if err != nil {
		return fmt.Errorf("recorded %#x answer, client returned %v", uint8(p.Type), err)
	}
	if p.Type == apdu.TypeSimpleACK {
		return nil
	}
	want := resp.Service
	if rp, ok := want.(service.ReadPropertyACK); ok {
		want = rp.Value
	}
	if !reflect.DeepEqual(got, want) {
		return fmt.Errorf("client returned %#v, recorded %#v", got, want)
	}
	return nil
}

// TestCaptureReplayDisrupted replays the recorded segmented answers with
// their segments duplicated, the last one lost, or the first two swapped.
// Duplicates must not change the result; a lost or misplaced segment must
// end the call with an error within the segment timeout, never hang it.
func TestCaptureReplayDisrupted(t *testing.T) {
	gs, err := capture.LoadGolden()
	if err != nil {
		t.Fatal(err)
	}
	type variant struct {
		name    string
		mutate  func([]capture.Message) []capture.Message
		mustErr bool
	}
	variants := []variant{
		{"duplicated", func(s []capture.Message) []capture.Message {
			var out []capture.Message
			for _, m := range s {
				out = append(out, m, m)
			}
			return out
		}, false},
		{"last lost", func(s []capture.Message) []capture.Message { return s[:len(s)-1] }, true},
		{"first two swapped", func(s []capture.Message) []capture.Message {
			out := append([]capture.Message(nil), s...)
			out[0], out[1] = out[1], out[0]
			return out
		}, true},
	}
	n := 0
	for _, g := range gs {
		msgs, err := g.Messages()
		if err != nil {
			t.Fatal(err)
		}
		txs, _ := capture.Transactions(msgs)
		for _, tx := range txs {
			req, resp := tx.Final()
			if len(tx.Response) < 2 || len(tx.Request) > 1 || resp.APDU.Type != apdu.TypeComplexACK {
				continue
			}
			call, ok := clientCall(req)
			if !ok {
				continue
			}
			for _, v := range variants {
				start := time.Now()
				got, callErr := disruptedReplay(t, tx, call, v.mutate(tx.Response))
				switch {
				case v.mustErr && callErr == nil:
					t.Errorf("%s frame %d, %s: no error", g.ID, req.Frame, v.name)
				case !v.mustErr:
					if err := checkOutcome(resp, got, callErr); err != nil {
						t.Errorf("%s frame %d, %s: %v", g.ID, req.Frame, v.name, err)
					}
				}
				if d := time.Since(start); d > 3*time.Second {
					t.Errorf("%s frame %d, %s: took %v", g.ID, req.Frame, v.name, d)
				}
				n++
			}
		}
	}
	if n < 30 {
		t.Fatalf("only %d disrupted replays", n)
	}
}

// disruptedReplay answers the client's request with segs, without waiting
// for its SegmentACKs.
func disruptedReplay(t *testing.T, tx capture.Transaction, call call, segs []capture.Message) (any, error) {
	t.Helper()
	req, _ := tx.Final()
	local, device := bip.NewEndpoint(req.Origin), bip.NewEndpoint(req.Dst)
	tr, peer := virtual.New(local, nil, 256), virtual.New(device, nil, 256)
	virtual.Link(tr, peer)
	c, err := New(WithTransport(AdaptVirtual(tr)), WithTransactionOptions(time.Second, 0, 300*time.Millisecond), WithSegmentWindow(16))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	target := Target{Endpoint: device, MaxAPDU: 1476}
	if req.NPDU.Control&0x20 != 0 {
		target.Address = req.NPDU.Destination
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() {
		pkt, err := peer.Recv(ctx)
		if err != nil {
			return
		}
		m := capture.NewDecoder().Decode(capture.Datagram{Src: pkt.ImmediatePeer.Addr, Dst: device.Addr, Data: pkt.Data})
		if m.APDU == nil || m.APDU.ConfirmedRequest == nil {
			return
		}
		for _, s := range segs {
			out := append([]byte(nil), s.Data...)
			out[len(out)-len(s.NPDU.APDU)+1] = m.APDU.ConfirmedRequest.InvokeID
			_ = peer.Send(ctx, virtual.OutboundPacket{Data: out, Destination: local})
		}
	}()
	return call(ctx, c, target)
}
