// SPDX-License-Identifier: MIT

package main

import (
	"flag"
	"io"
	"testing"

	"github.com/otfabric/go-bacnet"
)

func newFlags() *flag.FlagSet {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func TestUint32FlagRange(t *testing.T) {
	for _, tc := range []struct {
		arg  string
		want uint32
		ok   bool
	}{
		{"4294967295", 4294967295, true},
		{"4294967296", 0, false},
		{"-1", 0, false},
		{"x", 0, false},
	} {
		fs := newFlags()
		v := uint32Flag(fs, "n", 7, "n")
		if *v != 7 {
			t.Fatalf("default %d", *v)
		}
		err := fs.Parse([]string{"-n", tc.arg})
		if (err == nil) != tc.ok || (tc.ok && *v != tc.want) {
			t.Fatalf("%s: v=%d err=%v", tc.arg, *v, err)
		}
	}
}

func TestParseTarget(t *testing.T) {
	tg, err := parseTarget("192.168.1.10:47808")
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{192, 168, 1, 10, 0xBA, 0xC0}
	if got := tg.Address.MAC().Bytes(); string(got) != string(want) || tg.Address.Scope() != bacnet.AddressLocalStation {
		t.Fatalf("address %v mac % x", tg.Address, got)
	}
	for _, bad := range []string{"nope", "[::1]:47808"} {
		if _, err := parseTarget(bad); err == nil {
			t.Fatalf("%s: want error", bad)
		}
	}
}

func TestParseObject(t *testing.T) {
	obj, err := parseObject("2:1")
	if err != nil || obj.Type != bacnet.ObjectTypeAnalogValue || obj.Instance != 1 {
		t.Fatalf("obj=%v err=%v", obj, err)
	}
	for _, bad := range []string{"2", "70000:1", "2:4294967296", "a:1"} {
		if _, err := parseObject(bad); err == nil {
			t.Fatalf("%s: want error", bad)
		}
	}
}
