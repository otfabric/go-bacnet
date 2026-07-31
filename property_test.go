// SPDX-License-Identifier: MIT

package bacnet_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
)

func TestPropertyReferenceEqual(t *testing.T) {
	idx := uint32(1)
	a := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	b := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	c := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue, ArrayIndex: &idx}
	d := bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName, ArrayIndex: &idx}
	if !a.Equal(b) {
		t.Fatal("equal without index")
	}
	if a.Equal(c) || c.Equal(a) {
		t.Fatal("index vs nil")
	}
	if c.Equal(d) {
		t.Fatal("different identifiers")
	}
	idx2 := uint32(1)
	e := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue, ArrayIndex: &idx2}
	if !c.Equal(e) {
		t.Fatal("same index values")
	}
	idx3 := uint32(2)
	f := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue, ArrayIndex: &idx3}
	if c.Equal(f) {
		t.Fatal("different index values")
	}
}
