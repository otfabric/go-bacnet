// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
)

func TestSubscribeCOVRejectsNonSimpleACK(t *testing.T) {
	env := newVirtualPair(t)
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 10}

	errCh := make(chan error, 1)
	go func() {
		_, err := env.Client.SubscribeCOV(context.Background(), env.Target, obj, COVOptions{Lifetime: 60})
		errCh <- err
	}()

	invokeID, _ := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendComplexACK(nil, apdu.ComplexACK{
		InvokeID: invokeID, ServiceChoice: apdu.ServiceSubscribeCOV, Payload: []byte{0x01},
	}))

	if !errors.Is(<-errCh, bacnet.ErrProtocolViolation) {
		t.Fatal("expected protocol violation")
	}
}

func TestWritePropertyClosedClient(t *testing.T) {
	env := newVirtualPair(t)
	if err := env.Client.Close(); err != nil {
		t.Fatal(err)
	}
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogOutput, Instance: 1}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	err := env.Client.WriteProperty(context.Background(), env.Target, obj, prop, bacnet.RealValue(1.0), nil)
	if !errors.Is(err, bacnet.ErrClosed) {
		t.Fatalf("got %v", err)
	}
}
