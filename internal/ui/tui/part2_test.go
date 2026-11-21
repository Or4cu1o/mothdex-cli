package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/history"
	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

func mothdexLink(size int64) mothdex.Link { return mothdex.Link{Type: "Game", Size: size} }

var (
	t0 = time.Date(2026, 11, 20, 14, 3, 0, 0, time.Local)
)

func rec(id, title string, ago time.Duration, failed bool) app.HistoryRecord {
	r := app.HistoryRecord{ID: id, Slug: "slug-" + id, Title: title, Platform: "nes", StartedAt: t0.Add(-ago), LinkIndex: 2, State: "done",
		Bytes: 384 << 10, Path: "/roms/" + id + ".nes", SHA256: "sha" + id, CRC: "ok", Attempts: 1, DurationMS: 12000, Filename: id + ".nes"}
	if failed {
		r.State = "failed"
		r.Error = &history.Failure{Code: "size_mismatch", Message: "wrong size"}
	}
	return r
}

func historySvc() *fakeSvc {
	return &fakeSvc{history: []app.HistoryRecord{rec("a", "Kirby's Adventure", 3*time.Hour, false), rec("b", "Contra", time.Hour, true), rec("c", "Metroid", 2*time.Hour, false)}}
}

// noQuery leaves the Search text field, so the letters that open screens are commands again.
func (h *harness) leaveSearchField() { h.keys("esc") }

func TestHistoryListsFiltersAndShowsDetails(t *testing.T) {
	svc := historySvc()
	h := newHarness(t, svc)
	h.leaveSearchField()
	h.keys("H")
	if h.top() != scHistory {
		t.Fatalf("top %d", h.top())
	}
	v := h.view()
	if c, m, k := strings.Index(v, "Contra"), strings.Index(v, "Metroid"), strings.Index(v, "Kirby"); c >= m || m >= k {
		t.Fatalf("newest first (Contra, Metroid, Kirby):\n%s", v)
	}
	for _, w := range []string{"3 records", "size_mismatch", "2026-11-20"} {
		if !strings.Contains(v, w) {
			t.Errorf("history lacks %q:\n%s", w, v)
		}
	}
	h.keys("f")
	if got := svc.histFilters[len(svc.histFilters)-1]; !got.Failed {
		t.Fatalf("f asks for failed only: %+v", got)
	}
	if v := h.view(); strings.Contains(v, "Metroid") || !strings.Contains(v, "failed only") || !strings.Contains(v, "1 record") {
		t.Fatalf("failed only:\n%s", v)
	}
	h.keys("f", "/")
	h.typeText("metr")
	h.keys("enter")
	if v := h.view(); strings.Contains(v, "Kirby") || !strings.Contains(v, "Metroid") || !strings.Contains(v, "filter: metr") {
		t.Fatalf("text filter:\n%s", v)
	}
	h.keys("enter") // details of the row under the cursor
	if v := h.view(); !strings.Contains(v, "/roms/c.nes") || !strings.Contains(v, "shac") {
		t.Fatalf("details:\n%s", v)
	}
	h.keys("esc", "esc")
	if h.top() != scSearch {
		t.Fatalf("Esc closes the details, then goes back; top is %d", h.top())
	}
}

func TestHistoryRequeuesAFailedRowAndReloadsWhenAJobEnds(t *testing.T) {
	svc := historySvc()
	h := newHarness(t, svc)
	h.leaveSearchField()
	h.keys("H", "d") // the cursor is on Contra, which failed
	last := svc.enqueued[len(svc.enqueued)-1]
	if last.Ref.Slug != "slug-b" || last.Link.Index != 2 {
		t.Fatalf("requeued %+v", last)
	}
	h.keys("down", "d")
	if !strings.Contains(h.view(), "Only failed downloads can be queued again.") || len(svc.enqueued) != 1 {
		t.Fatalf("a finished row is not queued:\n%s", h.view())
	}
	before := len(svc.histFilters)
	h.job("z", "done")
	if len(svc.histFilters) != before+1 {
		t.Fatal("History must reload after a job ends")
	}
}

