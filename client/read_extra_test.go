// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/service"
)

func TestReadPropertyRejectsSimpleACK(t *testing.T) {
	env := newVirtualPair(t)
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogInput, Instance: 1}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}

	errCh := make(chan error, 1)
	go func() {
		_, err := env.Client.ReadProperty(context.Background(), env.Target, obj, prop)
		errCh <- err
	}()

	invokeID, _ := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendSimpleACK(nil, apdu.SimpleACK{
		InvokeID: invokeID, ServiceChoice: apdu.ServiceReadProperty,
	}))

	if !errors.Is(<-errCh, bacnet.ErrProtocolViolation) {
		t.Fatal("expected protocol violation for SimpleACK")
	}
}

func TestReadPropertyRejectsMismatchedACKObject(t *testing.T) {
	env := newVirtualPair(t)
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogInput, Instance: 1}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	wrongObj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogInput, Instance: 99}

	errCh := make(chan error, 1)
	go func() {
		_, err := env.Client.ReadProperty(context.Background(), env.Target, obj, prop)
		errCh <- err
	}()

	invokeID, _ := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	payload, _ := service.EncodeReadPropertyACK(service.ReadPropertyACK{
		Object: wrongObj, Property: prop, Value: bacnet.RealValue(1.0),
	})
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendComplexACK(nil, apdu.ComplexACK{
		InvokeID: invokeID, ServiceChoice: apdu.ServiceReadProperty, Payload: payload,
	}))

	if !errors.Is(<-errCh, bacnet.ErrProtocolViolation) {
		t.Fatal("expected protocol violation for object mismatch")
	}
}

func TestReadPropertyMultipleRejectsSimpleACK(t *testing.T) {
	env := newVirtualPair(t)
	specs := []service.ReadAccessSpecification{{
		Object:     bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		Properties: []bacnet.PropertyReference{{Identifier: bacnet.PropertyObjectName}},
	}}

	errCh := make(chan error, 1)
	go func() {
		_, err := env.Client.ReadPropertyMultiple(context.Background(), env.Target, specs)
		errCh <- err
	}()

	invokeID, _ := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendSimpleACK(nil, apdu.SimpleACK{
		InvokeID: invokeID, ServiceChoice: apdu.ServiceReadPropertyMultiple,
	}))

	if !errors.Is(<-errCh, bacnet.ErrProtocolViolation) {
		t.Fatal("expected protocol violation")
	}
}

func TestReadPropertyCancelDuringWait(t *testing.T) {
	env := newVirtualPair(t, WithTransactionOptions(time.Second, 0, 0))
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogInput, Instance: 2}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := env.Client.ReadProperty(ctx, env.Target, obj, prop)
		errCh <- err
	}()
	_, _ = waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	cancel()

	err := <-errCh
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}
