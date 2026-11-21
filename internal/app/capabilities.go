package app

import (
	"context"
	"errors"
	"strings"

	"github.com/Or4cu1o/mothdex-cli/internal/config"
)

// History lists the transfer history, newest first (history.Filter{Failed} for --failed).
func (s *Service) History(ctx context.Context, f HistoryFilter) ([]HistoryRecord, error) {
	if s.hist == nil {
		return nil, newError(CodeInternal, "history is not enabled")
	}
	recs, err := s.hist.List(ctx, f)
	return recs, wrap(err)
}

// CacheStats reports items and bytes per namespace.
func (s *Service) CacheStats() (CacheStats, error) {
	st, err := s.o.Cache.Stats()
	return st, wrap(err)
}

// CacheClear empties the given namespaces; none means all evictable ones (never session or state).
func (s *Service) CacheClear(ns ...string) error {
	if err := s.o.Cache.Clear(ns...); err != nil {
		return &Error{Code: CodeInvalidRequest, Message: "cannot clear the cache namespace", Cause: err}
	}
	return nil
}

// ConfigSource says where settings come from.
type ConfigSource struct {
	File    string            // config file path ("" = none)
	Environ []string          // KEY=VALUE, as os.Environ()
	Flags   map[string]string // config key -> raw value from command flags
}

// loadConfig reads the effective settings. Reads use Force so that a file holding
// download.on_exists=overwrite can still be shown, fixed or reset: only a download refuses it.
func (s *Service) loadConfig() (config.Result, error) {
	res, err := config.Load(config.Options{File: s.cfg.File, Environ: s.cfg.Environ, Flags: s.cfg.Flags, Force: true})
	if err != nil {
		return config.Result{}, configError(err)
	}
	return res, nil
}

// ConfigGet returns one effective value with its source.
func (s *Service) ConfigGet(key string) (Setting, error) {
	res, err := s.loadConfig()
	if err != nil {
		return Setting{}, err
	}
	st, gerr := res.Get(key)
	return st, configError(gerr)
}

// ConfigList returns every effective value.
func (s *Service) ConfigList() ([]Setting, error) {
	res, err := s.loadConfig()
	if err != nil {
		return nil, err
	}
	return res.Settings(), nil
}

// ConfigSet validates and writes key=value to the config file atomically.
func (s *Service) ConfigSet(key, value string) error {
	if s.cfg.File == "" {
		return newError(CodeInvalidConfig, "there is no config file path")
	}
	if key == "download.on_exists" && strings.EqualFold(strings.TrimSpace(value), "overwrite") {
		return &Error{Code: CodeInvalidConfig, Message: "download.on_exists=overwrite cannot be stored: it is only honored with --force on the command line", Args: map[string]any{"key": key, "value": "overwrite", "source": "config set"}}
	}
	return configError(config.Set(s.cfg.File, key, value))
}

// ConfigReset removes a key (or "all") from the config file.
func (s *Service) ConfigReset(key string) error {
	if s.cfg.File == "" {
		return newError(CodeInvalidConfig, "there is no config file path")
	}
	return configError(config.Reset(s.cfg.File, key))
}

// configError maps a config.Error to the stable codes: an unknown key is a usage error, any other
// bad value or file is invalid_config (both exit 2).
func configError(err error) error {
	if err == nil {
		return nil
	}
	var ce *config.Error
	if errors.As(err, &ce) {
		return &Error{Code: ce.Code(), Message: ce.Reason, Cause: err, Args: map[string]any{"key": ce.Key, "value": ce.Value, "source": string(ce.Source), "detail": ce.Reason}}
	}
	return wrap(err)
}

// ActiveProfile is the profile resolved at startup.
func (s *Service) ActiveProfile() ProfileInfo {
	s.profMu.RLock()
	defer s.profMu.RUnlock()
	return s.profile
}

// DetectProfile re-runs detection for `profile detect`.
func (s *Service) DetectProfile(ctx context.Context) (ProfileInfo, error) {
	if s.detect == nil {
		return s.ActiveProfile(), nil
	}
	info, err := s.detect(ctx)
	if err != nil {
		return ProfileInfo{}, wrap(err)
	}
	s.profMu.Lock()
	s.profile = info
	s.profMu.Unlock()
	return info, nil
}
