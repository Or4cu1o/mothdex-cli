package proxy

import (
	"math"
	"testing"
)

// ledger.swap (downloader 3.2 step 5) keeps a job going when free - (reserved - its own claim) covers the
// new need. These cases pin its edges, which the case tests of cases_size_test.go reach only far from them.
func TestLedgerSwapEdges(t *testing.T) {
	const dev = uint64(7)
	seed := func() *ledger {
		l := newLedger()
		// Another job holds 60, this job holds 40: 100 reserved of 200 free.
		if !l.reserve([]claim{{dev: dev, free: 200, need: 60}}) || !l.reserve([]claim{{dev: dev, free: 200, need: 40}}) {
			t.Fatal("setup claims must fit")
		}
		return l
	}
	own := []claim{{dev: dev, free: 200, need: 40}}

	t.Run("an exact fit swaps, counting the own claim only once", func(t *testing.T) {
		l := seed()
		// 200 - (100 - 40) = 140 free for this job: a need of exactly 140 fits.
		if !l.swap(own, []claim{{dev: dev, free: 200, need: 140}}) {
			t.Fatal("a need equal to the room left must fit")
		}
		if got := l.reserved[dev]; got != 200 {
			t.Fatalf("reserved %d, want 200 (60 of the other job + the new 140)", got)
		}
	})

	t.Run("one byte more is refused and nothing changes", func(t *testing.T) {
		l := seed()
		if l.swap(own, []claim{{dev: dev, free: 200, need: 141}}) {
			t.Fatal("a need above the room left must be refused")
		}
		if got := l.reserved[dev]; got != 100 {
			t.Fatalf("reserved %d after a refused swap, want the old 100", got)
		}
	})

	t.Run("a smaller need shrinks the claim", func(t *testing.T) {
		l := seed()
		if !l.swap(own, []claim{{dev: dev, free: 200, need: 10}}) {
			t.Fatal("a smaller need must fit")
		}
		if got := l.reserved[dev]; got != 70 {
			t.Fatalf("reserved %d, want 70 (60 + 10)", got)
		}
	})

	t.Run("two claims on one device that overflow int64 are refused", func(t *testing.T) {
		l := seed()
		next := []claim{{dev: dev, free: math.MaxInt64, need: math.MaxInt64}, {dev: dev, free: math.MaxInt64, need: 1}}
		if l.swap(own, next) {
			t.Fatal("needs that overflow int64 must be refused, not wrapped")
		}
		if got := l.reserved[dev]; got != 100 {
			t.Fatalf("reserved %d after a refused swap, want the old 100", got)
		}
	})
}
