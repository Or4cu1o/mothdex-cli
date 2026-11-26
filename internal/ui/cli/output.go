package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/width"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/buildinfo"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	"github.com/Or4cu1o/mothdex-cli/internal/update"
)

const (
	schemaVersion = 1
	minWidth      = 60
	narrowWidth   = 80
	defaultWidth  = 80
)

// printer renders everything a command prints. Human text always goes through app.SanitizeText
// (CR-14); JSON keeps the raw values, escaped by the encoder.
type printer struct {
	d       Deps
	g       *Globals
	t       *i18n.Translator
	out     io.Writer
	err     io.Writer
	color   bool
	tty     bool // stdout is a terminal
	width   int
	meta    *app.ResultMeta
	logPath string // shown by errors.internal without a session log
	rep     *Reporter
	warns   []string
}

func newPrinter(d Deps, g *Globals, t *i18n.Translator) *printer {
	tty := d.IsTerminal != nil && d.IsTerminal(d.Stdout)
	errTTY := d.IsTerminal != nil && d.IsTerminal(d.Stderr)
	p := &printer{d: d, g: g, t: t, out: d.Stdout, err: d.Stderr, tty: tty, width: defaultWidth, rep: newReporter(d, g)}
	if tty && d.Width != nil {
		if w := d.Width(); w > 0 {
			p.width = w
		}
	}
	env := func(k string) string {
		if d.Getenv == nil {
			return ""
		}
		return d.Getenv(k)
	}
	p.color = !g.NoColor && !g.JSON && env("NO_COLOR") == "" && env("TERM") != "dumb" && (tty || errTTY)
	return p
}

// setColor applies interface.color: never, always, or auto (a terminal, NO_COLOR unset, TERM not
// dumb). --no-color and --json win over everything.
func (p *printer) setColor(mode string) {
	if p.g.NoColor || p.g.JSON || mode == "never" {
		p.color = false
		return
	}
	if mode == "always" {
		p.color = true
	}
}

// IsUTF8 reports whether the locale is UTF-8 and the terminal can show the glyphs (not TERM=linux).
func IsUTF8(getenv func(string) string) bool {
	if getenv == nil || getenv("TERM") == "linux" {
		return false
	}
	for _, k := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := strings.ToLower(getenv(k)); v != "" {
			return strings.Contains(v, "utf-8") || strings.Contains(v, "utf8")
		}
	}
	return false
}

// sym is a status symbol with its ASCII fallback when the locale is not UTF-8 or TERM=linux.
func (p *printer) sym(name string) string {
	utf8 := IsUTF8(p.d.Getenv)
	switch {
	case name == "ok" && utf8:
		return "\u2714"
	case name == "ok":
		return "[ok]"
	case name == "fail" && utf8:
		return "\u2718"
	case name == "warn":
		return "!"
	}
	return "[x]"
}

func (p *printer) ok(s string) string { return p.style("32", s) }

// N translates a plural key.
func (p *printer) N(key string, n int, args i18n.Args) string { return p.t.N(key, n, args) }

// style wraps s in an SGR sequence when color is on.
func (p *printer) style(code, s string) string {
	if !p.color || s == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func (p *printer) bold(s string) string   { return p.style("1", s) }
func (p *printer) dim(s string) string    { return p.style("2", s) }
func (p *printer) accent(s string) string { return p.style("36", s) }
func (p *printer) warn(s string) string   { return p.style("33", s) }
func (p *printer) bad(s string) string    { return p.style("1;31", s) }

// info writes to stderr unless -q or --json (footers, hints, progress).
func (p *printer) info(format string, a ...any) {
	if p.g.Quiet || p.g.JSON {
		return
	}
	_, _ = fmt.Fprintf(p.err, format+"\n", a...)
}

// warning records a warning for the --json envelope and writes it to stderr (also with --json;
// -q hides it).
func (p *printer) warning(s string) {
	s = app.SanitizeText(s)
	p.warns = append(p.warns, s)
	if p.g.Quiet {
		return
	}
	_, _ = fmt.Fprintln(p.err, p.warn(p.T("ui.cli.warning", nil)+": "+s))
}

// T translates a key with arguments.
func (p *printer) T(key string, args i18n.Args) string { return p.t.T(key, args) }

// envelope is the --json document (cli-output.md section 2).
type envelope struct {
	SchemaVersion int        `json:"schema_version"`
	Command       string     `json:"command"`
	OK            bool       `json:"ok"`
	Data          any        `json:"data"`
	Error         *errorBody `json:"error"`
	Meta          meta       `json:"meta"`
	Warnings      []string   `json:"warnings"`
}

type errorBody struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Retryable bool           `json:"retryable"`
	ExitCode  int            `json:"exit_code"`
	Ref       string         `json:"ref"`
	Details   map[string]any `json:"details"`
}

