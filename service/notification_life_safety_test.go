// SPDX-License-Identifier: MIT

package service_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestChangeOfLifeSafetyRoundTrip(t *testing.T) {
	in := service.NotificationParameters{
		ChangeOfLifeSafety: &service.ChangeOfLifeSafetyParams{
			NewState: 2, NewMode: 1,
			StatusFlags:       bacnet.BitString{UnusedBits: 4, Bytes: []byte{0x50}},
			OperationExpected: 4,
		},
	}
	els, err := service.EncodeNotificationParameters(in)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeNotificationParameters(els)
	if err != nil {
		t.Fatal(err)
	}
	if got.ChangeOfLifeSafety == nil || got.ChangeOfLifeSafety.NewState != 2 || got.ChangeOfLifeSafety.OperationExpected != 4 {
		t.Fatalf("%#v", got.ChangeOfLifeSafety)
	}
}

func TestExtendedNotificationRoundTrip(t *testing.T) {
	in := service.NotificationParameters{
		Extended: &service.ExtendedParams{
			VendorID: 999, ExtendedEventType: 7,
			Parameters: []bacnet.Element{{Value: bacnet.UnsignedValue(1)}},
		},
	}
	els, err := service.EncodeNotificationParameters(in)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeNotificationParameters(els)
	if err != nil {
		t.Fatal(err)
	}
	if got.Extended == nil || got.Extended.VendorID != 999 || got.Extended.ExtendedEventType != 7 {
		t.Fatalf("%#v", got.Extended)
	}
}
