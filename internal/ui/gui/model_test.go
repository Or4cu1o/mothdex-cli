package gui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/assets"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/guitest"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/text"
	"github.com/Or4cu1o/mothdex-cli/locales"
)

var shared struct {
	once   sync.Once
	bundle *i18n.Bundle
	font   *text.Font
	err    error
}

var epoch = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)

// harness drives the model like the run loop does: messages in, Cmds run at once.
type harness struct {
	t       *testing.T
	m       Model
	now     time.Time
	svc     *guitest.Fake
	pending []Cmd // what New asked for, until run
	hold    bool  // keep the Cmds the model returns instead of running them, to deliver their answers by hand
	held    []Cmd
}

type harnessOpt func(*Options)

func newHarness(t *testing.T, svc *guitest.Fake, w, h int, opts ...harnessOpt) *harness {
	t.Helper()
	shared.once.Do(func() {
		shared.bundle, shared.err = i18n.Load(locales.FS)
		if shared.err == nil {
			shared.font, shared.err = text.Parse(assets.NotoSans)
		}
	})
	if shared.err != nil {
		t.Fatal(shared.err)
	}
	o := Options{
		Service: svc, T: shared.bundle.New("en_us"), Font: shared.font, Clock: &fakeClock{now: epoch},
		DataDir: t.TempDir(), Version: "0.3.0", Width: w, Height: h, Safe: AutoSafeArea,
	}
	for _, opt := range opts {
		opt(&o)
	}
	m, cmds := New(o, epoch)
	return &harness{t: t, m: m, now: epoch, svc: svc, pending: cmds}
}

// started is a harness past the Splash: the service answered and Home is showing.
func started(t *testing.T, svc *guitest.Fake, w, h int, opts ...harnessOpt) *harness {
	t.Helper()
	hn := newHarness(t, svc, w, h, opts...)
	hn.run(hn.pending)
	hn.pending = nil
	if hn.m.screen != screenHome {
		t.Fatalf("not on Home after the service answered: screen %d", hn.m.screen)
	}
	return hn
}

func (h *harness) run(cmds []Cmd) {
	h.t.Helper()
	if h.hold {
		h.held = append(h.held, cmds...)
		return
	}
	for len(cmds) > 0 {
		next := cmds[0]
		cmds = cmds[1:]
		var more []Cmd
		h.m, more = h.m.Update(next(), h.now)
		cmds = append(cmds, more...)
	}
}

func (h *harness) send(msg Msg) {
	h.t.Helper()
	var cmds []Cmd
	h.m, cmds = h.m.Update(msg, h.now)
	h.run(cmds)
}

func (h *harness) press(a input.Action) { h.t.Helper(); h.send(InputMsg{Action: a}) }

// advance moves the clock and lets the timers fire.
func (h *harness) advance(d time.Duration) {
	h.t.Helper()
	h.now = h.now.Add(d)
	h.send(TickMsg{})
}

func (h *harness) render() *draw.List {
	var l draw.List
	h.m.Render(h.now, &l)
	return &l
}

// texts are the text runs of the frame. The top bar draws the wordmark in the moth color and the crumbs after it in the
// text color (two runs); here they are joined again, so a test reads the title as one string.
func (h *harness) texts() []string {
	runs := h.render().Texts()
	out := make([]string, 0, len(runs))
	for i := 0; i < len(runs); i++ {
		if runs[i] == brand && i+1 < len(runs) && strings.HasPrefix(runs[i+1], crumb) {
			out = append(out, brand+runs[i+1])
			i++
			continue
		}
		out = append(out, runs[i])
	}
	return out
}

func (h *harness) mustHave(s ...string) {
	h.t.Helper()
	texts := h.texts()
	for _, want := range s {
		if !slices.Contains(texts, want) {
			h.t.Errorf("no text run %q in %q", want, texts)
		}
	}
}

func (h *harness) mustNotHave(s ...string) {
	h.t.Helper()
	texts := h.texts()
	for _, no := range s {
		if slices.Contains(texts, no) {
			h.t.Errorf("unexpected text run %q in %q", no, texts)
		}
	}
}

func exitCalls(svc *guitest.Fake) []string { return svc.ExitCalls() }

func activeJob(id string) app.JobSnapshot {
	return app.JobSnapshot{ID: app.JobID(id), Title: "Contra (USA)", State: app.StateTransferring}
}

func queuedJob(id string) app.JobSnapshot {
	return app.JobSnapshot{ID: app.JobID(id), Title: "Zelda", State: app.StateQueued}
}

