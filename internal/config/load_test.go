package config

import (
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"time"
)

func memFile(content string) func(string) ([]byte, error) {
	return func(string) ([]byte, error) { return []byte(content), nil }
}

func mustLoad(t *testing.T, o Options) Result {
	t.Helper()
	r, err := Load(o)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return r
}

func TestDefaultsMatchContract(t *testing.T) {
	c := mustLoad(t, Options{}).Config
	if c.API.BaseURL != "https://api.mothdex.net/v1" || c.API.Timeout != 15*time.Second || c.API.RateLimit != 5 || c.API.MaxRetries != 2 {
		t.Errorf("api defaults: %+v", c.API)
	}
	d := c.Download
	if d.Concurrency != 2 || d.MaxAttempts != 5 || d.NotFoundRetries != 3 || d.OnExists != "skip" || !d.Verify || !d.Boxart ||
		d.FirstByteTimeout != 10*time.Minute || d.IdleTimeout != 5*time.Minute || d.MaxFileSize != 256<<30 ||
		d.Prefer.Size != "none" || !reflect.DeepEqual(d.AllowedHosts, []string{"proxy.mothdex.net"}) {
		t.Errorf("download defaults: %+v", d)
	}
	if c.Cache.MaxSizeMB != 200 || c.Cache.TTL.Meta != 168*time.Hour || c.Cache.TTL.Covers != 720*time.Hour || c.Cache.TTL.Info != time.Hour {
		t.Errorf("cache defaults: %+v", c.Cache)
	}
	if c.Interface.Language != "auto" || c.Interface.PageSize != 10 || c.Profile.ID != "auto" || !c.Profile.Refresh ||
		c.Log.Level != "info" || !c.Log.File || c.Log.Console || c.Log.MaxFiles != 5 {
		t.Errorf("other defaults: %+v %+v %+v", c.Interface, c.Profile, c.Log)
	}
}

func TestSchemaDefaultsRoundTrip(t *testing.T) {
	for _, s := range schema {
		def := s.defaultValue()
		again, err := s.parse(s.out(def))
		if err != nil || !reflect.DeepEqual(again, def) {
			t.Errorf("%s: default %v does not survive out/parse (%v, %v)", s.key(), def, again, err)
		}
	}
}

func TestEnvNamesAreUnique(t *testing.T) {
	seen := map[string]string{}
	for _, s := range schema {
		if other, dup := seen[envName(s.key())]; dup {
			t.Errorf("%s and %s share %s", s.key(), other, envName(s.key()))
		}
		seen[envName(s.key())] = s.key()
	}
	if envName("download.first_byte_timeout") != "MOTHDEX_DOWNLOAD_FIRST_BYTE_TIMEOUT" || envName("cache.ttl.search") != "MOTHDEX_CACHE_TTL_SEARCH" {
		t.Error("contract examples do not match")
	}
}

func TestPrecedencePerKey(t *testing.T) {
	file := "download:\n  concurrency: 3\n  max_attempts: 7\ninterface:\n  page_size: 20\n"
	tests := []struct {
		name    string
		env     []string
		flags   map[string]string
		want    int
		wantSrc Source
	}{
		{"file beats default", nil, nil, 3, SourceFile},
		{"env beats file", []string{"MOTHDEX_DOWNLOAD_CONCURRENCY=4"}, nil, 4, SourceEnv},
		{"flag beats env", []string{"MOTHDEX_DOWNLOAD_CONCURRENCY=4"}, map[string]string{"download.concurrency": "5"}, 5, SourceFlag},
		{"flag beats file", nil, map[string]string{"download.concurrency": "6"}, 6, SourceFlag},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := mustLoad(t, Options{File: "c.yaml", ReadFile: memFile(file), Environ: tt.env, Flags: tt.flags})
			got, _ := r.Get("download.concurrency")
			if got.Value != tt.want || got.Source != tt.wantSrc || r.Config.Download.Concurrency != tt.want {
				t.Fatalf("got %+v", got)
			}
			// Keys are merged one by one: untouched keys keep their own layer.
			if a, _ := r.Get("download.max_attempts"); a.Value != 7 || a.Source != SourceFile {
				t.Fatalf("max_attempts %+v", a)
			}
			if p, _ := r.Get("download.verify"); p.Source != SourceDefault {
				t.Fatalf("verify %+v", p)
			}
		})
	}
}

