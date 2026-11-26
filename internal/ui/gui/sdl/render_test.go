//go:build gui

package sdl

import (
	"context"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/cache"
	"github.com/Or4cu1o/mothdex-cli/internal/history"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui"
	gdraw "github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/guitest"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/text"
	"github.com/Or4cu1o/mothdex-cli/locales"
	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

// The render harness (T-67): the screens of the GUI drawn by the real SDL2 software renderer with the real
// font (the pinned Noto Sans), read back and written as PNG files, so a person can look at them. It is not a
// golden test: nothing is compared with a stored image. It fails only when SDL cannot draw a frame or a frame
// comes out blank.
//
//	GUI_RENDER_DIR=/some/dir SDL_VIDEODRIVER=dummy go test -tags gui -run TestRenderScenes ./internal/ui/gui/sdl
//
// Without GUI_RENDER_DIR the test is skipped, so the normal runs do not write files. The CI job "render"
// sets it, requires SDL (GUI_REQUIRE_SDL=1) and uploads the folder as a workflow artifact. The files are named
// <scene>_<width>x<height>.png, and index.txt says what each scene is. A scene is added to the list below: give
// it a name, one line of description and the steps that reach it from Home.

// renderSizes are the displays of docs/ux/gui.md section 1.1 that draw at scale 1 (the handheld 480x320 is the
// 640x480 layout scaled by SDL, and a read-back inside a scaled viewport is not the whole frame).
var renderSizes = [][2]int{{640, 480}, {1280, 720}, {1920, 1080}}

// player drives the model the way the run loop does: messages in, the commands it returns run at once.
type player struct {
	t   *testing.T
	o   gui.Options
	m   gui.Model
	now time.Time
}

func newPlayer(t *testing.T, font *text.Font, w, h int, svc *guitest.Fake, confirm bool, lang string, hold bool) *player {
	t.Helper()
	o := guiOptions(t, font, svc, w, h)
	o.Confirm = confirm
	o.LogPath = func() string { return "/userdata/system/mothdex/data/logs/latest.log" }
	o.ExportLogs = func(context.Context) (string, error) {
		return "/userdata/system/mothdex/data/logs/bundles/mothdex-logs-20261126-030000.zip", nil
	}
	o.MappingsPath = "/userdata/system/mothdex/data/gamecontrollerdb.txt"
	if lang != "" { // the screen in another language: the longest labels and reasons show what fits
		bundle, err := i18n.Load(locales.FS)
		if err != nil {
			t.Fatal(err)
		}
		o.T = bundle.New(lang)
	}
	m, cmds := gui.New(o, testEpoch)
	p := &player{t: t, o: o, m: m, now: testEpoch}
	if !hold { // a held start never runs the first commands, so the model stays on the Splash
		p.run(cmds)
	}
	return p
}

func (p *player) run(cmds []gui.Cmd) {
	p.t.Helper()
	for len(cmds) > 0 {
		next := cmds[0]
		cmds = cmds[1:]
		var more []gui.Cmd
		p.m, more = p.m.Update(next(), p.now)
		cmds = append(cmds, more...)
	}
}

func (p *player) send(msg gui.Msg) {
	p.t.Helper()
	var cmds []gui.Cmd
	p.m, cmds = p.m.Update(msg, p.now)
	p.run(cmds)
}

func (p *player) press(actions ...input.Action) {
	p.t.Helper()
	for _, a := range actions {
		p.send(gui.InputMsg{Action: a})
	}
}

// advance moves the clock and lets the timers fire (a dialog ignores input for a moment after it opens).
func (p *player) advance(d time.Duration) {
	p.t.Helper()
	p.now = p.now.Add(d)
	p.send(gui.TickMsg{})
}

// openQueue opens the Queue once the toasts that the jobs raised on Home have gone, so they do not cover the list.
func (p *player) openQueue() {
	p.t.Helper()
	p.advance(10 * time.Second)
	p.press(input.Queue)
}

func (p *player) job(j app.JobSnapshot) { p.t.Helper(); p.send(gui.JobMsg{Job: j}) }

// renderCatalog is a small catalog with real-looking titles, covers for some of them and two links for one.
func renderCatalog() *guitest.Fake {
	svc := guitest.New()
	for _, g := range []struct{ slug, title, platform, region string }{
		{"mario-world", "Super Mario World (USA)", "snes", "us"},
		{"zelda-alttp", "The Legend of Zelda: A Link to the Past (USA)", "snes", "us"},
		{"zelda-oot", "The Legend of Zelda: Ocarina of Time (USA)", "n64", "us"},
		{"zelda-links", "The Legend of Zelda: Link's Awakening DX (USA, Europe)", "gbc", "us"},
		{"metroid", "Super Metroid (Japan, USA)", "snes", "us"},
		{"sonic2", "Sonic the Hedgehog 2 (World)", "md", "us"},
		{"kirby", "Kirby's Dream Land (USA, Europe)", "gb", "us"},
	} {
		e := guitest.Game(g.slug, g.title, g.platform, g.region)
		if g.slug == "zelda-alttp" {
			e.Links = append(e.Links, mothdex.Link{Type: "Game", Filename: g.slug + "-alt.7z", URL: "https://example.org/" + g.slug + "-alt", Size: 3 << 19, SizeStr: "1.5 MB", SourceName: "Archive"})
		}
		svc.Catalog = append(svc.Catalog, e)
	}
	svc.Covers = map[string]bool{"zelda-alttp": true, "zelda-oot": true, "mario-world": true, "metroid": true}
	svc.Platforms_ = []mothdex.Platform{
		{ID: "snes", Name: "Super Nintendo", Brand: "Nintendo"}, {ID: "n64", Name: "Nintendo 64", Brand: "Nintendo"},
		{ID: "gbc", Name: "Game Boy Color", Brand: "Nintendo"}, {ID: "gb", Name: "Game Boy", Brand: "Nintendo"},
		{ID: "md", Name: "Mega Drive / Genesis", Brand: "Sega"},
	}
	svc.Regions_ = []mothdex.Region{{ID: "us", Name: "USA"}, {ID: "eu", Name: "Europe"}, {ID: "jp", Name: "Japan"}}
	svc.Records = renderRecords()
	svc.Settings = renderSettings()
	svc.Cache = cache.Stats{TotalBytes: 84 << 20}
	return svc
}

// renderRecords is a history with one record of each look: done, failed, stopped on its size, canceled and skipped.
func renderRecords() []app.HistoryRecord {
	at := func(min int) time.Time { return time.Date(2026, 11, 20, 14, 3-min, 0, 0, time.Local) }
	rec := func(id, slug, title, platform, state string, bytes int64, min int, code, msg string) app.HistoryRecord {
		r := app.HistoryRecord{ID: id, Slug: slug, Title: title, Platform: platform, State: state, LinkIndex: 1, Bytes: bytes, Filename: slug + ".zip",
			Path: "/userdata/roms/" + platform + "/" + slug + ".zip", FinishedAt: at(min), StartedAt: at(min + 1), Attempts: 1, DurationMS: 42000,
			SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}
		if code != "" {
			r.Error, r.Bytes, r.Path = &history.Failure{Code: code, Message: msg}, 0, ""
		}
		return r
	}
	stopped := rec("h3", "zelda-oot", "The Legend of Zelda: Ocarina of Time (USA)", "n64", "failed", 0, 9, "confirmation_required", "needs confirmation")
	stopped.ContentLength, stopped.CatalogSize = 1234<<20, 40<<20
	return []app.HistoryRecord{
		rec("h1", "kirby", "Kirby's Dream Land (USA, Europe)", "gb", "done", 512<<10, 0, "", ""),
		rec("h2", "contra", "Contra (USA)", "nes", "failed", 0, 5, "size_mismatch", "the downloaded file has the wrong size"),
		stopped,
		rec("h4", "mario-world", "Super Mario World (USA)", "snes", "skipped", 0, 14, "", ""),
		rec("h5", "metroid", "Super Metroid (Japan, USA)", "snes", "canceled", 0, 20, "", ""),
	}
}

// renderSettings is a configuration as the service gives it: defaults, values from the file and one value for each
// editor.
func renderSettings() []app.Setting {
	return []app.Setting{
		{Key: "download.concurrency", Value: 2, Source: "default"},
		{Key: "download.on_exists", Value: "skip", Source: "file"},
		{Key: "download.boxart", Value: true, Source: "default"},
		{Key: "download.prefer.languages", Value: []string{"en", "pt"}, Source: "file"},
		{Key: "download.prefer.size", Value: "none", Source: "default"},
		{Key: "gui.confirm_button", Value: "south", Source: "default"},
		{Key: "gui.text_size", Value: "normal", Source: "default"},
		{Key: "gui.safe_area", Value: "auto", Source: "default"},
		{Key: "interface.language", Value: "auto", Source: "default"},
		{Key: "interface.confirm_downloads", Value: true, Source: "default"},
	}
}

// openFromMenu opens a screen from the Start menu, which reads the same at every size (the grid of tiles does not):
// the items are Home, Queue, History, Settings, Buttons and Quit, with focus on Home.
func (p *player) openFromMenu(downs int) {
	p.t.Helper()
	p.press(input.Menu)
	for i := 0; i < downs; i++ {
		p.press(input.Down)
	}
	p.press(input.Confirm)
}

// fourPads are four joysticks: three that SDL maps and one it does not.
func fourPads() []input.PadInfo {
	return []input.PadInfo{
		{Name: "Pad One", GUID: "03000000aaaa0000", Type: input.TypeXbox360, Mapped: true},
		{Name: "Pad Two", GUID: "03000000bbbb0000", Type: input.TypePS4, Mapped: true},
		{Name: "Pad Three", GUID: "03000000cccc0000", Mapped: true},
		{Name: "Pad Four", GUID: "03000000dddd0000"},
	}
}

func (p *player) repeat(a input.Action, n int) {
	p.t.Helper()
	for i := 0; i < n; i++ {
		p.press(a)
	}
}

// stopped is a download that needs the person's OK at its real size (T-56): the catalog says est, the proxy says real.
func stopped(id app.JobID, slug, title string, real, est int64) app.JobSnapshot {
	return app.JobSnapshot{
		ID: id, Slug: slug, Title: title, Platform: "n64", LinkIndex: 1, State: app.StateFailed, Expected: real, CatalogSize: est,
		Err: &app.Error{Code: "confirmation_required", Message: "needs confirmation", Args: map[string]any{"size": real, "path": "/userdata/roms/n64"}},
	}
}

// queueJobs is a Queue with one job of each state that has its own look.
func queueJobs(p *player) {
	p.job(app.JobSnapshot{ID: "1", Slug: "mario-world", Title: "Super Mario World (USA)", State: app.StateTransferring, Bytes: 6 << 20, Expected: 10 << 20, RateBps: 2 << 20, Started: p.now})
	p.job(app.JobSnapshot{ID: "2", Slug: "metroid", Title: "Super Metroid (Japan, USA)", State: app.StateWarming})
	p.job(app.JobSnapshot{ID: "3", Slug: "sonic2", Title: "Sonic the Hedgehog 2 (World)", State: app.StateQueued})
	p.job(app.JobSnapshot{ID: "4", Slug: "kirby", Title: "Kirby's Dream Land (USA, Europe)", State: app.StateDone, Name: "kirby.gb", Path: "/userdata/roms/gb/kirby.gb", Bytes: 1 << 19, Expected: 1 << 19, Started: p.now, Finished: p.now.Add(12 * time.Second)})
	p.job(app.JobSnapshot{ID: "5", Slug: "zelda-links", Title: "The Legend of Zelda: Link's Awakening DX (USA, Europe)", State: app.StateFailed, Err: &app.Error{Code: app.CodeNetwork, Message: "down"}})
	p.job(stopped("6", "zelda-oot", "The Legend of Zelda: Ocarina of Time (USA)", 1234<<20, 40<<20))
	p.openQueue()
}

// stoppedQueue is a Queue whose first row is a job that waits for the OK (the focus starts on the first job row,
// and the newest finished job is the first of the Finished group).
func stoppedQueue(p *player, est int64) {
	p.job(app.JobSnapshot{ID: "1", Slug: "kirby", Title: "Kirby's Dream Land (USA, Europe)", State: app.StateDone, Name: "kirby.gb", Path: "/userdata/roms/gb/kirby.gb", Bytes: 1 << 19, Expected: 1 << 19, Started: p.now, Finished: p.now.Add(12 * time.Second)})
	p.job(app.JobSnapshot{ID: "2", Slug: "zelda-links", Title: "The Legend of Zelda: Link's Awakening DX (USA, Europe)", State: app.StateFailed, Err: &app.Error{Code: app.CodeNetwork, Message: "down"}})
	p.job(stopped("3", "zelda-oot", "The Legend of Zelda: Ocarina of Time (USA)", 1234<<20, est))
	p.openQueue()
}

// scene is one screen to render. expect lists texts the screen must show: a scene whose steps drift (a focus that
// lands on another row, a screen that did not open, a build without the feature) fails instead of writing a PNG of
// the wrong screen, which would mislead whoever reviews the renders. Each is looked for in the text runs of the
// draw list, joined, so a line that wrapped still matches.
type scene struct {
	name, what string
	expect     []string
	confirm    bool                 // interface.confirm_downloads on
	svc        func() *guitest.Fake // the service of the scene; the shared catalog when nil
	lang       string               // the interface language; English when empty
	hold       bool                 // the start commands do not run: the scene is the Splash
	steps      func(p *player)
}

var renderScenes = []scene{
	{name: "splash", what: "T-83: the Splash with the moth (wings down) before the wordmark, the sky, and the spinner", expect: []string{"mothdex", "Starting…"}, hold: true, steps: func(p *player) { p.advance(300 * time.Millisecond) }},
	{name: "splash-wings-up", what: "T-83: the Splash 250 ms later: the wings up", expect: []string{"Starting…"}, hold: true, steps: func(p *player) { p.advance(550 * time.Millisecond) }},
	{name: "splash-ru", lang: "ru_ru", what: "T-83: the Splash in ru_ru", expect: []string{"mothdex"}, hold: true, steps: func(p *player) { p.advance(300 * time.Millisecond) }},
	{name: "home", what: "Home, first focus", expect: []string{"Search", "Platforms", "Queue", "Settings"}, steps: func(*player) {}},
	{name: "home-menu", what: "Home with the Start menu open", expect: []string{"Quit"}, steps: func(p *player) { p.press(input.Menu) }},
	{name: "search-keyboard", what: "Search, empty field and on-screen keyboard", expect: []string{"0/80", "space", "Del"}, steps: func(p *player) { p.press(input.Confirm) }},
	{name: "search-typed", what: "Search with a typed query", expect: []string{"5/80"}, steps: func(p *player) {
		p.press(input.Confirm)
		p.send(gui.TextMsg{Text: "zelda"})
	}},
	{name: "results", what: "Results of a search, with covers", expect: []string{"3 results", "The Legend of Zelda: Ocarina of Time (USA)"}, steps: func(p *player) {
		p.press(input.Confirm)
		p.send(gui.TextMsg{Text: "zelda"})
		p.press(input.Menu)
	}},
	{name: "entry", what: "Entry with two links", expect: []string{"Links", "Myrient", "Archive"}, steps: func(p *player) {
		p.press(input.Confirm)
		p.send(gui.TextMsg{Text: "link to the past"})
		p.press(input.Menu, input.Confirm)
	}},
	{name: "queue", what: "Queue: transferring, warming, queued, done, failed and a job that needs the OK", expect: []string{"Super Mario World (USA)", "Queued", "Finished (3)", "Needs your OK: the file is 1.2 GB, listed as 40.0 MB.", "Cannot reach the Mothdex server."}, steps: queueJobs},
	{name: "queue-sheet-active", what: "Queue action sheet of a running job", expect: []string{"Cancel download", "Details", "Close"}, steps: func(p *player) { queueJobs(p); p.press(input.Confirm) }},
	{name: "queue-details-failed", what: "Queue details of a failed job (network)", expect: []string{"Error:", "Cannot reach the Mothdex server."}, steps: func(p *player) {
		queueJobs(p)
		// The rows: Mario, Metroid, Sonic, then the Finished group, newest first: the job that needs the OK, the failed one.
		p.press(input.Down, input.Down, input.Down, input.Down, input.Info)
	}},
	{name: "anyway-row", what: "T-56: the row of a download that needs the OK (warn color and !)", expect: []string{"Needs your OK: the file is 1.2 GB, listed as 40.0 MB."}, steps: func(p *player) { stoppedQueue(p, 40<<20) }},
	{name: "anyway-sheet", what: "T-56: its action sheet: Download anyway, Details, Close", expect: []string{"Download anyway", "Details", "Close"}, steps: func(p *player) { stoppedQueue(p, 40<<20); p.press(input.Confirm) }},
	{name: "anyway-dialog", what: "T-56: the dialog with the real and the catalog size", expect: []string{"This file is 1.2 GB, not 40.0 MB.", "Don't download", "Download 1.2 GB"}, steps: func(p *player) {
		stoppedQueue(p, 40<<20)
		p.press(input.Confirm, input.Confirm)
	}},
	{name: "anyway-dialog-unknown", what: "T-56: the dialog when the catalog had no size", expect: []string{"This file is 1.2 GB.", "The catalog had no size for it.", "Don't download"}, steps: func(p *player) {
		stoppedQueue(p, 0)
		p.press(input.Confirm, input.Confirm)
	}},
	{name: "anyway-details", what: "T-56: Details of the stopped job", expect: []string{"Status:", "Size: 1.2 GB"}, steps: func(p *player) { stoppedQueue(p, 40<<20); p.press(input.Info) }},
	{name: "anyway-toast", what: "T-56: the toast on Home when the job stops", expect: []string{"Needs your OK: The Legend of Zelda: Ocarina of Time (USA)"}, steps: func(p *player) {
		p.job(stopped("1", "zelda-oot", "The Legend of Zelda: Ocarina of Time (USA)", 1234<<20, 40<<20))
	}},
	{name: "history", what: "T-78: History, newest first: done, failed, stopped on its size, skipped, canceled", expect: []string{"› History", "All · 5 records", "Contra (USA)", "Needs your OK: the file is 1.2 GB, listed as 40.0 MB."}, steps: func(p *player) { p.openFromMenu(2) }},
	{name: "history-failed", what: "T-78: History with Failed only", expect: []string{"Failed only · 2 records", "Show all", "Contra (USA)"}, steps: func(p *player) { p.openFromMenu(2); p.press(input.Info) }},
	{name: "history-details", what: "T-78: History details of a done record, with Download again", expect: []string{"Kirby's Dream Land (USA, Europe)", "Download again", "Close", "Attempts: 1"}, steps: func(p *player) { p.openFromMenu(2); p.press(input.Confirm) }},
	{name: "history-stopped-details", what: "T-78: History details of a record stopped on its size: Download anyway", expect: []string{"Download anyway", "Status:", "Size: 1.2 GB"}, steps: func(p *player) {
		p.openFromMenu(2)
		p.repeat(input.Down, 2)
		p.press(input.Confirm)
	}},
	{name: "history-stopped-dialog", what: "T-78: the T-56 dialog opened from the history", expect: []string{"This file is 1.2 GB, not 40.0 MB.", "Don't download", "Download 1.2 GB"}, steps: func(p *player) {
		p.openFromMenu(2)
		p.repeat(input.Down, 2)
		p.press(input.Confirm)
		p.press(input.Confirm)
	}},
	{name: "history-empty", what: "T-78: History with nothing in it", expect: []string{"No downloads yet. Search for a game to start."}, svc: func() *guitest.Fake { s := renderCatalog(); s.Records = nil; return s }, steps: func(p *player) { p.openFromMenu(2) }},
	{name: "settings", what: "T-78: Settings, first page", expect: []string{"› Settings", "Concurrent downloads", "Preferred languages", "En, Pt"}, steps: func(p *player) { p.openFromMenu(3) }},
	{name: "settings-scrolled", what: "T-78: Settings, last page (Storage and system, About)", expect: []string{"Confirm large downloads", "Cache", "84.0 MB", "About", "More settings: run \"mothdex config list\" over SSH."}, steps: func(p *player) { p.openFromMenu(3); p.repeat(input.Down, 12) }},
	{name: "settings-choice", what: "T-78: the list of a choice row", expect: []string{"Skip", "Rename"}, steps: func(p *player) { p.openFromMenu(3); p.press(input.Down); p.press(input.Confirm) }},
	{name: "settings-languages", what: "T-78: the ordered list of preferred languages", expect: []string{"En", "Pt", "+ Add"}, steps: func(p *player) { p.openFromMenu(3); p.repeat(input.Down, 3); p.press(input.Confirm) }},
	{name: "settings-language-item", what: "T-78: Move up, Move down, Remove for one language", expect: []string{"Move down", "Remove"}, steps: func(p *player) {
		p.openFromMenu(3)
		p.repeat(input.Down, 3)
		p.press(input.Confirm)
		p.press(input.Confirm)
	}},
	{name: "settings-margin", what: "T-78: the calibration view of the screen margin", expect: []string{"Adjust until you can see all four corners."}, steps: func(p *player) {
		p.openFromMenu(3)
		p.repeat(input.Down, 7)
		p.press(input.Confirm)
		p.press(input.Right)
		p.press(input.Left)
	}},
	{name: "settings-cache-dialog", what: "T-78: Clear cache (84.0 MB)? with Keep as the default", expect: []string{"Clear cache (84.0 MB)?", "Keep", "Clear"}, steps: func(p *player) { p.openFromMenu(3); p.repeat(input.Down, 10); p.press(input.Confirm) }},
	{name: "settings-about", what: "T-78: About, read-only", expect: []string{"Version: 0.3.0", "Catalog entries:"}, steps: func(p *player) { p.openFromMenu(3); p.repeat(input.Down, 12); p.press(input.Confirm) }},
	{name: "settings-readonly", what: "T-78: a value set by the environment says so in the hint bar", expect: []string{"Set by MOTHDEX_DOWNLOAD_CONCURRENCY"},
		svc: func() *guitest.Fake {
			s := renderCatalog()
			s.Settings[0] = app.Setting{Key: "download.concurrency", Value: 4, Source: "env"}
			return s
		}, steps: func(p *player) { p.openFromMenu(3) }},
	{name: "settings-error", what: "T-78: a service error under the row, old value kept", expect: []string{"Not saved: Invalid setting download.concurrency=3 (from file)."},
		svc: func() *guitest.Fake {
			s := renderCatalog()
			s.ConfigSetErr = map[string]error{"download.concurrency": &app.Error{Code: app.CodeInvalidConfig, Message: "out of range", Args: map[string]any{"key": "download.concurrency", "value": "3", "source": "file"}}}
			return s
		}, steps: func(p *player) { p.openFromMenu(3); p.press(input.Right) }},
	{name: "buttons", what: "T-78: Start menu > Buttons on Home", expect: []string{"Buttons", "Open", "Quit", "Move"}, steps: func(p *player) { p.press(input.Menu); p.repeat(input.Down, 4); p.press(input.Confirm) }},
	{name: "buttons-queue", what: "T-78: Buttons on the Queue (pad: Nintendo labels)", expect: []string{"Actions", "Details", "Move"}, steps: func(p *player) {
		p.send(gui.ControllerMsg{Type: 5})
		p.press(input.Queue)
		p.press(input.Menu)
		p.repeat(input.Down, 4)
		p.press(input.Confirm)
	}},
	{name: "history-pt", lang: "pt_br", what: "T-78: History in pt_br: the reasons are in the interface language", expect: []string{"Histórico", "Tamanho de arquivo errado"}, steps: func(p *player) { p.openFromMenu(2) }},
	{name: "history-ru", lang: "ru_ru", what: "T-78: History in ru_ru", expect: []string{"История", "Неверный размер файла"}, steps: func(p *player) { p.openFromMenu(2) }},
	{name: "settings-es", lang: "es_es", what: "T-78: Settings in es_es: long labels wrap, none is cut", expect: []string{"Ajustes", "Descargas simultáneas"}, steps: func(p *player) { p.openFromMenu(3) }},
	{name: "settings-ru", lang: "ru_ru", what: "T-78: Settings in ru_ru, the longest labels", expect: []string{"Настройки", "Одновременных загрузок"}, steps: func(p *player) { p.openFromMenu(3) }},
	{name: "settings-ru-scrolled", lang: "ru_ru", what: "T-78: Settings in ru_ru, last page", expect: []string{"Подтверждать большие загрузки"}, steps: func(p *player) { p.openFromMenu(3); p.repeat(input.Down, 12) }},
	{name: "pad-none", what: "T-80: no controller: the warn toast and the No controller badge in the top bar", expect: []string{"No controller", "Connect a controller. On a keyboard: arrows, Enter, Esc."}, steps: func(p *player) { p.send(gui.PadsMsg{}) }},
	{name: "pad-none-ru", lang: "ru_ru", what: "T-80: the same in ru_ru (the longest badge and sentence)", expect: []string{"Нет контроллера", "Подключите контроллер."}, steps: func(p *player) { p.send(gui.PadsMsg{}) }},
	{name: "pad-unmapped", what: "T-80: a joystick SDL has no mapping for: the toast names it and points to About", expect: []string{"No controller", "Controller not recognized: Mystery Gamepad. See Settings › About."}, steps: func(p *player) {
		p.send(gui.PadsMsg{Pads: []input.PadInfo{{Name: "Mystery Gamepad", GUID: "03000000aaaabbbb0000000000000000"}}})
		p.advance(7 * time.Second) // past the first notice (no controller that works)
	}},
	{name: "pad-unmapped-long", what: "T-80: a long device name is the only thing cut", expect: []string{"See Settings › About."}, steps: func(p *player) {
		p.send(gui.PadsMsg{Pads: []input.PadInfo{{Name: "Shenzhen Generic USB Two-Player Gamepad Adapter With Turbo Button Model 4000", GUID: "03000000aaaabbbb0000000000000000"}, {Name: "Xbox 360 Controller", Type: 1, Mapped: true}}})
	}},
	{name: "settings-about-pads", what: "T-80: About lists each controller, where to add a mapping for the unknown one, and the GUID", expect: []string{"Controller: Xbox 360 Controller", "GUID: 030000005e0400008e02000014050000", "Controller: Mystery Gamepad", "GUID: Unavailable on this system"}, steps: func(p *player) {
		p.send(gui.PadsMsg{Pads: []input.PadInfo{{Name: "Xbox 360 Controller", GUID: "030000005e0400008e02000014050000", Type: 1, Mapped: true}, {Name: "Mystery Gamepad"}}})
		p.advance(7 * time.Second)
		p.openFromMenu(3)
		p.repeat(input.Down, 12)
		p.press(input.Confirm)
	}},
	{name: "settings-about-pads-4", what: "T-80: About with four controllers, one unknown: each is a group", expect: []string{"Controller: Pad One", "Controller: Pad Four", "GUID: 03000000dddd0000"}, steps: func(p *player) {
		p.send(gui.PadsMsg{Pads: fourPads()})
		p.advance(7 * time.Second)
		p.openFromMenu(3)
		p.repeat(input.Down, 12)
		p.press(input.Confirm)
	}},
	{name: "settings-about-pads-many", what: "T-80: About with eight controllers scrolls: a triangle says which way there is more, Move in the hint bar", expect: []string{"Controller: Pad One", "Move"}, steps: func(p *player) {
		pads := fourPads()
		for _, n := range []string{"Pad Five", "Pad Six", "Pad Seven", "Pad Eight"} {
			pads = append(pads, input.PadInfo{Name: n, GUID: "0300000099990000", Mapped: true})
		}
		p.send(gui.PadsMsg{Pads: pads})
		p.advance(7 * time.Second)
		p.openFromMenu(3)
		p.repeat(input.Down, 12)
		p.press(input.Confirm)
	}},
	{name: "settings-system-ram", what: "T-93: the System card with a RAM disk: two warn lines of their own, no Not verified prefix, Verified kept", expect: []string{"System", "Verified", "Games folder is on a RAM disk: downloaded games are lost at reboot.", "Data folder is on a RAM disk: history, cache and settings are lost at reboot."},
		svc: func() *guitest.Fake {
			s := renderCatalog()
			s.Profile = app.ProfileInfo{ID: "batocera", Name: "Batocera", RomRoot: "/userdata/roms", Verified: true,
				StorageWarnings: []string{"Games folder is on a RAM disk: downloaded games are lost at reboot.", "Data folder is on a RAM disk: history, cache and settings are lost at reboot."}}
			return s
		},
		steps: func(p *player) { p.openFromMenu(3); p.repeat(input.Down, 11); p.press(input.Confirm) }},
	{name: "settings-about-nopad", what: "T-80: About with no controller", expect: []string{"Controller: None connected"}, steps: func(p *player) {
		p.send(gui.PadsMsg{})
		p.advance(7 * time.Second)
		p.openFromMenu(3)
		p.repeat(input.Down, 12)
		p.press(input.Confirm)
	}},
	{name: "settings-export-ask", what: "T-115: Settings, About, Export logs: the confirmation with Cancel as the default", expect: []string{"Export the logs?", "Cancel", "Export logs"}, steps: aboutExport},
	{name: "settings-export-done", what: "T-115: where the exported zip is", expect: []string{"Logs saved", "OK"}, steps: func(p *player) {
		aboutExport(p)
		p.advance(200 * time.Millisecond)
		p.press(input.Right, input.Confirm)
	}},
	{name: "error-in-place", what: "T-115: an error in place with the code, the ref and the log", expect: []string{"ref: ", "log: ", "latest.log"}, svc: failingCatalog, steps: func(p *player) {
		p.press(input.Confirm)
		p.press(input.Up, input.Up, input.Confirm)
	}},
	{name: "crash", what: "T-115: the crash screen", expect: []string{"Something went wrong", "code: internal · ref: 7F3K2Q", "latest.log", "Export logs", "Quit"}, steps: crashed},
	{name: "crash-pt", lang: "pt_br", what: "T-115: the crash screen in pt_br", expect: []string{"Algo deu errado", "7F3K2Q", "Exportar logs"}, steps: crashed},
	{name: "crash-ru", lang: "ru_ru", what: "T-115: the crash screen in ru_ru, the longest texts", expect: []string{"Что-то пошло не так", "7F3K2Q"}, steps: crashed},
	{name: "update-about", what: "T-117: Settings, About with a newer version: Update and Export logs", expect: []string{"New version: 0.4.0", "Update", "Export logs"}, steps: updateAbout},
	{name: "update-confirm", what: "T-117: the confirmation after the download is verified, Cancel as the default", expect: []string{"Update 0.3.0 to 0.4.0?", "SHA-256", "Cancel", "Update"}, steps: func(p *player) {
		updateAbout(p)
		p.press(input.Confirm)
	}},
	{name: "update-confirm-ru", lang: "ru_ru", what: "T-117: the confirmation in ru_ru", expect: []string{"0.4.0"}, steps: func(p *player) {
		updateAbout(p)
		p.press(input.Confirm)
	}},
	{name: "update-done", what: "T-117: installed, restart now or later", expect: []string{"Updated to 0.4.0", "Later", "Restart"}, steps: func(p *player) {
		updateAbout(p)
		p.press(input.Confirm)
		p.advance(200 * time.Millisecond)
		p.press(input.Right, input.Confirm)
	}},
	{name: "exit", what: "T-83: the exit screen after Quit with nothing running", expect: []string{"Returning to"}, steps: func(p *player) { p.send(gui.QuitRequestMsg{}) }},
	{name: "quit-busy", what: "Quit with downloads running", expect: []string{"Keep downloading"}, steps: func(p *player) {
		p.job(app.JobSnapshot{ID: "1", Slug: "mario-world", Title: "Super Mario World (USA)", State: app.StateTransferring, Bytes: 1 << 19, Expected: 1 << 20})
		p.send(gui.QuitRequestMsg{})
	}},
}

func aboutExport(p *player) {
	p.openFromMenu(3)
	p.repeat(input.Down, 12)
	p.press(input.Confirm)
	p.advance(200 * time.Millisecond)
	p.press(input.Confirm)
}

func crashed(p *player) { p.m = gui.NewCrash(p.o, nil, "7F3K2Q", p.now) }

// renderUpdates is an update that goes straight to its question and installs when told to.
type renderUpdates struct{}

func (renderUpdates) Check(context.Context) (gui.Release, error) {
	return gui.Release{Version: "0.4.0", PageURL: "https://github.com/Or4cu1o/mothdex-cli/releases/tag/v0.4.0", Notify: true}, nil
}

func (renderUpdates) MarkNotified(string) error { return nil }

func (renderUpdates) Apply(_ context.Context, _ func(gui.UpdateProgress), confirm func(gui.Staged) bool) error {
	confirm(gui.Staged{From: "0.3.0", To: "0.4.0", Signature: "none", Notes: "## [0.4.0] - 2026-12-01\n### Added\n- T-117: the GUI tells you about a new version and updates itself when you ask.\n- Session logs and crash files.\n### Fixed\n- With no display the GUI exits with code 8."})
	return nil
}

// updateAbout starts over with an updater, reaches Home so that the check runs, and opens About.
func updateAbout(p *player) {
	p.o.Updates = renderUpdates{}
	m, cmds := gui.New(p.o, p.now)
	p.m = m
	p.run(cmds)
	p.openFromMenu(3)
	p.repeat(input.Down, 12)
	p.press(input.Confirm)
	p.advance(7 * time.Second) // past the notice
}

func failingCatalog() *guitest.Fake {
	s := renderCatalog()
	s.PlatformErr = &app.Error{Code: app.CodeNetwork, Message: "down"}
	return s
}

func TestRenderScenes(t *testing.T) {
	dir := os.Getenv("GUI_RENDER_DIR")
	if dir == "" {
		t.Skip("GUI_RENDER_DIR is not set: the render harness writes nothing")
	}
	if err := os.MkdirAll(dir, 0o750); err != nil { //nolint:gosec // the folder the person chose with GUI_RENDER_DIR
		t.Fatal(err)
	}
	var index []string
	for _, size := range renderSizes {
		w, h := size[0], size[1]
		t.Run(fmt.Sprintf("%dx%d", w, h), func(t *testing.T) {
			be, font := headless(t, w, h) // one window per size: SDL stays on this goroutine's thread
			for _, sc := range renderScenes {
				svc := renderCatalog()
				if sc.svc != nil {
					svc = sc.svc()
				}
				p := newPlayer(t, font, w, h, svc, sc.confirm, sc.lang, sc.hold)
				sc.steps(p)
				p.advance(400 * time.Millisecond) // past the dialog's input pause; covers and toasts settle
				img, shown := drawAndRead(t, be, p.m, p.now, w, h)
				name := fmt.Sprintf("%s_%dx%d.png", sc.name, w, h)
				flat := strings.ReplaceAll(shown, "\n", " ") // a line that wrapped is one sentence again
				for _, want := range sc.expect {
					if !strings.Contains(flat, want) {
						t.Errorf("%s: the screen does not show %q, so this is not the screen the scene means; it shows: %s", name, want, strings.ReplaceAll(shown, "\n", " | "))
					}
				}
				if colors := distinctColors(img); colors < 6 {
					t.Errorf("%s: the frame has only %d colors, SDL drew nothing useful", name, colors)
				}
				writePNG(t, filepath.Join(dir, name), img)
				index = append(index, fmt.Sprintf("%s\t%s", name, sc.what))
			}
		})
	}
	sort.Strings(index)
	body := "GUI render harness (T-67): real SDL2 software renderer, pinned Noto Sans, dummy video driver.\n" +
		"Nothing here is compared with a stored image; look at the files.\n\n" + strings.Join(index, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "index.txt"), []byte(body), 0o600); err != nil { //nolint:gosec // the same folder
		t.Fatal(err)
	}
}

