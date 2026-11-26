package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	"github.com/spf13/pflag"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	mlog "github.com/Or4cu1o/mothdex-cli/internal/log"
)

// Reporter gives every error a person sees its ref and writes the log line behind it, for the
// command line, the Shell and the TUI (logs.md section 5). Asking again about the same error
// value returns the same ref, so a screen may redraw it.
type Reporter struct {
	Logs *mlog.Session // nil: refs are given, nothing is written
	// Attach opens the log files of a session that has none yet (an error before the service).
	Attach func()
	refs   mlog.Refs
}

// Ref returns the ref of e, recording it on the first call. extra goes on the same log line.
func (rp *Reporter) Ref(cmd string, e *app.Error, extra ...slog.Attr) string {
	var logger *slog.Logger
	if rp.Logs != nil {
		if !rp.Logs.Attached() && rp.Attach != nil {
			rp.Attach()
		}
		logger = rp.Logs.Logger()
	}
	return rp.refs.Of(logger, e, mlog.ErrorRecord{Code: e.Code, Cmd: cmd, Message: englishMessage(e), Cause: e.Cause, Attrs: extra})
}

// LogPath is the log to name on screen, empty when nothing is written.
func (rp *Reporter) LogPath() string {
	if rp.Logs == nil {
		return ""
	}
	if path := rp.Logs.LogPath(); path != "" {
		return clean(mlog.DisplayPath(path))
	}
	return ""
}

// CodeLine is "code: network · ref: 7F3K2Q" in the interface language.
func CodeLine(t *i18n.Translator, utf8 bool, code, ref string) string {
	sep := " - "
	if utf8 {
		sep = " · "
	}
	return t.T("ui.logs.code", nil) + ": " + code + sep + "ref: " + ref
}

// LogLine is "log: <path>" in the interface language.
func LogLine(t *i18n.Translator, path string) string { return t.T("ui.logs.log", nil) + ": " + path }

func newReporter(d Deps, g *Globals) *Reporter {
	rp := &Reporter{Logs: d.Logs}
	if d.Diagnostics != nil {
		// a configuration that does not load is the error being reported; the defaults are used
		rp.Attach = func() { _, _ = d.Diagnostics(*g) }
	}
	return rp
}

// ref is the ref of an error this printer shows.
func (p *printer) ref(e *app.Error) string {
	cmd := p.g.Command
	if c, ok := e.Args["command"].(string); cmd == "" && ok {
		cmd = c
	}
	return p.rep.Ref(cmd, e)
}

// printLogLine says where the details of the errors above are.
func (p *printer) printLogLine() {
	if path := p.rep.LogPath(); path != "" {
		_, _ = fmt.Fprintln(p.err, "  "+p.dim(LogLine(p.t, path)))
	}
}

// Report records a mistake the Shell found in a line, and returns the lines to print under it.
func (r *Runner) Report(code, message string) []string {
	rp := newReporter(r.d, &Globals{Command: "shell"})
	ref := rp.Ref("shell", &app.Error{Code: code, Message: message})
	lines := []string{CodeLine(r.s.T, IsUTF8(r.d.Getenv), code, ref)}
	if path := rp.LogPath(); path != "" {
		lines = append(lines, LogLine(r.s.T, path))
	}
	return lines
}

// panicGlobals reads again the global flags that decide where the log goes and how the error is
// printed: the parsed ones are lost with the panic.
func panicGlobals(args []string) Globals {
	var g Globals
	flags := pflag.NewFlagSet("panic", pflag.ContinueOnError)
	flags.ParseErrorsAllowlist.UnknownFlags = true
	flags.SetOutput(io.Discard)
	flags.StringVar(&g.Config, "config", "", "")
	flags.StringVar(&g.Profile, "profile", "", "")
	flags.StringVar(&g.Lang, "lang", "", "")
	flags.BoolVar(&g.JSON, "json", false, "")
	_ = flags.Parse(args) // what was read before a bad flag still counts
	return g
}

// ReportPanic is the recover of main: the panic goes to the log and to a crash file, and the
// person sees an internal error with its ref. It returns the exit code.
func ReportPanic(d Deps, args []string, recovered any, stack []byte) int {
	g := panicGlobals(args)
	var t *i18n.Translator
	if d.Translator != nil {
		t = d.Translator(g.Lang)
	}
	p := newPrinter(d, &g, t)
	e := &app.Error{Code: app.CodeInternal, Message: "panic", Cause: fmt.Errorf("%v", recovered)}
	ref := p.rep.Ref(g.Command, e, slog.String("stack", string(stack)))
	var crash string
	if d.Logs != nil {
		crash, _ = d.Logs.WriteCrash(recovered, stack, ref) // the log line above already holds the stack
	}
	if g.JSON {
		_ = json.NewEncoder(d.Stdout).Encode(p.envelope("", nil, e))
	}
	if t != nil {
		p.renderError(e)
		if crash != "" {
			_, _ = fmt.Fprintln(p.err, "  "+p.dim(clean(mlog.DisplayPath(crash))))
		}
	}
	return app.ExitCodeFor(app.CodeInternal)
}
