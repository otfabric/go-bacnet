//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"errors"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

// AtomicReadFile and AtomicWriteFile with the access method the File object
// does not have (stream access on a record file and the reverse) are refused
// with Error services/invalid-file-access-method, and the files stay as they
// were.
//
// BACnet4J 6.1.x does that. bacnet-stack does it from 1.6.1 on. bacnet-stack
// 1.6.0 executed such a request: a stream read of the record file returned
// octets of its first record, a record read of the stream file returned one
// empty record, and the writes overwrote the files. The test reads the
// Device's Firmware_Revision to tell the two releases apart and asserts the
// 1.6.0 reads as they were (it does not send the writes to 1.6.0).
func TestFileAccessMethodMismatch(t *testing.T) {
	forPeersWithFixture(t, []interopPeer{stackPeer(), bacnet4jPeer()}, deviceBaselineV4Path, "device-baseline-v4",
		func(t *testing.T, peer *peerHandle) {
			c := newClient(t)
			ctx, cancel := withTimeout(t, 20*time.Second)
			defer cancel()
			streamFile := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 1}
			recordFile := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 2}
			const streamContent = "hello-world-stream-file-data"

			requireMismatch := func(what string, err error) {
				t.Helper()
				var er *bacnet.ErrorResponse
				if !errors.As(err, &er) || er.Class != bacnet.ErrorClassServices || er.Code != bacnet.ErrorCodeInvalidFileAccessMethod {
					t.Fatalf("%s: got %v, want Error services/invalid-file-access-method", what, err)
				}
			}
			streamOfRecord := service.AtomicReadFileRequest{File: recordFile, Access: service.FileAccessStream, Count: 8}
			recordOfStream := service.AtomicReadFileRequest{File: streamFile, Access: service.FileAccessRecord, Count: 1}

			if firmware := bacnetStackFirmware(t, peer); firmware == "1.6.0" {
				ack, err := c.AtomicReadFile(ctx, peer.target, streamOfRecord)
				if err != nil || string(ack.Data) != "record-0" {
					t.Fatalf("bacnet-stack 1.6.0, stream read of the record file: %q, %v; it used to return \"record-0\"", ack.Data, err)
				}
				ack, err = c.AtomicReadFile(ctx, peer.target, recordOfStream)
				if err != nil || ack.RecordCount != 1 || len(ack.Records) != 1 || len(ack.Records[0]) != 0 {
					t.Fatalf("bacnet-stack 1.6.0, record read of the stream file: %+v, %v; it used to return one empty record", ack, err)
				}
				t.Log("bacnet-stack 1.6.0 serves a read with the wrong access method; 1.6.1 refuses it")
				return
			}

			_, err := c.AtomicReadFile(ctx, peer.target, streamOfRecord)
			requireMismatch("stream read of the record file", err)
			_, err = c.AtomicReadFile(ctx, peer.target, recordOfStream)
			requireMismatch("record read of the stream file", err)
			_, err = c.AtomicWriteFile(ctx, peer.target, service.AtomicWriteFileRequest{
				File: recordFile, Access: service.FileAccessStream, Data: []byte("zz")})
			requireMismatch("stream write to the record file", err)
			_, err = c.AtomicWriteFile(ctx, peer.target, service.AtomicWriteFileRequest{
				File: streamFile, Access: service.FileAccessRecord, Records: [][]byte{[]byte("zz")}})
			requireMismatch("record write to the stream file", err)

			ack, err := c.AtomicReadFile(ctx, peer.target, service.AtomicReadFileRequest{
				File: streamFile, Access: service.FileAccessStream, Count: 64})
			if err != nil || string(ack.Data) != streamContent {
				t.Fatalf("stream file after the refused writes: %q, %v; want %q", ack.Data, err, streamContent)
			}
		})
}

// bacnetStackFirmware returns the Device Firmware_Revision of a bacnet-stack
// peer ("1.6.0", "1.6.1": upstream's BACNET_VERSION_TEXT) and "" for another
// peer.
func bacnetStackFirmware(t *testing.T, peer *peerHandle) string {
	t.Helper()
	c := newClient(t)
	ctx, cancel := withTimeout(t, 10*time.Second)
	defer cancel()
	const propertyFirmwareRevision bacnet.PropertyIdentifier = 44
	const propertyModelName bacnet.PropertyIdentifier = 70
	read := func(prop bacnet.PropertyIdentifier) string {
		t.Helper()
		v, err := c.ReadProperty(ctx, peer.target, bacnet.AnyDevice(), bacnet.PropertyReference{Identifier: prop})
		if err != nil {
			t.Fatalf("ReadProperty device property %d: %v", prop, err)
		}
		return characterString(t, v)
	}
	firmware := read(propertyFirmwareRevision)
	if len(firmware) < 4 || firmware[:4] != "1.6." {
		return ""
	}
	t.Logf("bacnet-stack firmware revision %s (model %q)", firmware, read(propertyModelName))
	return firmware
}
