// SPDX-License-Identifier: MIT

package clock_test

import (
	"testing"
	"time"

	"github.com/otfabric/go-bacnet/internal/clock"
)

func TestRealClockSmoke(t *testing.T) {
	var r clock.Real
	now := r.Now()
	if now.IsZero() {
		t.Fatal("zero Now")
	}
	tm := r.NewTimer(5 * time.Millisecond)
	select {
	case <-tm.C():
	case <-time.After(time.Second):
		t.Fatal("real timer")
	}
	tm.Stop()
	select {
	case <-r.After(5 * time.Millisecond):
	case <-time.After(time.Second):
		t.Fatal("real After")
	}
}

func TestManualAfterAndReset(t *testing.T) {
	m := clock.NewManual(time.Unix(0, 0))
	ch := m.After(10 * time.Millisecond)
	m.Advance(5 * time.Millisecond)
	select {
	case <-ch:
		t.Fatal("fired early")
	default:
	}
	m.Advance(10 * time.Millisecond)
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("After not fired")
	}

	tm := m.NewTimer(time.Second)
	_ = tm.Reset(50 * time.Millisecond) // may be false if already fired; still exercise path
	m.Advance(60 * time.Millisecond)
	select {
	case <-tm.C():
	case <-time.After(time.Second):
		t.Fatal("reset timer")
	}
}
