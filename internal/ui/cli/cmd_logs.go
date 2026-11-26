package cli

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	mlog "github.com/Or4cu1o/mothdex-cli/internal/log"
)

// maxTail bounds `logs last --tail` (TM-24).
const maxTail = 100_000

var errNoDisplay = errors.New("there is no graphical session to open a folder in")

// logsTopic is `mothdex help logs`.
func logsTopic(t *i18n.Translator) HelpTopic {
	return HelpTopic{Name: "logs", Short: "Where the logs are and how to send them", Text: func() string { return t.T("ui.logs.topic", nil) }}
}

// logsEnv is what every logs subcommand works on. None of them opens the service: they must work
// when the configuration or the server is what is broken.
type logsEnv struct {
	p       *printer
	options mlog.BundleOptions
	current string // this process's session id
}

// logs runs a logs subcommand: fn returns the --json data and the human rendering.
func (r *runtime) logs(fn func(e logsEnv) (any, func(p *printer), error)) error {
	r.g.Command = "logs"
	return r.local(func(p *printer) (any, func(p *printer), error) {
		p.t = r.helpTranslator() // interface.language without opening the service
		if r.run != nil {
			p.setColor(r.run.s.Color)
		}
		if r.d.Diagnostics == nil {
			return nil, nil, &app.Error{Code: app.CodeInternal, Message: "the logs are not available"}
		}
		options, err := r.d.Diagnostics(r.g)
		if options.Dir == "" {
			return nil, nil, &app.Error{Code: app.CodeInternal, Message: "cannot find the log folder", Cause: err}
		}
		if err != nil {
			p.warning(err.Error())
		}
		options.Manifest.Lang = p.t.Locale()
		e := logsEnv{p: p, options: options}
		if r.d.Logs != nil {
			e.current = r.d.Logs.ID()
		}
		return fn(e)
	})
}

func (r *runtime) logsCmd() *cobra.Command {
	c := &cobra.Command{Use: "logs", Short: "Find and send logs"}
	c.AddCommand(r.logsPathCmd(), r.logsLastCmd(), r.logsListCmd(), r.logsShowCmd(), r.logsOpenCmd(), r.logsBundleCmd(), r.logsClearCmd())
	return c
}

func (r *runtime) logsPathCmd() *cobra.Command {
	return &cobra.Command{
		Use: "path", Short: "Show where the logs are", Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return r.logs(func(e logsEnv) (any, func(*printer), error) {
				latest := filepath.Join(e.options.Dir, mlog.LatestName)
				session := ""
				if r.d.Logs != nil {
					session = r.d.Logs.SessionPath()
				}
				return map[string]any{"dir": e.options.Dir, "latest": latest, "session": session}, func(p *printer) {
					_, _ = fmt.Fprintln(p.out, clean(e.options.Dir))
					_, _ = fmt.Fprintln(p.out, clean(latest))
					if mlog.NoPermissions(e.options.Dir) {
						p.warning(p.T("ui.logs.no_permissions", nil))
					}
				}, nil
			})
		},
	}
}

func (r *runtime) logsLastCmd() *cobra.Command {
	var tail int
	c := &cobra.Command{
		Use: "last", Short: "Show the log of the previous session", Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return r.logs(func(e logsEnv) (any, func(*printer), error) {
				if tail < 0 || tail > maxTail {
					return nil, nil, usageErr("logs", "%s", e.p.T("ui.logs.bad_tail", i18n.Args{"max": maxTail}))
				}
				id, ok := mlog.Last(e.options.Dir, e.current)
				if !ok {
					return nil, nil, logsNotFound("there is no earlier session", "ui.logs.no_previous", nil)
				}
				lines, err := mlog.Lines(e.options.Dir, id, tail, e.options.Secrets...)
				if err != nil {
					return nil, nil, logsFailure(e, err)
				}
				return map[string]any{"session": id, "lines": lines}, func(p *printer) { printLines(p, lines) }, nil
			})
		},
	}
	c.Flags().IntVar(&tail, "tail", 0, "only the last lines (0 = all)")
	logValues(c.Flags(), "tail")
	return c
}

func (r *runtime) logsListCmd() *cobra.Command {
	return &cobra.Command{
		Use: "list", Short: "List the sessions", Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return r.logs(func(e logsEnv) (any, func(*printer), error) {
				sessions := mlog.List(e.options.Dir, e.current)
				return map[string]any{"sessions": sessions}, func(p *printer) { sessionsTable(p, sessions) }, nil
			})
		},
	}
}

