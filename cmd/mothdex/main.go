// Command mothdex is the Mothdex command-line client.
package main

import (
	"context"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"syscall"
	"time"

	// Fallback CA roots for systems without a CA bundle (CR-12); verification is never disabled.
	_ "golang.org/x/crypto/x509roots/fallback"
	"golang.org/x/term"

	"github.com/Or4cu1o/mothdex-cli/internal/buildinfo"
	"github.com/Or4cu1o/mothdex-cli/internal/compose"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	mlog "github.com/Or4cu1o/mothdex-cli/internal/log"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/cli"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/shell"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/tui"
	"github.com/Or4cu1o/mothdex-cli/internal/update"
	"github.com/Or4cu1o/mothdex-cli/locales"
)

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) (code int) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigs)
	router := newSignalRouter(cancel, os.Exit, time.Now)
	go func() {
		for sig := range sigs {
			router.handle(sig)
		}
	}()
	// The session starts before cobra and compose, so that their errors are logged too.
	rules := cli.LogFlagRules()
	logs := mlog.StartSession(mlog.SessionOptions{Binary: "cli", Args: args, Flags: rules, Build: buildinfo.Get(),
		GeneralOnly: !cli.Logged(args, rules)})
	logs.Update(mlog.Info{Mode: "command"})
	defer func() { _ = logs.Close() }() // nothing is left to tell it to
	d := deps(stdin, stdout, stderr, logs)
	d.Interrupts = router.interrupts
	// Deferred after the session's Close, so it runs while the log is still open.
	defer func() {
		if v := recover(); v != nil {
			code = cli.ReportPanic(d, args, v, debug.Stack())
		}
	}()
	return cli.Execute(ctx, d, args)
}

func deps(stdin io.Reader, stdout, stderr io.Writer, logs *mlog.Session) cli.Deps {
	return cli.Deps{
		Open:       opener(logs),
		CleanupOld: func() { cleanupOld(logs) },
		Translator: embeddedTranslator,
		Version:    buildinfo.Get,
		Logs:       logs,
		Diagnostics: func(g cli.Globals) (mlog.BundleOptions, error) {
			return compose.Diagnostics(logs, g.Config, g.Profile, g.Lang)
		},
		Stdin: stdin, Stdout: stdout, Stderr: stderr,
		IsTerminal: isTerminal,
		Width:      terminalWidth,
		Getenv:     os.Getenv,
		Shell:      shell.Run,
		TUI:        tui.Run,
	}
}

// embeddedTranslator serves errors that happen before the service opens (a bad flag, a broken
// configuration file): only the built-in locales, no file system access.
func embeddedTranslator(lang string) *i18n.Translator {
	b, err := i18n.Load(locales.FS)
	if err != nil {
		return nil
	}
	return b.New(b.Resolve(lang, os.Getenv, i18n.SystemLanguages))
}

// isTerminal is a real terminal check (isatty / GetConsoleMode). Checking ModeCharDevice alone is
// wrong: /dev/null and NUL are character devices too, so a piped-from-null stdin looked interactive.
func isTerminal(v any) bool {
	f, ok := v.(*os.File)
	return ok && term.IsTerminal(int(f.Fd())) //nolint:gosec // a file descriptor fits an int
}

// terminalWidth reads COLUMNS; without it the width is unknown and the caller uses 80.
func terminalWidth() int {
	n, err := strconv.Atoi(os.Getenv("COLUMNS"))
	if err != nil || n < 1 {
		return 0
	}
	return n
}

func cleanupOld(logs *mlog.Session) {
	if exe, err := os.Executable(); err == nil {
		if exe, err = filepath.EvalSymlinks(exe); err == nil {
			update.CleanupOld(filepath.Dir(exe), logs.Logger())
		}
	}
}
