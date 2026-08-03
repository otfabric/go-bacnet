// SPDX-License-Identifier: MIT

package service_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestComplexEventTypeRoundTrip(t *testing.T) {
	in := service.NotificationParameters{
		ComplexEventType: &service.ComplexEventTypeParams{
			Values: []bacnet.Element{{Value: bacnet.UnsignedValue(9)}},
		},
	}
	els, err := service.EncodeNotificationParameters(in)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeNotificationParameters(els)
	if err != nil || got.ComplexEventType == nil || len(got.ComplexEventType.Values) != 1 {
		t.Fatalf("%#v err=%v", got, err)
	}
}
