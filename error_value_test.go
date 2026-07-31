// SPDX-License-Identifier: MIT

package bacnet_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
)

func TestDecodeBACnetErrorContextAndApplication(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()

	ctxPayload, err := bacnet.EncodeBACnetError(nil, 2, 32)
	if err != nil {
		t.Fatal(err)
	}
	ctxEls, n, err := bacnet.ParseSequence(ctxPayload, limits, -1)
	if err != nil || n != len(ctxPayload) {
		t.Fatalf("parse context: n=%d err=%v", n, err)
	}
	class, code, err := bacnet.DecodeBACnetError(ctxEls)
	if err != nil || class != 2 || code != 32 {
		t.Fatalf("context form: %d/%d err=%v", class, code, err)
	}

	var appPayload []byte
	appPayload, err = bacnet.AppendApplicationValue(appPayload, bacnet.EnumValue(2))
	if err != nil {
		t.Fatal(err)
	}
	appPayload, err = bacnet.AppendApplicationValue(appPayload, bacnet.EnumValue(32))
	if err != nil {
		t.Fatal(err)
	}
	appEls, n, err := bacnet.ParseSequence(appPayload, limits, -1)
	if err != nil || n != len(appPayload) {
		t.Fatalf("parse app: n=%d err=%v", n, err)
	}
	class, code, err = bacnet.DecodeBACnetError(appEls)
	if err != nil || class != 2 || code != 32 {
		t.Fatalf("application form: %d/%d err=%v", class, code, err)
	}
}
