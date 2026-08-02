// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestAdvancedServicesCanceledContext(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ot := bacnet.ObjectTypeAnalogValue
	calls := []struct {
		name string
		fn   func() error
	}{
		{"atomic-read", func() error {
			_, err := env.Client.AtomicReadFile(ctx, env.Target, service.AtomicReadFileRequest{
				File: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 1}, Access: service.FileAccessStream, Count: 1,
			})
			return err
		}},
		{"atomic-write", func() error {
			_, err := env.Client.AtomicWriteFile(ctx, env.Target, service.AtomicWriteFileRequest{
				File: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 1}, Access: service.FileAccessStream, Data: []byte{1},
			})
			return err
		}},
		{"create", func() error {
			_, err := env.Client.CreateObject(ctx, env.Target, service.CreateObjectRequest{ObjectType: &ot})
			return err
		}},
		{"delete", func() error {
			return env.Client.DeleteObject(ctx, env.Target, service.DeleteObjectRequest{
				Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
			})
		}},
		{"add-list", func() error {
			return env.Client.AddListElement(ctx, env.Target, service.ListElementRequest{
				Object:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
				Property: bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName},
				Elements: []bacnet.Element{{Value: bacnet.UnsignedValue(1)}},
			})
		}},
		{"private", func() error {
			return env.Client.ConfirmedPrivateTransfer(ctx, env.Target, service.PrivateTransfer{VendorID: 1, ServiceNumber: 1})
		}},
		{"text", func() error {
			return env.Client.ConfirmedTextMessage(ctx, env.Target, service.TextMessage{
				TextMessageSourceDevice: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}, Message: "x",
			})
		}},
		{"audit", func() error {
			_, err := env.Client.AuditLogQuery(ctx, env.Target, service.AuditLogQueryRequest{
				AuditLog: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAuditLog, Instance: 1},
			})
			return err
		}},
		{"auth", func() error {
			return env.Client.AuthRequest(ctx, env.Target, service.AuthRequest{})
		}},
		{"life", func() error {
			return env.Client.LifeSafetyOperation(ctx, env.Target, service.LifeSafetyOperationRequest{
				RequestingProcessIdentifier: 1, RequestingSource: "a", Request: 1,
			})
		}},
		{"vt-open", func() error {
			_, err := env.Client.VTOpen(ctx, env.Target, service.VTOpenRequest{VTClass: 1, LocalVTSessionIdentifier: 1})
			return err
		}},
		{"vt-close", func() error {
			return env.Client.VTClose(ctx, env.Target, service.VTCloseRequest{RemoteVTSessionIdentifiers: []uint8{1}})
		}},
		{"vt-data", func() error {
			return env.Client.VTData(ctx, env.Target, service.VTDataRequest{VTSessionIdentifier: 1, VTNewData: []byte{1}})
		}},
		{"whoami", func() error {
			return env.Client.WhoAmI(ctx, env.Target.Endpoint, true, service.WhoAmI{VendorID: 1})
		}},
		{"write-group", func() error {
			return env.Client.WriteGroup(ctx, env.Target.Endpoint, true, service.WriteGroup{
				GroupNumber: 1, WritePriority: 8, ChangeList: []bacnet.Element{{Value: bacnet.UnsignedValue(1)}},
			})
		}},
	}
	for _, tc := range calls {
		if err := tc.fn(); err == nil {
			t.Fatalf("%s: expected cancel error", tc.name)
		}
	}
}

func TestAdvancedEncodeValidationErrors(t *testing.T) {
	env := newVirtualPair(t)
	ctx := context.Background()
	if err := env.Client.AddListElement(ctx, env.Target, service.ListElementRequest{}); err == nil {
		t.Fatal("empty list")
	}
	if _, err := env.Client.CreateObject(ctx, env.Target, service.CreateObjectRequest{}); err == nil {
		t.Fatal("empty create")
	}
	if err := env.Client.VTClose(ctx, env.Target, service.VTCloseRequest{}); err == nil {
		t.Fatal("empty vt-close")
	}
	if err := env.Client.WriteGroup(ctx, env.Target.Endpoint, true, service.WriteGroup{GroupNumber: 1, WritePriority: 1}); err == nil {
		t.Fatal("empty write-group")
	}
}
