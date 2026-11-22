package gui

import (
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/guitest"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
)

// The rules of docs/ux/gui.md v1.1 from the PR-71 UX review (Bússola): the top bar keeps its title, no string is
// cut except titles, toasts stay off the focus, and so on.

func withLocale(loc string) harnessOpt {
	return func(o *Options) { o.T = shared.bundle.New(loc) }
}

// pendingQuit leaves the harness with Quit when finished chosen, a download running and, if offline is set,
// the catalog down: the worst case of the top bar.
func pendingQuit(t *testing.T, w, h int, offline bool, opts ...harnessOpt) *harness {
	t.Helper()
	svc := guitest.New()
	svc.JobList = []app.JobSnapshot{activeJob("1"), queuedJob("2")}
	if offline {
		svc.InfoErr = &app.Error{Code: app.CodeNetwork, Message: "no route"}
	}
	hn := started(t, svc, w, h, opts...)
	hn.press(input.Back)
	hn.advance(200 * time.Millisecond)
	hn.press(input.Right)
	if hn.m.dlgFocus != btnWhenFinished {
		hn.press(input.Down) // the buttons are stacked when the row does not fit
	}
	if hn.m.dlgFocus != btnWhenFinished {
		t.Fatalf("dialog focus %q", hn.m.dlgFocus)
	}
	hn.press(input.Confirm)
	if !hn.m.quitPending {
		t.Fatal("quit is not pending")
	}
	hn.press(input.Back) // Quit when finished opened the Queue: back to Home
	return hn
}

// openScreen opens one of the screens that have a dim extra in the top bar (or Home, which has none).
func openScreen(h *harness, name string) {
	switch name {
	case "history":
		h.press(input.Menu)
		h.press(input.Down)
		h.press(input.Down)
		h.press(input.Confirm)
	case "settings":
		h.press(input.Menu)
		for i := 0; i < 3; i++ {
			h.press(input.Down)
		}
		h.press(input.Confirm)
	case "results":
		h.press(input.Confirm) // Search
		h.send(TextMsg{Text: "zelda"})
		h.press(input.Menu)
	}
}

// M1 and the T-80 order: with Offline, No controller, a pending quit and the queue badge, in a long language and large
// text at 640x480, the title is whole on every screen, Offline and No controller are never cut, and nothing leaves the
// safe rectangle. When the bar does not fit, items are hidden in the order of docs/ux/gui.md 4: the pending-quit notice,
// the screen's dim extra, then the queue badge; nothing is cut with an ellipsis.
func TestTopBarKeepsTheTitleAndNeverCutsOfflineOrTheBadge(t *testing.T) {
	for _, loc := range []string{"en_us", "pt_br", "es_es", "ru_ru"} {
		for _, size := range [][2]int{{640, 480}, {854, 480}, {1280, 720}} {
			for _, large := range []bool{false, true} {
				for _, screen := range []string{"home", "history", "settings", "results"} {
					h := pendingQuit(t, size[0], size[1], true, withLocale(loc), func(o *Options) { o.LargeText = large })
					h.send(PadsMsg{}) // no controller: its badge joins Offline, the pending quit and the queue badge
					openScreen(h, screen)
					texts := h.texts()
					where := loc + " " + screen + " " + strconv.Itoa(size[0]) + "x" + strconv.Itoa(size[1]) + " large=" + strconv.FormatBool(large)
					offline := h.m.tr("ui.gui.bar.offline", nil)
					noPad := h.m.tr("ui.gui.bar.no_pad", nil)
					badge := h.m.tr("ui.gui.bar.badge_queued", map[string]any{"active": 1, "queued": 1})
					for _, want := range []string{offline, noPad} {
						if !slices.Contains(texts, want) {
							t.Errorf("%s: %q is not drawn whole in %q", where, want, texts)
						}
					}
					// The screen name (the last crumb) is always whole; the brand and the crumbs before it may drop behind an
					// ellipsis (docs/ux/gui.md 4). Home has only the brand.
					last := map[string]string{"home": "mothdex", "history": h.m.tr("ui.gui.screen.history", nil), "settings": h.m.tr("ui.gui.screen.settings", nil), "results": `"zelda"`}[screen]
					found := false
					for _, txt := range texts {
						if (strings.HasPrefix(txt, "mothdex") || strings.HasPrefix(txt, "…")) && strings.HasSuffix(txt, last) {
							found = true
						}
					}
					if !found {
						t.Errorf("%s: the screen name %q is not whole in the title: %q", where, last, texts)
					}
					notice, extra := false, false
					for _, txt := range texts {
						if txt == h.m.tr("ui.gui.bar.will_quit", nil) || txt == h.m.tr("ui.gui.bar.will_quit_short", nil) {
							notice = true
						}
						if strings.HasSuffix(txt, "…") && strings.HasPrefix(h.m.tr("ui.gui.bar.will_quit_short", nil), strings.TrimSuffix(txt, "…")) {
							t.Errorf("%s: the pending-quit notice is cut (%q): it is hidden instead", where, txt)
						}
						if screen == "history" && strings.Contains(txt, " · ") && strings.Contains(txt, "0") && txt != badge {
							extra = true
						}
					}
					hasBadge := slices.Contains(texts, badge)
					switch {
					case notice && !extra && screen == "history":
						t.Errorf("%s: the extra went before the pending-quit notice, against the order: %q", where, texts)
					case notice && !hasBadge, extra && !hasBadge:
						t.Errorf("%s: the badge went before the notice or the extra, against the order: %q", where, texts)
					}
					assertInsideSafe(t, h, where)
				}
			}
		}
	}
}

