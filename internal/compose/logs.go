package compose

import (
	"errors"
	"io/fs"
	"os"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/buildinfo"
	"github.com/Or4cu1o/mothdex-cli/internal/config"
	mlog "github.com/Or4cu1o/mothdex-cli/internal/log"
)

func attachOptions(cfg config.Config, paths config.ResolvedPaths, plat config.Platform) mlog.AttachOptions {
	return mlog.AttachOptions{
		Dir: paths.Logs, Level: cfg.Log.SessionLevel, Keep: cfg.Log.SessionsKeep,
		Secrets: []string{cfg.API.BaseURL}, Home: plat.HomeDir, MaxSizeMB: cfg.Log.MaxSizeMB, MaxFiles: cfg.Log.MaxFiles,
	}
}

// configSource names where the settings came from, for the session header.
func configSource(file string) string {
	if _, err := os.Stat(file); errors.Is(err, fs.ErrNotExist) {
		return "defaults"
	}
	return file
}

// Diagnostics attaches the process log without opening the service, for a command that needs no
// service (`logs`, and an error before the service exists), and returns what a bundle needs. A
// configuration that cannot be read is returned as the error, next to options built from the
// defaults.
func Diagnostics(logs *mlog.Session, configFile, profile, lang string) (mlog.BundleOptions, error) {
	plat, err := config.CurrentPlatform()
	if err != nil {
		return mlog.BundleOptions{}, err
	}
	file, err := config.FilePath(plat, configFile)
	if err != nil {
		return mlog.BundleOptions{}, err
	}
	flagPath := configFile
	res, loadErr := config.Load(config.Options{File: file, Environ: os.Environ()})
	if loadErr != nil {
		flagPath = "" // never take a path from a configuration that did not load
		if res, err = config.Load(config.Options{}); err != nil {
			return mlog.BundleOptions{}, errors.Join(loadErr, err)
		}
	}
	paths, err := config.ResolvePaths(plat, res.Config, flagPath)
	if err != nil {
		return mlog.BundleOptions{}, errors.Join(loadErr, err)
	}
	var attachErr error
	first := logs != nil && !logs.Attached() // else compose.Open already described the session
	if logs != nil {
		// A folder that cannot be written can still hold old sessions to read.
		attachErr = logs.Attach(attachOptions(res.Config, paths, plat))
	}
	if lang == "" {
		lang = res.Config.Interface.Language
	}
	if profile == "" {
		profile = res.Config.Profile.ID
	}
	if first {
		logs.Update(mlog.Info{Profile: profile, Lang: lang, ConfigSource: configSource(file)})
	}
	settings := res.Settings()
	entries := make([]mlog.ConfigEntry, len(settings))
	for i, s := range settings {
		entries[i] = mlog.ConfigEntry{Key: s.Key, Value: s.Value, Source: string(s.Source)}
	}
	dir := paths.Logs
	if logs != nil && logs.Dir() != "" {
		dir = logs.Dir() // the default folder, when the configured one was refused
	}
	b := buildinfo.Get()
	return mlog.BundleOptions{
		Dir: dir, Home: plat.HomeDir, Secrets: []string{res.Config.API.BaseURL},
		Manifest: mlog.Manifest{Version: b.Version, Commit: b.Commit, Profile: profile, Lang: lang, Config: entries},
	}, errors.Join(loadErr, attachErr)
}

// BundleOptions are the inputs of a bundle for an open session, with the settings in use now.
func (s *Session) BundleOptions() mlog.BundleOptions {
	// the service already runs on this configuration and reported what was wrong with it
	o, _ := Diagnostics(s.Logs, s.configFile, s.profile, s.lang())
	o.Dir = s.LogsDir
	if list, err := s.Svc.ConfigList(); err == nil {
		o.Manifest.Config = settingEntries(list)
	}
	return o
}

func (s *Session) lang() string {
	if s.T == nil {
		return ""
	}
	return s.T.Locale()
}

func settingEntries(list []app.Setting) []mlog.ConfigEntry {
	out := make([]mlog.ConfigEntry, len(list))
	for i, s := range list {
		out[i] = mlog.ConfigEntry{Key: s.Key, Value: s.Value, Source: string(s.Source)}
	}
	return out
}
