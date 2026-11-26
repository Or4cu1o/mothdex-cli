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
	"runtime/debug"
	"syscall"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/buildinfo"
	"github.com/Or4cu1o/mothdex-cli/internal/compose"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	mlog "github.com/Or4cu1o/mothdex-cli/internal/log"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/assets"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/loop"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/sdl"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/text"
	"github.com/Or4cu1o/mothdex-cli/locales"
)

const userMappingsFile = "gamecontrollerdb.txt" // in paths.data (docs/contracts/cli/gui.md section 4)

// runGUI is the normal mode: build the service, open SDL, run the loop. Every failure gets a ref and goes to
// the session log as well as to stderr, because a Port has no visible terminal.
func runGUI(o options, logs *mlog.Session, stderr io.Writer) (code int) {
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	defer recoverInternal(&code, o, logs, stderr) // a panic before there is a service

	sess, err := compose.Open(ctx, compose.Options{ConfigFile: o.config, Profile: o.profile, Lang: o.lang, LogLevel: o.logLevel, Logs: logs, Downloads: true, Session: true})
	if err != nil {
		errCode, message := appError(err)
		ref := recordError(logs.Logger(), errCode, message, err)
		_ = logs.Close() // a configuration that did not load: the lines go to the default log folder
		_, _ = fmt.Fprintf(stderr, "mothdex-gui: %v\n", err)
		if tr, terr := cmdlineTranslator(o.config, o.lang); terr == nil {
			support(stderr, tr, errCode, ref, logs.SessionPath())
		}
		return configExitCode(err)
	}
	defer sess.Close()
	log := sess.Log
	defer recoverInternal(&code, o, logs, stderr) // runs before sess.Close, so mothdex.log gets the line too
	for _, w := range sess.Warnings {
		log.Warn(w)
	}
	fail := func(code int, err error) int {
		ref := recordError(log, app.CodeInternal, fmt.Sprintf("gui: stopped with exit code %d", code), err)
		_, _ = fmt.Fprintf(stderr, "mothdex-gui: %v\n", err)
		support(stderr, sess.T, app.CodeInternal, ref, logs.LogPath())
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
	display, err := text.Parse(assets.Handjet)
	if err != nil {
		return fail(exitInternal, err)
	}
	tr, fallback, err := translator(sess.T)
	if err != nil {
		return fail(exitInternal, err)
	}

	warn := func(msg string) { log.Warn(msg) }
	mappings, mappingCounts := input.LoadMappingsCounted(assets.ControllerDB, filepath.Join(sess.DataDir, userMappingsFile), input.SDLPlatform(runtime.GOOS), os.Getenv, warn)
	icon, err := appIcon()
	if err != nil {
		log.Warn("gui: no window icon", "err", err) // cosmetic: the window opens with SDL's default icon
	}
	be, err := sdl.OpenBackend(lib, font, sdl.BackendOptions{
		Display: display, Title: "mothdex", Icon: icon, Driver: o.driver, Windowed: o.height > 0, Width: o.width, Height: o.height, Mappings: mappings, Log: log,
	})
	if err != nil {
		return fail(exitSDL, err)
	}
	defer be.Close()
	w, h := be.Size()
	log.Info("gui: diagnostics", diagnostics{
		SDLVersion: lib.Version().String(), SDLOrigin: lib.Origin(), Driver: be.Driver(), Lookup: os.LookupEnv, Mappings: mappingCounts, Pads: be.ControllerTypes(),
	}.attrs()...)
	log.Info("gui: started", "driver", be.Driver(), "width", w, "height", h, "version", buildinfo.Get().Version)

	updates := newUpdates(sess.Updater, sess.UpdateCheck)
	defer updates.wait() // runs before the closes above: an installation ends before the program does
	opts := gui.Options{
		Updates: updates.model(),
		Service: sess.Svc, T: tr, Font: font, Display: display, Clock: gui.SystemClock(), Log: log, DataDir: sess.DataDir,
		MappingsPath: filepath.Join(sess.DataDir, userMappingsFile),
		Version:      buildinfo.Get().Version, LocaleFallback: fallback, Width: w, Height: h, Safe: gui.AutoSafeArea,
		Prefer: sess.Prefer, Boxart: sess.Boxart, Confirm: sess.Confirm,
		LogPath:    func() string { return mlog.DisplayPath(logs.LogPath()) },
		ExportLogs: exportLogs(logs, sess.BundleOptions),
	}
	model, cmds := gui.New(opts, time.Now())

	// Read now, for the crash screen: after a panic the service is not called again, since a lock it holds may never
	// be released.
	settings, err := sess.Svc.ConfigList()
	if err != nil {
		log.Warn("gui: ConfigList", "err", err) // a crash screen would keep the default buttons and sizes
	}
	run := loop.Config{
		Model: model, Cmds: cmds, Backend: wrapBackend(be), Events: sess.Svc, Clock: time.Now, Signals: terminationSignals(), Log: log,
	}
	final, crash, err := runLoop(ctx, run)
	if crash != nil {
		ref := reportPanic(logs, crash.value, crash.stack)
		_, _ = fmt.Fprintln(stderr, "mothdex-gui: internal error")
		support(stderr, sess.T, app.CodeInternal, ref, logs.LogPath())
		showCrash(ctx, gui.NewCrash(opts, settings, ref, time.Now()), run)
		updates.wait()
		leaveCrashed(be.Close, sess.Close, logs, exitProcess)
		return exitInternal // only reached by a test: os.Exit does not return
	}
	if err != nil {
		return fail(sdlExitCode(err), err)
	}
	closeAll := func() {
		updates.wait()
		be.Close()
		_ = logs.Close()
	}
	if leaveStuck(final.ExitTimedOut(), log, closeAll, os.Exit) {
		return exitOK // only reached by a test: os.Exit does not return
	}
	return exitOK
}

// recoverInternal turns a panic into exit code 1, with a ref, the stack in the log line and a crash file: a
// Port has no visible terminal, so those are what a tester can send. The value is not printed: only the log is
// redacted. It must be deferred directly.
func recoverInternal(code *int, o options, logs *mlog.Session, stderr io.Writer) {
	r := recover()
	if r == nil {
		return
	}
	ref := reportPanic(logs, r, debug.Stack())
	_, _ = fmt.Fprintln(stderr, "mothdex-gui: internal error")
	if tr, err := cmdlineTranslator(o.config, o.lang); err == nil {
		support(stderr, tr, app.CodeInternal, ref, logs.LogPath())
	}
	*code = exitInternal
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

// configExitCode is 2 for a configuration or usage problem and 1 for anything else that stopped the start.
func configExitCode(err error) int {
	var ae *app.Error
	if errors.As(err, &ae) && ae.ExitCode() == exitConfig {
		return exitConfig
	}
	return exitInternal
}
