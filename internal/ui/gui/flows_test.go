package gui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/guitest"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

// The flows of docs/ux/gui.md sections 5.3 to 5.8: Home › Search › Results › Entry and the Queue, against the
// shared fake service. Assertions are on state and on the content of the draw list, never on pixels.

// mustContain checks that the text runs of the frame, joined with spaces, contain s: for text the screen wraps
// over several lines.
func (h *harness) mustContain(s string) {
	h.t.Helper()
	if joined := strings.Join(h.texts(), " "); !strings.Contains(joined, s) {
		h.t.Errorf("%q is not in %q", s, joined)
	}
}

func zeldaService(n int) *guitest.Fake {
	svc := guitest.New()
	for i := 1; i <= n; i++ {
		svc.Catalog = append(svc.Catalog, guitest.Game(fmt.Sprintf("zelda-%03d", i), fmt.Sprintf("Zelda %03d", i), "snes", "us"))
	}
	svc.Platforms_ = []mothdex.Platform{
		{ID: "snes", Name: "Super Nintendo", Brand: "Nintendo"}, {ID: "nes", Name: "Nintendo Entertainment System", Brand: "Nintendo"},
		{ID: "gbc", Name: "Game Boy Color", Brand: "Nintendo"}, {ID: "md", Name: "Mega Drive / Genesis", Brand: "Sega"},
	}
	svc.Regions_ = []mothdex.Region{{ID: "us", Name: "USA"}, {ID: "eu", Name: "Europe"}}
	return svc
}

// searchFor opens Search from Home, types text with the physical keyboard and runs the search.
func searchFor(t *testing.T, h *harness, text string) {
	t.Helper()
	h.press(input.Confirm) // Home › Search
	if h.m.screen != screenSearch || !h.m.TextMode() {
		t.Fatalf("screen %d: Search opens from the Search tile and takes text", h.m.screen)
	}
	h.send(TextMsg{Text: text})
	h.press(input.Menu) // Start runs the search
}

func TestSearchOpensFromHomeWithFocusOnQAndTheKeyboardTypesWithA(t *testing.T) {
	h := started(t, zeldaService(3), 1280, 720)
	h.press(input.Confirm)
	if h.m.search.focus != gridKeyID(1, 0) {
		t.Fatalf("focus %q: q when the field is empty", h.m.search.focus)
	}
	h.mustHave("0/80", "Platform: any", "Region: any", "space", "Del")
	for _, a := range []input.Action{input.Confirm, input.Right, input.Confirm} { // q, w
		h.press(a)
	}
	if got := h.m.search.field.String(); got != "qw" {
		t.Fatalf("field %q, want qw", got)
	}
	h.press(input.Download) // X is a space
	h.press(input.PageUp)   // L moves the cursor left
	h.press(input.Confirm)  // types the focused key (w) before the space
	if got := h.m.search.field.String(); got != "qww " {
		t.Fatalf("field %q after the cursor moved", got)
	}
	h.press(input.Back) // B deletes
	if got := h.m.search.field.String(); got != "qw " {
		t.Fatalf("field %q after B", got)
	}
	h.mustHave("3/80")
}

// Y cycles the layouts, and the key shows the name of the next one; the accent layout types accents.
func TestYCyclesTheLayoutsAndTheLayoutKeyNamesTheNextOne(t *testing.T) {
	h := started(t, zeldaService(1), 1280, 720)
	h.press(input.Confirm)
	h.mustHave("àé") // the layout key of abc
	h.press(input.Info)
	if h.m.currentLayout().name != "àé" {
		t.Fatalf("layout %s", h.m.currentLayout().name)
	}
	h.mustHave("abc", "á", "ñ")
	h.press(input.Confirm) // the focused key is q's place: row 1 col 0 of the accent grid
	if got := h.m.search.field.String(); got != "ê" {
		t.Fatalf("typed %q from the accent layout", got)
	}
	h.press(input.Info) // back to abc: no third layout for English
	if h.m.currentLayout().name != "abc" {
		t.Fatalf("layout %s", h.m.currentLayout().name)
	}
	ru := started(t, zeldaService(1), 1280, 720, withLocale("ru_ru"))
	ru.press(input.Confirm)
	ru.press(input.Info)
	ru.press(input.Info)
	if ru.m.currentLayout().name != "абв" {
		t.Fatalf("Russian cycle reaches %s", ru.m.currentLayout().name)
	}
}

// B at an empty field goes back, but a held B that deleted the last character must not also leave.
func TestHeldBDeletesAndOnlyLeavesWhenTheFieldWasAlreadyEmpty(t *testing.T) {
	h := started(t, zeldaService(1), 1280, 720)
	h.press(input.Confirm)
	h.send(TextMsg{Text: "ab"})
	h.send(InputMsg{Action: input.Back})               // deletes b
	h.send(InputMsg{Action: input.Back, Repeat: true}) // deletes a
	h.send(InputMsg{Action: input.Back, Repeat: true}) // empty now: a repeat is ignored
	if h.m.screen != screenSearch {
		t.Fatal("a repeated B must not leave the screen")
	}
	h.send(InputMsg{Action: input.Back})
	if h.m.screen != screenHome {
		t.Fatalf("a fresh B at an empty field goes back, screen %d", h.m.screen)
	}
}