// drawAndRead draws the model with the real backend and reads the whole frame back.
// It also returns the text runs of the draw list, joined by newlines, for the checks of a scene.
func drawAndRead(t *testing.T, be *Backend, m gui.Model, now time.Time, w, h int) (*image.RGBA, string) {
	t.Helper()
	var list gdraw.List
	m.Render(now, &list)
	if err := be.Draw(layoutOf(m), &list); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, w*h*4)
	if err := be.lib.ReadPixels(be.ren, Rect{W: int32(w), H: int32(h)}, PixelFormatARGB8888, buf, int32(w*4)); err != nil { //nolint:gosec // the window sizes of the table above
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(buf); i += 4 { // ARGB8888 in memory is B, G, R, A on a little-endian machine
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = buf[i+2], buf[i+1], buf[i], 0xff
	}
	return img, strings.Join(list.Texts(), "\n")
}

func distinctColors(img *image.RGBA) int {
	seen := map[[3]byte]struct{}{}
	for i := 0; i < len(img.Pix) && len(seen) < 64; i += 4 {
		seen[[3]byte{img.Pix[i], img.Pix[i+1], img.Pix[i+2]}] = struct{}{}
	}
	return len(seen)
}

func writePNG(t *testing.T, path string, img image.Image) {
	t.Helper()
	f, err := os.Create(path) //nolint:gosec // a file name built here, in the folder the person chose with GUI_RENDER_DIR
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}
