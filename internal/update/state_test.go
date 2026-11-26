package update

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
)

func fixture(t *testing.T) (*Updater, State) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil { //nolint:gosec // installation fixtures intentionally reproduce executable and state permissions.
		t.Fatal(err)
	}
	exe := filepath.Join(dir, binaryName(false))
	if err := os.WriteFile(exe, []byte("old"), 0755); err != nil { //nolint:gosec // installation fixtures intentionally reproduce executable and state permissions.
		t.Fatal(err)
	}
	s := State{SchemaVersion: 1, Source: "installer", Installer: "install.sh", Dir: dir, Version: "1.0.0", Target: target(), Profile: "generic", InstalledAt: "2026-11-26T14:00:00Z", UpdatedAt: "2026-11-26T14:00:00Z", UpdatedBy: "installer"}
	if runtime.GOOS == "windows" {
		s.Installer = "install.ps1"
	}
	writeStateFixture(t, s)
	u, err := New(Options{Exe: exe, Current: "1.0.0", Now: func() time.Time { return time.Date(2026, 11, 26, 15, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	return u, s
}
func writeStateFixture(t *testing.T, s State) {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(s.Dir, stateName), b, 0644); err != nil { //nolint:gosec // installation fixtures intentionally reproduce executable and state permissions.
		t.Fatal(err)
	}
}
func errorCode(t *testing.T, err error, code, reason string) {
	t.Helper()
	var ae *app.Error
	if !errors.As(err, &ae) {
		t.Fatalf("expected app.Error, got %v", err)
	}
	if ae.Code != code || reason != "" && ae.Args["reason"] != reason {
		t.Fatalf("got %s %v; want %s %s", ae.Code, ae.Args, code, reason)
	}
}
func TestManagedState(t *testing.T) {
	cases := []struct {
		name, reason string
		change       func(*State)
	}{
		{"valid", "", func(*State) {}},
		{"newer", "state_newer", func(s *State) { s.SchemaVersion = 2 }},
		{"package", "not_installer", func(s *State) { s.Source = "package" }},
		{"moved", "moved", func(s *State) { s.Dir = filepath.Dir(s.Dir) }},
		{"alias traversal", "bad_state", func(s *State) { s.Alias = "../../victim"; s.AliasKind = "symlink" }},
		{"reserved alias", "bad_state", func(s *State) { s.Alias = "nul"; s.AliasKind = "symlink" }},
		{"wrong target", "bad_state", func(s *State) { s.Target = "other_arch" }},
		{"version injection", "bad_state", func(s *State) { s.Version = "1.0.0\x1b[31m" }},
		{"installer", "bad_state", func(s *State) { s.Installer = "other" }},
		{"timestamp", "bad_state", func(s *State) { s.InstalledAt = "yesterday" }},
		{"path", "bad_state", func(s *State) {
			s.PathAdded = true
			s.PathKind = "rc_line"
			s.PathFile = "/etc/profile"
			s.PathValue = "$(touch victim)"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u, s := fixture(t)
			tc.change(&s)
			b, _ := json.Marshal(s)
			if err := os.WriteFile(filepath.Join(filepath.Dir(u.o.Exe), stateName), b, 0644); err != nil { //nolint:gosec // installation fixtures intentionally reproduce executable and state permissions.
				t.Fatal(err)
			}
			_, err := u.Managed()
			if tc.reason == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				errorCode(t, err, app.CodeUpdateUnavailable, tc.reason)
			}
		})
	}
}
func TestManagedMissingCorruptBOM(t *testing.T) {
	for _, tc := range []struct{ name, body, reason string }{{"missing", "", "no_state"}, {"corrupt", "{", "bad_state"}, {"trailing", "{} {}", "bad_state"}, {"oversized", string(make([]byte, (16<<10)+1)), "bad_state"}, {"bom", "BOM", ""}} {
		t.Run(tc.name, func(t *testing.T) {
			u, s := fixture(t)
			p := filepath.Join(s.Dir, stateName)
			switch tc.body {
			case "":
				_ = os.Remove(p)
			case "BOM":
				b, _ := json.Marshal(s)
				_ = os.WriteFile(p, append([]byte{0xef, 0xbb, 0xbf}, b...), 0644) //nolint:gosec // installation fixtures intentionally reproduce executable and state permissions.
			default:
				_ = os.WriteFile(p, []byte(tc.body), 0644) //nolint:gosec // installation fixtures intentionally reproduce executable and state permissions.
			}
			_, err := u.Managed()
			if tc.reason == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				errorCode(t, err, app.CodeUpdateUnavailable, tc.reason)
			}
		})
	}
}
func TestVersions(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{{"1.9.0", "1.10.0", -1}, {"1.0.0-beta.2", "1.0.0-beta.10", -1}, {"1.0.0", "1.0.0-rc.1", 1}, {"1.0.0+abc", "1.0.0+def", 0}, {"1.0.0-alpha", "1.0.0-alpha.1", -1}, {"1.0.0-1", "1.0.0-a", -1}} {
		if got := compare(tc.a, tc.b); got != tc.want {
			t.Errorf("compare(%s,%s)=%d", tc.a, tc.b, got)
		}
	}
	for _, v := range []string{"v1.0.0", "01.0.0", "1.0.0-01", "1.0.0\n", "1.0.0-", "1.2", "1.0.0?x"} {
		if validVersion(v) {
			t.Errorf("accepted %q", v)
		}
	}
}
