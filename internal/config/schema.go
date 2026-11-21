package config

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	minute = time.Minute
	hour   = time.Hour
)

// schema is the closed list of keys, in the order of config.md section 2. It is the single
// source for defaults, validation, environment names and `config list` order.
var schema = []setting{
	text("api.base_url", "https://api.mothdex.net/v1", func(c *Config) *string { return &c.API.BaseURL }, httpURL),
	duration("api.timeout", 15*time.Second, time.Second, 5*minute, func(c *Config) *time.Duration { return &c.API.Timeout }),
	rate("api.rate_limit", 5, 1, 10, func(c *Config) *float64 { return &c.API.RateLimit }),
	integer("api.max_retries", 2, 0, 5, func(c *Config) *int { return &c.API.MaxRetries }),

	text("download.dir", "", func(c *Config) *string { return &c.Download.Dir }, pathLike),
	integer("download.concurrency", 2, 1, 6, func(c *Config) *int { return &c.Download.Concurrency }),
	integer("download.max_attempts", 5, 1, 10, func(c *Config) *int { return &c.Download.MaxAttempts }),
	integer("download.not_found_retries", 3, 0, 5, func(c *Config) *int { return &c.Download.NotFoundRetries }),
	duration("download.first_byte_timeout", 10*minute, 30*time.Second, 15*minute, func(c *Config) *time.Duration { return &c.Download.FirstByteTimeout }),
	duration("download.idle_timeout", 5*minute, 30*time.Second, 15*minute, func(c *Config) *time.Duration { return &c.Download.IdleTimeout }),
	oneOf("download.on_exists", "skip", func(c *Config) *string { return &c.Download.OnExists }, "skip", "rename", "overwrite"),
	size("download.max_file_size", 256*gib, func(c *Config) *int64 { return &c.Download.MaxFileSize }),
	boolean("download.verify", true, func(c *Config) *bool { return &c.Download.Verify }),
	boolean("download.boxart", true, func(c *Config) *bool { return &c.Download.Boxart }),
	list("download.allowed_hosts", []string{"proxy.mothdex.net"}, func(c *Config) *[]string { return &c.Download.AllowedHosts }, hostItem),
	list("download.prefer.languages", nil, func(c *Config) *[]string { return &c.Download.Prefer.Languages }, freeItem),
	list("download.prefer.formats", nil, func(c *Config) *[]string { return &c.Download.Prefer.Formats }, freeItem),
	list("download.prefer.sources", nil, func(c *Config) *[]string { return &c.Download.Prefer.Sources }, freeItem),
	oneOf("download.prefer.size", "none", func(c *Config) *string { return &c.Download.Prefer.Size }, "none", "smallest", "largest"),

	boolean("cache.enabled", true, func(c *Config) *bool { return &c.Cache.Enabled }),
	integer("cache.max_size_mb", 200, 10, 10240, func(c *Config) *int { return &c.Cache.MaxSizeMB }),
	ttl("cache.ttl.search", 24*hour, func(c *Config) *time.Duration { return &c.Cache.TTL.Search }),
	ttl("cache.ttl.entry", 24*hour, func(c *Config) *time.Duration { return &c.Cache.TTL.Entry }),
	ttl("cache.ttl.meta", 168*hour, func(c *Config) *time.Duration { return &c.Cache.TTL.Meta }),
	ttl("cache.ttl.info", hour, func(c *Config) *time.Duration { return &c.Cache.TTL.Info }),
	ttl("cache.ttl.covers", 720*hour, func(c *Config) *time.Duration { return &c.Cache.TTL.Covers }),

	text("paths.temp", "", func(c *Config) *string { return &c.Paths.Temp }, pathLike),
	text("paths.data", "", func(c *Config) *string { return &c.Paths.Data }, pathLike),
	text("paths.logs", "", func(c *Config) *string { return &c.Paths.Logs }, pathLike),

	language("interface.language", func(c *Config) *string { return &c.Interface.Language }),
	oneOf("interface.color", "auto", func(c *Config) *string { return &c.Interface.Color }, "auto", "always", "never"),
	boolean("interface.confirm_downloads", true, func(c *Config) *bool { return &c.Interface.ConfirmDownloads }),
	integer("interface.page_size", 10, 1, 100, func(c *Config) *int { return &c.Interface.PageSize }),

	oneOf("profile.id", "auto", func(c *Config) *string { return &c.Profile.ID }, profileIDs...),
	text("profile.rom_root", "", func(c *Config) *string { return &c.Profile.ROMRoot }, pathLike),
	text("profile.retrobat_path", "", func(c *Config) *string { return &c.Profile.RetroBatPath }, pathLike),
	boolean("profile.refresh", true, func(c *Config) *bool { return &c.Profile.Refresh }),

	oneOf("log.level", "info", func(c *Config) *string { return &c.Log.Level }, "debug", "info", "warn", "error"),
	boolean("log.file", true, func(c *Config) *bool { return &c.Log.File }),
	boolean("log.console", false, func(c *Config) *bool { return &c.Log.Console }),
	integer("log.max_size_mb", 10, 1, 1024, func(c *Config) *int { return &c.Log.MaxSizeMB }),
	integer("log.max_files", 5, 1, 100, func(c *Config) *int { return &c.Log.MaxFiles }),

	oneOf("gui.confirm_button", "south", func(c *Config) *string { return &c.GUI.ConfirmButton }, "south", "east"),
	oneOf("gui.text_size", "normal", func(c *Config) *string { return &c.GUI.TextSize }, "normal", "large"),
	safeArea("gui.safe_area", func(c *Config) *string { return &c.GUI.SafeArea }),
}