func TestTheLimitIsEightyCharactersAndTheHintSaysNotAvailable(t *testing.T) {
	h := started(t, zeldaService(1), 1280, 720)
	h.press(input.Confirm)
	h.send(TextMsg{Text: strings.Repeat("a", 90)})
	if n := len(h.m.search.field.text); n != maxQueryRunes {
		t.Fatalf("%d characters", n)
	}
	h.mustHave("80/80", "(not available)")
	h.press(input.Confirm)
	if len(h.m.search.field.text) != maxQueryRunes {
		t.Fatal("A on a character key does nothing at the limit")
	}
	h.press(input.Back)
	h.mustNotHave("(not available)")
}

func TestPhysicalKeyboardTextIsLowercasedAndFiltered(t *testing.T) {
	h := started(t, zeldaService(1), 1280, 720)
	h.press(input.Confirm)
	h.send(TextMsg{Text: "ZeLdA\x1b\n"})
	if got := h.m.search.field.String(); got != "zelda" {
		t.Fatalf("%q", got)
	}
	for i := 0; i < 5; i++ {
		h.press(input.Back) // B deletes the five characters
	}
	h.press(input.Back) // and the next one leaves
	if h.m.screen != screenHome {
		t.Fatalf("screen %d", h.m.screen)
	}
	h.send(TextMsg{Text: "x"}) // text outside Search is ignored
	if h.m.search.field.String() != "" {
		t.Fatalf("typing outside Search must change nothing: %q", h.m.search.field.String())
	}
}

func TestSearchWithNothingTypedSaysSoAndCallsNothing(t *testing.T) {
	svc := zeldaService(3)
	h := started(t, svc, 1280, 720)
	h.press(input.Confirm)
	h.press(input.Menu)
	h.mustHave("Type something or pick a platform.")
	if h.m.screen != screenSearch || svc.Count("search:") != 0 {
		t.Fatalf("screen %d, calls %v: no call may be made", h.m.screen, svc.Calls())
	}
	h.press(input.Right) // any action clears the message
	h.mustNotHave("Type something or pick a platform.")
}

// The pickers: multi-select with checkboxes from Search, Done applies, B cancels, the chip lists the ids.
func TestPlatformPickerSelectsAndAppliesAndCancels(t *testing.T) {
	svc := zeldaService(3)
	h := started(t, svc, 1280, 720)
	h.press(input.Confirm)
	h.press(input.Up)
	h.press(input.Up) // the Platform chip
	if h.m.search.focus != chipPlatform {
		t.Fatalf("focus %q", h.m.search.focus)
	}
	h.press(input.Confirm)
	if h.m.screen != screenPicker || svc.Count("platforms") != 1 {
		t.Fatalf("screen %d, calls %v", h.m.screen, svc.Calls())
	}
	h.mustHave("Nintendo", "Sega", "Game Boy Color", "Super Nintendo", "snes")
	h.press(input.Confirm) // the first choice: Game Boy Color
	h.press(input.Down)
	h.press(input.Down) // Super Nintendo (Nintendo Entertainment System sorts before)
	h.press(input.Confirm)
	h.mustHave("2 selected", "[x]")
	h.press(input.Menu) // Done
	if h.m.screen != screenSearch || !slices.Equal(h.m.search.platforms, []string{"gbc", "snes"}) {
		t.Fatalf("screen %d platforms %v", h.m.screen, h.m.search.platforms)
	}
	h.mustHave("Platform: gbc, snes")

	h.press(input.Confirm) // reopen: the choices are still there, and B drops changes
	h.press(input.Info)    // clear all
	h.press(input.Back)    // cancel
	if !slices.Equal(h.m.search.platforms, []string{"gbc", "snes"}) {
		t.Fatalf("B must cancel the changes: %v", h.m.search.platforms)
	}
	if svc.Count("platforms") != 1 {
		t.Fatal("the platform list is loaded once and kept")
	}
}

func TestPickerGroupsJumpWithLAndR(t *testing.T) {
	h := started(t, zeldaService(1), 1280, 720)
	h.press(input.Confirm)
	h.press(input.Up)
	h.press(input.Up)
	h.press(input.Confirm)
	rows := h.m.pickerRows()
	if !rows[0].header || rows[0].name != "Nintendo" || rows[len(rows)-2].name != "Mega Drive / Genesis" && !rows[len(rows)-1].header && rows[len(rows)-1].id != "md" {
		t.Fatalf("rows %+v: groups by brand, sorted", rows)
	}
	h.press(input.PageDown) // R: the next group
	if got := h.m.pickerRows()[h.m.picker.list.focus].id; got != "md" {
		t.Fatalf("R went to %q, want the first Sega platform", got)
	}
	h.press(input.PageUp) // L: the previous group
	if got := h.m.pickerRows()[h.m.picker.list.focus].id; got != "gbc" {
		t.Fatalf("L went to %q, want the first Nintendo platform", got)
	}
}

func TestPickerLoadFailureOffersRetry(t *testing.T) {
	svc := zeldaService(1)
	svc.PlatformErr = &app.Error{Code: app.CodeNetwork, Message: "down"}
	h := started(t, svc, 1280, 720)
	h.press(input.Confirm)
	h.press(input.Up)
	h.press(input.Up)
	h.press(input.Confirm)
	h.mustHave("Cannot reach the Mothdex server.", "code: network", "Retry")
	svc.PlatformErr = nil
	h.press(input.Confirm) // A retries
	h.mustHave("Super Nintendo")
	if svc.Count("platforms") != 2 {
		t.Fatalf("calls %v", svc.Calls())
	}
}

