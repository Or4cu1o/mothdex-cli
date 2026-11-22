package gui

import (
	"slices"
	"time"
)

// Screens that sit on a stack: Home, then Search, then Results, then Entry (docs/ux/gui.md section 3), with the
// Queue (and later History and Settings) opening on top of whatever is showing. B goes back one level.

// enter puts a screen on top of the current one.
func (m Model) enter(k screenKind) Model {
	m.back = append(slices.Clone(m.back), m.screen)
	m.screen = k
	return m
}

// leave returns to the screen below, or stays when there is none.
func (m Model) leave() Model {
	if len(m.back) == 0 {
		return m
	}
	m.screen = m.back[len(m.back)-1]
	m.back = slices.Clone(m.back[:len(m.back)-1])
	return m
}

// replaceTop swaps the current screen for another, so that B goes back to what was under it.
func (m Model) replaceTop(k screenKind) Model {
	m.screen = k
	return m
}

// toHome drops the whole stack and shows Home.
func (m Model) toHome() Model {
	m.screen, m.back = screenHome, nil
	return m
}

// onStack reports whether a screen is the current one or somewhere below it.
func (m Model) onStack(k screenKind) bool { return m.screen == k || slices.Contains(m.back, k) }

// loadPhase is where a list loaded once and kept for the process stands.
type loadPhase int

const (
	loadIdle loadPhase = iota
	loadBusy
	loadOK
	loadFailed
)

// catalogLoad is a list the service answers once (platforms, regions): loaded on first use, kept for the run.
type catalogLoad[T any] struct {
	phase loadPhase
	items []T
	fail  failure
	since time.Time // when the load began, for the spinner that waits 200 ms
}
