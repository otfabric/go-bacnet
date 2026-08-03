// SPDX-License-Identifier: MIT

package service_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestEncodePropertyStatesVariants(t *testing.T) {
	flags := bacnet.BitString{UnusedBits: 4, Bytes: []byte{0x00}}
	for _, ps := range []service.PropertyStates{
		{Choice: 1, Value: bacnet.BoolValue(true)},
		{Choice: 2, Value: bacnet.UnsignedValue(3)},
		{Choice: 3, Value: bacnet.EnumValue(4)},
		{Choice: 4, Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{{Value: bacnet.RealValue(1)}}}},
	} {
		in := service.NotificationParameters{ChangeOfState: &service.ChangeOfStateParams{NewState: ps, StatusFlags: flags}}
		els, err := service.EncodeNotificationParameters(in)
		if err != nil {
			t.Fatalf("%#v: %v", ps, err)
		}
		if _, err := service.DecodeNotificationParameters(els); err != nil {
			t.Fatalf("decode %#v: %v", ps, err)
		}
	}
}
