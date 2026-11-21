package tui

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	"github.com/Or4cu1o/mothdex-cli/locales"
	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

var errBoom = errors.New("boom")

func entryOf(slug string) mothdex.Entry { return mothdex.Entry{Slug: slug, Title: "Title " + slug} }

// kp builds the key press a terminal would send for the key name used in the key map.
func kp(name string) tea.KeyPressMsg {
	switch name {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "pgup":
		return tea.KeyPressMsg{Code: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyPressMsg{Code: tea.KeyPgDown}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	case "ctrl+u":
		return tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}
	}
	r := []rune(name)
	return tea.KeyPressMsg{Code: r[0], Text: name}
}

func TestReduce(t *testing.T) {
	tests := []struct {
		name  string
		sc    screenID
		focus focusKind
		key   string
		root  bool
		want  action
	}{
		// a text field owns the letters
		{"a letter in a field is text", scSearch, focusText, "q", true, actText},
		{"Q in a field is text", scSearch, focusText, "Q", true, actText},
		{"H in a field is text", scSearch, focusText, "H", true, actText},
		{"S in a field is text", scSearch, focusText, "S", true, actText},
		{"slash in a field is text", scSearch, focusText, "/", true, actText},
		{"space in a field is text", scSearch, focusText, "space", true, actText},
		{"Esc leaves the field", scSearch, focusText, "esc", true, actBlur},
		{"Enter submits the field", scSearch, focusText, "enter", true, actEnter},
		{"Tab goes to the next field", scSearch, focusText, "tab", true, actNextField},
		{"Shift+Tab goes back", scSearch, focusText, "shift+tab", true, actPrevField},
		{"Down leaves the field for the list", scSearch, focusText, "down", true, actDown},
		{"Ctrl+C quits even in a field", scSearch, focusText, "ctrl+c", true, actQuit},
		// q quits on the root screen only
		{"q on the root screen quits", scSearch, focusNone, "q", true, actQuit},
		{"q on another screen does nothing", scResults, focusNone, "q", false, actNone},
		{"Ctrl+C anywhere quits", scEntry, focusNone, "ctrl+c", false, actQuit},
		// the global keys of tui.md section 8
		{"Q opens the queue", scResults, focusNone, "Q", false, actQueue},
		{"shift+q is Q", scResults, focusNone, "shift+q", false, actQueue},
		{"H opens the history", scEntry, focusNone, "H", false, actHistory},
		{"S opens the settings", scQueue, focusNone, "S", false, actSettings},
		{"? opens help", scSearch, focusNone, "?", true, actHelp},
		{"slash goes to Search", scResults, focusNone, "/", false, actSearchScreen},
		{"j moves down", scResults, focusNone, "j", false, actDown},
		{"k moves up", scResults, focusNone, "k", false, actUp},
		{"g goes first", scResults, focusNone, "g", false, actFirst},
		{"G goes last", scResults, focusNone, "G", false, actLast},
		{"Space toggles", scResults, focusNone, "space", false, actToggle},
		{"Esc goes back", scEntry, focusNone, "esc", false, actBack},
		{"Ctrl+L redraws", scResults, focusNone, "ctrl+l", false, actRedraw},
		{"d downloads", scEntry, focusNone, "d", false, actDownload},
		// letters that mean different things per screen
		{"r on Search is random", scSearch, focusNone, "r", true, actRandom},
		{"Tab cycles the Search fields", scSearch, focusNone, "tab", true, actNextField},
		{"Shift+Tab cycles back", scSearch, focusNone, "shift+tab", true, actPrevField},
		{"r on Results retries", scResults, focusNone, "r", false, actRetry},
		{"r on Queue retries", scQueue, focusNone, "r", false, actRetry},
		{"r on Entry retries", scEntry, focusNone, "r", false, actRetry},
		{"s sorts Results", scResults, focusNone, "s", false, actSort},
		{"s does nothing on Search", scSearch, focusNone, "s", true, actNone},
		{"n pages forward", scResults, focusNone, "n", false, actNextPage},
		{"p pages back", scResults, focusNone, "p", false, actPrevPage},
		{"PgDn pages Results", scResults, focusNone, "pgdown", false, actNextPage},
		{"PgDn scrolls elsewhere", scEntry, focusNone, "pgdown", false, actPageDown},
		{"n does nothing on Entry", scEntry, focusNone, "n", false, actNone},
		{"D downloads the best link on Entry", scEntry, focusNone, "D", false, actDownloadBest},
		{"D does nothing on Results", scResults, focusNone, "D", false, actNone},
		{"c is the cover on Entry", scEntry, focusNone, "c", false, actCover},
		{"c clears finished on Queue", scQueue, focusNone, "c", false, actClearFinished},
		{"x cancels a job", scQueue, focusNone, "x", false, actCancel},
		{"X cancels all", scQueue, focusNone, "X", false, actCancelAll},
		{"x does nothing elsewhere", scResults, focusNone, "x", false, actNone},
		{"Left is the previous entry on Entry", scEntry, focusNone, "left", false, actPrevEntry},
		{"] is the next entry on Entry", scEntry, focusNone, "]", false, actNextEntry},
		{"Left is just left elsewhere", scQueue, focusNone, "left", false, actLeft},
		{"P opens preferences on Entry", scEntry, focusNone, "P", false, actPrefer},
		{"f filters failed on History", scHistory, focusNone, "f", false, actFailedOnly},
		{"e exports on History", scHistory, focusNone, "e", false, actExport},
		// a [y/N] question
		{"y confirms", scEntry, focusPrompt, "y", false, actYes},
		{"Y confirms", scEntry, focusPrompt, "Y", false, actYes},
		{"n declines", scEntry, focusPrompt, "n", false, actNo},
		{"Enter declines: the default is No", scEntry, focusPrompt, "enter", false, actNo},
		{"Esc declines", scEntry, focusPrompt, "esc", false, actNo},
		{"another key does nothing", scEntry, focusPrompt, "x", false, actNone},
		{"Ctrl+C still quits", scEntry, focusPrompt, "ctrl+c", false, actQuit},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := reduce(tc.sc, tc.focus, tc.key, tc.root); got != tc.want {
				t.Fatalf("reduce(%d, %d, %q, %v) = %d, want %d", tc.sc, tc.focus, tc.key, tc.root, got, tc.want)
			}
		})
	}
}

