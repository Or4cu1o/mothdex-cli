// Package shell is the interactive REPL: a thin layer over the CLI command tree (through
// cli.Runner) with a line editor, history, Tab completion and the paging shortcuts of
// docs/ux/shell.md. It holds no business rules; every command runs as the CLI would run it.
package shell

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/buildinfo"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/cli"
	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

const (
	// networkWait bounds the calls made for the banner and for Tab, so a dead network never freezes typing.
	networkWait = 2 * time.Second
	// retryAfter is how long a failed Tab source waits before it is asked again.
	retryAfter = 30 * time.Second

	exitUsage       = 2
	exitInterrupted = 130
	clearScreen     = "\x1b[H\x1b[2J"
)

// runner is the part of cli.Runner the Shell uses.
type runner interface {
	Run(ctx context.Context, args []string) int
	RunSearch(ctx context.Context, q app.SearchQuery) int
	LastPage() (app.SearchPage, bool)
	LastEntry() (mothdex.Entry, bool)
	Commands() []cli.CommandInfo
	SetPrompt(fn func(label string) (string, error))
}

// service is the part of the app service the Shell uses itself (the banner and Tab).
type service interface {
	DBInfo(ctx context.Context) (app.Result[mothdex.DBInfo], error)
	Platforms(ctx context.Context, f app.PlatformFilter) (app.Result[[]mothdex.Platform], error)
	Regions(ctx context.Context) (app.Result[[]mothdex.Region], error)
	ConfigList() ([]app.Setting, error)
	CacheStats() (app.CacheStats, error)
	ActiveProfile() app.ProfileInfo
}

// Options is everything a Shell needs; tests build one with fakes.
type Options struct {
	UpdateNotice string
	Runner       runner
	Service      service
	Editor       lineEditor
	Out, Err     io.Writer
	T            *i18n.Translator
	Now          func() time.Time
	Interrupts   func() (<-chan os.Signal, func())
	DataDir      string
	Log          *slog.Logger
	Interactive  bool   // a person at a terminal: banner, history, prompts
	Color        bool   // styles allowed
	Version      string // for the banner
	Clear        string // the sequence that clears the screen; empty when the terminal cannot
	Paths        func(prefix string) []string
}

// Run is the launcher the composition root injects as cli.Deps.Shell.
func Run(ctx context.Context, env cli.ShellEnv) int {
	var ed lineEditor
	if env.IsTerminal && linerSupported() {
		ed = newLinerEditor()
	} else {
		ed = newLineReader(env.Stdin, env.Stdout, env.IsTerminal)
	}
	defer func() { _ = ed.Close() }()
	s := env.Session
	version := buildinfo.Get().Version
	if version == "" { // a plain `go build` has no version (buildinfo prints "dev")
		version = "dev"
	}
	opts := Options{
		UpdateNotice: env.UpdateNotice, Runner: env.Runner, Service: s.Svc, Editor: ed, Out: env.Stdout, Err: env.Stderr, T: s.T, Now: time.Now,
		Interrupts: env.Interrupts, DataDir: s.DataDir, Log: s.Log, Interactive: env.IsTerminal,
		Color: env.IsTerminal && !s.NoColor, Version: version, Paths: completePaths,
	}
	if os.Getenv("TERM") != "dumb" {
		opts.Clear = clearScreen
	}
	return New(opts).Loop(ctx)
}

// Shell is one interactive session.
type Shell struct {
	o        Options
	hist     historyStore
	lines    []string
	sig      <-chan os.Signal
	lastCode int
	numbers  *message.Printer
}

// New builds a Shell from its options.
func New(o Options) *Shell {
	if o.Now == nil {
		o.Now = time.Now
	}
	tag := language.English
	if o.T != nil {
		tag = language.Make(strings.ReplaceAll(o.T.Locale(), "_", "-"))
	}
	return &Shell{o: o, hist: newHistoryStore(o.DataDir, o.Log), numbers: message.NewPrinter(tag)}
}