func TestHistoryExportAsksBeforeOverwriting(t *testing.T) {
	svc := historySvc()
	h := newHarness(t, svc)
	h.leaveSearchField()
	h.keys("H", "e")
	if !strings.Contains(h.view(), "Export to: [ ./mothdex-history-20261121T120000.csv") {
		t.Fatalf("default name:\n%s", h.view())
	}
	target := filepath.Join(t.TempDir(), "out.csv")
	h.keys("ctrl+u")
	h.typeText(target)
	h.keys("enter")
	data, err := os.ReadFile(target) //nolint:gosec // a path inside t.TempDir()
	if err != nil || !strings.Contains(string(data), "Contra") {
		t.Fatalf("export file %q, %v", data, err)
	}
	// the toast is cut to the status bar, and a temporary folder path can be long: check the start
	if !strings.Contains(h.view(), "Saved ") {
		t.Fatalf("no confirmation:\n%s", h.view())
	}
	h.keys("e", "ctrl+u")
	h.typeText(target)
	h.keys("enter")
	if !strings.Contains(h.view(), "Overwrite? [y/N]") {
		t.Fatalf("an existing file asks first:\n%s", h.view())
	}
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.keys("n")
	if data, _ := os.ReadFile(target); string(data) != "keep" { //nolint:gosec // a path inside t.TempDir()
		t.Fatalf("n must leave the file alone: %q", data)
	}
	h.keys("e", "ctrl+u")
	h.typeText(target)
	h.keys("enter", "y")
	if data, _ := os.ReadFile(target); !strings.Contains(string(data), "Contra") { //nolint:gosec // a path inside t.TempDir()
		t.Fatalf("y overwrites: %q", data)
	}
}

func settingsSvc() *fakeSvc {
	return &fakeSvc{
		cache: app.CacheStats{TotalBytes: 84 << 20, MaxBytes: 200 << 20},
		configs: []app.Setting{
			{Key: "download.dir", Value: "", Source: "default"},
			{Key: "download.concurrency", Value: 2, Source: "default"},
			{Key: "download.on_exists", Value: "skip", Source: "default"},
			{Key: "download.boxart", Value: true, Source: "default"},
			{Key: "download.prefer.languages", Value: []string(nil), Source: "default"},
			{Key: "download.prefer.formats", Value: []string(nil), Source: "default"},
			{Key: "download.prefer.sources", Value: []string(nil), Source: "default"},
			{Key: "download.prefer.size", Value: "none", Source: "default"},
			{Key: "interface.language", Value: "auto", Source: "default"},
			{Key: "interface.color", Value: "auto", Source: "env"},
			{Key: "interface.page_size", Value: 10, Source: "default"},
			{Key: "interface.confirm_downloads", Value: true, Source: "default"},
		},
		configSet: func(key, value string) error {
			if key == "download.concurrency" && value == "9" {
				return &app.Error{Code: app.CodeInvalidConfig, Message: "out of range", Args: map[string]any{"key": key, "value": value, "source": "file"}}
			}
			return nil
		},
	}
}

func (h *harness) openSettings() {
	h.leaveSearchField()
	h.keys("S")
}

func TestSettingsEditToggleResetAndTheLimitsOfANumber(t *testing.T) {
	svc := settingsSvc()
	h := newHarness(t, svc)
	h.openSettings()
	v := h.view()
	for _, w := range []string{"Download", "Concurrent downloads", "Cover art", "on", "default", "Cache", "84.0 MB of 200.0 MB", "Advanced:"} {
		if !strings.Contains(v, w) {
			t.Errorf("settings lacks %q:\n%s", w, v)
		}
	}
	h.keys("down", "enter") // Concurrent downloads: a number
	h.keys("ctrl+u")
	h.typeText("9")
	h.keys("enter")
	if v := h.view(); !strings.Contains(v, "1-6") || !strings.Contains(v, "Invalid setting") {
		t.Fatalf("an invalid number is refused in place, with its range:\n%s", v)
	}
	h.keys("ctrl+u")
	h.typeText("4")
	h.keys("enter")
	if last := svc.sets[len(svc.sets)-1]; last != [2]string{"download.concurrency", "4"} {
		t.Fatalf("sets %v", svc.sets)
	}
	if v := h.view(); !strings.Contains(v, "4") || !strings.Contains(v, "applies on next start") {
		t.Fatalf("the saved value shows, with its note:\n%s", v)
	}
	h.keys("down", "down", "space") // Cover art: a toggle, applied at once
	if last := svc.sets[len(svc.sets)-1]; last != [2]string{"download.boxart", "false"} || h.m.boxart {
		t.Fatalf("sets %v, boxart %v", svc.sets, h.m.boxart)
	}
	h.keys("backspace")
	if len(svc.resets) != 1 || svc.resets[0] != "download.boxart" {
		t.Fatalf("Backspace resets the key: %v", svc.resets)
	}
}

