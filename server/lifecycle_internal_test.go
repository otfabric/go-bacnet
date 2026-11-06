// SPDX-License-Identifier: MIT

package server

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/internal/clock"
	"github.com/otfabric/go-bacnet/internal/diag"
	"github.com/otfabric/go-bacnet/npdu"
	"github.com/otfabric/go-bacnet/server/object"
	"github.com/otfabric/go-bacnet/service"
)

func TestOptionsValidation(t *testing.T) {
	dev, err := object.NewDevice(object.DeviceConfig{Instance: 1, Name: "d", VendorID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(nil); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil device: %v", err)
	}
	for name, o := range map[string]Option{
		"transport":    WithTransport(nil),
		"port":         WithPort(70000),
		"workers":      WithWorkers(0, 1),
		"queue":        WithWorkers(1, -1),
		"max apdu":     WithMaxAPDU(49),
		"max apdu big": WithMaxAPDU(1477),
		"segments":     WithSegmentation(1, 1),
		"window":       WithSegmentation(4, 0),
		"transaction":  WithTransactionOptions(0, 1, time.Second),
		"retries":      WithTransactionOptions(time.Second, -1, time.Second),
		"password":     WithPassword(strings.Repeat("x", 21)),
		"cov":          WithCOVLimits(-1, 0),
		"cov lifetime": WithCOVLimits(1, -time.Second),
	} {
		if _, err := New(dev, o); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestUDPServer(t *testing.T) {
	dev, err := object.NewDevice(object.DeviceConfig{Instance: 1, Name: "d", VendorID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(dev, WithLocalAddr("256.0.0.1:0")); err == nil {
		t.Fatal("bad local address accepted")
	}
	s, err := New(dev, WithLocalAddr("127.0.0.1:0"), WithPort(0), WithInterface(""), WithDecodeLimits(bacnet.DefaultDecodeLimits()), WithMaxAPDU(480))
	if err != nil {
		t.Fatal(err)
	}
	if s.Device() != dev || !s.Local().Addr.Addr().IsLoopback() {
		t.Fatalf("device %v local %v", s.Device(), s.Local())
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal("second Close:", err)
	}
	if s.submit(func(context.Context) {}) {
		t.Fatal("work accepted after Close")
	}
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func TestLogger(t *testing.T) {
	var buf syncBuffer
	h := newHarness(t, WithLogger(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))))
	h.injectRaw([]byte{0x81, 0x0A, 0x00})
	waitUntil(t, func() bool { return strings.Contains(buf.String(), "kind=malformed") })

	// Below the logger's level nothing is formatted.
	var quiet syncBuffer
	s := newDiagSink(slog.New(slog.NewTextHandler(&quiet, &slog.HandlerOptions{Level: slog.LevelError})), nil)
	s.Report(diag.Event{Kind: diag.KindTransport, Message: "x"})
	if quiet.String() != "" {
		t.Fatalf("logged below level: %q", quiet.String())
	}
}

func TestHousekeeping(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 3, 4, 10, 0, 0, 0, time.Local))
	h := newHarness(t, withClock(clk))
	minutes := uint16(1)
	h.confirmed(1, apdu.ServiceDeviceCommunicationControl, dcc(t, service.EnableDisableDisableInitiation, &minutes, ""))
	h.expectType(apdu.TypeSimpleACK)
	h.confirmed(2, apdu.ServiceSubscribeCOV, mustEncode(t)(service.EncodeSubscribeCOV(service.SubscribeCOVRequest{ProcessIdentifier: 1, MonitoredObject: av1, Lifetime: 30})))
	h.expectType(apdu.TypeSimpleACK)
	// The initial notification is not initiated while DCC disables it.
	h.quiet(20 * time.Millisecond)
	waitUntil(t, func() bool {
		clk.Advance(time.Second)
		return h.s.cov.count() == 0 && h.s.dcc.mayInitiate(clk.Now())
	})
	if err := h.s.IAm(); err != nil {
		t.Fatal(err)
	}
	h.next()
}

func TestRequesterAnswers(t *testing.T) {
	h := newHarness(t)
	other := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.9:47808"))
	answer := func(reply func(id uint8) []byte, from bip.Endpoint) error {
		t.Helper()
		done := make(chan error, 1)
		go func() {
			done <- h.s.req.invoke(context.Background(), clientEP, bacnet.Address{}, apdu.ServiceConfirmedEventNotification, []byte{0x09, 0x01})
		}()
		got := h.expectType(apdu.TypeConfirmedRequest)
		if reply != nil {
			raw := reply(got.pdu.ConfirmedRequest.InvokeID)
			if from != clientEP {
				h.injectFrom(from, bvlc.FunctionOriginalUnicastNPDU, npdu.NPDU{Version: npdu.Version1, APDU: raw})
			}
			h.inject(raw)
		}
		return <-done
	}
	var rj *bacnet.RejectError
	if err := answer(func(id uint8) []byte {
		return apdu.AppendReject(nil, apdu.RejectPDU{InvokeID: id, Reason: bacnet.RejectReasonOther})
	}, other); !errors.As(err, &rj) {
		t.Fatalf("reject: %v", err)
	}
	var ab *bacnet.AbortError
	if err := answer(func(id uint8) []byte {
		return apdu.AppendAbort(nil, apdu.AbortPDU{Server: true, InvokeID: id, Reason: bacnet.AbortReasonOther})
	}, clientEP); !errors.As(err, &ab) {
		t.Fatalf("abort: %v", err)
	}
	if err := answer(func(id uint8) []byte {
		return apdu.AppendError(nil, apdu.ErrorPDU{InvokeID: id, ServiceChoice: 2, Payload: []byte{0xFF}})
	}, clientEP); err == nil {
		t.Fatal("malformed Error accepted")
	}
	if err := answer(func(id uint8) []byte {
		return apdu.AppendComplexACK(nil, apdu.ComplexACK{InvokeID: id, ServiceChoice: 2})
	}, clientEP); err != nil {
		t.Fatalf("complex ack: %v", err)
	}
	// No answer: the request is sent again (one retry), then times out.
	done := make(chan error, 1)
	go func() {
		done <- h.s.req.invoke(context.Background(), clientEP, bacnet.Address{}, apdu.ServiceConfirmedEventNotification, []byte{0x09, 0x01})
	}()
	h.expectType(apdu.TypeConfirmedRequest)
	h.expectType(apdu.TypeConfirmedRequest)
	if err := <-done; !errors.Is(err, bacnet.ErrTimeout) {
		t.Fatalf("timeout: %v", err)
	}
	// An answer for no pending request is ignored.
	h.inject(apdu.AppendSimpleACK(nil, apdu.SimpleACK{InvokeID: 200, ServiceChoice: 2}))
	h.quiet(20 * time.Millisecond)
}