// Home › Platforms is a single select that opens Results for the platform with empty text.
func TestHomePlatformsOpensResultsForOnePlatform(t *testing.T) {
	svc := zeldaService(3)
	h := started(t, svc, 1280, 720)
	h.press(input.Right)
	h.press(input.Confirm)
	if h.m.screen != screenPicker || !h.m.picker.single {
		t.Fatal("Platforms is a single-select picker")
	}
	h.mustNotHave("[ ]", "[x]")
	h.press(input.Down) // Nintendo Entertainment System; the focus starts on Game Boy Color
	h.press(input.Confirm)
	if h.m.screen != screenResults || !slices.Equal(h.m.results.query.Platforms, []string{"nes"}) || h.m.results.query.Text != "" {
		t.Fatalf("screen %d, query %+v", h.m.screen, h.m.results.query)
	}
	h.press(input.Back)
	if h.m.screen != screenHome {
		t.Fatalf("B goes back to Home, got screen %d", h.m.screen)
	}
}

func TestSearchRunsAndResultsListTheRowsWithTheirTwoLines(t *testing.T) {
	svc := zeldaService(3)
	svc.Records = []app.HistoryRecord{{State: "done", Slug: "zelda-002", LinkIndex: 1}}
	h := started(t, svc, 1280, 720)
	searchFor(t, h, "zelda")
	if h.m.screen != screenResults {
		t.Fatalf("screen %d", h.m.screen)
	}
	h.mustHave(`mothdex › Results › "zelda"`, "Zelda 001", "Zelda 002", "Zelda 003", "snes · us · 1.0 MB", "Downloaded", "3 results")
	if svc.Count("search:zelda:1") != 1 {
		t.Fatalf("calls %v", svc.Calls())
	}
	// The recent search is saved where the TUI keeps its own.
	h.run(nil)
	data, err := os.ReadFile(filepath.Join(h.m.o.DataDir, "tui_state"))
	if err != nil || !strings.Contains(string(data), `"zelda"`) {
		t.Fatalf("tui_state %q %v", data, err)
	}
}

func TestResultsStatesEmptyErrorAndStale(t *testing.T) {
	svc := zeldaService(3)
	h := started(t, svc, 1280, 720)
	searchFor(t, h, "nothing like this")
	h.mustContain(`No results for "nothing like this". Try fewer words, or remove filters.`)
	h.press(input.Info) // Y edits the search
	if h.m.screen != screenSearch || h.m.search.field.String() != "nothing like this" || h.m.search.focus != keySearch {
		t.Fatalf("screen %d text %q focus %q", h.m.screen, h.m.search.field.String(), h.m.search.focus)
	}

	svc.SearchErr = &app.Error{Code: app.CodeNetwork, Message: "down"}
	h.press(input.Menu)
	h.mustHave("Cannot reach the Mothdex server.", "Check your connection.", "code: network", "Retry")
	svc.SearchErr = nil
	h.send(TextMsg{})      // nothing
	h.press(input.Confirm) // A retries
	h.mustContain(`No results for "nothing like this". Try fewer words, or remove filters.`)

	stale := zeldaService(2)
	stale.Stale = true
	hs := started(t, stale, 1280, 720)
	searchFor(t, hs, "zelda")
	hs.mustHave("2 results · stale: served from cache")
}

// The list is one continuous list over service pages of 25: the next page is asked in the last five rows.
func TestResultsFetchTheNextPageNearTheEndAndKeepTwoHundredRows(t *testing.T) {
	svc := zeldaService(300)
	h := started(t, svc, 1280, 720)
	searchFor(t, h, "zelda")
	if len(h.m.results.win.rows) != 25 || svc.Count("search:zelda:2") != 0 {
		t.Fatalf("%d rows, calls %v", len(h.m.results.win.rows), svc.Calls())
	}
	for i := 0; i < 19; i++ {
		h.press(input.Down)
	}
	if svc.Count("search:zelda:2") != 0 {
		t.Fatal("the next page is asked only in the last five rows")
	}
	h.press(input.Down) // row 21
	if svc.Count("search:zelda:2") != 1 || len(h.m.results.win.rows) != 50 {
		t.Fatalf("page 2: %d rows, calls %d", len(h.m.results.win.rows), svc.Count("search:zelda:2"))
	}
	// Scroll all the way down: the window never holds more than 200 rows, and the focus stays on its row.
	for i := 0; i < 270; i++ {
		h.press(input.Down)
	}
	s := h.m.results
	if len(s.win.rows) > maxWindowRows || s.win.first <= 1 {
		t.Fatalf("%d rows from page %d", len(s.win.rows), s.win.first)
	}
	if row, _ := s.focusedRow(); row.slug != "zelda-291" {
		t.Fatalf("focused %q, want zelda-291 after 290 moves down", row.slug)
	}
	// Back up: the pages that dropped off come back from the service (its cache).
	for i := 0; i < 200; i++ {
		h.press(input.Up)
	}
	if h.m.results.win.first >= s.win.first {
		t.Fatalf("scrolling up must fetch the previous pages: first %d was %d", h.m.results.win.first, s.win.first)
	}
	if row, _ := h.m.results.focusedRow(); row.slug != "zelda-091" {
		t.Fatalf("focused %q, want zelda-091", row.slug)
	}
}