func TestSettingsEnumReadOnlyRowsAndTheLiveOnes(t *testing.T) {
	svc := settingsSvc()
	h := newHarness(t, svc)
	h.openSettings()
	h.keys("down", "down", "enter") // When file exists: an enum list
	h.keys("down", "enter")
	if last := svc.sets[len(svc.sets)-1]; last != [2]string{"download.on_exists", "rename"} {
		t.Fatalf("sets %v", svc.sets)
	}
	h.keys("down", "down", "down", "down", "down", "down", "down", "enter") // Color: set by the environment
	if len(svc.sets) != 1 || !strings.Contains(h.view(), "Set by MOTHDEX_INTERFACE_COLOR") {
		t.Fatalf("an environment row is read-only, and says why:\n%s", h.view())
	}
	h.keys("down", "enter") // Results per page: applied at once
	h.keys("ctrl+u")
	h.typeText("25")
	h.keys("enter")
	if h.m.pageSize != 25 {
		t.Fatalf("page size %d", h.m.pageSize)
	}
	h.keys("esc", "tab") // back to Search, whose focus sits on Random: Tab wraps to the query field
	h.typeText("zelda")
	h.keys("enter")
	if got := svc.searches[len(svc.searches)-1].PageSize; got != 25 {
		t.Fatalf("the next search used page size %d", got)
	}
}

func TestSettingsLinkPreferenceAppliesAtOnce(t *testing.T) {
	svc := settingsSvc()
	h := newHarness(t, svc)
	h.openSettings()
	h.keys("down", "down", "down", "down", "enter") // Preferred languages
	h.keys("ctrl+u")
	h.typeText("En,Pt")
	h.keys("enter")
	if got := h.m.prefs.Languages; len(got) != 2 || got[0] != "En" || got[1] != "Pt" {
		t.Fatalf("prefs %+v", h.m.prefs)
	}
	h.keys("esc")
	h.search("zelda")
	h.keys("enter")
	if last := svc.linkSels[len(svc.linkSels)-1]; len(last.Prefer.Languages) != 2 {
		t.Fatalf("Links asked with %+v", last)
	}
}

func TestSettingsClearCacheAsksFirst(t *testing.T) {
	svc := settingsSvc()
	h := newHarness(t, svc)
	h.openSettings()
	h.keys("G", "enter")
	if !strings.Contains(h.view(), "Clear cache (84.0 MB)? [y/N]") {
		t.Fatalf("no question:\n%s", h.view())
	}
	h.keys("n")
	if svc.cacheCleared != 0 {
		t.Fatal("n declines")
	}
	h.keys("enter", "y")
	if svc.cacheCleared != 1 || !strings.Contains(h.view(), "Cache cleared") {
		t.Fatalf("cleared %d:\n%s", svc.cacheCleared, h.view())
	}
}

func TestConfirmDownloadsAsksForABatchOrABigFile(t *testing.T) {
	on := func(e *env) { e.confirm = true }
	svc := &fakeSvc{}
	h := newHarness(t, svc, on)
	h.search("zelda")
	h.keys("space", "down", "space", "d")
	if len(svc.enqueued) != 0 || !strings.Contains(h.view(), "Download 2 files (0 B)? [y/N]") {
		t.Fatalf("a batch asks first:\n%s", h.view())
	}
	h.keys("n")
	if len(svc.enqueued) != 0 {
		t.Fatal("n declines")
	}
	h.keys("d", "y")
	if len(svc.enqueued) != 2 {
		t.Fatalf("y queues the batch: %d", len(svc.enqueued))
	}
	big := &fakeSvc{links: func(string, app.LinkSelector) ([]app.LinkChoice, error) {
		return []app.LinkChoice{{Index: 1, Available: true, Link: mothdexLink(600 << 20)}}, nil
	}}
	h = newHarness(t, big, on)
	h.search("zelda")
	h.keys("enter", "d")
	if len(big.enqueued) != 0 || !strings.Contains(h.view(), "Download 1 file (600.0 MB)? [y/N]") {
		t.Fatalf("a file over 500 MB asks:\n%s", h.view())
	}
	off := newHarness(t, &fakeSvc{})
	off.search("zelda")
	off.keys("space", "down", "space", "d")
	if len(off.svc.enqueued) != 2 {
		t.Fatal("with confirm_downloads off nothing is asked")
	}
}