func sessionsTable(p *printer, sessions []mlog.SessionSummary) {
	rows := make([][]string, len(sessions))
	for i, s := range sessions {
		started := s.StartedAt
		if at, err := time.Parse(time.RFC3339, s.StartedAt); err == nil {
			started = at.Local().Format("2006-01-02 15:04")
		}
		rows[i] = []string{s.ID, started, s.Binary, s.Mode, humanDuration(time.Duration(s.DurationMS) * time.Millisecond), strconv.Itoa(s.Errors)}
	}
	head := func(key string) string { return p.T("ui.logs.column."+key, nil) }
	p.table([]column{{head: head("session")}, {head: head("started")}, {head: head("binary"), drop: narrowWidth},
		{head: head("mode"), drop: narrowWidth}, {head: head("duration"), right: true, drop: narrowWidth}, {head: head("errors"), right: true}}, rows)
}

func (r *runtime) logsShowCmd() *cobra.Command {
	var ref string
	c := &cobra.Command{
		Use: "show [session]", Short: "Show a session or the lines around an error", Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return r.logs(func(e logsEnv) (any, func(*printer), error) {
				id := ""
				if len(args) == 1 {
					id = args[0]
				}
				if ref != "" {
					return showRef(e, id, ref)
				}
				if id == "" {
					return nil, nil, usageErr("logs", "%s", e.p.T("ui.logs.need_target", nil))
				}
				lines, err := mlog.Lines(e.options.Dir, id, 0, e.options.Secrets...)
				if err != nil {
					return nil, nil, logsFailure(e, err)
				}
				return map[string]any{"session": id, "lines": lines}, func(p *printer) { printLines(p, lines) }, nil
			})
		},
	}
	c.Flags().StringVar(&ref, "ref", "", "show the error with this ref")
	logValues(c.Flags(), "ref")
	return c
}

func showRef(e logsEnv, id, typed string) (any, func(*printer), error) {
	ref, err := mlog.ParseRef(typed)
	if err != nil {
		return nil, nil, usageErr("logs", "%s", e.p.T("ui.logs.bad_ref", nil))
	}
	matches, err := mlog.Find(e.options.Dir, id, ref, e.options.Secrets...)
	if err != nil {
		return nil, nil, logsFailure(e, err)
	}
	if len(matches) == 0 {
		return nil, nil, logsNotFound("no session has the ref "+ref, "ui.logs.no_ref", i18n.Args{"ref": ref})
	}
	return map[string]any{"session": matches[0].Session, "matches": matches}, func(p *printer) {
		for i, m := range matches {
			if i > 0 {
				_, _ = fmt.Fprintln(p.out)
			}
			p.info("%s", p.bold(p.T("ui.logs.match", i18n.Args{"session": m.Session, "line": m.Line})))
			printLines(p, m.Context)
		}
	}, nil
}

func printLines(p *printer, lines []string) {
	for _, line := range lines {
		_, _ = fmt.Fprintln(p.out, clean(line))
	}
}

// logsNotFound is a not_found of the logs: key is its message in the interface language.
func logsNotFound(message, key string, args i18n.Args) *app.Error {
	a := map[string]any{"hint_key": "ui.logs.list_hint", "message_key": key}
	for k, v := range args {
		a[k] = v
	}
	return &app.Error{Code: app.CodeNotFound, Message: message, Args: a}
}

// logsFailure maps the errors of internal/log to the codes of the command line.
func logsFailure(e logsEnv, err error) error {
	switch {
	case errors.Is(err, mlog.ErrSession):
		return usageErr("logs", "%s", e.p.T("ui.logs.bad_session", nil))
	case errors.Is(err, mlog.ErrOutDir):
		return usageErr("logs", "%s", e.p.T("ui.logs.bad_folder", nil))
	case errors.Is(err, mlog.ErrSessions):
		return usageErr("logs", "%s", e.p.T("ui.logs.bad_sessions", i18n.Args{"max": mlog.MaxBundleSessions}))
	case errors.Is(err, context.Canceled):
		return &app.Error{Code: app.CodeCanceled, Message: "canceled", Cause: err}
	}
	return &app.Error{Code: app.CodePermissionDenied, Message: "cannot use the log folder", Cause: err}
}