func TestValueErrorsCarryKeyValueAndSource(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		key  string
		src  Source
	}{
		{"file type", Options{File: "c", ReadFile: memFile("download:\n  concurrency: lots\n")}, "download.concurrency", SourceFile},
		{"file range", Options{File: "c", ReadFile: memFile("download:\n  concurrency: 9\n")}, "download.concurrency", SourceFile},
		{"env range", Options{Environ: []string{"MOTHDEX_API_MAX_RETRIES=6"}}, "api.max_retries", SourceEnv},
		{"env bool", Options{Environ: []string{"MOTHDEX_LOG_FILE=maybe"}}, "log.file", SourceEnv},
		{"flag enum", Options{Flags: map[string]string{"interface.color": "rainbow"}}, "interface.color", SourceFlag},
		{"duration", Options{Environ: []string{"MOTHDEX_API_TIMEOUT=15"}}, "api.timeout", SourceEnv},
		{"duration range", Options{Environ: []string{"MOTHDEX_DOWNLOAD_IDLE_TIMEOUT=1s"}}, "download.idle_timeout", SourceEnv},
		{"size", Options{Environ: []string{"MOTHDEX_DOWNLOAD_MAX_FILE_SIZE=12XB"}}, "download.max_file_size", SourceEnv},
		{"size zero", Options{Environ: []string{"MOTHDEX_DOWNLOAD_MAX_FILE_SIZE=0"}}, "download.max_file_size", SourceEnv},
		{"language", Options{Environ: []string{"MOTHDEX_INTERFACE_LANGUAGE=portuguese"}}, "interface.language", SourceEnv},
		{"url", Options{Environ: []string{"MOTHDEX_API_BASE_URL=ftp://x"}}, "api.base_url", SourceEnv},
		{"host", Options{Environ: []string{"MOTHDEX_DOWNLOAD_ALLOWED_HOSTS=https://evil.example/x"}}, "download.allowed_hosts", SourceEnv},
		{"profile id", Options{Flags: map[string]string{"profile.id": "amiga"}}, "profile.id", SourceFlag},
		{"folder launcher", Options{File: "c", ReadFile: memFile("profile:\n  folders:\n    md: ports\n")}, "profile.folders.md", SourceFile},
		{"folder separator", Options{File: "c", ReadFile: memFile("profile:\n  folders:\n    md: a/b\n")}, "profile.folders.md", SourceFile},
		{"folder dotdot", Options{File: "c", ReadFile: memFile("profile:\n  folders:\n    md: '..'\n")}, "profile.folders.md", SourceFile},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(tt.opts)
			var e *Error
			if !errors.As(err, &e) || e.Kind != KindInvalidValue || e.Key != tt.key || e.Source != tt.src || e.Value == "" {
				t.Fatalf("got %#v", err)
			}
			if e.ExitCode() != 2 || e.Code() != "invalid_config" {
				t.Fatalf("exit %d code %s", e.ExitCode(), e.Code())
			}
		})
	}
}

func TestValueParsing(t *testing.T) {
	r := mustLoad(t, Options{Environ: []string{
		"MOTHDEX_API_RATE_LIMIT=50",
		"MOTHDEX_DOWNLOAD_MAX_FILE_SIZE=512MiB",
		"MOTHDEX_DOWNLOAD_PREFER_LANGUAGES= En, Pt ,,",
		"MOTHDEX_DOWNLOAD_ALLOWED_HOSTS=Proxy.Mothdex.net,mirror.example:8443",
		"MOTHDEX_INTERFACE_LANGUAGE=PT_BR",
		"MOTHDEX_CACHE_TTL_SEARCH=90m",
	}})
	c := r.Config
	if c.API.RateLimit != 10 || c.Download.MaxFileSize != 512<<20 || c.Interface.Language != "pt_br" || c.Cache.TTL.Search != 90*time.Minute ||
		!reflect.DeepEqual(c.Download.Prefer.Languages, []string{"En", "Pt"}) ||
		!reflect.DeepEqual(c.Download.AllowedHosts, []string{"proxy.mothdex.net", "mirror.example:8443"}) {
		t.Fatalf("parsed: %+v", c)
	}
	if s, _ := r.Get("download.max_file_size"); s.Value != "512MiB" {
		t.Fatalf("size renders as %v", s.Value)
	}
	if s, _ := r.Get("cache.ttl.search"); s.Value != "1h30m0s" {
		t.Fatalf("duration renders as %v", s.Value)
	}
}