func TestTheCoverIsSavedAfterADoneJobWhenTheSettingIsOn(t *testing.T) {
	svc := &fakeSvc{}
	h := newHarness(t, svc, func(e *env) { e.boxart = true })
	h.job("1", "transferring")
	if len(svc.jobCovers) != 0 {
		t.Fatal("no cover before the file is done")
	}
	h.job("1", "done")
	h.job("1", "done") // a repeated event must not save it twice
	if len(svc.jobCovers) != 1 || svc.jobCovers[0].ID != "1" {
		t.Fatalf("covers %+v", svc.jobCovers)
	}
	h.job("2", "failed")
	if len(svc.jobCovers) != 1 {
		t.Fatal("a failed job gets no cover")
	}
	svc.jobCoverErr = &app.Error{Code: app.CodeNotFound, Message: "no cover"}
	h.job("3", "done")
	if strings.Contains(h.view(), "Cannot save the cover") {
		t.Fatal("an entry without a cover is normal, not a warning")
	}
	svc.jobCoverErr = &app.Error{Code: app.CodeNetwork, Message: "down"}
	h.job("4", "done")
	if !strings.Contains(h.view(), "Cannot save the cover of Title 4 [network]") {
		t.Fatalf("a cover failure is a warning:\n%s", h.view())
	}
	off := newHarness(t, &fakeSvc{})
	off.job("1", "done")
	if len(off.svc.jobCovers) != 0 {
		t.Fatal("download.boxart off: no cover")
	}
}

func TestPastLastPageAndHiddenResultsShareOneToast(t *testing.T) {
	svc := &fakeSvc{search: func(_ context.Context, q app.SearchQuery) (app.SearchPage, error) {
		p := pageOf(q)
		p.PastLastPage, p.Dropped = true, 3
		return p, nil
	}}
	h := newHarness(t, svc)
	h.search("zelda")
	v := h.view()
	if !strings.Contains(v, "does not exist; showing page") || !strings.Contains(v, "3 results were hidden") {
		t.Fatalf("both notes must be seen:\n%s", v)
	}
}

func TestAFailedRetryPutsTheJobsBack(t *testing.T) {
	svc := &fakeSvc{}
	h := newHarness(t, svc)
	h.search("zelda")
	h.keys("d")
	h.send(eventMsg{ev: app.Event{Job: app.JobSnapshot{ID: "q1", Slug: "zelda", Title: "Title q1", State: app.StateFailed, Err: &app.Error{Code: "size_mismatch", Message: "bad size"}}}, ok: true})
	h.keys("Q")
	svc.enqueueErr = &app.Error{Code: app.CodeNetwork, Message: "down"}
	h.keys("r")
	if !strings.Contains(h.view(), "[size_mismatch]") {
		t.Fatalf("after a failed Enqueue the job is back in the list:\n%s", h.view())
	}
	svc.enqueueErr = nil
	before := len(svc.enqueued)
	h.keys("r")
	if len(svc.enqueued) != before+1 {
		t.Fatal("r works again once the network is back")
	}
}

func TestEntryShowsThePlatformNameFromTheCachedList(t *testing.T) {
	svc := &fakeSvc{platforms: func() ([]mothdex.Platform, error) {
		return []mothdex.Platform{{ID: "gbc", Name: "Game Boy Color"}, {ID: "nes", Name: "Nintendo\x1b[2J"}}, nil
	}}
	h := newHarness(t, svc)
	h.search("zelda")
	h.keys("enter") // the default entry is on gbc
	if v := h.view(); !strings.Contains(v, "Platform gbc (Game Boy Color)") {
		t.Fatalf("id and name:\n%s", v)
	}

	failing := &fakeSvc{platforms: func() ([]mothdex.Platform, error) { return nil, &app.Error{Code: app.CodeNetwork, Message: "down"} }}
	h = newHarness(t, failing)
	h.search("zelda")
	h.keys("enter")
	if v := h.view(); !strings.Contains(v, "Platform gbc ·") {
		t.Fatalf("with no list the id stands alone:\n%s", v)
	}
}
