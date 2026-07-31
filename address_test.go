// SPDX-License-Identifier: MIT

package bacnet_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
)

func TestAddressConstructorsAndEqual(t *testing.T) {
	mac := bacnet.MustMAC([]byte{10, 0, 0, 1, 0xBA, 0xC0})
	local := bacnet.LocalStation(mac)
	if local.Scope() != bacnet.AddressLocalStation || local.MAC() != mac || local.IsBroadcast() {
		t.Fatalf("local %#v", local)
	}
	lb := bacnet.LocalBroadcast()
	if !lb.IsBroadcast() || lb.String() != "local-broadcast" {
		t.Fatalf("local broadcast %#v %q", lb, lb.String())
	}
	remote := bacnet.RemoteStation(2, mac)
	if remote.Network() != 2 || remote.String() == "" || !remote.Equal(bacnet.RemoteStation(2, mac)) {
		t.Fatalf("remote %#v", remote)
	}
	rb := bacnet.RemoteBroadcast(3)
	if !rb.IsBroadcast() || rb.Network() != 3 {
		t.Fatalf("remote broadcast %#v", rb)
	}
	gb := bacnet.GlobalBroadcast()
	if !gb.IsBroadcast() || gb.String() != "global-broadcast" {
		t.Fatalf("global %#v %q", gb, gb.String())
	}
	if local.Equal(remote) {
		t.Fatal("local should not equal remote")
	}
}

func TestMACHelpers(t *testing.T) {
	var zero bacnet.MAC
	if !zero.IsZero() || zero.Len() != 0 || zero.String() != "" || zero.Bytes() != nil {
		t.Fatalf("zero mac %#v", zero)
	}
	m := bacnet.MustMAC([]byte{1, 2, 3})
	if m.Len() != 3 || m.IsZero() || m.String() != "010203" {
		t.Fatalf("%#v %q", m, m.String())
	}
	if _, err := bacnet.NewMAC(make([]byte, bacnet.MaxMACLength+1)); err == nil {
		t.Fatal("expected oversized MAC error")
	}
}
