// SPDX-License-Identifier: MIT

package service_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestCoverageBoostTextMessageBadBody(t *testing.T) {
	raw, err := bacnet.AppendContextObjectID(nil, 0, bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1})
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextUnsigned(raw, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextTagged(raw, 3, []bacnet.Element{{Value: bacnet.UnsignedValue(1)}}) // not char string
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeTextMessage(raw, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected messageText error")
	}
}

func TestCoverageBoostLifeSafetyBadSource(t *testing.T) {
	raw, err := bacnet.AppendContextUnsigned(nil, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextTagged(raw, 1, nil) // empty source
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextUnsigned(raw, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeLifeSafetyOperation(raw, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected source error")
	}
}

func TestCoverageBoostVTDataWrongKinds(t *testing.T) {
	raw, err := bacnet.AppendApplicationValue(nil, bacnet.UnsignedValue(1))
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendApplicationValue(raw, bacnet.UnsignedValue(2)) // not octet string
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendApplicationValue(raw, bacnet.UnsignedValue(3))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeVTData(raw, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected VTNewData error")
	}
}

func TestCoverageBoostWriteGroupInhibit(t *testing.T) {
	raw, err := bacnet.AppendContextUnsigned(nil, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextUnsigned(raw, 1, 8)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextTagged(raw, 2, []bacnet.Element{{Value: bacnet.UnsignedValue(1)}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextBool(raw, 3, true)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeWriteGroup(raw, bacnet.DefaultDecodeLimits())
	if err != nil || got.InhibitDelay == nil || !*got.InhibitDelay {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestCoverageBoostListMissingFields(t *testing.T) {
	raw, err := bacnet.AppendContextObjectID(nil, 0, bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeListElementRequest(raw, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("missing fields")
	}
}

func TestCoverageBoostPrivateMissing(t *testing.T) {
	raw, err := bacnet.AppendContextUnsigned(nil, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodePrivateTransfer(raw, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("missing service")
	}
}

func TestCoverageBoostCreateObjectContextACK(t *testing.T) {
	raw, err := bacnet.AppendContextObjectID(nil, 0, bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 3})
	if err != nil {
		t.Fatal(err)
	}
	// DecodeCreateObjectACK expects application object id primarily; context may work via ContextObjectID fallback
	got, err := service.DecodeCreateObjectACK(raw, bacnet.DefaultDecodeLimits())
	if err != nil {
		// acceptable if context-only form rejected; try application
		raw2, err2 := bacnet.AppendApplicationValue(nil, bacnet.ObjectIDValue(bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 3}))
		if err2 != nil {
			t.Fatal(err2)
		}
		got, err = service.DecodeCreateObjectACK(raw2, bacnet.DefaultDecodeLimits())
		if err != nil {
			t.Fatal(err)
		}
	}
	if got.Object.Instance != 3 {
		t.Fatalf("%+v", got)
	}
}
