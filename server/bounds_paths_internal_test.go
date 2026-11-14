// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/server/object"
	"github.com/otfabric/go-bacnet/service"
)

// A ReadPropertyMultiple that runs out of budget at the start of an object,
// known or unknown, is answered with the same Abort as one that runs out
// inside an object.
func TestReadPropertyMultipleBudgetAtObjectStart(t *testing.T) {
	h := newHarness(t)
	unknown := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogInput, Instance: 9}
	dev := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 100}
	for _, second := range []bacnet.ObjectIdentifier{unknown, dev} {
		for p := 1; p <= 12; p++ {
			props := make([]bacnet.PropertyReference, p)
			for i := range props {
				props[i] = bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName}
			}
			specs := []service.ReadAccessSpecification{{Object: unknown, Properties: props}, {Object: second, Properties: props[:1]}}
			for _, seg := range []bool{false, true} {
				req := &apdu.ConfirmedRequest{SegmentedResponseAccepted: seg, MaxAPDU: 0, InvokeID: 1, ServiceChoice: apdu.ServiceReadPropertyMultiple,
					Payload: mustEncode(t)(service.EncodeReadPropertyMultiple(specs))}
				res := h.s.readPropertyMultiple(context.Background(), req, peer{})
				if res.err == nil {
					if len(res.payload) > h.s.responseBudget(req) {
						t.Fatalf("%v p=%d: %d octets over the budget", second, p, len(res.payload))
					}
					continue
				}
				var ab *abortError
				if !errors.As(res.err, &ab) {
					t.Fatalf("%v p=%d seg %v: %+v", second, p, seg, res)
				}
			}
		}
	}
	// Directly at the encoder: begin past the budget is errReplyTooLarge.
	enc := rpmEncoder{budget: 3}
	if err := enc.begin(dev); !errors.Is(err, errReplyTooLarge) {
		t.Fatalf("begin: %v", err)
	}
}

// countingCtx reports cancellation from its n-th Err call on.
type countingCtx struct {
	context.Context
	calls, after int64
}

func (c *countingCtx) Err() error {
	if atomic.AddInt64(&c.calls, 1) > c.after {
		return context.Canceled
	}
	return nil
}

// A GetEventInformation whose deadline passes while the reply is being sized
// is answered with the context error, not a reply.
func TestGetEventInformationDeadlineDuringSearch(t *testing.T) {
	h := newHarness(t, WithMaxAPDU(128), WithSegmentation(0, 0))
	const n = 20
	for i := 0; i < n; i++ {
		ai, err := object.NewAnalogInput(object.PointConfig{Instance: uint32(10 + i), Name: "AI-" + strconv.Itoa(i), Value: 95, Events: &object.EventConfig{
			EventEnable: 7, NotifyType: object.NotifyAlarm, HighLimit: 80, LowLimit: 20, LimitEnable: object.LimitEnableHigh,
		}})
		if err != nil {
			t.Fatal(err)
		}
		if err := h.dev.Add(ai); err != nil {
			t.Fatal(err)
		}
	}
	h.s.events.tick()
	waitUntil(t, func() bool { return len(h.s.eventInfos()) >= n })
	req := &apdu.ConfirmedRequest{MaxAPDU: 5, InvokeID: 1, ServiceChoice: apdu.ServiceGetEventInformation}
	infos := int64(len(h.s.eventInfos()))
	ctx := &countingCtx{Context: context.Background(), after: infos}
	if res := h.s.getEventInformation(ctx, req, peer{}); !errors.Is(res.err, context.Canceled) {
		t.Fatalf("result %+v", res)
	}
	// A reply that fits is sent.
	big := &apdu.ConfirmedRequest{SegmentedResponseAccepted: true, MaxSegments: 7, MaxAPDU: 5, InvokeID: 2, ServiceChoice: apdu.ServiceGetEventInformation}
	res := h.s.getEventInformation(context.Background(), big, peer{})
	ack, err := service.DecodeGetEventInformationACK(res.payload, bacnet.DefaultDecodeLimits())
	if err != nil || len(ack.Summaries) == 0 || len(res.payload) > h.s.responseBudget(big) {
		t.Fatalf("ack %v %+v", err, ack)
	}
}

// Invalid segment and COV reference limits are configuration errors.
func TestBoundOptionsValidated(t *testing.T) {
	dev, err := object.NewDevice(object.DeviceConfig{Instance: 100, Name: "dev", VendorID: 999})
	if err != nil {
		t.Fatal(err)
	}
	for name, opt := range map[string]Option{
		"negative segment limit": WithSegmentLimits(SegmentLimits{Replies: -1}),
		"zero reference limit":   WithCOVReferenceLimit(0),
	} {
		if _, err := New(dev, opt); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// Without segmentation the largest reply is one APDU.
func TestMaxReplyWithoutSegmentation(t *testing.T) {
	h := newHarness(t, WithMaxAPDU(480), WithSegmentation(0, 0))
	if got := h.s.maxReply(); got != 477 {
		t.Fatalf("maxReply %d", got)
	}
}

// AtomicWriteFile with record access gives BeforeWrite one octet string per
// record after the start position.
func TestAtomicWriteFileRecordsBeforeWrite(t *testing.T) {
	var mu sync.Mutex
	var seen []object.Write
	h := newHarness(t, WithHooks(Hooks{BeforeWrite: func(_ context.Context, id bacnet.ObjectIdentifier, w object.Write) error {
		mu.Lock()
		defer mu.Unlock()
		if id.Type == bacnet.ObjectTypeFile {
			seen = append(seen, w)
		}
		return nil
	}}))
	f, err := object.NewFile(object.FileConfig{Instance: 8, Name: "log", Record: true, Records: [][]byte{[]byte("a")}})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.dev.Add(f); err != nil {
		t.Fatal(err)
	}
	id := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 8}
	h.confirmed(1, apdu.ServiceAtomicWriteFile, mustEncode(t)(service.EncodeAtomicWriteFile(service.AtomicWriteFileRequest{
		File: id, Access: service.FileAccessRecord, StartPosition: -1, Records: [][]byte{[]byte("b"), []byte("c")},
	})))
	h.expectType(apdu.TypeComplexACK)
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 1 {
		t.Fatalf("hook saw %+v", seen)
	}
	els := seen[0].Value.Elements
	if len(els) != 3 || els[0].Value.Signed != -1 || string(els[1].Value.OctetString) != "b" || string(els[2].Value.OctetString) != "c" {
		t.Fatalf("write value %+v", seen[0].Value)
	}
}

// Without a BeforeWrite hook the Runtime's WriteGuard allows every write.
func TestRuntimeWriteGuardWithoutHook(t *testing.T) {
	h := newHarness(t)
	g := runtimeInfo{s: h.s}
	if err := g.BeforeWrite(context.Background(), bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}, object.Write{}); err != nil {
		t.Fatal(err)
	}
}