// assertInsideSafe fails for any rectangle, text or spinner of the frame outside the safe rectangle.
func assertInsideSafe(t *testing.T, h *harness, what string) {
	t.Helper()
	met := h.m.Metrics()
	for _, op := range h.render().Ops {
		switch op.Kind {
		case draw.KindRect:
			if full := op.Rect == (draw.Rect{W: met.W, H: met.H}); !full && !op.Rect.Within(met.Safe) {
				t.Errorf("%s %dx%d: rect %+v outside %+v", what, met.DisplayW, met.DisplayH, op.Rect, met.Safe)
			}
		case draw.KindText:
			w, ascent, descent := inkOf(op)
			if op.X < met.Safe.X || op.X+w > met.Safe.Right() || op.Base-ascent < met.Safe.Y || op.Base+descent > met.Safe.Bottom() {
				t.Errorf("%s %dx%d: text %q at %d,%d (%d px) outside %+v", what, met.DisplayW, met.DisplayH, op.Text, op.X, op.Base, w, met.Safe)
			}
		}
	}
}

// With room, the quit notice keeps its full text; the short key is only the fallback.
func TestPendingQuitKeepsItsFullTextWhenItFits(t *testing.T) {
	h := pendingQuit(t, 1920, 1080, false)
	h.mustHave("Will quit when downloads finish")
	h.mustNotHave("Quit pending")
	tight := pendingQuit(t, 640, 480, true, withLocale("ru_ru"))
	tight.mustHave("Ожидается выход") // the short form, when the full one does not fit
	tight.mustNotHave("Выход после завершения загрузок")
	// Where even the short form does not fit, the notice is hidden (not cut); Offline stays whole, and the Start menu keeps
	// Don't quit, so the state is still reachable (docs/ux/gui.md 4, Bússola in T-80).
	cut := pendingQuit(t, 640, 480, true, withLocale("ru_ru"), func(o *Options) { o.LargeText = true })
	cut.mustHave("Нет сети")
	cut.mustNotHave("Ожидается…", "Ожидается выход")
}

// m3: a toast takes at most 60 % of the safe width and wraps to two lines; the language notice stays 6 s and
// the others 3 s; and the toast never covers the focused element (section 3).
func TestToastIsNarrowWrappedAndStaysOffTheFocus(t *testing.T) {
	h := started(t, guitest.New(), 640, 480, func(o *Options) { o.LocaleFallback = true })
	const notice = "This language is not available in this screen mode yet. Showing English."
	lines := h.m.toastLines(h.m.painter(nil))
	if len(lines) != 2 || strings.Join(lines, " ") != notice {
		t.Fatalf("lines %q: the notice wraps to two whole lines", lines)
	}
	r := h.m.toastRect(h.m.painter(nil), lines)
	met := h.m.Metrics()
	if r.W > met.Safe.W*60/100+2*met.Unit || !r.Within(met.Safe) {
		t.Errorf("toast %+v is wider than 60 %% of %+v or outside it", r, met.Safe)
	}
	focus, _ := h.m.focusRect()
	if intersects(r, focus.Inset(-met.Ring)) {
		t.Errorf("the toast %+v covers the focus %+v", r, focus)
	}
	h.mustHave(lines...)
	h.advance(5900 * time.Millisecond)
	h.mustHave(lines...) // the language notice stays 6 s
	h.advance(200 * time.Millisecond)
	h.mustNotHave(lines...)

	short := started(t, guitest.New(), 1280, 720)
	short.press(input.Confirm)
	short.advance(3100 * time.Millisecond)
	short.mustNotHave("Search is not available yet.") // other toasts stay 3 s
}

