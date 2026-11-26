//go:build gui && linux

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/assets"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/guitest"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/loop"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/sdl"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/text"
	"github.com/Or4cu1o/mothdex-cli/internal/update"
	"github.com/Or4cu1o/mothdex-cli/locales"
)

const releaseOrigin = "https://github.com/Or4cu1o/mothdex-cli/releases"

// localRelease sends the updater's requests for the pinned origin to a local TLS server; anything else fails the
// test, so nothing can leave the machine.
type localRelease struct {
	t      *testing.T
	base   http.RoundTripper
	server *url.URL
}

func (l localRelease) RoundTrip(req *http.Request) (*http.Response, error) {
	if !strings.HasPrefix(req.URL.String(), releaseOrigin) {
		l.t.Errorf("a request outside the release origin: %s", req.URL)
		return nil, fmt.Errorf("refused")
	}
	to := *req.URL
	to.Scheme, to.Host = l.server.Scheme, l.server.Host
	local := req.Clone(req.Context())
	local.URL = &to
	return l.base.RoundTrip(local)
}

type signed struct{}

func (signed) Verify(context.Context, update.VerifyInput) (string, error) { return "cosign", nil }

// releaseArchive is a release archive with exactly the members the updater accepts (release-artifacts.md section 1).
func releaseArchive(t *testing.T, program string, binary []byte, notices []string) []byte {
	t.Helper()
	files := map[string][]byte{program: binary, "LICENSE": []byte("x"), "README.md": []byte("x"),
		"CHANGELOG.md": []byte("## [2.0.0]\n### Added\n- The new thing.\n## [1.0.0]\n- Old.")}
	for _, n := range notices {
		files["THIRD_PARTY/"+n] = []byte("x")
	}
	var b bytes.Buffer
	z := gzip.NewWriter(&b)
	a := tar.NewWriter(z)
	for name, data := range files {
		if err := a.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(data))}); err != nil {
			t.Fatal(err)
		}
		if _, err := a.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// installation is a managed installation of 1.0.0 with the GUI, as install.sh records it, and a local release 2.0.0.
