package log

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/buildinfo"
)

var testStart = time.Date(2026, 11, 23, 1, 47, 0, 0, time.UTC)

// startAt is a session that began at testStart plus offset and writes nowhere until attached.
func startAt(t *testing.T, offset time.Duration, args ...string) *Session {
	t.Helper()
	s := StartSession(SessionOptions{
		Binary: "cli", Args: args, Build: buildinfo.Info{Version: "0.2.0"},
		Now:        func() time.Time { return testStart.Add(offset) },
		DefaultDir: func() (string, string, error) { return "", "", errors.New("no default folder in tests") },
	})
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func attach(t *testing.T, s *Session, dir string) {
	t.Helper()
	if err := s.Attach(AttachOptions{Dir: dir}); err != nil {
		t.Fatal(err)
	}
}

func logDir(t *testing.T) string { return filepath.Join(t.TempDir(), "logs") }

func TestLinesBeforeAttachReachTheFilesAndTheLevelApplies(t *testing.T) {
	dir := logDir(t)
	s := startAt(t, 0, "search", "mario")
	l := slog.New(s.Handler())
	l.Debug("early debug")
	l.Info("early info")
	if err := s.Attach(AttachOptions{Dir: dir, Level: "info"}); err != nil {
		t.Fatal(err)
	}
	l.Debug("late debug")
	l.Info("late info")
	for _, path := range []string{s.SessionPath(), filepath.Join(dir, LatestName)} {
		got := readFile(t, path)
		if !strings.Contains(strings.SplitN(got, "\n", 2)[0], `msg="session start"`) {
			t.Errorf("%s does not start with the header: %q", path, got)
		}
		if strings.Contains(got, "debug") || !strings.Contains(got, "early info") || !strings.Contains(got, "late info") {
			t.Errorf("%s = %q", path, got)
		}
	}
	if !sessionIDPattern.MatchString(s.ID()) || !strings.HasPrefix(s.ID(), "20261123-014700-cli-") {
		t.Errorf("id = %q", s.ID())
	}
}

func TestTheNewestSessionOwnsLatestLog(t *testing.T) {
	dir := logDir(t)
	first, second := startAt(t, 0), startAt(t, time.Second)
	attach(t, first, dir)
	if got := first.LogPath(); got != filepath.Join(dir, LatestName) {
		t.Fatalf("first LogPath = %q", got)
	}
	slog.New(first.Handler()).Info("first-before")
	attach(t, second, dir)
	slog.New(first.Handler()).Info("first-after")
	slog.New(second.Handler()).Info("second-line")

	latest := readFile(t, filepath.Join(dir, LatestName))
	if strings.Contains(latest, "first-") || !strings.Contains(latest, "second-line") {
		t.Errorf("latest.log = %q", latest)
	}
	if got := first.LogPath(); got != first.SessionPath() {
		t.Errorf("a session that lost latest.log reports %q", got)
	}
	own := readFile(t, first.SessionPath())
	if !strings.Contains(own, "first-before") || !strings.Contains(own, "first-after") || strings.Contains(own, "second-line") {
		t.Errorf("first session = %q", own)
	}
}

func TestAHeldLockDelaysButNeverStopsASession(t *testing.T) {
	dir := logDir(t)
	if err := safeDir(dir); err != nil {
		t.Fatal(err)
	}
	holder, err := openFile(filepath.Join(dir, lockName), os.O_CREATE|os.O_RDWR)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Close() }()
	if ok, err := tryLock(holder); !ok || err != nil {
		t.Fatalf("lock: %v %v", ok, err)
	}
	s := startAt(t, 0)
	if err := s.Attach(AttachOptions{Dir: dir}); !errors.Is(err, errLockBusy) {
		t.Fatalf("Attach = %v, want the busy lock", err)
	}
	slog.New(s.Handler()).Info("still logged")
	if s.LogPath() != s.SessionPath() || !strings.Contains(readFile(t, s.SessionPath()), "still logged") {
		t.Error("the session file is not used while the lock is held")
	}
}