func (r *runtime) logsOpenCmd() *cobra.Command {
	return &cobra.Command{
		Use: "open", Short: "Open the log folder", Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return r.logs(func(e logsEnv) (any, func(*printer), error) {
				open := r.d.OpenFolder
				if open == nil {
					open = func(dir string) error { return openFolder(r.d.Getenv, dir) }
				}
				if err := open(e.options.Dir); err != nil {
					return nil, nil, usageErr("logs", "%s", e.p.T("ui.logs.cannot_open", i18n.Args{"path": clean(e.options.Dir)}))
				}
				return map[string]any{"dir": e.options.Dir}, func(p *printer) { p.info("%s", clean(e.options.Dir)) }, nil
			})
		},
	}
}

// openerFor is the file manager of an operating system and its arguments. The folder is always one
// argument of its own: no shell reads it (TM-28).
func openerFor(goos string, getenv func(string) string, dir string) (string, []string, error) {
	if !filepath.IsAbs(dir) {
		return "", nil, errors.New("the folder is not an absolute path")
	}
	switch goos {
	case "windows":
		return "explorer.exe", []string{dir}, nil
	case "darwin":
		return "open", []string{dir}, nil
	}
	if getenv == nil || getenv("DISPLAY") == "" && getenv("WAYLAND_DISPLAY") == "" {
		return "", nil, errNoDisplay
	}
	return "xdg-open", []string{dir}, nil
}

func openFolder(getenv func(string) string, dir string) error {
	name, args, err := openerFor(goruntime.GOOS, getenv, dir)
	if err != nil {
		return err
	}
	// not tied to the command: the file manager outlives it
	cmd := exec.CommandContext(context.Background(), name, args...) // #nosec G204 -- a fixed program with the log folder as its only argument
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }() // explorer.exe reports failure even when the window opens
	return nil
}

func (r *runtime) logsBundleCmd() *cobra.Command {
	var sessions int
	c := &cobra.Command{
		Use: "bundle", Short: "Make a zip to attach to an issue", Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return r.logs(func(e logsEnv) (any, func(*printer), error) {
				o := e.options
				if sessions < 1 || sessions > mlog.MaxBundleSessions {
					return nil, nil, logsFailure(e, mlog.ErrSessions)
				}
				o.Sessions = sessions
				if r.g.Output != "" {
					abs, err := filepath.Abs(r.g.Output)
					if err != nil {
						return nil, nil, logsFailure(e, mlog.ErrOutDir)
					}
					o.OutDir = abs
				}
				if r.run == nil {
					o.Skip = e.current // this run only bundles; inside the Shell it holds the errors
				}
				res, err := mlog.Bundle(r.ctx, o)
				if err != nil {
					return nil, nil, logsFailure(e, err)
				}
				return res, func(p *printer) {
					_, _ = fmt.Fprintln(p.out, clean(res.Path))
					p.info("%s", p.T("ui.logs.bundle_next", i18n.Args{"size": humanSize(res.Bytes)}))
				}, nil
			})
		},
	}
	c.Flags().StringVarP(&r.g.Output, "output", "o", "", "destination folder")
	c.Flags().IntVar(&sessions, "sessions", mlog.DefaultBundleSessions, "newest sessions to include (1..20)")
	logValues(c.Flags(), "sessions")
	return c
}

func (r *runtime) logsClearCmd() *cobra.Command {
	return &cobra.Command{
		Use: "clear", Short: "Delete sessions, crash files and bundles", Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return r.logs(func(e logsEnv) (any, func(*printer), error) {
				if !r.g.Force {
					if r.g.JSON || r.g.Quiet || !r.canPrompt() {
						return nil, nil, &app.Error{Code: app.CodeConfirmationNeeded, Message: "this command needs confirmation"}
					}
					answer, _ := r.readAnswer(clean(e.p.T("ui.logs.clear_ask", i18n.Args{"path": e.options.Dir})) + " ") // no answer is no
					if !e.p.isYes(strings.TrimSpace(answer)) {
						return nil, nil, &app.Error{Code: app.CodeCanceled, Message: "canceled"}
					}
				}
				removed, err := mlog.Clear(e.options.Dir, e.current)
				if err != nil {
					return nil, nil, logsFailure(e, err)
				}
				return map[string]any{"removed": removed}, func(p *printer) {
					p.info("%s", p.T("ui.logs.cleared", i18n.Args{"sessions": removed.Sessions, "crashes": removed.Crashes, "bundles": removed.Bundles}))
				}, nil
			})
		},
	}
}
