package tui

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	"github.com/Or4cu1o/mothdex-cli/locales"
	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

// fakeSvc is a service with one hook per call the tests care about, and a record of what was asked.
type fakeSvc struct {
	mu        sync.Mutex
	search    func(ctx context.Context, q app.SearchQuery) (app.SearchPage, error)
	entry     func(slug string) (mothdex.Entry, error)
	links     func(slug string, sel app.LinkSelector) ([]app.LinkChoice, error)
	random    func() ([]mothdex.Entry, error)
	platforms func() ([]mothdex.Platform, error)
	boxart    func() (string, error)
	history   []app.HistoryRecord
	jobs      []app.JobSnapshot

	configs      []app.Setting
	configSet    func(key, value string) error
	cache        app.CacheStats
	jobCoverErr  error
	enqueueErr   error
	sets         [][2]string
	resets       []string
	cacheCleared int
	jobCovers    []app.JobSnapshot
	histFilters  []app.HistoryFilter

	searches  []app.SearchQuery
	linkSels  []app.LinkSelector
	entries   []string
	enqueued  []app.DownloadRequest
	cancelled []app.JobID
}

func (f *fakeSvc) Search(ctx context.Context, q app.SearchQuery) (app.SearchPage, error) {
	f.mu.Lock()
	f.searches = append(f.searches, q)
	hook := f.search
	f.mu.Unlock()
	if hook != nil {
		return hook(ctx, q)
	}
	return pageOf(q), nil
}

// pageOf is the default answer: two results, page q.Page of 4.
func pageOf(q app.SearchQuery) app.SearchPage {
	zelda := mothdex.Entry{Slug: "zelda", Title: "Legend of Zelda", Platform: "snes", Regions: []string{"us", "eu"},
		Links: []mothdex.Link{{Type: "Game", SizeStr: "1.0 MB"}, {Type: "Game"}, {Type: "Demo"}}}
	mario := mothdex.Entry{Slug: "mario", Title: "Super Mario", Platform: "nes", Regions: []string{"us"}, Links: []mothdex.Link{{Type: "Game", SizeStr: "384 KB"}}}
	page := max(q.Page, 1)
	return app.SearchPage{Query: q, Page: page, TotalPages: 4, TotalResults: 37, Results: []app.IndexedEntry{{Index: 1, Entry: zelda}, {Index: 2, Entry: mario}}}
}

func (f *fakeSvc) Entry(_ context.Context, ref app.Ref) (app.Result[mothdex.Entry], error) {
	f.mu.Lock()
	f.entries = append(f.entries, ref.Slug)
	hook := f.entry
	f.mu.Unlock()
	if hook != nil {
		e, err := hook(ref.Slug)
		return app.Result[mothdex.Entry]{Value: e}, err
	}
	return app.Result[mothdex.Entry]{Value: mothdex.Entry{Slug: ref.Slug, Title: "Entry " + ref.Slug, Platform: "gbc", Regions: []string{"us"}}}, nil
}

func (f *fakeSvc) Links(_ context.Context, ref app.Ref, sel app.LinkSelector) ([]app.LinkChoice, error) {
	f.mu.Lock()
	f.linkSels = append(f.linkSels, sel)
	hook := f.links
	f.mu.Unlock()
	if hook != nil {
		return hook(ref.Slug, sel)
	}
	return []app.LinkChoice{
		{Index: 1, Available: true, Link: mothdex.Link{Type: "Game", Format: "gbc", SizeStr: "1.0 MB", Extras: []string{"En"}, SourceName: "Myrient"}, Reasons: []string{"lang=En"}},
		{Index: 2, Available: true, Link: mothdex.Link{Type: "Game", Format: "gbc", SizeStr: "1.0 MB", SourceName: "Myrient"}},
		{Index: 3, Available: false, Link: mothdex.Link{Type: "Demo", SourceName: "Evil"}, Reasons: []string{"host not allowed: evil.example"}},
	}, nil
}

func (f *fakeSvc) Random(context.Context, app.RandomQuery) (app.Result[[]mothdex.Entry], error) {
	if f.random != nil {
		e, err := f.random()
		return app.Result[[]mothdex.Entry]{Value: e}, err
	}
	return app.Result[[]mothdex.Entry]{Value: []mothdex.Entry{entryOf("r1"), entryOf("r2"), entryOf("r3")}}, nil
}

