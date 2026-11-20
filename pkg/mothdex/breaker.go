package mothdex

import (
	"sync"
	"time"
)

// Breach circuit constants from the contract (section 4).
const (
	breachThreshold = 3
	breachWindow    = 10 * time.Minute
	breachCooldown  = 10 * time.Minute
)

// breaker counts HTTP 429 answers and opens a cooldown after repeated breaches.
// It is the only mutable state of a Client and is guarded by a mutex.
type breaker struct {
	mu    sync.Mutex
	hits  []time.Time
	until time.Time
}

func newBreaker(seed time.Time) *breaker { return &breaker{until: seed} }

// remaining returns how long the circuit stays open at now, or 0 when closed.
func (b *breaker) remaining(now time.Time) time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	return max(b.until.Sub(now), 0)
}

// record registers a 429 seen at now. It reports the cooldown end and whether
// this very hit opened the circuit.
func (b *breaker) record(now time.Time) (until time.Time, opened bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	hits := append(recentHits(b.hits, now), now)
	if !breachReached(hits) {
		b.hits = hits
		return b.until, false
	}
	b.hits = nil
	b.until = now.Add(breachCooldown)
	return b.until, true
}

// recentHits returns a new slice with only the hits inside the window ending at now.
func recentHits(hits []time.Time, now time.Time) []time.Time {
	out := make([]time.Time, 0, len(hits)+1)
	for _, h := range hits {
		if now.Sub(h) < breachWindow {
			out = append(out, h)
		}
	}
	return out
}

// breachReached is the pure rule: the third 429 inside the window opens the circuit.
func breachReached(hits []time.Time) bool { return len(hits) >= breachThreshold }
