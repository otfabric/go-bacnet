// SPDX-License-Identifier: MIT

package bacnet_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
)

func TestErrorNames(t *testing.T) {
	cases := []struct{ got, want string }{
		{bacnet.ErrorClassName(bacnet.ErrorClassProperty), "property"},
		{bacnet.ErrorClassName(64), "class-64"},
		{bacnet.ErrorCodeName(bacnet.ErrorCodeUnknownObject), "unknown-object"},
		{bacnet.ErrorCodeName(1), "code-1"}, // unassigned
		{bacnet.ErrorCodeName(300), "code-300"},
		{bacnet.RejectReasonName(bacnet.RejectReasonInvalidTag), "invalid-tag"},
		{bacnet.RejectReasonName(64), "reason-64"},
		{bacnet.AbortReasonName(4), "segmentation-not-supported"},
		{bacnet.AbortReasonName(200), "reason-200"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}
