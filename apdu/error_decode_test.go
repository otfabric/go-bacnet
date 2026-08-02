// SPDX-License-Identifier: MIT

package apdu_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
)

func TestDecodeErrorClassCodeTrailing(t *testing.T) {
	raw, err := bacnet.EncodeBACnetError(nil, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := apdu.DecodeErrorClassCode(append(append([]byte{}, raw...), 0x00), bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected trailing")
	}
	if _, _, err := apdu.DecodeErrorClassCode([]byte{0xff}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected malformed")
	}
}
