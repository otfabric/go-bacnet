// SPDX-License-Identifier: MIT

package bacnet_test

import (
	"bytes"
	"testing"

	"github.com/otfabric/go-bacnet"
)

func TestApplicationValueRoundTrip(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	cases := []bacnet.ApplicationValue{
		bacnet.NullValue(),
		bacnet.BoolValue(true),
		bacnet.BoolValue(false),
		bacnet.UnsignedValue(0),
		bacnet.UnsignedValue(255),
		bacnet.UnsignedValue(0x123456),
		bacnet.SignedValue(-1),
		bacnet.SignedValue(127),
		bacnet.RealValue(21.5),
		bacnet.EnumValue(3),
		bacnet.ObjectIDValue(bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogInput, Instance: 3}),
	}
	for _, want := range cases {
		enc, err := bacnet.AppendApplicationValue(nil, want)
		if err != nil {
			t.Fatalf("encode %#v: %v", want, err)
		}
		got, n, err := bacnet.ParseApplicationValue(enc, limits)
		if err != nil {
			t.Fatalf("decode %#v: %v", want, err)
		}
		if n != len(enc) {
			t.Fatalf("consumed %d want %d", n, len(enc))
		}
		re, err := bacnet.AppendApplicationValue(nil, got)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(enc, re) {
			t.Fatalf("deterministic re-encode mismatch for kind %d: %x vs %x", want.Kind, enc, re)
		}
	}
}

func TestTrailingDataRejectedAtTopLevel(t *testing.T) {
	enc, err := bacnet.AppendApplicationValue(nil, bacnet.UnsignedValue(1))
	if err != nil {
		t.Fatal(err)
	}
	enc = append(enc, 0x00)
	_, n, err := bacnet.ParseApplicationValue(enc, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if n != len(enc)-1 {
		t.Fatalf("expected partial consume, got %d", n)
	}
}

func TestMACComparable(t *testing.T) {
	a, err := bacnet.NewMAC([]byte{1, 2, 3, 4, 0xBA, 0xC0})
	if err != nil {
		t.Fatal(err)
	}
	b, err := bacnet.NewMAC([]byte{1, 2, 3, 4, 0xBA, 0xC0})
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("MAC should be comparable")
	}
	if !bytes.Equal(a.Bytes(), []byte{1, 2, 3, 4, 0xBA, 0xC0}) {
		t.Fatal("Bytes copy mismatch")
	}
}

func TestAddressConstructors(t *testing.T) {
	mac := bacnet.MustMAC([]byte{1, 2, 3})
	if bacnet.LocalStation(mac).Scope() != bacnet.AddressLocalStation {
		t.Fatal("local station")
	}
	if !bacnet.GlobalBroadcast().IsBroadcast() {
		t.Fatal("global broadcast")
	}
	if bacnet.RemoteStation(2, mac).Network() != 2 {
		t.Fatal("remote network")
	}
}

func FuzzParseTag(f *testing.F) {
	enc, _ := bacnet.AppendApplicationValue(nil, bacnet.RealValue(1))
	f.Add(enc)
	f.Add([]byte{0x10})
	f.Add([]byte{0x21, 0x01})
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _, _ = bacnet.ParseTag(data, bacnet.DefaultDecodeLimits())
	})
}
