package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Or4cu1o/mothdex-cli/internal/cache"
	"github.com/Or4cu1o/mothdex-cli/internal/history"
	"github.com/Or4cu1o/mothdex-cli/internal/library"
)

// Crivo (PR 32): a file that holds on_exists=overwrite must still be readable and fixable.
func TestConfigReadsAFileHoldingOverwriteAndCanFixIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("version: 1\ndownload:\n  on_exists: overwrite\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := newSvc(t, &fakeClient{}, func(o *Options) { o.Config = ConfigSource{File: path} })

	got, err := s.ConfigGet("download.on_exists")
	if err != nil || got.Value != "overwrite" || got.Source != "file" {
		t.Fatalf("get: %+v %v", got, err)
	}
	if list, err := s.ConfigList(); err != nil || len(list) == 0 {
		t.Fatalf("list: %v", err)
	}
	if err := s.ConfigSet("download.on_exists", "rename"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ConfigGet("download.on_exists"); got.Value != "rename" {
		t.Fatalf("after set: %+v", got)
	}
	if err := s.ConfigReset("all"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ConfigGet("download.on_exists"); got.Source != "default" {
		t.Fatalf("after reset: %+v", got)
	}
}

func TestConfigErrorsAreUsageOrInvalidConfig(t *testing.T) {
	s := newSvc(t, &fakeClient{}, func(o *Options) { o.Config = ConfigSource{File: filepath.Join(t.TempDir(), "c.yaml")} })
	if _, err := s.ConfigGet("nope.key"); code(err) != CodeUsage {
		t.Fatalf("unknown key: %v", err)
	}
	if err := s.ConfigSet("cache.max_size_mb", "9999999"); code(err) != CodeInvalidConfig {
		t.Fatalf("bad value: %v", err)
	}
	var e *Error
	if err := s.ConfigSet("nope", "1"); !errors.As(err, &e) || e.ExitCode() != 2 {
		t.Fatalf("exit code: %v", err)
	}
	if err := newSvc(t, &fakeClient{}).ConfigSet("a", "b"); code(err) != CodeInvalidConfig {
		t.Fatal("no config path")
	}
}

func TestHistoryCacheAndProfileCapabilities(t *testing.T) {
	hs, _ := history.Open(filepath.Join(t.TempDir(), "h.jsonl"))
	_ = hs.Append(history.Record{ID: "1", State: "done"})
	_ = hs.Append(history.Record{ID: "2", State: "failed"})
	info := library.ProfileInfo{ID: "generic"}
	s := newSvc(t, &fakeClient{}, func(o *Options) {
		o.History, o.Profile = hs, info
		o.Detect = func(context.Context) (library.ProfileInfo, error) { return library.ProfileInfo{ID: "retrobat"}, nil }
	})
	if got, err := s.History(context.Background(), history.Filter{Failed: true}); err != nil || len(got) != 1 || got[0].ID != "2" {
		t.Fatalf("history: %v %v", got, err)
	}
	_ = s.o.Cache.PutJSON(cache.NSInfo, "k", 1)
	if st, err := s.CacheStats(); err != nil || len(st.Namespaces) == 0 {
		t.Fatalf("stats: %+v %v", st, err)
	}
	if err := s.CacheClear("info"); err != nil {
		t.Fatal(err)
	}
	if err := s.CacheClear("nope"); code(err) != CodeInvalidRequest {
		t.Fatalf("unknown namespace: %v", err)
	}
	if s.ActiveProfile().ID != "generic" {
		t.Fatal("active profile")
	}
	if got, _ := s.DetectProfile(context.Background()); got.ID != "retrobat" || s.ActiveProfile().ID != "retrobat" {
		t.Fatal("detect")
	}
}
