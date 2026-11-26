// Package compose is the composition root: it builds the app service and the settings a front end
// reads from the configuration, the same way for every binary (mothdex and mothdex-gui).
package compose

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/buildinfo"
	"github.com/Or4cu1o/mothdex-cli/internal/cache"
	"github.com/Or4cu1o/mothdex-cli/internal/config"
	"github.com/Or4cu1o/mothdex-cli/internal/cover"
	"github.com/Or4cu1o/mothdex-cli/internal/history"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	"github.com/Or4cu1o/mothdex-cli/internal/library"
	mlog "github.com/Or4cu1o/mothdex-cli/internal/log"
	"github.com/Or4cu1o/mothdex-cli/internal/proxy"
	"github.com/Or4cu1o/mothdex-cli/internal/update"
	"github.com/Or4cu1o/mothdex-cli/locales"
	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
	"github.com/Or4cu1o/mothdex-cli/profiles"
)

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// cooldownKey is the `state` item that carries the 429 cooldown across runs (api-client section 4).
const cooldownKey = "api_cooldown"

// maxCooldown is the breaker cooldown (api-client section 4); a stored value beyond it is clamped.
const maxCooldown = 10 * time.Minute

// Options are what the front end decides before the service exists (flags and the command it runs).
type Options struct {
	ConfigFile      string // --config; empty selects the default location
	Profile         string // --profile
	Lang            string // --lang; empty uses interface.language
	LogLevel        string // --log-level; empty keeps log.level
	Jobs            int    // -j; 0 keeps download.concurrency
	Force           bool
	AllowExecutable bool
	NoColor         bool
	// Downloads enables the Downloads capability (a command that downloads, the Shell, the TUI, the GUI).
	Downloads bool
	// Session is true when one service serves many commands (Shell, TUI, GUI): -f and
	// --allow-executable given at start-up are not applied to the whole session (ui.md section 2.1).
	Session bool
	// Repair tolerates a broken configuration file (config set and config reset must be able to fix it).
	Repair bool
	// Logs is the log of the process, started in main; Open attaches it to paths.logs.
	Logs *mlog.Session
}

// Session is what Open returns: the service plus the settings the front end reads. It carries no
// UI types, so every front end wraps it.
type Session struct {
	Updater       *update.Updater // nil when it cannot be built
	UpdateCheck   bool            // the automatic check is on: every off switch applied
	UpdateChannel string          // update.channel
	Svc           *app.Service
	T             *i18n.Translator
	Prefer        app.Preference // download.prefer baseline
	Boxart        bool           // download.boxart
	Confirm       bool           // interface.confirm_downloads
	PageSize      int            // interface.page_size
	Color         string         // interface.color: auto | always | never
	Warnings      []string
	LogPath       string        // mothdex.log
	Logs          *mlog.Session // the process log; nil when main started none
	LogsDir       string        // resolved paths.logs
	DataDir       string        // resolved paths.data
	NoColor       bool          // the final color decision of cli-conventions section 4
	Log           *slog.Logger  // never nil
	Close         func()        // releases the service and mothdex.log; main closes Logs

	configFile, profile string // the --config and --profile of Open, for BundleOptions
}