// Loop reads and runs lines until the user leaves, and returns the exit code (ui.md section 2.5).
func (s *Shell) Loop(ctx context.Context) int {
	// Only a person at a terminal takes SIGINT over. A piped script keeps the default: the first
	// SIGINT cancels the root context, and the loop below ends instead of running the next line.
	if s.o.Interactive && s.o.Interrupts != nil {
		sig, release := s.o.Interrupts()
		s.sig = sig
		defer release()
	}
	s.o.Runner.SetPrompt(s.ask)
	if s.o.Interactive {
		s.lines = s.hist.load()
		for _, l := range s.lines {
			s.o.Editor.AppendHistory(l)
		}
		defer func() { s.hist.save(s.lines) }()
		s.banner(ctx)
	}
	s.o.Editor.SetCompleter(s.completer(ctx))
	for {
		if ctx.Err() != nil {
			return exitInterrupted
		}
		line, err := s.readLine(ctx)
		switch {
		case err == nil:
		case errors.Is(err, errAborted): // Ctrl+C at the prompt clears the line and says how to leave
			s.say("ui.shell.ctrlc_hint", nil)
			continue
		case errors.Is(err, io.EOF):
			if s.o.Interactive {
				return 0
			}
			return s.lastCode
		case ctx.Err() != nil:
			return exitInterrupted
		default: // the editor failed: say so, never end silently
			s.logWarn("cannot read a line", err)
			_, _ = fmt.Fprintln(s.o.Err, s.bad(s.t("ui.messages.error", nil)+":"), s.t("errors.internal.message", nil))
			return 1
		}
		quit, code := s.handle(ctx, line)
		if quit {
			return code
		}
		s.lastCode = code
	}
}

// readLine prompts in the background so that SIGTERM (a canceled context) ends the Shell even
// while it waits for a key. When that happens the abandoned Prompt still holds the line editor while
// the deferred Close restores the terminal; this only happens as the process ends (review PR-53 s4,
// backlog).
func (s *Shell) readLine(ctx context.Context) (string, error) {
	page, ok := s.o.Runner.LastPage()
	label := promptText(page, ok)
	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		l, e := s.o.Editor.Prompt(label)
		ch <- result{l, e}
	}()
	select {
	case r := <-ch:
		return r.line, r.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// ask is the prompt of the commands (link chooser, confirmation): it goes through the editor.
func (s *Shell) ask(label string) (string, error) { return s.o.Editor.Prompt(label) }

// handle runs one line and says whether the Shell should end.
func (s *Shell) handle(ctx context.Context, line string) (quit bool, code int) {
	words, err := splitLine(line)
	if err != nil {
		return false, s.fail(err)
	}
	if len(words) == 0 {
		return false, s.lastCode
	}
	if s.o.Interactive && recordable(line) && !repeats(s.lines, line) {
		s.lines = record(s.lines, line)
		s.o.Editor.AppendHistory(line)
	}
	name, args := words[0], words[1:]
	switch {
	case name == "exit" || name == "quit":
		return true, 0
	case name == "clear":
		s.clear()
		return false, 0
	case name == "help":
		return false, s.help(ctx, args)
	case name == "last":
		return false, s.last(ctx)
	case name == "n":
		return false, s.step(ctx, +1)
	case name == "p":
		return false, s.step(ctx, -1)
	case name == "page":
		return false, s.page(ctx, args)
	case name == "sort":
		return false, s.sort(ctx, args)
	case isNumber(name): // a bare number is info
		return false, s.command(ctx, append([]string{"info", name}, args...))
	case name == "d":
		return false, s.command(ctx, append([]string{"download"}, args...))
	}
	if _, ok := findCommand(s.o.Runner.Commands(), name); !ok {
		return false, s.unknown(name)
	}
	return false, s.command(ctx, words)
}

// command runs a CLI command line, expanding index ranges for the commands that take entries.
func (s *Shell) command(ctx context.Context, words []string) int {
	cmd, ok := findCommand(s.o.Runner.Commands(), words[0])
	if !ok || !indexed(words[0]) {
		return s.exec(ctx, func(c context.Context) int { return s.o.Runner.Run(c, words) })
	}
	takesValue := func(flag string) bool {
		f, found := findFlag(cmd, flag)
		return found && f.TakesValue
	}
	expanded, err := expandIndexes(words[1:], takesValue)
	if err != nil {
		return s.fail(err)
	}
	if !perEntry(words[0]) {
		words = append([]string{words[0]}, expanded...)
		return s.exec(ctx, func(c context.Context) int { return s.o.Runner.Run(c, words) })
	}
	// info and boxart take one entry: a list or a range runs the command once for each.
	flags, entries := splitEntries(expanded, takesValue)
	if len(entries) <= 1 {
		words = append([]string{words[0]}, expanded...)
		return s.exec(ctx, func(c context.Context) int { return s.o.Runner.Run(c, words) })
	}
	return s.exec(ctx, func(c context.Context) int {
		code := 0
		for _, entry := range entries {
			if c.Err() != nil { // Ctrl+C: the rest of the list is dropped
				break
			}
			one := append(append([]string{words[0]}, flags...), entry)
			if rc := s.o.Runner.Run(c, one); rc != 0 && code == 0 {
				code = rc // the first failure is the exit code, and the rest still run
			}
		}
		if code == 0 && c.Err() != nil { // Ctrl+C between two entries: the rest was dropped, so it did not succeed
			code = exitInterrupted
		}
		return code
	})
}

func indexed(name string) bool { return name == "info" || name == "download" || name == "boxart" }

// perEntry says whether a command takes one entry at a time: download takes a list, info and boxart do not.
func perEntry(name string) bool { return name == "info" || name == "boxart" }

// exec runs fn with a context that Ctrl+C cancels: the command stops and the Shell stays
// (shell.md section 4). A signal that arrived while nobody was running is dropped first.
func (s *Shell) exec(ctx context.Context, fn func(context.Context) int) int {
	for drained := false; !drained; {
		select {
		case <-s.sig:
		default:
			drained = true
		}
	}
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-s.sig:
			cancel()
		case <-done:
		}
	}()
	return fn(cctx)
}

