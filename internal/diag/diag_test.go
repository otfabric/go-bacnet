// SPDX-License-Identifier: MIT

package diag_test

import (
	"testing"

	"github.com/otfabric/go-bacnet/internal/diag"
)

func TestDiscardAndFuncSink(t *testing.T) {
	diag.Discard{}.Report(diag.Event{Kind: diag.KindMalformed, Message: "x"})
	var got diag.Event
	s := diag.Func(func(e diag.Event) { got = e })
	s.Report(diag.Event{Kind: diag.KindRouter, Message: "y", Fields: map[string]any{"n": 1}})
	if got.Message != "y" {
		t.Fatalf("%#v", got)
	}
	if s := diag.Format(got); s == "" {
		t.Fatal("format")
	}
	diag.Func(nil).Report(diag.Event{})
}
