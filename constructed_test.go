// SPDX-License-Identifier: MIT

package bacnet_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
)

func TestParseSequenceDepthLimit(t *testing.T) {
	// Build nested opening tags deeper than MaxConstructedDepth.
	limits := bacnet.DecodeLimits{MaxConstructedDepth: 3, MaxElements: 64}
	var data []byte
	for i := 0; i < 5; i++ {
		data = append(data, 0x0E) // opening context 0
	}
	_, _, err := bacnet.ParseSequence(data, limits, -1)
	if err == nil {
		t.Fatal("expected depth limit")
	}
}

func TestConstructedRoundTrip(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	// [3] { application unsigned 1 }
	raw := []byte{0x3E, 0x21, 0x01, 0x3F}
	els, n, err := bacnet.ParseSequence(raw, limits, -1)
	if err != nil || n != len(raw) || len(els) != 1 {
		t.Fatalf("%v n=%d els=%d", err, n, len(els))
	}
	if !bacnet.IsContextConstructed(els[0]) {
		t.Fatalf("%+v", els[0])
	}
	enc, err := bacnet.AppendTag(nil, els[0])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(enc, raw) {
		t.Fatalf("got %x want %x", enc, raw)
	}
}

func TestAggregateElementBudget(t *testing.T) {
	limits := bacnet.DecodeLimits{MaxConstructedDepth: 8, MaxElements: 3}
	// Three primitives + nested constructed would exceed budget 3.
	raw := []byte{
		0x21, 0x01, // unsigned 1
		0x21, 0x02, // unsigned 2
		0x0E, 0x21, 0x03, 0x0F, // constructed with one element
	}
	_, _, err := bacnet.ParseSequence(raw, limits, -1)
	if err == nil {
		t.Fatal("expected max elements")
	}
}

func TestParseSequenceClosingErrors(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()

	if _, _, err := bacnet.ParseSequence([]byte{0x0F}, limits, -1); err == nil {
		t.Fatal("expected unexpected closing")
	}
	if _, _, err := bacnet.ParseSequence([]byte{0x0E, 0x21, 0x01}, limits, 0); err == nil {
		t.Fatal("expected missing closing")
	}
}

func TestAppendContextTaggedBodyFail(t *testing.T) {
	body := []bacnet.Element{{Value: bacnet.ApplicationValue{Kind: bacnet.ValueKind(200)}}}
	if _, err := bacnet.AppendContextTagged(nil, 1, body); !errors.Is(err, bacnet.ErrUnsupported) {
		t.Fatalf("body fail: %v", err)
	}
}

func TestAppendContextObjectIDInvalid(t *testing.T) {
	bad := bacnet.ObjectIdentifier{Type: bacnet.ObjectType(0x400), Instance: 1}
	if _, err := bacnet.AppendContextObjectID(nil, 0, bad); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("invalid object id: %v", err)
	}
}

func TestContextUnsignedAndObjectIDGuards(t *testing.T) {
	appEl := bacnet.Element{Value: bacnet.UnsignedValue(1)}
	if _, err := bacnet.ContextUnsigned(appEl); err == nil {
		t.Fatal("expected non-context unsigned error")
	}
	if _, err := bacnet.ContextObjectID(appEl); err == nil {
		t.Fatal("expected non-context object id error")
	}

	opening := bacnet.Element{Context: true, TagNumber: 1, Opening: true}
	if _, err := bacnet.ContextUnsigned(opening); err == nil {
		t.Fatal("expected opening tag unsigned error")
	}
	if _, err := bacnet.ContextObjectID(opening); err == nil {
		t.Fatal("expected opening tag object id error")
	}
}