// A toast that would sit on the focused tile goes above the hint bar instead.
func TestAToastMovesAwayFromAFocusItWouldCover(t *testing.T) {
	h := started(t, guitest.New(), 640, 480)
	for i := 0; i < 3; i++ {
		h.press(input.Right)
	}
	h.press(input.Confirm)
	met := h.m.Metrics()
	focus, _ := h.m.focusRect()
	r := h.m.toastRect(h.m.painter(nil), h.m.toastLines(h.m.painter(nil)))
	if intersects(r, focus.Inset(-met.Ring)) {
		t.Fatalf("toast %+v covers the focus %+v", r, focus)
	}
}

func TestShortenPathKeepsTheFirstAndTheLastFolder(t *testing.T) {
	h := started(t, guitest.New(), 1280, 720)
	p := h.m.painter(nil)
	size := h.m.Metrics().Secondary
	const path = "/home/someone/Games/retro/emulation/collection"
	if got := shortenPath(p, size, path, 4000); got != path {
		t.Errorf("a path that fits is untouched: %q", got)
	}
	full := p.width(size, path)
	for _, w := range []int{full - 10, full * 2 / 3, full / 2, p.width(size, "/home/…/collection") + 1} {
		got := shortenPath(p, size, path, w)
		if p.width(size, got) > w {
			t.Errorf("width %d: %q is %d px", w, got, p.width(size, got))
		}
		if !strings.HasSuffix(got, "/collection") || !strings.HasPrefix(got, "/home") || !strings.Contains(got, "…") {
			t.Errorf("width %d: %q must keep the first and the last folder", w, got)
		}
	}
	if got := shortenPath(p, size, path, p.width(size, "/…/collection")); !strings.HasSuffix(got, "collection") {
		t.Errorf("the last folder is whole when it can be: %q", got)
	}
	win := `C:\Users\someone\Games\retro\collection`
	if got := shortenPath(p, size, win, p.width(size, win)*2/3); !strings.HasPrefix(got, `C:\`) || !strings.HasSuffix(got, `\collection`) {
		t.Errorf("a Windows path: %q", got)
	}
	if got := shortenPath(p, size, "/a", 1); got == "" {
		t.Error("a tiny width still gives something")
	}
}

// m1: the generic profile's footer names the folder; it wraps to two lines, and the folder is kept.
func TestTheGenericFooterKeepsTheFolderAtSixHundredAndForty(t *testing.T) {
	svc := guitest.New()
	svc.Profile = app.ProfileInfo{ID: "generic", Name: "Generic", RomRoot: "/home/someone/Games/retro/emulation/collection"}
	for _, loc := range []string{"en_us", "pt_br"} {
		for _, large := range []bool{false, true} {
			h := started(t, svc, 640, 480, withLocale(loc), func(o *Options) { o.LargeText = large })
			var footer []string
			for _, txt := range h.texts() {
				if strings.Contains(txt, "collection") || strings.Contains(txt, "…") {
					footer = append(footer, txt)
				}
			}
			joined := strings.Join(footer, " ")
			if !strings.Contains(joined, "collection") {
				t.Errorf("%s large=%v: the folder was lost: %q", loc, large, footer)
			}
			if len(footer) > 2 {
				t.Errorf("%s large=%v: footer takes %d lines", loc, large, len(footer))
			}
			assertInsideSafe(t, h, loc+" footer")
		}
	}
}

// m2: a tile label wraps at its spaces before it is cut, and a single long word is never split in the middle:
// it drops to a smaller size instead.
func TestTileLabelsWrapBeforeTheyAreCutAndWordsAreNeverSplit(t *testing.T) {
	h := started(t, guitest.New(), 640, 480, withLocale("pt_br"), func(o *Options) { o.LargeText = true })
	if !slices.Contains(h.texts(), "Configurações") {
		t.Errorf("the Settings label must be drawn whole on one line: %q", h.texts())
	}
	for _, op := range h.render().Ops {
		if op.Kind == draw.KindText && op.Text == "Configurações" && op.Size >= h.m.Metrics().Body {
			t.Errorf("a word wider than the tile must drop below the body size, drawn at %d", op.Size)
		}
	}
	for _, txt := range h.texts() {
		if strings.Contains(txt, "…") && !strings.Contains(txt, "Catálogo") {
			t.Errorf("%q is cut with an ellipsis", txt)
		}
	}
	assertInsideSafe(t, h, "tiles")

	// A label with spaces wraps to two lines at its spaces, in the display face, and never in the middle of a word.
	_, size, lines := tileLabel(h.m.painter(nil), "Game Boy", 90)
	if len(lines) != 2 || size != h.m.Metrics().Display || !wrappedWhole(lines, "Game Boy") {
		t.Errorf("a label with spaces wraps to two whole lines at the display size: %d %q", size, lines)
	}
	// A word wider than the tile drops to the body face, whole, and is cut only when even the minimum does not fit.
	lp, size, lines := tileLabel(h.m.painter(nil), "Configurações", 80)
	if lp.disp || size >= h.m.Metrics().Display || strings.Contains(strings.Join(lines, ""), "…") && size > h.m.Metrics().Minimum {
		t.Errorf("a long word falls back to the body face before it is cut: disp=%v size=%d %q", lp.disp, size, lines)
	}
}

// s1: stacked dialog buttons all take the width of the panel's inner area.
func TestStackedDialogButtonsAreAsWideAsThePanel(t *testing.T) {
	svc := guitest.New()
	svc.JobList = []app.JobSnapshot{activeJob("1")}
	h := started(t, svc, 640, 480, func(o *Options) { o.LargeText = true; o.Safe = SafeArea{Percent: 10} })
	h.press(input.Back)
	g := h.m.dialogGeometry()
	if g.buttons[0].rect.Y == g.buttons[2].rect.Y {
		t.Skip("the buttons fit in a row here")
	}
	for _, b := range g.buttons {
		if b.rect.W != g.buttons[0].rect.W || b.rect.W != g.panel.W-4*h.m.Metrics().Unit {
			t.Errorf("button %+v is not as wide as the inner panel %d", b.rect, g.panel.W-4*h.m.Metrics().Unit)
		}
	}
}

// s2: a hint with a word label has 1u of air at each end; a one-letter label stays a disc.
func TestHintPillsHaveAirAroundWords(t *testing.T) {
	h := started(t, guitest.New(), 1280, 720)
	p := h.m.painter(nil)
	met := h.m.Metrics()
	hh := met.HintBar - met.Unit
	if got, want := p.pillWidth(hh, "Cross"), p.width(met.Secondary, "Cross")+2*met.Unit; got != want {
		t.Errorf("Cross pill %d, want %d", got, want)
	}
	if got := p.pillWidth(hh, "A"); got != hh {
		t.Errorf("a letter is a disc of %d, got %d", hh, got)
	}
}

// s3: the spinner is on the right of the text on the Splash, as on the exit screen.
func TestTheSplashSpinnerIsOnTheRightOfTheText(t *testing.T) {
	h := newHarness(t, guitest.New(), 1280, 720)
	h.now = h.now.Add(400 * time.Millisecond)
	var textRight, spinnerX int
	for _, op := range h.render().Ops {
		if op.Kind == draw.KindText && op.Text == "Starting…" {
			face, _ := shared.font.Face(op.Size)
			textRight = op.X + face.Width(op.Text)
		}
		if op.Kind == draw.KindSpinner {
			spinnerX = op.CX
		}
	}
	if textRight == 0 || spinnerX <= textRight {
		t.Fatalf("spinner at %d, text ends at %d: the spinner goes on the right", spinnerX, textRight)
	}
}

// s4: during the 4 s refresh notice the exit screen shows one hint, (A) Continue.
func TestTheExitNoticeShowsAContinueHint(t *testing.T) {
	svc := guitest.New()
	svc.Batch = app.BatchResult{Done: 1, RefreshWarning: "Refresh the game list from the frontend menu."}
	h := started(t, svc, 1280, 720)
	h.press(input.Menu)
	for i := 0; i < 5; i++ {
		h.press(input.Down)
	}
	h.press(input.Confirm)
	h.mustHave("Continue")
	i := slices.Index(h.texts(), "Continue")
	if h.texts()[i-1] != "A" {
		t.Errorf("the hint is (A) Continue, got %q", h.texts()[i-1])
	}
	plain := started(t, guitest.New(), 1280, 720)
	plain.send(SignalMsg{})
	plain.mustNotHave("Continue")
	assertInsideSafe(t, h, "exit notice")
}
