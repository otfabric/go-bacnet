// SPDX-License-Identifier: MIT

package clock_test

import (
	"context"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet/internal/clock"
)

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