type meta struct {
	Cached     bool   `json:"cached"`
	Stale      bool   `json:"stale"`
	FetchedAt  string `json:"fetched_at"`
	CLIVersion string `json:"cli_version"`
}

func (p *printer) envelope(command string, data any, err error) envelope {
	e := envelope{SchemaVersion: schemaVersion, Command: command, OK: err == nil, Data: data, Warnings: append([]string{}, p.warns...)}
	v := buildinfo.Info{}
	if p.d.Version != nil {
		v = p.d.Version()
	}
	e.Meta = meta{CLIVersion: v.Version, FetchedAt: time.Now().UTC().Format(time.RFC3339)}
	if p.meta != nil {
		e.Meta.Cached, e.Meta.Stale = p.meta.Cached, p.meta.Stale
		if !p.meta.FetchedAt.IsZero() {
			e.Meta.FetchedAt = p.meta.FetchedAt.UTC().Format(time.RFC3339)
		}
	}
	if err != nil {
		ae := asAppError(err)
		code := ae.ExitCode()
		var ee *exitError
		if errors.As(err, &ee) {
			code = ee.code // the process exit code, not the one of the inner error
		}
		e.Error = &errorBody{Code: ae.Code, Message: englishMessage(ae), Retryable: ae.Retryable, ExitCode: code, Ref: p.ref(ae), Details: errorDetails(ae)}
	}
	return e
}

// writeJSON prints the envelope as one document on stdout.
func (p *printer) writeJSON(command string, data any, err error) error {
	enc := json.NewEncoder(app.EscapeC1(p.out))
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(p.envelope(command, data, err))
}

// englishMessage is the stable English message of an error: the code text plus the service message,
// cleaned of terminal escapes (the JSON encoder escapes the rest).
func englishMessage(e *app.Error) string {
	if e.Message != "" {
		return app.SanitizeText(e.Message)
	}
	return e.Code
}

func asAppError(err error) *app.Error {
	var ae *app.Error
	if errors.As(err, &ae) {
		return ae
	}
	return &app.Error{Code: app.CodeInternal, Message: "unexpected error", Cause: err}
}

// clean is the text a human may see: API strings with Cc and Cf runes removed.
func clean(s string) string { return app.SanitizeText(s) }

// renderError prints the error block of cli-conventions.md section 7 on stderr. The text comes
// from the i18n keys errors.<code>.message and .hint; the code is always shown.
func (p *printer) renderError(err error) {
	ae := asAppError(err)
	ref := p.ref(ae) // first: it opens the log whose path is shown
	logPath := p.rep.LogPath()
	args := errorArgs(ae)
	if path := firstNonEmpty(logPath, p.logPath); path != "" {
		args["log_path"] = path
	}
	_, _ = fmt.Fprintf(p.err, "%s %s\n", p.bad(p.T("ui.messages.error", nil)+":"), clean(p.localizedMessage(ae, args)))
	hintKey := "errors." + ae.Code + ".hint"
	if k, ok := ae.Args["hint_key"].(string); ok {
		hintKey = k
	}
	if hintKey == "errors.confirmation_required.size_hint" {
		hintKey += "_cli" // only the command line and the Shell have -f; other UIs use the neutral text
	}
	if ae.Code == app.CodeUsage && ae.Args["command"] == "" {
		hintKey = "ui.cli.usage_root_hint"
	}
	if p.t.Has(hintKey) {
		if hint := p.t.T(hintKey, args); hint != "" && !unfilled(hint) {
			_, _ = fmt.Fprintf(p.err, "  %s\n", clean(hint))
		}
	}
	_, _ = fmt.Fprintf(p.err, "  %s\n", p.dim(CodeLine(p.t, IsUTF8(p.d.Getenv), ae.Code, ref)))
	p.printLogLine()
	if p.g.Verbose && ae.Cause != nil {
		_, _ = fmt.Fprintf(p.err, "  %s\n", p.dim("cause: "+clean(ae.Cause.Error())))
	}
}

// localizedMessage is the message of an error in the interface language. A placeholder with no
// value falls back to the service's English text, so a raw {name} is never shown.
func (p *printer) localizedMessage(ae *app.Error, args i18n.Args) string {
	if args == nil {
		args = errorArgs(ae)
	}
	key := "errors." + ae.Code + ".message"
	if k, ok := ae.Args["message_key"].(string); ok {
		key = k // a message more precise than the one of the code
	}
	if !p.t.Has(key) {
		return englishMessage(ae)
	}
	msg := p.t.T(key, args)
	if unfilled(msg) {
		return englishMessage(ae)
	}
	return msg
}

// unfilled reports a {name} placeholder left in translated text.
func unfilled(s string) bool { return placeholder.MatchString(s) }

var placeholder = regexp.MustCompile(`\{[a-z_]+\}`)

