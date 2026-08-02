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

// Peers with upstream-native File + AtomicReadFile server support at current pins.
// BACpypes3 0.0.106 and Worldiety do not implement File servers — do not invent shims here.
func fileServicePeers() []struct {
	name  string
	image string
} {
	return []struct {
		name  string
		image string
	}{
		{"bacnet4j", bacnet4jImage()},
		{"bacnet-stack", getEnv("BACNET_STACK_IMAGE", defaultStackImage)},
	}
}

func runAtomicReadFileStream(t *testing.T, image, adapter string) {
	t.Helper()
	t.Setenv("BACNET_DEVICE_FIXTURE", deviceBaselineV4Path(t))
	peer := startPeer(t, image, adapter,
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

func runAtomicReadFileRecord(t *testing.T, image, adapter string) {
	t.Helper()
	t.Setenv("BACNET_DEVICE_FIXTURE", deviceBaselineV4Path(t))
	peer := startPeer(t, image, adapter,
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
	// bacnet-stack h_arf rejects RecordCount > ARRAY_SIZE(fileData) (often 1).
	// Request one record for multi-peer compatibility; BACnet4J also accepts it.
	ack, err := c.AtomicReadFile(ctx, peer.target, service.AtomicReadFileRequest{
		File: file, Access: service.FileAccessRecord, StartPosition: 0, Count: 1,
	})
	if err != nil {
		t.Fatalf("AtomicReadFile record: %v", err)
	}
	if len(ack.Records) < 1 {
		t.Fatalf("expected records, got %+v", ack)
	}
}

func TestAtomicReadFileStream(t *testing.T) {
	for _, p := range fileServicePeers() {
		t.Run(p.name, func(t *testing.T) {
			runAtomicReadFileStream(t, p.image, p.name)
		})
	}
}

func TestAtomicReadFileRecord(t *testing.T) {
	for _, p := range fileServicePeers() {
		t.Run(p.name, func(t *testing.T) {
			runAtomicReadFileRecord(t, p.image, p.name)
		})
	}
}

func runAtomicWriteFileStreamReadback(t *testing.T, image, adapter string) {
	t.Helper()
	t.Setenv("BACNET_DEVICE_FIXTURE", deviceBaselineV4Path(t))
	peer := startPeer(t, image, adapter,
		"DEVICE_FIXTURE_FILE=/fixtures/device/device-baseline-v4.json",
		"FIXTURE=device-baseline-v4",
	)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	file := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 1}
	payload := []byte("WRITEBACK")
	ack, err := c.AtomicWriteFile(ctx, peer.target, service.AtomicWriteFileRequest{
		File: file, Access: service.FileAccessStream, StartPosition: 0, Data: payload,
	})
	if err != nil {
		t.Fatalf("AtomicWriteFile: %v", err)
	}
	if ack.StartPosition != 0 {
		t.Fatalf("write ack start=%d", ack.StartPosition)
	}
	read, err := c.AtomicReadFile(ctx, peer.target, service.AtomicReadFileRequest{
		File: file, Access: service.FileAccessStream, StartPosition: 0, Count: uint32(len(payload)),
	})
	if err != nil {
		t.Fatalf("AtomicReadFile after write: %v", err)
	}
	if string(read.Data) != string(payload) {
		t.Fatalf("readback=%q want=%q", read.Data, payload)
	}
}

func TestAtomicWriteFileStreamReadback(t *testing.T) {
	for _, p := range fileServicePeers() {
		t.Run(p.name, func(t *testing.T) {
			runAtomicWriteFileStreamReadback(t, p.image, p.name)
		})
	}
}