func TestFileLayerWarningsAndFolders(t *testing.T) {
	file := "version: 1\nfuture:\n  thing: 1\napi:\n  nope: 2\nprofile:\n  folders:\n    md: megadrive\n    ps1: psx\n"
	r := mustLoad(t, Options{File: "c", ReadFile: memFile(file), Environ: []string{"MOTHDEX_NOPE=1", "MOTHDEX_CONFIG=/x", "PATH=/bin"}})
	if got := r.Config.Profile.Folders; !reflect.DeepEqual(got, map[string]string{"md": "megadrive", "ps1": "psx"}) {
		t.Fatalf("folders %v", got)
	}
	warn := strings.Join(r.Warnings, "|")
	for _, want := range []string{`"future.thing"`, `"api.nope"`, "MOTHDEX_NOPE"} {
		if !strings.Contains(warn, want) {
			t.Errorf("missing warning for %s in %q", want, warn)
		}
	}
	if strings.Contains(warn, "MOTHDEX_CONFIG") || len(r.Warnings) != 3 {
		t.Fatalf("warnings %v", r.Warnings)
	}
	if s, _ := r.Get("profile.folders.md"); s.Source != SourceFile || s.Value != "megadrive" {
		t.Fatalf("folder setting %+v", s)
	}
}

func TestFileProblems(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		kind ErrorKind
	}{
		{"newer schema", Options{File: "c", ReadFile: memFile("version: 2\n")}, KindNewerSchema},
		{"bad version", Options{File: "c", ReadFile: memFile("version: zero\n")}, KindInvalidValue},
		{"invalid yaml", Options{File: "c", ReadFile: memFile("a: [unclosed\n")}, KindUnreadable},
		{"folders not a map", Options{File: "c", ReadFile: memFile("profile:\n  folders: nope\n")}, KindInvalidValue},
		{"unreadable", Options{File: "c", ReadFile: func(string) ([]byte, error) { return nil, fs.ErrPermission }}, KindUnreadable},
		{"unknown flag key", Options{Flags: map[string]string{"nope.nope": "1"}}, KindUnknownKey},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var e *Error
			if _, err := Load(tt.opts); !errors.As(err, &e) || e.Kind != tt.kind || e.ExitCode() != 2 {
				t.Fatalf("got %#v", err)
			}
		})
	}
}

func TestMissingOrEmptyFileIsFine(t *testing.T) {
	missing := func(string) ([]byte, error) { return nil, fs.ErrNotExist }
	for name, o := range map[string]Options{
		"missing": {File: "c", ReadFile: missing},
		"empty":   {File: "c", ReadFile: memFile("")},
		"no file": {},
	} {
		if r := mustLoad(t, o); len(r.Warnings) != 0 || r.Config.Download.Concurrency != 2 {
			t.Errorf("%s: %+v", name, r.Warnings)
		}
	}
}

func TestLoadsAreIndependent(t *testing.T) {
	a := mustLoad(t, Options{})
	a.Config.Download.AllowedHosts[0] = "evil.example"
	a.Config.Profile.Folders["x"] = "y"
	b := mustLoad(t, Options{})
	if b.Config.Download.AllowedHosts[0] != "proxy.mothdex.net" || len(b.Config.Profile.Folders) != 0 {
		t.Fatalf("state leaked between loads: %+v", b.Config.Download.AllowedHosts)
	}
}

func TestSettingsListsEveryKeyInSchemaOrder(t *testing.T) {
	list := mustLoad(t, Options{}).Settings()
	if len(list) != len(schema) {
		t.Fatalf("%d settings for %d keys", len(list), len(schema))
	}
	for i, s := range schema {
		if list[i].Key != s.key() || list[i].Source != SourceDefault {
			t.Fatalf("entry %d: %+v", i, list[i])
		}
	}
	if _, err := (Result{}).Get("nope"); err == nil {
		t.Fatal("unknown key must fail")
	}
}

func TestSizeParsingAndFormatting(t *testing.T) {
	ok := map[string]int64{"1": 1, "5B": 5, "2KiB": 2048, "3mib": 3 << 20, "256GiB": 256 << 30, "1 TiB": 1 << 40}
	for in, want := range ok {
		if got, err := parseSize(in); err != nil || got != want {
			t.Errorf("%q = %d, %v (want %d)", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "-1", "1.5GiB", "9999999999TiB", "GiB", "1KB"} {
		if _, err := parseSize(bad); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
	for n, want := range map[int64]string{256 << 30: "256GiB", 1536 << 20: "1536MiB", 1000: "1000B", 1 << 40: "1TiB"} {
		if got := formatSize(n); got != want {
			t.Errorf("formatSize(%d) = %s, want %s", n, got, want)
		}
	}
}