func TestEveryKeyHelpLabelIsTranslated(t *testing.T) {
	bundle, err := i18n.Load(locales.FS)
	if err != nil {
		t.Fatal(err)
	}
	tr := bundle.New("en_us")
	for sc := scSearch; sc <= scSettings; sc++ {
		for _, k := range helpFor(sc) {
			if !tr.Has("ui.tui.key." + k.label) {
				t.Errorf("screen %d: no text for ui.tui.key.%s", sc, k.label)
			}
		}
	}
}

func TestTierAndColumns(t *testing.T) {
	all := []col{colMark, colIndex, colTitle, colPlatform, colRegions, colLinks, colSize}
	few := []col{colMark, colIndex, colTitle, colPlatform}
	tests := []struct {
		name string
		w, h int
		tier sizeTier
		cols []col
	}{
		{"the reference size", 80, 24, tierFull, all},
		{"wide and tall", 200, 60, tierFull, all},
		{"79 columns drops three columns", 79, 24, tierNarrow, few},
		{"the minimum", 60, 16, tierShort, few},
		{"19 rows is short", 80, 19, tierShort, all},
		{"20 rows is not", 80, 20, tierFull, all},
		{"59 columns is too small", 59, 24, tierTooSmall, nil},
		{"15 rows is too small", 80, 15, tierTooSmall, nil},
		{"54x14", 54, 14, tierTooSmall, nil},
		{"nothing yet", 0, 0, tierTooSmall, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tier(tc.w, tc.h)
			if got != tc.tier {
				t.Fatalf("tier(%d,%d) = %d, want %d", tc.w, tc.h, got, tc.tier)
			}
			if cols := columns(got, tc.w); !reflect.DeepEqual(cols, tc.cols) {
				t.Fatalf("columns = %v, want %v", cols, tc.cols)
			}
		})
	}
	if statusKeyLimit(80) != statusFull || statusKeyLimit(79) != statusTiny {
		t.Fatal("the status bar shows 8 keys at 80 columns and 4 below")
	}
}

func snap(id, state string) app.JobSnapshot {
	return app.JobSnapshot{ID: app.JobID(id), Slug: "s" + id, Title: "T" + id, State: app.State(state)}
}

