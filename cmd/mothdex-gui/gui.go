//go:build gui

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/buildinfo"
	"github.com/Or4cu1o/mothdex-cli/internal/compose"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/assets"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/loop"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/sdl"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/text"
	"github.com/Or4cu1o/mothdex-cli/locales"
)

const userMappingsFile = "gamecontrollerdb.txt" // in paths.data (docs/contracts/cli/gui.md section 4)

// runGUI is the normal mode: build the service, open SDL, run the loop. Every failure goes to the log file
// as well as to stderr, because a Port has no visible terminal.
func runGUI(o options, stderr io.Writer) (code int) {
	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	sess, err := compose.Open(ctx, compose.Options{ConfigFile: o.config, Profile: o.profile, Lang: o.lang, Downloads: true, Session: true})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "mothdex-gui: %v\n", err)
		return configExitCode(err)
	}
	defer sess.Close()
	log := sess.Log
	defer func() {
		if r := recover(); r != nil {
			log.Error("gui: internal error", "panic", r)
			_, _ = fmt.Fprintf(stderr, "mothdex-gui: internal error: %v (see %s)\n", r, sess.LogPath)
			code = exitInternal
		}
	}()
	for _, w := range sess.Warnings {
		log.Warn(w)
	}
	fail := func(code int, err error) int {
		log.Error("gui: "+err.Error(), "exit", code)
		_, _ = fmt.Fprintf(stderr, "mothdex-gui: %v (see %s)\n", err, sess.LogPath)
		return code
	}

	lib, err := sdl.Open()
	if err != nil {
		return fail(exitSDL, err)
	}
	if v := lib.Version(); !v.AtLeast(sdl.MinimumVersion()) {
		return fail(exitSDL, fmt.Errorf("%w: have %s, need %s", sdl.ErrTooOld, v, sdl.MinimumVersion()))
	} else if !v.AtLeast(sdl.TargetVersion) {
		log.Warn("gui: SDL2 is older than the version this program was meant for", "have", v.String(), "meant", sdl.TargetVersion.String())
	}
	font, err := text.Parse(assets.NotoSans)
	if err != nil {
		return fail(exitInternal, err)
	}
	tr, fallback, err := translator(sess.T)
	if err != nil {
		return fail(exitInternal, err)
	}

	warn := func(msg string) { log.Warn(msg) }
	mappings, mappingCounts := input.LoadMappingsCounted(assets.ControllerDB, filepath.Join(sess.DataDir, userMappingsFile), input.SDLPlatform(runtime.GOOS), os.Getenv, warn)
	be, err := sdl.OpenBackend(lib, font, sdl.BackendOptions{
		Title: "mothdex", Driver: o.driver, Windowed: o.height > 0, Width: o.width, Height: o.height, Mappings: mappings, Log: log,
	})
	if err != nil {
		return fail(exitSDL, err)
	}
	defer be.Close()
	w, h := be.Size()
	log.Info("gui: diagnostics", diagnostics{
		SDLVersion: lib.Version().String(), Driver: be.Driver(), Lookup: os.LookupEnv, Mappings: mappingCounts, Pads: be.ControllerTypes(),
	}.attrs()...)
	log.Info("gui: started", "driver", be.Driver(), "width", w, "height", h, "version", buildinfo.Get().Version)

	model, cmds := gui.New(gui.Options{
		Service: sess.Svc, T: tr, Font: font, Clock: gui.SystemClock(), Log: log, DataDir: sess.DataDir,
		Version: buildinfo.Get().Version, LocaleFallback: fallback, Width: w, Height: h, Safe: gui.AutoSafeArea,
		LogPath: sess.LogPath, Prefer: sess.Prefer, Boxart: sess.Boxart, Confirm: sess.Confirm,
	}, time.Now())

	final, err := loop.Run(ctx, loop.Config{
		Model: model, Cmds: cmds, Backend: be, Events: sess.Svc, Clock: time.Now, Signals: terminationSignals(), Log: log,
	})
	if err != nil {
		return fail(exitSDL, err)
	}
	if leaveStuck(final.ExitTimedOut(), log, be.Close, os.Exit) {
		return exitOK // only reached by a test: os.Exit does not return
	}
	return exitOK
}

// leaveStuck ends the process at once when the exit sequence ran out of its budget. A Wait or Close that did
// not answer in time is still running, and the deferred session close would call Close again and wait as long
// as the first one, so the frontend would have to kill the Port. It frees SDL, which does not wait on the
// service, and exits with the code of a normal exit (docs/contracts/cli/gui.md section 5). It reports whether it
// did, for a test that passes a fake exit.
func leaveStuck(timedOut bool, log *slog.Logger, closeBackend func(), exit func(int)) bool {
	if !timedOut {
		return false
	}
	log.Warn("gui: the exit sequence used up its 2 s budget, leaving without closing the service again")
	closeBackend()
	exit(exitOK)
	return true
}

// terminationSignals is closed on the first SIGTERM or SIGINT: the frontend closing the Port, or Ctrl+C in a
// desktop test. SDL's own handlers are off, so the exit sequence runs through the model.
func terminationSignals() <-chan struct{} {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		<-ch
		close(done)
	}()
	return done
}

// translator returns the translator the GUI draws with: the session's, except for the languages the embedded
// font cannot draw, which get en_us and a notice on Home.
func translator(t *i18n.Translator) (*i18n.Translator, bool, error) {
	if !gui.NeedsEnglish(t.Locale()) {
		return t, false, nil
	}
	bundle, err := i18n.Load(locales.FS)
	if err != nil {
		return nil, false, err
	}
	return bundle.New(i18n.Fallback), true, nil
}

// configExitCode is 2 for a configuration problem and 3 for anything else that stopped the start.
func configExitCode(err error) int {
	var ae *app.Error
	if errors.As(err, &ae) && ae.Code == app.CodeInvalidConfig {
		return exitConfig
	}
	return exitInternal
}
