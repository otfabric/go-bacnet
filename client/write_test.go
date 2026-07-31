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

func TestWritePropertySimpleACK(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go serveSimpleACK(ctx, env.PeerTr, env.Local)

	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogOutput, Instance: 1}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	err := env.Client.WriteProperty(ctx, env.Target, obj, prop, bacnet.RealValue(72.0), nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestWritePropertyErrorPDU(t *testing.T) {
	env := newVirtualPair(t, WithTransactionOptions(500*time.Millisecond, 0, 0))

	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogOutput, Instance: 1}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}

	errCh := make(chan error, 1)
	go func() {
		ctx := context.Background()
		errCh <- env.Client.WriteProperty(ctx, env.Target, obj, prop, bacnet.RealValue(1.0), nil)
	}()

	invokeID, _ := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	errPDU := apdu.AppendError(nil, apdu.ErrorPDU{
		InvokeID:      invokeID,
		ServiceChoice: apdu.ServiceWriteProperty,
		Payload:       []byte{0x91, 0x02, 0x91, 0x20}, // property / write-access-denied
	})
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), errPDU)

	err := <-errCh
	var er *bacnet.ErrorResponse
	if !errors.As(err, &er) {
		t.Fatalf("expected ErrorResponse, got %v", err)
	}
	if er.Class != 2 || er.Code != 32 {
		t.Fatalf("class/code %d/%d", er.Class, er.Code)
	}
}
