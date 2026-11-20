// Package fakeclock is a manually advanced clock for tests: nothing sleeps for
// real, time only moves when the test calls Advance.
package fakeclock

import (
	"sync"
	"testing"
	"time"
)

// blockTimeout is a deadlock guard for BlockUntil, not a unit of test timing.
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
// A non-positive d fires immediately.
func (c *Clock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	if d <= 0 {
		ch <- c.now
		return ch
	}
	c.waiters = append(c.waiters, &waiter{at: c.now.Add(d), ch: ch})
	c.signalLocked()
	return ch
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
	guard := time.NewTimer(blockTimeout)
	defer guard.Stop()
	for {
		c.mu.Lock()
		ready := len(c.waiters) >= n
		ch := c.changed
		c.mu.Unlock()
		if ready {
			return
		}
		select {
		case <-ch:
		case <-guard.C:
			t.Fatalf("fakeclock: timed out waiting for %d pending timers (have %d)", n, c.Pending())
		}
	}
}

func (c *Clock) signalLocked() {
	close(c.changed)
	c.changed = make(chan struct{})
}