func TestNothingSensitiveReachesAnyFile(t *testing.T) {
	dir := logDir(t)
	const api = "https://apiuser:apipass@api.internal.example/v1" // #nosec G101 -- synthetic, the redaction under test
	s := startAt(t, 0, "config", "set", "api.base_url", api, "--config", "/home/bob/c.yaml", "-o/home/bob/roms", "--made-up", "S3CRET-flag")
	// logged before the configuration is known: redacted again when it is
	slog.New(s.Handler()).Info("early", "host", "api.internal.example")
	logger, closer, err := New(Options{Level: "debug", File: true, Dir: dir, MaxSizeMB: 1, MaxFiles: 1, Session: s, Secrets: []string{api}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Attach(AttachOptions{Dir: dir, Secrets: []string{api}}); err != nil {
		t.Fatal(err)
	}
	failure := &url.Error{Op: "Get", URL: "https://cdn.example/file.zip?token=S3CRET#frag", Err: errors.New("proxy http://pu:S3CRET@proxy.example:8080 refused")}
	logger.Debug("download failed", "err", failure)
	logger.Info("api", "err", &url.Error{Op: "Get", URL: api + "/entries?q=1", Err: errors.New("dial tcp: lookup api.internal.example: no such host")})
	logger.With("Authorization", "Bearer S3CRET").WithGroup("g").Info("headers",
		"x_api_key_token", "S3CRET", "headers", http.Header{"Cookie": {"id=S3CRET"}}, "raw", "X-Api-Key: S3CRET", "proxy", "pu:S3CRET@proxy.example:8080")
	if _, err = s.WriteCrash("boom at "+api+" and https://h.example/p?token=S3CRET", []byte("stack"), "ABC123"); err != nil {
		t.Fatal(err)
	}
	_ = closer.Close()
	_ = s.Close()

	files := 0
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		files++
		got := readFile(t, path)
		for _, secret := range []string{"S3CRET", "apiuser", "apipass", "api.internal.example", "/home/bob"} {
			if strings.Contains(got, secret) {
				t.Errorf("%s holds %q:\n%s", filepath.Base(path), secret, got)
			}
		}
		return nil
	})
	if err != nil || files < 4 {
		t.Fatalf("walked %d files: %v", files, err)
	}
	if got := readFile(t, filepath.Join(dir, FileName)); !strings.Contains(got, "https://cdn.example/file.zip") || !strings.Contains(got, apiMarker) {
		t.Errorf("the useful part of the addresses is gone: %s", got)
	}
}

func TestHeaderKeepsOnlyAllowedFlagValues(t *testing.T) {
	rules := map[string]FlagRule{
		"platform": {TakesValue: true, Log: true}, "p": {TakesValue: true, Log: true},
		"config": {TakesValue: true}, "q": {}, "json": {Log: true},
	}
	scrub := newScrubber("/home/bob", "")
	cases := map[string]string{ // #nosec G101 -- synthetic values the header must hide
		"search mario --platform snes -pgba --json": "search mario --platform snes -p gba --json",
		"--config /etc/c.yaml search x":             "--config <redacted> search x",
		"--config=/etc/c.yaml -qpsnes":              "--config=<redacted> -q -p snes",
		"config set download.dir secret":            "config set download.dir <redacted>",
		"--nope hunter2 info /home/bob/x":           "--nope <redacted> info ~/x",
		"info https://u:p@h/x -zhunter2":            "info <redacted> -z <redacted>",
		"--platform https://h/x -- a@b.example":     "--platform <redacted> -- a@b.example",
	}
	for in, want := range cases {
		if got := strings.Join(maskArgs(strings.Fields(in), rules, scrub), " "); got != want {
			t.Errorf("%q => %q, want %q", in, got, want)
		}
	}
}

