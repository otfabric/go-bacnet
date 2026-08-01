// SPDX-License-Identifier: MIT

package bacnet_test

import (
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
)

func TestContextSignedBitStringCharacterRoundTrip(t *testing.T) {
	enc, err := bacnet.AppendContextSigned(nil, 4, -12)
	if err != nil {
		t.Fatal(err)
	}
	els, n, err := bacnet.ParseSequence(enc, bacnet.DefaultDecodeLimits(), -1)
	if err != nil || n != len(enc) || len(els) != 1 {
		t.Fatalf("parse signed: %v n=%d", err, n)
	}
	got, err := bacnet.ContextSigned(els[0])
	if err != nil || got != -12 {
		t.Fatalf("signed=%d err=%v", got, err)
	}

	bs := bacnet.BitString{UnusedBits: 5, Bytes: []byte{0xA0}}
	enc, err = bacnet.AppendContextBitString(nil, 3, bs)
	if err != nil {
		t.Fatal(err)
	}
	els, _, err = bacnet.ParseSequence(enc, bacnet.DefaultDecodeLimits(), -1)
	if err != nil {
		t.Fatal(err)
	}
	gotBS, err := bacnet.ContextBitString(els[0])
	if err != nil || gotBS.UnusedBits != 5 || len(gotBS.Bytes) != 1 || gotBS.Bytes[0] != 0xA0 {
		t.Fatalf("bitstring %+v err=%v", gotBS, err)
	}

	enc, err = bacnet.AppendContextTime(nil, 0, bacnet.Time{Hour: 1, Minute: 2, Second: 3, Hundredths: 4})
	if err != nil {
		t.Fatal(err)
	}
	els, _, err = bacnet.ParseSequence(enc, bacnet.DefaultDecodeLimits(), -1)
	if err != nil || len(els) != 1 {
		t.Fatalf("%v %d", err, len(els))
	}
	gotT, err := bacnet.ContextTime(els[0])
	if err != nil || gotT.Hour != 1 || gotT.Hundredths != 4 {
		t.Fatalf("%v %+v", err, gotT)
	}
	if _, err := bacnet.ContextTime(bacnet.Element{}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("empty time: %v", err)
	}
	if _, err := bacnet.ContextTime(bacnet.Element{
		Context: true, Value: bacnet.ApplicationValue{Kind: bacnet.ValueOctetString, OctetString: []byte{1}},
	}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("short time: %v", err)
	}

	cs := bacnet.CharacterString{Encoding: 0, Value: "zone"}
	enc, err = bacnet.AppendContextCharacterString(nil, 3, cs)
	if err != nil {
		t.Fatal(err)
	}
	els, _, err = bacnet.ParseSequence(enc, bacnet.DefaultDecodeLimits(), -1)
	if err != nil {
		t.Fatal(err)
	}
	gotCS, err := bacnet.ContextCharacterString(els[0])
	if err != nil || gotCS.Value != "zone" {
		t.Fatalf("char %+v err=%v", gotCS, err)
	}

	if _, err := bacnet.ContextSigned(bacnet.Element{}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("empty signed: %v", err)
	}
	if _, err := bacnet.ContextBitString(bacnet.Element{Context: true}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("empty bitstring: %v", err)
	}
	if _, err := bacnet.ContextCharacterString(bacnet.Element{Context: true}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("empty char: %v", err)
	}
	constructed := bacnet.Element{
		Context: true, TagNumber: 1,
		Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed},
	}
	if _, err := bacnet.ContextBitString(constructed); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("constructed bitstring: %v", err)
	}
	if _, err := bacnet.ContextCharacterString(constructed); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("constructed char: %v", err)
	}
	if _, err := bacnet.ContextSigned(constructed); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("constructed signed: %v", err)
	}
	if _, err := bacnet.ContextTime(constructed); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("constructed time: %v", err)
	}
}
