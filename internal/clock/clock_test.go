// SPDX-License-Identifier: MIT

package clock_test

import (
	"context"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet/internal/clock"
)

func TestRealClockBasics(t *testing.T) {
	var r clock.Real
	if r.Now().IsZero() {
		t.Fatal("Real.Now zero")
	}
	tm := r.NewTimer(time.Hour)
	if !tm.Stop() {
		t.Fatal("Stop active timer")
	}
	// Stopped timer: Reset starts it again.
	if tm.Reset(30 * time.Millisecond) {
		t.Fatal("Reset on stopped timer should report was-not-active")
	}
	select {
	case <-tm.C():
	case <-time.After(2 * time.Second):
		t.Fatal("Reset timer did not fire")
	}
	select {
	case <-r.After(5 * time.Millisecond):
	case <-time.After(2 * time.Second):
		t.Fatal("After did not fire")
	}
}

func TestManualAdvanceFiresTimer(t *testing.T) {
	m := clock.NewManual(time.Unix(0, 0).UTC())
	tm := m.NewTimer(5 * time.Second)
	select {
	case <-tm.C():
		t.Fatal("timer fired early")
	default:
	}
	m.Advance(5 * time.Second)
	select {
	case got := <-tm.C():
		if !got.Equal(time.Unix(5, 0).UTC()) {
			t.Fatalf("got %v", got)
		}
	default:
		t.Fatal("timer did not fire")
	}
}

func TestManualTimerZeroFires(t *testing.T) {
	m := clock.NewManual(time.Unix(0, 0).UTC())
	tm := m.NewTimer(0)
	select {
	case <-tm.C():
	default:
		t.Fatal("zero-duration timer should fire immediately")
	}
}

func TestManualTimerResetAfterFire(t *testing.T) {
	m := clock.NewManual(time.Unix(0, 0).UTC())
	tm := m.NewTimer(time.Second)
	m.Advance(time.Second)
	select {
	case <-tm.C():
	default:
		t.Fatal("expected first fire")
	}
	tm.Reset(2 * time.Second)
	m.Advance(2 * time.Second)
	select {
	case <-tm.C():
	default:
		t.Fatal("Reset after fire must re-register with Manual clock")
	}
}

func TestContextWithTimeoutCancelStopsHelper(t *testing.T) {
	m := clock.NewManual(time.Unix(0, 0).UTC())
	ctx, stop := clock.ContextWithTimeout(context.Background(), m, time.Hour)
	stop()
	select {
	case <-ctx.Done():
	default:
		t.Fatal("stop should cancel context")
	}
}