func TestResultsNeverAskTwiceForTheSamePageWhileItLoads(t *testing.T) {
	svc := zeldaService(100)
	h := started(t, svc, 1280, 720)
	searchFor(t, h, "zelda")
	for i := 0; i < 24; i++ {
		h.press(input.Down)
	}
	if n := svc.Count("search:zelda:2"); n != 1 {
		t.Fatalf("page 2 asked %d times", n)
	}
}

// X downloads the row with the default link; the toast names the link; the row then carries the mark.
func TestXQueuesTheRowAndToastsTheLink(t *testing.T) {
	svc := zeldaService(3)
	h := started(t, svc, 1280, 720)
	searchFor(t, h, "zelda")
	h.press(input.Down)
	h.press(input.Download)
	if svc.Count("enqueue:zelda-002:0") != 1 {
		t.Fatalf("calls %v: the default link is asked with no index", svc.Calls())
	}
	h.mustHave("Queued: Zelda 002 (link 1)", "0 · 1 queued")
	done := svc.JobList[0]
	done.State = app.StateDone
	done.LinkIndex = 1
	h.send(JobMsg{Job: done})
	h.mustHave("Downloaded")
}

// The cover is asked 250 ms after the focus settles, and only one decode runs at a time.
func TestCoversAreAskedAfterTheFocusSettlesAndOneAtATime(t *testing.T) {
	svc := zeldaService(5)
	svc.Covers = map[string]bool{"zelda-001": true, "zelda-002": true, "zelda-003": true}
	h := started(t, svc, 1280, 720)
	searchFor(t, h, "zelda")
	tick := func() []Cmd { // a tick the harness does not run, so the decode stays in flight
		var cmds []Cmd
		h.m, cmds = h.m.Update(TickMsg{}, h.now)
		return cmds
	}
	h.now = h.now.Add(200 * time.Millisecond)
	if len(tick()) != 0 {
		t.Fatal("no request before the focus has settled for 250 ms")
	}
	h.press(input.Down)
	h.now = h.now.Add(240 * time.Millisecond)
	h.press(input.Down) // still moving: the timer restarts
	h.now = h.now.Add(240 * time.Millisecond)
	if len(tick()) != 0 || svc.Count("cover:") != 0 {
		t.Fatal("scrolling must not request covers")
	}
	h.now = h.now.Add(20 * time.Millisecond)
	cmds := tick()
	if len(cmds) != 1 || h.m.coverBusy != "zelda-003" {
		t.Fatalf("%d cmds, busy %q: the settled row's cover is asked", len(cmds), h.m.coverBusy)
	}
	h.press(input.Up) // the focus moves while the decode is in flight
	h.now = h.now.Add(500 * time.Millisecond)
	if len(tick()) != 0 {
		t.Fatal("a second decode must wait for the first")
	}
	h.run(cmds) // the first one answers; the settled row is asked next
	h.run([]Cmd{func() Msg { return nil }})
	h.now = h.now.Add(time.Millisecond)
	h.run(tick())
	if svc.Count("cover:zelda-003") != 1 || svc.Count("cover:zelda-002") != 1 {
		t.Fatalf("calls %v", svc.Calls())
	}
}

func TestACoverReachesTheDrawListAndAPlaceholderStandsForAMissingOne(t *testing.T) {
	svc := zeldaService(3)
	svc.Covers = map[string]bool{"zelda-001": true}
	h := started(t, svc, 1280, 720)
	searchFor(t, h, "zelda")
	h.advance(300 * time.Millisecond)
	h.run(nil)
	images := func() int {
		n := 0
		for _, op := range h.render().Ops {
			if op.Kind == draw.KindImage {
				n++
			}
		}
		return n
	}
	if images() != 1 {
		t.Fatalf("%d image ops: the cover of the focused row", images())
	}
	h.press(input.Down)
	h.advance(300 * time.Millisecond)
	if images() != 0 {
		t.Fatal("a row with no cover shows the placeholder, not an image")
	}
	h.mustHave("snes") // the platform id on the placeholder
	if svc.Count("cover:zelda-002") != 1 {
		t.Fatalf("calls %v", svc.Calls())
	}
	h.press(input.Up)
	h.advance(300 * time.Millisecond)
	if svc.Count("cover:zelda-001") != 1 || images() != 1 {
		t.Fatal("a cover decoded once stays in the memory cache")
	}
}

func TestCompactAndNarrowScreensHaveNoCoverPanel(t *testing.T) {
	svc := zeldaService(3)
	svc.Covers = map[string]bool{"zelda-001": true}
	h := started(t, svc, 640, 480)
	searchFor(t, h, "zelda")
	h.advance(400 * time.Millisecond)
	if svc.Count("cover:") != 0 || h.m.resultsLayout().hasPan {
		t.Fatal("no cover panel, and no cover request, below the standard tier")
	}
}

