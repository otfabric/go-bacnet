// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
)

// closingTransport closes the client from inside the first Send, waits until
// ready reports the close has got far enough, and then fails the send the way
// a closed socket does.
type closingTransport struct {
	*chaosNet
	once  sync.Once
	close func()
	ready func() bool
}

func (c *closingTransport) Send(context.Context, OutboundPacket) error {
	c.once.Do(func() {
		go c.close()
		for deadline := time.Now().Add(5 * time.Second); !c.ready() && time.Now().Before(deadline); {
			time.Sleep(time.Millisecond)
		}
	})
	return errors.New("use of closed network connection")
}

func newClosingClient(t *testing.T) (*Client, *closingTransport) {
	t.Helper()
	tr := &closingTransport{chaosNet: newChaosNet()}
	c, err := New(WithTransport(tr), WithTransactionOptions(chaosAPDUTimeout, 0, chaosAPDUTimeout))
	if err != nil {
		t.Fatal(err)
	}
	tr.close = func() { _ = c.Close() }
	t.Cleanup(tr.close)
	return c, tr
}

// A send that fails because the client has been closed reports ErrClosed,
// not the transport's error; a send that fails for another reason keeps it.
func TestSendAfterCloseReportsErrClosed(t *testing.T) {
	t.Parallel()
	c, tr := newClosingClient(t)
	tr.once.Do(func() {}) // plain failing transport
	failure := c.send(context.Background(), OutboundPacket{Destination: tr.devices[0]})
	if failure == nil || errors.Is(failure, bacnet.ErrClosed) {
		t.Fatalf("open client: got %v, want the transport's error", failure)
	}
	chaosClose(t, c)
	if err := c.send(context.Background(), OutboundPacket{Destination: tr.devices[0]}); !errors.Is(err, bacnet.ErrClosed) {
		t.Fatalf("closed client: got %v, want ErrClosed", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.send(ctx, OutboundPacket{Destination: tr.devices[0]}); errors.Is(err, bacnet.ErrClosed) {
		t.Fatalf("cancelled context: got %v, want the send's own error", err)
	}
}

// A request in flight when the client is closed reports ErrClosed.
func TestRequestDuringCloseReportsErrClosed(t *testing.T) {
	t.Parallel()
	c, tr := newClosingClient(t)
	tr.ready = c.isClosed
	_, err := c.ReadProperty(context.Background(), tr.target(0), bacnet.AnyDevice(),
		bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName})
	if !errors.Is(err, bacnet.ErrClosed) {
		t.Fatalf("got %v, want ErrClosed", err)
	}
}

// Close of the client while a subscription request is in flight: Close and
// the failing subscribe both end the half-made subscription. Whichever came
// second used to close the events channel again; which one that is depends on
// scheduling, so this test sets the scene and the chaos tests
// (TestChaosCloseDuringTraffic) provide the repetitions.
func TestSubscribeDuringCloseDoesNotPanic(t *testing.T) {
	t.Parallel()
	c, tr := newClosingClient(t)
	// Until Close has taken the subscription out of the registry and waits
	// for it.
	tr.ready = func() bool {
		c.subs.mu.Lock()
		defer c.subs.mu.Unlock()
		return c.isClosed() && len(c.subs.byID) == 0
	}
	sub, err := c.SubscribeCOV(context.Background(), tr.target(0),
		bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}, COVOptions{})
	if sub != nil || !errors.Is(err, bacnet.ErrClosed) {
		t.Fatalf("got %v, %v, want no subscription and ErrClosed", sub, err)
	}
	chaosClose(t, c)
}