func (f *fakeSvc) Platforms(context.Context, app.PlatformFilter) (app.Result[[]mothdex.Platform], error) {
	if f.platforms != nil {
		p, err := f.platforms()
		return app.Result[[]mothdex.Platform]{Value: p}, err
	}
	return app.Result[[]mothdex.Platform]{Value: []mothdex.Platform{{ID: "nes", Name: "Nintendo"}, {ID: "snes", Name: "Super Nintendo"}, {ID: "gba", Name: "Game Boy Advance"}}}, nil
}

func (f *fakeSvc) Regions(context.Context) (app.Result[[]mothdex.Region], error) {
	return app.Result[[]mothdex.Region]{Value: []mothdex.Region{{ID: "us", Name: "USA"}, {ID: "eu", Name: "Europe"}}}, nil
}

func (f *fakeSvc) DBInfo(context.Context) (app.Result[mothdex.DBInfo], error) {
	return app.Result[mothdex.DBInfo]{Value: mothdex.DBInfo{TotalEntries: 1284019}}, nil
}

func (f *fakeSvc) SaveBoxart(context.Context, app.Ref, string) (string, error) {
	if f.boxart != nil {
		return f.boxart()
	}
	return "/roms/cover.png", nil
}

// Enqueue accepts every request and shows it as a queued job, like the real service.
func (f *fakeSvc) Enqueue(_ context.Context, reqs []app.DownloadRequest) ([]app.JobID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.enqueueErr != nil {
		return nil, f.enqueueErr
	}
	ids := make([]app.JobID, len(reqs))
	for i, r := range reqs {
		f.enqueued = append(f.enqueued, r)
		ids[i] = app.JobID("q" + strconv.Itoa(len(f.enqueued)))
		link := max(r.Link.Index, 1)
		f.jobs = append(f.jobs, app.JobSnapshot{ID: ids[i], Slug: r.Ref.Slug, Title: "Title " + r.Ref.Slug, LinkIndex: link, State: app.StateQueued})
	}
	return ids, nil
}

func (f *fakeSvc) Jobs() []app.JobSnapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]app.JobSnapshot(nil), f.jobs...)
}

func (f *fakeSvc) Events(context.Context) <-chan app.Event {
	ch := make(chan app.Event)
	close(ch) // nothing arrives by itself: the tests send events as messages
	return ch
}

func (f *fakeSvc) Cancel(id app.JobID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancelled = append(f.cancelled, id)
	return nil
}

func (f *fakeSvc) History(_ context.Context, flt app.HistoryFilter) ([]app.HistoryRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.histFilters = append(f.histFilters, flt)
	if !flt.Failed {
		return f.history, nil
	}
	var out []app.HistoryRecord
	for _, r := range f.history {
		if r.Error != nil {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeSvc) ConfigList() ([]app.Setting, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]app.Setting(nil), f.configs...), nil
}

// ConfigSet stores the value like the service would: bools and ints are parsed, lists split.
func (f *fakeSvc) ConfigSet(key, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sets = append(f.sets, [2]string{key, value})
	if f.configSet != nil {
		if err := f.configSet(key, value); err != nil {
			return err
		}
	}
	for i, c := range f.configs {
		if c.Key != key {
			continue
		}
		switch c.Value.(type) {
		case bool:
			f.configs[i].Value = value == "true"
		case int:
			n, _ := strconv.Atoi(value)
			f.configs[i].Value = n
		case []string:
			f.configs[i].Value = splitIDs(value)
		default:
			f.configs[i].Value = value
		}
		f.configs[i].Source = "file"
	}
	return nil
}

func (f *fakeSvc) ConfigReset(key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resets = append(f.resets, key)
	return nil
}

func (f *fakeSvc) CacheStats() (app.CacheStats, error) { return f.cache, nil }

func (f *fakeSvc) CacheClear(...string) error {
	f.cacheCleared++
	return nil
}

func (f *fakeSvc) SaveJobBoxart(_ context.Context, j app.JobSnapshot, _ string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.jobCovers = append(f.jobCovers, j)
	return "/roms/x.png", f.jobCoverErr
}

func (f *fakeSvc) ActiveProfile() app.ProfileInfo { return app.ProfileInfo{ID: "generic"} }

// harness drives the model the way Bubble Tea would: messages in, commands run at once.
type harness struct {
	t    *testing.T
	m    model
	svc  *fakeSvc
	quit bool
}

type harnessOpt func(*env)

