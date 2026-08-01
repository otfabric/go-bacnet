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

func TestWritePropertyMultipleSimpleACK(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go serveSimpleACK(ctx, env.PeerTr, env.Local)

	specs := []service.WriteAccessSpecification{{
		Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogOutput, Instance: 1},
		Properties: []service.WritePropertyValue{{
			Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
			Value:    bacnet.RealValue(22.0),
		}},
	}}
	if err := env.Client.WritePropertyMultiple(ctx, env.Target, specs); err != nil {
		t.Fatal(err)
	}
}

func TestWritePropertyMultipleErrorPDU(t *testing.T) {
	env := newVirtualPair(t, WithTransactionOptions(500*time.Millisecond, 0, 0))

	specs := []service.WriteAccessSpecification{{
		Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogOutput, Instance: 1},
		Properties: []service.WritePropertyValue{{
			Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
			Value:    bacnet.RealValue(1.0),
		}},
	}}

	errCh := make(chan error, 1)
	go func() {
		errCh <- env.Client.WritePropertyMultiple(context.Background(), env.Target, specs)
	}()

	invokeID, _ := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	payload, err := service.EncodeWritePropertyMultipleError(service.WritePropertyMultipleError{
		Class:         2,
		Code:          32,
		FirstFailed:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogOutput, Instance: 1},
		FirstProperty: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
	})
	if err != nil {
		t.Fatal(err)
	}
	errPDU := apdu.AppendError(nil, apdu.ErrorPDU{
		InvokeID:      invokeID,
		ServiceChoice: apdu.ServiceWritePropertyMultiple,
		Payload:       payload,
	})
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), errPDU)

	got := <-errCh
	var wpm *service.WritePropertyMultipleError
	if !errors.As(got, &wpm) {
		t.Fatalf("expected WritePropertyMultipleError, got %v", got)
	}
	if wpm.Class != 2 || wpm.Code != 32 {
		t.Fatalf("class/code %d/%d", wpm.Class, wpm.Code)
	}
}

func TestDefaultRetransmitPolicyWritePropertyMultiple(t *testing.T) {
	if DefaultRetransmitPolicy(apdu.ServiceWritePropertyMultiple) != RetransmitDisabled {
		t.Fatal("WPM should disable exact-APDU retransmit")
	}
}