func TestEntryShowsLinksAndSkipsTheUnavailableOnes(t *testing.T) {
	svc := zeldaService(2)
	svc.LinkList = map[string][]app.LinkChoice{"zelda-001": {
		{Index: 2, Link: mothdex.Link{Type: "Game", SizeStr: "1.0 MB", SourceName: "Myrient", Extras: []string{"En"}}, Available: true, Reasons: []string{"lang=En"}},
		{Index: 3, Link: mothdex.Link{Type: "Demo", SizeStr: "0.2 MB", SourceName: "Archive"}, Available: true},
		{Index: 1, Link: mothdex.Link{Type: "Game", SizeStr: "1.0 MB", SourceName: "archive.org"}, Available: false},
	}}
	h := started(t, svc, 1280, 720)
	searchFor(t, h, "zelda")
	h.press(input.Confirm)
	if h.m.screen != screenEntry {
		t.Fatalf("screen %d", h.m.screen)
	}
	h.mustHave("Zelda 001", "Links", "*")
	h.mustContain("(not available)")
	h.press(input.Down)
	h.press(input.Down)
	h.press(input.Down)
	if h.m.entry.focus != 1 {
		t.Fatalf("focus %d: the D-pad skips unavailable links", h.m.entry.focus)
	}
	h.mustNotHave("lang=En")
	h.press(input.Info)
	h.mustHave("lang=En")

	h.press(input.Confirm) // downloads the focused link by its index
	if svc.Count("enqueue:zelda-001:3") != 1 {
		t.Fatalf("calls %v", svc.Calls())
	}
	h.press(input.Download) // X: the default link, with no index chosen by hand
	if svc.Count("enqueue:zelda-001:2") != 1 {
		t.Fatalf("calls %v: X queues the first ranked link", svc.Calls())
	}
	if h.m.screen != screenEntry {
		t.Fatal("the person stays on the screen after queueing")
	}
}

func TestEntryLAndRMoveToTheNeighbouringResult(t *testing.T) {
	h := started(t, zeldaService(3), 1280, 720)
	searchFor(t, h, "zelda")
	h.press(input.Confirm)
	h.mustHave("Zelda 001")
	h.press(input.PageDown)
	h.mustHave("Zelda 002")
	if row, _ := h.m.results.focusedRow(); row.slug != "zelda-002" {
		t.Fatalf("Results focus follows: %q", row.slug)
	}
	h.press(input.PageUp)
	h.press(input.PageUp) // the first entry has no previous one
	h.mustHave("Zelda 001")
	h.press(input.Back)
	if h.m.screen != screenResults {
		t.Fatalf("screen %d", h.m.screen)
	}
}

func TestEntryWithoutLinksHidesTheDownloadActions(t *testing.T) {
	svc := zeldaService(1)
	svc.LinkList = map[string][]app.LinkChoice{"zelda-001": nil}
	h := started(t, svc, 1280, 720)
	searchFor(t, h, "zelda")
	h.press(input.Confirm)
	h.mustHave("No links available for this entry.")
	h.mustNotHave("Download this", "Download best")
	h.press(input.Confirm)
	if svc.Count("enqueue:") != 0 {
		t.Fatal("nothing to download")
	}
}

func TestEntryLoadFailureShowsTheErrorInPlaceWithRetry(t *testing.T) {
	svc := zeldaService(1)
	h := started(t, svc, 1280, 720)
	searchFor(t, h, "zelda")
	svc.Catalog = nil // the entry vanished
	h.press(input.Confirm)
	h.mustHave("code: not_found")
	svc.Catalog = zeldaService(1).Catalog
	h.press(input.Confirm) // A retries
	h.mustHave("Zelda 001", "Links")
}

func TestAlreadyDownloadedLinkAsksFirstWithCancelAsTheDefault(t *testing.T) {
	svc := zeldaService(1)
	svc.Records = []app.HistoryRecord{{State: "done", Slug: "zelda-001", LinkIndex: 1}}
	h := started(t, svc, 1280, 720)
	searchFor(t, h, "zelda")
	h.press(input.Confirm)
	h.press(input.Confirm)
	h.mustHave("Already downloaded. Download again?", "Cancel", "Download again")
	if h.m.dlgFocus != btnCancel {
		t.Fatalf("default %q", h.m.dlgFocus)
	}
	if svc.Count("enqueue:") != 0 {
		t.Fatal("nothing is queued before the answer")
	}
	h.advance(200 * time.Millisecond)
	h.press(input.Right)
	h.press(input.Confirm)
	if svc.Count("enqueue:zelda-001:1") != 1 {
		t.Fatalf("calls %v", svc.Calls())
	}
}

// interface.confirm_downloads asks only above 500 MB, with Download as the default; B cancels.
func TestBigFilesAskWhenConfirmationsAreOn(t *testing.T) {
	svc := zeldaService(2)
	svc.Catalog[0].Links[0].Size = 700 << 20
	svc.Catalog[0].Links[0].SizeStr = "700 MB"
	on := started(t, svc, 1280, 720, func(o *Options) { o.Confirm = true })
	searchFor(t, on, "zelda")
	on.press(input.Download)
	on.mustContain("Download Zelda 001 (700.0 MB) to /userdata/roms?")
	on.mustHave("Cancel", "Download")
	if on.m.dlgFocus != btnDownload {
		t.Fatalf("default %q: Download, since the person just pressed it", on.m.dlgFocus)
	}
	on.advance(200 * time.Millisecond)
	on.press(input.Back) // B cancels
	if svc.Count("enqueue:") != 0 {
		t.Fatal("B must cancel")
	}
	on.press(input.Download)
	on.advance(200 * time.Millisecond)
	on.press(input.Confirm)
	if svc.Count("enqueue:zelda-001") != 1 {
		t.Fatalf("calls %v", svc.Calls())
	}

	off := started(t, svc, 1280, 720)
	searchFor(t, off, "zelda")
	off.press(input.Download)
	if off.m.overlay != overlayNone {
		t.Fatal("no confirmation with interface.confirm_downloads off")
	}
	small := started(t, svc, 1280, 720, func(o *Options) { o.Confirm = true })
	searchFor(t, small, "zelda")
	small.press(input.Down)
	small.press(input.Download)
	if small.m.overlay != overlayNone {
		t.Fatal("a 1 MB file never asks")
	}
}