func testEnv(t *testing.T, svc *fakeSvc, opts ...harnessOpt) *env {
	t.Helper()
	bundle, err := i18n.Load(locales.FS)
	if err != nil {
		t.Fatal(err)
	}
	tr := bundle.New("en_us")
	e := &env{
		ctx: context.Background(), svc: svc, t: tr, st: newStyles(true, false), pageSize: 10, dataDir: t.TempDir(),
		now:   func() time.Time { return time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC) },
		after: func(time.Duration, tea.Msg) tea.Cmd { return nil }, // no timers: nothing waits on a clock
		seq:   &atomic.Int64{}, num: printer(tr),
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

func newHarness(t *testing.T, svc *fakeSvc, opts ...harnessOpt) *harness {
	t.Helper()
	e := testEnv(t, svc, opts...)
	h := &harness{t: t, m: newModel(e, newSearch(nil)), svc: svc}
	h.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	h.run(h.m.Init())
	return h
}

func (h *harness) send(msg tea.Msg) {
	next, cmd := h.m.Update(msg)
	h.m = next.(model)
	h.run(cmd)
}

func (h *harness) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch v := cmd().(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range v {
			h.run(c)
		}
	case tea.QuitMsg:
		h.quit = true
	default:
		h.send(v)
	}
}

func (h *harness) keys(names ...string) {
	for _, n := range names {
		h.send(kp(n))
	}
}

