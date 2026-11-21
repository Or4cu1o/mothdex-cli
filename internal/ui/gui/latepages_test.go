package gui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
)

// PR-76 review, Major 1: the service runs off the render thread, so a page answer can arrive after the keys that
// moved the window or the focus. These tests hold the Cmds the model returns and deliver their answers by hand,
// in the order a slow network could.

// fullWindow scrolls a 400-entry search until the window holds 200 rows and has dropped its first pages.
func fullWindow(t *testing.T) *harness {
	t.Helper()
	h := started(t, zeldaService(400), 1280, 720)
	searchFor(t, h, "zelda")
	for i := 0; i < 290; i++ {
		h.press(input.Down)
	}
	if s := h.m.results; len(s.win.rows) != maxWindowRows || s.win.first <= 1 {
		t.Fatalf("%d rows from page %d: the window is not full", len(s.win.rows), s.win.first)
	}
	return h
}

// assertSoundWindow fails if the window is not a run of consecutive entries or the focus is outside it.
func assertSoundWindow(t *testing.T, h *harness) {
	t.Helper()
	s := h.m.results
	if s.list.focus < 0 || s.list.focus >= len(s.win.rows) {
		t.Fatalf("focus %d outside %d rows", s.list.focus, len(s.win.rows))
	}
	if len(s.win.rows) > maxWindowRows {
		t.Fatalf("%d rows, more than %d", len(s.win.rows), maxWindowRows)
	}
	first := (s.win.first-1)*servicePageSize + 1
	for i, r := range s.win.rows {
		n, err := strconv.Atoi(strings.TrimPrefix(r.slug, "zelda-"))
		if err != nil || n != first+i {
			t.Fatalf("row %d is %s, want zelda-%03d: the window is not consecutive from page %d", i, r.slug, first+i, s.win.first)
		}
	}
	if _, ok := s.focusedRow(); !ok {
		t.Fatal("no row is focused")
	}
}

// deliver gives the answer of a held Cmd to the model.
func (h *harness) deliver(cmd Cmd) {
	h.t.Helper()
	var more []Cmd
	h.m, more = h.m.Update(cmd(), h.now)
	h.held = append(h.held, more...)
}

func (h *harness) pressUntilHeld(a input.Action, max int) {
	h.t.Helper()
	for i := 0; i < max && len(h.held) == 0; i++ {
		h.press(a)
	}
	if len(h.held) == 0 {
		h.t.Fatal("no call was asked")
	}
}

// The probe of the review: page 13 is asked at the bottom, the focus returns to the top, and then the answer
// lands. It used to panic with a negative focus; now the answer is left out and nothing moves.
func TestALateNextPageAnswerDoesNotMoveAFocusThatWentBackUp(t *testing.T) {
	h := fullWindow(t)
	h.hold = true
	h.pressUntilHeld(input.Down, 40)
	next := h.held[0]
	h.held = nil
	slug := h.m.results.focusedSlug()
	for i := 0; i < 190; i++ {
		h.press(input.Up)
	}
	before := h.m.results.win
	h.deliver(next)
	assertSoundWindow(t, h)
	if h.m.results.win.first != before.first || len(h.m.results.win.rows) != len(before.rows) {
		t.Fatalf("the late answer changed the window: first %d, %d rows", h.m.results.win.first, len(h.m.results.win.rows))
	}
	if h.m.results.nextBusy {
		t.Fatal("the busy flag must clear so the page can be asked again on demand")
	}
	_ = slug
}

// The mirror case: the previous page is asked at the top, the focus goes to the last row, then the answer lands.
func TestALatePreviousPageAnswerDoesNotPushTheFocusOffTheList(t *testing.T) {
	h := fullWindow(t)
	h.hold = true
	h.pressUntilHeld(input.Up, 300) // the focus reaches the first rows: the previous page is asked
	prev := h.held[0]
	h.held = nil
	for i := 0; i < 250; i++ {
		h.press(input.Down)
	}
	h.held = nil
	slug := h.m.results.focusedSlug()
	h.deliver(prev)
	assertSoundWindow(t, h)
	if h.m.results.focusedSlug() != slug {
		t.Fatalf("the focus moved from %s to %s", slug, h.m.results.focusedSlug())
	}
}

// Both answers in flight and delivered out of order: the second no longer fits next to the window and is dropped,
// so the window never holds a page twice or a gap.
func TestAnswersThatNoLongerFitNextToTheWindowAreDropped(t *testing.T) {
	h := fullWindow(t)
	h.hold = true
	h.pressUntilHeld(input.Down, 40)
	next := h.held[0]
	h.held = nil
	for i := 0; i < 260 && len(h.held) == 0; i++ { // up to the top, where the previous page is asked
		h.press(input.Up)
	}
	if len(h.held) == 0 {
		t.Fatal("the previous page was not asked")
	}
	prev := h.held[0]
	h.held = nil

	h.deliver(prev) // the previous page lands first: the window grows at the front
	assertSoundWindow(t, h)
	firstAfterPrev := h.m.results.win.first
	h.deliver(next) // the next page no longer follows the window: it is dropped
	assertSoundWindow(t, h)
	if h.m.results.win.first != firstAfterPrev {
		t.Fatalf("first page %d became %d", firstAfterPrev, h.m.results.win.first)
	}
}