func (s *Shell) search(ctx context.Context, q app.SearchQuery) int {
	return s.exec(ctx, func(c context.Context) int { return s.o.Runner.RunSearch(c, q) })
}

// last prints the last search page again.
func (s *Shell) last(ctx context.Context) int {
	page, ok := s.o.Runner.LastPage()
	if !ok {
		return s.noPage()
	}
	q := page.Query
	q.Page = page.Page
	return s.search(ctx, q)
}

// step goes to the next (+1) or previous (-1) page of the last search.
func (s *Shell) step(ctx context.Context, dir int) int {
	page, ok := s.o.Runner.LastPage()
	if !ok {
		return s.noPage()
	}
	switch {
	case dir > 0 && page.Page >= page.TotalPages:
		s.say("ui.shell.last_page", nil)
		return 0
	case dir < 0 && page.Page <= 1:
		s.say("ui.shell.first_page", nil)
		return 0
	}
	q := page.Query
	q.Page = page.Page + dir
	return s.search(ctx, q)
}

// page jumps to a page of the last search.
func (s *Shell) page(ctx context.Context, args []string) int {
	page, ok := s.o.Runner.LastPage()
	if !ok {
		return s.noPage()
	}
	n, err := strconv.Atoi(strings.Join(args, ""))
	if len(args) != 1 || err != nil || n < 1 {
		return s.fail(&keyError{key: "ui.shell.bad_page"})
	}
	q := page.Query
	q.Page = n
	return s.search(ctx, q)
}

// sort re-sorts the current page (the service sorts one page only).
func (s *Shell) sort(ctx context.Context, args []string) int {
	page, ok := s.o.Runner.LastPage()
	if !ok {
		return s.noPage()
	}
	if len(args) != 1 || !contains(sortKeys, args[0]) {
		return s.fail(&keyError{key: "ui.shell.bad_sort"})
	}
	q := page.Query
	q.Page, q.Sort = page.Page, app.SortKey(args[0])
	return s.search(ctx, q)
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func (s *Shell) noPage() int {
	s.say("ui.shell.no_page", nil)
	return app.ExitCodeFor(app.CodeNoSession)
}

func (s *Shell) clear() {
	if s.o.Interactive && s.o.Clear != "" {
		_, _ = fmt.Fprint(s.o.Out, s.o.Clear)
	}
}

// unknown reports a command that does not exist, with the nearest name when there is one.
func (s *Shell) unknown(name string) int {
	names := append(commandNames(s.o.Runner.Commands()), metaCommands...)
	msg := s.t("ui.shell.unknown_command", i18n.Args{"name": name})
	if near, ok := closest(name, names); ok {
		msg += " " + s.t("ui.shell.did_you_mean", i18n.Args{"suggestion": near})
	}
	_, _ = fmt.Fprintln(s.o.Err, app.SanitizeText(msg))
	_, _ = fmt.Fprintln(s.o.Err, s.t("ui.shell.type_help", nil))
	return exitUsage
}

// fail prints a usage mistake of the line and returns the usage exit code.
func (s *Shell) fail(err error) int {
	var ke *keyError
	msg := err.Error()
	if errors.As(err, &ke) {
		msg = s.t(ke.key, ke.args)
	}
	_, _ = fmt.Fprintln(s.o.Err, s.bad(s.t("ui.messages.error", nil)+":"), app.SanitizeText(msg))
	_, _ = fmt.Fprintln(s.o.Err, " ", s.t("ui.shell.type_help", nil))
	return exitUsage
}

// say prints a one-line note on stderr, dim.
func (s *Shell) say(key string, args i18n.Args) {
	_, _ = fmt.Fprintln(s.o.Err, s.dim(s.t(key, args)))
}

func (s *Shell) t(key string, args i18n.Args) string { return s.o.T.T(key, args) }

func (s *Shell) logWarn(msg string, err error) {
	if s.o.Log != nil {
		s.o.Log.Warn(msg, "err", err)
	}
}

func (s *Shell) style(code, text string) string {
	if !s.o.Color || text == "" {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

func (s *Shell) dim(text string) string { return s.style("2", text) }
func (s *Shell) bad(text string) string { return s.style("1;31", text) }
