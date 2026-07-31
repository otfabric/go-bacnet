// SPDX-License-Identifier: MIT

package bvlc_test

import (
	"bytes"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bvlc"
)

func TestOriginalUnicastRoundTrip(t *testing.T) {
	payload := []byte{0x01, 0x00, 0x10, 0x08}
	enc, err := bvlc.Append(nil, bvlc.Message{
		Function: bvlc.FunctionOriginalUnicastNPDU,
		Payload:  payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := bvlc.Parse(enc, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if msg.Function != bvlc.FunctionOriginalUnicastNPDU {
		t.Fatalf("function %v", msg.Function)
	}
	if !bytes.Equal(msg.Payload, payload) {
		t.Fatalf("payload %x", msg.Payload)
	}
}

func TestForwardedNPDUOrigin(t *testing.T) {
	enc, err := bvlc.Append(nil, bvlc.Message{
		Function:   bvlc.FunctionForwardedNPDU,
		OriginIP:   [4]byte{192, 168, 1, 10},
		OriginPort: 47808,
		Payload:    []byte{0x01, 0x00},
	})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := bvlc.Parse(enc, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	ap, ok := msg.OriginAddrPort()
	if !ok || ap.String() != "192.168.1.10:47808" {
		t.Fatalf("origin %v ok=%v", ap, ok)
	}
}

func TestBVLCLengthMismatch(t *testing.T) {
	// Declared length 6 does not match 5-octet datagram.
	_, err := bvlc.Parse([]byte{0x81, 0x0A, 0x00, 0x06, 0x00}, bacnet.DefaultDecodeLimits())
	if err == nil {
		t.Fatal("expected length mismatch")
	}
}

func FuzzParseBVLC(f *testing.F) {
	enc, _ := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionOriginalBroadcastNPDU, Payload: []byte{1, 0}})
	f.Add(enc)
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = bvlc.Parse(data, bacnet.DefaultDecodeLimits())
	})
}
