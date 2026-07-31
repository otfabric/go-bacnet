// SPDX-License-Identifier: MIT

package bacnet_test

import (
	"fmt"

	"github.com/otfabric/go-bacnet"
)

func ExampleAddress() {
	mac := bacnet.MustMAC([]byte{192, 168, 1, 10, 0xBA, 0xC0})
	local := bacnet.LocalStation(mac)
	remote := bacnet.RemoteStation(2, mac)
	fmt.Println(local.Scope() == bacnet.AddressLocalStation)
	fmt.Println(remote.Network())
	fmt.Println(bacnet.GlobalBroadcast().IsBroadcast())
	// Output:
	// true
	// 2
	// true
}

func ExampleApplicationValue_helpers() {
	v := bacnet.RealValue(21.5)
	f, err := bacnet.AsReal(v)
	if err != nil {
		fmt.Println("err", err)
		return
	}
	fmt.Println(v.Kind == bacnet.ValueReal)
	fmt.Printf("%.1f\n", f)

	id := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogInput, Instance: 1}
	oid := bacnet.ObjectIDValue(id)
	got, err := bacnet.AsObjectID(oid)
	if err != nil {
		fmt.Println("err", err)
		return
	}
	fmt.Println(got.String())
	// Output:
	// true
	// 21.5
	// 0:1
}
