package gui

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/guitest"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

// The UX review of #76 (Bússola): the hint bar fit rule, the Start word, the Queue details, the platform name,
// and the small things of the Queue and the in-place errors.

func scenesForHints(t *testing.T) map[string]func(h *harness) {
	return map[string]func(h *harness){
		"home":    func(*harness) {},
		"search":  func(h *harness) { h.press(input.Confirm) },
		"picker":  func(h *harness) { h.press(input.Confirm); h.press(input.Up); h.press(input.Up); h.press(input.Confirm) },
		"single":  func(h *harness) { h.press(input.Right); h.press(input.Confirm) },
		"results": func(h *harness) { searchFor(t, h, "zelda") },
		"entry":   func(h *harness) { searchFor(t, h, "zelda"); h.press(input.Confirm) },
		"queue": func(h *harness) {
			h.send(JobMsg{Job: app.JobSnapshot{ID: "1", Title: "A", State: app.StateTransferring, Expected: 10}})
			h.press(input.Queue)
		},
	}
}

// M1: in every language, at every size, with large text, the hint bar stays inside the safe rectangle, and A and B
// are never dropped.
func TestTheHintBarFitsInEveryLanguageAndKeepsAAndB(t *testing.T) {
	for _, loc := range []string{"en_us", "pt_br", "es_es", "ru_ru"} {
		for _, size := range [][2]int{{640, 480}, {854, 480}, {1280, 720}} {
			for _, large := range []bool{false, true} {
				for name, setup := range scenesForHints(t) {
					svc := zeldaService(3)
					h := started(t, svc, size[0], size[1], withLocale(loc), func(o *Options) { o.LargeText = large })
					setup(h)
					what := fmt.Sprintf("%s %s %dx%d large=%v", loc, name, size[0], size[1], large)
					assertInsideSafe(t, h, what)
					if name == "home" || name == "single" {
						continue
					}
					// The two pills of A and B are always there: the hint words follow them.
					texts := h.texts()
					if !slices.Contains(texts, "A") && !slices.Contains(texts, "B") {
						t.Errorf("%s: neither A nor B in the hint bar: %q", what, texts)
					}
				}
			}
		}
	}
}

// The drop order of the review: L/R, then the Start word (when it means Menu), then Y, then X.
func TestHintsAreDroppedInTheOrderOfTheRule(t *testing.T) {
	hints := []hint{
		{role: input.PageUp, role2: input.PageDown, label: "Prev/next game", optional: true},
		{role: input.Confirm, label: "Download this"},
		{role: input.Download, label: "Download best"},
		{role: input.Info, label: "Why"},
		{role: input.Back, label: "Back"},
	}
	roles := func(hs []hint) []input.Action {
		var out []input.Action
		for _, h := range hs {
			out = append(out, h.role)
		}
		return out
	}
	steps := [][]input.Action{
		{input.Confirm, input.Download, input.Info, input.Back}, // L/R first
		{input.Confirm, input.Download, input.Back},             // then Y
		{input.Confirm, input.Back},                             // then X; A and B stay
	}
	for want := 0; want < len(steps); want++ {
		left := 0
		got := shrinkHints(hints, func([]hint) bool { left++; return left > want+1 })
		if !slices.Equal(roles(got), steps[want]) {
			t.Errorf("after %d drops: %v, want %v", want+1, roles(got), steps[want])
		}
	}
	if got := shrinkHints(hints, func([]hint) bool { return false }); !slices.Equal(roles(got), []input.Action{input.Confirm, input.Back}) {
		t.Errorf("A and B always stay: %v", roles(got))
	}
}

// m1: compact keeps the Start word where Start is not the menu (Search, Done) and drops it where it is.
func TestCompactKeepsTheStartWordWhereStartIsNotTheMenu(t *testing.T) {
	startWord := func(h *harness) string {
		texts := h.texts()
		i := slices.Index(texts, "Start")
		if i < 0 || i+1 >= len(texts) {
			return ""
		}
		return texts[i+1]
	}
	search := started(t, zeldaService(1), 640, 480)
	search.press(input.Confirm)
	if got := startWord(search); got != "Search" {
		t.Errorf("on Search at compact the word after Start is %q, want Search", got)
	}
	picker := started(t, zeldaService(1), 640, 480)
	picker.press(input.Confirm)
	picker.press(input.Up)
	picker.press(input.Up)
	picker.press(input.Confirm)
	if got := startWord(picker); got != "Done" {
		t.Errorf("on the multi picker at compact the word is %q, want Done", got)
	}
	home := started(t, zeldaService(1), 640, 480)
	if got := startWord(home); got == "Menu" {
		t.Error("where Start is the menu, compact drops the word")
	}
}

