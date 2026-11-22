//go:build gui

package sdl

import (
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
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui"
	gdraw "github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/guitest"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/text"
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
	m   gui.Model
	now time.Time
}

func newPlayer(t *testing.T, font *text.Font, w, h int, svc *guitest.Fake, confirm bool) *player {
	t.Helper()
	o := guiOptions(t, font, svc, w, h)
	o.Confirm = confirm
	m, cmds := gui.New(o, testEpoch)
	p := &player{t: t, m: m, now: testEpoch}
	p.run(cmds)
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
	return svc
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
	confirm    bool // interface.confirm_downloads on
	steps      func(p *player)
}

var renderScenes = []scene{
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
	{name: "quit-busy", what: "Quit with downloads running", expect: []string{"Keep downloading"}, steps: func(p *player) {
		p.job(app.JobSnapshot{ID: "1", Slug: "mario-world", Title: "Super Mario World (USA)", State: app.StateTransferring, Bytes: 1 << 19, Expected: 1 << 20})
		p.send(gui.QuitRequestMsg{})
	}},
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
				p := newPlayer(t, font, w, h, renderCatalog(), sc.confirm)
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
