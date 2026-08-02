// SPDX-License-Identifier: MIT

package service_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestDecodeGetAlarmSummaryACKEmptyAndTrailing(t *testing.T) {
	got, err := service.DecodeGetAlarmSummaryACK(nil, bacnet.DefaultDecodeLimits())
	if err != nil || len(got.Entries) != 0 {
		t.Fatalf("%v %+v", err, got)
	}
	raw, err := service.EncodeGetAlarmSummaryACK(service.GetAlarmSummaryACK{Entries: []service.AlarmSummaryEntry{{
		Object:           bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
		AlarmState:       1,
		AckedTransitions: bacnet.BitString{UnusedBits: 5, Bytes: []byte{0x00}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeGetAlarmSummaryACK(append(append([]byte{}, raw...), 0x00), bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected trailing")
	}
	// wrong middle type
	bad, err := bacnet.AppendApplicationValue(nil, bacnet.ObjectIDValue(bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}))
	if err != nil {
		t.Fatal(err)
	}
	bad, err = bacnet.AppendApplicationValue(bad, bacnet.RealValue(1))
	if err != nil {
		t.Fatal(err)
	}
	bad, err = bacnet.AppendApplicationValue(bad, bacnet.ApplicationValue{Kind: bacnet.ValueBitString, BitString: bacnet.BitString{UnusedBits: 5, Bytes: []byte{0}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeGetAlarmSummaryACK(bad, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected type error")
	}
}

func TestGetAlarmSummaryACKRoundTrip(t *testing.T) {
	ack := service.GetAlarmSummaryACK{Entries: []service.AlarmSummaryEntry{{
		Object:           bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
		AlarmState:       3,
		AckedTransitions: bacnet.BitString{UnusedBits: 5, Bytes: []byte{0xa0}},
	}}}
	raw, err := service.EncodeGetAlarmSummaryACK(ack)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeGetAlarmSummaryACK(raw, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 1 || got.Entries[0].Object.Instance != 1 || got.Entries[0].AlarmState != 3 {
		t.Fatalf("%+v", got)
	}
}

func TestGetAlarmSummaryACKEncodeDecodeErrors(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	badObj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: bacnet.MaxObjectInstance + 1}
	if _, err := service.EncodeGetAlarmSummaryACK(service.GetAlarmSummaryACK{Entries: []service.AlarmSummaryEntry{{
		Object: badObj, AlarmState: 1,
		AckedTransitions: bacnet.BitString{UnusedBits: 5, Bytes: []byte{0xe0}},
	}}}); err == nil {
		t.Fatal("expected encode error")
	}
	emptyACK, err := service.EncodeGetAlarmSummaryACK(service.GetAlarmSummaryACK{})
	if err != nil || emptyACK != nil {
		t.Fatalf("empty alarm ACK: %v %v", emptyACK, err)
	}
	one, err := bacnet.AppendApplicationValue(nil, bacnet.UnsignedValue(1))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeGetAlarmSummaryACK(one, limits); err == nil {
		t.Fatal("expected length error")
	}
	if _, err := service.DecodeGetAlarmSummaryACK([]byte{0xff}, limits); err == nil {
		t.Fatal("expected parse error")
	}
	badAlarmObj, err := bacnet.AppendApplicationValue(nil, bacnet.UnsignedValue(1))
	if err != nil {
		t.Fatal(err)
	}
	badAlarmObj, err = bacnet.AppendApplicationValue(badAlarmObj, bacnet.EnumValue(1))
	if err != nil {
		t.Fatal(err)
	}
	badAlarmObj, err = bacnet.AppendApplicationValue(badAlarmObj, bacnet.ApplicationValue{
		Kind: bacnet.ValueBitString, BitString: bacnet.BitString{UnusedBits: 5, Bytes: []byte{0}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeGetAlarmSummaryACK(badAlarmObj, limits); err == nil {
		t.Fatal("expected bad object")
	}
	badAlarmBits, err := bacnet.AppendApplicationValue(nil, bacnet.ObjectIDValue(bacnet.ObjectIdentifier{
		Type: bacnet.ObjectTypeAnalogInput, Instance: 1,
	}))
	if err != nil {
		t.Fatal(err)
	}
	badAlarmBits, err = bacnet.AppendApplicationValue(badAlarmBits, bacnet.EnumValue(1))
	if err != nil {
		t.Fatal(err)
	}
	badAlarmBits, err = bacnet.AppendApplicationValue(badAlarmBits, bacnet.UnsignedValue(1))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeGetAlarmSummaryACK(badAlarmBits, limits); err == nil {
		t.Fatal("expected bad bitstring")
	}
}