func TestJobBook(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	b := newJobBook()
	if _, _, _, ok := b.settled(); ok {
		t.Fatal("an empty book has nothing to summarize")
	}
	b = b.apply(snap("1", "queued"), now).apply(snap("2", "queued"), now).apply(snap("3", "queued"), now)
	if a, q := b.counts(); a != 0 || q != 3 || b.busy() != 3 {
		t.Fatalf("counts %d %d busy %d", a, q, b.busy())
	}
	b = b.apply(snap("1", "transferring"), now).apply(snap("2", "warming"), now)
	if a, q := b.counts(); a != 2 || q != 1 {
		t.Fatalf("counts %d %d", a, q)
	}
	if !b.anyWarming() {
		t.Fatal("job 2 is warming")
	}
	// a later warming event keeps the first time: the counter must not restart
	b = b.apply(snap("2", "warming"), now.Add(time.Minute))
	if got := b.warmSince["2"]; !got.Equal(now) {
		t.Fatalf("warm since moved to %v", got)
	}
	b = b.apply(snap("1", "done"), now).apply(snap("2", "failed"), now)
	if _, _, _, ok := b.settled(); ok {
		t.Fatal("job 3 is still queued")
	}
	if b.anyWarming() {
		t.Fatal("a failed job is not warming")
	}
	b = b.apply(snap("3", "skipped"), now)
	done, skipped, failed, ok := b.settled()
	if !ok || done != 1 || skipped != 1 || failed != 1 {
		t.Fatalf("settled %d %d %d %v", done, skipped, failed, ok)
	}
	// seeing a terminal state again does not count it twice
	b = b.apply(snap("1", "done"), now)
	if d, _, _, _ := b.settled(); d != 1 {
		t.Fatalf("done counted twice: %d", d)
	}
	if _, _, _, ok := b.resetBatch().settled(); ok {
		t.Fatal("a summary was shown: the count starts again")
	}
}

func TestJobBookIsAValue(t *testing.T) {
	now := time.Now()
	a := newJobBook().apply(snap("1", "queued"), now)
	b := a.apply(snap("2", "queued"), now)
	if len(a.order) != 1 || len(b.order) != 2 {
		t.Fatalf("apply changed its receiver: %d, %d", len(a.order), len(b.order))
	}
	c := a.withRequests([]app.JobID{"1"}, []app.DownloadRequest{{Ref: app.Ref{Slug: "x"}}})
	if len(a.reqs) != 0 || c.reqs["1"].Ref.Slug != "x" {
		t.Fatal("withRequests changed its receiver")
	}
}

func TestQuitNeedsAsk(t *testing.T) {
	for busy, want := range map[int]bool{0: false, 1: true, 5: true} {
		if got := quitNeedsAsk(busy); got != want {
			t.Errorf("quitNeedsAsk(%d) = %v", busy, got)
		}
	}
}

func TestProgressBar(t *testing.T) {
	g := newGlyphs(false)
	tests := []struct {
		name            string
		bytes, expected int64
		pct, full       int
	}{
		{"nothing yet", 0, 100, 0, 0},
		{"half", 50, 100, 50, 12},
		{"done", 100, 100, 100, 24},
		{"more than expected is clamped", 150, 100, 100, 24},
		{"unknown size", 50, 0, 0, 0},
		{"negative is clamped", -5, 100, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bar, pct := progressBar(barWidth, tc.bytes, tc.expected, g)
			if pct != tc.pct || strings.Count(bar, g.bar) != tc.full || len(bar) != barWidth {
				t.Fatalf("bar %q pct %d, want %d full and %d%%", bar, pct, tc.full, tc.pct)
			}
		})
	}
}