func TestAnUnsafeLogFolderDisablesFileLoggingOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX links and modes")
	}
	t.Run("shared folder", func(t *testing.T) {
		dir := logDir(t)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0o777); err != nil { // #nosec G302 -- the unsafe case under test
			t.Fatal(err)
		}
		s := startAt(t, 0)
		if err := s.Attach(AttachOptions{Dir: dir}); !errors.Is(err, errUnsafeDir) {
			t.Fatalf("Attach = %v", err)
		}
		logger, closer, err := New(Options{Level: "info", File: true, Dir: dir, MaxSizeMB: 1, MaxFiles: 1, Session: s})
		if err == nil {
			t.Error("mothdex.log was accepted in a shared folder")
		}
		logger.Error("dropped")
		_ = closer.Close()
		if entries, _ := os.ReadDir(dir); len(entries) != 0 || s.LogPath() != "" {
			t.Errorf("entries %v, path %q", entries, s.LogPath())
		}
	})
	t.Run("planted latest.log link", func(t *testing.T) {
		dir := logDir(t)
		victim := filepath.Join(t.TempDir(), "victim")
		if err := os.WriteFile(victim, []byte("untouched"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := safeDir(dir); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(victim, filepath.Join(dir, LatestName)); err != nil {
			t.Fatal(err)
		}
		s := startAt(t, 0)
		if err := s.Attach(AttachOptions{Dir: dir}); err == nil {
			t.Error("the link was followed or ignored silently")
		}
		slog.New(s.Handler()).Info("line")
		if readFile(t, victim) != "untouched" || s.LogPath() != s.SessionPath() {
			t.Error("the link target changed or latest.log is still claimed")
		}
		info, err := os.Stat(s.SessionPath())
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("session file mode: %v %v", info, err)
		}
	})
}

