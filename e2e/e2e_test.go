// SPDX-License-Identifier: MIT

package e2e_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/client"
)

var presentValue = bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}

func newClient(t *testing.T) *client.Client {
	t.Helper()
	c, err := client.New(
		client.WithLocalAddr("127.0.0.1:0"),
		client.WithTransactionOptions(300*time.Millisecond, 0, time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func ctxT(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// discover finds the device with a unicast Who-Is, as a loopback socket cannot
// broadcast, and returns the target its I-Am produced.
func discover(ctx context.Context, t *testing.T, c *client.Client, d *device) client.Target {
	t.Helper()
	if err := c.SendWhoIs(ctx, d.endpoint(), false, client.DiscoveryOptions{}); err != nil {
		t.Fatal(err)
	}
	for ctx.Err() == nil {
		for _, o := range c.Devices() {
			if o.Instance == deviceInstance {
				return o.Target()
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no I-Am")
	return client.Target{}
}

func TestDiscoverReadWrite(t *testing.T) {
	d := startDevice(t)
	c := newClient(t)
	ctx := ctxT(t)
	target := discover(ctx, t, c, d)

	v, err := c.ReadProperty(ctx, target, analogValue, presentValue)
	if err != nil {
		t.Fatal(err)
	}
	if f, err := bacnet.AsReal(v); err != nil || f != 21.5 {
		t.Fatalf("present value %v %v", v, err)
	}
	name, err := c.ReadProperty(ctx, target, analogValue, bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName})
	if err != nil || name.Character.Value != "room temperature" {
		t.Fatalf("object name %+v %v", name, err)
	}
	if err := c.WriteProperty(ctx, target, analogValue, presentValue, bacnet.RealValue(19), nil); err != nil {
		t.Fatal(err)
	}
	v, err = c.ReadProperty(ctx, target, analogValue, presentValue)
	if err != nil {
		t.Fatal(err)
	}
	if f, err := bacnet.AsReal(v); err != nil || f != 19 {
		t.Fatalf("after the write: %v %v", v, err)
	}
}

func TestDeviceRefusals(t *testing.T) {
	d := startDevice(t)
	c := newClient(t)
	ctx := ctxT(t)
	target := discover(ctx, t, c, d)

	var er *bacnet.ErrorResponse
	if _, err := c.ReadProperty(ctx, target, missing, presentValue); !errors.As(err, &er) || er.Class != errorClassObj || er.Code != errorCodeObj {
		t.Fatalf("unknown object: %v", err)
	}
	err := c.WriteProperty(ctx, target, analogValue, bacnet.PropertyReference{Identifier: bacnet.PropertyDescription}, bacnet.RealValue(1), nil)
	if !errors.As(err, &er) || er.Class != errorClassProp || er.Code != errorCodeProp {
		t.Fatalf("unknown property: %v", err)
	}
}

func TestSilentDeviceTimesOut(t *testing.T) {
	d := startDevice(t)
	c := newClient(t)
	ctx := ctxT(t)
	target := discover(ctx, t, c, d)

	if _, err := c.ReadProperty(ctx, target, silentValue, presentValue); !errors.Is(err, bacnet.ErrTimeout) {
		t.Fatalf("silent device: %v", err)
	}
	// The client is still usable.
	if _, err := c.ReadProperty(ctx, target, analogValue, presentValue); err != nil {
		t.Fatal(err)
	}
}

func TestCOVSubscription(t *testing.T) {
	d := startDevice(t)
	c := newClient(t)
	ctx := ctxT(t)
	target := discover(ctx, t, c, d)

	sub, err := c.SubscribeCOV(ctx, target, analogValue, client.COVOptions{Lifetime: 60})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.WriteProperty(ctx, target, analogValue, presentValue, bacnet.RealValue(7.5), nil); err != nil {
		t.Fatal(err)
	}
	for got := false; !got; {
		select {
		case ev, ok := <-sub.Events():
			if !ok {
				t.Fatal("the stream ended before the notification")
			}
			if ev.Notification == nil {
				continue
			}
			if f, err := bacnet.AsReal(ev.Notification.Values[0].Value); err != nil || f != 7.5 {
				t.Fatalf("notification %+v %v", ev.Notification, err)
			}
			got = true
		case <-ctx.Done():
			t.Fatal("no notification")
		}
	}
	if err := sub.Close(); err != nil {
		t.Fatal(err)
	}
	d.mu.Lock()
	cancelled := d.cancelled
	d.mu.Unlock()
	if !cancelled {
		t.Fatal("Close did not cancel the subscription on the device")
	}
}

func TestConcurrentRequests(t *testing.T) {
	d := startDevice(t)
	c := newClient(t)
	ctx := ctxT(t)
	target := discover(ctx, t, c, d)

	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for i := 0; i < cap(errs); i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := c.ReadProperty(ctx, target, analogValue, presentValue); err != nil {
				errs <- fmt.Errorf("read %d: %w", i, err)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestCloseFailsLaterCalls(t *testing.T) {
	d := startDevice(t)
	c := newClient(t)
	ctx := ctxT(t)
	target := discover(ctx, t, c, d)
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ReadProperty(ctx, target, analogValue, presentValue); !errors.Is(err, bacnet.ErrClosed) {
		t.Fatalf("after Close: %v", err)
	}
}
