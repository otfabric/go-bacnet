// SPDX-License-Identifier: MIT

package service_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestPrivateTransferRoundTripAndErrors(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	raw, err := service.EncodePrivateTransfer(service.PrivateTransfer{VendorID: 1, ServiceNumber: 2})
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodePrivateTransfer(raw, limits)
	if err != nil || len(got.ServiceParameters) != 0 {
		t.Fatalf("%v %+v", err, got)
	}

	bad, err := bacnet.AppendContextUnsigned(nil, 0, 0x10000)
	if err != nil {
		t.Fatal(err)
	}
	bad, err = bacnet.AppendContextUnsigned(bad, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodePrivateTransfer(bad, limits); err == nil {
		t.Fatal("expected vendor overflow")
	}
	partial, err := bacnet.AppendContextUnsigned(nil, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodePrivateTransfer(partial, limits); err == nil {
		t.Fatal("expected missing fields")
	}
	ptBad, err := bacnet.AppendContextUnsigned(nil, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	ptBad, err = bacnet.AppendContextUnsigned(ptBad, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	ptBad, err = bacnet.AppendContextUnsigned(ptBad, 9, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodePrivateTransfer(ptBad, limits); err == nil {
		t.Fatal("expected bad tag")
	}
	if _, err := service.DecodePrivateTransfer([]byte{0xff}, limits); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestTextMessageRoundTrip(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	cls := uint32(7)
	msg := service.TextMessage{
		TextMessageSourceDevice: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		MessageClass:            &cls,
		MessagePriority:         0,
		Message:                 "interop-hello",
	}
	raw, err := service.EncodeTextMessage(msg)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeTextMessage(raw, limits)
	if err != nil {
		t.Fatal(err)
	}
	if got.MessageClass == nil || *got.MessageClass != cls || got.Message != msg.Message {
		t.Fatalf("got %+v", got)
	}

	msg.MessageClass = nil
	raw2, err := service.EncodeTextMessage(msg)
	if err != nil {
		t.Fatal(err)
	}
	got2, err := service.DecodeTextMessage(raw2, limits)
	if err != nil || got2.MessageClass != nil || got2.Message != msg.Message {
		t.Fatalf("got %+v err=%v", got2, err)
	}

	urgent := service.TextMessage{
		TextMessageSourceDevice: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		MessageClass:            &cls,
		MessagePriority:         1,
		Message:                 "classy",
	}
	uraw, err := service.EncodeTextMessage(urgent)
	if err != nil {
		t.Fatal(err)
	}
	ugot, err := service.DecodeTextMessage(uraw, limits)
	if err != nil || ugot.Message != "classy" || ugot.MessageClass == nil || *ugot.MessageClass != 7 {
		t.Fatalf("%v %+v", err, ugot)
	}
}

func TestTextMessageLegacyDecode(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	legacy, err := bacnet.AppendContextObjectID(nil, 0, bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1})
	if err != nil {
		t.Fatal(err)
	}
	legacy, err = bacnet.AppendContextUnsigned(legacy, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err = bacnet.AppendContextTagged(legacy, 3, []bacnet.Element{{
		Value: bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: "legacy"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	lg, err := service.DecodeTextMessage(legacy, limits)
	if err != nil || lg.Message != "legacy" {
		t.Fatalf("legacy constructed: %v %+v", err, lg)
	}

	flat, err := bacnet.AppendContextObjectID(nil, 0, bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 2})
	if err != nil {
		t.Fatal(err)
	}
	flat, err = bacnet.AppendContextUnsigned(flat, 1, 9)
	if err != nil {
		t.Fatal(err)
	}
	flat, err = bacnet.AppendContextUnsigned(flat, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	flat, err = bacnet.AppendContextCharacterString(flat, 3, bacnet.CharacterString{Value: "flat"})
	if err != nil {
		t.Fatal(err)
	}
	fg, err := service.DecodeTextMessage(flat, limits)
	if err != nil || fg.Message != "flat" || fg.MessageClass == nil || *fg.MessageClass != 9 {
		t.Fatalf("legacy flat class: %v %+v", err, fg)
	}
}

func TestTextMessageDecodeErrors(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	if _, err := service.DecodeTextMessage([]byte{0xff}, limits); err == nil {
		t.Fatal("expected malformed")
	}
	tm, err := bacnet.AppendContextObjectID(nil, 0, bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeTextMessage(tm, limits); err == nil {
		t.Fatal("expected missing fields")
	}
	tmBad, err := bacnet.AppendContextObjectID(nil, 0, bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1})
	if err != nil {
		t.Fatal(err)
	}
	tmBad, err = bacnet.AppendContextUnsigned(tmBad, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	tmBad, err = bacnet.AppendContextCharacterString(tmBad, 3, bacnet.CharacterString{Value: "x"})
	if err != nil {
		t.Fatal(err)
	}
	tmBad, err = bacnet.AppendContextUnsigned(tmBad, 7, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeTextMessage(tmBad, limits); err == nil {
		t.Fatal("expected bad tag")
	}
	tmLegacyBad, err := bacnet.AppendContextObjectID(nil, 0, bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1})
	if err != nil {
		t.Fatal(err)
	}
	tmLegacyBad, err = bacnet.AppendContextUnsigned(tmLegacyBad, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	tmLegacyBad, err = bacnet.AppendContextTagged(tmLegacyBad, 3, []bacnet.Element{
		{Value: bacnet.UnsignedValue(1)},
		{Value: bacnet.UnsignedValue(2)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeTextMessage(tmLegacyBad, limits); err == nil {
		t.Fatal("expected bad legacy messageText")
	}
	badBody, err := bacnet.AppendContextObjectID(nil, 0, bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1})
	if err != nil {
		t.Fatal(err)
	}
	badBody, err = bacnet.AppendContextUnsigned(badBody, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	badBody, err = bacnet.AppendContextTagged(badBody, 3, []bacnet.Element{{Value: bacnet.UnsignedValue(1)}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeTextMessage(badBody, limits); err == nil {
		t.Fatal("expected messageText kind error")
	}
}

func TestTextMessageEncodeErrors(t *testing.T) {
	badInst := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: bacnet.MaxObjectInstance + 1}
	if _, err := service.EncodeTextMessage(service.TextMessage{
		TextMessageSourceDevice: badInst, Message: "x",
	}); err == nil {
		t.Fatal("expected bad source")
	}
}

func TestWriteGroupAndTimeSynchronizationEncode(t *testing.T) {
	raw, err := service.EncodeWriteGroup(service.WriteGroup{
		GroupNumber:   1,
		WritePriority: 8,
		ChangeList:    []bacnet.Element{{Value: bacnet.RealValue(1.0)}},
	})
	if err != nil || len(raw) == 0 {
		t.Fatalf("%v %x", err, raw)
	}
	inhibit := true
	wraw, err := service.EncodeWriteGroup(service.WriteGroup{
		GroupNumber:   2,
		WritePriority: 8,
		ChangeList:    []bacnet.Element{{Value: bacnet.UnsignedValue(1)}},
		InhibitDelay:  &inhibit,
	})
	if err != nil || len(wraw) == 0 {
		t.Fatalf("%v %x", err, wraw)
	}

	inhibitRaw, err := bacnet.AppendContextUnsigned(nil, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	inhibitRaw, err = bacnet.AppendContextUnsigned(inhibitRaw, 1, 8)
	if err != nil {
		t.Fatal(err)
	}
	inhibitRaw, err = bacnet.AppendContextTagged(inhibitRaw, 2, []bacnet.Element{{Value: bacnet.UnsignedValue(1)}})
	if err != nil {
		t.Fatal(err)
	}
	inhibitRaw, err = bacnet.AppendContextBool(inhibitRaw, 3, true)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeWriteGroup(inhibitRaw, bacnet.DefaultDecodeLimits())
	if err != nil || got.InhibitDelay == nil || !*got.InhibitDelay {
		t.Fatalf("%v %+v", err, got)
	}

	ts, err := service.EncodeTimeSynchronization(service.TimeSynchronization{
		Date: bacnet.Date{Year: 126, Month: 8, Day: 2, Weekday: 7},
		Time: bacnet.Time{Hour: 12, Minute: 0, Second: 0, Hundredths: 0},
	})
	if err != nil || len(ts) == 0 {
		t.Fatalf("%v %x", err, ts)
	}
}
