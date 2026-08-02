// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestAdvancedClientEncodeErrors(t *testing.T) {
	env := newVirtualPair(t)
	ctx := context.Background()
	badID := bacnet.ObjectIdentifier{Type: 0xFFFF, Instance: 1}
	if _, err := env.Client.AtomicReadFile(ctx, env.Target, service.AtomicReadFileRequest{
		File: badID, Access: service.FileAccessStream, Count: 1,
	}); err == nil {
		t.Fatal("atomic read")
	}
	if _, err := env.Client.AtomicWriteFile(ctx, env.Target, service.AtomicWriteFileRequest{
		File: badID, Access: service.FileAccessStream, Data: []byte{1},
	}); err == nil {
		t.Fatal("atomic write")
	}
	if _, err := env.Client.CreateObject(ctx, env.Target, service.CreateObjectRequest{ObjectIdentifier: &badID}); err == nil {
		t.Fatal("create")
	}
	if err := env.Client.DeleteObject(ctx, env.Target, service.DeleteObjectRequest{Object: badID}); err == nil {
		t.Fatal("delete")
	}
	if err := env.Client.LifeSafetyOperation(ctx, env.Target, service.LifeSafetyOperationRequest{
		RequestingProcessIdentifier: 1, RequestingSource: "a", Request: 1, Object: &badID,
	}); err == nil {
		t.Fatal("life")
	}
	if _, err := env.Client.AuditLogQuery(ctx, env.Target, service.AuditLogQueryRequest{AuditLog: badID}); err == nil {
		t.Fatal("audit")
	}
	if err := env.Client.YouAre(ctx, env.Target.Endpoint, true, service.YouAre{Device: badID}); err == nil {
		t.Fatal("you-are")
	}
}
