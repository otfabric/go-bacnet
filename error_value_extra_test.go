// SPDX-License-Identifier: MIT

package bacnet_test

import (
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
)

func TestDecodeBACnetErrorVariants(t *testing.T) {
	// Context encoding via EncodeBACnetError.
	enc, err := bacnet.EncodeBACnetError(nil, 2, 5)
	if err != nil {
		t.Fatal(err)
	}
	els, _, err := bacnet.ParseSequence(enc, bacnet.DefaultDecodeLimits(), -1)
	if err != nil {
		t.Fatal(err)
	}
	class, code, err := bacnet.DecodeBACnetError(els)
	if err != nil || class != 2 || code != 5 {
		t.Fatalf("%d/%d err=%v", class, code, err)
	}

	// Application enumerated pair.
	app := []bacnet.Element{
		{Value: bacnet.EnumValue(1)},
		{Value: bacnet.EnumValue(3)},
	}
	class, code, err = bacnet.DecodeBACnetError(app)
	if err != nil || class != 1 || code != 3 {
		t.Fatalf("app %d/%d err=%v", class, code, err)
	}

	if _, _, err := bacnet.DecodeBACnetError(nil); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatal(err)
	}
	if _, _, err := bacnet.DecodeBACnetError([]bacnet.Element{
		{Context: true, TagNumber: 0, Value: bacnet.UnsignedValue(1)},
		{Value: bacnet.EnumValue(2)},
	}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatal("mixed encoding")
	}
	if _, _, err := bacnet.DecodeBACnetError([]bacnet.Element{
		{Context: true, TagNumber: 0, Value: bacnet.UnsignedValue(1)},
	}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatal("missing code")
	}
	if _, _, err := bacnet.DecodeBACnetError([]bacnet.Element{
		{Value: bacnet.RealValue(1)},
		{Value: bacnet.EnumValue(1)},
	}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatal("bad app kind")
	}
}

func TestDecodeBACnetErrorExtraMalformed(t *testing.T) {
	constructed := []bacnet.Element{{
		Context: true, TagNumber: 0, Opening: true,
	}}
	if _, _, err := bacnet.DecodeBACnetError(constructed); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("constructed in error: %v", err)
	}

	if _, _, err := bacnet.DecodeBACnetError([]bacnet.Element{
		{Context: true, TagNumber: 0, Value: bacnet.UnsignedValue(1)},
		{Context: true, TagNumber: 0, Value: bacnet.UnsignedValue(2)},
	}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("duplicate class: %v", err)
	}

	if _, _, err := bacnet.DecodeBACnetError([]bacnet.Element{
		{Context: true, TagNumber: 0, Value: bacnet.UnsignedValue(1)},
		{Context: true, TagNumber: 2, Value: bacnet.UnsignedValue(2)},
	}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("unexpected tag: %v", err)
	}

	if _, _, err := bacnet.DecodeBACnetError([]bacnet.Element{
		{Value: bacnet.EnumValue(1)},
	}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("app wrong arity: %v", err)
	}

	if _, _, err := bacnet.DecodeBACnetError([]bacnet.Element{
		{Value: bacnet.RealValue(1)},
		{Value: bacnet.RealValue(2)},
	}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("app wrong kinds: %v", err)
	}

	if _, _, err := bacnet.DecodeBACnetError([]bacnet.Element{
		{Context: true, TagNumber: 0, Value: bacnet.UnsignedValue(1 << 17)},
		{Context: true, TagNumber: 1, Value: bacnet.UnsignedValue(1)},
	}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("context overflow: %v", err)
	}

	if _, _, err := bacnet.DecodeBACnetError([]bacnet.Element{
		{Value: bacnet.EnumValue(1 << 17)},
		{Value: bacnet.EnumValue(1)},
	}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("app enum overflow: %v", err)
	}
}
