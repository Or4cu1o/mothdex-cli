package tui

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

// service is the part of the app service the TUI uses; tests pass a fake (ui.md section 1).
type service interface {
	Search(ctx context.Context, q app.SearchQuery) (app.SearchPage, error)
	Entry(ctx context.Context, ref app.Ref) (app.Result[mothdex.Entry], error)
	Random(ctx context.Context, q app.RandomQuery) (app.Result[[]mothdex.Entry], error)
	Platforms(ctx context.Context, f app.PlatformFilter) (app.Result[[]mothdex.Platform], error)
	Regions(ctx context.Context) (app.Result[[]mothdex.Region], error)
	DBInfo(ctx context.Context) (app.Result[mothdex.DBInfo], error)
	Links(ctx context.Context, ref app.Ref, sel app.LinkSelector) ([]app.LinkChoice, error)
	SaveBoxart(ctx context.Context, ref app.Ref, destDir string) (string, error)
	SaveJobBoxart(ctx context.Context, job app.JobSnapshot, destDir string) (string, error)
	Enqueue(ctx context.Context, reqs []app.DownloadRequest) ([]app.JobID, error)
	Jobs() []app.JobSnapshot
	Events(ctx context.Context) <-chan app.Event
	Cancel(id app.JobID) error
	History(ctx context.Context, f app.HistoryFilter) ([]app.HistoryRecord, error)
	ConfigList() ([]app.Setting, error)
	ConfigSet(key, value string) error
	ConfigReset(key string) error
	CacheStats() (app.CacheStats, error)
	CacheClear(ns ...string) error
	ActiveProfile() app.ProfileInfo
}

// env is what every screen shares and never changes.
type env struct {
	ctx      context.Context
	svc      service
	t        *i18n.Translator
	st       styles
	prefer   app.Preference // download.prefer, the baseline of the P overlay
	confirm  bool           // interface.confirm_downloads at start-up
	boxart   bool           // download.boxart at start-up
	pageSize int
	dataDir  string
	logPath  string
	log      *slog.Logger
	now      func() time.Time
	// after runs msg once d has passed. Tests replace it, so nothing waits on a real clock.
	after func(d time.Duration, msg tea.Msg) tea.Cmd
	seq   *atomic.Int64
	num   *message.Printer // numbers with the grouping of the locale
}

// realAfter is the production timer.
func realAfter(d time.Duration, msg tea.Msg) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return msg })
}

// token returns a number no other request has: a screen accepts an answer only if it carries the
// token of its latest request, so a canceled or superseded one is ignored.
func (e *env) token() int64 { return e.seq.Add(1) }

// yesAlias turns the first letter of the localized "Yes" into y, so [s/N] and [y/N] both work.
func (e *env) yesAlias(k string) string {
	yes := []rune(strings.ToLower(e.t.T("ui.messages.yes", nil)))
	if len(yes) > 0 && k == string(yes[0]) {
		return "y"
	}
	return k
}

// T translates a key.
func (e *env) T(key string, args i18n.Args) string { return e.t.T(key, args) }

// N translates a plural key.
func (e *env) N(key string, n int, args i18n.Args) string { return e.t.N(key, n, args) }

var unfilled = regexp.MustCompile(`\{[a-z_]+\}`)

// failure is an error ready to show: the localized message and hint, and the stable code.
type failure struct {
	msg, hint, code string
}

// failureOf localizes an error from the service the way the CLI does: errors.<code>.message and
// .hint, falling back to the service text when a placeholder has no value. Server text is cleaned.
func (e *env) failureOf(err error) failure {
	var ae *app.Error
	if !errors.As(err, &ae) {
		ae = &app.Error{Code: app.CodeInternal, Message: "unexpected error", Cause: err}
	}
	args := i18n.Args{"detail": ae.Message, "log_path": e.logPath}
	for k, v := range ae.Args {
		args[k] = v
	}
	msg := ae.Message
	if key := "errors." + ae.Code + ".message"; e.t.Has(key) {
		if m := e.t.T(key, args); !unfilled.MatchString(m) {
			msg = m
		}
	}
	hint := ""
	if key := "errors." + ae.Code + ".hint"; e.t.Has(key) {
		if h := e.t.T(key, args); !unfilled.MatchString(h) {
			hint = h
		}
	}
	return failure{msg: clean(msg), hint: clean(hint), code: ae.Code}
}

// humanSize renders bytes as KB/MB/GB, one decimal above 1 MB (cli-conventions section 5).
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

// clock renders a duration as mm:ss, or h:mm:ss from one hour.
func clock(d time.Duration) string {
	s := int(d.Round(time.Second).Seconds())
	if s < 0 {
		s = 0
	}
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s%3600/60, s%60)
	}
	return fmt.Sprintf("%02d:%02d", s/60, s%60)
}

// age renders how old a cached answer is: 1m, 5m, 3h, 2d.
func age(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "1m"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// preferSummary writes a preference the way --prefer does: lang=En,Pt format=gbc size=smallest.
func preferSummary(p app.Preference) string {
	var parts []string
	add := func(key string, vs []string) {
		if len(vs) > 0 {
			parts = append(parts, key+"="+strings.Join(vs, ","))
		}
	}
	add("lang", p.Languages)
	add("format", p.Formats)
	add("source", p.Sources)
	if p.Size != app.SizeNone && p.Size != "" {
		parts = append(parts, "size="+string(p.Size))
	}
	return strings.Join(parts, " ")
}

// doneKey identifies a downloaded file: the entry and the link number.
func doneKey(slug string, link int) string { return fmt.Sprintf("%s|%d", slug, link) }

// printer returns a number printer for the translator's locale (1,284,019 or 1.284.019).
func printer(t *i18n.Translator) *message.Printer {
	return message.NewPrinter(language.Make(strings.ReplaceAll(t.Locale(), "_", "-")))
}
