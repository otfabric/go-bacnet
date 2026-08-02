// SPDX-License-Identifier: MIT

package service_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestWhoAmIYouAreRoundTrip(t *testing.T) {
	who := service.WhoAmI{VendorID: 999, ModelName: "InteropModel", SerialNumber: "SN-001"}
	raw, err := service.EncodeWhoAmI(who)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeWhoAmI(raw, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if got != who {
		t.Fatalf("got %+v want %+v", got, who)
	}

	dev := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1234}
	you := service.YouAre{
		VendorID: 999, ModelName: "InteropModel", SerialNumber: "SN-001", Device: dev,
	}
	yraw, err := service.EncodeYouAre(you)
	if err != nil {
		t.Fatal(err)
	}
	ygot, err := service.DecodeYouAre(yraw, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if ygot.VendorID != you.VendorID || ygot.ModelName != you.ModelName ||
		ygot.SerialNumber != you.SerialNumber || ygot.Device != you.Device {
		t.Fatalf("got %+v want %+v", ygot, you)
	}
}

func TestWhoAmIYouAreDecodeErrors(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	if _, err := service.DecodeWhoAmI([]byte{0xff}, limits); err == nil {
		t.Fatal("expected WhoAmI error")
	}
	if _, err := service.DecodeYouAre([]byte{0xff}, limits); err == nil {
		t.Fatal("expected YouAre error")
	}

	badVendor, err := bacnet.AppendApplicationValue(nil, bacnet.RealValue(1.0))
	if err != nil {
		t.Fatal(err)
	}
	badVendor, err = bacnet.AppendApplicationValue(badVendor, bacnet.ApplicationValue{
		Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: "m"},
	})
	if err != nil {
		t.Fatal(err)
	}
	badVendor, err = bacnet.AppendApplicationValue(badVendor, bacnet.ApplicationValue{
		Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: "s"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeWhoAmI(badVendor, limits); err == nil {
		t.Fatal("expected WhoAmI bad vendor")
	}
	if _, err := service.DecodeYouAre(badVendor, limits); err == nil {
		t.Fatal("expected YouAre bad vendor")
	}

	who, err := service.EncodeWhoAmI(service.WhoAmI{VendorID: 1, ModelName: "m", SerialNumber: "s"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeWhoAmI(append(append([]byte{}, who...), 0x00), limits); err == nil {
		t.Fatal("expected WhoAmI trailing")
	}

	youNoDev, err := bacnet.AppendApplicationValue(nil, bacnet.UnsignedValue(1))
	if err != nil {
		t.Fatal(err)
	}
	youNoDev, err = bacnet.AppendApplicationValue(youNoDev, bacnet.ApplicationValue{
		Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: "m"},
	})
	if err != nil {
		t.Fatal(err)
	}
	youNoDev, err = bacnet.AppendApplicationValue(youNoDev, bacnet.ApplicationValue{
		Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: "s"},
	})
	if err != nil {
		t.Fatal(err)
	}
	ygot, err := service.DecodeYouAre(youNoDev, limits)
	if err != nil {
		t.Fatal(err)
	}
	if ygot.Device.Type != 0 || ygot.Device.Instance != 0 {
		t.Fatalf("optional device: %+v", ygot.Device)
	}
	if _, err := service.DecodeYouAre(append(append([]byte{}, youNoDev...), 0x00), limits); err == nil {
		t.Fatal("expected YouAre trailing / bad device")
	}
	if _, err := service.DecodeYouAre(append(append([]byte{}, youNoDev...), 0x21, 0x01), limits); err == nil {
		t.Fatal("expected YouAre bad device")
	}

	vendorOnly, err := bacnet.AppendApplicationValue(nil, bacnet.UnsignedValue(1))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeWhoAmI(vendorOnly, limits); err == nil {
		t.Fatal("expected WhoAmI truncated")
	}
	if _, err := service.DecodeYouAre(vendorOnly, limits); err == nil {
		t.Fatal("expected YouAre truncated")
	}
	badCS, err := bacnet.AppendApplicationValue(nil, bacnet.UnsignedValue(1))
	if err != nil {
		t.Fatal(err)
	}
	badCS, err = bacnet.AppendApplicationValue(badCS, bacnet.UnsignedValue(2))
	if err != nil {
		t.Fatal(err)
	}
	badCS, err = bacnet.AppendApplicationValue(badCS, bacnet.ApplicationValue{
		Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: "s"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeWhoAmI(badCS, limits); err == nil {
		t.Fatal("expected WhoAmI bad model")
	}
	if _, err := service.DecodeYouAre(badCS, limits); err == nil {
		t.Fatal("expected YouAre bad model")
	}

	overflow, err := bacnet.AppendApplicationValue(nil, bacnet.UnsignedValue(0x10000))
	if err != nil {
		t.Fatal(err)
	}
	overflow, err = bacnet.AppendApplicationValue(overflow, bacnet.ApplicationValue{
		Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: "m"},
	})
	if err != nil {
		t.Fatal(err)
	}
	overflow, err = bacnet.AppendApplicationValue(overflow, bacnet.ApplicationValue{
		Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: "s"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeWhoAmI(overflow, limits); err == nil {
		t.Fatal("expected WhoAmI vendor overflow")
	}
	if _, err := service.DecodeYouAre(overflow, limits); err == nil {
		t.Fatal("expected YouAre vendor overflow")
	}
}

func TestWhoAmIYouAreEncodeErrors(t *testing.T) {
	badType := bacnet.ObjectIdentifier{Type: bacnet.ObjectType(0x400), Instance: 1}
	if _, err := service.EncodeYouAre(service.YouAre{
		VendorID: 1, ModelName: "m", SerialNumber: "s", Device: badType,
	}); err == nil {
		t.Fatal("expected YouAre bad type")
	}
	badInst := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: bacnet.MaxObjectInstance + 1}
	if _, err := service.EncodeYouAre(service.YouAre{
		VendorID: 1, ModelName: "m", SerialNumber: "s", Device: badInst,
	}); err == nil {
		t.Fatal("expected YouAre bad instance")
	}
}

func TestAuthRequestRoundTripAndErrors(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	if _, err := service.DecodeAuthRequest(nil, limits); err != nil {
		t.Fatalf("empty AuthRequest: %v", err)
	}
	if _, err := service.DecodeAuthRequest([]byte{}, limits); err != nil {
		t.Fatalf("zero AuthRequest: %v", err)
	}
	empty, err := service.EncodeAuthRequest(service.AuthRequest{})
	if err != nil || empty != nil {
		t.Fatalf("%v %v", empty, err)
	}
	raw, err := service.EncodeAuthRequest(service.AuthRequest{
		Parameters: []bacnet.Element{{Value: bacnet.UnsignedValue(9)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeAuthRequest(raw, limits)
	if err != nil || len(got.Parameters) == 0 {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := service.DecodeAuthRequest([]byte{0x0e, 0xff}, limits); err == nil {
		t.Fatal("expected malformed AuthRequest")
	}
}
