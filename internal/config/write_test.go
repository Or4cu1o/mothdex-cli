package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func readText(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // G304: path under t.TempDir
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func TestSetCreatesFileAndRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.yaml")
	steps := [][2]string{
		{"download.concurrency", "4"},
		{"api.timeout", "30s"},
		{"download.max_file_size", "1GiB"},
		{"download.prefer.languages", "En, Pt"},
		{"profile.folders.md", "megadrive"},
		{"interface.language", "PT_BR"},
	}
	for _, s := range steps {
		if err := Set(path, s[0], s[1]); err != nil {
			t.Fatalf("Set %s: %v", s[0], err)
		}
	}
	if !strings.Contains(readText(t, path), "version: 1") {
		t.Fatal("written file lacks the schema version")
	}
	r := mustLoad(t, Options{File: path})
	c := r.Config
	if c.Download.Concurrency != 4 || c.API.Timeout != 30*time.Second || c.Download.MaxFileSize != 1<<30 ||
		c.Interface.Language != "pt_br" || c.Profile.Folders["md"] != "megadrive" || len(c.Download.Prefer.Languages) != 2 {
		t.Fatalf("round trip lost values: %+v", c)
	}
	if s, _ := r.Get("api.timeout"); s.Source != SourceFile {
		t.Fatalf("source %v", s.Source)
	}
	if len(r.Warnings) != 0 {
		t.Fatalf("warnings %v", r.Warnings)
	}
}

func TestSetRejectsBadInputWithoutTouchingTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Set(path, "download.concurrency", "3"); err != nil {
		t.Fatal(err)
	}
	before := readText(t, path)
	tests := []struct {
		key, val string
		kind     ErrorKind
	}{
		{"download.concurrency", "99", KindInvalidValue},
		{"download.concurrency", "x", KindInvalidValue},
		{"nope.key", "1", KindUnknownKey},
		{"profile.folders.Bad Id", "x", KindUnknownKey},
		{"profile.folders.md", "../up", KindInvalidValue},
		{"profile.folders", "x", KindUnknownKey},
	}
	for _, tt := range tests {
		var e *Error
		if err := Set(path, tt.key, tt.val); !errors.As(err, &e) || e.Kind != tt.kind {
			t.Errorf("%s=%s: %v", tt.key, tt.val, err)
		}
	}
	if readText(t, path) != before {
		t.Fatal("a rejected set changed the file")
	}
}

func TestSetKeepsOtherAndUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("# comment\nversion: 1\nfuture:\n  flag: true\nlog:\n  level: debug\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Set(path, "log.console", "true"); err != nil {
		t.Fatal(err)
	}
	text := readText(t, path)
	for _, want := range []string{"level: debug", "console: true", "flag: true"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
}

func TestSetRefusesToOverwriteBrokenOrNewerFiles(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{"broken": "a: [unclosed\n", "newer": "version: 9\n"} {
		path := filepath.Join(dir, name+".yaml")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := Set(path, "log.level", "warn"); err == nil {
			t.Errorf("%s: want an error", name)
		}
		if readText(t, path) != content {
			t.Errorf("%s: file was modified", name)
		}
	}
}

func TestReset(t *testing.T) {
	setup := func(t *testing.T) string {
		path := filepath.Join(t.TempDir(), "config.yaml")
		for k, v := range map[string]string{
			"download.concurrency": "4", "cache.ttl.search": "2h", "cache.ttl.entry": "3h",
			"profile.folders.md": "megadrive", "log.level": "warn",
		} {
			if err := Set(path, k, v); err != nil {
				t.Fatal(err)
			}
		}
		return path
	}
	tests := []struct {
		key   string
		check func(Config) bool
	}{
		{"download.concurrency", func(c Config) bool { return c.Download.Concurrency == 2 && c.Log.Level == "warn" }},
		{"cache.ttl", func(c Config) bool {
			return c.Cache.TTL.Search == 24*time.Hour && c.Cache.TTL.Entry == 24*time.Hour && c.Log.Level == "warn"
		}},
		{"profile.folders", func(c Config) bool { return len(c.Profile.Folders) == 0 && c.Log.Level == "warn" }},
		{"profile.folders.md", func(c Config) bool { return len(c.Profile.Folders) == 0 }},
		{ResetAll, func(c Config) bool {
			return c.Log.Level == "info" && c.Download.Concurrency == 2 && c.Cache.TTL.Search == 24*time.Hour
		}},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			path := setup(t)
			if err := Reset(path, tt.key); err != nil {
				t.Fatalf("Reset: %v", err)
			}
			if c := mustLoad(t, Options{File: path}).Config; !tt.check(c) {
				t.Fatalf("unexpected config after reset: %+v", c)
			}
		})
	}
}

func TestResetUnknownKeyAndMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	var e *Error
	if err := Reset(path, "nope"); !errors.As(err, &e) || e.Kind != KindUnknownKey {
		t.Fatalf("got %v", err)
	}
	if err := Reset(path, ResetAll); err != nil {
		t.Fatalf("reset all on a missing file: %v", err)
	}
	if got := readText(t, path); strings.TrimSpace(got) != "version: 1" {
		t.Fatalf("file after reset all: %q", got)
	}
}

func TestAtomicWriteLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := Set(path, "log.level", "warn"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "config.yaml" {
		t.Fatalf("directory contents: %v, %v", entries, err)
	}
}

// Alicerce s2: the refusal of a stored overwrite lives in Set, so every caller gets it; the file is untouched.
func TestSetRefusesAStoredOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	for _, v := range []string{"overwrite", " Overwrite "} {
		err := Set(path, "download.on_exists", v)
		var ce *Error
		if !errors.As(err, &ce) || ce.Kind != KindInvalidValue || ce.Key != "download.on_exists" {
			t.Fatalf("%q: %v", v, err)
		}
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("a refused value must not create the file")
	}
	if err := Set(path, "download.on_exists", "rename"); err != nil {
		t.Fatal(err)
	}
}