func (h *harness) typeText(s string) {
	for _, r := range s {
		h.send(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func (h *harness) view() string { return ansi.Strip(h.m.render()) }

func (h *harness) resize(w, hh int) { h.send(tea.WindowSizeMsg{Width: w, Height: hh}) }

func (h *harness) top() screenID { return h.m.top().id() }

// search runs a search for text and waits for the results.
func (h *harness) search(text string) {
	h.typeText(text)
	h.keys("enter")
}

func (h *harness) job(id, state string) {
	h.send(eventMsg{ev: app.Event{Job: app.JobSnapshot{ID: app.JobID(id), Slug: "s" + id, Title: "Title " + id, State: app.State(state)}}, ok: true})
}

func TestSearchOpensResultsAndRemembersTheQuery(t *testing.T) {
	svc := &fakeSvc{}
	h := newHarness(t, svc)
	if v := h.view(); !strings.Contains(v, "1,284,019 entries") || !strings.Contains(v, "profile: generic") {
		t.Fatalf("the Search screen shows the catalog size and the profile:\n%s", v)
	}
	h.search("zelda")
	if h.top() != scResults {
		t.Fatalf("top is %d, want Results", h.top())
	}
	want := app.SearchQuery{Text: "zelda", Page: 1, PageSize: 10, Sort: app.SortRelevance}
	if len(svc.searches) != 1 || svc.searches[0].Text != want.Text || svc.searches[0].Page != 1 || svc.searches[0].PageSize != 10 {
		t.Fatalf("searches %+v", svc.searches)
	}
	v := h.view()
	for _, w := range []string{`Results`, `"zelda"`, "Legend of Zelda", "Super Mario", "Page 1/4 · 37 results · 10 per page"} {
		if !strings.Contains(v, w) {
			t.Errorf("the Results screen lacks %q:\n%s", w, v)
		}
	}
	data, err := os.ReadFile(filepath.Join(h.m.env.dataDir, "tui_state")) //nolint:gosec // a path inside t.TempDir()
	if err != nil || !strings.Contains(string(data), `"zelda"`) {
		t.Fatalf("the recent query was not saved: %q, %v", data, err)
	}
	if got := readState(h.m.env.dataDir); len(got) != 1 || got[0] != "zelda" {
		t.Fatalf("readState %v", got)
	}
}

func TestSearchNeedsTextOrAFilterAndNeverCallsTheNetworkWithout(t *testing.T) {
	svc := &fakeSvc{}
	h := newHarness(t, svc)
	h.keys("enter")
	if len(svc.searches) != 0 || h.top() != scSearch {
		t.Fatalf("searches %v, top %d", svc.searches, h.top())
	}
	if !strings.Contains(h.view(), "Type something or pick a platform.") {
		t.Fatalf("no inline error:\n%s", h.view())
	}
	h.typeText("a") // typing clears the error
	if strings.Contains(h.view(), "Type something") {
		t.Fatal("the error stays after typing")
	}
}

func TestPlatformPickerAndTheTypedFallback(t *testing.T) {
	svc := &fakeSvc{}
	h := newHarness(t, svc)
	h.keys("tab", "enter") // the Platform field, open its picker
	if v := h.view(); !strings.Contains(v, "Super Nintendo") {
		t.Fatalf("the picker shows the loaded list:\n%s", v)
	}
	h.keys("down", "space", "enter") // mark snes, confirm
	h.keys("shift+tab", "enter")     // back to the query, run: a filter alone is enough
	if len(svc.searches) != 1 || len(svc.searches[0].Platforms) != 1 || svc.searches[0].Platforms[0] != "snes" {
		t.Fatalf("searches %+v", svc.searches)
	}

	failing := &fakeSvc{platforms: func() ([]mothdex.Platform, error) { return nil, &app.Error{Code: app.CodeNetwork, Message: "down"} }}
	h = newHarness(t, failing)
	h.keys("tab", "enter") // the list cannot load: the field becomes free text
	h.typeText("snes,nes")
	h.keys("enter")
	if len(failing.searches) != 1 || strings.Join(failing.searches[0].Platforms, "|") != "snes|nes" {
		t.Fatalf("typed ids: %+v", failing.searches)
	}
}

func TestResultsMarkDownloadPageAndSort(t *testing.T) {
	svc := &fakeSvc{}
	h := newHarness(t, svc)
	h.search("zelda")
	h.keys("space", "down", "space", "d") // mark both rows, download the marked ones
	if len(svc.enqueued) != 2 || svc.enqueued[0].Ref.Slug != "zelda" || svc.enqueued[1].Ref.Slug != "mario" {
		t.Fatalf("enqueued %+v", svc.enqueued)
	}
	if !strings.Contains(h.view(), "Queued 2 files") {
		t.Fatalf("no toast:\n%s", h.view())
	}
	if !strings.Contains(h.view(), "v 2 queued") {
		t.Fatalf("the title bar shows the queue:\n%s", h.view())
	}

	h.keys("n")
	if got := svc.searches[len(svc.searches)-1].Page; got != 2 {
		t.Fatalf("n asked for page %d", got)
	}
	h.keys("p", "p")
	if !strings.Contains(h.view(), "This is the first page.") {
		t.Fatalf("no first-page note:\n%s", h.view())
	}
	h.keys("s")
	if last := svc.searches[len(svc.searches)-1]; last.Sort != app.SortTitle || last.Page != 1 {
		t.Fatalf("sort asked %+v", last)
	}
	if !strings.Contains(h.view(), "sort: title (this page)") {
		t.Fatalf("the sort is labeled:\n%s", h.view())
	}
	// with nothing marked, d takes the row under the cursor
	h.keys("space", "up", "space", "d")
	if got := svc.enqueued[len(svc.enqueued)-1].Ref.Slug; got != "zelda" {
		t.Fatalf("d took %q", got)
	}
}

func TestResultsEmptyErrorAndRetry(t *testing.T) {
	svc := &fakeSvc{search: func(context.Context, app.SearchQuery) (app.SearchPage, error) { return app.SearchPage{Page: 1}, nil }}
	h := newHarness(t, svc)
	h.search("xyz")
	if !strings.Contains(h.view(), `No results for "xyz". Try fewer words, or remove filters.`) {
		t.Fatalf("empty state:\n%s", h.view())
	}
	h.keys("esc")
	if h.top() != scSearch {
		t.Fatal("Esc goes back to Search")
	}

	fail := true
	svc = &fakeSvc{search: func(_ context.Context, q app.SearchQuery) (app.SearchPage, error) {
		if fail {
			return app.SearchPage{}, &app.Error{Code: app.CodeNetwork, Message: "down"}
		}
		return pageOf(q), nil
	}}
	h = newHarness(t, svc)
	h.search("zelda")
	v := h.view()
	if !strings.Contains(v, "Cannot reach the Mothdex server.") || !strings.Contains(v, "Check your connection.") || !strings.Contains(v, "code: network") {
		t.Fatalf("error state:\n%s", v)
	}
	fail = false
	h.keys("r")
	if !strings.Contains(h.view(), "Legend of Zelda") {
		t.Fatalf("r did not retry:\n%s", h.view())
	}
}

func TestEscCancelsALoadingSearchThroughItsContext(t *testing.T) {
	started, canceled := make(chan struct{}), make(chan struct{})
	svc := &fakeSvc{search: func(ctx context.Context, _ app.SearchQuery) (app.SearchPage, error) {
		close(started)
		<-ctx.Done()
		close(canceled)
		return app.SearchPage{}, ctx.Err()
	}}
	h := newHarness(t, svc)
	// Push Results by hand: the harness would run its blocking search to the end, and this test
	// needs to run that command on its own goroutine.
	next, cmd := h.m.Update(pushMsg{s: newResults(app.SearchQuery{Text: "zelda", Page: 1}, false)})
	h.m = next.(model)
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	<-started
	if !h.m.top().loading() {
		t.Fatal("the screen is not loading")
	}
	h.keys("esc")
	<-canceled // the context the service saw was canceled
	if h.top() != scSearch {
		t.Fatalf("Esc must return to Search, top is %d", h.top())
	}
	h.send(<-done) // the answer that arrives late is ignored
	if h.top() != scSearch {
		t.Fatal("a late answer changed the screen")
	}
}

func TestEntryDownloadsTheHighlightedLinkTheBestLinkAndAsksBeforeAgain(t *testing.T) {
	svc := &fakeSvc{history: []app.HistoryRecord{{State: "done", Slug: "zelda", LinkIndex: 1}}}
	h := newHarness(t, svc)
	h.search("zelda")
	h.keys("enter") // open Legend of Zelda
	if h.top() != scEntry {
		t.Fatal("Enter opens the entry")
	}
	v := h.view()
	for _, w := range []string{"Entry zelda", "1*", "Myrient", "lang=En", "downloaded", "not downloadable"} {
		if !strings.Contains(v, w) {
			t.Errorf("entry screen lacks %q:\n%s", w, v)
		}
	}
	h.keys("down", "d") // link 2 was never downloaded: queued without asking
	if got := svc.enqueued[len(svc.enqueued)-1]; got.Link.Index != 2 || got.Ref.Slug != "zelda" {
		t.Fatalf("enqueued %+v", got)
	}
	if !strings.Contains(h.view(), "Queued: Title zelda (link 2)") {
		t.Fatalf("toast:\n%s", h.view())
	}
	h.keys("D") // the best link: link 1, which the history says is downloaded
	if !strings.Contains(h.view(), "Already downloaded. Download again? [y/N]") {
		t.Fatalf("no question:\n%s", h.view())
	}
	n := len(svc.enqueued)
	h.keys("n")
	if len(svc.enqueued) != n || strings.Contains(h.view(), "Already downloaded") {
		t.Fatal("n declines")
	}
	h.keys("D", "y")
	if got := svc.enqueued[len(svc.enqueued)-1]; len(svc.enqueued) != n+1 || got.Link.Index != 0 {
		t.Fatalf("y queues the best link (index 0): %+v", svc.enqueued)
	}
	h.keys("right") // the next entry of the list, without going back
	if svc.entries[len(svc.entries)-1] != "mario" {
		t.Fatalf("entries asked %v", svc.entries)
	}
	h.keys("esc")
	if h.top() != scResults {
		t.Fatal("Esc returns to Results")
	}
}

func TestEntryWithoutLinksAndTheCover(t *testing.T) {
	svc := &fakeSvc{links: func(string, app.LinkSelector) ([]app.LinkChoice, error) { return nil, nil }}
	h := newHarness(t, svc)
	h.search("zelda")
	h.keys("enter", "d")
	if len(svc.enqueued) != 0 || !strings.Contains(h.view(), "No links available for this entry.") {
		t.Fatalf("download must be disabled:\n%s", h.view())
	}
	h.keys("c")
	if !strings.Contains(h.view(), "Cover saved: /roms/cover.png") {
		t.Fatalf("cover toast:\n%s", h.view())
	}
	svc.boxart = func() (string, error) { return "", &app.Error{Code: app.CodeNotFound, Message: "none"} }
	h.keys("c")
	if !strings.Contains(h.view(), "This entry has no cover.") {
		t.Fatalf("no-cover toast:\n%s", h.view())
	}
}

func TestPreferenceOverlayRanksTheLinksAgain(t *testing.T) {
	svc := &fakeSvc{}
	h := newHarness(t, svc)
	h.search("zelda")
	h.keys("enter", "P")
	h.typeText("En") // the languages field has the focus
	h.keys("enter")
	last := svc.linkSels[len(svc.linkSels)-1]
	if len(last.Prefer.Languages) != 1 || last.Prefer.Languages[0] != "En" {
		t.Fatalf("Links asked with %+v", last)
	}
	if !strings.Contains(h.view(), "ranked by: lang=En") {
		t.Fatalf("the ranking is shown:\n%s", h.view())
	}
	h.keys("P", "tab", "tab", "tab")
	h.typeText("huge")
	h.keys("enter")
	if !strings.Contains(h.view(), "Size takes none, smallest or largest.") {
		t.Fatalf("a bad size is refused in place:\n%s", h.view())
	}
}

func TestEventsDriveTheTitleBarAndTheSummary(t *testing.T) {
	h := newHarness(t, &fakeSvc{})
	h.job("1", "transferring")
	h.job("2", "queued")
	title := strings.Split(h.view(), "\n")[0]
	if !strings.Contains(title, "v 1 active · 1 queued") {
		t.Fatalf("title bar %q", title)
	}
	h.job("1", "done")
	h.job("2", "failed")
	title = strings.Split(h.view(), "\n")[0]
	if strings.Contains(title, "active") || strings.Contains(title, "queued") {
		t.Fatalf("the summary hides when idle: %q", title)
	}
	if !strings.Contains(h.view(), "1 downloaded, 0 skipped, 1 failed") {
		t.Fatalf("no summary toast:\n%s", h.view())
	}
}

func TestQuitAsksOnlyWhileDownloadsRun(t *testing.T) {
	svc := &fakeSvc{}
	h := newHarness(t, svc)
	h.keys("ctrl+c")
	if !h.quit {
		t.Fatal("with nothing running Ctrl+C quits at once")
	}

	h = newHarness(t, svc)
	h.job("1", "transferring")
	h.keys("ctrl+c")
	if h.quit || !strings.Contains(h.view(), "Quit? Downloads in progress will be canceled. [y/N]") {
		t.Fatalf("quit=%v\n%s", h.quit, h.view())
	}
	h.keys("n")
	if h.quit || strings.Contains(h.view(), "Quit?") {
		t.Fatal("n keeps the program running")
	}
	h.keys("ctrl+c", "y")
	if !h.quit || len(svc.cancelled) != 1 || svc.cancelled[0] != "1" {
		t.Fatalf("quit=%v cancelled=%v", h.quit, svc.cancelled)
	}

	h = newHarness(t, svc)
	h.search("zelda")
	h.keys("q")
	if h.quit {
		t.Fatal("q quits on the root screen only")
	}
}

func TestATerminalThatIsTooSmallKeepsTheStateAndComesBack(t *testing.T) {
	h := newHarness(t, &fakeSvc{})
	h.typeText("abc")
	h.resize(54, 14)
	if v := h.view(); !strings.Contains(strings.Join(strings.Fields(v), " "), "Terminal too small (need 60x16, have 54x14). Resize or press q.") {
		t.Fatalf("too-small notice:\n%s", v)
	}
	for _, l := range strings.Split(h.m.render(), "\n") {
		if cells(l) > 54 {
			t.Fatalf("the notice does not fit 54 columns: %q", l)
		}
	}
	h.typeText("xyz") // nothing reaches the screen while it is hidden
	h.resize(80, 24)
	v := h.view()
	if !strings.Contains(v, "abc") || strings.Contains(v, "abcxyz") || h.top() != scSearch {
		t.Fatalf("the screen did not come back as it was:\n%s", v)
	}
	h.resize(54, 14)
	h.keys("q")
	if !h.quit {
		t.Fatal("q leaves from the too-small screen")
	}
}

func TestQueueScreenCancelsRetriesAndShowsDetails(t *testing.T) {
	svc := &fakeSvc{}
	h := newHarness(t, svc)
	h.search("zelda")
	h.keys("d") // queue zelda: its request is remembered for a retry
	h.job("q1", "transferring")
	h.keys("Q")
	if h.top() != scQueue || !strings.Contains(h.view(), "Title q1") {
		t.Fatalf("Q opens the queue:\n%s", h.view())
	}
	h.keys("x")
	if len(svc.cancelled) != 1 || svc.cancelled[0] != "q1" {
		t.Fatalf("x cancels the highlighted job: %v", svc.cancelled)
	}
	h.send(eventMsg{ev: app.Event{Job: app.JobSnapshot{ID: "q1", Slug: "zelda", Title: "Title q1", State: app.StateFailed, Err: &app.Error{Code: "size_mismatch", Message: "bad size"}}}, ok: true})
	v := h.view()
	if !strings.Contains(v, "finished (1)") || !strings.Contains(v, "[size_mismatch]") {
		t.Fatalf("finished section:\n%s", v)
	}
	before := len(svc.enqueued)
	h.keys("r") // retry the failed job from its original request
	if len(svc.enqueued) != before+1 || svc.enqueued[before].Ref.Slug != "zelda" {
		t.Fatalf("retry enqueued %+v", svc.enqueued)
	}
	h.send(eventMsg{ev: app.Event{Job: app.JobSnapshot{ID: "q9", Slug: "mario", Title: "Done game", State: app.StateDone, Path: "/roms/x.nes", SHA256: "abc123", Attempt: 1}}, ok: true})
	h.keys("down", "enter") // details of a finished job
	if v := h.view(); !strings.Contains(v, "/roms/x.nes") || !strings.Contains(v, "abc123") {
		t.Fatalf("details:\n%s", v)
	}
	h.keys("esc", "c")
	if strings.Contains(h.view(), "finished (") {
		t.Fatalf("c hides the finished jobs:\n%s", h.view())
	}
	h.job("a1", "transferring")
	h.job("a2", "queued")
	h.keys("X")
	if !strings.Contains(h.view(), "Cancel all downloads? [y/N]") {
		t.Fatalf("X asks first:\n%s", h.view())
	}
	h.keys("y")
	got := map[app.JobID]bool{}
	for _, id := range svc.cancelled {
		got[id] = true
	}
	if !got["a1"] || !got["a2"] {
		t.Fatalf("X then y cancels every running job: %v", svc.cancelled)
	}
}

func TestHostileServerTextNeverReachesTheTerminal(t *testing.T) {
	hostile := "Evil\x1b]0;pwned\x07 \x1b[2JGame\u202e"
	svc := &fakeSvc{
		search: func(_ context.Context, q app.SearchQuery) (app.SearchPage, error) {
			p := pageOf(q)
			p.Results[0].Title, p.Results[0].Platform, p.Results[0].Regions = hostile, hostile, []string{hostile}
			return p, nil
		},
		entry: func(slug string) (mothdex.Entry, error) {
			return mothdex.Entry{Slug: slug, Title: hostile, Platform: hostile, Serial: hostile}, nil
		},
		links: func(string, app.LinkSelector) ([]app.LinkChoice, error) {
			return []app.LinkChoice{{Index: 1, Available: true, Link: mothdex.Link{Type: hostile, Format: hostile, SourceName: hostile, Extras: []string{hostile}}, Reasons: []string{hostile}}}, nil
		},
	}
	h := newHarness(t, svc)
	check := func(where string) {
		t.Helper()
		raw := h.m.render()
		for _, bad := range []string{"\x1b]", "\x07", "\x1b[2J", "\u202e"} {
			if strings.Contains(raw, bad) {
				t.Errorf("%s leaks %q", where, bad)
			}
		}
		if !strings.Contains(raw, "Game") {
			t.Errorf("%s lost the text itself", where)
		}
	}
	h.search("evil")
	check("Results")
	h.keys("enter")
	check("Entry")
	h.send(eventMsg{ev: app.Event{Job: app.JobSnapshot{ID: "h1", Title: hostile, Name: hostile, State: app.StateDone, Path: hostile}}, ok: true})
	h.keys("esc", "esc", "esc", "Q") // back to Search, leave the text field, open the queue
	check("Queue")
	h.send(toastMsg{text: hostile, kind: toastInfo})
	check("toast")
	svc.history = []app.HistoryRecord{{ID: "x", Title: hostile, Platform: hostile, Path: hostile, Filename: hostile, SHA256: hostile, StartedAt: t0}}
	h.keys("esc", "H", "enter")
	check("History details")
	svc.configs = []app.Setting{{Key: "download.dir", Value: hostile, Source: "file"}}
	h.keys("esc", "esc", "S")
	check("Settings")
}

// sgrParams lists every numeric parameter of every SGR sequence in s.
func sgrParams(s string) map[int]bool {
	out := map[int]bool{}
	for _, m := range regexp.MustCompile(`\x1b\[([0-9;]*)m`).FindAllStringSubmatch(s, -1) {
		for _, p := range strings.Split(m[1], ";") {
			n, _ := strconv.Atoi(p)
			out[n] = true
		}
	}
	return out
}

func TestNoColorUsesOnlyBoldFaintAndReverse(t *testing.T) {
	plain := map[int]bool{0: true, 1: true, 2: true, 7: true, 22: true, 27: true}
	for _, noColor := range []bool{true, false} {
		h := newHarness(t, &fakeSvc{}, func(e *env) { e.st = newStyles(noColor, true) })
		h.search("zelda")
		h.job("1", "transferring")
		raw := h.m.render()
		colored := false
		for p := range sgrParams(raw) {
			if !plain[p] {
				colored = true
			}
		}
		if noColor && colored {
			t.Fatalf("--no-color drew colors: %v", sgrParams(raw))
		}
		if !noColor && !colored {
			t.Fatal("with color on, the roles must be colored")
		}
		if !strings.Contains(raw, ">") {
			t.Fatal("the cursor row is marked with > besides reverse video")
		}
	}
}

func TestEveryScreenFillsTheTerminalExactlyAtEverySize(t *testing.T) {
	for _, sz := range [][2]int{{60, 16}, {79, 19}, {80, 24}, {100, 30}, {200, 60}} {
		w, hh := sz[0], sz[1]
		svc := settingsSvc()
		svc.history = historySvc().history
		h := newHarness(t, svc)
		h.resize(w, hh)
		shots := map[string]string{"search": h.m.render()}
		h.search("zelda")
		shots["results"] = h.m.render()
		h.keys("enter")
		shots["entry"] = h.m.render()
		h.job("1", "transferring")
		h.keys("Q")
		shots["queue"] = h.m.render()
		h.keys("?")
		shots["help"] = h.m.render()
		h.keys("esc", "esc", "H") // close help, leave the queue, open the history
		shots["history"] = h.m.render()
		h.keys("enter")
		shots["history details"] = h.m.render()
		h.keys("esc", "esc", "S")
		shots["settings"] = h.m.render()
		h.keys("enter")
		shots["settings editor"] = h.m.render()
		for name, raw := range shots {
			lines := strings.Split(raw, "\n")
			if len(lines) != hh {
				t.Errorf("%s at %dx%d is %d lines", name, w, hh, len(lines))
			}
			for i, l := range lines {
				if got := cells(l); got != w {
					t.Errorf("%s at %dx%d line %d is %d cells: %q", name, w, hh, i, got, ansi.Strip(l))
					break
				}
			}
		}
	}
}

func TestSlashReturnsToSearchWithTheFieldsFilled(t *testing.T) {
	h := newHarness(t, &fakeSvc{})
	h.keys("tab", "enter", "space", "enter", "shift+tab") // pick the first platform
	h.typeText("zelda")
	h.keys("enter")
	h.keys("/")
	if h.top() != scSearch || !strings.Contains(h.view(), "zelda") || !strings.Contains(h.view(), "nes") {
		t.Fatalf("the query and filters are back in the fields:\n%s", h.view())
	}
}

func TestRandomPicksHaveNoPaging(t *testing.T) {
	svc := &fakeSvc{}
	h := newHarness(t, svc)
	h.keys("tab", "tab", "tab", "tab") // to the Random button
	h.keys("enter")
	v := h.view()
	if !strings.Contains(v, "Random picks") || !strings.Contains(v, "Title r1") {
		t.Fatalf("random results:\n%s", v)
	}
	n := len(svc.searches)
	h.keys("n", "s")
	if len(svc.searches) != n {
		t.Fatal("n and s do nothing on random picks")
	}
	h.keys("d")
	if svc.enqueued[len(svc.enqueued)-1].Ref.Slug != "r1" {
		t.Fatalf("enqueued %+v", svc.enqueued)
	}
}

func TestTheLocalizedYesInitialAnswersAQuestion(t *testing.T) {
	bundle, err := i18n.Load(locales.FS)
	if err != nil {
		t.Fatal(err)
	}
	svc := &fakeSvc{}
	h := newHarness(t, svc, func(e *env) { e.t = bundle.New("pt_br"); e.num = printer(e.t) })
	h.job("1", "transferring")
	h.keys("ctrl+c")
	if !strings.Contains(h.view(), "[s/N]") {
		t.Fatalf("question in Portuguese:\n%s", h.view())
	}
	h.keys("s")
	if !h.quit {
		t.Fatal("s (Sim) must confirm")
	}
}

func TestHiddenResultsAreReportedNotSilentlyDropped(t *testing.T) {
	svc := &fakeSvc{search: func(_ context.Context, q app.SearchQuery) (app.SearchPage, error) {
		p := pageOf(q)
		p.Dropped = 2
		return p, nil
	}}
	h := newHarness(t, svc)
	h.search("zelda")
	if !strings.Contains(h.view(), "2 results were hidden because their slug has an unexpected form.") {
		t.Fatalf("no warning for the hidden results:\n%s", h.view())
	}
}

func TestRetryNeverQueuesTheSameFailedJobsTwice(t *testing.T) {
	svc := &fakeSvc{}
	h := newHarness(t, svc)
	h.search("zelda")
	h.keys("d") // queued as q1, with its request remembered
	h.send(eventMsg{ev: app.Event{Job: app.JobSnapshot{ID: "q1", Slug: "zelda", Title: "Title zelda", State: app.StateFailed, Err: &app.Error{Code: "size_mismatch", Message: "bad size"}}}, ok: true})
	h.keys("Q")
	before := len(svc.enqueued)
	h.keys("r", "r")
	if got := len(svc.enqueued) - before; got != 1 {
		t.Fatalf("pressing r twice queued %d requests, want 1", got)
	}
	if strings.Contains(h.view(), "[size_mismatch]") {
		t.Fatalf("the retried job must leave the finished list:\n%s", h.view())
	}
}
