package tui

import (
	"maps"
	"slices"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/consent"
)

// jobBook is the TUI's picture of the downloads of this run: every job it has seen, in the order it
// saw them, plus what the UI needs and the service does not keep (the original request for a retry,
// when a job began to warm up or to transfer, and how many finished since the queue was last idle).
// It is a value: apply and friends return a new book.
type jobBook struct {
	order      []app.JobID
	byID       map[app.JobID]app.JobSnapshot
	reqs       map[app.JobID]app.DownloadRequest
	warmSince  map[app.JobID]time.Time
	xferSince  map[app.JobID]time.Time
	counted    map[app.JobID]bool
	bDone      int
	bSkipped   int
	bFailed    int
	bWaiting   int  // stopped on their real size, waiting for the person's OK: nothing failed
	hasPending bool // something was queued since the last summary
}

func newJobBook() jobBook {
	return jobBook{
		byID: map[app.JobID]app.JobSnapshot{}, reqs: map[app.JobID]app.DownloadRequest{},
		warmSince: map[app.JobID]time.Time{}, xferSince: map[app.JobID]time.Time{}, counted: map[app.JobID]bool{},
	}
}

func (b jobBook) clone() jobBook {
	c := b
	c.order = slices.Clone(b.order)
	c.byID, c.reqs = maps.Clone(b.byID), maps.Clone(b.reqs)
	c.warmSince, c.xferSince, c.counted = maps.Clone(b.warmSince), maps.Clone(b.xferSince), maps.Clone(b.counted)
	return c
}

// apply records a snapshot of a job (an event or an entry of Jobs).
func (b jobBook) apply(j app.JobSnapshot, now time.Time) jobBook {
	c := b.clone()
	if _, known := c.byID[j.ID]; !known {
		c.order = append(c.order, j.ID)
	}
	c.byID[j.ID] = j
	c.hasPending = true
	if j.State == app.StateWarming {
		if _, ok := c.warmSince[j.ID]; !ok {
			c.warmSince[j.ID] = now
		}
	} else {
		delete(c.warmSince, j.ID)
	}
	if j.State == app.StateTransferring {
		if _, ok := c.xferSince[j.ID]; !ok {
			c.xferSince[j.ID] = now
		}
	}
	if j.Terminal() && !c.counted[j.ID] {
		c.counted[j.ID] = true
		switch j.State {
		case app.StateDone:
			c.bDone++
		case app.StateSkipped:
			c.bSkipped++
		default:
			if consent.Needed(j) {
				c.bWaiting++
			} else { // failed or canceled
				c.bFailed++
			}
		}
	}
	return c
}

// withRequests remembers the request behind each new job, for retry.
func (b jobBook) withRequests(ids []app.JobID, reqs []app.DownloadRequest) jobBook {
	c := b.clone()
	for i, id := range ids {
		if i < len(reqs) {
			c.reqs[id] = reqs[i]
		}
	}
	return c
}

// counts says how many jobs transfer (or prepare to) and how many wait.
func (b jobBook) counts() (active, queued int) {
	for _, j := range b.byID {
		switch {
		case j.Terminal():
		case j.State == app.StateQueued:
			queued++
		default:
			active++
		}
	}
	return active, queued
}

// busy is the number of jobs that are not finished: the quit decision and the title summary.
func (b jobBook) busy() int {
	active, queued := b.counts()
	return active + queued
}

func (b jobBook) anyWarming() bool { return len(b.warmSince) > 0 }

// settled reports, once the queue is idle again, how many finished since the last summary.
func (b jobBook) settled() (done, skipped, failed, waiting int, ok bool) {
	if !b.hasPending || b.busy() > 0 || b.bDone+b.bSkipped+b.bFailed+b.bWaiting == 0 {
		return 0, 0, 0, 0, false
	}
	return b.bDone, b.bSkipped, b.bFailed, b.bWaiting, true
}

// resetBatch starts a new count after a summary was shown.
func (b jobBook) resetBatch() jobBook {
	c := b.clone()
	c.bDone, c.bSkipped, c.bFailed, c.bWaiting, c.hasPending = 0, 0, 0, 0, false
	return c
}

// list returns the jobs in the order they were first seen.
func (b jobBook) list() []app.JobSnapshot {
	out := make([]app.JobSnapshot, 0, len(b.order))
	for _, id := range b.order {
		out = append(out, b.byID[id])
	}
	return out
}
