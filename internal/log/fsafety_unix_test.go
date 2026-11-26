//go:build !windows

package log

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// foreign gives path to another user, which only root can do.
func foreign(t *testing.T, path string) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("changing the owner needs root")
	}
	if err := os.Chown(path, 12345, 12345); err != nil {
		t.Skip("cannot change the owner here:", err)
	}
}

// victim is a file outside the log folder that a hard link brings inside it.
func victim(t *testing.T, link string) string {
	t.Helper()
	path := filepath.Join(filepath.Dir(filepath.Dir(link)), "victim")
	if err := safeDir(filepath.Dir(link)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("S3CRET untouched\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(path, link); err != nil {
		t.Skip("no hard links here:", err)
	}
	return path
}

func TestAFileOfAnotherUserOrWithAnotherNameIsRefused(t *testing.T) {
	t.Run("hard linked latest.log", func(t *testing.T) {
		dir := logDir(t)
		path := victim(t, filepath.Join(dir, LatestName))
		s := startAt(t, 0)
		if err := s.Attach(AttachOptions{Dir: dir}); !errors.Is(err, errUnsafeDir) {
			t.Fatalf("Attach = %v", err)
		}
		slog.New(s.Handler()).Info("line")
		if readFile(t, path) != "S3CRET untouched\n" || s.LogPath() != s.SessionPath() {
			t.Error("the linked file was written or latest.log is claimed")
		}
	})
	t.Run("foreign latest.log", func(t *testing.T) {
		dir := logDir(t)
		plant(t, dir, ".", LatestName)
		foreign(t, filepath.Join(dir, LatestName))
		s := startAt(t, 0)
		if err := s.Attach(AttachOptions{Dir: dir}); !errors.Is(err, errUnsafeDir) {
			t.Fatalf("Attach = %v", err)
		}
		if readFile(t, filepath.Join(dir, LatestName)) != "x\n" {
			t.Error("another user's file was written")
		}
	})
	t.Run("hard linked session is not read", func(t *testing.T) {
		dir := logDir(t)
		const id = "20260101-000000-cli-1"
		victim(t, filepath.Join(dir, SessionsDir, id+".log"))
		if lines, err := Lines(dir, id, 0); err == nil {
			t.Errorf("read %q", lines)
		}
		res, err := Bundle(context.Background(), BundleOptions{Dir: dir, Now: func() time.Time { return testStart }})
		if err != nil || len(res.Sessions) != 0 {
			t.Fatalf("%+v, %v", res, err)
		}
		for name, content := range unzip(t, res.Path) {
			if strings.Contains(content, "untouched") {
				t.Errorf("%s holds the linked file", name)
			}
		}
	})
}

// shared makes a folder others can write to, sticky or not.
func shared(t *testing.T, mode os.FileMode) string {
	t.Helper()
	dir := logDir(t)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, mode); err != nil { // #nosec G302 -- the unsafe case under test
		t.Fatal(err)
	}
	return dir
}

func TestASharedFolderFallsBackUnlessItsFileSystemHasNoModes(t *testing.T) {
	real := permissionless
	t.Cleanup(func() { permissionless = real })

	session := func(t *testing.T, fallback string) *Session {
		t.Helper()
		s := StartSession(SessionOptions{Binary: "cli", Now: func() time.Time { return testStart },
			DefaultDir: func() (string, string, error) { return fallback, "", nil }})
		t.Cleanup(func() { _ = s.Close() })
		return s
	}
	t.Run("ext4 shared, even sticky", func(t *testing.T) {
		permissionless = func(string) bool { return false }
		for _, mode := range []os.FileMode{0o777, 0o777 | os.ModeSticky} {
			dir, fallback := shared(t, mode), logDir(t)
			s := session(t, fallback)
			logger, closer, err := New(Options{Level: "info", File: false, Session: s})
			if err != nil {
				t.Fatal(err)
			}
			err = s.Attach(AttachOptions{Dir: dir})
			if err == nil || !strings.Contains(err.Error(), dir) || !strings.Contains(err.Error(), fallback) {
				t.Fatalf("%v: warning = %v", mode, err)
			}
			logger.Info("moved")
			_ = closer.Close()
			if s.Dir() != fallback || !strings.Contains(readFile(t, s.SessionPath()), "moved") {
				t.Errorf("%v: dir %q", mode, s.Dir())
			}
			if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
				t.Errorf("%v: the shared folder got %v, %v", mode, entries, err)
			}
			_, closer, err = New(Options{Level: "info", File: true, Dir: dir, MaxSizeMB: 1, MaxFiles: 1, Session: s})
			if err != nil || !exists(filepath.Join(fallback, FileName)) {
				t.Errorf("%v: mothdex.log did not follow: %v", mode, err)
			}
			_ = closer.Close()
		}
	})
	t.Run("exfat", func(t *testing.T) {
		dir := shared(t, 0o777)
		permissionless = func(path string) bool { return strings.HasPrefix(path, dir) }
		s := session(t, logDir(t))
		attach(t, s, dir)
		if s.Dir() != dir || !NoPermissions(dir) || !strings.Contains(readFile(t, s.SessionPath()), msgNoModes) {
			t.Errorf("dir %q: %s", s.Dir(), readFile(t, s.SessionPath()))
		}
		if !strings.Contains(readFile(t, filepath.Join(dir, LatestName)), "level=WARN") {
			t.Error("latest.log lacks the warning")
		}
	})
}

// The runtime writes crash traces to the session file as they are: showing them redacts them.
func TestShownLinesAreRedacted(t *testing.T) {
	dir := logDir(t)
	const id = "20260101-000000-cli-1"
	plant(t, dir, SessionsDir, id+".log")
	raw := "panic: Get \"https://cdn.example/f?token=S3CRET\": api https://my.api.example/v1/x\n" +
		"time=x level=ERROR ref=ABC123 msg=x\nserver said token=S3CRET sig=S3CRET X-Amz-Signature=S3CRET password=S3CRET api_key=S3CRET\n"
	if err := os.WriteFile(sessionFile(dir, id), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	lines, err := Lines(dir, id, 0, "https://my.api.example/v1")
	if err != nil {
		t.Fatal(err)
	}
	matches, err := Find(dir, "", "ABC123", "https://my.api.example/v1")
	if err != nil {
		t.Fatal(err)
	}
	for _, shown := range [][]string{lines, matches[0].Context} {
		if got := strings.Join(shown, "\n"); strings.Contains(got, "S3CRET") || strings.Contains(got, "my.api.example") {
			t.Errorf("shown: %s", got)
		}
	}
}
