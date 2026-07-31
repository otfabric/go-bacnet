// SPDX-License-Identifier: MIT

package diag_test

import (
	"testing"

	"github.com/otfabric/go-bacnet/internal/diag"
)

func TestDiscardAndFunc(t *testing.T) {
	diag.Discard{}.Report(diag.Event{Kind: diag.KindMalformed, Message: "ignored"})

	var got diag.Event
	var sink diag.Func = func(e diag.Event) { got = e }
	sink.Report(diag.Event{Kind: diag.KindCOV, Message: "cov"})
	if got.Kind != diag.KindCOV || got.Message != "cov" {
		t.Fatalf("got %#v", got)
	}

	var nilFunc diag.Func
	nilFunc.Report(diag.Event{Kind: diag.KindRouter, Message: "noop"}) // must not panic
}

func TestFormat(t *testing.T) {
	s := diag.Format(diag.Event{Kind: diag.KindWrongSource, Message: "peer"})
	if s != "wrong_source: peer" {
		t.Fatalf("Format=%q", s)
	}
}
