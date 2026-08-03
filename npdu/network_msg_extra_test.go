// SPDX-License-Identifier: MIT

package npdu_test

import (
	"testing"

	"github.com/otfabric/go-bacnet/npdu"
)

func TestDecodeICouldBeRouterToNetwork(t *testing.T) {
	netn, perf, err := npdu.DecodeICouldBeRouterToNetwork([]byte{0x00, 0x05, 0x03})
	if err != nil || netn != 5 || perf != 3 {
		t.Fatalf("net=%d perf=%d err=%v", netn, perf, err)
	}
	if _, _, err := npdu.DecodeICouldBeRouterToNetwork([]byte{0x00}); err == nil {
		t.Fatal("expected malformed")
	}
}

func TestDecodeRejectMessageToNetwork(t *testing.T) {
	reason, netn, err := npdu.DecodeRejectMessageToNetwork([]byte{0x01, 0x00, 0x02})
	if err != nil || reason != 1 || netn != 2 {
		t.Fatalf("%d %d %v", reason, netn, err)
	}
	if _, _, err := npdu.DecodeRejectMessageToNetwork([]byte{0x01}); err == nil {
		t.Fatal("expected truncated")
	}
	if _, _, err := npdu.DecodeRejectMessageToNetwork([]byte{0x01, 0x00, 0x02, 0x00}); err == nil {
		t.Fatal("expected trailing")
	}
}