// plant creates files in a subfolder of the log folder.
func plant(t *testing.T, dir, sub string, names ...string) {
	t.Helper()
	if err := safeDir(filepath.Join(dir, sub)); err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, sub, n), []byte("x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRetentionAndClearDeleteOnlyOurFiles(t *testing.T) {
	dir := logDir(t)
	old := []string{"20260101-000000-cli-1.log", "20260102-000000-gui-2.log", "20260103-000000-cli-3.log"}
	plant(t, dir, SessionsDir, append([]string{"foo.txt", "20260101-000000-cli-1.log.bak"}, old...)...)
	if err := os.Mkdir(filepath.Join(dir, SessionsDir, "20250101-000000-cli-9.log"), 0o700); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(t.TempDir(), "important")
	if err := os.WriteFile(victim, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	linked := os.Symlink(victim, filepath.Join(dir, SessionsDir, "20250102-000000-cli-9.log")) == nil
	plant(t, dir, ".", "20260101-000000-cli-1.log")

	s := startAt(t, 0)
	if err := s.Attach(AttachOptions{Dir: dir, Keep: 2}); err != nil {
		t.Fatal(err)
	}
	got := matching(filepath.Join(dir, SessionsDir), sessionFilePattern)
	if len(got) != 2 || got[0] != old[2] || got[1] != s.ID()+".log" {
		t.Fatalf("kept %v", got)
	}
	plant(t, dir, CrashDir, "20260101-000000-cli-1.txt", "notes.txt")
	plant(t, dir, BundlesDir, "mothdex-logs-20260101-000000.zip", "mine.zip")
	removed, err := Clear(dir, s.ID())
	if err != nil || removed != (Removed{Sessions: 1, Crashes: 1, Bundles: 1}) {
		t.Fatalf("removed %+v, %v", removed, err)
	}
	survivors := []string{"sessions/foo.txt", "sessions/20260101-000000-cli-1.log.bak", "sessions/20250101-000000-cli-9.log",
		"crash/notes.txt", "bundles/mine.zip", "20260101-000000-cli-1.log", "sessions/" + s.ID() + ".log"}
	if linked {
		survivors = append(survivors, "sessions/20250102-000000-cli-9.log")
	}
	for _, name := range survivors {
		if !exists(filepath.Join(dir, filepath.FromSlash(name))) {
			t.Errorf("%s was deleted", name)
		}
	}
	if readFile(t, victim) != "keep" {
		t.Error("the link target changed")
	}
}

func TestShowFindsARefAndIgnoresForgedLines(t *testing.T) {
	dir := logDir(t)
	s := startAt(t, 0)
	attach(t, s, dir)
	l := slog.New(s.Handler())
	l.Info("title from a server", "title", "x\nlevel=ERROR ref=AAAAAA \x1b]52;c;cGFzdGU=\a")
	for range 12 {
		l.Debug("noise")
	}
	var refs Refs
	failure := errors.New("no such host")
	ref := refs.Of(l, failure, ErrorRecord{Code: "network", Cmd: "search", Message: "cannot reach the server", Cause: failure})
	if again := refs.Of(l, failure, ErrorRecord{Code: "network"}); again != ref {
		t.Errorf("a redrawn error got a new ref: %s, %s", ref, again)
	}
	matches, err := Find(dir, "", ref)
	if err != nil || len(matches) != 1 || matches[0].Session != s.ID() || len(matches[0].Context) != ShowContext+1 {
		t.Fatalf("matches %+v, %v", matches, err)
	}
	if line := matches[0].Context[ShowContext]; !strings.Contains(line, "code=network cmd=search") || !strings.Contains(line, `cause="no such host"`) {
		t.Errorf("line = %q", line)
	}
	if forged, _ := Find(dir, "", "AAAAAA"); len(forged) != 0 {
		t.Errorf("a value inside a line matched: %+v", forged)
	}
	list := List(dir, s.ID())
	if len(list) != 1 || list[0].Errors != 1 || !list[0].Current || list[0].Binary != "cli" {
		t.Errorf("list = %+v", list)
	}
	for _, id := range []string{"../../etc/passwd", `..\x`, "latest", "20260101-000000-cli-1"} {
		if _, err := Lines(dir, id, 0); !errors.Is(err, ErrSession) {
			t.Errorf("Lines(%q) = %v", id, err)
		}
	}
}

func TestRefsAreCrockfordAndTypingIsForgiven(t *testing.T) {
	for range 2000 {
		if ref := NewRef(); !refPattern.MatchString(ref) {
			t.Fatalf("ref %q", ref)
		}
	}
	if got, err := ParseRef(" 7f3-koL "); err != nil || got != "7F3K01" {
		t.Errorf("ParseRef = %q, %v", got, err)
	}
	for _, bad := range []string{"", "7F3K2", "7F3K2QQ", "7F3K2U", "../abc"} {
		if _, err := ParseRef(bad); !errors.Is(err, ErrRef) {
			t.Errorf("ParseRef(%q) = %v", bad, err)
		}
	}
}

func TestACrashLeavesTheStackAndALine(t *testing.T) {
	dir := logDir(t)
	s := startAt(t, 0)
	attach(t, s, dir)
	var path string
	func() {
		defer func() {
			v := recover()
			slog.New(s.Handler()).Error("panic", "ref", "ABC123", "code", "internal")
			var err error
			if path, err = s.WriteCrash(v, debug.Stack(), "ABC123"); err != nil {
				t.Error(err)
			}
		}()
		panic("forced")
	}()
	got := readFile(t, path)
	for _, want := range []string{"ref: ABC123", "panic: forced", "TestACrashLeavesTheStackAndALine"} {
		if !strings.Contains(got, want) {
			t.Errorf("crash file lacks %q:\n%s", want, got)
		}
	}
	if filepath.Dir(path) != filepath.Join(dir, CrashDir) || !strings.Contains(readFile(t, filepath.Join(dir, LatestName)), "ref=ABC123") {
		t.Errorf("path %q or latest.log line missing", path)
	}
}

func TestCloseWithoutAttachWritesToTheDefaultFolder(t *testing.T) {
	dir := logDir(t)
	s := StartSession(SessionOptions{Binary: "gui", Now: func() time.Time { return testStart },
		DefaultDir: func() (string, string, error) { return dir, "", nil }})
	slog.New(s.Handler()).Error("configuration refused")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, filepath.Join(dir, LatestName))
	for _, want := range []string{"configuration refused", `msg="session end"`, "errors=1"} {
		if !strings.Contains(got, want) {
			t.Errorf("lacks %q: %s", want, got)
		}
	}
	if err := s.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

// Looking at the logs must not push the session with the evidence out of the retention, and an
// error shown meanwhile must still be found by its ref.
func TestAGeneralOnlySessionKeepsNoSessionFile(t *testing.T) {
	dir := logDir(t)
	evidence := startAt(t, 0)
	attach(t, evidence, dir)
	slog.New(evidence.Handler()).Error("the evidence", "ref", "ABC123")
	_ = evidence.Close()
	general := func(offset time.Duration) *Session {
		s := StartSession(SessionOptions{Binary: "cli", GeneralOnly: true, Now: func() time.Time { return testStart.Add(offset) }})
		if err := s.Attach(AttachOptions{Dir: dir, Keep: 1}); err != nil {
			t.Fatal(err)
		}
		slog.New(s.Handler()).Info("looking")
		return s
	}
	quiet := general(time.Second)
	if quiet.Dir() != dir || quiet.SessionPath() != "" || quiet.LogPath() != "" {
		t.Errorf("dir %q, session %q, log %q", quiet.Dir(), quiet.SessionPath(), quiet.LogPath())
	}
	if err := quiet.Close(); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(dir, FileName)) {
		t.Error("a run without errors wrote mothdex.log")
	}
	failing := general(2 * time.Second)
	slog.New(failing.Handler()).Error("it failed", "ref", "DEF456")
	if failing.LogPath() != filepath.Join(dir, FileName) {
		t.Errorf("log path %q", failing.LogPath())
	}
	if err := failing.Close(); err != nil {
		t.Fatal(err)
	}
	if len(List(dir, "")) != 1 || !strings.Contains(readFile(t, filepath.Join(dir, LatestName)), "the evidence") {
		t.Fatal("the sessions changed")
	}
	for ref, where := range map[string]string{"ABC123": evidence.ID(), "DEF456": FileName} {
		if found, err := Find(dir, "", ref); err != nil || len(found) != 1 || found[0].Session != where {
			t.Errorf("%s: %+v, %v", ref, found, err)
		}
	}
}