// Splash, then Home when DBInfo answers (docs/ux/gui.md section 5.1 and 5.2).
func TestSplashShowsNameAndVersionThenHomeWhenDBInfoAnswers(t *testing.T) {
	h := newHarness(t, guitest.New(), 1280, 720)
	h.mustHave("mothdex", "Starting…", "0.3.0 · Batocera")
	h.mustNotHave("Search")
	if h.render().Ops[len(h.render().Ops)-1].Kind == draw.KindSpinner {
		t.Error("the spinner must not appear before 200 ms")
	}
	h.run(h.pending)
	h.mustHave("Search", "Platforms", "Random", "Queue", "History", "Settings", "1,284,019 entries · Batocera · /userdata/roms")
	h.mustNotHave("Starting…")
	if h.m.focus != tileSearch {
		t.Errorf("initial focus %q, want Search", h.m.focus)
	}
}

func TestSplashSpinnerAppearsAfter200ms(t *testing.T) {
	h := newHarness(t, guitest.New(), 1280, 720)
	spinners := func() int {
		n := 0
		for _, op := range h.render().Ops {
			if op.Kind == draw.KindSpinner {
				n++
			}
		}
		return n
	}
	if spinners() != 0 {
		t.Fatal("a spinner on the first frame")
	}
	h.now = h.now.Add(250 * time.Millisecond)
	if spinners() != 1 {
		t.Fatal("no spinner after 200 ms")
	}
}

// Home opens after 3 s even when DBInfo is slow, and a late answer fills the footer.
func TestHomeOpensAfterThreeSecondsAndALateDBInfoFillsTheFooter(t *testing.T) {
	h := newHarness(t, guitest.New(), 1280, 720)
	h.advance(2900 * time.Millisecond)
	if h.m.screen != screenSplash {
		t.Fatal("Home opened before 3 s")
	}
	h.advance(200 * time.Millisecond)
	if h.m.screen != screenHome {
		t.Fatal("Home did not open at 3 s")
	}
	h.mustHave("Batocera · /userdata/roms")
	h.run(h.pending) // the answer arrives late
	h.mustHave("1,284,019 entries · Batocera · /userdata/roms")
}

func TestOfflineCatalogShowsOfflineAndTheFooterNotice(t *testing.T) {
	svc := guitest.New()
	svc.InfoErr = &app.Error{Code: app.CodeNetwork, Message: "no route"}
	h := started(t, svc, 1280, 720)
	h.mustHave("Offline", "Catalog unavailable. Cached searches still work.")
	h.mustNotHave("1,284,019 entries · Batocera · /userdata/roms")
}

func TestGenericProfileFooterNamesTheFolderInWarnColor(t *testing.T) {
	svc := guitest.New()
	svc.Profile = app.ProfileInfo{ID: "generic", Name: "Generic", RomRoot: "/home/me/roms"}
	h := started(t, svc, 1280, 720)
	h.mustHave("No emulation system detected. Games go to /home/me/roms.")
	for _, op := range h.render().Ops {
		if op.Kind == draw.KindText && strings.HasPrefix(op.Text, "No emulation") && op.Color != colWarn {
			t.Error("the generic notice must use the warn color")
		}
	}
}

// Focus moves by geometry: Search, Platforms, Random in a wrapping row, then down to Queue.
func TestHomeFocusMovesByGeometryAndRowsWrap(t *testing.T) {
	h := started(t, guitest.New(), 1280, 720)
	steps := []struct {
		a    input.Action
		want string
	}{
		{input.Right, tilePlatforms}, {input.Right, tileRandom}, {input.Right, tileSearch}, // wraps
		{input.Left, tileRandom}, {input.Down, tileSettings}, {input.Down, tileSettings}, // lists do not wrap
		{input.Left, tileHistory}, {input.Up, tilePlatforms},
	}
	for _, s := range steps {
		h.press(s.a)
		if h.m.focus != s.want {
			t.Fatalf("after %d focus is %q, want %q", s.a, h.m.focus, s.want)
		}
	}
}

func TestNarrowHomeIsTwoByThree(t *testing.T) {
	h := started(t, guitest.New(), 640, 480)
	g := h.m.homeGeometry()
	col := func(id string) int {
		for _, tl := range g.tiles {
			if tl.id == id {
				return tl.rect.X
			}
		}
		return -1
	}
	if col(tilePlatforms) <= col(tileSearch) || col(tileRandom) != col(tileSearch) {
		t.Fatalf("2 columns expected: search x=%d platforms x=%d random x=%d", col(tileSearch), col(tilePlatforms), col(tileRandom))
	}
	h.press(input.Down)
	h.press(input.Down)
	if h.m.focus != tileHistory {
		t.Errorf("two rows down in a two-column grid is History, got %q", h.m.focus)
	}
}