// Open builds config, logger, i18n, cache, API client, cover fetcher, history, profile and the app
// service. It is the only place that knows every piece.
func Open(_ context.Context, o Options) (*Session, error) {
	plat, err := config.CurrentPlatform()
	if err != nil {
		return nil, &app.Error{Code: app.CodeInternal, Message: "cannot describe the machine", Cause: err}
	}
	file, err := config.FilePath(plat, o.ConfigFile)
	if err != nil {
		return nil, &app.Error{Code: app.CodeInvalidConfig, Message: "cannot find the configuration file", Cause: err}
	}
	var warnings []string
	flags := map[string]string{}
	if o.Jobs > 0 {
		flags["download.concurrency"] = strconv.Itoa(o.Jobs)
	}
	if o.LogLevel != "" {
		flags["log.level"] = o.LogLevel
	}
	res, err := config.Load(config.Options{File: file, Environ: os.Environ(), Flags: flags, Force: o.Force && !o.Session}) // a session never honors a start-up -f, so a stored overwrite is reported here
	if err != nil {
		// `config reset` and `config set` must be able to repair a broken file: carry on with the
		// defaults for those commands and let the service report the problem when it reads it.
		if !o.Repair {
			return nil, configFailure(err)
		}
		warnings = append(warnings, "the configuration file is invalid; using defaults for this command")
		if res, err = config.Load(config.Options{}); err != nil {
			return nil, configFailure(err)
		}
	}
	cfg := res.Config
	warnings = append(warnings, res.Warnings...)
	paths, err := config.ResolvePaths(plat, cfg, o.ConfigFile)
	if err != nil {
		return nil, &app.Error{Code: app.CodeInvalidConfig, Message: "cannot resolve the data folders", Cause: err}
	}

	if o.Logs != nil {
		if aerr := o.Logs.Attach(attachOptions(cfg, paths, plat)); aerr != nil {
			warnings = append(warnings, "the session log cannot be written: "+aerr.Error())
		}
	}
	logOpts := mlog.FromConfig(cfg.Log, paths, os.Stderr)
	logOpts.Session, logOpts.Secrets = o.Logs, []string{cfg.API.BaseURL}
	logger, closeLog, lerr := mlog.New(logOpts)
	if lerr != nil {
		warnings = append(warnings, "the log file cannot be written: "+lerr.Error())
	}

	bundle, err := i18n.LoadDir(locales.FS, filepath.Join(filepath.Dir(paths.ConfigFile), "locales"))
	if err != nil {
		return nil, &app.Error{Code: app.CodeInternal, Message: "cannot load the translations", Cause: err}
	}
	warnings = append(warnings, bundle.Warnings()...)
	lang := o.Lang
	if lang == "" {
		lang = cfg.Interface.Language
	}
	tr := bundle.New(bundle.Resolve(lang, os.Getenv, i18n.SystemLanguages))

	store, err := cache.Open(filepath.Join(paths.Temp, "cache"), cache.Config{
		Enabled: cfg.Cache.Enabled, MaxBytes: int64(cfg.Cache.MaxSizeMB) << 20,
		TTL: cache.TTL{Search: cfg.Cache.TTL.Search, Entry: cfg.Cache.TTL.Entry, Meta: cfg.Cache.TTL.Meta, Info: cfg.Cache.TTL.Info, Covers: cfg.Cache.TTL.Covers},
	}, realClock{}, logger)
	if err != nil {
		return nil, &app.Error{Code: app.CodeInternal, Message: "cannot open the cache", Cause: err}
	}

	ua := mothdex.DefaultUserAgent(buildinfo.Get().Version)
	opts := []mothdex.Option{
		mothdex.WithBaseURL(cfg.API.BaseURL), mothdex.WithTimeout(cfg.API.Timeout), mothdex.WithRateLimit(cfg.API.RateLimit, 1),
		mothdex.WithMaxRetries(cfg.API.MaxRetries), mothdex.WithUserAgent(ua), mothdex.WithLogger(logger),
		mothdex.WithBreachHook(func(until time.Time) { _ = store.PutJSON(cache.NSState, cooldownKey, until) }),
	}
	var until time.Time
	if e, gerr := store.GetJSON(cache.NSState, cooldownKey, &until); gerr == nil && e.Hit && until.After(time.Now()) {
		if limit := time.Now().Add(maxCooldown); until.After(limit) {
			until = limit // a corrupt state file or a clock jump must not block the API for long
		}
		opts = append(opts, mothdex.WithCooldown(until))
	}
	client, err := mothdex.New(opts...)
	if err != nil {
		return nil, &app.Error{Code: app.CodeInvalidConfig, Message: "invalid API settings", Cause: err}
	}
	fetcher, err := cover.New(cover.Options{Resolver: cover.SystemResolver{}, Cache: store, Log: logger, UserAgent: ua})
	if err != nil {
		return nil, &app.Error{Code: app.CodeInternal, Message: "cannot build the cover fetcher", Cause: err}
	}
	hist, err := history.Open(filepath.Join(paths.Data, "history.jsonl"))
	if err != nil {
		return nil, &app.Error{Code: app.CodeInvalidConfig, Message: "the data folder must be an absolute path", Cause: err}
	}

	info, detect := detectProfile(o, cfg, paths, file, &warnings)
	if o.Logs != nil {
		o.Logs.Update(mlog.Info{Profile: info.Profile.ID, Lang: tr.Locale(), ConfigSource: configSource(file)})
	}
	// RAM disk (PRT-G11): said at every start in the selected interface language.
	for _, w := range info.StorageWarnings {
		warnings = append(warnings, tr.T(library.StorageWarningKey(w), nil))
	}
	svc, err := app.New(app.Options{
		Client: client, Cache: store, Clock: realClock{}, BaseURL: cfg.API.BaseURL, Covers: fetcher,
		AllowedHosts: cfg.Download.AllowedHosts, PageSize: cfg.Interface.PageSize, Prefer: preference(cfg.Download.Prefer),
		History: hist, Config: app.ConfigSource{File: file, Environ: os.Environ(), Flags: flags}, Profile: info, Detect: detect,
	})
	if err != nil {
		return nil, wrapInternal(err)
	}
	if o.Downloads {
		// The Shell opens the service once for many commands, so -f and --allow-executable given at
		// start-up must not allow overwrites or executables for the whole session (ui.md section 2.1).
		force, allowExec := startupGrants(o)
		env := app.Env{
			TempDir: paths.Temp, DataDir: paths.Data, Profile: info, History: hist, Concurrency: cfg.Download.Concurrency,
			Download: app.DownloadConfig{AllowedHosts: cfg.Download.AllowedHosts, Concurrency: cfg.Download.Concurrency, OnExists: cfg.Download.OnExists,
				SkipVerify: !cfg.Download.Verify, AllowExecutable: allowExec, Force: force, Dir: cfg.Download.Dir, Confirm: cfg.Interface.ConfirmDownloads},
		}
		if cfg.Profile.Refresh {
			env.Refresh = func(ctx context.Context) error { return info.Profile.Refresh(ctx, library.NewRefreshClient()) }
		}
		if err := svc.EnableDownloads(env); err != nil {
			return nil, wrapInternal(err)
		}
	}
	updater, uerr := NewUpdater(cfg.Update, paths.Data, logger)
	if uerr != nil {
		logger.Debug("update unavailable", "cause", uerr)
	}
	updateCheck := false
	if updater != nil {
		reason := updater.SkipReason(cfg.Update.Check, os.Getenv)
		updateCheck = reason == ""
		if reason != "" {
			updater.LogSkipped(reason)
		}
	}
	return &Session{
		Updater: updater, UpdateCheck: updateCheck, UpdateChannel: cfg.Update.Channel,
		Svc: svc, T: tr, Prefer: preference(cfg.Download.Prefer), Boxart: cfg.Download.Boxart, Confirm: cfg.Interface.ConfirmDownloads,
		PageSize: cfg.Interface.PageSize, Color: cfg.Interface.Color, Warnings: warnings, LogPath: filepath.Join(paths.Logs, mlog.FileName),
		Close: func() { _ = svc.Close(); _ = closeLog.Close() },
		Logs:  o.Logs, LogsDir: paths.Logs, configFile: o.ConfigFile, profile: o.Profile,
		DataDir: paths.Data, Log: logger,
		NoColor: o.NoColor || cfg.Interface.Color == "never" || os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb",
	}, nil
}

