// Package cli is the command-line UI: cobra commands that call the app service and render its
// answers (cli-output.md, cli-conventions.md). It holds no business rules.
package cli

import (
	"context"
	"io"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/buildinfo"
	"github.com/Or4cu1o/mothdex-cli/internal/cache"
	"github.com/Or4cu1o/mothdex-cli/internal/config"
	"github.com/Or4cu1o/mothdex-cli/internal/history"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	"github.com/Or4cu1o/mothdex-cli/internal/library"
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
	Enqueue(ctx context.Context, reqs []app.DownloadRequest) ([]app.JobID, error)
	Events(ctx context.Context) <-chan app.Event
	Cancel(id app.JobID) error
	Wait(ctx context.Context) (app.BatchResult, error)
	History(ctx context.Context, f history.Filter) ([]history.Record, error)
	CacheStats() (cache.Stats, error)
	CacheClear(ns ...string) error
	ConfigGet(key string) (config.Setting, error)
	ConfigList() ([]config.Setting, error)
	ConfigSet(key, value string) error
	ConfigReset(key string) error
	ActiveProfile() library.ProfileInfo
	DetectProfile(ctx context.Context) (library.ProfileInfo, error)
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
}

// Deps are the things the commands need from the outside: tests replace all of them.
type Deps struct {
	// Open builds the service for one run (config, cache, client, downloader). It is called once,
	// after the global flags are parsed, and only by commands that need a service.
	Open func(ctx context.Context, g Globals) (*Session, error)
	// Translator returns an en_us-capable translator for errors that happen before Open.
	Translator func(lang string) *i18n.Translator
	Version    func() buildinfo.Info
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
	// IsTerminal reports whether a stream is a terminal; Width is the terminal width in columns.
	IsTerminal func(any) bool
	Width      func() int
	Getenv     func(string) string
}
