// SPDX-License-Identifier: MIT

package clock

import (
	"context"
	"sync"
	"time"
)

// ContextWithTimeout returns a child context cancelled when parent cancels,
// Close-equivalent parent Done, or the injected clock timer fires.
// The returned cancel stops the timer and terminates the helper goroutine.
func ContextWithTimeout(parent context.Context, clk Clock, d time.Duration) (context.Context, context.CancelFunc) {
	if clk == nil {
		clk = Real{}
	}
	ctx, cancel := context.WithCancel(parent)
	if d < 0 {
		d = 0
	}
	timer := clk.NewTimer(d)
	var once sync.Once
	stop := func() {
		once.Do(func() {
			timer.Stop()
			cancel()
		})
	}
	go func() {
		select {
		case <-timer.C():
			cancel()
		case <-ctx.Done():
			timer.Stop()
		}
	}()
	return ctx, stop
}