func TestRandomPicksAndNewPicks(t *testing.T) {
	svc := zeldaService(30)
	h := started(t, svc, 1280, 720)
	h.press(input.Right)
	h.press(input.Right)
	h.press(input.Confirm)
	if h.m.screen != screenResults || !h.m.results.random || svc.Count("random:10") != 1 {
		t.Fatalf("screen %d calls %v", h.m.screen, svc.Calls())
	}
	h.mustHave("mothdex › Random picks", "New picks")
	h.mustNotHave("Edit search")
	h.press(input.Info)
	if svc.Count("random:10") != 2 || h.m.screen != screenResults {
		t.Fatalf("Y asks for new picks: calls %v", svc.Calls())
	}
	h.press(input.Back)
	if h.m.screen != screenHome {
		t.Fatalf("one B goes back: screen %d", h.m.screen)
	}
}

// Select opens the Queue from any screen and B comes back to where the person was.
func TestSelectOpensTheQueueFromAnyScreenAndBReturns(t *testing.T) {
	h := started(t, zeldaService(3), 1280, 720)
	searchFor(t, h, "zelda")
	h.press(input.Queue)
	if h.m.screen != screenQueue {
		t.Fatalf("screen %d", h.m.screen)
	}
	h.mustHave("No downloads. Press X on a game to download it.")
	h.press(input.Back)
	if h.m.screen != screenResults {
		t.Fatalf("B returns to Results, got %d", h.m.screen)
	}
	h.press(input.Back) // Results go back to Search
	if h.m.screen != screenSearch {
		t.Fatalf("screen %d", h.m.screen)
	}
}

func queueHarness(t *testing.T) (*harness, *guitest.Fake) {
	t.Helper()
	svc := zeldaService(3)
	h := started(t, svc, 1280, 720)
	return h, svc
}

func TestQueueListsActiveQueuedThenFinishedAndShowsTheStateTexts(t *testing.T) {
	h := started(t, zeldaService(3), 1920, 1080)
	h.send(JobMsg{Job: app.JobSnapshot{ID: "1", Title: "Super Mario Bros. 3 (USA)", State: app.StateTransferring, Bytes: 222 << 10, Expected: 384 << 10, RateBps: 1 << 20}})
	h.send(JobMsg{Job: app.JobSnapshot{ID: "2", Title: "Metroid (USA)", State: app.StateWarming}})
	h.send(JobMsg{Job: app.JobSnapshot{ID: "3", Title: "Zelda II (USA)", State: app.StateQueued}})
	h.send(JobMsg{Job: app.JobSnapshot{ID: "4", Title: "Kirby (USA)", State: app.StateDone, Path: "/roms/nes", Bytes: 384 << 10, Started: h.now, Finished: h.now.Add(12 * time.Second)}})
	h.send(JobMsg{Job: app.JobSnapshot{ID: "5", Title: "Contra (USA)", State: app.StateFailed, Err: &app.Error{Code: app.CodeNetwork, Message: "x"}}})
	h.press(input.Queue)
	texts := h.texts()
	order := []string{"Super Mario Bros. 3 (USA)", "Metroid (USA)", "Zelda II (USA)", "Finished (2)", "Contra (USA)", "Kirby (USA)"}
	last := -1
	for _, want := range order {
		i := slices.Index(texts, want)
		if i < 0 || i < last {
			t.Fatalf("%q is out of order or missing in %q", want, texts)
		}
		last = i
	}
	h.mustHave("2 active · 1 queued", "Warming up the server... 00:00", "Queued")
	h.advance(31 * time.Second)
	h.mustHave("Warming up the server... 00:31", "The server is fetching this file from peers. This is normal.")
}

func focusJob(t *testing.T, h *harness, id app.JobID) {
	t.Helper()
	for i := 0; i < 10 && h.m.queue.focusJob != id; i++ {
		h.press(input.Down)
	}
	for i := 0; i < 10 && h.m.queue.focusJob != id; i++ {
		h.press(input.Up)
	}
	if h.m.queue.focusJob != id {
		t.Fatalf("could not focus job %s", id)
	}
}

func TestAOpensAnActionSheetByStateAndNeverCancelsDirectly(t *testing.T) {
	h, svc := queueHarness(t)
	h.send(JobMsg{Job: app.JobSnapshot{ID: "1", Slug: "zelda-001", Title: "Zelda 001", State: app.StateTransferring, Expected: 10, Bytes: 5}})
	h.send(JobMsg{Job: app.JobSnapshot{ID: "2", Slug: "zelda-002", Title: "Zelda 002", LinkIndex: 2, State: app.StateFailed, Err: &app.Error{Code: app.CodeNetwork, Message: "x"}}})
	h.send(JobMsg{Job: app.JobSnapshot{ID: "3", Slug: "zelda-003", Title: "Zelda 003", State: app.StateDone}})
	h.press(input.Queue)

	h.press(input.Confirm) // the active job
	h.mustHave("Cancel download", "Details", "Close")
	if svc.Count("cancel:") != 0 {
		t.Fatal("A must never cancel directly")
	}
	h.press(input.Confirm) // Cancel download
	if svc.Count("cancel:1") != 1 {
		t.Fatalf("calls %v", svc.Calls())
	}

	focusJob(t, h, "2")
	h.press(input.Confirm)
	h.mustHave("Retry", "Details", "Close")
	h.press(input.Confirm) // Retry re-queues with the same link
	if svc.Count("enqueue:zelda-002:2") != 1 {
		t.Fatalf("calls %v", svc.Calls())
	}

	focusJob(t, h, "3")
	h.press(input.Confirm)
	h.mustHave("Details", "Hide from list", "Close")
	h.press(input.Down)
	h.press(input.Confirm) // Hide from list
	if slices.Contains(h.texts(), "Zelda 003") {
		t.Fatal("the finished job must be hidden")
	}
}