// errorArgs are the named placeholders of the error messages; values with no source stay as
// written in the text, which is visible rather than silently wrong.
func errorArgs(e *app.Error) i18n.Args {
	a := i18n.Args{"detail": e.Message}
	if e.Code == app.CodeUpdateUnavailable || e.Code == app.CodeUpdateFailed {
		a["install_command"] = update.InstallCommand()
	}
	for k, v := range e.Args {
		a[k] = v
	}
	for _, k := range []string{"got", "expected", "needed", "size", "estimate"} { // byte counts read as sizes
		if n, ok := byteCount(e.Args[k]); ok {
			a[k] = humanSize(n)
		}
	}
	return a
}

func byteCount(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int:
		return int64(n), true
	}
	return 0, false
}

// ---- tables ----

type column struct {
	head  string
	right bool
	drop  int // dropped below this width (0 = never)
	title bool
	blank bool // an empty cell stays empty instead of showing "-"
}

// cells is the display width of s in terminal cells: wide East Asian runes count 2, combining
// marks 0 (kept in step with internal/history).
func cells(s string) int {
	n := 0
	for _, r := range s {
		n += runeCells(r)
	}
	return n
}

func runeCells(r rune) int {
	if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) {
		return 0
	}
	if k := width.LookupRune(r).Kind(); k == width.EastAsianWide || k == width.EastAsianFullwidth {
		return 2
	}
	return 1
}

func cut(s string, n int) string {
	if cells(s) <= n {
		return s
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		if w := runeCells(r); used+w <= n-3 {
			b.WriteRune(r)
			used += w
		} else {
			break
		}
	}
	return b.String() + "..."
}

func padTo(s string, n int, right bool) string {
	gap := n - cells(s)
	if gap <= 0 {
		return s
	}
	if right {
		return strings.Repeat(" ", gap) + s
	}
	return s + strings.Repeat(" ", gap)
}

// table writes aligned columns separated by two spaces. On a terminal only the Title column shrinks
// and columns drop below 80 and 60 columns; piped output is never truncated. Empty cells show "-".
func (p *printer) table(cols []column, rows [][]string) { p.renderTable(cols, rows, true) }

// renderTable is table with the header row optional (the link chooser has none).
func (p *printer) renderTable(cols []column, rows [][]string, header bool) {
	keep := make([]int, 0, len(cols))
	for i, c := range cols {
		if p.tty && c.drop > 0 && p.width < c.drop {
			continue
		}
		keep = append(keep, i)
	}
	clamp := func(row []string) []string {
		out := make([]string, len(keep))
		for j, i := range keep {
			v := clean(row[i])
			if v == "" && !cols[i].blank {
				v = "-"
			}
			out[j] = v
		}
		return out
	}
	head := make([]string, len(cols))
	for i, c := range cols {
		head[i] = c.head
	}
	var all [][]string
	if header {
		all = append(all, clamp(head))
	}
	for _, r := range rows {
		all = append(all, clamp(r))
	}
	widths := make([]int, len(keep))
	for _, r := range all {
		for j, v := range r {
			widths[j] = max(widths[j], cells(v))
		}
	}
	if p.tty {
		p.shrinkTitle(cols, keep, widths, all)
	}
	for ri, r := range all {
		parts := make([]string, len(r))
		for j, v := range r {
			if w := widths[j]; cells(v) > w {
				v = cut(v, w)
			}
			parts[j] = padTo(v, widths[j], cols[keep[j]].right)
		}
		line := strings.TrimRight(strings.Join(parts, "  "), " ")
		if ri == 0 && header {
			line = p.bold(line)
		}
		_, _ = fmt.Fprintln(p.out, line)
	}
}

func (p *printer) shrinkTitle(cols []column, keep, widths []int, rows [][]string) {
	total := 2 * (len(keep) - 1)
	for _, w := range widths {
		total += w
	}
	if total <= p.width {
		return
	}
	for j, i := range keep {
		if cols[i].title {
			widths[j] = max(widths[j]-(total-p.width), 8)
		}
	}
	_ = rows
}

// humanSize renders bytes as KB/MB/GB, one decimal above 1 MB.
func humanSize(n int64) string {
	const k = 1024.0
	f := float64(n)
	switch {
	case n < 0:
		return ""
	case n == 0:
		return "0 B"
	case f < k:
		return fmt.Sprintf("%d B", n)
	case f < k*k:
		return fmt.Sprintf("%.0f KB", f/k)
	case f < k*k*k:
		return fmt.Sprintf("%.1f MB", f/(k*k))
	}
	return fmt.Sprintf("%.1f GB", f/(k*k*k))
}

func errorDetails(e *app.Error) map[string]any {
	d := map[string]any{}
	if e.Code == app.CodeUpdateFailed || e.Code == app.CodeUpdateUnavailable {
		for _, key := range []string{"reason", "restored"} {
			if v, ok := e.Args[key]; ok {
				d[key] = v
			}
		}
	}
	return d
}