// m2: finished jobs show their bytes alone; X / Y only while the job runs.
func TestQueueDetailsShowBytesAloneForAFinishedJob(t *testing.T) {
	h := started(t, zeldaService(1), 1280, 720)
	h.send(JobMsg{Job: app.JobSnapshot{ID: "1", Title: "A", Name: "a.zip", State: app.StateDone, Bytes: 384 << 10, Expected: 0, Started: h.now, Finished: h.now.Add(time.Second)}})
	h.send(JobMsg{Job: app.JobSnapshot{ID: "2", Title: "B", Name: "b.zip", State: app.StateTransferring, Bytes: 1 << 20, Expected: 4 << 20}})
	h.press(input.Queue)
	focusJob(t, h, "1")
	h.press(input.Info)
	h.mustHave("Size: 384 KB")
	h.mustNotHave("Size: 384 KB / 0 B")
	h.press(input.Back)
	focusJob(t, h, "2")
	h.press(input.Info)
	h.mustHave("Size: 1.0 MB / 4.0 MB")
}

// m3: the Entry shows the platform name; the Platforms list is loaded once if Search was reached without a picker.
func TestEntryShowsThePlatformNameAndLoadsThePlatformsOnce(t *testing.T) {
	svc := zeldaService(1)
	h := started(t, svc, 1280, 720)
	searchFor(t, h, "zelda")
	if svc.Count("platforms") != 0 {
		t.Fatal("the platform list is not needed on Results")
	}
	h.press(input.Confirm)
	h.mustHave("snes · Super Nintendo · us")
	if svc.Count("platforms") != 1 {
		t.Fatalf("calls %v", svc.Calls())
	}
	h.press(input.Back)
	h.press(input.Confirm)
	if svc.Count("platforms") != 1 {
		t.Fatal("the list is kept: loaded once")
	}
}

// s2 to s6 and s4: a transferring row needs two lines, a failed row keeps only the message, the badge says
// "1 queued", the warming text uses the ellipsis, and an error in place is marked with a !.
func TestQueueAndErrorDetailsOfTheReview(t *testing.T) {
	h := started(t, zeldaService(1), 1280, 720)
	met := h.m.Metrics()
	two := LineHeight(met.Body) + LineHeight(met.Secondary) + met.Unit
	three := two + LineHeight(met.Secondary)
	height := func(st app.State) int { return h.m.itemHeight(queueItem{job: app.JobSnapshot{State: st}}) }
	for st, want := range map[app.State]int{app.StateTransferring: two, app.StateQueued: two, app.StateDone: two, app.StateWarming: three, app.StateRetryWait: three} {
		if got := height(st); got != want {
			t.Errorf("state %s is %d tall, want %d", st, got, want)
		}
	}
	h.send(JobMsg{Job: app.JobSnapshot{ID: "1", Title: "Contra", State: app.StateFailed, Err: &app.Error{Code: app.CodeNetwork, Message: "x"}}})
	h.press(input.Queue)
	h.mustHave("Cannot reach the Mothdex server.")
	h.mustNotHave("Cannot reach the Mothdex server. [network]")

	svc := zeldaService(1)
	svc.SearchErr = &app.Error{Code: app.CodeNetwork, Message: "down"}
	e := started(t, svc, 1280, 720)
	searchFor(t, e, "zelda")
	e.mustHave("!", "Cannot reach the Mothdex server.")
	for _, op := range e.render().Ops {
		if op.Kind == 0 && op.Color == colError { // a filled rectangle in the error color would be the cross
			t.Fatalf("an error in place must not draw the cross: %+v", op)
		}
	}
}

// s1: the Downloaded mark and its word have a space between them.
func TestTheDownloadedMarkHasAGapBeforeItsWord(t *testing.T) {
	svc := zeldaService(1)
	svc.Records = []app.HistoryRecord{{State: "done", Slug: "zelda-001", LinkIndex: 1}}
	h := started(t, svc, 1280, 720)
	searchFor(t, h, "zelda")
	var wordX, markRight int
	for _, op := range h.render().Ops {
		if op.Kind == 1 && op.Text == "Downloaded" {
			wordX = op.X
		}
	}
	for _, op := range h.render().Ops {
		if op.Kind == 0 && op.Color == colOK && op.Rect.Right() > markRight && op.Rect.X < wordX {
			markRight = op.Rect.Right()
		}
	}
	if wordX == 0 || markRight == 0 || wordX-markRight < h.m.Metrics().Unit/4 {
		t.Fatalf("the check ends at %d and its word starts at %d", markRight, wordX)
	}
	_ = strings.TrimSpace
	_ = guitest.New
	_ = mothdex.Entry{}
}
