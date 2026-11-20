// Package fakeclock is a manually advanced clock for tests: nothing sleeps for
// real, time only moves when the test calls Advance.
package fakeclock

import (
	"context"
	"sync"
	"testing"
	"time"
)

// blockTimeout is a deadlock guard for BlockUntil. It uses real time on
// purpose and only fires when a test is already failing; do not use it to pace
// anything.
const blockTimeout = 10 * time.Second

type waiter struct {
	at time.Time
	ch chan time.Time
}

// Clock is safe for concurrent use.
type Clock struct {
	mu      sync.Mutex
	now     time.Time
	waiters []*waiter
	changed chan struct{} // closed and replaced whenever the waiter set changes
}

// New returns a clock frozen at start.
func New(start time.Time) *Clock {
	return &Clock{now: start, changed: make(chan struct{})}
}

// Now returns the current fake time.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// After returns a channel that receives once the clock has advanced by d.
// A non-positive d fires immediately. The timer stays pending until it fires;
// use NewTimer when the wait can be abandoned.
func (c *Clock) After(d time.Duration) <-chan time.Time {
	ch, _ := c.NewTimer(d)
	return ch
}

// NewTimer is After plus a stop function that removes the timer if it has not
// fired yet, so an abandoned wait does not stay pending. stop reports whether
// it removed the timer.
func (c *Clock) NewTimer(d time.Duration) (<-chan time.Time, func() bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	if d <= 0 {
		ch <- c.now
		return ch, func() bool { return false }
	}
	w := &waiter{at: c.now.Add(d), ch: ch}
	c.waiters = append(c.waiters, w)
	c.signalLocked()
	return ch, func() bool { return c.remove(w) }
}

// Sleep waits d of fake time or until ctx ends, whichever comes first, and
// leaves no timer pending either way. It matches a Sleeper interface.
func (c *Clock) Sleep(ctx context.Context, d time.Duration) error {
	ch, stop := c.NewTimer(d)
	defer stop()
	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Clock) remove(w *waiter) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, x := range c.waiters {
		if x == w {
			c.waiters = append(c.waiters[:i], c.waiters[i+1:]...)
			c.signalLocked()
			return true
		}
	}
	return false
}

// Advance moves time forward by d and fires every waiter that is now due.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	kept := c.waiters[:0]
	for _, w := range c.waiters {
		if w.at.After(c.now) {
			kept = append(kept, w)
			continue
		}
		w.ch <- c.now
	}
	c.waiters = kept
	c.signalLocked()
}

// Pending reports how many timers are waiting.
func (c *Clock) Pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.waiters)
}

// BlockUntil waits until at least n timers are pending, so a test can
// Advance only after the code under test has started waiting.
func (c *Clock) BlockUntil(t testing.TB, n int) {
	t.Helper()
	c.waitFor(t, func(pending int) bool { return pending >= n }, "at least", n)
}

// BlockUntilIdle waits until no timer is pending, for example after the code
// under test abandoned a wait.
func (c *Clock) BlockUntilIdle(t testing.TB) {
	t.Helper()
	c.waitFor(t, func(pending int) bool { return pending == 0 }, "exactly", 0)
}

func (c *Clock) waitFor(t testing.TB, ok func(int) bool, kind string, n int) {
	t.Helper()
	guard := time.NewTimer(blockTimeout)
	defer guard.Stop()
	for {
		c.mu.Lock()
		ready := ok(len(c.waiters))
		ch := c.changed
		c.mu.Unlock()
		if ready {
			return
		}
		select {
		case <-ch:
		case <-guard.C:
			t.Fatalf("fakeclock: timed out waiting for %s %d pending timers (have %d)", kind, n, c.Pending())
		}
	}
}

func (c *Clock) signalLocked() {
	close(c.changed)
	c.changed = make(chan struct{})
}
