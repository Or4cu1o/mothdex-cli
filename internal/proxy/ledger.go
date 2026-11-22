package proxy

import (
	"math"
	"strings"
	"sync"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const (
	minSpaceMargin = int64(256) << 20 // 256 MiB
	marginDivisor  = 20               // 5 %
)

// spaceNeed is size plus max(5 %, 256 MiB) with overflow-checked integer math (no floats).
func spaceNeed(size int64) (int64, bool) {
	margin := max(size/marginDivisor, minSpaceMargin)
	if size < 0 || size > math.MaxInt64-margin {
		return 0, false
	}
	return size + margin, true
}

// claim is one reservation on one file system, identified by its device id.
type claim struct {
	dev  uint64
	free int64
	need int64
}

// ledger tracks the bytes promised to running jobs per file system, so that parallel jobs
// that each fit together do not fill the disk (TH-14).
type ledger struct {
	mu       sync.Mutex
	reserved map[uint64]int64
}

func newLedger() *ledger { return &ledger{reserved: map[uint64]int64{}} }

// reserve takes every claim or none. Claims on the same device add up.
func (l *ledger) reserve(claims []claim) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	want := map[uint64]int64{}
	free := map[uint64]int64{}
	for _, c := range claims {
		if c.need > math.MaxInt64-want[c.dev] {
			return false
		}
		want[c.dev] += c.need
		free[c.dev] = c.free
	}
	for dev, n := range want {
		if free[dev]-l.reserved[dev] < n {
			return false
		}
	}
	for dev, n := range want {
		l.reserved[dev] += n
	}
	return true
}

// swap replaces a job's own claims by new ones in one step under the ledger lock (downloader.md 3.2
// step 5): the job keeps going only if free - (reserved - its own claim) covers the new need on
// every file system, and its old claim stays untouched when it does not (the caller then fails the
// job, which releases it). A smaller need shrinks the claim.
func (l *ledger) swap(old, next []claim) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	own, want, free := map[uint64]int64{}, map[uint64]int64{}, map[uint64]int64{}
	for _, c := range old {
		own[c.dev] += c.need
	}
	for _, c := range next {
		if c.need > math.MaxInt64-want[c.dev] {
			return false
		}
		want[c.dev] += c.need
		free[c.dev] = c.free
	}
	for dev, n := range want {
		if free[dev]-(l.reserved[dev]-own[dev]) < n {
			return false
		}
	}
	for dev, n := range own {
		l.reserved[dev] = max(l.reserved[dev]-n, 0)
	}
	for dev, n := range want {
		l.reserved[dev] += n
	}
	return true
}

func (l *ledger) release(claims []claim) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, c := range claims {
		l.reserved[c.dev] = max(l.reserved[c.dev]-c.need, 0)
	}
}

// nameSet is the in-process name reservation, keyed by casefold(NFC(destdir/name)) so that
// Game.zip and game.zip, or two spellings of one accented name, collide (TH-08).
type nameSet struct {
	mu    sync.Mutex
	held  map[string]string
	final map[string]bool // names finalized by this Downloader: overwrite never replaces them
}

func newNameSet() *nameSet { return &nameSet{held: map[string]string{}, final: map[string]bool{}} }

func nameKey(destdir, name string) string {
	return cases.Fold().String(norm.NFC.String(strings.TrimSuffix(destdir, "/") + "/" + name))
}

// take reserves the name for jobID and reports false if another job holds it.
func (n *nameSet) take(destdir, name, jobID string) bool {
	k := nameKey(destdir, name)
	n.mu.Lock()
	defer n.mu.Unlock()
	if _, taken := n.held[k]; taken {
		return false
	}
	n.held[k] = jobID
	return true
}

func (n *nameSet) release(destdir, name string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.held, nameKey(destdir, name))
}

// markFinal records a name a job finalized; the reservation itself is released at the terminal
// state, so without this a later job of the same batch could overwrite the earlier file.
func (n *nameSet) markFinal(destdir, name string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.final[nameKey(destdir, name)] = true
}

func (n *nameSet) isFinal(destdir, name string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.final[nameKey(destdir, name)]
}