// startupGrants are the start-up flags that reach the downloader: a session serves many commands, so
// -f and --allow-executable given when it started are not applied to all of them (ui.md section 2.1).
func startupGrants(o Options) (force, allowExecutable bool) {
	return o.Force && !o.Session, o.AllowExecutable && !o.Session
}

func preference(p config.Prefer) app.Preference {
	size := app.SizeNone
	switch p.Size {
	case "smallest":
		size = app.SizeSmallest
	case "largest":
		size = app.SizeLargest
	}
	return app.Preference{Languages: p.Languages, Formats: p.Formats, Sources: p.Sources, Size: size}
}

// detectProfile loads the embedded and the user's profiles and returns the active one plus the
// function `profile detect` calls to run detection again.
func detectProfile(o Options, cfg config.Config, paths config.ResolvedPaths, file string, warnings *[]string) (library.ProfileInfo, func(context.Context) (library.ProfileInfo, error)) {
	var user fs.FS
	if dir := filepath.Join(filepath.Dir(file), "profiles"); dirExists(dir) {
		user = os.DirFS(dir)
	}
	loaded := library.LoadAll(profiles.FS, user, library.LoadOptions{})
	*warnings = append(*warnings, loaded.Warnings...)
	for _, e := range loaded.Errors {
		*warnings = append(*warnings, e.Error())
	}
	lcfg := library.Config{Flag: o.Profile, ID: cfg.Profile.ID, ROMRoot: cfg.Profile.ROMRoot, RetroBatPath: cfg.Profile.RetroBatPath,
		Folders: cfg.Profile.Folders, RefreshOff: !cfg.Profile.Refresh, GenericROMRoot: paths.ROMRoot, DataDir: paths.Data}
	run := func(context.Context) (library.ProfileInfo, error) {
		env, err := library.OSEnv()
		if err != nil {
			return library.ProfileInfo{}, err
		}
		env.MemoryBacked = proxy.IsMemoryBacked
		return library.Detect(env, loaded.Profiles, lcfg), nil
	}
	info, _ := run(context.Background())
	return info, run
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// configFailure maps a configuration error to the stable invalid_config / usage codes.
func configFailure(err error) error {
	var ce *config.Error
	if errors.As(err, &ce) {
		return &app.Error{Code: ce.Code(), Message: ce.Reason, Cause: err,
			Args: map[string]any{"key": ce.Key, "value": ce.Value, "source": string(ce.Source)}}
	}
	return &app.Error{Code: app.CodeInvalidConfig, Message: "cannot read the configuration", Cause: err}
}

func wrapInternal(err error) error {
	return &app.Error{Code: app.CodeInternal, Message: "cannot start", Cause: err}
}
