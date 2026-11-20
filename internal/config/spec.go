package config

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// setting is one schema key. parse turns a raw layer value into the canonical Go value;
// out renders it for get/list and for the YAML file (durations and sizes as strings).
type setting interface {
	key() string
	noEnv() bool
	defaultValue() any
	parse(raw any) (any, error)
	bind(c *Config, v any)
	out(v any) any
}

// spec is the generic implementation: ptr points at the Config field of the key.
type spec[T any] struct {
	k       string
	def     T
	parser  func(any) (T, error)
	ptr     func(*Config) *T
	render  func(T) any
	skipEnv bool
}

func (s spec[T]) key() string { return s.k }
func (s spec[T]) noEnv() bool { return s.skipEnv }

func (s spec[T]) defaultValue() any {
	v, err := s.parser(s.def)
	if err != nil {
		panic(fmt.Sprintf("config: invalid default for %s: %v", s.k, err)) // table bug, caught by tests
	}
	return v
}

func (s spec[T]) parse(raw any) (any, error) { return s.parser(raw) }

func (s spec[T]) bind(c *Config, v any) { *s.ptr(c) = v.(T) }

func (s spec[T]) out(v any) any {
	if s.render != nil {
		return s.render(v.(T))
	}
	return v
}

// --- constructors -------------------------------------------------------------------------

func text(k, def string, ptr func(*Config) *string, checks ...func(string) error) setting {
	return spec[string]{k: k, def: def, ptr: ptr, parser: func(raw any) (string, error) {
		s, err := asString(raw)
		if err != nil {
			return "", err
		}
		return s, runChecks(s, checks)
	}}
}

func oneOf(k, def string, ptr func(*Config) *string, allowed ...string) setting {
	return text(k, def, ptr, func(s string) error {
		for _, a := range allowed {
			if s == a {
				return nil
			}
		}
		return fmt.Errorf("must be one of: %s", strings.Join(allowed, ", "))
	})
}

func integer(k string, def, lo, hi int, ptr func(*Config) *int) setting {
	return spec[int]{k: k, def: def, ptr: ptr, parser: func(raw any) (int, error) {
		n, err := asInt(raw)
		if err != nil {
			return 0, err
		}
		if n < lo || n > hi {
			return 0, fmt.Errorf("must be between %d and %d", lo, hi)
		}
		return n, nil
	}}
}

// rate is the only clamped numeric key (contract: "clamped 1..10").
func rate(k string, def, lo, hi float64, ptr func(*Config) *float64) setting {
	return spec[float64]{k: k, def: def, ptr: ptr, parser: func(raw any) (float64, error) {
		f, err := asFloat(raw)
		return min(max(f, lo), hi), err
	}}
}

func boolean(k string, def bool, ptr func(*Config) *bool) setting {
	return spec[bool]{k: k, def: def, ptr: ptr, parser: asBool}
}

func duration(k string, def, lo, hi time.Duration, ptr func(*Config) *time.Duration) setting {
	return spec[time.Duration]{k: k, def: def, ptr: ptr,
		render: func(d time.Duration) any { return d.String() },
		parser: func(raw any) (time.Duration, error) {
			d, err := asDuration(raw)
			if err != nil {
				return 0, err
			}
			if d < lo || d > hi {
				return 0, fmt.Errorf("must be between %s and %s", lo, hi)
			}
			return d, nil
		}}
}

func size(k string, def int64, ptr func(*Config) *int64) setting {
	return spec[int64]{k: k, def: def, ptr: ptr,
		render: func(n int64) any { return formatSize(n) },
		parser: func(raw any) (int64, error) {
			n, err := asSize(raw)
			if err == nil && n <= 0 {
				err = fmt.Errorf("must be greater than zero")
			}
			return n, err
		}}
}

func list(k string, def []string, ptr func(*Config) *[]string, item func(string) (string, error)) setting {
	return spec[[]string]{k: k, def: def, ptr: ptr, parser: func(raw any) ([]string, error) {
		items, err := asList(raw)
		if err != nil {
			return nil, err
		}
		for i, it := range items {
			if items[i], err = item(it); err != nil {
				return nil, fmt.Errorf("%q: %w", it, err)
			}
		}
		return items, nil
	}}
}

func runChecks(s string, checks []func(string) error) error {
	for _, c := range checks {
		if err := c(s); err != nil {
			return err
		}
	}
	return nil
}

// --- value checks ----------------------------------------------------------------------------

const maxPathLen = 4096

func pathLike(s string) error {
	if len(s) > maxPathLen {
		return fmt.Errorf("longer than %d bytes", maxPathLen)
	}
	return nil
}

func httpURL(s string) error {
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return fmt.Errorf("must be an http(s) URL with a host")
	}
	return nil
}

var languagePattern = regexp.MustCompile(`^[a-z]{2,3}(_[a-z]{2})?$`)

func languageCheck(s string) error {
	if strings.EqualFold(s, "auto") || languagePattern.MatchString(strings.ToLower(s)) {
		return nil
	}
	return fmt.Errorf("must be auto or a locale such as pt_BR")
}

var hostPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?(:[0-9]{1,5})?$`)

func hostItem(s string) (string, error) {
	h := strings.ToLower(s)
	if !hostPattern.MatchString(h) {
		return "", fmt.Errorf("must be a bare host name (no scheme or path)")
	}
	return h, nil
}

func freeItem(s string) (string, error) {
	if len(s) > 64 {
		return "", fmt.Errorf("longer than 64 bytes")
	}
	return s, nil
}

// Platform ids follow downloader.md section 6.2.
var platformIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// launcherFolders are frontend script folders a download must never land in (profiles.md CR-3).
var launcherFolders = []string{"ports", "ports_scripts", "windows"}

// folderName accepts a single local path component that is not a launcher folder.
func folderName(s string) error {
	if s == "" || strings.ContainsAny(s, `/\`) || !filepath.IsLocal(s) || s == "." || s == ".." {
		return fmt.Errorf("must be a single local folder name")
	}
	for _, l := range launcherFolders {
		if strings.EqualFold(s, l) {
			return fmt.Errorf("%q is a launcher folder and cannot receive downloads", s)
		}
	}
	return nil
}

// folderSetting is the dynamic key profile.folders.<platform id>.
type folderSetting struct{ id string }

func (f folderSetting) key() string     { return folderPrefix + f.id }
func (folderSetting) noEnv() bool       { return true }
func (folderSetting) defaultValue() any { return "" }
func (folderSetting) out(v any) any     { return v }
func (folderSetting) parse(raw any) (any, error) {
	s, err := asString(raw)
	if err != nil {
		return nil, err
	}
	s = strings.TrimSpace(s)
	return s, folderName(s)
}

func (f folderSetting) bind(c *Config, v any) {
	if c.Profile.Folders == nil {
		c.Profile.Folders = map[string]string{}
	}
	c.Profile.Folders[f.id] = v.(string)
}

const folderPrefix = "profile.folders."
