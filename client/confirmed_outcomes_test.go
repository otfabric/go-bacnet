// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/service"
)

func TestReadPropertyErrorRejectAbort(t *testing.T) {
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogInput, Instance: 1}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}

	tests := []struct {
		name    string
		build   func(invokeID uint8) []byte
		wantErr func(error) bool
	}{
		{
			name: "Error",
			build: func(id uint8) []byte {
				return apdu.AppendError(nil, apdu.ErrorPDU{
					InvokeID: id, ServiceChoice: apdu.ServiceReadProperty,
					Payload: []byte{0x91, 0x02, 0x91, 0x20},
				})
			},
			wantErr: func(err error) bool {
				var er *bacnet.ErrorResponse
				return errors.As(err, &er) && er.Class == 2 && er.Code == 32
			},
		},
		{
			name: "Reject",
			build: func(id uint8) []byte {
				return apdu.AppendReject(nil, apdu.RejectPDU{InvokeID: id, Reason: 1})
			},
			wantErr: func(err error) bool {
				var rj *bacnet.RejectError
				return errors.As(err, &rj) && rj.Reason == 1
			},
		},
		{
			name: "Abort",
			build: func(id uint8) []byte {
				return apdu.AppendAbort(nil, apdu.AbortPDU{Server: true, InvokeID: id, Reason: 2})
			},
			wantErr: func(err error) bool {
				var ab *bacnet.AbortError
				return errors.As(err, &ab) && ab.Server && ab.Reason == 2
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := newVirtualPair(t)
			errCh := make(chan error, 1)
			go func() {
				ctx := context.Background()
				_, err := env.Client.ReadProperty(ctx, env.Target, obj, prop)
				errCh <- err
			}()

			invokeID, _ := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
			injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), tc.build(invokeID))

			err := <-errCh
			if !tc.wantErr(err) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestReadPropertyWrongSourceIgnoredThenTimeout(t *testing.T) {
	env := newVirtualPair(t, WithTransactionOptions(500*time.Millisecond, 0, 0))
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogInput, Instance: 1}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}

	errCh := make(chan error, 1)
	go func() {
		ctx := context.Background()
		_, err := env.Client.ReadProperty(ctx, env.Target, obj, prop)
		errCh <- err
	}()

	invokeID, _ := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	wrongPeer := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.99:47808"))
	ackPayload, _ := service.EncodeReadPropertyACK(service.ReadPropertyACK{
		Object:   obj,
		Property: prop,
		Value:    bacnet.RealValue(1.0),
	})
	complexACK := apdu.AppendComplexACK(nil, apdu.ComplexACK{
		InvokeID: invokeID, ServiceChoice: apdu.ServiceReadProperty, Payload: ackPayload,
	})
	injectUnicastNPDU(t, env.ClientTr, wrongPeer, env.Clk.Now(), complexACK)

	env.Clk.Advance(600 * time.Millisecond)
	err := <-errCh
	if !errors.Is(err, bacnet.ErrTimeout) {
		t.Fatalf("expected timeout, got %v", err)
	}
}