func TestYOpensDetailsAndBClosesThem(t *testing.T) {
	h, _ := queueHarness(t)
	h.send(JobMsg{Job: app.JobSnapshot{ID: "1", Slug: "zelda-001", Title: "Zelda 001", Name: "zelda-001.zip", Path: "/roms/snes/zelda-001.zip", State: app.StateDone,
		Bytes: 1 << 20, Expected: 1 << 20, SHA256: "0123456789abcdef0123456789abcdef", Attempt: 2, Started: h.now, Finished: h.now.Add(75 * time.Second)}})
	h.press(input.Queue)
	h.press(input.Info)
	h.mustHave("File: zelda-001.zip", "Path: /roms/snes/zelda-001.zip", "SHA-256: 0123456789abcdef…", "Attempts: 2", "Duration: 01:15")
	h.press(input.Back)
	h.mustNotHave("File: zelda-001.zip")
	if h.m.screen != screenQueue {
		t.Fatal("B closes the details, not the Queue")
	}
}

func TestQueueMenuActionsCancelAllAsksAndHideFinishedHides(t *testing.T) {
	h, svc := queueHarness(t)
	h.send(JobMsg{Job: app.JobSnapshot{ID: "1", Title: "A", State: app.StateTransferring, Expected: 10}})
	h.send(JobMsg{Job: app.JobSnapshot{ID: "2", Title: "B", State: app.StateQueued}})
	h.send(JobMsg{Job: app.JobSnapshot{ID: "3", Title: "C", State: app.StateDone}})
	h.press(input.Queue)
	h.press(input.Menu)
	h.mustHave("Cancel all downloads", "Hide finished")
	focusMenu := func(id string) {
		for i := 0; i < 12 && h.m.menuFocus != id; i++ {
			h.press(input.Down)
		}
	}
	focusMenu(itemCancelAll)
	h.press(input.Confirm)
	h.mustHave("Cancel 2 downloads?", "Keep", "Cancel all")
	if h.m.dlgFocus != btnKeep {
		t.Fatalf("default %q", h.m.dlgFocus)
	}
	h.advance(200 * time.Millisecond)
	h.press(input.Right)
	h.press(input.Confirm)
	if svc.Count("cancel:1") != 1 || svc.Count("cancel:2") != 1 || svc.Count("cancel:3") != 0 {
		t.Fatalf("calls %v: both unfinished jobs, and only them", svc.Calls())
	}

	h.press(input.Menu)
	focusMenu(itemHideDone)
	h.press(input.Confirm)
	h.mustNotHave("C")
}

func TestToastsForTheBatchSummaryAndForFailuresOffTheQueue(t *testing.T) {
	h, _ := queueHarness(t)
	h.send(JobMsg{Job: app.JobSnapshot{ID: "1", Title: "A", State: app.StateTransferring, Expected: 10}})
	h.send(JobMsg{Job: app.JobSnapshot{ID: "2", Title: "B", State: app.StateQueued}})
	h.send(JobMsg{Job: app.JobSnapshot{ID: "1", Title: "A", State: app.StateDone}})
	h.send(JobMsg{Job: app.JobSnapshot{ID: "2", Title: "B", State: app.StateSkipped}})
	h.mustHave("1 downloaded, 1 skipped, 0 failed")
	h.advance(4 * time.Second)

	h.send(JobMsg{Job: app.JobSnapshot{ID: "3", Title: "C", State: app.StateFailed, Err: &app.Error{Code: app.CodeNetwork, Message: "x"}}})
	h.mustHave("Download failed: C")
	h.advance(4 * time.Second)
	h.press(input.Queue)
	h.send(JobMsg{Job: app.JobSnapshot{ID: "4", Title: "D", State: app.StateFailed, Err: &app.Error{Code: app.CodeNetwork, Message: "x"}}})
	h.mustNotHave("Download failed: D") // the person is on the Queue and sees the row
}

func TestBoxartIsSavedAfterADoneJobWhenTheOptionIsOn(t *testing.T) {
	svc := zeldaService(1)
	h := started(t, svc, 1280, 720, func(o *Options) { o.Boxart = true })
	h.send(JobMsg{Job: app.JobSnapshot{ID: "1", Slug: "zelda-001", Title: "Z", State: app.StateDone}})
	if svc.Count("boxart:1") != 1 {
		t.Fatalf("calls %v", svc.Calls())
	}
	off := started(t, svc, 1280, 720)
	off.send(JobMsg{Job: app.JobSnapshot{ID: "2", Slug: "zelda-001", Title: "Z", State: app.StateDone}})
	if svc.Count("boxart:2") != 0 {
		t.Fatal("download.boxart off: no call")
	}
}

