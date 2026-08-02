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

func injectComplexACK(t *testing.T, env *virtualPair, invokeID, choice uint8, payload []byte) {
	t.Helper()
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendComplexACK(nil, apdu.ComplexACK{
		InvokeID: invokeID, ServiceChoice: choice, Payload: payload,
	}))
}

func TestAdvancedComplexACKDecodeErrors(t *testing.T) {
	cases := []struct {
		name    string
		choice  uint8
		payload []byte
		start   func(*virtualPair, context.Context) error
	}{
		{"atomic-read", apdu.ServiceAtomicReadFile, []byte{0x21, 0x01}, func(env *virtualPair, ctx context.Context) error {
			_, err := env.Client.AtomicReadFile(ctx, env.Target, service.AtomicReadFileRequest{
				File: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 1}, Access: service.FileAccessStream, Count: 1,
			})
			return err
		}},
		{"atomic-write", apdu.ServiceAtomicWriteFile, []byte{0x21, 0x01}, func(env *virtualPair, ctx context.Context) error {
			_, err := env.Client.AtomicWriteFile(ctx, env.Target, service.AtomicWriteFileRequest{
				File: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 1}, Access: service.FileAccessStream, Data: []byte{1},
			})
			return err
		}},
		{"create-object", apdu.ServiceCreateObject, []byte{0x21, 0x01}, func(env *virtualPair, ctx context.Context) error {
			ot := bacnet.ObjectTypeAnalogValue
			_, err := env.Client.CreateObject(ctx, env.Target, service.CreateObjectRequest{ObjectType: &ot})
			return err
		}},
		{"audit-query", apdu.ServiceAuditLogQuery, []byte{0xff}, func(env *virtualPair, ctx context.Context) error {
			_, err := env.Client.AuditLogQuery(ctx, env.Target, service.AuditLogQueryRequest{
				AuditLog: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAuditLog, Instance: 1},
			})
			return err
		}},
		{"vt-open", apdu.ServiceVTOpen, []byte{0x21, 0x01, 0x21, 0x02}, func(env *virtualPair, ctx context.Context) error {
			_, err := env.Client.VTOpen(ctx, env.Target, service.VTOpenRequest{VTClass: 1, LocalVTSessionIdentifier: 1})
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newVirtualPair(t)
			errCh := make(chan error, 1)
			go func() { errCh <- tc.start(env, context.Background()) }()
			invokeID, _ := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
			injectComplexACK(t, env, invokeID, tc.choice, tc.payload)
			if err := <-errCh; err == nil {
				t.Fatal("expected decode/protocol error")
			}
		})
	}
}

func TestAdvancedSimpleACKProtocolViolations(t *testing.T) {
	cases := []struct {
		name   string
		choice uint8
		start  func(*virtualPair, context.Context) error
	}{
		{"delete", apdu.ServiceDeleteObject, func(env *virtualPair, ctx context.Context) error {
			return env.Client.DeleteObject(ctx, env.Target, service.DeleteObjectRequest{
				Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
			})
		}},
		{"add-list", apdu.ServiceAddListElement, func(env *virtualPair, ctx context.Context) error {
			return env.Client.AddListElement(ctx, env.Target, service.ListElementRequest{
				Object:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
				Property: bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName},
				Elements: []bacnet.Element{{Value: bacnet.UnsignedValue(1)}},
			})
		}},
		{"life-safety", apdu.ServiceLifeSafetyOperation, func(env *virtualPair, ctx context.Context) error {
			return env.Client.LifeSafetyOperation(ctx, env.Target, service.LifeSafetyOperationRequest{
				RequestingProcessIdentifier: 1, RequestingSource: "a", Request: 1,
			})
		}},
		{"text", apdu.ServiceConfirmedTextMessage, func(env *virtualPair, ctx context.Context) error {
			return env.Client.ConfirmedTextMessage(ctx, env.Target, service.TextMessage{
				TextMessageSourceDevice: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
				Message:                 "x",
			})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newVirtualPair(t)
			errCh := make(chan error, 1)
			go func() { errCh <- tc.start(env, context.Background()) }()
			invokeID, _ := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
			injectComplexACK(t, env, invokeID, tc.choice, nil)
			if err := <-errCh; err != bacnet.ErrProtocolViolation {
				t.Fatalf("got %v", err)
			}
		})
	}
}
