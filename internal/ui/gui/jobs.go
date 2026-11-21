package gui

import (
	"maps"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
)

// jobTally is the GUI's count of the downloads of this run: the last snapshot of every job it has seen.
// It is a value: apply returns a new tally.
type jobTally struct {
	byID map[app.JobID]app.JobSnapshot
}

// apply records a snapshot (an event or an entry of Jobs) and returns the previous one, if any.
func (t jobTally) apply(j app.JobSnapshot) (jobTally, app.JobSnapshot, bool) {
	prev, known := t.byID[j.ID]
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

// summary counts the jobs of this run by how they ended.
func (t jobTally) summary() (done, skipped, failed, canceled int) {
	for _, j := range t.byID {
		switch j.State {
		case app.StateDone:
			done++
		case app.StateSkipped:
			skipped++
		case app.StateFailed:
			failed++
		case app.StateCanceled:
			canceled++
		}
	}
	return done, skipped, failed, canceled
}
