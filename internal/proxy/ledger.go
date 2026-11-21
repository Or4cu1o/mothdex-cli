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
	mu   sync.Mutex
	held map[string]string
}

func newNameSet() *nameSet { return &nameSet{held: map[string]string{}} }

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