// safeArea accepts auto or a whole percentage 0..10, written as a number or as text in the file.
func safeArea(k string, ptr func(*Config) *string) setting {
	allowed := append([]string{"auto"}, percents...)
	return spec[string]{k: k, def: "auto", ptr: ptr, meta: SettingSpec{Kind: KindEnum, Enum: allowed}, parser: func(raw any) (string, error) {
		if n, ok := raw.(int); ok {
			raw = strconv.Itoa(n)
		}
		v, err := asString(raw)
		if err != nil {
			return "", err
		}
		if v = strings.TrimSpace(v); !slices.Contains(allowed, v) {
			return "", fmt.Errorf("must be auto or a number from 0 to 10")
		}
		return v, nil
	}}
}

// percents are the safe-area insets the GUI accepts: 0 to 10 percent of the screen.
var percents = []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10"}

var profileIDs = []string{"auto", "batocera", "emuelec", "recalbox", "lakka", "retrobat", "emudeck", "retrodeck", "generic"}

func ttl(k string, def time.Duration, ptr func(*Config) *time.Duration) setting {
	return duration(k, def, time.Second, 8760*hour, ptr)
}

// language normalizes to lower case ("pt_BR" and "pt_br" are the same value).
func language(k string, ptr func(*Config) *string) setting {
	return spec[string]{k: k, def: "auto", ptr: ptr, meta: SettingSpec{Kind: KindString}, parser: func(raw any) (string, error) {
		s, err := asString(raw)
		if err != nil {
			return "", err
		}
		s = strings.ToLower(strings.TrimSpace(s))
		return s, languageCheck(s)
	}}
}

// index maps key -> setting; built once from schema.
var index = func() map[string]setting {
	m := make(map[string]setting, len(schema))
	for _, s := range schema {
		m[s.key()] = s
	}
	return m
}()

// lookup resolves a schema key or a dynamic profile.folders.<id> key.
func lookup(key string) (setting, bool) {
	if s, ok := index[key]; ok {
		return s, true
	}
	if id, ok := strings.CutPrefix(key, folderPrefix); ok && platformIDPattern.MatchString(id) {
		return folderSetting{id: id}, true
	}
	return nil, false
}

// isGroup reports whether key is a prefix of schema keys (for example "cache.ttl"), which
// reset accepts to clear a whole section.
func isGroup(key string) bool {
	if key == strings.TrimSuffix(folderPrefix, ".") {
		return true
	}
	for _, s := range schema {
		if strings.HasPrefix(s.key(), key+".") {
			return true
		}
	}
	return false
}
