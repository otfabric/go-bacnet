// SPDX-License-Identifier: MIT

// Package clock provides an injectable clock for deterministic lifecycle tests.
package clock

import (
	"sync"
	"time"
)

// Timer is a stoppable timer.
type Timer interface {
	C() <-chan time.Time
	Stop() bool
	Reset(d time.Duration) bool
}

// Clock abstracts time for production and tests.
type Clock interface {
	Now() time.Time
	NewTimer(d time.Duration) Timer
	After(d time.Duration) <-chan time.Time
}

// Real is the wall-clock implementation.
type Real struct{}

func (Real) Now() time.Time { return time.Now() }

func (Real) NewTimer(d time.Duration) Timer {
	return &realTimer{t: time.NewTimer(d)}
}

func (Real) After(d time.Duration) <-chan time.Time { return time.After(d) }

type realTimer struct{ t *time.Timer }

func (t *realTimer) C() <-chan time.Time        { return t.t.C }
func (t *realTimer) Stop() bool                 { return t.t.Stop() }
func (t *realTimer) Reset(d time.Duration) bool { return t.t.Reset(d) }

// Manual is a test clock advanced explicitly. Timers fire when Now reaches their deadline.
type Manual struct {
	mu     sync.Mutex
	now    time.Time
	timers []*manualTimer
}

// NewManual returns a Manual clock starting at t0.
func NewManual(t0 time.Time) *Manual {
	return &Manual{now: t0}
}

func (m *Manual) Now() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.now
}

func (m *Manual) NewTimer(d time.Duration) Timer {
	m.mu.Lock()
	defer m.mu.Unlock()
	ch := make(chan time.Time, 1)
	if d < 0 {
		d = 0
	}
	t := &manualTimer{m: m, ch: ch, deadline: m.now.Add(d), active: true}
	m.timers = append(m.timers, t)
	if d == 0 {
		t.fireLocked()
	}
	return t
}

func (m *Manual) After(d time.Duration) <-chan time.Time {
	return m.NewTimer(d).C()
}

// Advance moves time forward by d and fires due timers.
func (m *Manual) Advance(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.now = m.now.Add(d)
	alive := m.timers[:0]
	for _, t := range m.timers {
		if t.active && !m.now.Before(t.deadline) {
			t.fireLocked()
		}
		if t.active {
			alive = append(alive, t)
		}
	}
	m.timers = alive
}

func (m *Manual) trackLocked(t *manualTimer) {
	for _, existing := range m.timers {
		if existing == t {
			return
		}
	}
	m.timers = append(m.timers, t)
}

type manualTimer struct {
	m        *Manual
	ch       chan time.Time
	deadline time.Time
	active   bool
}

func (t *manualTimer) C() <-chan time.Time { return t.ch }

func (t *manualTimer) Stop() bool {
	t.m.mu.Lock()
	defer t.m.mu.Unlock()
	if !t.active {
		return false
	}
	t.active = false
	return true
}

func (t *manualTimer) Reset(d time.Duration) bool {
	t.m.mu.Lock()
	defer t.m.mu.Unlock()
	active := t.active
	if d < 0 {
		d = 0
	}
	t.deadline = t.m.now.Add(d)
	t.active = true
	t.m.trackLocked(t)
	if d == 0 {
		t.fireLocked()
	}
	return active
}

func (t *manualTimer) fireLocked() {
	if !t.active {
		return
	}
	t.active = false
	select {
	case t.ch <- t.m.now:
	default:
	}
}
