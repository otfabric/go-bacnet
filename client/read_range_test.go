// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/service"
)

func TestReadRangeComplexACK(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeTrendLog, Instance: 1}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer}
	go serveComplexACK(ctx, env.PeerTr, env.Local, func(serviceChoice uint8) ([]byte, error) {
		if serviceChoice != apdu.ServiceReadRange {
			return nil, bacnet.ErrProtocolViolation
		}
		return service.EncodeReadRangeACK(service.ReadRangeACK{
			Object:      obj,
			Property:    prop,
			ResultFlags: service.EncodeResultFlags(true, true, false),
			ItemCount:   1,
			ItemData:    []bacnet.ApplicationValue{bacnet.RealValue(3.25)},
		})
	})

	ack, err := env.Client.ReadRange(ctx, env.Target, service.ReadRangeRequest{
		Object:         obj,
		Property:       prop,
		By:             service.ReadRangeByPosition,
		ReferenceIndex: 1,
		Count:          1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ack.FirstItem() || !ack.LastItem() || ack.MoreItems() || ack.ItemCount != 1 {
		t.Fatalf("ack %+v", ack)
	}
}

func TestDefaultRetransmitPolicyReadRange(t *testing.T) {
	if DefaultRetransmitPolicy(apdu.ServiceReadRange) != RetransmitEnabled {
		t.Fatal("ReadRange should enable exact-APDU retransmit")
	}
	_ = time.Second
}

func TestReadRangeRejectsWrongObjectEcho(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reqObj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeTrendLog, Instance: 1}
	wrong := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeTrendLog, Instance: 9}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer}
	go serveComplexACK(ctx, env.PeerTr, env.Local, func(serviceChoice uint8) ([]byte, error) {
		return service.EncodeReadRangeACK(service.ReadRangeACK{
			Object: wrong, Property: prop,
			ResultFlags: service.EncodeResultFlags(true, true, false),
			ItemCount:   0, ItemData: nil,
		})
	})
	_, err := env.Client.ReadRange(ctx, env.Target, service.ReadRangeRequest{
		Object: reqObj, Property: prop, By: service.ReadRangeByPosition, Count: 1,
	})
	if err != bacnet.ErrProtocolViolation {
		t.Fatalf("got %v", err)
	}
}

func TestReadRangeEncodeError(t *testing.T) {
	env := newVirtualPair(t)
	_, err := env.Client.ReadRange(context.Background(), env.Target, service.ReadRangeRequest{
		Object: bacnet.ObjectIdentifier{Type: 20, Instance: 1},
		By:     service.ReadRangeByPosition, Count: 0,
	})
	if err == nil {
		t.Fatal("expected encode error")
	}
}

func TestReadRangeSimpleACKIsProtocolViolation(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go serveSimpleACK(ctx, env.PeerTr, env.Local)
	_, err := env.Client.ReadRange(ctx, env.Target, service.ReadRangeRequest{
		Object:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeTrendLog, Instance: 1},
		Property: bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer},
		By:       service.ReadRangeByPosition,
		Count:    1,
	})
	if err != bacnet.ErrProtocolViolation {
		t.Fatalf("got %v", err)
	}
}
