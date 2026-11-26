// Package cli is the command-line UI: cobra commands that call the app service and render its
// answers (cli-output.md, cli-conventions.md). It holds no business rules.
package cli

import (
	"context"
	"io"
	"log/slog"
	"os"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/buildinfo"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	mlog "github.com/Or4cu1o/mothdex-cli/internal/log"
	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

// Service is what the commands use of *app.Service; tests pass a fake.
type Service interface {
	Search(ctx context.Context, q app.SearchQuery) (app.SearchPage, error)
	Entry(ctx context.Context, ref app.Ref) (app.Result[mothdex.Entry], error)
	Random(ctx context.Context, q app.RandomQuery) (app.Result[[]mothdex.Entry], error)
	Platforms(ctx context.Context, f app.PlatformFilter) (app.Result[[]mothdex.Platform], error)
	Regions(ctx context.Context) (app.Result[[]mothdex.Region], error)
	DBInfo(ctx context.Context) (app.Result[mothdex.DBInfo], error)
	Links(ctx context.Context, ref app.Ref, sel app.LinkSelector) ([]app.LinkChoice, error)
	Cover(ctx context.Context, ref app.Ref) (io.ReadCloser, string, error)
	SaveBoxart(ctx context.Context, ref app.Ref, destDir string) (string, error)
	SaveJobBoxart(ctx context.Context, job app.JobSnapshot, destDir string) (string, error)
	Enqueue(ctx context.Context, reqs []app.DownloadRequest) ([]app.JobID, error)
	Jobs() []app.JobSnapshot
	Events(ctx context.Context) <-chan app.Event
	Cancel(id app.JobID) error
	Wait(ctx context.Context) (app.BatchResult, error)
	History(ctx context.Context, f app.HistoryFilter) ([]app.HistoryRecord, error)
	CacheStats() (app.CacheStats, error)
	CacheClear(ns ...string) error
	ConfigGet(key string) (app.Setting, error)
	ConfigList() ([]app.Setting, error)
	ConfigSchema() []app.SettingSpec
	ConfigSet(key, value string) error
	ConfigReset(key string) error
	ActiveProfile() app.ProfileInfo
	DetectProfile(ctx context.Context) (app.ProfileInfo, error)
	Close() error
}

var _ Service = (*app.Service)(nil)

// Globals are the flags every command accepts.
type Globals struct {
	JSON, Quiet, NoColor, Verbose, Version bool
	Lang, Config, Profile                  string
	Refresh                                bool
	Force, AllowExecutable                 bool
	Jobs                                   int
	Output                                 string // -o on download and boxart
	Command                                string // top-level command being run
	Downloads                              bool   // the command needs the Downloads capability
	Shell                                  bool   // an interactive UI (Shell or TUI) opens the service (ui.md section 2.1)
}

// Session is what Open returns: the service plus the settings the commands read.
type Session struct {
	Svc      Service
	T        *i18n.Translator
	Prefer   app.Preference // download.prefer baseline; --prefer replaces a key's list
	Boxart   bool           // download.boxart
	Confirm  bool           // interface.confirm_downloads
	PageSize int            // interface.page_size
	Color    string         // interface.color: auto | always | never
	Warnings []string
	LogPath  string // the log file, for the internal error hint
	Close    func() // releases the service; may be nil

	// What the interactive UIs need (ui.md section 1).
	DataDir string        // resolved paths.data, home of the UI-owned files
	NoColor bool          // the final color decision of cli-conventions section 4
	UTF8    bool          // glyphs, or their ASCII fallbacks
	Log     *slog.Logger  // may be nil
	Logs    *mlog.Session // the process log; may be nil
}

// Deps are the things the commands need from the outside: tests replace all of them.
type Deps struct {
	// Open builds the service for one run (config, cache, client, downloader). It is called once,
	// after the global flags are parsed, and only by commands that need a service.
	Open func(ctx context.Context, g Globals) (*Session, error)
	// Translator returns an en_us-capable translator for errors that happen before Open.
	Translator func(lang string) *i18n.Translator
	Version    func() buildinfo.Info
	// Logs is the log of the process; nil gives refs without writing them.
	Logs *mlog.Session
	// Diagnostics opens the log files without opening the service and returns what a bundle needs,
	// with the error of a configuration that does not load.
	Diagnostics func(g Globals) (mlog.BundleOptions, error)
	Stdin       io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
	// IsTerminal reports whether a stream is a terminal; Width is the terminal width in columns.
	IsTerminal func(any) bool
	Width      func() int
	Getenv     func(string) string

	// Shell runs the interactive Shell (internal/ui/shell); nil in tests that do not need it. The
	// composition root injects it so that cli never imports the UI packages (ui.md section 1).
	Shell func(ctx context.Context, env ShellEnv) int
	// TUI runs the full-screen terminal UI (internal/ui/tui); nil in tests that do not need it.
	TUI func(ctx context.Context, env TUIEnv) int
	// Interrupts hands SIGINT to the caller until release runs; meanwhile the root context is not
	// canceled by it. SIGTERM keeps canceling the root. Nil means there is nothing to hand over.
	Interrupts func() (sig <-chan os.Signal, release func())
}

// TUIEnv is what the TUI launcher receives. The session is open with the downloads capability.
type TUIEnv struct {
	Session    *Session
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
	Interrupts func() (sig <-chan os.Signal, release func())
}

// ShellEnv is what the Shell launcher receives.
type ShellEnv struct {
	Runner     *Runner
	Session    *Session
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
	IsTerminal bool // stdin and stdout are terminals
	Interrupts func() (sig <-chan os.Signal, release func())
}
