// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet/apdu"
)

func TestMergeCapabilitiesIgnoresUnknownSource(t *testing.T) {
	var dst DeviceCapabilities
	dst.SetIAmFields(480, 1, 42)
	src := DeviceCapabilities{}
	src.MaxAPDULengthAccepted = Capability[uint16]{Value: 999, Known: true, Source: CapabilityUnknown}
	mergeCapabilities(&dst, src)
	if dst.MaxAPDULengthAccepted.Value != 480 || dst.MaxAPDULengthAccepted.Source != CapabilityFromIAm {
		t.Fatalf("unknown source overwrote: %#v", dst.MaxAPDULengthAccepted)
	}
}

func TestCapabilityRankUserOverrideWins(t *testing.T) {
	if capabilityRank(CapabilityUserOverride) <= capabilityRank(CapabilityFromDeviceObject) {
		t.Fatal("user override should outrank device object")
	}
	if capabilityRank(CapabilityUnknown) != 0 {
		t.Fatal("unknown rank should be zero")
	}
}

func TestHandleConfirmedIndicationUnknownService(t *testing.T) {
	env := newVirtualPair(t)
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendConfirmedRequest(nil, apdu.ConfirmedRequest{
		InvokeID: 3, ServiceChoice: 99, MaxAPDU: 5, Payload: []byte{0x01},
	}))
	time.Sleep(10 * time.Millisecond)
}

func TestHandleUnconfirmedMalformedCOV(t *testing.T) {
	env := newVirtualPair(t)
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{
		ServiceChoice: apdu.ServiceUnconfirmedCOV,
		Payload:       []byte{0x09, 0x01},
	}))
	time.Sleep(10 * time.Millisecond)
}

func TestSendWhoIsPartialLimitsEncodeError(t *testing.T) {
	env := newVirtualPair(t)
	low := uint32(1)
	err := env.Client.SendWhoIs(context.Background(), env.Peer, false, DiscoveryOptions{LowLimit: &low})
	if err == nil {
		t.Fatal("expected encode error for partial Who-Is limits")
	}
}
