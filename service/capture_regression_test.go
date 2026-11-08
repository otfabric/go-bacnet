// SPDX-License-Identifier: MIT

package service_test

import (
	"bytes"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

// An extended [9] event notification with an empty parameters list
// (kargs/unknown_unknown_cen-9-11.pcap frame 3): parameters [2] is not
// OPTIONAL, so it is encoded again even when empty. Before v0.7.0 the
// encoder dropped it.
func TestExtendedNotificationEmptyParametersRecorded(t *testing.T) {
	recorded := fromHex("096f1c0240000b2c000000013e19023f49015901690" + "97d0b004d79204d65737361676589019900a900b902ce9e091819212e2f9fcf")
	note, err := service.DecodeEventNotification(recorded, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if note.Parameters == nil || note.Parameters.Extended == nil || note.Parameters.Extended.VendorID != 24 {
		t.Fatalf("%+v", note.Parameters)
	}
	again, err := service.EncodeEventNotification(note)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(again, recorded) {
		t.Fatalf("re-encoded % x\nrecorded    % x", again, recorded)
	}
}
