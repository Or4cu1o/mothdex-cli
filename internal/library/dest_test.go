package library

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func resolved(root, os, layout, unmapped string, folders map[string]string) Profile {
	return Profile{OS: os, Roms: Roms{Root: root, Layout: layout, Unmapped: unmapped, Folders: folders},
		Launcher: Launcher{Kind: "sh", Dir: root + "/ports"}}
}

func TestDestDir(t *testing.T) {
	perSystem := resolved("/userdata/roms", "linux", LayoutPerSystem, UnmappedError, map[string]string{"md": "megadrive", "scd": "megacd"})
	tests := []struct {
		name    string
		p       Profile
		id      string
		want    string
		wantErr error
	}{
		{"mapped platform", perSystem, "md", "/userdata/roms/megadrive", nil},
		{"folder differs from id", perSystem, "scd", "/userdata/roms/megacd", nil},
		{"unmapped with error policy", perSystem, "mame", "", ErrUnmappedPlatform},
		{"unmapped falls back to the id", resolved("/r", "linux", LayoutPerSystem, UnmappedPlatformID, nil), "mame", "/r/mame", nil},
		{"per_platform_id layout", resolved("/storage/roms", "linux", LayoutPerPlatformID, "", nil), "snes", "/storage/roms/snes", nil},
		{"windows separator on any host", resolved(`C:\RetroBat\roms`, "windows", LayoutPerSystem, UnmappedError, map[string]string{"md": "megadrive"}), "md", `C:\RetroBat\roms\megadrive`, nil},
		{"trailing separator trimmed", resolved("/r/", "linux", LayoutPerPlatformID, "", nil), "gba", "/r/gba", nil},
		{"traversal id", perSystem, "../etc", "", ErrInvalidPlatform},
		{"separator in id", perSystem, "a/b", "", ErrInvalidPlatform},
		{"upper case id", perSystem, "MD", "", ErrInvalidPlatform},
		{"empty id", perSystem, "", "", ErrInvalidPlatform},
		{"id too long", perSystem, strings.Repeat("a", 33), "", ErrInvalidPlatform},
		{"no root", resolved("", "linux", LayoutPerPlatformID, "", nil), "gba", "", ErrNoRomRoot},
		{"poisoned folder map", resolved("/r", "linux", LayoutPerSystem, UnmappedError, map[string]string{"md": "../../etc"}), "md", "", nil},
		{"launcher folder in map", resolved("/r", "linux", LayoutPerSystem, UnmappedError, map[string]string{"md": "ports"}), "md", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.p.DestDir(tt.id)
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("want %v, got %v", tt.wantErr, err)
			}
			if tt.want == "" && tt.wantErr == nil && err == nil {
				t.Fatalf("a poisoned map must be refused, got %q", got)
			}
			if tt.want != "" && (err != nil || got != tt.want) {
				t.Fatalf("got %q, %v, want %q", got, err, tt.want)
			}
		})
	}
}

func TestDestDirNeverLeavesTheRoot(t *testing.T) {
	info := detect(t, testEnv(map[string]string{"usr/share/batocera/batocera.version": "40", "userdata/roms/.keep": ""}),
		Config{Folders: map[string]string{"md": "../escape", "gba": "ok"}})
	if _, err := info.Profile.DestDir("md"); err != nil {
		t.Fatalf("the rejected override must leave the profile folder in place: %v", err)
	}
	if dir, _ := info.Profile.DestDir("md"); !strings.HasPrefix(dir, "/userdata/roms/") || strings.Contains(dir, "..") {
		t.Fatalf("destination %q", dir)
	}
}

// doer answers with a fixed response and records the request.
type doer struct {
	status int
	body   io.Reader
	err    error
	req    *http.Request
}

func (d *doer) Do(req *http.Request) (*http.Response, error) {
	d.req = req
	if d.err != nil {
		return nil, d.err
	}
	return &http.Response{StatusCode: d.status, Body: io.NopCloser(d.body)}, nil
}

func hookProfile(kind, url string) Profile {
	return Profile{Hook: Hook{Kind: kind, URL: url, Timeout: RefreshTimeout}}
}

