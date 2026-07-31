// SPDX-License-Identifier: MIT

package bacnet

import (
	"testing"
)

func TestAddressStringDefaultScope(t *testing.T) {
	a := Address{scope: AddressScope(200)}
	if got := a.String(); got != "address(scope=200)" {
		t.Fatalf("default scope string: %q", got)
	}
}

func TestAddressStringLocalAndRemote(t *testing.T) {
	mac := MustMAC([]byte{10, 0, 0, 1, 0xBA, 0xC0})
	local := LocalStation(mac)
	if got := local.String(); got != "local:"+mac.String() {
		t.Fatalf("local station: got %q", got)
	}
	rb := RemoteBroadcast(42)
	if got := rb.String(); got != "net=42:broadcast" {
		t.Fatalf("remote broadcast: got %q", got)
	}
}
