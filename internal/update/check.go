package update

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CheckResult is safe to display; Notes are available only after Apply verifies an archive.
type CheckResult struct {
	Current   string    `json:"current"`
	Latest    string    `json:"latest"`
	Channel   string    `json:"channel"`
	PageURL   string    `json:"page_url"`
	Available bool      `json:"available"`
	Notify    bool      `json:"-"`
	CheckedAt time.Time `json:"checked_at"`
	FromCache bool      `json:"-"`
	Skipped   string    `json:"-"`
}
type checkCache struct {
	SchemaVersion   int       `json:"schema_version"`
	CheckedAt       time.Time `json:"checked_at"`
	Channel         string    `json:"channel"`
	Current         string    `json:"current"`
	Latest          string    `json:"latest"`
	NotifiedVersion string    `json:"notified_version"`
	NotifiedAt      time.Time `json:"notified_at"`
}

// SkipReason centralizes the opt-out rules. An empty result enables automatic checks.
func (u *Updater) SkipReason(enabled bool, getenv func(string) string) string {
	if !enabled {
		return "config"
	}
	if getenv == nil {
		getenv = u.o.Getenv
	}
	v := getenv("MOTHDEX_NO_UPDATE_CHECK")
	if v != "" && v != "0" && v != "false" {
		return "env"
	}
	if getenv("CI") != "" {
		return "ci"
	}
	if !validVersion(u.o.Current) {
		return "dev_build"
	}
	if _, err := u.Managed(); err != nil {
		return "unmanaged"
	}
	return ""
}

// LogSkipped records automatic checks that deliberately open no connection.
func (u *Updater) LogSkipped(reason string) {
	u.o.Log.Info("update check", "current", u.o.Current, "latest", "", "channel", u.o.Channel, "result", "skipped:"+reason, "duration_ms", 0)
}
func (u *Updater) readCache() (checkCache, bool) {
	var c checkCache
	b, err := readBounded(filepath.Join(u.o.DataDir, "update-check.json"), 16<<10)
	if err != nil || decode(b, &c, true) != nil || c.SchemaVersion != 1 || !validVersion(c.Current) || !validVersion(c.Latest) || (c.NotifiedVersion != "" && !validVersion(c.NotifiedVersion)) || (c.Channel != "stable" && c.Channel != "beta") || c.CheckedAt.IsZero() {
		return checkCache{}, false
	}
	return c, true
}
func (u *Updater) writeCache(c checkCache) {
	if !filepath.IsAbs(u.o.DataDir) {
		return
	}
	if err := os.MkdirAll(u.o.DataDir, 0700); err != nil {
		return
	}
	st, err := os.Lstat(u.o.DataDir)
	if err != nil || !st.IsDir() || isReparse(st) || st.Mode()&os.ModeSymlink != 0 {
		return
	}
	if err = atomicJSON(filepath.Join(u.o.DataDir, "update-check.json"), c, 0600); err != nil {
		u.o.Log.Warn("update cache write failed")
	}
}

// Check is cache-aware; force is used by explicit upgrade --check and always rediscovers.
func (u *Updater) Check(ctx context.Context, force bool) (result CheckResult, err error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	start := u.o.Now()
	result = CheckResult{Current: u.o.Current, Channel: u.o.Channel}
	defer func() {
		status := "current"
		if err != nil {
			status = "failed"
		} else if result.Available {
			status = "available"
		}
		u.o.Log.Info("update check", "current", u.o.Current, "latest", result.Latest, "channel", u.o.Channel, "result", status, "duration_ms", u.o.Now().Sub(start).Milliseconds())
	}()
	if _, err = u.Managed(); err != nil {
		return result, err
	}
	c, ok := u.readCache()
	now := u.o.Now()
	fresh := ok && c.Current == u.o.Current && c.Channel == u.o.Channel && !c.CheckedAt.After(now) && now.Sub(c.CheckedAt) < 24*time.Hour
	if !force && fresh {
		result.FromCache = true
	} else {
		release, e := u.Latest(ctx)
		if e != nil {
			return result, e
		}
		c = checkCache{SchemaVersion: 1, Current: u.o.Current, Channel: u.o.Channel, Latest: release.Version, CheckedAt: now, NotifiedVersion: c.NotifiedVersion, NotifiedAt: c.NotifiedAt}
		u.writeCache(c)
	}
	result.Latest = c.Latest
	result.CheckedAt = c.CheckedAt
	result.PageURL = origin + "/tag/v" + c.Latest
	result.Available = compare(c.Latest, u.o.Current) > 0
	result.Notify = result.Available && (c.NotifiedVersion != c.Latest || c.NotifiedAt.After(now) || now.Sub(c.NotifiedAt) >= 24*time.Hour)
	return result, nil
}

// Cached returns a validated hint without performing network I/O, for shell/TUI banners.
func (u *Updater) Cached() CheckResult {
	c, ok := u.readCache()
	if !ok || c.Current != u.o.Current || c.Channel != u.o.Channel {
		return CheckResult{}
	}
	now := u.o.Now()
	if c.CheckedAt.After(now) || now.Sub(c.CheckedAt) >= 24*time.Hour {
		return CheckResult{}
	}
	available := compare(c.Latest, u.o.Current) > 0
	return CheckResult{Current: c.Current, Latest: c.Latest, Channel: c.Channel, PageURL: origin + "/tag/v" + c.Latest, Available: available, Notify: available && (c.NotifiedVersion != c.Latest || c.NotifiedAt.After(now) || now.Sub(c.NotifiedAt) >= 24*time.Hour), CheckedAt: c.CheckedAt, FromCache: true}
}

// MarkNotified records an actual displayed notice, never a background discovery alone.
func (u *Updater) MarkNotified(latest string) {
	if !validVersion(latest) {
		return
	}
	c, ok := u.readCache()
	if !ok || c.Latest != latest || c.Current != u.o.Current || c.Channel != u.o.Channel {
		return
	}
	c.NotifiedAt = u.o.Now()
	c.NotifiedVersion = latest
	u.writeCache(c)
}

// NoticeArgs exposes only validated strings, with the invoked alias substituted by the caller.
func (c CheckResult) NoticeArgs(name string) map[string]any {
	return map[string]any{"current": c.Current, "latest": c.Latest, "command": strings.TrimSuffix(filepath.Base(name), ".exe")}
}
