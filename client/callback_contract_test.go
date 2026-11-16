// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"errors"
	"net/netip"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/internal/virtual"
	"github.com/otfabric/go-bacnet/service"
)

// Rows L9 and L10 of the lifecycle contract for the Client (TESTING.md).
// The event notification handler and the diagnostic function run on the
// client's own goroutines, the receive loop among them. API.md ("Event
// delivery") says a synchronous handler must not call a Client method that
// waits for an answer, and Client.Close says it must not be called from the
// handler or the diagnostic function. These tests show what happens when
// they are. They run in a testing/synctest bubble: the APDU timeout passes
// on the bubble's clock, and "every goroutine waits" is synctest.Wait.

const contractAPDUTimeout = time.Second

// bubblePair is a client and a peer transport on virtual transports, made
// inside a bubble (newVirtualPair marks its test parallel, which a bubble
// does not allow, and drives a manual clock).
func bubblePair(t *testing.T, opts ...Option) *virtualPair {
	t.Helper()
	local := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.1:47808"))
	peerEP := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.2:47808"))
	clientTr := virtual.New(local, nil, 32)
	peerTr := virtual.New(peerEP, nil, 32)
	virtual.Link(clientTr, peerTr)
	c, err := New(append([]Option{
		WithTransport(AdaptVirtual(clientTr)),
		WithTransactionOptions(contractAPDUTimeout, 0, contractAPDUTimeout),
	}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return &virtualPair{
		Client: c, ClientTr: clientTr, PeerTr: peerTr, Local: local, Peer: peerEP,
		Target: Target{Address: bacnet.LocalStation(bacnet.MustMAC([]byte{10, 0, 0, 2, 0xBA, 0xC0})), Endpoint: peerEP, MaxAPDU: 480},
	}
}

var (
	contractObject   = bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	contractProperty = bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
)

// servePresentValue answers every confirmed request of the peer at once
// with a ReadProperty-ACK.
func servePresentValue(ctx context.Context, env *virtualPair) {
	serveComplexACK(ctx, env.PeerTr, env.Local, func(uint8) ([]byte, error) {
		return service.EncodeReadPropertyACK(service.ReadPropertyACK{Object: contractObject, Property: contractProperty, Value: bacnet.RealValue(21.5)})
	})
}

func injectEventNotification(t *testing.T, env *virtualPair) {
	t.Helper()
	payload, err := service.EncodeEventNotification(service.EventNotification{
		ProcessIdentifier: 1,
		InitiatingDevice:  bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 100},
		EventObject:       contractObject,
		TimeStamp:         service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 1},
		NotificationClass: 1, Priority: 2, ToState: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	injectUnicastNPDU(t, env.ClientTr, env.Peer, time.Now(), apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{
		ServiceChoice: apdu.ServiceUnconfirmedEventNotification, Payload: payload,
	}))
}

// L9: a synchronous event notification handler that calls a Client method
// waiting for an answer holds the receive loop, which is what would read
// that answer. The device answers at once; the call still ends at its APDU
// timeout with ErrTimeout, and nothing else is received meanwhile. Once
// the handler has returned the client works again. With WithEventDispatcher
// the handler runs on a worker of its own and the same call is answered.
func TestEventHandlerCallsTheClient(t *testing.T) {
	for _, tc := range []struct {
		name       string
		dispatcher bool
	}{
		{name: "synchronous handler"},
		{name: "handler on the event dispatcher", dispatcher: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				var env *virtualPair
				type outcome struct {
					v       bacnet.ApplicationValue
					err     error
					elapsed time.Duration
				}
				handled := make(chan outcome, 1)
				var calls atomic.Int32
				handler := func(EventNotificationDelivery) {
					if calls.Add(1) != 1 {
						return
					}
					start := time.Now()
					v, err := env.Client.ReadProperty(ctx, env.Target, contractObject, contractProperty)
					handled <- outcome{v, err, time.Since(start)}
				}
				opt := WithEventNotificationHandler(handler)
				if tc.dispatcher {
					opt = WithEventDispatcher(EventDispatcherConfig{Workers: 1, BufferSize: 1, Handler: handler})
				}
				env = bubblePair(t, opt)
				go servePresentValue(ctx, env)
				injectEventNotification(t, env)
				got := <-handled
				if tc.dispatcher {
					if got.err != nil || got.v.Real != 21.5 || got.elapsed != 0 {
						t.Fatalf("ReadProperty from the dispatcher's handler: %+v, %v after %v; want 21.5 at once", got.v, got.err, got.elapsed)
					}
					return
				}
				if !errors.Is(got.err, bacnet.ErrTimeout) || got.elapsed != contractAPDUTimeout {
					t.Fatalf("ReadProperty from the handler: %v after %v; want ErrTimeout after the APDU timeout %v", got.err, got.elapsed, contractAPDUTimeout)
				}
				v, err := env.Client.ReadProperty(ctx, env.Target, contractObject, contractProperty)
				if err != nil || v.Real != 21.5 {
					t.Fatalf("ReadProperty after the handler returned: %+v, %v", v, err)
				}
			})
		})
	}
}

// L10: Close waits for the client's goroutines, the one that runs the
// callback among them. The callback here starts Close on another goroutine
// and waits: Close does not return while the callback runs, and returns
// once it has. Called on the callback's own goroutine, Close would wait for
// itself and never return; that is why it is not allowed there.
func TestCallbackCallsClose(t *testing.T) {
	for _, tc := range []struct {
		name    string
		opt     func(cb func()) Option
		trigger func(t *testing.T, env *virtualPair)
	}{
		{
			name:    "event notification handler",
			opt:     func(cb func()) Option { return WithEventNotificationHandler(func(EventNotificationDelivery) { cb() }) },
			trigger: injectEventNotification,
		},
		{
			name: "diagnostic function",
			opt:  func(cb func()) Option { return WithDiagnosticFunc(func(Diagnostic) { cb() }) },
			trigger: func(t *testing.T, env *virtualPair) {
				env.ClientTr.Inject(virtual.InboundPacket{Data: []byte{0x81, 0x0a, 0x00}, ImmediatePeer: env.Peer, ReceivedAt: time.Now()})
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var env *virtualPair
				var first atomic.Bool
				release := make(chan struct{})
				closed := make(chan error, 1)
				cb := func() {
					if !first.CompareAndSwap(false, true) {
						return
					}
					go func() { closed <- env.Client.Close() }()
					<-release
				}
				env = bubblePair(t, tc.opt(cb))
				tc.trigger(t, env)
				synctest.Wait()
				if !first.Load() {
					t.Fatal("the callback was not called")
				}
				select {
				case err := <-closed:
					t.Fatalf("Close returned (%v) while the callback that called it runs", err)
				default:
				}
				close(release)
				if err := <-closed; err != nil {
					t.Fatalf("Close after the callback returned: %v", err)
				}
			})
		})
	}
}