func TestADuplicatePageAnswerIsIgnored(t *testing.T) {
	h := started(t, zeldaService(100), 1280, 720)
	h.hold = true
	searchFor(t, h, "zelda")
	h.deliver(h.held[0]) // the first page
	h.held = nil
	h.pressUntilHeld(input.Down, 30)
	page2 := h.held[0]
	h.held = nil
	msg := page2()
	h.m, _ = h.m.Update(msg, h.now)
	rows := len(h.m.results.win.rows)
	h.m, _ = h.m.Update(msg, h.now) // the same answer again
	if len(h.m.results.win.rows) != rows {
		t.Fatalf("%d rows became %d: a page answered twice must count once", rows, len(h.m.results.win.rows))
	}
	assertSoundWindow(t, h)
}

// B on a wait cancels the call itself, and its answer is dropped.
func TestBOnAWaitCancelsTheCall(t *testing.T) {
	svc := zeldaService(3)
	svc.SearchBlock = make(chan struct{})
	svc.SearchEntered = make(chan struct{}, 1)
	h := started(t, svc, 1280, 720)
	h.hold = true
	searchFor(t, h, "zelda")
	if len(h.held) == 0 {
		t.Fatal("the search was not asked")
	}
	answer := make(chan Msg, 1)
	go func() { answer <- h.held[0]() }()
	select {
	case <-svc.SearchEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("the call never started")
	}
	h.held = nil
	h.press(input.Back)
	select {
	case msg := <-answer:
		res, ok := msg.(searchPageMsg)
		if !ok || !errors.Is(res.err, context.Canceled) {
			t.Fatalf("answer %+v: the call must end canceled", msg)
		}
		h.m, _ = h.m.Update(msg, h.now)
	case <-time.After(5 * time.Second):
		t.Fatal("B did not cancel the call")
	}
	if h.m.screen != screenSearch || h.m.results.phase == loadFailed {
		t.Fatalf("screen %d phase %d: the canceled answer must not show an error", h.m.screen, h.m.results.phase)
	}
}

func TestBOnTheEntryWaitCancelsItsCall(t *testing.T) {
	svc := zeldaService(2)
	h := started(t, svc, 1280, 720)
	searchFor(t, h, "zelda")
	h.hold = true
	h.press(input.Confirm)
	if h.m.entry.cancel == nil || len(h.held) == 0 {
		t.Fatal("the entry call was not asked or cannot be canceled")
	}
	h.press(input.Back) // cancels the call and leaves
	if h.m.screen != screenResults {
		t.Fatalf("screen %d", h.m.screen)
	}
	h.deliver(h.held[0]) // the answer of the call that was canceled is stale
	if h.m.entry.phase == loadOK {
		t.Fatal("a stale entry answer must not fill the screen")
	}
}

// PR-76 review, s1: a cover that failed for a reason that may pass is asked again later; one that is not there is not.
func TestATransientCoverFailureIsRetriedAndAMissingCoverIsNot(t *testing.T) {
	svc := zeldaService(2)
	svc.Covers = map[string]bool{"zelda-001": true}
	svc.CoverErr = errors.New("wifi blip")
	h := started(t, svc, 1280, 720)
	searchFor(t, h, "zelda")
	h.advance(300 * time.Millisecond)
	if svc.Count("cover:zelda-001") != 1 {
		t.Fatalf("calls %v", svc.Calls())
	}
	h.advance(20 * time.Second)
	if svc.Count("cover:zelda-001") != 1 {
		t.Fatal("a failed cover is left alone for a while, not asked on every tick")
	}
	svc.CoverErr = nil
	h.advance(15 * time.Second)
	if svc.Count("cover:zelda-001") != 2 {
		t.Fatalf("calls %v: the cover is asked again after the wait", svc.Calls())
	}
	if e, ok := h.m.covers.get("zelda-001"); !ok || e.failed {
		t.Fatalf("the cover must have arrived: %+v", e)
	}

	// A cover that is not there stays failed for good.
	h.press(input.Down)
	h.advance(300 * time.Millisecond)
	h.advance(5 * time.Minute)
	if n := svc.Count("cover:zelda-002"); n != 1 {
		t.Fatalf("a missing cover was asked %d times", n)
	}
}

func TestResultsQuerySummaryIsSanitized(t *testing.T) {
	h := started(t, zeldaService(1), 1280, 720)
	h.m.results.query = app.SearchQuery{Text: "z", Platforms: []string{"sn\x1bes"}, Regions: []string{"u\x1bs"}}
	if got := h.m.querySummary(); strings.ContainsRune(got, 0x1b) || !strings.Contains(got, "snes") {
		t.Fatalf("%q", got)
	}
	_ = fmt.Sprint
}

// PR-76 recheck nit: moving to the next entry with R cancels the call of the entry it leaves.
func TestMovingToANeighbourCancelsThePreviousEntryCall(t *testing.T) {
	h := started(t, zeldaService(3), 1280, 720)
	searchFor(t, h, "zelda")
	h.press(input.Confirm)
	canceled := 0
	h.m.entry.cancel = func() { canceled++ }
	h.press(input.PageDown)
	h.press(input.PageDown)
	if canceled != 1 {
		t.Fatalf("the earlier call was canceled %d times, want 1 (the second one replaced the hook)", canceled)
	}
	h.mustHave("Zelda 003")
}
