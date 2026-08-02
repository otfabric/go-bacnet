//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func deviceBaselineV4Path(t *testing.T) string {
	t.Helper()
	for _, p := range []string{
		filepath.Join("..", "bacnet-interop", "fixtures", "device", "device-baseline-v4.json"),
		filepath.Join("..", "..", "bacnet-interop", "fixtures", "device", "device-baseline-v4.json"),
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Fatal("device-baseline-v4.json not found; set BACNET_INTEROP_ROOT or open sibling checkout")
	return ""
}

func TestBACnet4JAtomicReadFileStream(t *testing.T) {
	// Peer serves FILE objects from device-baseline-v4 (verified in adapter logs),
	// but BACnet4J currently returns Error class=services code=10 on our
	// AtomicReadFile request encoding. Tracked as bacnet-interop BLOCKERS B8.
	t.Skip("blocker B8: BACnet4J AtomicReadFile Error services/10 vs go-bacnet request encoding")

	t.Setenv("BACNET_DEVICE_FIXTURE", deviceBaselineV4Path(t))

	peer := startPeer(t, bacnet4jImage(), "bacnet4j",
		"DEVICE_FIXTURE_FILE=/fixtures/device/device-baseline-v4.json",
		"FIXTURE=device-baseline-v4",
	)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	file := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 1}
	ack, err := c.AtomicReadFile(ctx, peer.target, service.AtomicReadFileRequest{
		File: file, Access: service.FileAccessStream, StartPosition: 0, Count: 64,
	})
	if err != nil {
		t.Fatalf("AtomicReadFile: %v", err)
	}
	want, err := base64.StdEncoding.DecodeString("aGVsbG8td29ybGQtc3RyZWFtLWZpbGUtZGF0YQ==")
	if err != nil {
		t.Fatal(err)
	}
	if string(ack.Data) != string(want) {
		t.Fatalf("data=%q want=%q eof=%v", ack.Data, want, ack.EndOfFile)
	}
}

func TestBACnet4JAtomicReadFileRecord(t *testing.T) {
	t.Skip("blocker B8: BACnet4J AtomicReadFile record CHOICE parse/timeout vs go-bacnet encoding")

	t.Setenv("BACNET_DEVICE_FIXTURE", deviceBaselineV4Path(t))

	peer := startPeer(t, bacnet4jImage(), "bacnet4j",
		"DEVICE_FIXTURE_FILE=/fixtures/device/device-baseline-v4.json",
		"FIXTURE=device-baseline-v4",
	)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	file := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 2}
	ack, err := c.AtomicReadFile(ctx, peer.target, service.AtomicReadFileRequest{
		File: file, Access: service.FileAccessRecord, StartPosition: 0, Count: 3,
	})
	if err != nil {
		t.Fatalf("AtomicReadFile record: %v", err)
	}
	if len(ack.Records) < 1 {
		t.Fatalf("expected records, got %+v", ack)
	}
}