func TestFindPassesOverAnUnreadableSession(t *testing.T) {
	dir := logDir(t)
	s := startAt(t, 0)
	attach(t, s, dir)
	slog.New(s.Handler()).Error("x", "ref", "ABC123")
	plant(t, dir, ".", "elsewhere")
	if err := os.Link(filepath.Join(dir, "elsewhere"), sessionFile(dir, "20990101-000000-cli-1")); err != nil {
		t.Skip("no hard links here:", err)
	}
	if found, err := Find(dir, "", "ABC123"); err != nil || len(found) != 1 {
		t.Errorf("%+v, %v", found, err)
	}
}

func TestRefsForgetsTheOldestErrors(t *testing.T) {
	var refs Refs
	first := errors.New("first")
	ref := refs.Of(nil, first, ErrorRecord{})
	for i := range maxRefs {
		refs.Of(nil, fmt.Errorf("error %d", i), ErrorRecord{})
	}
	if len(refs.seen) != maxRefs || refs.Of(nil, first, ErrorRecord{}) == ref {
		t.Errorf("%d remembered", len(refs.seen))
	}
}

func TestDisplayPathHidesTheUser(t *testing.T) {
	cases := []struct{ goos, home, local, path, want string }{
		{"linux", "/home/bob", "", "/home/bob/.local/state/mothdex/logs/latest.log", "~/.local/state/mothdex/logs/latest.log"},
		{"linux", "/home/bob", "", "/home/bobby/x", "/home/bobby/x"},
		{"windows", `C:\Users\Bob`, `C:\Users\Bob\AppData\Local`, `c:\users\bob\appdata\local\mothdex\logs`, `%LocalAppData%\mothdex\logs`},
		{"windows", `C:\Users\Bob`, `D:\Local`, `C:\Users\Bob\logs`, `%USERPROFILE%\logs`},
	}
	for _, c := range cases {
		if got := displayPath(c.goos, c.home, c.local, c.path); got != c.want {
			t.Errorf("displayPath(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}
