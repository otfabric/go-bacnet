// SPDX-License-Identifier: MIT

package fixtures_test

import (
	"testing"

	"github.com/otfabric/go-bacnet/internal/fixtures"
)

func TestMetaHasTagAndMalformed(t *testing.T) {
	m := fixtures.Meta{
		Tags:    []string{"roundtrip", "error"},
		License: fixtures.License{Status: "malformed-constructed"},
	}
	if !m.HasTag("roundtrip") || m.HasTag("missing") {
		t.Fatalf("HasTag %#v", m.Tags)
	}
	if !m.Malformed() {
		t.Fatal("expected Malformed")
	}
	ok := fixtures.Meta{License: fixtures.License{Status: "ok"}}
	if ok.Malformed() {
		t.Fatal("ok should not be malformed")
	}
	raw, err := (fixtures.Meta{ID: "empty-payload"}).Bytes()
	if err != nil || len(raw) != 0 {
		t.Fatalf("empty input_hex: %v %#v", err, raw)
	}
	if _, err := (fixtures.Meta{ID: "bad", InputHex: "zz"}).Bytes(); err == nil {
		t.Fatal("expected hex decode error")
	}
}
