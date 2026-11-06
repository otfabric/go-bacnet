// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"fmt"
	"math/rand"
	"net/netip"
	"os"
	"strconv"
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

// The server chaos test: several requesters at once send valid requests,
// repeats of them and junk while objects change and COV subscriptions
// notify. Every valid request must get exactly one terminal answer for its
// invoke ID (an ACK, Error, Reject or Abort out-of-resources), every frame
// the server sends must parse, and Close must end every goroutine
// (TestMain). A panic or a data race fails the run by itself (make chaos
// runs it under -race). BACNET_CHAOS_SEED pins one seed, BACNET_CHAOS_SCALE
// multiplies the load.

func chaosScale() int {
	n, _ := strconv.Atoi(os.Getenv("BACNET_CHAOS_SCALE"))
	return max(n, 1)
}

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

// router hands the server's unicast frames to the requester they go to.
type router struct {
	tr    *virtual.Transport
	mu    sync.Mutex
	peers map[string]chan apdu.PDU
	bad   atomic.Value
}

func (r *router) run(ctx context.Context) {
	seen := 0
	for ctx.Err() == nil {
		out := r.tr.Outbox()
		for _, p := range out[seen:] {
			m, err := bvlc.Parse(p.Data, bacnet.DefaultDecodeLimits())
			if err != nil {
				r.bad.Store(fmt.Sprintf("BVLC % x: %v", p.Data, err))
				continue
			}
			if m.Function != bvlc.FunctionOriginalUnicastNPDU && m.Function != bvlc.FunctionOriginalBroadcastNPDU {
				continue
			}
			n, _, err := npdu.Parse(m.Payload, bacnet.DefaultDecodeLimits())
			if err != nil {
				r.bad.Store(fmt.Sprintf("NPDU % x: %v", m.Payload, err))
				continue
			}
			pdu, err := apdu.Parse(n.APDU, bacnet.DefaultDecodeLimits())
			if err != nil {
				r.bad.Store(fmt.Sprintf("APDU % x: %v", n.APDU, err))
				continue
			}
			if p.Broadcast {
				continue
			}
			r.mu.Lock()
			ch := r.peers[p.Destination.String()]
			r.mu.Unlock()
			if ch != nil {
				select {
				case ch <- pdu:
				default:
					r.bad.Store("requester inbox overflow")
				}
			}
		}
		seen = len(out)
		time.Sleep(200 * time.Microsecond)
	}
}

func invokeIDOf(pdu apdu.PDU) (uint8, bool) {
	switch pdu.Type {
	case apdu.TypeSimpleACK:
		return pdu.SimpleACK.InvokeID, true
	case apdu.TypeComplexACK:
		return pdu.ComplexACK.InvokeID, true
	case apdu.TypeError:
		return pdu.Error.InvokeID, true
	case apdu.TypeReject:
		return pdu.Reject.InvokeID, true
	case apdu.TypeAbort:
		return pdu.Abort.InvokeID, true
	default:
		return 0, false
	}
}

func TestChaosServer(t *testing.T) {
	chaosSeeds(t, func(t *testing.T, seed int64) {
		const peers = 6
		perPeer := 60 * chaosScale()
		tr := virtual.New(serverEP, nil, 8192)
		dev := fuzzDevice(t)
		s, err := New(dev, WithTransport(tr), WithWorkers(2, 4), WithTransactionOptions(50*time.Millisecond, 0, 50*time.Millisecond))
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		r := &router{tr: tr, peers: make(map[string]chan apdu.PDU)}
		var wg sync.WaitGroup
		wg.Add(1)
		go func() { defer wg.Done(); r.run(ctx) }()

		// Objects change while requests run; COV notifications follow.
		wg.Add(1)
		go func() {
			defer wg.Done()
			rng := rand.New(rand.NewSource(seed))
			for ctx.Err() == nil {
				if o, ok := dev.Object(av1); ok {
					if w, ok := o.(interface {
						SetPresentValue(bacnet.ApplicationValue) error
					}); ok {
						_ = w.SetPresentValue(bacnet.RealValue(float32(rng.Intn(100))))
					}
				}
				time.Sleep(time.Millisecond)
			}
		}()

		var answered, refused atomic.Int64
		errs := make(chan error, peers)
		var pw sync.WaitGroup
		for i := range peers {
			from := bip.NewEndpoint(netip.AddrPortFrom(netip.AddrFrom4([4]byte{10, 0, 1, byte(i + 1)}), 47808))
			inbox := make(chan apdu.PDU, 4096)
			r.mu.Lock()
			r.peers[from.String()] = inbox
			r.mu.Unlock()
			pw.Add(1)
			go func() {
				defer pw.Done()
				errs <- chaosPeer(t, tr, from, inbox, seed+int64(i)*7919, perPeer, &answered, &refused)
			}()
		}
		pw.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Error(err)
			}
		}
		closed := make(chan struct{})
		go func() { _ = s.Close(); close(closed) }()
		select {
		case <-closed:
		case <-time.After(5 * time.Second):
			t.Fatal("Close did not return")
		}
		cancel()
		wg.Wait()
		if v := r.bad.Load(); v != nil {
			t.Fatalf("server sent a bad frame: %v", v)
		}
		if answered.Load() == 0 {
			t.Fatal("no request answered")
		}
		t.Logf("seed %d: %d answered, %d refused for lack of workers", seed, answered.Load(), refused.Load())
	})
}