func installation(t *testing.T) (dir string, newBinary []byte, u *update.Updater, data string) {
	t.Helper()
	dir, data = t.TempDir(), t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // an installation folder
		t.Fatal(err)
	}
	for _, name := range []string{"mothdex", "mothdex-gui"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("old"), 0o755); err != nil { //nolint:gosec // an installed program
			t.Fatal(err)
		}
	}
	target := runtime.GOOS + "_" + runtime.GOARCH
	state, err := json.Marshal(update.State{SchemaVersion: 1, Source: "installer", Installer: "install.sh", Dir: dir, Version: "1.0.0",
		Target: target, Profile: "generic", GUI: true, InstalledAt: "2026-11-26T14:00:00Z", UpdatedAt: "2026-11-26T14:00:00Z", UpdatedBy: "installer"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".mothdex-install.json"), state, 0o600); err != nil {
		t.Fatal(err)
	}

	// The program the release carries is the fixture of internal/update: it answers --version and --probe.
	built := filepath.Join(t.TempDir(), "probe")
	cmd := exec.CommandContext(context.Background(), "go", "build", "-o", built, "../../internal/update/testdata/probe") //nolint:gosec // fixed arguments
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("probe build: %v %s", err, out)
	}
	newBinary, err = os.ReadFile(built) //nolint:gosec // built just above
	if err != nil {
		t.Fatal(err)
	}
	cli := releaseArchive(t, "mothdex", newBinary, []string{"go-modules-mothdex.txt"})
	front := releaseArchive(t, "mothdex-gui", newBinary, []string{"README.md", "go-modules-mothdex.txt", "go-modules-mothdex-gui.txt",
		"SDL2-zlib.txt", "SDL_GameControllerDB-zlib.txt", "purego-Apache-2.0.txt", "x-image-BSD-3-Clause.txt", "NotoSans-OFL-1.1.txt", "Handjet-OFL-1.1.txt"})
	cliName, frontName := "mothdex_2.0.0_"+target+".tar.gz", "mothdex-gui_2.0.0_"+target+".tar.gz"
	sums := fmt.Sprintf("%x  %s\n%x  %s\n", sha256.Sum256(cli), cliName, sha256.Sum256(front), frontName)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch path := r.URL.Path; {
		case strings.HasSuffix(path, "/latest"):
			w.Header().Set("Location", releaseOrigin+"/tag/v2.0.0")
			w.WriteHeader(http.StatusFound)
		case strings.HasSuffix(path, "checksums.txt"):
			_, _ = w.Write([]byte(sums))
		case strings.HasSuffix(path, ".sigstore.json"):
			_, _ = w.Write([]byte("bundle"))
		case strings.HasSuffix(path, "/"+cliName):
			_, _ = w.Write(cli)
		case strings.HasSuffix(path, "/"+frontName):
			_, _ = w.Write(front)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	address, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	u, err = update.New(update.Options{Exe: filepath.Join(dir, "mothdex-gui"), Current: "1.0.0", DataDir: data, Verifier: signed{},
		Client: &http.Client{Transport: localRelease{t, server.Client().Transport, address}}, Log: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	return dir, newBinary, u, data
}

// padStep presses buttons once a frame that shows a text has been drawn.
type padStep struct {
	shows string
	press []input.Button
}

// scriptedPad is the real SDL backend with a gamepad that follows a script: each step waits for its screen, never
// for a time.
type scriptedPad struct {
	loop.Backend
	now    *time.Time
	script []padStep
	queue  []input.Incoming
	last   string
}

func (p *scriptedPad) Present(layout draw.Layout, l *draw.List) error {
	*p.now = p.now.Add(500 * time.Millisecond) // past the pause a dialog gives a held button
	p.last = strings.Join(l.Texts(), " | ")
	if len(p.script) > 0 && len(p.queue) == 0 && strings.Contains(p.last, p.script[0].shows) {
		for _, b := range p.script[0].press {
			for _, kind := range []input.Kind{input.ButtonPress, input.ButtonRelease} {
				p.queue = append(p.queue, input.Incoming{Kind: input.IncomingInput, Input: input.Event{Kind: kind, Button: b}})
			}
		}
		p.script = p.script[1:]
	}
	return p.Backend.Present(layout, l)
}

// Wait never lets the loop sleep on the script's clock: a timer of the model that has not come due in a moment
// comes due at once, by moving that clock to it.
func (p *scriptedPad) Wait(timeout time.Duration) (input.Incoming, bool) {
	if len(p.queue) > 0 {
		return p.Poll()
	}
	if timeout < 0 {
		return p.Backend.Wait(timeout) // until a service call answers
	}
	ev, ok := p.Backend.Wait(min(timeout, 20*time.Millisecond))
	if !ok {
		*p.now = p.now.Add(timeout)
	}
	return ev, ok
}

func (p *scriptedPad) Poll() (input.Incoming, bool) {
	if len(p.queue) > 0 {
		ev := p.queue[0]
		p.queue = p.queue[1:]
		return ev, true
	}
	return p.Backend.Poll()
}

// The acceptance of the spec, in a test environment: the notice, About, Update, the confirmation, the swap of both
// programs and the prompt to open it again, by gamepad, with the real updater against a local release.
func TestTheGUITellsAboutANewVersionAndUpdatesByGamepadToTheEnd(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	lib, err := sdl.Open()
	if err != nil {
		if os.Getenv("GUI_REQUIRE_SDL") == "1" {
			t.Fatal(err)
		}
		t.Skipf("no SDL2 here: %v", err)
	}
	isolate(t)
	dir, newBinary, u, data := installation(t)

	font, err := text.Parse(assets.NotoSans)
	if err != nil {
		t.Fatal(err)
	}
	window, err := sdl.OpenBackend(lib, font, sdl.BackendOptions{Driver: "dummy", Windowed: true, Hidden: true, Software: true, Width: 640, Height: 480})
	if err != nil {
		t.Fatal(err)
	}
	defer window.Close()
	bundle, err := i18n.Load(locales.FS)
	if err != nil {
		t.Fatal(err)
	}

	toSettings := []input.Button{input.Start, input.DPadDown, input.DPadDown, input.DPadDown, input.South}
	toAbout := append(slices.Repeat([]input.Button{input.DPadDown}, 12), input.South)
	now := time.Date(2026, 11, 26, 15, 0, 0, 0, time.UTC)
	pad := &scriptedPad{Backend: window, now: &now, script: []padStep{
		{"Version 2.0.0 is available", toSettings},
		{"Concurrent downloads", toAbout},
		{"New version: 2.0.0", []input.Button{input.South}}, // Update has the focus
		{"Update 1.0.0 to 2.0.0?", []input.Button{input.DPadRight, input.South}},
		{"Close and open Mothdex again.", []input.Button{input.South}}, // the only button quits
	}}
	updates := newUpdates(u, true)
	model, cmds := gui.New(gui.Options{Service: guitest.New(), T: bundle.New("en_us"), Font: font, Clock: gui.SystemClock(),
		DataDir: t.TempDir(), Width: 640, Height: 480, Safe: gui.AutoSafeArea, Updates: updates.model()}, now)

	stuck := make(chan struct{}) // only a failing run reaches it
	watchdog := time.AfterFunc(4*time.Minute, func() { close(stuck) })
	defer watchdog.Stop()
	final, err := loop.Run(context.Background(), loop.Config{Model: model, Cmds: cmds, Backend: pad, Signals: stuck,
		Clock: func() time.Time { return now }, Log: slog.New(slog.DiscardHandler)})
	updates.wait()
	if err != nil {
		t.Fatal(err)
	}
	if len(pad.script) != 0 {
		t.Fatalf("the script stopped before %q; the last frame: %s", pad.script[0].shows, pad.last)
	}
	if !final.Done() || final.ExitTimedOut() {
		t.Fatalf("the confirm button did not quit cleanly; the last frame: %s", pad.last)
	}
	for _, name := range []string{"mothdex", "mothdex-gui"} {
		if !bytes.Equal([]byte(readFile(t, filepath.Join(dir, name))), newBinary) {
			t.Errorf("%s was not replaced", name)
		}
	}
	var state update.State
	if err := json.Unmarshal([]byte(readFile(t, filepath.Join(dir, ".mothdex-install.json"))), &state); err != nil {
		t.Fatal(err)
	}
	if state.Version != "2.0.0" || state.UpdatedBy != "upgrade" {
		t.Errorf("the install state: %+v", state)
	}
	var cache struct {
		NotifiedVersion string `json:"notified_version"`
	}
	if err := json.Unmarshal([]byte(readFile(t, filepath.Join(data, "update-check.json"))), &cache); err != nil || cache.NotifiedVersion != "2.0.0" {
		t.Errorf("the notice was not recorded: %q (%v)", cache.NotifiedVersion, err)
	}
}
