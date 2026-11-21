package main

import (
	"context"
	"errors"
	"io/fs"
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
	"github.com/Or4cu1o/mothdex-cli/internal/ui/cli"
	"github.com/Or4cu1o/mothdex-cli/locales"
	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
	"github.com/Or4cu1o/mothdex-cli/profiles"
)

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// cooldownKey is the `state` item that carries the 429 cooldown across runs (api-client section 4).
const cooldownKey = "api_cooldown"

// open is the composition root: config, logger, i18n, cache, API client, cover fetcher, history,
// profile, then the app service. It is the only place that knows every piece.
func open(_ context.Context, g cli.Globals) (*cli.Session, error) {
	plat, err := config.CurrentPlatform()
	if err != nil {
		return nil, &app.Error{Code: app.CodeInternal, Message: "cannot describe the machine", Cause: err}
	}
	file, err := config.FilePath(plat, g.Config)
	if err != nil {
		return nil, &app.Error{Code: app.CodeInvalidConfig, Message: "cannot find the configuration file", Cause: err}
	}
	var warnings []string
	flags := map[string]string{}
	if g.Jobs > 0 {
		flags["download.concurrency"] = strconv.Itoa(g.Jobs)
	}
	res, err := config.Load(config.Options{File: file, Environ: os.Environ(), Flags: flags, Force: g.Force})
	if err != nil {
		// `config reset` and `config set` must be able to repair a broken file: carry on with the
		// defaults for those commands and let the service report the problem when it reads it.
		if g.Command != "config" {
			return nil, configFailure(err)
		}
		warnings = append(warnings, "the configuration file is invalid; using defaults for this command")
		if res, err = config.Load(config.Options{}); err != nil {
			return nil, configFailure(err)
		}
	}
	cfg := res.Config
	warnings = append(warnings, res.Warnings...)
	paths, err := config.ResolvePaths(plat, cfg, g.Config)
	if err != nil {
		return nil, &app.Error{Code: app.CodeInvalidConfig, Message: "cannot resolve the data folders", Cause: err}
	}

	logger, closeLog, lerr := mlog.New(mlog.FromConfig(cfg.Log, paths, os.Stderr))
	if lerr != nil {
		warnings = append(warnings, "the log file cannot be written: "+lerr.Error())
	}

	bundle, err := i18n.LoadDir(locales.FS, filepath.Join(filepath.Dir(paths.ConfigFile), "locales"))
	if err != nil {
		return nil, &app.Error{Code: app.CodeInternal, Message: "cannot load the translations", Cause: err}
	}
	warnings = append(warnings, bundle.Warnings()...)
	lang := g.Lang
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

	info, detect := detectProfile(g, cfg, paths, file, &warnings)
	svc, err := app.New(app.Options{
		Client: client, Cache: store, Clock: realClock{}, BaseURL: cfg.API.BaseURL, Covers: fetcher,
		AllowedHosts: cfg.Download.AllowedHosts, PageSize: cfg.Interface.PageSize, Prefer: preference(cfg.Download.Prefer),
		History: hist, Config: app.ConfigSource{File: file, Environ: os.Environ(), Flags: flags}, Profile: info, Detect: detect,
	})
	if err != nil {
		return nil, wrapInternal(err)
	}
	if g.Downloads {
		env := app.Env{
			TempDir: paths.Temp, DataDir: paths.Data, Profile: info, History: hist, Concurrency: cfg.Download.Concurrency,
			Download: app.DownloadConfig{AllowedHosts: cfg.Download.AllowedHosts, Concurrency: cfg.Download.Concurrency, OnExists: cfg.Download.OnExists,
				SkipVerify: !cfg.Download.Verify, AllowExecutable: g.AllowExecutable, Force: g.Force, Dir: cfg.Download.Dir},
		}
		if cfg.Profile.Refresh {
			env.Refresh = func(ctx context.Context) error { return info.Profile.Refresh(ctx, library.NewRefreshClient()) }
		}
		if err := svc.EnableDownloads(env); err != nil {
			return nil, wrapInternal(err)
		}
	}
	return &cli.Session{
		Svc: svc, T: tr, Prefer: preference(cfg.Download.Prefer), Boxart: cfg.Download.Boxart, Confirm: cfg.Interface.ConfirmDownloads,
		PageSize: cfg.Interface.PageSize, Color: cfg.Interface.Color, Warnings: warnings,
		Close: func() { _ = svc.Close(); _ = closeLog.Close() },
	}, nil
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
func detectProfile(g cli.Globals, cfg config.Config, paths config.ResolvedPaths, file string, warnings *[]string) (library.ProfileInfo, func(context.Context) (library.ProfileInfo, error)) {
	var user fs.FS
	if dir := filepath.Join(filepath.Dir(file), "profiles"); dirExists(dir) {
		user = os.DirFS(dir)
	}
	loaded := library.LoadAll(profiles.FS, user, library.LoadOptions{})
	*warnings = append(*warnings, loaded.Warnings...)
	for _, e := range loaded.Errors {
		*warnings = append(*warnings, e.Error())
	}
	lcfg := library.Config{Flag: g.Profile, ID: cfg.Profile.ID, ROMRoot: cfg.Profile.ROMRoot, RetroBatPath: cfg.Profile.RetroBatPath,
		Folders: cfg.Profile.Folders, RefreshOff: !cfg.Profile.Refresh, GenericROMRoot: paths.ROMRoot}
	run := func(context.Context) (library.ProfileInfo, error) {
		env, err := library.OSEnv()
		if err != nil {
			return library.ProfileInfo{}, err
		}
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
