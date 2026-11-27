//go:build gui

package main

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	mlog "github.com/Or4cu1o/mothdex-cli/internal/log"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/sdl"
)

// files lists the files under root that belong to a log: a name of paths.logs, or anything below it.
func files(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			out = append(out, filepath.ToSlash(path))
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// onlyFile is the one file under root whose path holds part.
func onlyFile(t *testing.T, root, part string) string {
	t.Helper()
	var found []string
	for _, f := range files(t, root) {
		if strings.Contains(f, part) {
			found = append(found, f)
		}
	}
	if len(found) != 1 {
		t.Fatalf("want one file with %q, got %v among %v", part, found, files(t, root))
	}
	return found[0]
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // a file the test found under its own temporary folder
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func refOf(t *testing.T, stderr string) string {
	t.Helper()
	m := regexp.MustCompile(`ref: ([0-9ABCDEFGHJKMNPQRSTVWXYZ]{6})`).FindStringSubmatch(stderr)
	if m == nil {
		t.Fatalf("stderr shows no ref: %q", stderr)
	}
	return m[1]
}

// A Port shows no terminal: a configuration that does not load still leaves a log, in the default folder and
// never in a folder the bad file names (T-110 TM-27), with the ref that stderr shows.
func TestAnInvalidConfigurationIsLoggedInTheDefaultFolderOnly(t *testing.T) {
	home := isolate(t)
	elsewhere := t.TempDir()
	bad := configFile(t, "paths:\n  logs: "+elsewhere+"\ndownload:\n  concurrency: 99\n")
	var out, errOut bytes.Buffer
	if code := run([]string{"mothdex-gui", "--config", bad}, &out, &errOut); code != exitConfig {
		t.Fatalf("exit %d, stderr %q", code, errOut.String())
	}
	ref := refOf(t, errOut.String())
	if !strings.Contains(errOut.String(), "code: invalid_config") || !strings.Contains(errOut.String(), "log: ~") {
		t.Errorf("stderr lacks the code or the log: %q", errOut.String())
	}
	log := readFile(t, onlyFile(t, home, "sessions/"))
	if !strings.Contains(log, "ref="+ref+" code=invalid_config cmd=gui") || !strings.Contains(log, `msg="session start"`) {
		t.Errorf("the fallback log lacks the line of %s:\n%s", ref, log)
	}
	if got := files(t, elsewhere); len(got) != 0 {
		t.Errorf("a path of the invalid configuration was used: %v", got)
	}
}

func TestABadCommandLineIsLoggedUnderTheRefItShows(t *testing.T) {
	home := isolate(t)
	var out, errOut bytes.Buffer
	if code := run([]string{"mothdex-gui", "--lang", "pt_br", "--nosuchflag"}, &out, &errOut); code != exitConfig {
		t.Fatalf("exit %d", code)
	}
	ref := refOf(t, errOut.String())
	log := readFile(t, onlyFile(t, home, "sessions/"))
	if !strings.Contains(log, `msg="Unknown flag: -nosuchflag" ref=`+ref+" code=usage") {
		t.Errorf("the log lacks the English line of %s:\n%s", ref, log)
	}
}

// The header names the flags and keeps their values back unless they cannot hold anything personal, however the
// flag is written (T-110 TM-17).
func TestTheSessionHeaderNeverHoldsAPathOrAnUnknownValue(t *testing.T) {
	for _, args := range [][]string{
		{"-config", "/srv/private-dir/c.yaml", "--lang", "en_us"},
		{"--config=/srv/private-dir/c.yaml"},
		{"-config=/srv/private-dir/c.yaml"},
		{"--token", "private-dir"},
		{"-token", "private-dir"},
	} {
		home := isolate(t)
		var out, errOut bytes.Buffer
		run(append([]string{"mothdex-gui"}, append(args, "--window", "bad")...), &out, &errOut)
		header, _, _ := strings.Cut(readFile(t, onlyFile(t, home, "latest.log")), "\n")
		if !strings.Contains(header, "<redacted>") || strings.Contains(header, "private-dir") {
			t.Errorf("%v: header %q", args, header)
		}
	}
}

// updateLock is the file update.CleanupOld would create beside the program.
func updateLock(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(filepath.Dir(exe), ".mothdex-update.lock")
}

// The installer runs --probe as root: it, --version, --help and --spike leave no file behind.
func TestProbeVersionAndHelpWriteNoLog(t *testing.T) {
	defer func(old func() (sdl.Found, error)) { checkSDL = old }(checkSDL)
	checkSDL = func() (sdl.Found, error) { return sdl.Found{Version: sdl.Version{Major: 2, Minor: 32}}, nil }
	for _, args := range [][]string{{"--probe"}, {"--version"}, {"--help"}, {"--probe", "extra"}, {"--version", "--window", "bad"}} {
		home := isolate(t)
		lock := updateLock(t)
		_ = os.Remove(lock) // an earlier test that opened the GUI leaves it
		var out, errOut bytes.Buffer
		run(append([]string{"mothdex-gui"}, args...), &out, &errOut)
		if got := files(t, home); len(got) != 0 {
			t.Errorf("%v wrote %v", args, got)
		}
		if _, err := os.Stat(lock); err == nil {
			t.Errorf("%v left the update lock beside the program", args)
		}
	}
}

// A panic value may quote an address: the log line and the crash file are redacted, and stderr never prints it.
func TestAPanicLeavesARedactedCrashFileAndExitsOne(t *testing.T) {
	home := isolate(t)
	var errOut bytes.Buffer
	logs := startLog(nil)
	code := func() (code int) {
		defer recoverInternal(&code, options{}, logs, &errOut)
		panicWithAnAddress()
		return exitOK
	}()
	if err := logs.Close(); err != nil {
		t.Fatal(err)
	}
	ref := refOf(t, errOut.String())
	crash := readFile(t, onlyFile(t, home, "crash/"))
	latest := readFile(t, onlyFile(t, home, "latest.log"))
	if code != exitInternal || !strings.Contains(crash, "ref: "+ref) || !strings.Contains(crash, "panicWithAnAddress") {
		t.Errorf("exit %d, crash file:\n%s", code, crash)
	}
	if !strings.Contains(latest, "ref="+ref+" code=internal") || !strings.Contains(latest, "panicWithAnAddress") {
		t.Errorf("latest.log:\n%s", latest)
	}
	for name, text := range map[string]string{"crash file": crash, "latest.log": latest, "stderr": errOut.String()} {
		if strings.Contains(text, "S3CRET") {
			t.Errorf("the secret reached the %s:\n%s", name, text)
		}
	}
}

func panicWithAnAddress() { panic("get https://cdn.example.test/file?token=S3CRET failed") }

// Export logs: the zip holds this run, and the path shown does not carry the user's folder.
func TestExportLogsWritesTheBundleAndShowsItWithoutTheHomeFolder(t *testing.T) {
	home := isolate(t)
	logs := startLog(nil)
	logs.Logger().Info("gui: marker line")
	shown, err := exportLogs(logs, logs.BundleOptions)(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = logs.Close() }()
	if !strings.HasPrefix(shown, "~") || strings.Contains(shown, home) {
		t.Errorf("shown path %q", shown)
	}
	z, err := zip.OpenReader(onlyFile(t, home, "bundles/"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = z.Close() }()
	var held strings.Builder
	for _, f := range z.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(&held, r); err != nil { //nolint:gosec // the test's own small zip
			t.Fatal(err)
		}
		_ = r.Close()
	}
	if !strings.Contains(held.String(), "gui: marker line") || strings.Contains(held.String(), home) {
		t.Errorf("the bundle:\n%s", held.String())
	}
}

// What panicked may hold a lock: leaving must not wait for it.
func TestACrashedRunLeavesEvenWhenTheServiceDoesNotClose(t *testing.T) {
	isolate(t)
	defer func(old time.Duration) { closeBudget = old }(closeBudget)
	closeBudget = time.Millisecond
	release := make(chan struct{})
	defer close(release)
	closed, exited := 0, -1
	leaveCrashed(func() { closed++ }, func() { <-release }, mlog.StartSession(mlog.SessionOptions{Binary: "gui"}), func(c int) { exited = c })
	if exited != exitInternal || closed != 1 {
		t.Errorf("exit %d, backend closed %d times", exited, closed)
	}
}