// PR-71 UX review, decision 6: Quit when finished opens the Queue.
func TestQuitWhenFinishedOpensTheQueue(t *testing.T) {
	svc := zeldaService(1)
	svc.JobList = []app.JobSnapshot{activeJob("1")}
	h := started(t, svc, 1280, 720)
	h.press(input.Back)
	h.advance(200 * time.Millisecond)
	h.press(input.Right)
	h.press(input.Confirm)
	if h.m.screen != screenQueue || !h.m.quitPending {
		t.Fatalf("screen %d pending %v", h.m.screen, h.m.quitPending)
	}
	h.mustHave("Will quit when downloads finish", "Contra (USA)")
}

// Nothing is drawn outside the safe rectangle on the new screens, at the three sizes, with large text.
func TestTheNewScreensStayInsideTheSafeRectangle(t *testing.T) {
	for _, size := range [][2]int{{640, 480}, {1280, 720}, {1920, 1080}} {
		for _, large := range []bool{false, true} {
			svc := zeldaService(40)
			svc.Covers = map[string]bool{"zelda-001": true}
			svc.Records = []app.HistoryRecord{{State: "done", Slug: "zelda-002", LinkIndex: 1}}
			scenes := map[string]func(h *harness){
				"search": func(h *harness) {
					h.press(input.Confirm)
					h.send(TextMsg{Text: "the legend of zelda a link to the past and more"})
				},
				"picker": func(h *harness) { h.press(input.Confirm); h.press(input.Up); h.press(input.Up); h.press(input.Confirm) },
				"results": func(h *harness) {
					searchFor(t, h, "zelda")
					h.advance(300 * time.Millisecond)
				},
				"entry": func(h *harness) { searchFor(t, h, "zelda"); h.press(input.Confirm); h.advance(300 * time.Millisecond) },
				"queue": func(h *harness) {
					for i, st := range []app.State{app.StateTransferring, app.StateWarming, app.StateQueued, app.StateDone, app.StateFailed} {
						h.send(JobMsg{Job: app.JobSnapshot{ID: app.JobID(fmt.Sprint(i)), Title: "A fairly long game title (USA) (Rev 1)", State: st, Bytes: 5, Expected: 10, Path: "/userdata/roms/nes/a/long/folder", Err: failureFor(st)}})
					}
					h.press(input.Queue)
					h.advance(40 * time.Second)
				},
				"sheet": func(h *harness) {
					h.send(JobMsg{Job: app.JobSnapshot{ID: "1", Title: "A", State: app.StateQueued}})
					h.press(input.Queue)
					h.press(input.Confirm)
				},
				"details": func(h *harness) {
					h.send(JobMsg{Job: app.JobSnapshot{ID: "1", Title: "A", State: app.StateDone, Path: "/a/b"}})
					h.press(input.Queue)
					h.press(input.Info)
				},
				"error": func(h *harness) {
					svc.SearchErr = &app.Error{Code: app.CodeNetwork, Message: "down"}
					searchFor(t, h, "zelda")
				},
			}
			for name, setup := range scenes {
				h := started(t, svc, size[0], size[1], func(o *Options) { o.LargeText = large })
				setup(h)
				assertInsideSafe(t, h, fmt.Sprintf("%s large=%v", name, large))
				svc.SearchErr = nil
			}
		}
	}
}

func failureFor(st app.State) *app.Error {
	if st == app.StateFailed {
		return &app.Error{Code: app.CodeNetwork, Message: "x"}
	}
	return nil
}

// Retry on a failed Results replaces the screen: the stack does not grow, and B goes back to where it came from.
func TestRetryOnResultsDoesNotStackScreens(t *testing.T) {
	svc := zeldaService(3)
	svc.SearchErr = &app.Error{Code: app.CodeNetwork, Message: "down"}
	h := started(t, svc, 1280, 720)
	searchFor(t, h, "zelda")
	depth := len(h.m.back)
	h.press(input.Confirm) // fails again
	h.press(input.Confirm)
	if len(h.m.back) != depth {
		t.Fatalf("stack depth %d, want %d: a retry must not stack screens", len(h.m.back), depth)
	}
	svc.SearchErr = nil
	h.press(input.Confirm)
	h.mustHave("Zelda 001")
	h.press(input.Back)
	if h.m.screen != screenSearch {
		t.Fatalf("B returns to Search, got %d", h.m.screen)
	}
}

// Y on Results reached from a recent search (no Search screen below) shows Search filled in, and B goes Home.
func TestEditSearchFromARecentChipReturnsToHome(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tui_state"), []byte(`{"recent":["zelda"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h := started(t, zeldaService(3), 1280, 720, func(o *Options) { o.DataDir = dir })
	h.press(input.Down)
	h.press(input.Down) // the chip
	h.press(input.Confirm)
	if h.m.screen != screenResults {
		t.Fatalf("screen %d", h.m.screen)
	}
	h.press(input.Info)
	if h.m.screen != screenSearch || h.m.search.field.String() != "zelda" {
		t.Fatalf("screen %d text %q", h.m.screen, h.m.search.field.String())
	}
	for i := 0; i < 6; i++ {
		h.press(input.Back) // five deletions, then leave
	}
	if h.m.screen != screenHome {
		t.Fatalf("screen %d: B returns to Home", h.m.screen)
	}
}