// A toast ends after 3 s (a failed download raises one while the person is on Home).
func TestAToastEndsAfterThreeSeconds(t *testing.T) {
	h := started(t, guitest.New(), 1280, 720)
	h.send(JobMsg{Job: app.JobSnapshot{ID: "1", Title: "Contra (USA)", State: app.StateFailed, Err: &app.Error{Code: app.CodeNetwork, Message: "down"}}})
	h.mustHave("Download failed: Contra (USA)")
	h.advance(2900 * time.Millisecond)
	h.mustHave("Download failed: Contra (USA)")
	h.advance(200 * time.Millisecond)
	h.mustNotHave("Download failed: Contra (USA)")
}

func TestRecentSearchesComeFromTheSharedStateFile(t *testing.T) {
	dir := t.TempDir()
	body, _ := json.Marshal(map[string][]string{"recent": {"zelda", "metroid", "chrono trigger", "", "a\x1b[31mb"}})
	if err := os.WriteFile(filepath.Join(dir, "tui_state"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	h := started(t, guitest.New(), 1280, 720, func(o *Options) { o.DataDir = dir })
	h.mustHave("Recent", "zelda", "metroid", "chrono trigger", "a[31mb")
	for _, txt := range h.texts() {
		if strings.ContainsRune(txt, 0x1b) {
			t.Errorf("a control character reached the draw list: %q", txt)
		}
	}
	h.press(input.Down)
	h.press(input.Down)
	if !strings.HasPrefix(h.m.focus, chipPrefix) {
		t.Fatalf("two rows down from Search is a chip, got %q", h.m.focus)
	}
	h.press(input.Confirm) // a recent search runs again with no filters
	if h.m.screen != screenResults || h.m.results.query.Text != "zelda" || len(h.m.results.query.Platforms) != 0 {
		t.Fatalf("screen %d, query %+v: the chip must open Results for its text", h.m.screen, h.m.results.query)
	}

	empty := started(t, guitest.New(), 1280, 720)
	empty.mustNotHave("Recent")
}

// The quit table of section 5.13: B on Home asks, Stay is the default and B equals it.
func TestBackOnHomeAsksAndStayKeepsHome(t *testing.T) {
	h := started(t, guitest.New(), 1280, 720)
	h.press(input.Back)
	h.mustHave("Quit and return to Batocera?", "Stay", "Quit")
	if h.m.dlgFocus != btnStay {
		t.Fatalf("default focus %q, want Stay", h.m.dlgFocus)
	}
	h.press(input.Right) // inside the 150 ms window: ignored
	h.press(input.Confirm)
	if h.m.overlay != overlayDialog || h.m.dlgFocus != btnStay {
		t.Fatal("input right after the dialog opened must be ignored")
	}
	h.advance(200 * time.Millisecond)
	h.press(input.Back)
	if h.m.overlay != overlayNone || h.m.screen != screenHome {
		t.Fatal("B must close the dialog like Stay")
	}
	h.press(input.Back)
	h.advance(200 * time.Millisecond)
	h.press(input.Confirm) // Stay
	if h.m.screen != screenHome || h.m.overlay != overlayNone {
		t.Fatal("Stay must keep Home")
	}
	if len(exitCalls(h.svc)) != 0 {
		t.Fatalf("nothing may be called: %v", exitCalls(h.svc))
	}
}

func TestQuitFromTheDialogShowsTheExitScreenForAtLeastOneSecond(t *testing.T) {
	h := started(t, guitest.New(), 1280, 720)
	h.press(input.Back)
	h.advance(200 * time.Millisecond)
	h.press(input.Right)
	h.press(input.Confirm)
	if h.m.screen != screenExit {
		t.Fatal("Quit must show the exit screen")
	}
	h.mustHave("Returning to Batocera")
	if got := exitCalls(h.svc); len(got) != 2 || got[0] != "wait" || got[1] != "close" {
		t.Fatalf("calls %v, want wait then close", got)
	}
	h.advance(900 * time.Millisecond)
	if h.m.Done() {
		t.Fatal("the exit screen must stay at least 1 s")
	}
	h.advance(150 * time.Millisecond)
	if !h.m.Done() {
		t.Fatal("not done after 1 s")
	}
}

func TestStartMenuQuitWithNothingRunningQuitsWithoutADialog(t *testing.T) {
	h := started(t, guitest.New(), 1280, 720)
	h.press(input.Menu)
	h.mustHave("Home", "Queue", "History", "Settings", "Buttons", "Quit")
	for i := 0; i < 5; i++ {
		h.press(input.Down)
	}
	if h.m.menuFocus != menuQuit {
		t.Fatalf("menu focus %q, want Quit", h.m.menuFocus)
	}
	h.press(input.Confirm)
	if h.m.screen != screenExit || h.m.overlay != overlayNone {
		t.Fatalf("screen %d overlay %d: no dialog was expected", h.m.screen, h.m.overlay)
	}
}

func TestStartMenuOpensHistoryAndClosesItself(t *testing.T) {
	h := started(t, guitest.New(), 1280, 720)
	h.press(input.Menu)
	h.press(input.Down)
	h.press(input.Down) // History
	h.press(input.Confirm)
	if h.m.screen != screenHistory || h.m.overlay != overlayNone {
		t.Fatalf("screen %d overlay %d: the menu closes and History opens", h.m.screen, h.m.overlay)
	}
	h.press(input.Back)
	if h.m.screen != screenHome {
		t.Fatalf("B returns to Home, got screen %d", h.m.screen)
	}
	h.press(input.Menu)
	h.press(input.Back)
	if h.m.overlay != overlayNone {
		t.Fatal("B closes the menu")
	}
	h.press(input.Menu)
	h.press(input.Menu)
	if h.m.overlay != overlayNone {
		t.Fatal("Start closes the menu")
	}
}

// With downloads running every quit asks, with three buttons and Keep downloading by default.
func TestQuitWithDownloadsAsksAndQuitWhenFinishedWaitsForTheQueue(t *testing.T) {
	svc := guitest.New()
	svc.JobList = []app.JobSnapshot{activeJob("1"), queuedJob("2")}
	h := started(t, svc, 1280, 720)
	h.mustHave("1 · 1 queued")
	h.press(input.Back)
	h.mustHave("2 downloads are still running.", "Keep downloading", "Quit when finished", "Cancel and quit")
	if h.m.dlgFocus != btnKeep {
		t.Fatalf("default %q, want Keep downloading", h.m.dlgFocus)
	}
	h.advance(200 * time.Millisecond)
	h.press(input.Right)
	h.press(input.Confirm) // Quit when finished
	if h.m.screen != screenQueue || !h.m.quitPending {
		t.Fatalf("screen %d: Quit when finished marks the quit as pending and opens the Queue (PR-71 UX review)", h.m.screen)
	}
	h.mustHave("Will quit when downloads finish")

	h.press(input.Menu)
	h.mustHave("Don't quit")
	h.press(input.Back)
	h.press(input.Back) // back from the Queue to Home

	done := activeJob("1")
	done.State = app.StateDone
	h.send(JobMsg{Job: done})
	if h.m.screen != screenHome {
		t.Fatal("one job is still queued: the app must stay")
	}
	last := queuedJob("2")
	last.State = app.StateDone
	h.send(JobMsg{Job: last})
	if h.m.screen != screenExit {
		t.Fatal("the exit screen must follow when the queue empties")
	}
	if got := exitCalls(h.svc); !slices.Equal(got, []string{"wait", "close"}) {
		t.Fatalf("calls %v: no job is canceled when quitting after the queue", got)
	}
}

func TestDontQuitClearsAPendingQuit(t *testing.T) {
	svc := guitest.New()
	svc.JobList = []app.JobSnapshot{activeJob("1")}
	h := started(t, svc, 1280, 720)
	h.press(input.Back)
	h.advance(200 * time.Millisecond)
	h.press(input.Right)
	h.press(input.Confirm) // the Queue opens
	h.press(input.Menu)
	for i := 0; i < 10 && h.m.menuFocus != menuDontQuit; i++ {
		h.press(input.Down) // Don't quit sits just above Quit, after the Queue's own actions
	}
	if h.m.menuFocus != menuDontQuit {
		t.Fatalf("menu focus %q", h.m.menuFocus)
	}
	h.press(input.Confirm)
	if h.m.quitPending {
		t.Fatal("Don't quit must clear the pending quit")
	}
	h.mustNotHave("Will quit when downloads finish")
	done := activeJob("1")
	done.State = app.StateDone
	h.send(JobMsg{Job: done})
	if h.m.screen == screenExit {
		t.Fatal("the app must stay when the quit was withdrawn")
	}
}

func TestCancelAndQuitCancelsEveryUnfinishedJobThenWaitsAndCloses(t *testing.T) {
	svc := guitest.New()
	svc.JobList = []app.JobSnapshot{activeJob("1"), queuedJob("2")}
	h := started(t, svc, 1280, 720)
	h.press(input.Menu)
	for i := 0; i < 5; i++ {
		h.press(input.Down)
	}
	h.press(input.Confirm) // Quit, with downloads: asks
	h.mustHave("2 downloads are still running.")
	h.advance(200 * time.Millisecond)
	h.press(input.Right)
	h.press(input.Right)
	h.press(input.Confirm)
	if got := exitCalls(h.svc); !slices.Equal(got, []string{"cancel:1", "cancel:2", "wait", "close"}) {
		t.Fatalf("calls %v", got)
	}
	if h.m.screen != screenExit {
		t.Fatal("exit screen expected")
	}
}

// SIGTERM: no dialog and no minimum time on screen; the exit sequence runs and the loop stops.
func TestSIGTERMDrivesTheExitSequenceWithoutADialog(t *testing.T) {
	svc := guitest.New()
	svc.JobList = []app.JobSnapshot{activeJob("1")}
	svc.Batch = app.BatchResult{Done: 1}
	h := started(t, svc, 1280, 720)
	h.press(input.Back) // a dialog is open when the signal arrives
	h.send(SignalMsg{})
	if h.m.screen != screenExit || h.m.overlay != overlayNone {
		t.Fatalf("screen %d overlay %d", h.m.screen, h.m.overlay)
	}
	if got := exitCalls(h.svc); !slices.Equal(got, []string{"cancel:1", "wait", "close"}) {
		t.Fatalf("calls %v", got)
	}
	if !h.m.Done() {
		t.Fatal("SIGTERM leaves at once: no hold on the exit screen")
	}
	before := len(exitCalls(h.svc))
	h.send(SignalMsg{}) // a second signal changes nothing
	if len(exitCalls(h.svc)) != before {
		t.Fatal("a second signal ran the sequence again")
	}
}

func TestExitScreenSummarizesTheRunAndKeepsARefreshNoticeForFourSeconds(t *testing.T) {
	svc := guitest.New()
	svc.Batch = app.BatchResult{Done: 3, Failed: 1, RefreshWarning: "Refresh the game list from the frontend menu."}
	h := started(t, svc, 1280, 720)
	h.press(input.Menu)
	for i := 0; i < 5; i++ {
		h.press(input.Down)
	}
	h.press(input.Confirm)
	h.mustHave("3 downloaded · 1 failed", "Refresh the game list from the frontend menu.")
	h.advance(3900 * time.Millisecond)
	if h.m.Done() {
		t.Fatal("the notice stays for 4 s after Close")
	}
	h.advance(200 * time.Millisecond)
	if !h.m.Done() {
		t.Fatal("not done after 4 s")
	}

	skip := started(t, svc, 1280, 720)
	skip.press(input.Menu)
	for i := 0; i < 5; i++ {
		skip.press(input.Down)
	}
	skip.press(input.Confirm)
	skip.press(input.Confirm) // A skips the wait
	if !skip.m.Done() {
		t.Fatal("A must skip the notice")
	}
}

func TestExitScreenOmitsTheSummaryWithoutDownloadsAndSaysClosingForGeneric(t *testing.T) {
	svc := guitest.New()
	svc.Profile = app.ProfileInfo{ID: "generic", Name: "Generic", RomRoot: "/r"}
	h := started(t, svc, 1280, 720)
	h.send(SignalMsg{})
	h.mustHave("Closing")
	for _, s := range h.texts() {
		if strings.Contains(s, "downloaded") || strings.Contains(s, "Updating") {
			t.Errorf("unexpected exit text %q", s)
		}
	}
}

func TestAFailedDownloadMakesOneToast(t *testing.T) {
	h := started(t, guitest.New(), 1280, 720)
	failed := app.JobSnapshot{ID: "9", Title: "Contra (USA)\x1b[31m", State: app.StateFailed}
	h.send(JobMsg{Job: failed})
	const toast = "Download failed: Contra (USA)[31m" // the escape character is gone: server text is sanitized
	h.mustHave(toast)
	for _, txt := range h.texts() {
		if strings.ContainsRune(txt, 0x1b) {
			t.Errorf("a control character reached the draw list: %q", txt)
		}
	}
	h.advance(3500 * time.Millisecond)
	h.send(JobMsg{Job: failed}) // the same state again is not news
	h.mustNotHave(toast)
}

// The hint labels follow the pad in hand and the confirm button (sections 2.1 and 11).
func TestHintLabelsFollowThePadAndTheConfirmButton(t *testing.T) {
	label := func(h *harness, word string) string {
		texts := h.texts()
		i := slices.Index(texts, word)
		if i <= 0 {
			t.Fatalf("no hint %q in %q", word, texts)
		}
		return texts[i-1]
	}
	h := started(t, guitest.New(), 1280, 720)
	if label(h, "Open") != "A" || label(h, "Quit") != "B" {
		t.Errorf("an unknown pad shows Xbox letters: %q %q", label(h, "Open"), label(h, "Quit"))
	}
	h.send(ControllerMsg{Type: input.TypeSwitchPro})
	if label(h, "Open") != "B" || label(h, "Quit") != "A" {
		t.Errorf("a Nintendo pad has B at south: %q %q", label(h, "Open"), label(h, "Quit"))
	}
	h.send(ControllerMsg{Type: input.TypePS4})
	if label(h, "Open") != "Cross" || label(h, "Quit") != "Circle" {
		t.Errorf("a PlayStation pad: %q %q", label(h, "Open"), label(h, "Quit"))
	}

	svc := guitest.New()
	svc.Settings = []app.Setting{{Key: "gui.confirm_button", Value: "east"}}
	swapped := started(t, svc, 1280, 720)
	if !swapped.m.InputConfig().ConfirmEast {
		t.Fatal("gui.confirm_button = east must reach the input layer")
	}
	if label(swapped, "Open") != "B" || label(swapped, "Quit") != "A" {
		t.Errorf("with confirm on east the hints swap: %q %q", label(swapped, "Open"), label(swapped, "Quit"))
	}
}

func TestCompactHintBarKeepsTheStartDiscAndDropsItsWord(t *testing.T) {
	compact := started(t, guitest.New(), 640, 480)
	compact.mustNotHave("Menu")
	compact.mustHave("Start", "Open", "Quit")
	standard := started(t, guitest.New(), 1280, 720)
	standard.mustHave("Start", "Menu")
}

func TestGuiSettingsFromConfigListChangeTheLayout(t *testing.T) {
	svc := guitest.New()
	svc.Settings = []app.Setting{
		{Key: "gui.safe_area", Value: "10"},
		{Key: "gui.text_size", Value: "large"},
		{Key: "paths.data", Value: "/x"},
	}
	h := started(t, svc, 1920, 1080)
	if h.m.Metrics().LargeText != true || h.m.Metrics().Safe.X != 192 {
		t.Fatalf("metrics %+v", h.m.Metrics())
	}
	bad := guitest.New()
	bad.Settings = []app.Setting{{Key: "gui.safe_area", Value: "99"}, {Key: "gui.text_size", Value: "huge"}}
	hb := started(t, bad, 1920, 1080)
	if hb.m.Metrics().Safe.X != 96 || hb.m.Metrics().LargeText {
		t.Fatalf("invalid values must keep the defaults: %+v", hb.m.Metrics().Safe)
	}
}

func TestResizeRetiersTheLayoutAndKeepsFocus(t *testing.T) {
	h := started(t, guitest.New(), 640, 480)
	h.press(input.Right)
	h.press(input.Down)
	focus := h.m.focus
	h.send(ResizeMsg{W: 1920, H: 1080})
	if h.m.Metrics().Tier != Large || h.m.focus != focus {
		t.Fatalf("tier %d focus %q (was %q)", h.m.Metrics().Tier, h.m.focus, focus)
	}
}

func TestOverlayEpochChangesWhenAnOverlayOpensOrCloses(t *testing.T) {
	h := started(t, guitest.New(), 1280, 720)
	e0 := h.m.OverlayEpoch()
	h.press(input.Menu)
	e1 := h.m.OverlayEpoch()
	h.press(input.Back)
	if e1 == e0 || h.m.OverlayEpoch() == e1 {
		t.Fatal("opening and closing the menu must each change the epoch, so a held button is released")
	}
}

func TestNextWakeIsTheNearestTimer(t *testing.T) {
	h := newHarness(t, guitest.New(), 1280, 720)
	if wake, ok := h.m.NextWake(h.now); !ok || wake.Sub(h.now) != 200*time.Millisecond {
		t.Fatalf("on the Splash the spinner is due first: %v %v", wake.Sub(h.now), ok)
	}
	h.run(h.pending)
	if _, ok := h.m.NextWake(h.now); ok {
		t.Fatal("a calm Home needs no frames")
	}
	h.send(JobMsg{Job: app.JobSnapshot{ID: "1", Title: "Contra (USA)", State: app.StateFailed, Err: &app.Error{Code: app.CodeNetwork, Message: "down"}}}) // raises a toast
	if wake, ok := h.m.NextWake(h.now); !ok || wake.Sub(h.now) != toastFor {
		t.Fatalf("a toast ends in 3 s: %v %v", wake.Sub(h.now), ok)
	}
}

// Nothing is drawn outside the safe rectangle, at three resolutions and three margins, on every screen and
// overlay of this pull request, and the focused element is always drawn with its ring (section 11).
func TestEverythingIsDrawnInsideTheSafeRectangle(t *testing.T) {
	font, err := text.Parse(assets.NotoSans)
	if err != nil {
		t.Fatal(err)
	}
	screens := map[string]func(h *harness){
		"home":   func(*harness) {},
		"menu":   func(h *harness) { h.press(input.Menu) },
		"quit":   func(h *harness) { h.press(input.Back) },
		"busy":   func(h *harness) { h.press(input.Back) },
		"toast":  func(h *harness) { h.press(input.Confirm) },
		"splash": nil,
		"exit":   func(h *harness) { h.send(SignalMsg{}) },
	}
	for _, size := range [][2]int{{640, 480}, {1280, 720}, {1920, 1080}} {
		for _, safe := range []SafeArea{{Percent: 0}, {Percent: 5}, {Percent: 10}} {
			for _, large := range []bool{false, true} {
				for name, setup := range screens {
					svc := guitest.New()
					svc.Batch = app.BatchResult{Done: 12, Failed: 3, RefreshWarning: "Refresh the game list from the frontend menu to see the new games."}
					if name == "busy" {
						svc.JobList = []app.JobSnapshot{activeJob("1"), queuedJob("2")}
					}
					dir := t.TempDir()
					body, _ := json.Marshal(map[string][]string{"recent": {"zelda", "metroid", "chrono trigger", "a very long search text that must be cut"}})
					_ = os.WriteFile(filepath.Join(dir, "tui_state"), body, 0o600)
					opt := func(o *Options) { o.Safe, o.LargeText, o.DataDir = safe, large, dir }
					var h *harness
					if setup == nil {
						h = newHarness(t, svc, size[0], size[1], opt)
						h.now = h.now.Add(time.Second)
					} else {
						h = started(t, svc, size[0], size[1], opt)
						setup(h)
					}
					l := h.render()
					met := h.m.Metrics()
					for i, op := range l.Ops {
						where := func() string {
							return name + " " + string(rune('0'+i%10))
						}
						switch op.Kind {
						case draw.KindRect:
							full := op.Rect == draw.Rect{W: met.W, H: met.H}
							if !full && !op.Rect.Within(met.Safe) {
								t.Errorf("%dx%d safe=%d large=%v %s: rect %+v outside %+v", size[0], size[1], safe.Percent, large, where(), op.Rect, met.Safe)
							}
						case draw.KindText:
							face, _ := font.Face(op.Size)
							width := face.Width(op.Text)
							if op.X < met.Safe.X || op.X+width > met.Safe.Right() || op.Base-face.Ascent() < met.Safe.Y || op.Base+face.Descent() > met.Safe.Bottom() {
								t.Errorf("%dx%d safe=%d large=%v %s: text %q at (%d,%d) width %d outside %+v", size[0], size[1], safe.Percent, large, where(), op.Text, op.X, op.Base, width, met.Safe)
							}
						case draw.KindSpinner:
							if !(draw.Rect{X: op.CX - 3*op.Cell, Y: op.CY - 3*op.Cell, W: 6 * op.Cell, H: 6 * op.Cell}).Within(met.Safe) {
								t.Errorf("%dx%d %s: spinner outside the safe rectangle", size[0], size[1], where())
							}
						}
					}
					if name == "home" || name == "menu" || name == "quit" || name == "busy" {
						ring := false
						for _, op := range l.Ops {
							ring = ring || (op.Kind == draw.KindRect && op.Color == colFocusRing)
						}
						if !ring {
							t.Errorf("%dx%d %s: no focus ring is drawn", size[0], size[1], name)
						}
					}
				}
			}
		}
	}
}

// Large text at 640x480 must keep the dialog buttons on screen: three long buttons stack when they do not
// fit in a row.
func TestDialogButtonsStackWhenTheRowDoesNotFit(t *testing.T) {
	svc := guitest.New()
	svc.JobList = []app.JobSnapshot{activeJob("1")}
	row := started(t, svc, 1280, 720)
	row.press(input.Back)
	g := row.m.dialogGeometry()
	if g.buttons[0].rect.Y != g.buttons[2].rect.Y {
		t.Fatal("at 1280x720 the three buttons share a row")
	}
	tight := started(t, svc, 640, 480, func(o *Options) { o.LargeText = true; o.Safe = SafeArea{Percent: 10} })
	tight.press(input.Back)
	g = tight.m.dialogGeometry()
	if g.buttons[0].rect.Y == g.buttons[2].rect.Y {
		t.Fatal("at 640x480 with large text and a 10% margin the buttons must stack")
	}
	tight.advance(200 * time.Millisecond)
	tight.press(input.Down)
	if tight.m.dlgFocus != btnWhenFinished {
		t.Fatalf("Down moves through stacked buttons: %q", tight.m.dlgFocus)
	}
}

// PR-71 review, Major 1: the last job ended while the dialog was open, so Quit when finished has nothing to wait
// for and must quit at once instead of leaving a pending quit that no event can ever finish.
func TestQuitWhenFinishedQuitsAtOnceIfTheLastJobEndedUnderTheDialog(t *testing.T) {
	svc := guitest.New()
	svc.JobList = []app.JobSnapshot{activeJob("1")}
	h := started(t, svc, 1280, 720)
	h.press(input.Back)
	done := activeJob("1")
	done.State = app.StateDone
	h.send(JobMsg{Job: done}) // the download ends while the dialog is open
	h.advance(200 * time.Millisecond)
	h.press(input.Right)
	h.press(input.Confirm) // Quit when finished
	if h.m.screen != screenExit || h.m.quitPending {
		t.Fatalf("screen %d, pending %v: with nothing left to wait for the exit sequence starts", h.m.screen, h.m.quitPending)
	}
}

// PR-71 review, suggestion 1: closing the window asks like Quit and never cancels downloads on its own.
func TestClosingTheWindowAsksLikeQuit(t *testing.T) {
	idle := started(t, guitest.New(), 1280, 720)
	idle.send(QuitRequestMsg{})
	if idle.m.screen != screenExit {
		t.Fatal("with nothing running, closing the window quits")
	}

	svc := guitest.New()
	svc.JobList = []app.JobSnapshot{activeJob("1"), queuedJob("2")}
	busy := started(t, svc, 1280, 720)
	busy.send(QuitRequestMsg{})
	if busy.m.screen != screenHome || busy.m.overlay != overlayDialog {
		t.Fatalf("screen %d overlay %d: with downloads a dialog must ask", busy.m.screen, busy.m.overlay)
	}
	busy.mustHave("2 downloads are still running.")
	if got := exitCalls(svc); len(got) != 0 {
		t.Fatalf("nothing may be called or canceled before the answer: %v", got)
	}
	busy.send(SignalMsg{}) // SIGTERM still goes straight through
	if busy.m.screen != screenExit {
		t.Fatal("a signal must not ask")
	}
}

func TestAProgressEventDoesNotCopyTheTally(t *testing.T) {
	var tally jobTally
	tally, _, _ = tally.apply(app.JobSnapshot{ID: "1", State: app.StateTransferring, Bytes: 10})
	same, prev, known := tally.apply(app.JobSnapshot{ID: "1", State: app.StateTransferring, Bytes: 20})
	if !known || prev.Bytes != 10 {
		t.Fatalf("known %v prev %+v", known, prev)
	}
	if len(same.byID) != 1 || reflect.ValueOf(same.byID).Pointer() != reflect.ValueOf(tally.byID).Pointer() {
		t.Fatal("a progress event must reuse the same map")
	}
	changed, _, _ := same.apply(app.JobSnapshot{ID: "1", State: app.StateDone})
	if done, _, _, _, _ := changed.summary(); done != 1 {
		t.Fatalf("a state change is recorded: %d done", done)
	}
	if done, _, _, _, _ := same.summary(); done != 0 {
		t.Fatal("the earlier tally must not change")
	}
}
