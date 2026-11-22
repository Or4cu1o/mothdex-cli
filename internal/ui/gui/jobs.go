package gui

import (
	"maps"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/consent"
)

// jobTally is the GUI's count of the downloads of this run: the last snapshot of every job it has seen.
// It is a value: apply returns a new tally.
type jobTally struct {
	byID map[app.JobID]app.JobSnapshot
}

// apply records a snapshot (an event or an entry of Jobs) and returns the previous one, if any. The tally
// counts jobs by state, so an event that only moves a job's progress changes nothing and copies nothing.
func (t jobTally) apply(j app.JobSnapshot) (jobTally, app.JobSnapshot, bool) {
	prev, known := t.byID[j.ID]
	if known && prev.State == j.State {
		return t, prev, true // only progress changed: counts depend on the state alone, so nothing is copied
	}
	next := maps.Clone(t.byID)
	if next == nil {
		next = map[app.JobID]app.JobSnapshot{}
	}
	next[j.ID] = j
	return jobTally{byID: next}, prev, known
}

// counts is the number of jobs transferring or waiting for a retry (active) and the number still queued, as
// the queue badge shows them.
func (t jobTally) counts() (active, queued int) {
	for _, j := range t.byID {
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

// unfinished is the number of jobs that have not reached a final state.
func (t jobTally) unfinished() int {
	active, queued := t.counts()
	return active + queued
}

// summary counts the jobs of this run by how they ended. A job that stopped on its real size and waits for the
// person's OK (T-56) is counted apart: nothing failed, so it is not in failed.
func (t jobTally) summary() (done, skipped, failed, canceled, waiting int) {
	for _, j := range t.byID {
		switch {
		case j.State == app.StateDone:
			done++
		case j.State == app.StateSkipped:
			skipped++
		case consent.Needed(j):
			waiting++
		case j.State == app.StateFailed:
			failed++
		case j.State == app.StateCanceled:
			canceled++
		}
	}
	return done, skipped, failed, canceled, waiting
}

// without drops a job from the tally: a stopped job that was queued again is replaced by the new one.
func (t jobTally) without(id app.JobID) jobTally {
	if _, ok := t.byID[id]; !ok {
		return t
	}
	next := maps.Clone(t.byID)
	delete(next, id)
	return jobTally{byID: next}
}