const reloadURL = "http://127.0.0.1:1234/reloadgames"

func TestRefresh(t *testing.T) {
	d := &doer{status: 200, body: strings.NewReader("ok")}
	if err := hookProfile(RefreshHTTPGet, reloadURL).Refresh(context.Background(), d); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if d.req.Method != http.MethodGet || d.req.URL.String() != reloadURL {
		t.Fatalf("request %s %s", d.req.Method, d.req.URL)
	}
	if dl, ok := d.req.Context().Deadline(); !ok || time.Until(dl) > RefreshTimeout {
		t.Fatal("the request must carry a 3 s deadline")
	}
}

func TestRefreshFailuresAreErrorsNotPanics(t *testing.T) {
	tests := map[string]*doer{
		"transport error": {err: errors.New("connection refused")},
		"HTTP 500":        {status: 500, body: strings.NewReader("")},
		"redirect":        {status: 302, body: strings.NewReader("")},
		"HTTP 404":        {status: 404, body: strings.NewReader("")},
	}
	for name, d := range tests {
		if err := hookProfile(RefreshHTTPGet, reloadURL).Refresh(context.Background(), d); err == nil {
			t.Errorf("%s: want an error the caller turns into a warning", name)
		}
	}
}

func TestRefreshRefusesATamperedURLWithoutRequest(t *testing.T) {
	for _, u := range []string{"http://evil.example/reloadgames", "http://localhost:1234/reloadgames", "http://127.0.0.1:1234/reloadgames?x=1"} {
		d := &doer{status: 200, body: strings.NewReader("")}
		if err := hookProfile(RefreshHTTPGet, u).Refresh(context.Background(), d); err == nil || d.req != nil {
			t.Errorf("%q: err %v, request sent %v", u, err, d.req != nil)
		}
	}
}

func TestRefreshKindsAndBodyCap(t *testing.T) {
	d := &doer{status: 200, body: strings.NewReader("")}
	if err := hookProfile(RefreshManual, "").Refresh(context.Background(), d); !errors.Is(err, ErrManualRefresh) || d.req != nil {
		t.Fatalf("manual: %v", err)
	}
	if err := hookProfile(RefreshNone, "").Refresh(context.Background(), d); err != nil || d.req != nil {
		t.Fatalf("none: %v", err)
	}
	counting := &countingReader{}
	if err := hookProfile(RefreshHTTPGet, reloadURL).Refresh(context.Background(), &doer{status: 200, body: counting}); err != nil {
		t.Fatal(err)
	}
	if counting.n > maxRefreshBody {
		t.Fatalf("read %d bytes, the cap is %d", counting.n, maxRefreshBody)
	}
}

// countingReader is an endless body that counts what is read.
type countingReader struct{ n int }

func (c *countingReader) Read(p []byte) (int, error) { c.n += len(p); return len(p), nil }

func TestRefreshClientPolicy(t *testing.T) {
	c := NewRefreshClient()
	if c.Timeout != RefreshTimeout || c.CheckRedirect == nil {
		t.Fatal("3 s timeout and a redirect policy are required")
	}
	if err := c.CheckRedirect(nil, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("redirects must not be followed: %v", err)
	}
	tr, ok := c.Transport.(*http.Transport)
	if !ok || tr.Proxy != nil {
		t.Fatal("the transport must not use a proxy")
	}
}

func TestDisabledRefreshIsNone(t *testing.T) {
	info := detect(t, testEnv(map[string]string{"usr/share/batocera/batocera.version": "40", "userdata/roms/.keep": ""}), Config{RefreshOff: true})
	if info.Profile.Hook.Kind != RefreshNone {
		t.Fatalf("profile.refresh=false must switch the hook off, got %q", info.Profile.Hook.Kind)
	}
	on := detect(t, testEnv(map[string]string{"usr/share/batocera/batocera.version": "40", "userdata/roms/.keep": ""}), Config{})
	if on.Profile.Hook.Kind != RefreshHTTPGet {
		t.Fatalf("batocera refreshes by default, got %q", on.Profile.Hook.Kind)
	}
}
