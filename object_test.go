// SPDX-License-Identifier: MIT

package bacnet_test

import (
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
)

func TestObjectIdentifierEncodeDecode(t *testing.T) {
	id := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1234}
	v, err := bacnet.EncodeObjectIdentifier(id)
	if err != nil {
		t.Fatal(err)
	}
	got := bacnet.DecodeObjectIdentifier(v)
	if got != id {
		t.Fatalf("%#v", got)
	}
	raw, err := bacnet.AppendObjectIdentifier(nil, id)
	if err != nil || len(raw) != 4 {
		t.Fatalf("%x %v", raw, err)
	}
	if id.String() != "8:1234" {
		t.Fatalf("String=%q", id.String())
	}

	if _, err := bacnet.EncodeObjectIdentifier(bacnet.ObjectIdentifier{Type: 0x400, Instance: 0}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("type range: %v", err)
	}
	if _, err := bacnet.EncodeObjectIdentifier(bacnet.ObjectIdentifier{Type: 0, Instance: bacnet.MaxObjectInstance + 1}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("instance range: %v", err)
	}
}
