// SPDX-License-Identifier: MIT

package e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/server"
	"github.com/otfabric/go-bacnet/server/object"
	"github.com/otfabric/go-bacnet/service"
)

// A client's WriteProperty reaches BeforeWrite with the client's own B/IP
// endpoint as the requester.
func TestServerRequesterOfWrite(t *testing.T) {
	got := make(chan server.Requester, 1)
	s := startServer(t, server.WithHooks(server.Hooks{
		BeforeWrite: func(ctx context.Context, _ bacnet.ObjectIdentifier, _ object.Write) error {
			r, ok := server.RequesterOf(ctx)
			if !ok {
				t.Error("BeforeWrite without a requester")
			}
			got <- r
			return nil
		},
	}))
	c := newClient(t)
	if err := c.WriteProperty(ctxT(t), serverTarget(s), av1, presentValue, bacnet.RealValue(3), nil); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-got:
		if !r.Endpoint.Equal(c.LocalEndpoint()) || !r.Via.Equal(c.LocalEndpoint()) || r.Source != (bacnet.Address{}) {
			t.Fatalf("requester %+v, want the client at %v", r, c.LocalEndpoint())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("BeforeWrite was not called")
	}
}

// statusFault reports the fault flag (bit 1) of a Status_Flags value.
func statusFault(v bacnet.ApplicationValue) bool {
	return len(v.BitString.Bytes) > 0 && v.BitString.Bytes[0]&0x40 != 0
}

// Value.SetReliability reaches a client as Reliability, as the fault flag of
// Status_Flags, and in a COV notification to a subscriber.
func TestServerValueReliability(t *testing.T) {
	s := startServer(t)
	iv, err := object.NewIntegerValue(object.ValueConfig{Instance: 7, Name: "IV-7", Integer: 4})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.dev.Add(iv); err != nil {
		t.Fatal(err)
	}
	id := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeIntegerValue, Instance: 7}
	c := newClient(t)
	ctx := ctxT(t)
	tg := serverTarget(s)
	sub, err := c.SubscribeCOV(ctx, tg, id, client.COVOptions{Lifetime: 60})
	if err != nil {
		t.Fatal(err)
	}
	next := func() service.COVNotification {
		t.Helper()
		deadline := time.After(3 * time.Second)
		for {
			select {
			case ev := <-sub.Events():
				if ev.Err != nil {
					t.Fatalf("subscription error: %v", ev.Err)
				}
				if ev.Notification != nil {
					return *ev.Notification
				}
			case <-deadline:
				t.Fatal("no COV notification")
				return service.COVNotification{}
			}
		}
	}
	flags := func(n service.COVNotification) bacnet.ApplicationValue {
		t.Helper()
		for _, pv := range n.Values {
			if pv.Property.Identifier == bacnet.PropertyStatusFlags {
				return pv.Value
			}
		}
		t.Fatalf("no Status_Flags in %+v", n)
		return bacnet.ApplicationValue{}
	}
	if n := next(); statusFault(flags(n)) {
		t.Fatalf("initial notification with fault: %+v", n)
	}

	const communicationFailure = 12 // BACnetReliability communication-failure
	iv.SetReliability(communicationFailure)
	if n := next(); !statusFault(flags(n)) {
		t.Fatalf("notification after SetReliability without fault: %+v", n)
	}
	r, err := c.ReadProperty(ctx, tg, id, bacnet.PropertyReference{Identifier: bacnet.PropertyReliability})
	if err != nil || r.Enumerated != communicationFailure {
		t.Fatalf("Reliability %+v %v", r, err)
	}
	sf, err := c.ReadProperty(ctx, tg, id, bacnet.PropertyReference{Identifier: bacnet.PropertyStatusFlags})
	if err != nil || !statusFault(sf) {
		t.Fatalf("Status_Flags %+v %v", sf, err)
	}

	iv.SetReliability(0) // no-fault-detected
	if n := next(); statusFault(flags(n)) {
		t.Fatalf("notification after recovery with fault: %+v", n)
	}
}
