// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/service"
)

func TestInvokeConfirmedReadProperty(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogInput, Instance: 3}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	go serveComplexACK(ctx, env.PeerTr, env.Local, func(serviceChoice uint8) ([]byte, error) {
		return service.EncodeReadPropertyACK(service.ReadPropertyACK{
			Object: obj, Property: prop, Value: bacnet.RealValue(3.5),
		})
	})

	payload, err := service.EncodeReadProperty(service.ReadPropertyRequest{Object: obj, Property: prop})
	if err != nil {
		t.Fatal(err)
	}
	pdu, err := env.Client.InvokeConfirmed(context.Background(), env.Target, apdu.ServiceReadProperty, payload, ConfirmedInvokeOptions{})
	if err != nil || pdu.ComplexACK == nil {
		t.Fatalf("%v %#v", err, pdu)
	}
	ack, err := service.DecodeReadPropertyACK(pdu.ComplexACK.Payload, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	f, err := bacnet.AsReal(ack.Value)
	if err != nil || f != 3.5 {
		t.Fatalf("value %v err=%v", f, err)
	}
}