func TestEta(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 10, 0, time.UTC)
	started := now.Add(-5 * time.Second)
	tests := []struct {
		name  string
		job   app.JobSnapshot
		since time.Time
		want  string
	}{
		{"before three seconds of transfer", app.JobSnapshot{Bytes: 100, Expected: 1000, RateBps: 100}, now.Add(-2 * time.Second), "--:--"},
		{"no rate yet", app.JobSnapshot{Bytes: 100, Expected: 1000}, started, "--:--"},
		{"unknown size", app.JobSnapshot{Bytes: 100, RateBps: 100}, started, "--:--"},
		{"never started", app.JobSnapshot{Bytes: 100, Expected: 1000, RateBps: 100}, time.Time{}, "--:--"},
		{"nine seconds left", app.JobSnapshot{Bytes: 100, Expected: 1000, RateBps: 100}, started, "00:09"},
		{"over a minute", app.JobSnapshot{Bytes: 0, Expected: 6000, RateBps: 100}, started, "01:00"},
		{"already complete", app.JobSnapshot{Bytes: 1000, Expected: 1000, RateBps: 100}, started, "00:00"},
	}
	for _, tc := range tests {
		if got := eta(tc.job, tc.since, now); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestMarks(t *testing.T) {
	rows := []row{{e: entryOf("a")}, {e: entryOf("b")}, {e: entryOf("c")}}
	m := marks{}
	m1 := m.toggle(1, "c").toggle(1, "a")
	if len(m) != 0 {
		t.Fatal("toggle changed its receiver")
	}
	if got := pick(m1, 1, rows, 1); !reflect.DeepEqual(got, []string{"a", "c"}) {
		t.Fatalf("marked rows come in list order: %v", got)
	}
	if got := pick(m1, 2, rows, 1); !reflect.DeepEqual(got, []string{"b"}) {
		t.Fatalf("marks belong to a page; with none the cursor row is used: %v", got)
	}
	m2 := m1.toggle(1, "a")
	if m2.has(1, "a") || !m1.has(1, "a") || m2.count(1) != 1 {
		t.Fatal("toggling twice must unmark, without touching the earlier value")
	}
	if got := pick(marks{}, 1, nil, 0); len(got) != 0 {
		t.Fatalf("nothing to act on: %v", got)
	}
}

func TestSortCycle(t *testing.T) {
	want := []app.SortKey{app.SortTitle, app.SortSize, app.SortRelevance, app.SortTitle}
	k := app.SortRelevance
	for i, w := range want {
		if k = nextSort(k); k != w {
			t.Fatalf("step %d: %s, want %s", i, k, w)
		}
	}
}

func TestScrollTop(t *testing.T) {
	tests := []struct {
		name                string
		top, cur, height, n int
		want                int
	}{
		{"everything fits", 0, 3, 10, 5, 0},
		{"cursor inside the window", 2, 4, 5, 20, 2},
		{"cursor below the window scrolls down", 0, 7, 5, 20, 3},
		{"cursor above the window scrolls up", 6, 2, 5, 20, 2},
		{"the last row", 0, 19, 5, 20, 15},
		{"no height", 3, 3, 0, 20, 0},
	}
	for _, tc := range tests {
		if got := scrollTop(tc.top, tc.cur, tc.height, tc.n); got != tc.want {
			t.Errorf("%s: %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestAddRecent(t *testing.T) {
	tests := []struct {
		name   string
		recent []string
		q      string
		want   []string
	}{
		{"first query", nil, "zelda", []string{"zelda"}},
		{"newest first", []string{"zelda"}, "mario", []string{"mario", "zelda"}},
		{"a repeat moves to the front", []string{"a", "b", "c"}, "c", []string{"c", "a", "b"}},
		{"only five are kept", []string{"1", "2", "3", "4", "5"}, "6", []string{"6", "1", "2", "3", "4"}},
	}
	for _, tc := range tests {
		if got := addRecent(tc.recent, tc.q); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestPicker(t *testing.T) {
	items := []pickItem{{"nes", "Nintendo"}, {"snes", "Super Nintendo"}, {"gba", "Game Boy Advance"}, {"gbc", "Game Boy Color"}}
	if got := filterItems(items, "  BOY"); len(got) != 2 || got[0].id != "gba" {
		t.Fatalf("filter by label, ignoring case: %v", got)
	}
	if got := filterItems(items, "snes"); len(got) != 1 {
		t.Fatalf("filter by id: %v", got)
	}
	if got := filterItems(items, ""); len(got) != 4 {
		t.Fatalf("no filter: %v", got)
	}

	p := newPicker("Platform", items, []string{"nes"}, false)
	p, res := p.handle(kp("down"))
	p, _ = p.handle(kp("space")) // marks snes
	p, _ = p.handle(kp("down"))
	p, _ = p.handle(kp("down"))
	p, _ = p.handle(kp("down")) // already on the last row: stays
	if p.cur != 3 || res != pickNone {
		t.Fatalf("cursor %d", p.cur)
	}
	if got := p.selected(); !reflect.DeepEqual(got, []string{"nes", "snes"}) {
		t.Fatalf("selected in list order: %v", got)
	}
	p, _ = p.handle(kp("up"))
	p, _ = p.handle(kp("space"))
	if got := p.selected(); !reflect.DeepEqual(got, []string{"nes", "snes", "gba"}) {
		t.Fatalf("selected: %v", got)
	}
	p, _ = p.handle(kp("g")) // typing filters and returns the cursor to the top
	p, _ = p.handle(kp("b"))
	if vis := p.visible(); len(vis) != 2 || p.cur != 0 {
		t.Fatalf("visible %v cursor %d", vis, p.cur)
	}
	if _, res := p.handle(kp("enter")); res != pickConfirm {
		t.Fatal("Enter confirms")
	}
	if _, res := p.handle(kp("esc")); res != pickCancel {
		t.Fatal("Esc cancels")
	}
	empty := newPicker("Region", nil, nil, true).withItems(nil)
	if empty.loading || empty.move(1).cur != 0 || len(empty.selected()) != 0 {
		t.Fatal("an empty picker stays quiet")
	}
}

func TestSplitIDs(t *testing.T) {
	for in, want := range map[string][]string{
		"snes, nes": {"snes", "nes"}, "snes nes,gba": {"snes", "nes", "gba"}, "  ": nil, "": nil, "a,,b": {"a", "b"},
	} {
		if got := splitIDs(in); !reflect.DeepEqual(got, want) {
			t.Errorf("splitIDs(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestPreferenceForm(t *testing.T) {
	base := app.Preference{Languages: []string{"En", "Pt"}, Formats: []string{"gbc"}, Size: app.SizeSmallest}
	o := newPrefOverlay(base)
	got, ok := o.preference()
	if !ok || !reflect.DeepEqual(got, base) {
		t.Fatalf("the form round-trips: %+v, %v", got, ok)
	}
	tests := []struct {
		size string
		want app.SizePref
		ok   bool
	}{
		{"", app.SizeNone, true}, {"none", app.SizeNone, true}, {" LARGEST ", app.SizeLargest, true},
		{"smallest", app.SizeSmallest, true}, {"biggest", "", false},
	}
	for _, tc := range tests {
		o.in[3].SetValue(tc.size)
		p, ok := o.preference()
		if ok != tc.ok || (ok && p.Size != tc.want) {
			t.Errorf("size %q: %v %v, want %v %v", tc.size, p.Size, ok, tc.want, tc.ok)
		}
	}
	if s := preferSummary(app.Preference{Languages: []string{"En"}, Size: app.SizeLargest, Sources: []string{"Myrient"}}); s != "lang=En source=Myrient size=largest" {
		t.Fatalf("summary %q", s)
	}
	if preferSummary(app.Preference{}) != "" {
		t.Fatal("no preference, no summary")
	}
}

func TestDefaultChoice(t *testing.T) {
	tests := []struct {
		name    string
		choices []app.LinkChoice
		want    int
	}{
		{"first downloadable in rank order", []app.LinkChoice{{Index: 3, Available: true}, {Index: 1, Available: true}}, 3},
		{"a link that cannot be downloaded is skipped", []app.LinkChoice{{Index: 2}, {Index: 4, Available: true}}, 4},
		{"none downloadable: the first", []app.LinkChoice{{Index: 2}, {Index: 4}}, 2},
		{"no links", nil, 0},
	}
	for _, tc := range tests {
		if got := defaultChoice(tc.choices); got != tc.want {
			t.Errorf("%s: %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestFormatting(t *testing.T) {
	for n, want := range map[int64]string{0: "0 B", 500: "500 B", 2048: "2 KB", 1572864: "1.5 MB", 3221225472: "3.0 GB", -1: ""} {
		if got := humanSize(n); got != want {
			t.Errorf("humanSize(%d) = %q, want %q", n, got, want)
		}
	}
	for d, want := range map[time.Duration]string{0: "00:00", 75 * time.Second: "01:15", 3725 * time.Second: "1:02:05", -time.Second: "00:00"} {
		if got := clock(d); got != want {
			t.Errorf("clock(%v) = %q, want %q", d, got, want)
		}
	}
	for d, want := range map[time.Duration]string{time.Second: "1m", 5 * time.Minute: "5m", 3 * time.Hour: "3h", 72 * time.Hour: "3d"} {
		if got := age(d); got != want {
			t.Errorf("age(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestFailureOfLocalizesAndCleans(t *testing.T) {
	bundle, err := i18n.Load(locales.FS)
	if err != nil {
		t.Fatal(err)
	}
	e := &env{t: bundle.New("en_us"), logPath: "/tmp/mothdex.log"}
	f := e.failureOf(&app.Error{Code: app.CodeNotFound, Message: "no entry", Args: map[string]any{"ref": "x\x1b[2Jy"}})
	if f.code != "not_found" || !strings.Contains(f.msg, "No entry found for") || strings.ContainsRune(f.msg, 0x1b) {
		t.Fatalf("%+v", f)
	}
	if !strings.Contains(f.hint, "Check the spelling") {
		t.Fatalf("hint %q", f.hint)
	}
	// a placeholder with no value falls back to the service text, never a raw {name}
	f = e.failureOf(&app.Error{Code: app.CodeIndexOutOfRange, Message: "service text"})
	if f.msg != "service text" {
		t.Fatalf("msg %q", f.msg)
	}
	// something that is not an app error is an internal error
	if f = e.failureOf(errBoom); f.code != app.CodeInternal || !strings.Contains(f.hint, "/tmp/mothdex.log") {
		t.Fatalf("%+v", f)
	}
}