func chaosPeer(t *testing.T, tr *virtual.Transport, from bip.Endpoint, inbox <-chan apdu.PDU, seed int64, n int, answered, refused *atomic.Int64) error {
	rng := rand.New(rand.NewSource(seed))
	send := func(a []byte) {
		raw, _ := npdu.Append(nil, npdu.NPDU{Version: npdu.Version1, ExpectingReply: true, APDU: a})
		frame, _ := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionOriginalUnicastNPDU, Payload: raw})
		tr.Inject(bip.InboundPacket{Data: frame, ImmediatePeer: from})
	}
	enc := func(b []byte, _ error) []byte { return b }
	requests := []func() (uint8, []byte){
		func() (uint8, []byte) {
			return apdu.ServiceReadProperty, enc(service.EncodeReadProperty(service.ReadPropertyRequest{Object: av1, Property: pvRef}))
		},
		func() (uint8, []byte) {
			p := uint8(1 + rng.Intn(16))
			return apdu.ServiceWriteProperty, enc(service.EncodeWriteProperty(service.WritePropertyRequest{Object: av1, Property: pvRef, Value: bacnet.RealValue(float32(rng.Intn(50))), Priority: &p}))
		},
		func() (uint8, []byte) {
			return apdu.ServiceReadPropertyMultiple, enc(service.EncodeReadPropertyMultiple([]service.ReadAccessSpecification{{Object: av1, Properties: []bacnet.PropertyReference{{Identifier: bacnet.PropertyAll}}}}))
		},
		func() (uint8, []byte) {
			return apdu.ServiceSubscribeCOV, enc(service.EncodeSubscribeCOV(service.SubscribeCOVRequest{ProcessIdentifier: uint32(rng.Intn(3)), MonitoredObject: av1, Lifetime: 30, Cancellation: rng.Intn(4) == 0}))
		},
		func() (uint8, []byte) {
			return apdu.ServiceReadRange, enc(service.EncodeReadRange(service.ReadRangeRequest{Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeTrendLog, Instance: 1}, Property: bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer}}))
		},
		func() (uint8, []byte) {
			junk := make([]byte, rng.Intn(12))
			rng.Read(junk)
			return uint8(rng.Intn(40)), junk
		},
	}
	for i := range n {
		invokeID := uint8(i)
		if rng.Intn(10) == 0 {
			// Junk frame: no answer is required.
			junk := make([]byte, rng.Intn(24))
			rng.Read(junk)
			tr.Inject(bip.InboundPacket{Data: junk, ImmediatePeer: from})
		}
		svc, payload := requests[rng.Intn(len(requests))]()
		req := apdu.AppendConfirmedRequest(nil, apdu.ConfirmedRequest{MaxAPDU: 5, InvokeID: invokeID, ServiceChoice: svc, Payload: payload})
		send(req)
		if rng.Intn(5) == 0 {
			send(req) // a retransmission
		}
		deadline := time.After(3 * time.Second)
	wait:
		for {
			select {
			case pdu := <-inbox:
				id, ok := invokeIDOf(pdu)
				if !ok || id != invokeID {
					continue // notification, or a late repeat of an earlier reply
				}
				if pdu.Type == apdu.TypeAbort {
					if pdu.Abort.Reason != bacnet.AbortReasonOutOfResources {
						return fmt.Errorf("%v invoke %d: abort reason %d", from, invokeID, pdu.Abort.Reason)
					}
					refused.Add(1)
				}
				answered.Add(1)
				break wait
			case <-deadline:
				return fmt.Errorf("%v invoke %d (service %d): no answer", from, invokeID, svc)
			}
		}
		if rng.Intn(4) == 0 {
			time.Sleep(time.Duration(rng.Intn(500)) * time.Microsecond)
		}
	}
	return nil
}
