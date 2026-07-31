// SPDX-License-Identifier: MIT

package bvlc_test

import (
	"errors"
	"net/netip"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bvlc"
)

func TestBVLCResultAndRegisterFD(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	raw, err := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionResult, ResultCode: 0x0010})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := bvlc.Parse(raw, limits)
	if err != nil || msg.ResultCode != 0x0010 {
		t.Fatalf("%v %#v", err, msg)
	}

	raw, err = bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionRegisterForeignDevice, TTL: 60})
	if err != nil {
		t.Fatal(err)
	}
	msg, err = bvlc.Parse(raw, limits)
	if err != nil || msg.TTL != 60 {
		t.Fatalf("%v %#v", err, msg)
	}
}

func TestBVLCForwardedAndOrigin(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	raw, err := bvlc.Append(nil, bvlc.Message{
		Function:   bvlc.FunctionForwardedNPDU,
		OriginIP:   [4]byte{192, 0, 2, 10},
		OriginPort: 47808,
		Payload:    []byte{0x01, 0x00, 0x10, 0x08},
	})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := bvlc.Parse(raw, limits)
	if err != nil || msg.OriginPort != 47808 || msg.OriginIP != [4]byte{192, 0, 2, 10} {
		t.Fatalf("%v %#v", err, msg)
	}
	ap, ok := msg.OriginAddrPort()
	if !ok || ap != netip.MustParseAddrPort("192.0.2.10:47808") {
		t.Fatalf("origin %v ok=%v", ap, ok)
	}
	if _, ok := (bvlc.Message{Function: bvlc.FunctionOriginalUnicastNPDU}).OriginAddrPort(); ok {
		t.Fatal("origin should be false for non-forwarded")
	}
}

func TestBVLCBDTPassthroughAndErrors(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	raw, err := bvlc.Append(nil, bvlc.Message{
		Function: bvlc.FunctionReadBroadcastDistributionTableAck,
		Payload:  []byte{1, 2, 3, 4},
	})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := bvlc.Parse(raw, limits)
	if err != nil || string(msg.Payload) != "\x01\x02\x03\x04" {
		t.Fatalf("%v %#v", err, msg)
	}

	if _, err := bvlc.Parse([]byte{0x81}, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("trunc: %v", err)
	}
	if _, err := bvlc.Parse([]byte{0x82, 0x0A, 0x00, 0x04}, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("type: %v", err)
	}
	if _, err := bvlc.Parse([]byte{0x81, 0x0A, 0x00, 0x06, 0x00}, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("length mismatch: %v", err)
	}
}

func TestBVLCLimitsAndMalformedExtra(t *testing.T) {
	tiny := bacnet.DecodeLimits{MaxDatagramSize: 8}.Normalize()
	if _, err := bvlc.Parse([]byte{0x81, 0x0A, 0x00, 0x09, 0x01, 0x02, 0x03, 0x04, 0x05}, tiny); !errors.Is(err, bacnet.ErrLimitExceeded) {
		t.Fatalf("MaxDatagramSize: %v", err)
	}

	limits := bacnet.DefaultDecodeLimits()
	if _, err := bvlc.Parse([]byte{0x81, 0x00, 0x00, 0x04}, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("Result wrong length: %v", err)
	}
	if _, err := bvlc.Parse([]byte{0x81, 0x04, 0x00, 0x06, 0xC0, 0x00}, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("Forwarded truncated: %v", err)
	}
	if _, err := bvlc.Parse([]byte{0x81, 0x05, 0x00, 0x04}, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("Register-FD wrong length: %v", err)
	}
	msg, err := bvlc.Parse([]byte{0x81, 0x99, 0x00, 0x04}, limits)
	if err != nil || msg.Function != bvlc.Function(0x99) {
		t.Fatalf("unknown function: %v %#v", err, msg)
	}

	if _, err := bvlc.Append(nil, bvlc.Message{
		Function: bvlc.FunctionOriginalUnicastNPDU,
		Payload:  make([]byte, 0xFFFF),
	}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("Append oversized: %v", err)
	}
}
